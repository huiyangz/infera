# infera P2（Agent 执行层）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让流水线上的 4 个专职 Agent（Spec / Test / Coder / Reviewer）能在 Docker 容器里真正跑起来——stage 推进到对应节点时，系统选 Agent、起容器、跑 Claude Code、把产出写进 timeline。把 P1 的 stub advance 换成真执行。

**Architecture:** 抽象一个 `Backend` 接口（借鉴 multica 的 `server/pkg/agent/agent.go`），`FakeBackend` 用于测试、`DockerBackend` 是真实实现（Docker SDK 起容器跑 Claude Code CLI）。Agent 配置存 `agent_configs` 表（P1 已建），预置 4 个。`ExecuteService` 负责按 stage 选 Agent、调 Backend、写 timeline。P2 不做 loop（P3）和真仓库同步（P4）——Agent 在容器的工作目录里跑，产出文本写进 timeline，验证"能跑"。

**Tech Stack:** Go · github.com/docker/docker SDK · Claude Code CLI（`claude -p`，headless）· testify · Docker（本地 daemon）

**依赖：** P1 已完成（DB / Delivery 状态机 / advance / timeline）
**Spec：** `docs/superpowers/specs/2026-08-12-infera-product-design.md` §6.2（Agent 架构）、§7（stage 详解）

---

## 文件结构

```
server/
├── Dockerfile.agent                    # Claude Code 运行镜像
├── internal/
│   ├── agent/
│   │   ├── agent.go                    # AgentConfig / ExecResult / Backend 接口
│   │   ├── stage_agent.go              # stage → role 映射
│   │   ├── fake.go                     # FakeBackend（测试用）
│   │   └── docker.go                   # DockerBackend（真实实现）
│   └── service/
│       ├── execute.go                  # ExecuteService
│       └── execute_test.go
├── migrations/
│   ├── 000003_seed_agents.up.sql       # 4 个预置 Agent
│   └── 000003_seed_agents.down.sql
└── pkg/db/queries/
    └── agents.sql                      # P1 已建，P2 复用 GetAgentByRole
```

---

## Task 1: agent 包 — 配置类型 + Backend 接口 + FakeBackend

**Files:**
- Create: `server/internal/agent/agent.go`
- Create: `server/internal/agent/fake.go`
- Create: `server/internal/agent/fake_test.go`

- [ ] **Step 1: 先写测试（FakeBackend 行为）**

`server/internal/agent/fake_test.go`:
```go
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFakeBackendReturnsCannedOutput(t *testing.T) {
	b := NewFakeBackend()
	b.Stub("spec", "这是 spec 产出")

	res, err := b.Execute(context.Background(), ExecInput{
		Role:   "spec",
		Prompt: "写 spec",
	})
	assert.NoError(t, err)
	assert.Equal(t, "这是 spec 产出", res.Output)
	assert.NotEmpty(t, res.SessionID)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd server
go test ./internal/agent/ -v
```
Expected: FAIL / 编译错误（类型与 FakeBackend 未定义）

- [ ] **Step 3: 实现接口与类型**

`server/internal/agent/agent.go`:
```go
package agent

import "context"

// Role 是专职 Agent 的角色，对应流水线上的某类工作。
type Role string

const (
	RoleSpec    Role = "spec"
	RoleTest    Role = "test"
	RoleCoder   Role = "coder"
	RoleReviewer Role = "reviewer"
)

// AgentConfig 是一个专职 Agent 的配置（来自 agent_configs 表）。
type AgentConfig struct {
	ID       string
	Name     string
	Role     Role
	SystemPrompt string // 注入到 Claude Code 的指令
	Model    string
}

// ExecInput 是一次执行的输入。
type ExecInput struct {
	Role   Role
	Prompt string // 这次让 Agent 干什么（含上下文：需求/spec/代码等）
	Workdir string // 容器内工作目录（P2 可留空，P4 接真仓库后用）
}

// ExecResult 是一次执行的输出。
type ExecResult struct {
	SessionID string // 容器/执行 ID，便于追溯
	Output    string // Agent 产出文本（spec / 测试 / 代码 / 审核意见）
}

// Backend 抽象"在一个运行时里跑 Agent"。FakeBackend 测试，DockerBackend 生产。
type Backend interface {
	Execute(ctx context.Context, in ExecInput) (ExecResult, error)
}
```

- [ ] **Step 4: 实现 FakeBackend**

`server/internal/agent/fake.go`:
```go
package agent

import (
	"context"
	"fmt"
	"sync"
)

// FakeBackend 按 role 返回预设的产出，测试用。
type FakeBackend struct {
	mu    sync.Mutex
	stubs map[Role]string
	calls []ExecInput
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{stubs: map[Role]string{}}
}

// Stub 设定某 role 的返回产出。
func (b *FakeBackend) Stub(role Role, output string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stubs[role] = output
}

func (b *FakeBackend) Execute(ctx context.Context, in ExecInput) (ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, in)
	out, ok := b.stubs[in.Role]
	if !ok {
		out = fmt.Sprintf("fake output for role %s", in.Role)
	}
	return ExecResult{SessionID: "fake-session", Output: out}, nil
}

// Calls 返回收到的执行请求（断言用）。
func (b *FakeBackend) Calls() []ExecInput {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]ExecInput, len(b.calls))
	copy(cp, b.calls)
	return cp
}
```

- [ ] **Step 5: 跑测试确认通过**

```bash
go test ./internal/agent/ -v
```
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add server/internal/agent/
git commit -m "feat(agent): backend interface and fake backend"
```

---

## Task 2: 预置 4 个 Agent 的 seed migration

**Files:**
- Create: `server/migrations/000003_seed_agents.up.sql`
- Create: `server/migrations/000003_seed_agents.down.sql`

- [ ] **Step 1: 写 up seed**

`server/migrations/000003_seed_agents.up.sql`:
```sql
INSERT INTO agent_configs (name, role, config) VALUES
  (
    'Spec Agent',
    'spec',
    '{"system_prompt":"你是需求分析专家。把模糊需求收敛成清晰的 spec：功能描述、验收标准、边界与约束。输出结构化的 spec 文档。","model":"claude-sonnet-4-6"}'::jsonb
  ),
  (
    'Test Agent',
    'test',
    '{"system_prompt":"你是测试设计专家。根据 spec 生成测试用例，并写成可执行的单元测试代码（TDD）。测试用例就是可执行的 spec。","model":"claude-sonnet-4-6"}'::jsonb
  ),
  (
    'Coder Agent',
    'coder',
    '{"system_prompt":"你是资深工程师。根据 spec 和单元测试写实现代码，让所有测试通过。只改实现，不动测试意图。","model":"claude-sonnet-4-6"}'::jsonb
  ),
  (
    'Reviewer Agent',
    'reviewer',
    '{"system_prompt":"你是严格的代码审查者。审查 PR 的正确性、可读性、风险，产出具体的审核意见（approve / request change + 理由）。","model":"claude-opus-4-8"}'::jsonb
  )
ON CONFLICT (name) DO NOTHING;
```

- [ ] **Step 2: 写 down**

`server/migrations/000003_seed_agents.down.sql`:
```sql
DELETE FROM agent_configs WHERE name IN ('Spec Agent','Test Agent','Coder Agent','Reviewer Agent');
```

- [ ] **Step 3: 跑迁移并验证**

```bash
cd server
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
docker compose exec postgres psql -U infera -c "SELECT name, role FROM agent_configs ORDER BY role;"
```
Expected: 4 行，role 分别是 spec/test/coder/reviewer

- [ ] **Step 4: 提交**

```bash
git add server/migrations/
git commit -m "feat(db): seed 4 default agents"
```

---

## Task 3: stage → Agent 映射

**Files:**
- Create: `server/internal/agent/stage_agent.go`
- Create: `server/internal/agent/stage_agent_test.go`

- [ ] **Step 1: 先写测试**

`server/internal/agent/stage_agent_test.go`:
```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoleForStage(t *testing.T) {
	cases := []struct {
		stage string
		role  Role
		ok    bool
	}{
		{"spec", RoleSpec, true},
		{"test_gen", RoleTest, true},
		{"code_gen", RoleCoder, true},
		{"code_review", RoleReviewer, true},
		{"intake", "", false},         // 人，无 agent
		{"spec_approval", "", false},  // 人 gate
		{"unit_test", "", false},      // 系统
		{"deploy", "", false},         // 系统
	}
	for _, c := range cases {
		role, ok := RoleForStage(c.stage)
		assert.Equal(t, c.ok, ok, "stage %q", c.stage)
		assert.Equal(t, c.role, role, "stage %q", c.stage)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/agent/ -run TestRoleForStage -v
```
Expected: FAIL（`RoleForStage` 未定义）

- [ ] **Step 3: 实现映射**

`server/internal/agent/stage_agent.go`:
```go
package agent

// stageToRole 把需要 Agent 执行的 stage 映射到专职 role。
// 返回 ok=false 表示该 stage 由人或系统处理，不调 Agent。
var stageToRole = map[string]Role{
	"spec":        RoleSpec,
	"test_gen":    RoleTest,
	"code_gen":    RoleCoder,
	"code_review": RoleReviewer,
}

func RoleForStage(stage string) (Role, bool) {
	r, ok := stageToRole[stage]
	return r, ok
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/agent/ -v
```
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add server/internal/agent/stage_agent.go server/internal/agent/stage_agent_test.go
git commit -m "feat(agent): stage to role mapping"
```

---

## Task 4: ExecuteService（选 Agent → 调 Backend → 写 timeline）

**Files:**
- Create: `server/internal/service/execute.go`
- Create: `server/internal/service/execute_test.go`

- [ ] **Step 1: 先写测试（用 FakeBackend）**

`server/internal/service/execute_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
	"github.com/tokfinity/infera/pkg/db/generated"
)

func TestExecuteRunsAgentAndWritesTimeline(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")

	// 手动 seed 一个 spec agent（覆盖 migration 之外的隔离测试）
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Spec Agent','spec','{"system_prompt":"x"}')`)

	fake := agent.NewFakeBackend()
	fake.Stub(agent.RoleSpec, "## spec\n忘记密码流程…")
	svc := NewExecute(pool, fake)

	d, err := New(pool).Create(context.Background(), CreateInput{Title: "忘记密码"})
	assert.NoError(t, err)

	res, err := svc.ExecuteStage(context.Background(), d.ID, "spec", "需求：忘记密码")
	assert.NoError(t, err)
	assert.Equal(t, "## spec\n忘记密码流程…", res.Output)

	// timeline 应有一条 agent_output 事件
	q := generated.New(pool)
	events, err := q.ListTimelineEvents(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.True(t, len(events) >= 1)
	assert.Equal(t, "agent_output", events[len(events)-1].EventType)
}

func TestExecuteSkipsForHumanSystemStage(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")

	fake := agent.NewFakeBackend()
	svc := NewExecute(pool, fake)

	d, _ := New(pool).Create(context.Background(), CreateInput{Title: "t"})

	_, err := svc.ExecuteStage(context.Background(), d.ID, "intake", "")
	assert.Error(t, err) // intake 不该调 agent
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/service/ -run TestExecute -v
```
Expected: FAIL（`NewExecute`、`ExecuteStage` 未定义）

- [ ] **Step 3: 实现 ExecuteService**

`server/internal/service/execute.go`:
```go
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type ExecuteService struct {
	pool    *pgxpool.Pool
	q       *generated.Queries
	backend agent.Backend
}

func NewExecute(pool *pgxpool.Pool, backend agent.Backend) *ExecuteService {
	return &ExecuteService{pool: pool, q: generated.New(pool), backend: backend}
}

// ExecuteStage 对给定 stage 选对应专职 Agent 执行，并把产出写进 timeline。
// 若该 stage 不需要 Agent（人/系统），返回错误。
func (s *ExecuteService) ExecuteStage(ctx context.Context, deliveryID uuid.UUID, stage, prompt string) (agent.ExecResult, error) {
	role, ok := agent.RoleForStage(stage)
	if !ok {
		return agent.ExecResult{}, fmt.Errorf("stage %q has no agent", stage)
	}

	// 取 Agent 配置
	cfgRow, err := s.q.GetAgentByRole(ctx, string(role))
	if err != nil {
		return agent.ExecResult{}, fmt.Errorf("load agent %s: %w", role, err)
	}
	cfg := parseAgentConfig(cfgRow)

	res, err := s.backend.Execute(ctx, agent.ExecInput{
		Role:   role,
		Prompt: fmt.Sprintf("%s\n\n# 本次任务\n%s", cfg.SystemPrompt, prompt),
	})
	if err != nil {
		return res, fmt.Errorf("execute agent %s: %w", role, err)
	}

	// 写 timeline
	payload, _ := json.Marshal(map[string]any{
		"agent":      cfg.Name,
		"role":       role,
		"session_id": res.SessionID,
		"output":     res.Output,
	})
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID,
		Stage:      stage,
		EventType:  "agent_output",
		Payload:    payload,
	})
	return res, nil
}

func parseAgentConfig(row generated.AgentConfig) agent.AgentConfig {
	var cfg struct {
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
	}
	_ = json.Unmarshal(row.Config, &cfg)
	return agent.AgentConfig{
		ID:           row.ID.String(),
		Name:         row.Name,
		Role:         agent.Role(row.Role),
		SystemPrompt: cfg.SystemPrompt,
		Model:        cfg.Model,
	}
}

```


- [ ] **Step 4: 修正 import 后跑测试**

```bash
go test ./internal/service/ -run TestExecute -v
```
Expected: PASS（两个测试都过）

- [ ] **Step 5: 提交**

```bash
git add server/internal/service/
git commit -m "feat(server): execute service runs agent and writes timeline"
```

---

## Task 5: Claude Code 运行镜像（Dockerfile.agent）

**Files:**
- Create: `server/Dockerfile.agent`
- Create: `server/.dockerignore`

- [ ] **Step 1: 写 Dockerfile.agent**

`server/Dockerfile.agent`:
```dockerfile
FROM node:20-alpine

# 装 Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# 工作目录（P4 接真仓库后，仓库会挂载/克隆到这里）
WORKDIR /work

# claude 作为 entrypoint，由 DockerBackend 传 -p prompt 参数
ENTRYPOINT ["claude"]
```

- [ ] **Step 2: 写 .dockerignore**

`server/.dockerignore`:
```
*
!Dockerfile.agent
```

- [ ] **Step 3: 构建镜像并验证 claude 可用**

```bash
cd server
docker build -f Dockerfile.agent -t infera-agent .
docker run --rm infera-agent --version
```
Expected: 打印 Claude Code 版本号（确认 CLI 装好了）。若 `--version` 不支持，改用 `docker run --rm --entrypoint sh infera-agent -c "claude --help | head -3"`，看到帮助即成功。

- [ ] **Step 4: 提交**

```bash
git add server/Dockerfile.agent server/.dockerignore
git commit -m "feat(agent): claude code runtime image"
```

---

## Task 6: DockerBackend（Docker SDK 跑 Claude Code）

**Files:**
- Create: `server/internal/agent/docker.go`

- [ ] **Step 1: 装依赖**

```bash
cd server
go get github.com/docker/docker@latest
go mod tidy
```

- [ ] **Step 2: 实现 DockerBackend**

`server/internal/agent/docker.go`:
```go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// DockerBackend 用本地 Docker daemon 起容器跑 Claude Code。
type DockerBackend struct {
	cli     *client.Client
	image   string // 如 "infera-agent"
	apiKey  string // ANTHROPIC_API_KEY
}

func NewDockerBackend(image string) (*DockerBackend, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	return &DockerBackend{cli: cli, image: image, apiKey: apiKey}, nil
}

func (b *DockerBackend) Execute(ctx context.Context, in ExecInput) (ExecResult, error) {
	// 用 prompt 作为 claude -p 的参数；system 指令已在 prompt 前缀里（由 ExecuteService 拼好）
	cmd := []string{"-p", in.Prompt, "--output-format", "text"}

	createResp, err := b.cli.ContainerCreate(ctx,
		&container.Config{
			Image:    b.image,
			Cmd:      cmd,
			Env:      []string{"ANTHROPIC_API_KEY=" + b.apiKey},
			WorkingDir: "/work",
		},
		nil, nil, nil, "",
	)
	if err != nil {
		return ExecResult{}, fmt.Errorf("container create: %w", err)
	}
	containerID := createResp.ID
	// 保证退出后清理
	defer func() { _ = b.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}) }()

	if err := b.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return ExecResult{}, fmt.Errorf("container start: %w", err)
	}

	// 等结束
	statusCh, errCh := b.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return ExecResult{}, fmt.Errorf("container wait: %w", err)
		}
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	case <-statusCh:
	}

	// 读 stdout
	logs, err := b.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true, ShowStderr: false,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("container logs: %w", err)
	}
	defer logs.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, logs); err != nil {
		return ExecResult{}, fmt.Errorf("read logs: %w", err)
	}

	return ExecResult{SessionID: containerID, Output: buf.String()}, nil
}

// 避免 image import 未用告警（P4 会用到 image.Pull 等）。
var _ = image.PullOptions{}
```

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```
Expected: 无错误

- [ ] **Step 4: 提交**

```bash
git add server/internal/agent/docker.go server/go.mod server/go.sum
git commit -m "feat(agent): docker backend runs claude code"
```

---

## Task 7: 把 Agent 执行接到 advance（spec / test_gen / code_gen / code_review）

**Files:**
- Modify: `server/internal/service/delivery.go`
- Modify: `server/internal/handler/delivery.go`（注入 Backend）
- Modify: `server/internal/handler/router.go`
- Modify: `server/cmd/server/main.go`

> 改动：`DeliveryService` 持有一个可选的 `ExecuteService`。advance 推进到某 stage 后，若该 stage 有 Agent，就调 `ExecuteService.ExecuteStage`（用 FakeBackend 测试、DockerBackend 生产）。

- [ ] **Step 1: 给 DeliveryService 加 executor + 改 Advance**

在 `server/internal/service/delivery.go` 顶部 import 块加 `"github.com/tokfinity/infera/internal/agent"`，并修改：

```go
type DeliveryService struct {
	q        *generated.Queries
	executor *ExecuteService // 可为 nil（P1 模式 / 测试）
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
}

// WithExecutor 注入 Agent 执行层。main 里调。
func (s *DeliveryService) WithExecutor(ex *ExecuteService) *DeliveryService {
	s.executor = ex
	return s
}
```

修改 `Advance` 方法：在"推进到 next stage 之后、写 stage_started 之前"，插入 Agent 执行。把原来的 `Advance` 里成功推进分支改为：

```go
	updated, err := s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{
		ID: d.ID, CurrentStage: next,
	})
	if err != nil {
		return generated.Delivery{}, err
	}
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: d.ID, Stage: next, EventType: "stage_started", Payload: []byte(`{}`),
	})

	// 若该 stage 由 Agent 负责，立即执行（P2：顺序执行，无 loop；P3 加 loop）
	if s.executor != nil {
		if _, ok := agent.RoleForStage(next); ok {
			prompt := buildPromptForStage(next, d)
			if _, err := s.executor.ExecuteStage(ctx, d.ID, next, prompt); err != nil {
				// 执行失败：记一条失败事件，但不阻断状态推进（P3 会改成回环/卡住）
				payload, _ := json.Marshal(map[string]any{"error": err.Error()})
				_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
					DeliveryID: d.ID, Stage: next, EventType: "agent_failed", Payload: payload,
				})
			}
		}
	}
	return updated, nil
}

// buildPromptForStage 为不同 stage 拼给 Agent 的任务描述。
func buildPromptForStage(stage string, d generated.Delivery) string {
	switch stage {
	case "spec":
		return fmt.Sprintf("需求标题：%s\n需求描述：%s\n请产出 spec。", d.Title, d.Description)
	case "test_gen":
		return fmt.Sprintf("根据 spec（见 timeline）为「%s」生成测试用例与单元测试代码。", d.Title)
	case "code_gen":
		return fmt.Sprintf("为「%s」写实现代码，让单元测试通过。", d.Title)
	case "code_review":
		return fmt.Sprintf("审查「%s」的实现代码，产出审核意见。", d.Title)
	default:
		return d.Title
	}
}
```

> 顶部 import 块需含 `"encoding/json"`（若未有）。

- [ ] **Step 2: 写 Advance 触发 Agent 的测试**

新增 `server/internal/service/delivery_agent_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestAdvanceRunsSpecAgent(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Spec Agent','spec','{"system_prompt":"x"}')`)

	fake := agent.NewFakeBackend()
	fake.Stub(agent.RoleSpec, "spec 产出")
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), CreateInput{Title: "忘记密码"})
	_, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(fake.Calls()))         // spec 被调一次
	assert.Equal(t, agent.RoleSpec, fake.Calls()[0].Role)
}
```

- [ ] **Step 3: 跑测试**

```bash
go test ./internal/service/ -v
```
Expected: PASS（含之前所有）

- [ ] **Step 4: 在 main 装配 DockerBackend（生产）**

`server/cmd/server/main.go` 在 `NewRouter` 前插入：
```go
	// 装配 Agent 执行层：DockerBackend（生产）。无 ANTHROPIC_API_KEY 时降级为 nil（P1 模式）。
	var executor *service.ExecuteService
	if dbBackend, err := agent.NewDockerBackend("infera-agent"); err == nil {
		executor = service.NewExecute(pool, dbBackend)
	} else {
		fmt.Println("warning: agent backend disabled:", err)
	}
	deliverySvc := service.New(pool).WithExecutor(executor)
```

并把 `NewRouter` 改成接收 `deliverySvc`（router 透传给 handler）。同步改 `handler.NewRouter(pool, deliverySvc)` 与 `handler.NewDeliveryHandler` 持有 service（而非内部 new）。

> 最小改动：让 `DeliveryHandler` 直接接收外部传入的 `*service.DeliveryService`：
> ```go
> func NewDeliveryHandler(svc *service.DeliveryService, pool *pgxpool.Pool) *DeliveryHandler {
>     return &DeliveryHandler{svc: svc, pool: pool}
> }
> ```
> `router.go`：`NewRouter(pool *pgxpool.Pool, svc *service.DeliveryService) *chi.Mux`，内部 `dh := NewDeliveryHandler(svc, pool)`。

- [ ] **Step 5: 编译 + 手动跑（Fake 模式，无需 API key）**

临时把 main 里的 DockerBackend 换成 FakeBackend 跑一次验证链路：
```bash
go build ./...
```
Expected: 无错误

- [ ] **Step 6: 提交**

```bash
git add server/
git commit -m "feat(server): advance triggers agent execution on agent stages"
```

---

## Task 8: 端到端 smoke test（真 Docker + Claude Code）

> 这是手动验证任务，需要 `ANTHROPIC_API_KEY`，不计入自动化测试。

- [ ] **Step 1: 构建镜像**

```bash
cd server
docker build -f Dockerfile.agent -t infera-agent .
```

- [ ] **Step 2: 起 DB + 跑迁移 + 起 server**

```bash
docker compose up -d
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
export ANTHROPIC_API_KEY=sk-ant-...   # 你的 key
go run ./cmd/server &
sleep 2
```

- [ ] **Step 3: 创建一条 Delivery 并推进，观察 timeline**

```bash
ID=$(curl -s -X POST http://localhost:8080/api/deliveries -H 'Content-Type: application/json' -d '{"title":"加一个 hello world 接口"}' | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
curl -s -X POST http://localhost:8080/api/deliveries/$ID/advance > /dev/null  # intake → spec（Spec Agent 跑）
sleep 20  # 等 Claude Code 返回
curl -s http://localhost:8080/api/deliveries/$ID | python3 -m json.tool | grep -A3 agent_output
kill %1
```
Expected：timeline 里有一条 `agent_output` 事件，`payload.output` 是 Spec Agent 真生成的 spec 文本（来自 Claude Code）。

- [ ] **Step 4: 记录结果并提交（如有适配调整）**

若 smoke test 暴露问题（如 claude 参数名、超时），修正 `docker.go` 后提交：
```bash
git add server/
git commit -m "fix(agent): adjust claude invocation per smoke test"
```

---

## P2 完成标准（Definition of Done）

- [ ] 4 个专职 Agent 在 `agent_configs` 里（seed migration）
- [ ] `Backend` 接口 + `FakeBackend`（测试）+ `DockerBackend`（生产）都实现
- [ ] stage → role 映射正确（spec/test_gen/code_gen/code_review 调 Agent，其余不调）
- [ ] advance 推进到 Agent stage 时真调 Backend，产出写进 timeline（`agent_output` 事件）
- [ ] 所有 `go test ./...` 通过（用 FakeBackend，不依赖真 API key）
- [ ] smoke test：真 Docker + Claude Code，spec stage 产出真实 spec 文本进 timeline

## 给后续 Plan 的接口约定

- **P3（loop）**：unit_test ✗ / code_review ✗ 时，回退到 code_gen 重调 Coder Agent（连续 3 次升级）。P2 的 advance 是"前进即执行"，P3 加"验证结果决定前进还是回退"。
- **P4（GitHub）**：把真仓库克隆进容器 `/work`（DockerBackend 的 `Workdir`），Agent 改的代码 = 真 PR。P2 的容器工作目录是空的，产出只是文本。
- **P5（Gate UI）**：spec_approval / code_review 的人审批 UI；Reviewer Agent 的产出（P2 已进 timeline）展示给人做最终批准。
- **P6（实时）**：timeline 事件（含 agent_output）通过 WebSocket 实时推给前端。
