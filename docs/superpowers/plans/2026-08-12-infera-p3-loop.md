# infera P3（loop engineering）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给流水线加上回环——`unit_test` 跑不过、`code_review` 被打回时，自动回退到 `code_gen` 让 Coder Agent 重做；同一条 Delivery 连续失败 3 次则升级请人（`status=blocked`）。把 P2 的"前进即执行"升级为"执行 → 判定 → 前进 / 回退 / 升级"。

**Architecture:** 引入 `testrunner.Runner` 接口判定 `unit_test` 通过与否（P3 用 `FakeRunner`，P4 换 `RealRunner` 真跑仓库测试）；`code_review` 的 pass/fail 从 Reviewer Agent 的**结构化 decision**（JSON）解析。`Advance` 改造：执行后若判定失败，回退 `current_stage` 到 `code_gen`、`fail_count++` 并重调 Coder Agent；`fail_count >= 3` 则 `status=blocked`。

**Tech Stack:** Go · testify（沿用 P1/P2）
**依赖：** P2（Agent 执行已工作）
**Spec：** 产品设计文档 §3.2（loop）、§6.3、决策 #13/#17

---

## 文件结构

```
server/
├── internal/
│   ├── testrunner/
│   │   ├── testrunner.go        # Runner 接口 + Result
│   │   ├── fake.go              # FakeRunner（测试用）
│   │   └── fake_test.go
│   ├── agent/
│   │   └── review.go            # ParseReview（解析 Reviewer decision）
│   └── service/
│       ├── delivery.go          # 改造 Advance（loop 调度）
│       └── delivery_loop_test.go
├── migrations/
│   ├── 000004_delivery_fail_count.up.sql
│   ├── 000004_delivery_fail_count.down.sql
│   ├── 000005_reviewer_structured.up.sql   # 更新 reviewer prompt 要 JSON 输出
│   └── 000005_reviewer_structured.down.sql
└── pkg/db/queries/deliveries.sql            # 加 UpdateDeliveryFailCount
```

---

## Task 1: DB 迁移 — deliveries 加 fail_count

**Files:**
- Create: `server/migrations/000004_delivery_fail_count.up.sql`
- Create: `server/migrations/000004_delivery_fail_count.down.sql`
- Modify: `server/pkg/db/queries/deliveries.sql`

- [ ] **Step 1: 写 up 迁移**

`server/migrations/000004_delivery_fail_count.up.sql`:
```sql
ALTER TABLE deliveries ADD COLUMN fail_count integer NOT NULL DEFAULT 0;
```

- [ ] **Step 2: 写 down 迁移**

`server/migrations/000004_delivery_fail_count.down.sql`:
```sql
ALTER TABLE deliveries DROP COLUMN IF EXISTS fail_count;
```

- [ ] **Step 3: 加 sqlc 查询**

在 `server/pkg/db/queries/deliveries.sql` 末尾追加：
```sql
-- name: IncrementDeliveryFailCount :one
UPDATE deliveries
SET fail_count = fail_count + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ResetDeliveryFailCount :one
UPDATE deliveries
SET fail_count = 0, updated_at = now()
WHERE id = $1
RETURNING *;
```

- [ ] **Step 4: 跑迁移 + 重新生成 + 编译**

```bash
cd server
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
sqlc generate
go build ./...
```
Expected: 迁移成功、生成新查询方法、编译通过

- [ ] **Step 5: 提交**

```bash
git add server/migrations/ server/pkg/db/
git commit -m "feat(db): deliveries fail_count for loop escalation"
```

---

## Task 2: testrunner 包（接口 + FakeRunner）

**Files:**
- Create: `server/internal/testrunner/testrunner.go`
- Create: `server/internal/testrunner/fake.go`
- Create: `server/internal/testrunner/fake_test.go`

- [ ] **Step 1: 先写测试**

`server/internal/testrunner/fake_test.go`:
```go
package testrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFakeRunnerPassAndFail(t *testing.T) {
	pass := NewFakeRunner(true)
	r, err := pass.Run(context.Background(), "/work")
	assert.NoError(t, err)
	assert.True(t, r.Pass)

	fail := NewFakeRunner(false)
	r, err = fail.Run(context.Background(), "/work")
	assert.NoError(t, err)
	assert.False(t, r.Pass)
	assert.NotEmpty(t, r.Detail)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/testrunner/ -v
```
Expected: FAIL / 编译错误

- [ ] **Step 3: 实现接口**

`server/internal/testrunner/testrunner.go`:
```go
package testrunner

import "context"

// Result 是一次测试运行的判定。
type Result struct {
	Pass   bool
	Detail string // 失败时的摘要（给 Coder Agent 当修复线索）
}

// Runner 抽象"跑测试并判定通过与否"。
// P3 用 FakeRunner；P4 用 RealRunner（在容器里跑 go test / jest）。
type Runner interface {
	Run(ctx context.Context, workdir string) (Result, error)
}
```

- [ ] **Step 4: 实现 FakeRunner**

`server/internal/testrunner/fake.go`:
```go
package testrunner

import "context"

type FakeRunner struct{ pass bool }

func NewFakeRunner(pass bool) *FakeRunner { return &FakeRunner{pass: pass} }

func (f *FakeRunner) Run(ctx context.Context, workdir string) (Result, error) {
	if f.pass {
		return Result{Pass: true, Detail: "fake: all passed"}, nil
	}
	return Result{Pass: false, Detail: "fake: 2 cases failed"}, nil
}
```

- [ ] **Step 5: 跑测试确认通过 + 提交**

```bash
go test ./internal/testrunner/ -v
git add server/internal/testrunner/
git commit -m "feat(testrunner): runner interface and fake"
```

---

## Task 3: Reviewer 结构化 decision + 解析

**Files:**
- Create: `server/migrations/000005_reviewer_structured.up.sql`
- Create: `server/migrations/000005_reviewer_structured.down.sql`
- Create: `server/internal/agent/review.go`
- Create: `server/internal/agent/review_test.go`

- [ ] **Step 1: 更新 reviewer prompt 要 JSON 输出**

`server/migrations/000005_reviewer_structured.up.sql`:
```sql
UPDATE agent_configs
SET config = jsonb_set(config, '{system_prompt}',
  '"你是严格的代码审查者。审查代码后，必须只输出一个 JSON 对象，不要任何额外文字：{\"decision\":\"approve\"或\"reject\",\"reasons\":[\"...\"]}。approve 表示代码可合并；reject 表示必须改。"',
  true)
WHERE role = 'reviewer';
```

- [ ] **Step 2: 写 down**

`server/migrations/000005_reviewer_structured.down.sql`:
```sql
-- 不还原具体文案；留空即可（reviewer 仍可用，只是 prompt 回不到精确旧值）
```

- [ ] **Step 3: 先写解析测试**

`server/internal/agent/review_test.go`:
```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseReviewApprove(t *testing.T) {
	d, err := ParseReview(`{"decision":"approve","reasons":["ok"]}`)
	assert.NoError(t, err)
	assert.Equal(t, "approve", d.Decision)
}

func TestParseReviewReject(t *testing.T) {
	d, err := ParseReview(`{"decision":"reject","reasons":["缺少错误处理"]}`)
	assert.NoError(t, err)
	assert.Equal(t, "reject", d.Decision)
	assert.Len(t, d.Reasons, 1)
}

func TestParseReviewFromWrappedText(t *testing.T) {
	// Agent 偶尔在 JSON 外裹一段话，要能抽出 JSON
	d, err := ParseReview("我的审核：\n{\"decision\":\"reject\",\"reasons\":[\"x\"]}\n完")
	assert.NoError(t, err)
	assert.Equal(t, "reject", d.Decision)
}
```

- [ ] **Step 4: 跑测试确认失败**

```bash
go test ./internal/agent/ -run TestParseReview -v
```
Expected: FAIL（`ParseReview` 未定义）

- [ ] **Step 5: 实现 ParseReview**

`server/internal/agent/review.go`:
```go
package agent

import (
	"encoding/json"
	"regexp"
	"strings"
)

type ReviewDecision struct {
	Decision string   `json:"decision"` // "approve" | "reject"
	Reasons  []string `json:"reasons"`
}

var jsonObjectRe = regexp.MustCompile(`(?s)\{.*\}`)

// ParseReview 从 Reviewer Agent 的输出解析 decision。
// 支持纯 JSON，也支持 JSON 被文字包裹的情况。
func ParseReview(output string) (ReviewDecision, error) {
	var d ReviewDecision
	out := strings.TrimSpace(output)
	if err := json.Unmarshal([]byte(out), &d); err == nil {
		return d, nil
	}
	// 抽取第一个 JSON 对象
	if m := jsonObjectRe.FindString(out); m != "" {
		if err := json.Unmarshal([]byte(m), &d); err == nil {
			return d, nil
		}
	}
	return d, fmt.Errorf("cannot parse review decision from: %s", output)
}
```

> 顶部 import 块需补 `"fmt"`。

- [ ] **Step 6: 跑测试 + 迁移 + 提交**

```bash
go test ./internal/agent/ -run TestParseReview -v
cd .. && migrate -path server/migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
git add server/internal/agent/review.go server/internal/agent/review_test.go server/migrations/
git commit -m "feat(agent): structured review decision and parser"
```

---

## Task 4: 改造 Advance —— loop 调度（判定 → 前进/回退/升级）

**Files:**
- Modify: `server/internal/service/delivery.go`

> `DeliveryService` 增加 `testRunner testrunner.Runner` 字段。`Advance` 在推进到 `unit_test` / `code_review` 后做判定；失败则调 `retryCodeAt` 回退到 `code_gen`。

- [ ] **Step 1: 给 DeliveryService 加 testRunner**

修改 `server/internal/service/delivery.go`：

顶部 import 块加：
```go
"github.com/tokfinity/infera/internal/agent"
"github.com/tokfinity/infera/internal/stage"
"github.com/tokfinity/infera/internal/testrunner"
```

把 `DeliveryService` 结构改为：
```go
type DeliveryService struct {
	q          *generated.Queries
	executor   *ExecuteService
	testRunner testrunner.Runner // 可为 nil
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
}

func (s *DeliveryService) WithExecutor(ex *ExecuteService) *DeliveryService {
	s.executor = ex
	return s
}

func (s *DeliveryService) WithTestRunner(r testrunner.Runner) *DeliveryService {
	s.testRunner = r
	return s
}
```

- [ ] **Step 2: 重写 Advance 为 loop 调度**

替换 P2 里的 `Advance` 方法（整体替换）：
```go
func (s *DeliveryService) Advance(ctx context.Context, id uuid.UUID) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("get delivery: %w", err)
	}
	if d.Status != generated.DeliveryStatusActive {
		return d, fmt.Errorf("delivery not active (status=%s)", d.Status)
	}

	next, ok := stage.Next(d.CurrentStage)
	if !ok {
		return s.completeDelivery(ctx, d)
	}

	// 推进到 next
	d, err = s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{ID: d.ID, CurrentStage: next})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, d.ID, next, "stage_started", map[string]any{})

	// 执行 + 判定
	switch next {
	case "spec", "test_gen", "code_gen", "code_review":
		if s.executor != nil {
			if _, err := s.executor.ExecuteStage(ctx, d.ID, next, buildPromptForStage(next, d)); err != nil {
				s.timeline(ctx, d.ID, next, "agent_failed", map[string]any{"error": err.Error()})
			}
		}
	}

	// unit_test：系统跑测试判定
	if next == "unit_test" && s.testRunner != nil {
		res, err := s.testRunner.Run(ctx, "/work")
		if err == nil && !res.Pass {
			return s.retryCodeAt(ctx, d, "unit_test", res.Detail)
		}
	}

	// code_review：解析 Reviewer Agent 的 decision
	if next == "code_review" && s.executor != nil {
		if decision, err := s.latestReviewDecision(ctx, d.ID); err == nil && decision.Decision == "reject" {
			return s.retryCodeAt(ctx, d, "code_review", strings.Join(decision.Reasons, "; "))
		}
	}

	return d, nil
}

// retryCodeAt 回退到 code_gen 重做；连续 3 次失败则升级 blocked。
func (s *DeliveryService) retryCodeAt(ctx context.Context, d generated.Delivery, failedStage, reason string) (generated.Delivery, error) {
	d, err := s.q.IncrementDeliveryFailCount(ctx, d.ID)
	if err != nil {
		return generated.Delivery{}, err
	}
	if d.FailCount >= 3 {
		d, err = s.q.UpdateDeliveryStatus(ctx, generated.UpdateDeliveryStatusParams{ID: d.ID, Status: generated.DeliveryStatusBlocked})
		if err != nil {
			return generated.Delivery{}, err
		}
		s.timeline(ctx, d.ID, failedStage, "escalated", map[string]any{
			"reason": reason, "fail_count": d.FailCount,
		})
		return d, nil
	}
	// 回退到 code_gen 重做
	d, err = s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{ID: d.ID, CurrentStage: "code_gen"})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, d.ID, "code_gen", "loop_back", map[string]any{
		"from": failedStage, "reason": reason, "fail_count": d.FailCount,
	})
	if s.executor != nil {
		_, _ = s.executor.ExecuteStage(ctx, d.ID, "code_gen", "上一轮 "+failedStage+" 未过："+reason+"\n请修复。")
	}
	return d, nil
}

func (s *DeliveryService) completeDelivery(ctx context.Context, d generated.Delivery) (generated.Delivery, error) {
	d, err := s.q.UpdateDeliveryStatus(ctx, generated.UpdateDeliveryStatusParams{ID: d.ID, Status: generated.DeliveryStatusCompleted})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, d.ID, d.CurrentStage, "delivery_completed", map[string]any{})
	return d, nil
}

// timeline 是写 timeline event 的简写。
func (s *DeliveryService) timeline(ctx context.Context, deliveryID uuid.UUID, stageName, eventType string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID, Stage: stageName, EventType: eventType, Payload: b,
	})
}

// latestReviewDecision 从 timeline 取最近一条 code_review 的 agent_output 并解析。
func (s *DeliveryService) latestReviewDecision(ctx context.Context, deliveryID uuid.UUID) (agent.ReviewDecision, error) {
	events, err := s.q.ListTimelineEvents(ctx, deliveryID)
	if err != nil {
		return agent.ReviewDecision{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Stage == "code_review" && e.EventType == "agent_output" {
			var p struct{ Output string `json:"output"` }
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				return agent.ParseReview(p.Output)
			}
		}
	}
	return agent.ReviewDecision{}, fmt.Errorf("no review output found")
}
```

> 顶部 import 块需含 `"strings"`、`"github.com/google/uuid"`。`buildPromptForStage` 保留 P2 的实现不动。删掉 P2 里旧的 `Advance` 与重复的 timeline 内联写入。

- [ ] **Step 3: 更新所有用到 `stage` 包的 import**（service 现在直接用 `stage.Next`，确保 import）。

- [ ] **Step 4: 编译**

```bash
go build ./...
```
Expected: 无错误

- [ ] **Step 5: 提交**

```bash
git add server/internal/service/
git commit -m "feat(server): loop scheduling with retry and escalation"
```

---

## Task 5: 集成测试（完整 loop 场景）

**Files:**
- Create: `server/internal/service/delivery_loop_test.go`

- [ ] **Step 1: 写 loop 集成测试**

`server/internal/service/delivery_loop_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
	"github.com/tokfinity/infera/internal/testrunner"
)

func seedAgentsForLoop(t *testing.T, pool /* pgxpool.Pool */) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `
		INSERT INTO agent_configs (name, role, config) VALUES
		  ('Spec Agent','spec','{"system_prompt":"x"}'),
		  ('Coder Agent','coder','{"system_prompt":"x"}'),
		  ('Reviewer Agent','reviewer','{"system_prompt":"x"}')
		ON CONFLICT (name) DO NOTHING`)
}

func TestLoopRetriesThenBlocksAtThree(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	seedAgentsForLoop(t, pool)

	// unit_test 永远失败 → 每次回退 code_gen，3 次后 blocked
	runner := testrunner.NewFakeRunner(false)
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake)).WithTestRunner(runner)

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})

	// 推到 unit_test 需要先过 spec/spec_approval/test_gen/code_gen。
	// 为聚焦 loop，直接把 stage 设到 code_gen，下一步即 unit_test。
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='code_gen' WHERE id=$1", d.ID)

	// 第 1 次：code_gen → unit_test(失败) → 回 code_gen, fail_count=1
	d, _ = svc.Advance(context.Background(), d.ID)
	assert.Equal(t, "code_gen", d.CurrentStage)
	assert.Equal(t, int32(1), d.FailCount)
	assert.Equal(t, "active", string(d.Status))

	d, _ = svc.Advance(context.Background(), d.ID) // fail_count=2
	assert.Equal(t, int32(2), d.FailCount)

	d, _ = svc.Advance(context.Background(), d.ID) // 第 3 次 → blocked
	assert.Equal(t, "blocked", string(d.Status))
}

func TestLoopPassesWhenTestsGreen(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	seedAgentsForLoop(t, pool)

	runner := testrunner.NewFakeRunner(true) // 测试通过
	fake := agent.NewFakeBackend()
	// reviewer 也"approve"
	fake.Stub(agent.RoleReviewer, `{"decision":"approve","reasons":[]}`)
	svc := New(pool).WithExecutor(NewExecute(pool, fake)).WithTestRunner(runner)

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='code_gen' WHERE id=$1", d.ID)

	// code_gen → unit_test(pass) → code_review(approve) → deploy → completed
	for d.Status == "active" {
		var err error
		d, err = svc.Advance(context.Background(), d.ID)
		assert.NoError(t, err)
	}
	assert.Equal(t, "completed", string(d.Status))
	assert.Equal(t, int32(0), d.FailCount)
}
```

> `seedAgentsForLoop` 的参数类型写 `pool *pgxpool.Pool`（上面注释处补全签名）。

- [ ] **Step 2: 跑测试**

```bash
go test ./internal/service/ -run TestLoop -v
```
Expected: PASS（两个 loop 测试都过）

- [ ] **Step 3: 提交**

```bash
git add server/internal/service/delivery_loop_test.go
git commit -m "test(server): loop retry-and-escalate integration"
```

---

## P3 完成标准

- [ ] `deliveries.fail_count` 字段就位；sqlc 生成 `IncrementDeliveryFailCount` / `ResetDeliveryFailCount`
- [ ] `testrunner.Runner` 接口 + `FakeRunner`
- [ ] Reviewer Agent 输出结构化 JSON decision，`ParseReview` 能解析（含文字包裹）
- [ ] `Advance` 实现 loop：`unit_test` 不过 / `code_review` reject → 回退 `code_gen` + `fail_count++` + 重调 Coder Agent
- [ ] 连续 3 次失败 → `status=blocked` + `escalated` 事件
- [ ] 所有 `go test ./...` 通过

## 给后续 Plan 的接口约定

- **P4**：`RealTestRunner`（在容器 `/work` 真跑 `go test` / `jest`）替换 `FakeRunner`；仓库克隆进容器后，`unit_test` 才有真实 pass/fail 依据。`RetryCodeAt` 回退 `code_gen` 后 Coder Agent 改的是真文件 → 真 PR。
- **P5**：`spec_approval` / `code_review` 的人 gate：advance 到 gate 时暂停等人（P3 现在 gate 仍自动前进；P5 改成 gate 必须等人）。`escalated` 事件触发请人通知。
- **P6**：`loop_back` / `escalated` / `agent_output` 等事件走 WebSocket 实时推。
