// Package persist 把交付产出固化：本地 commit（绿地）、push + PR（绑定仓库）。
// 引擎在 code_review 门禁到达时调用——产物从此不随 workdir 清理而消失。
//
// 失败语义（数据安全）：
//   - commit/push 失败 → 返回 error，引擎 blocked 且不释放 workdir（人工救援）；
//   - PR 创建失败 → 只记 PRError，不算整体失败（push 已把产出固化到远端）。
package persist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tokfinity/infera/internal/git"
)

// Input 是一次固化的全部输入。RepoURL 空 = 绿地（只本地 commit，不 push）。
type Input struct {
	DeliveryID string
	RepoURL    string // 空 = 绿地
	BaseBranch string // PR 目标分支（绿地为空）
	BaseCommit string // clone 时的快照（绿地为空）
	Workdir    string
	Title      string
}

// Result 是固化产出：Diff 供 artifact 落库，Branch/PRURL 供展示。
type Result struct {
	Diff    string // 基线→当前的完整 diff
	PRURL   string // 开出的 PR 地址（绿地/本地远端/PR 失败为空）
	Branch  string // 产出所在分支（绑库 = 推上去的分支；绿地 = 本地 main）
	PRError string // PR 创建失败原因（push 已成功，不作为整体失败）
}

// Persister 是引擎依赖的最小契约（便于测试替身）。
type Persister interface {
	Persist(ctx context.Context, in Input) (Result, error)
}

// Local 基于本地 git 命令的实现。Token 用于 github.com 的 PR 创建
// （push 注入走 Git 实例自带的 Token）。
type Local struct {
	Git   *git.Git
	Token string

	apiBase string // GitHub API 地址，测试可替换；默认 api.github.com
}

func NewLocal(g *git.Git, token string) *Local {
	return &Local{Git: g, Token: token, apiBase: "https://api.github.com"}
}

// Persist 固化流程：确保是 git 仓库（绿地 init）→ commit → diff → push（绑库）
// → PR（仅 github.com + token）。驳回重做会再次调用：force 推同一分支。
//
// push/PR 的跳过规则（防存储与 422 噪音）：
//   - HEAD == 基线且本轮无新 commit（克隆后零产出）：不推——分支缺失正是
//     父合并循环「无变更子需求」的跳过信号；
//   - 远端分支已是 HEAD 内容（驳回重做但 agent 没改东西）：跳过 push/PR——
//     内容一致，PR 必然 422。
//
// 注意"workdir 干净"不等于"无变更"：合并循环的父在 persist 前已有合并 commit
// （HEAD 领先基线但 add -A 无新增），必须照常推。
func (l *Local) Persist(ctx context.Context, in Input) (Result, error) {
	res := Result{Branch: "main"} // 绿地产出落在本地 main
	if _, err := os.Stat(filepath.Join(in.Workdir, ".git")); os.IsNotExist(err) {
		// 绿地：Acquire 只建了目录，这里补成 git 仓库（含空 initial commit，HEAD 恒存在）。
		if err := l.Git.InitRepo(ctx, in.Workdir); err != nil {
			return res, fmt.Errorf("git init: %w", err)
		}
	}
	committed, err := l.Git.Commit(ctx, in.Workdir, commitMsg(in.Title, idPrefix(in.DeliveryID)))
	if err != nil {
		return res, fmt.Errorf("git commit: %w", err)
	}

	if in.BaseCommit != "" {
		res.Diff, err = l.Git.DiffRange(ctx, in.Workdir, in.BaseCommit)
	} else {
		res.Diff, err = l.Git.DiffRoot(ctx, in.Workdir)
	}
	if err != nil {
		return res, fmt.Errorf("git diff: %w", err)
	}

	if in.RepoURL == "" {
		return res, nil // 绿地：本地 commit 即固化（diff 走 artifact 落库）
	}
	res.Branch = "infera/" + idPrefix(in.DeliveryID)
	head, err := l.Git.Head(ctx, in.Workdir)
	if err != nil {
		return res, fmt.Errorf("git head: %w", err)
	}
	if !committed && head == in.BaseCommit {
		return res, nil // 零产出：不推分支（合并循环按"分支缺失"跳过）
	}
	if remoteTip, err := l.Git.RemoteRef(ctx, in.RepoURL, "refs/heads/"+res.Branch); err == nil && remoteTip == head {
		return res, nil // 远端已是该内容：无变更轮次，跳过 push/PR
	}
	if err := l.Git.Push(ctx, in.Workdir, in.RepoURL, "refs/heads/"+res.Branch, true); err != nil {
		return res, fmt.Errorf("git push: %w", err)
	}
	// PR 只对 github.com 且有 token 时尝试；失败不影响整体（push 已固化）。
	if owner, repo, ok := githubRepo(in.RepoURL); ok && l.Token != "" {
		res.PRURL, res.PRError = l.createPR(ctx, owner, repo, res.Branch, in.BaseBranch, in.Title)
	}
	return res, nil
}

// idPrefix 取 deliveryID 前 8 位做分支名后缀（UUID 前 8 位是纯 hex，无连字符）。
func idPrefix(deliveryID string) string {
	if len(deliveryID) > 8 {
		return deliveryID[:8]
	}
	return deliveryID
}

func commitMsg(title, prefix string) string {
	return fmt.Sprintf("infera: %s [%s]", title, prefix)
}

// githubRepo 解析 https://github.com/{owner}/{repo}(.git)；
// 其余（gitlab / 自托管 / 本地路径）不适用——push 已完成，只是跳过 PR。
func githubRepo(rawURL string) (owner, repo string, ok bool) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(rawURL, prefix) {
		return "", "", false
	}
	path := strings.TrimSuffix(strings.TrimPrefix(rawURL, prefix), ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// prTimeout 限制单次 PR 创建调用：引擎用 background ctx 驱动，
// 不能让挂起的 GitHub 请求无限期占住 per-delivery 驱动。
const prTimeout = 30 * time.Second

// createPR 调 GitHub REST 建 PR；成功返回 html_url，失败返回错误描述。
// 不返回 error——PR 失败不算固化失败（见包注释）。
func (l *Local) createPR(ctx context.Context, owner, repo, branch, base, title string) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()

	body, _ := json.Marshal(map[string]string{"title": title, "head": branch, "base": base})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		l.apiBase+"/repos/"+owner+"/"+repo+"/pulls", bytes.NewReader(body))
	if err != nil {
		return "", err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+l.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		var out struct {
			HTMLURL string `json:"html_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return "", err.Error()
		}
		return out.HTMLURL, ""
	}
	// 常见错误如 422（PR 已存在）：留描述进事件即可，分支上的产出已固化。
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return "", fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
}
