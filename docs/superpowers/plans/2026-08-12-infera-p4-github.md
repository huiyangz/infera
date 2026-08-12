# infera P4（GitHub 集成）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Agent 改的是**真代码仓库**——clone 仓库进容器、Coder Agent 改文件后推分支开 PR、`unit_test` 在容器里真跑 `go test`、PR 合并触发 deploy。把 P2/P3 的"容器空目录 + 假测试"换成真仓库闭环。

**Architecture:** 后端用 PAT 起步（`go-github` + `git` CLI）。`RepoCloner` 把仓库 clone 到本地临时目录；`DockerBackend` 把该目录挂载进容器 `/work`；`GitService` 在本地 clone 里 commit + push 新分支；`PRService` 用 `go-github` 开 PR；`RealTestRunner` 起容器跑 `go test` 解析 exit code。MVP 用 PAT，GitHub App 留 v2。

**Tech Stack:** Go · github.com/google/go-github/v62 · os/exec（git CLI）· Docker（沿用 P2）
**依赖：** P2（DockerBackend）、P3（testrunner 接口）
**Spec：** 产品设计文档 §7.5–7.8、§3.2

---

## 文件结构

```
server/internal/
├── github/
│   ├── client.go       # go-github client（PAT）
│   ├── repo.go         # RepoCloner：git clone 到本地
│   ├── git.go          # GitService：commit + push 新分支
│   └── pr.go           # PRService：创建 PR / 查合并状态
├── agent/
│   └── docker.go       # 改造：提取 runContainer + 挂载 workdir
└── testrunner/
    └── real.go         # RealTestRunner：容器内跑 go test
```

---

## Task 1: 配置 + go-github client + RepoCloner

**Files:**
- Modify: `server/internal/config/config.go`
- Create: `server/internal/github/client.go`
- Create: `server/internal/github/repo.go`
- Create: `server/internal/github/repo_test.go`

- [ ] **Step 1: 装依赖**

```bash
cd server
go get github.com/google/go-github/v62@latest
go get golang.org/x/oauth2@latest
go mod tidy
```

- [ ] **Step 2: 加配置项**

`server/internal/config/config.go` 的 `Config` 加字段、`Load` 加读取：
```go
type Config struct {
	DatabaseURL    string
	Port           string
	GitHubToken    string // PAT
	AgentImage     string // infera-agent
	RepoWorkRoot   string // 本地 clone 根目录，如 /tmp/infera-repos
}
// Load 里追加：
	GitHubToken:  getenv("GITHUB_TOKEN", ""),
	AgentImage:   getenv("AGENT_IMAGE", "infera-agent"),
	RepoWorkRoot: getenv("REPO_WORK_ROOT", "/tmp/infera-repos"),
```

- [ ] **Step 3: 先写 RepoCloner 测试**

`server/internal/github/repo_test.go`:
```go
package github

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestClonePublicRepo(t *testing.T) {
	// 用一个极小的公开仓库验证 clone 逻辑（无需 token）
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	c := RepoCloner{token: ""}
	dest := t.TempDir()
	// 注意：本测试需联网；CI 环境可跳过
	if err := c.Clone(context.Background(), "https://github.com/octocat/Hello-World.git", dest); err != nil {
		t.Skipf("network clone failed, skipping: %v", err)
	}
	out, _ := exec.Command("git", "-C", dest, "log", "--oneline", "-1").Output()
	if !strings.Contains(string(out), "Fix") && !strings.Contains(string(out), "merge") && len(out) == 0 {
		t.Fatalf("expected some commit log, got %q", out)
	}
}
```

- [ ] **Step 4: 实现 client + RepoCloner**

`server/internal/github/client.go`:
```go
package github

import (
	"context"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

func NewClient(token string) *github.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return github.NewClient(oauth2.NewClient(context.Background(), ts))
}
```

`server/internal/github/repo.go`:
```go
package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type RepoCloner struct{ token string }

func NewRepoCloner(token string) RepoCloner { return RepoCloner{token: token} }

// Clone 把 repoURL clone 到 dest。token 注入到 URL 用于私有仓库鉴权。
func (c RepoCloner) Clone(ctx context.Context, repoURL, dest string) error {
	authed := c.withToken(repoURL)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", authed, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w: %s", err, string(out))
	}
	return nil
}

func (c RepoCloner) withToken(url string) string {
	if c.token == "" || !strings.HasPrefix(url, "https://") {
		return url
	}
	return strings.Replace(url, "https://", "https://"+c.token+"@", 1)
}
```

- [ ] **Step 5: 跑测试 + 提交**

```bash
go test ./internal/github/ -v
git add server/internal/github/ server/internal/config/ server/go.mod server/go.sum
git commit -m "feat(github): client and repo cloner"
```

---

## Task 2: DockerBackend 改造 —— 挂载 workdir + 提取 runContainer

**Files:**
- Modify: `server/internal/agent/docker.go`

> 把"起容器跑命令"提取为 `runContainer`，让 `Execute`（跑 claude）和 P4 的 `RealTestRunner`（跑 go test）共用。容器挂载 `ExecInput.Workdir` 到 `/work`。

- [ ] **Step 1: 改 docker.go**

把 P2 的 `DockerBackend` 改为（整体替换 `Execute` 并新增 `runContainer`）：
```go
// runContainer 起一个容器跑 cmd，挂载 workdir 到 /work，返回 stdout 与退出码。
func (b *DockerBackend) runContainer(ctx context.Context, cmd []string, workdir string) (string, int, error) {
	var binds []string
	if workdir != "" {
		binds = []string{fmt.Sprintf("%s:/work", workdir)}
	}
	createResp, err := b.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      b.image,
			Cmd:        cmd,
			Env:        []string{"ANTHROPIC_API_KEY=" + b.apiKey},
			WorkingDir: "/work",
		},
		&container.HostConfig{Binds: binds, AutoRemove: false},
		nil, nil, "",
	)
	if err != nil {
		return "", -1, fmt.Errorf("container create: %w", err)
	}
	id := createResp.ID
	defer func() { _ = b.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}) }()

	if err := b.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return "", -1, fmt.Errorf("container start: %w", err)
	}
	statusCh, errCh := b.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", -1, err
		}
	case <-ctx.Done():
		return "", -1, ctx.Err()
	case s := <-statusCh:
		logs, _ := b.cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true})
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, logs)
		return buf.String(), int(s.StatusCode), nil
	}
	return "", -1, fmt.Errorf("unknown")
}

func (b *DockerBackend) Execute(ctx context.Context, in ExecInput) (ExecResult, error) {
	out, _, err := b.runContainer(ctx, []string{"claude", "-p", in.Prompt, "--output-format", "text"}, in.Workdir)
	if err != nil {
		return ExecResult{}, err
	}
	return ExecResult{Output: out, SessionID: ""}, nil
}

// RunCommand 暴露给 testrunner 等：在 workdir 起容器跑任意命令。
func (b *DockerBackend) RunCommand(ctx context.Context, cmd []string, workdir string) (stdout string, exitCode int, err error) {
	return b.runContainer(ctx, cmd, workdir)
}
```

> 顶部 import 块保留 P2 的 `bytes`、`context`、`fmt`、`io`、`os`、container、client。删掉旧的 Execute 实现与 `image` 占位 import。

- [ ] **Step 2: 编译**

```bash
go build ./...
```
Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add server/internal/agent/docker.go
git commit -m "refactor(agent): extract runContainer, mount workdir"
```

---

## Task 3: GitService（commit + push 新分支）

**Files:**
- Create: `server/internal/github/git.go`
- Create: `server/internal/github/git_test.go`

- [ ] **Step 1: 先写测试（用本地 git 仓库）**

`server/internal/github/git_test.go`:
```go
package github

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func initBareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	_ = exec.Command("git", "init", "--bare", remote).Run()
	return remote
}

func initLocalClone(t *testing.T, remote string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	_ = exec.Command("git", "clone", remote, work).Run()
	for _, args := range [][]string{
		{"-C", work, "config", "user.email", "t@t"},
		{"-C", work, "config", "user.name", "t"},
		{"-C", work, "commit", "--allow-empty", "-m", "init"},
		{"-C", work, "push", "origin", "HEAD"},
	} {
		_ = exec.Command("git", args...).Run()
	}
	return work
}

func TestCommitAndPushCreatesBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	remote := initBareRemote(t)
	work := initLocalClone(t, remote)
	// 写一个新文件触发改动
	writeFile(t, filepath.Join(work, "a.txt"), "hi")

	g := GitService{Workdir: work}
	err := g.CommitAndPush(context.Background(), "feat-1", "add a")
	assert.NoError(t, err)

	// 远端应有 feat-1 分支
	out, _ := exec.Command("git", "ls-remote", "--heads", remote, "feat-1").Output()
	assert.Contains(t, string(out), "feat-1")
}
```

> `writeFile` 是测试辅助：`os.WriteFile(path, []byte(s), 0644)`，自行定义在测试文件里。

- [ ] **Step 2: 实现 GitService**

`server/internal/github/git.go`:
```go
package github

import (
	"context"
	"fmt"
	"os/exec"
)

type GitService struct{ Workdir string }

// CommitAndPush 在 Workdir 建分支、提交全部改动、推到 origin。
func (g GitService) CommitAndPush(ctx context.Context, branch, message string) error {
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Workdir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w: %s", args, err, string(out))
		}
		return nil
	}
	if err := run("checkout", "-B", branch); err != nil {
		return err
	}
	if err := run("add", "-A"); err != nil {
		return err
	}
	if err := run("commit", "-m", message, "--allow-empty"); err != nil {
		return err
	}
	return run("push", "-u", "origin", branch, "--force")
}
```

- [ ] **Step 3: 跑测试 + 提交**

```bash
go test ./internal/github/ -run TestCommitAndPush -v
git add server/internal/github/git.go server/internal/github/git_test.go
git commit -m "feat(github): git service commit and push branch"
```

---

## Task 4: PRService（创建 PR + 查合并状态）

**Files:**
- Create: `server/internal/github/pr.go`

- [ ] **Step 1: 实现 PRService**

`server/internal/github/pr.go`:
```go
package github

import (
	"context"
	"strings"

	"github.com/google/go-github/v62/github"
)

type PRService struct {
	client *github.Client
}

func NewPRService(client *github.Client) *PRService { return &PRService{client: client} }

// ownerRepo 从 "owner/repo" 解析出 owner 与 repo。
func ownerRepo(s string) (string, string) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// Create 在 ownerRepo 上创建 head → base 的 PR。
func (s *PRService) Create(ctx context.Context, ownerRepoStr, head, base, title, body string) (*github.PullRequest, error) {
	owner, repo := ownerRepo(ownerRepoStr)
	pr, _, err := s.client.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title: github.String(title), Head: github.String(head), Base: github.String(base), Body: github.String(body),
	})
	if err != nil {
		return nil, err
	}
	return pr, nil
}

// IsMerged 轮询 PR 是否已合并。
func (s *PRService) IsMerged(ctx context.Context, ownerRepoStr string, prNumber int) (bool, error) {
	owner, repo := ownerRepo(ownerRepoStr)
	merged, _, err := s.client.PullRequests.IsMerged(ctx, owner, repo, prNumber)
	return merged, err
}
```

- [ ] **Step 2: 编译 + 提交**

```bash
go build ./...
git add server/internal/github/pr.go
git commit -m "feat(github): pr create and merge check"
```

> PR 创建走真实 GitHub API，单元测试需 mock 或跳过；P4 smoke test 里真实验证。

---

## Task 5: RealTestRunner（容器内跑 go test）

**Files:**
- Create: `server/internal/testrunner/real.go`

- [ ] **Step 1: 实现 RealTestRunner**

`server/internal/testrunner/real.go`:
```go
package testrunner

import (
	"context"
	"strings"
)

// CmdRunner 是 RealTestRunner 依赖的"在 workdir 起容器跑命令"能力（由 DockerBackend.RunCommand 提供）。
type CmdRunner interface {
	RunCommand(ctx context.Context, cmd []string, workdir string) (stdout string, exitCode int, err error)
}

type RealRunner struct {
	cmd     CmdRunner
	workdir string // 本地 clone 路径，挂载进容器 /work
}

func NewRealRunner(cmd CmdRunner, workdir string) *RealRunner {
	return &RealRunner{cmd: cmd, workdir: workdir}
}

func (r *RealRunner) Run(ctx context.Context, workdir string) (Result, error) {
	wd := workdir
	if wd == "" {
		wd = r.workdir
	}
	// 在容器里跑 go test；通过 exit code 判定
	out, code, err := r.cmd.RunCommand(ctx,
		[]string{"sh", "-c", "cd /work && go test ./... 2>&1"}, wd)
	if err != nil {
		return Result{Pass: false, Detail: "run error: " + err.Error()}, err
	}
	if code == 0 {
		return Result{Pass: true, Detail: out}, nil
	}
	// 截取失败摘要（最后 10 行）
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return Result{Pass: false, Detail: strings.Join(lines, "\n")}, nil
}
```

- [ ] **Step 2: 加一个用 FakeCmdRunner 的测试**

在 `server/internal/testrunner/real_test.go`:
```go
package testrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCmd struct{ out string; code int }
func (f fakeCmd) RunCommand(ctx context.Context, cmd []string, workdir string) (string, int, error) {
	return f.out, f.code, nil
}

func TestRealRunnerPassOnZeroExit(t *testing.T) {
	r := NewRealRunner(fakeCmd{out: "ok\nPASS", code: 0}, "/work")
	res, err := r.Run(context.Background(), "/work")
	assert.NoError(t, err)
	assert.True(t, res.Pass)
}

func TestRealRunnerFailOnNonZeroExit(t *testing.T) {
	r := NewRealRunner(fakeCmd{out: "FAIL: pkg_test.go:12", code: 1}, "/work")
	res, _ := r.Run(context.Background(), "/work")
	assert.False(t, res.Pass)
	assert.Contains(t, res.Detail, "FAIL")
}
```

- [ ] **Step 3: 跑测试 + 提交**

```bash
go test ./internal/testrunner/ -v
git add server/internal/testrunner/real.go server/internal/testrunner/real_test.go
git commit -m "feat(testrunner): real runner via container go test"
```

---

## Task 6: 接流水线 + main 装配 + smoke test

**Files:**
- Modify: `server/internal/service/execute.go`（code_gen 后 push + 开 PR）
- Modify: `server/internal/service/delivery.go`（advance 到 deploy 时等 PR 合并）
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: 给 ExecuteService 注入 repo 上下文 + code_gen 后 push/PR**

`server/internal/service/execute.go` 加字段与 code_gen 后处理：
```go
type ExecuteService struct {
	pool     *pgxpool.Pool
	q        *generated.Queries
	backend  agent.Backend
	cloner   *github.RepoCloner   // 可为 nil（无仓库模式）
	git      *github.GitService   // 可为 nil
	pr       *github.PRService    // 可为 nil
	repoRoot string               // 本地 clone 根目录
}

// 在 ExecuteStage 的 code_gen 分支后，执行 commit/push + 开 PR：
func (s *ExecuteService) maybePushAndOpenPR(ctx context.Context, d generated.Delivery, branch string) error {
	if s.cloner == nil || s.git == nil || s.pr == nil || d.RepoUrl == "" {
		return nil // 无仓库模式，跳过
	}
	workdir := filepath.Join(s.repoRoot, d.ID.String())
	// 首次 clone；已存在则跳过
	if _, err := os.Stat(workdir); os.IsNotExist(err) {
		if err := s.cloner.Clone(ctx, d.RepoUrl, workdir); err != nil {
			return err
		}
	}
	if err := s.git.WithWorkdir(workdir).CommitAndPush(ctx, branch, "infera: "+d.Title); err != nil {
		return err
	}
	ownerRepo := repoOwnerRepo(d.RepoUrl) // 从 URL 解析 "owner/repo"
	pr, err := s.pr.Create(ctx, ownerRepo, branch, "main", "["+d.Title+"] by Coder Agent", "自动生成")
	if err != nil {
		return err
	}
	// 把 PR 号记进 timeline
	s.timeline(ctx, d.ID, "code_gen", "pr_opened", map[string]any{"url": pr.GetHTMLURL(), "number": pr.GetNumber()})
	return nil
}
```

> 顶部 import 加 `"os"`、`"path/filepath"`、`"github.com/tokfinity/infera/internal/github"`。`repoOwnerRepo` 从 `https://github.com/owner/repo(.git)` 解析出 `owner/repo`，自行实现（strings.TrimPrefix + TrimSuffix `.git`）。
> `GitService.WithWorkdir` 返回一个设了 Workdir 的副本：
> ```go
> func (g GitService) WithWorkdir(dir string) GitService { g.Workdir = dir; return g }
> ```

- [ ] **Step 2: 在 code_gen 执行后调用**

`execute.go` 的 `ExecuteStage` 里，`code_gen` 执行成功后：
```go
	if stage == "code_gen" {
		branch := "infera/" + deliveryID.String()
		if err := s.maybePushAndOpenPR(ctx, /* delivery */, branch); err != nil {
			// 失败不阻断，记 timeline
		}
	}
```
（`ExecuteStage` 当前签名没有 delivery 全量，需先 `s.q.GetDelivery(ctx, deliveryID)` 取到 `d` 再用。）

- [ ] **Step 3: deploy stage 等 PR 合并**

`server/internal/service/delivery.go` 的 `Advance`，在 `next == "deploy"` 时：
```go
	if next == "deploy" && s.executor != nil {
		// 查最近 pr_opened 事件，轮询是否合并
		if merged, err := s.executor.IsLatestPRMerged(ctx, d.ID); err == nil && !merged {
			// 没合并：暂停在 deploy，写一条 waiting 事件，不前进
			s.timeline(ctx, d.ID, "deploy", "waiting_for_merge", map[string]any{})
			return d, nil
		}
	}
```
`IsLatestPRMerged` 在 ExecuteService 实现：取 timeline 最近 `pr_opened` 的 number → `prService.IsMerged`。

- [ ] **Step 4: main 装配真实组件**

`server/cmd/server/main.go` 在创建 executor 前：
```go
	ghClient := github.NewClient(cfg.GitHubToken)
	cloner := github.NewRepoCloner(cfg.GitHubToken)
	prSvc := github.NewPRService(ghClient)

	dbBackend, err := agent.NewDockerBackend(cfg.AgentImage)
	var testRunner testrunner.Runner
	if err == nil {
		testRunner = testrunner.NewRealRunner(dbBackend, "") // workdir 每次按 delivery 定
	}
	executor := service.NewExecute(pool, dbBackend).
		WithGitHub(cloner, prSvc, cfg.RepoWorkRoot) // 新增的链式方法，设置 cloner/git/pr/repoRoot
	deliverySvc := service.New(pool).
		WithExecutor(executor).
		WithTestRunner(testRunner)
```

- [ ] **Step 5: 编译 + smoke test（真实仓库）**

```bash
go build ./...
docker build -f server/Dockerfile.agent -t infera-agent .
export GITHUB_TOKEN=ghp_...
# 用一个有写权限的测试仓库创建 Delivery，repo_url 填该仓库
# 推进到 code_gen：应看到容器 clone、改代码、push 分支、开 PR
# 推进到 unit_test：容器内真跑 go test
# 在 GitHub 合并 PR 后推进 deploy：检测到合并 → completed
```
Expected：真实仓库出现分支与 PR；测试真实运行；PR 合并后 deploy 完成

- [ ] **Step 6: 提交**

```bash
git add server/
git commit -m "feat(server): wire github repo, pr, real tests into pipeline"
```

---

## P4 完成标准

- [ ] `RepoCloner` / `GitService` / `PRService`（go-github）就位，PAT 鉴权
- [ ] `DockerBackend` 挂载本地 clone 到容器 `/work`，提取 `runContainer` 供复用
- [ ] `RealTestRunner` 在容器内跑 `go test`，按 exit code 判定
- [ ] code_gen 后自动 push 分支 + 开 PR（timeline 记 `pr_opened`）
- [ ] unit_test 用 RealTestRunner（main 注入），不再用 Fake
- [ ] deploy 等 PR 合并（轮询 `IsMerged`），合并后 completed
- [ ] smoke test：真实仓库端到端跑通

## 给后续 Plan 的接口约定

- **P5**：gate 暂停 + 审批 UI——`pr_opened` 事件 + Reviewer Agent 意见展示给人；人 approve → 触发合并 → deploy 前进。
- **P6**：`pr_opened` / `waiting_for_merge` / `loop_back` 等事件走 WebSocket 实时推。
- **v2（GitHub App）**：把 PAT 换成 GitHub App（私钥签名 JWT + installation token + webhook 订阅 PR 合并，替代轮询）。
