# infera P5（Gate UI）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `spec_approval` 和 `code_review` 两个人 gate 落地成"暂停等人审批"——advance 到 gate 时流水线停下，人在 UI 上看 spec（或 Reviewer 意见 + PR），点批准/打回；批准则前进、打回则按 gate 类型回退（spec 打回→重写 spec；审核打回→回 code_gen）。blocked（升级）在 UI 上显式标出。

**Architecture:** `deliveries.pending_gate` 记录当前等在哪个 gate（null=没在等）。`Advance` 到 gate stage 时执行完前置 Agent（Reviewer 预审）后**设 pending_gate 并停下**，不自动前进。新增 `Approve` / `Reject` service+API：Approve 清 pending_gate 并前进；Reject 清 pending_gate 并回退（spec_approval→spec、code_review→code_gen）。前端一个 Gate 审批页按 gate 类型渲染不同内容。

**Tech Stack:** Go（沿用）· Next.js（沿用）
**依赖：** P3（loop/retryCodeAt）、P4（pr_opened 事件、Reviewer 意见在 timeline）
**Spec：** 产品设计文档 §3.6、§7.3、§7.7

---

## 文件结构

```
server/
├── migrations/000006_delivery_pending_gate.{up,down}.sql
├── pkg/db/queries/deliveries.sql          # 加 SetPendingGate / ClearPendingGate
└── internal/
    ├── service/delivery.go                # Advance 到 gate 暂停 + Approve/Reject
    ├── service/delivery_gate_test.go
    └── handler/delivery.go                # GET /gate, POST /approve, POST /reject
apps/web/app/deliveries/[id]/gate/page.tsx # Gate 审批页
```

---

## Task 1: DB 迁移 — pending_gate

**Files:**
- Create: `server/migrations/000006_delivery_pending_gate.up.sql`
- Create: `server/migrations/000006_delivery_pending_gate.down.sql`
- Modify: `server/pkg/db/queries/deliveries.sql`

- [ ] **Step 1: 迁移**

`up`:
```sql
ALTER TABLE deliveries ADD COLUMN pending_gate text;
```
`down`:
```sql
ALTER TABLE deliveries DROP COLUMN IF EXISTS pending_gate;
```

- [ ] **Step 2: sqlc 查询**（追加到 `deliveries.sql`）
```sql
-- name: SetDeliveryPendingGate :one
UPDATE deliveries SET pending_gate = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ClearDeliveryPendingGate :one
UPDATE deliveries SET pending_gate = NULL, updated_at = now() WHERE id = $1 RETURNING *;
```

- [ ] **Step 3: 生成 + 编译 + 提交**

```bash
cd server
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
sqlc generate
go build ./...
git add server/migrations/ server/pkg/db/
git commit -m "feat(db): deliveries pending_gate"
```

---

## Task 2: Advance 到 gate 暂停 + Approve/Reject service

**Files:**
- Modify: `server/internal/service/delivery.go`
- Create: `server/internal/service/delivery_gate_test.go`

> 改 P3 的 Advance：到 `spec_approval` / `code_review` 时，执行前置（Reviewer 预审）后**设 pending_gate 停下**，不自动前进/判定。code_review 不再用 P3 的"自动按 decision 前进"，改成暂停等人。

- [ ] **Step 1: 改 Advance 的 gate 分支**

在 `Advance` 里推进到 `next` 之后，加入 gate 暂停逻辑（替换 P3 里 code_review 的自动 decision 判定块）：
```go
	// gate：执行前置 Agent（code_review 先跑 Reviewer 预审），然后暂停等人
	if stage.IsGate(next) {
		if next == "code_review" && s.executor != nil {
			_, _ = s.executor.ExecuteStage(ctx, d.ID, next, buildPromptForStage(next, d))
		}
		d, err = s.q.SetDeliveryPendingGate(ctx, generated.SetDeliveryPendingGateParams{ID: d.ID, PendingGate: pgString(next)})
		if err != nil {
			return generated.Delivery{}, err
		}
		s.timeline(ctx, d.ID, next, "gate_waiting", map[string]any{"gate": next})
		return d, nil // 停下，等人
	}
```

> 删掉 P3 里 `if next == "code_review" && s.executor != nil { ... latestReviewDecision ... reject ... }` 那段自动判定。`unit_test` 的 RealTestRunner 判定保留（它不是 gate，是自动 loop）。
> `pgString` 把 stage 名转 `*string`：sqlc 对 nullable text 生成 `*string`。实现：
> ```go
> func pgString(s string) *string { return &s }
> ```

- [ ] **Step 2: 实现 Approve / Reject**

`server/internal/service/delivery.go` 追加：
```go
// Approve 人批准当前 gate：先记 gate 名，再清 pending_gate，然后前进。
// 必须在 Clear 前读 PendingGate，否则 Clear 后是 nil。
func (s *DeliveryService) Approve(ctx context.Context, id uuid.UUID) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, err
	}
	gate := ""
	if d.PendingGate != nil {
		gate = *d.PendingGate
	}
	if _, err := s.q.ClearDeliveryPendingGate(ctx, id); err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, id, gate, "gate_approved", map[string]any{})
	return s.Advance(ctx, id)
}
```

```go
// Reject 人打回当前 gate：清 pending_gate，按 gate 类型回退。
// spec_approval → spec；code_review → code_gen（复用 P3 的 retry，但不计 fail_count 升级——人打回不计次数）。
func (s *DeliveryService) Reject(ctx context.Context, id uuid.UUID, reason string) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil { return generated.Delivery{}, err }
	gate := ""
	if d.PendingGate != nil { gate = *d.PendingGate }
	if _, err := s.q.ClearDeliveryPendingGate(ctx, id); err != nil { return generated.Delivery{}, err }
	s.timeline(ctx, id, gate, "gate_rejected", map[string]any{"reason": reason})

	target := "code_gen"
	if gate == "spec_approval" { target = "spec" }
	d, err = s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{ID: id, CurrentStage: target})
	if err != nil { return generated.Delivery{}, err }
	s.timeline(ctx, id, target, "loop_back", map[string]any{"from": gate, "reason": reason})
	if s.executor != nil {
		_, _ = s.executor.ExecuteStage(ctx, id, target, "人打回："+reason+"\n请重做。")
	}
	return d, nil
}
```

- [ ] **Step 3: 写 gate 测试**

`server/internal/service/delivery_gate_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestAdvanceToCodeReviewPauses(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Reviewer Agent','reviewer','{"system_prompt":"x"}')`)
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='unit_test' WHERE id=$1", d.ID)

	// unit_test → code_review（gate）：应暂停，pending_gate=code_review，不进 deploy
	d, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Equal(t, "code_review", d.CurrentStage)
	assert.NotNil(t, d.PendingGate)
	assert.Equal(t, "code_review", *d.PendingGate)
}

func TestApproveAdvances(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(),
		"UPDATE deliveries SET current_stage='code_review', pending_gate='code_review' WHERE id=$1", d.ID)

	d, err := svc.Approve(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Nil(t, d.PendingGate)
	assert.Equal(t, "deploy", d.CurrentStage) // 批准后前进到 deploy
}

func TestRejectCodeReviewBacksToCodeGen(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(),
		"UPDATE deliveries SET current_stage='code_review', pending_gate='code_review' WHERE id=$1", d.ID)

	d, err := svc.Reject(context.Background(), d.ID, "边界 case 没覆盖")
	assert.NoError(t, err)
	assert.Equal(t, "code_gen", d.CurrentStage)
	assert.Nil(t, d.PendingGate)
}

func TestRejectSpecApprovalBacksToSpec(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(),
		"UPDATE deliveries SET current_stage='spec_approval', pending_gate='spec_approval' WHERE id=$1", d.ID)

	d, _ = svc.Reject(context.Background(), d.ID, "验收标准不清")
	assert.Equal(t, "spec", d.CurrentStage)
}
```

- [ ] **Step 4: 跑测试 + 提交**

```bash
go test ./internal/service/ -run "TestAdvanceToCodeReviewPauses|TestApprove|TestReject" -v
git add server/internal/service/
git commit -m "feat(server): gate pause, approve and reject"
```

---

## Task 3: Gate API（GET 内容 + POST approve/reject）

**Files:**
- Modify: `server/internal/handler/delivery.go`
- Modify: `server/internal/handler/router.go`

- [ ] **Step 1: 加 handler 方法**

`server/internal/handler/delivery.go` 追加：
```go
// Gate 返回当前 gate 需要人看的内容：gate 名 + 最近相关 agent_output（spec 或 review）+ PR 链接。
func (h *DeliveryHandler) Gate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := generated.New(h.pool).GetDelivery(r.Context(), uuidOrNil(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	gate := ""
	if d.PendingGate != nil { gate = *d.PendingGate }
	events, _ := generated.New(h.pool).ListTimelineEvents(r.Context(), d.ID)

	// 找最近一条对应 gate 的 agent_output
	var latest map[string]any
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == "agent_output" {
			_ = json.Unmarshal(events[i].Payload, &latest)
			break
		}
	}
	// 找 pr_opened 的 url
	var prURL string
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].EventType == "pr_opened" {
			var p map[string]any
			_ = json.Unmarshal(events[i].Payload, &p)
			if u, ok := p["url"].(string); ok { prURL = u }
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"delivery_id": d.ID, "gate": gate, "agent_output": latest, "pr_url": prURL,
	})
}

func (h *DeliveryHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := h.svc.Approve(r.Context(), uuidOrNil(id))
	if err != nil { writeErr(w, http.StatusBadRequest, err.Error()); return }
	writeJSON(w, http.StatusOK, d)
}

func (h *DeliveryHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct{ Reason string `json:"reason"` }
	_ = json.NewDecoder(r.Body).Decode(&body)
	d, err := h.svc.Reject(r.Context(), uuidOrNil(id), body.Reason)
	if err != nil { writeErr(w, http.StatusBadRequest, err.Error()); return }
	writeJSON(w, http.StatusOK, d)
}
```

- [ ] **Step 2: 注册路由**

`router.go` 的 `/api/deliveries` group 追加：
```go
		r.Get("/{id}/gate", dh.Gate)
		r.Post("/{id}/approve", dh.Approve)
		r.Post("/{id}/reject", dh.Reject)
```

- [ ] **Step 3: 编译 + 手动验证**

```bash
go build ./...
go run ./cmd/server &
sleep 1
ID=... # 一条 pending_gate=spec_approval 的 delivery
curl -s http://localhost:8080/api/deliveries/$ID/gate
curl -s -X POST http://localhost:8080/api/deliveries/$ID/approve
kill %1
```
Expected：gate 返回 gate 名 + spec 内容；approve 后 pending_gate 清空、stage 前进

- [ ] **Step 4: 提交**

```bash
git add server/internal/handler/
git commit -m "feat(server): gate, approve, reject endpoints"
```

---

## Task 4: 前端 Gate 审批页

**Files:**
- Create: `apps/web/lib/api.ts`（加 gate/approve/reject）
- Create: `apps/web/app/deliveries/[id]/gate/page.tsx`

- [ ] **Step 1: API client 追加**

`apps/web/lib/api.ts` 末尾追加：
```ts
export interface GateInfo {
  delivery_id: string;
  gate: string;
  agent_output: { agent?: string; output?: string } | null;
  pr_url: string;
}
export async function getGate(id: string): Promise<GateInfo> {
  const r = await fetch(`/api/deliveries/${id}/gate`);
  if (!r.ok) throw new Error("gate failed");
  return r.json();
}
export async function approveGate(id: string) {
  const r = await fetch(`/api/deliveries/${id}/approve`, { method: "POST" });
  if (!r.ok) throw new Error("approve failed");
  return r.json();
}
export async function rejectGate(id: string, reason: string) {
  const r = await fetch(`/api/deliveries/${id}/reject`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ reason }),
  });
  if (!r.ok) throw new Error("reject failed");
  return r.json();
}
```

- [ ] **Step 2: Gate 审批页**

`apps/web/app/deliveries/[id]/gate/page.tsx`:
```tsx
"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { useState } from "react";
import { getGate, approveGate, rejectGate } from "@/lib/api";

export default function GatePage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["gate", params.id],
    queryFn: () => getGate(params.id),
  });
  const [reason, setReason] = useState("");

  const approve = useMutation({
    mutationFn: () => approveGate(params.id),
    onSuccess: () => { qc.invalidateQueries(); router.push(`/deliveries/${params.id}`); },
  });
  const reject = useMutation({
    mutationFn: () => rejectGate(params.id, reason),
    onSuccess: () => { qc.invalidateQueries(); router.push(`/deliveries/${params.id}`); },
  });

  if (isLoading || !data) return <main className="p-8">加载中…</main>;
  const isSpec = data.gate === "spec_approval";

  return (
    <main className="max-w-3xl mx-auto p-8">
      <Link href={`/deliveries/${params.id}`} className="text-sm text-gray-500">← 返回详情</Link>
      <h1 className="text-2xl font-bold mt-2 mb-2">
        {isSpec ? "Spec 审批" : "代码审核"}
      </h1>
      <p className="text-sm text-gray-500 mb-6">gate: {data.gate}</p>

      <h2 className="font-semibold mb-2">{isSpec ? "Spec 内容" : "Reviewer 意见"}</h2>
      <pre className="bg-gray-50 border rounded p-4 whitespace-pre-wrap text-sm mb-4 min-h-24">
        {data.agent_output?.output || "（无 Agent 产出）"}
      </pre>

      {!isSpec && data.pr_url && (
        <p className="mb-4 text-sm">
          PR：<a className="text-blue-600 underline" href={data.pr_url} target="_blank" rel="noreferrer">{data.pr_url}</a>
        </p>
      )}

      <div className="flex gap-2 items-start">
        <button
          className="bg-green-700 text-white rounded px-4 py-2"
          onClick={() => approve.mutate()}
        >批准 →</button>
        <input
          className="flex-1 border rounded px-3 py-2"
          placeholder="打回理由…"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <button
          className="border border-red-600 text-red-700 rounded px-4 py-2"
          disabled={!reason.trim()}
          onClick={() => reject.mutate()}
        >打回</button>
      </div>
      <p className="text-xs text-gray-400 mt-2">
        批准：前进到下一 stage。打回：{isSpec ? "回 spec 重写" : "回 code_gen 重做"}。
      </p>
    </main>
  );
}
```

- [ ] **Step 3: 详情页加"去审批"入口**

`apps/web/app/deliveries/[id]/page.tsx` 在 status 显示附近加：
```tsx
{delivery.pending_gate && (
  <Link href={`/deliveries/${params.id}/gate`} className="inline-block bg-yellow-600 text-white rounded px-4 py-2 mb-4">
    需审批：{delivery.pending_gate} → 去审批
  </Link>
)}
```
（types.ts 的 `Delivery` 加 `pending_gate: string | null`。）

- [ ] **Step 4: 验证 + 提交**

```bash
# 后端 + 前端起着，推进到 code_review，详情页出现"去审批"，进入 gate 页批准/打回
cd /Users/huiyangz/tokfinity/infera
git add apps/web/
git commit -m "feat(web): gate approval page (spec + code review)"
```

---

## Task 5: 列表展示 gate/blocked 状态 + 升级提示

**Files:**
- Modify: `apps/web/app/page.tsx`

- [ ] **Step 1: 列表项加状态徽标**

`apps/web/app/page.tsx` 的列表项 `{d.current_stage} · {d.status}` 改为：
```tsx
<div className="text-sm text-gray-500 flex gap-2 items-center">
  <span>{d.current_stage}</span>
  {d.status === "blocked" && (
    <span className="bg-red-100 text-red-700 px-2 py-0.5 rounded text-xs">已升级 · 需人工介入</span>
  )}
  {d.pending_gate && (
    <Link href={`/deliveries/${d.id}/gate}`} className="bg-yellow-100 text-yellow-800 px-2 py-0.5 rounded text-xs">
      待审批：{d.pending_gate}
    </Link>
  )}
  {!d.pending_gate && d.status === "active" && <span>· {d.status}</span>}
</div>
```

- [ ] **Step 2: 验证 + 提交**

```bash
# 制造一条 blocked（连续 3 次单测失败）与一条 pending_gate，列表应显示对应徽标
git add apps/web/app/page.tsx
git commit -m "feat(web): show blocked and pending-gate badges in list"
```

---

## P5 完成标准

- [ ] `deliveries.pending_gate` 字段 + Advance 到 gate 暂停
- [ ] Approve（前进）/ Reject（spec→spec、review→code_gen）service + API + 测试
- [ ] Gate 审批页：spec 内容 / Reviewer 意见 + PR 链接，批准/打回
- [ ] 列表与详情显示 pending_gate（待审批）与 blocked（已升级）徽标
- [ ] 所有 `go test ./...` 通过

## 给后续 Plan 的接口约定

- **P6**：`gate_waiting` / `gate_approved` / `gate_rejected` / `escalated` 事件走 WebSocket 实时推——人能第一时间收到"待审批"和"已升级"通知。
