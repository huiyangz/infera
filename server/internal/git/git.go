// Package git 封装 git 命令行。agent 无关、引擎无关的纯库。
// token 注入 https URL（GitHub PAT），本地路径原样使用（测试/自托管）。
//
// 安全约定：
//   - 所有 git 子进程走同一个 run()：ctx 可取消/超时、禁用终端与 askpass
//     提示、忽略宿主机 global/system git 配置（credential helper 不干扰）。
//   - token 只存在于一次性命令行 URL 参数里；错误信息一律 redact；
//     克隆成功后立即把 origin 重置回 rawURL，secret 不落 .git/config。
package git

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

// ErrMergeConflict 合并产生冲突（输出含 CONFLICT，git 以非零退出）。
// 调用方用 errors.Is 识别后走人工解冲突流程。
var ErrMergeConflict = errors.New("git merge: conflict")

type Git struct{ Token string }

func New() *Git { return &Git{} }

func injectToken(rawURL, token string) string {
	if token == "" || !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = url.UserPassword(token, "")
	return u.String()
}

// gitEnv 给所有 git 子进程的环境变量：非交互（无 terminal prompt / askpass，
// CI 环境绝不挂起）+ hermetic（忽略 global/system 配置，宿主 credential
// helper 等不干扰）。同名继承变量先移除，保证覆盖一定生效。
func gitEnv() []string {
	const terminalPrompt = "GIT_TERMINAL_PROMPT=0"
	const askpass = "GIT_ASKPASS=/bin/true"
	const configGlobal = "GIT_CONFIG_GLOBAL=/dev/null"
	const configSystem = "GIT_CONFIG_SYSTEM=/dev/null"
	overrides := []string{terminalPrompt, askpass, configGlobal, configSystem}

	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		dup := false
		for _, o := range overrides {
			if strings.HasPrefix(kv, strings.SplitN(o, "=", 2)[0]+"=") {
				dup = true
				break
			}
		}
		if !dup {
			env = append(env, kv)
		}
	}
	return append(env, overrides...)
}

// redact 把输出/错误信息里的 token（含 URL 转义形态）抹掉，防止 secret 进日志。
func (g *Git) redact(s string) string {
	if g.Token == "" {
		return s
	}
	s = strings.ReplaceAll(s, g.Token, "***")
	if esc := url.QueryEscape(g.Token); esc != g.Token {
		s = strings.ReplaceAll(s, esc, "***")
	}
	return s
}

// run 是所有 git 调用的唯一出口：ctx 取消即杀进程，错误信息 redact token。
func (g *Git) run(ctx context.Context, cwd string, args ...string) (string, error) {
	full := append([]string{"-c", "user.email=agent@infera.dev", "-c", "user.name=infera-agent"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = cwd
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		redacted := g.redact(string(out))
		return redacted, fmt.Errorf("git %s: %w: %s", args[0], err, redacted)
	}
	return string(out), nil
}

// LsRemote 毫秒级可达性/权限校验（不落盘）。
func (g *Git) LsRemote(ctx context.Context, rawURL string) error {
	_, err := g.run(ctx, "", "ls-remote", "--heads", injectToken(rawURL, g.Token))
	return err
}

// Clone 浅克隆指定分支。成功后立刻把 origin 的 URL 重置为 rawURL：
// token 只出现在本次 clone 的命令行参数里，不写进 workdir 的 .git/config。
// （CommitAndPush 用显式 pushURL 推送，不依赖存储的 remote。）
func (g *Git) Clone(ctx context.Context, rawURL, branch, dir string) error {
	if _, err := g.run(ctx, "", "clone", "--depth", "1", "--branch", branch, injectToken(rawURL, g.Token), dir); err != nil {
		return err
	}
	_, err := g.run(ctx, dir, "remote", "set-url", "origin", rawURL)
	return err
}

// Head 返回当前 HEAD commit（快照基准）。
func (g *Git) Head(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// InitRepo 在 workdir 初始化 git 仓库（-b main，目录不存在则创建）并打一个
// 空的 initial commit：保证 HEAD 恒存在——后续 diff、无变更路径都有落点。
// 目录里已有的未跟踪文件不动，留给后续 Commit 收纳。
func (g *Git) InitRepo(ctx context.Context, dir string) error {
	if _, err := g.run(ctx, "", "init", "-b", "main", dir); err != nil {
		return err
	}
	_, err := g.run(ctx, dir, "commit", "--allow-empty", "-m", "init")
	return err
}

// Commit 提交 workdir 全部变更（add -A + commit）；无变更时返回 (false, nil)。
func (g *Git) Commit(ctx context.Context, dir, msg string) (bool, error) {
	if _, err := g.run(ctx, dir, "add", "-A"); err != nil {
		return false, err
	}
	st, err := g.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(st) == "" {
		return false, nil
	}
	if _, err := g.run(ctx, dir, "commit", "-m", msg); err != nil {
		return false, err
	}
	return true, nil
}

// Push 把 HEAD 推到 pushURL 的 ref；force 时覆盖远端（驳回重做后重推同一分支）。
func (g *Git) Push(ctx context.Context, dir, pushURL, ref string, force bool) error {
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, injectToken(pushURL, g.Token), "HEAD:"+ref)
	_, err := g.run(ctx, dir, args...)
	return err
}

// Fetch 从 repoURL 拉取单个 ref 到 FETCH_HEAD（不合并；token 注入同 Clone）。
// ref 形如 "infera/abcd1234"。
func (g *Git) Fetch(ctx context.Context, dir, rawURL, ref string) error {
	_, err := g.run(ctx, dir, "fetch", injectToken(rawURL, g.Token), ref)
	return err
}

// Merge 把 FETCH_HEAD 合并进当前分支（--no-edit）。冲突时返回 ErrMergeConflict
// （可 errors.Is 识别）；workdir 停留在冲突状态，由调用方决定 reset/人工救援。
func (g *Git) Merge(ctx context.Context, dir, msg string) error {
	out, err := g.run(ctx, dir, "merge", "--no-edit", "-m", msg, "FETCH_HEAD")
	if err != nil && strings.Contains(out, "CONFLICT") {
		return fmt.Errorf("%w: %s", ErrMergeConflict, g.redact(out))
	}
	return err
}

// ResetHard 把 workdir 硬重置到指定 ref（冲突恢复：对齐人工解决后的远端分支）。
func (g *Git) ResetHard(ctx context.Context, dir, ref string) error {
	_, err := g.run(ctx, dir, "reset", "--hard", ref)
	return err
}

// DiffRange 返回 base..HEAD 的完整 diff。
func (g *Git) DiffRange(ctx context.Context, dir, base string) (string, error) {
	out, err := g.run(ctx, dir, "diff", base+"..HEAD")
	return out, err
}

// emptyTreeHash 是空树对象的哈希：git 内建的魔法值，无需对象真实存在即可引用。
const emptyTreeHash = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// DiffRoot 返回 HEAD 相对空树的完整 diff（绿地项目 = 全部产出）。
// 注意不能用 `git diff --root HEAD`：--root 是 log/diff-tree 的选项，
// git-diff 会静默忽略它并输出空 diff。
func (g *Git) DiffRoot(ctx context.Context, dir string) (string, error) {
	out, err := g.run(ctx, dir, "diff", emptyTreeHash, "HEAD")
	return out, err
}

// CommitAndPush 提交 workdir 全部变更并推到远端分支；无变更时返回 (false, nil)。
func (g *Git) CommitAndPush(ctx context.Context, dir, msg, ref, pushURL string) (bool, error) {
	pushed, err := g.Commit(ctx, dir, msg)
	if err != nil || !pushed {
		return pushed, err
	}
	if err := g.Push(ctx, dir, pushURL, ref, false); err != nil {
		return false, err
	}
	return true, nil
}
