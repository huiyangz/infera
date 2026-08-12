# infera P6（实时时间线）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delivery 的每一次进展——stage 推进、Agent 产出、loop 回退、gate 等待/批准、升级——通过 WebSocket 实时推到前端，详情页 timeline 自动刷新，人不用手点。也用于"待审批 / 已升级"的即时通知。

**Architecture:** 一个进程内 WebSocket Hub，维护 `deliveryID → 客户端列表`。`DeliveryService` 持有 `Broadcaster` 接口，每次写 timeline event 后调 `Broadcast`。`/ws?delivery=<id>` 升级连接、订阅该 delivery。前端 `useDeliveryEvents` hook 收到事件后让 TanStack Query 失效重拉。

**Tech Stack:** Go · github.com/gorilla/websocket · Next.js（沿用）
**依赖：** P1-P5（所有 timeline 事件类型都已定义）
**Spec：** 产品设计文档 §6.4、§4.2（Delivery 时间线）

---

## 文件结构

```
server/internal/
├── realtime/
│   ├── hub.go          # Hub + Client + Broadcast
│   └── hub_test.go
├── realtime/realtime.go  # Broadcaster 接口（service 依赖它，解耦）
├── handler/ws.go       # /ws handler
└── service/delivery.go # timeline 后调 broadcaster（已存在，注入即可）
apps/web/lib/useDeliveryEvents.ts
```

---

## Task 1: Hub + Broadcaster 接口

**Files:**
- Create: `server/internal/realtime/realtime.go`
- Create: `server/internal/realtime/hub.go`
- Create: `server/internal/realtime/hub_test.go`

- [ ] **Step 1: 装依赖**

```bash
cd server
go get github.com/gorilla/websocket@latest
go mod tidy
```

- [ ] **Step 2: 先写测试**

`server/internal/realtime/hub_test.go`:
```go
package realtime

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestBroadcastReachesSubscribedClient(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	did := uuid.New()
	ch := h.Subscribe(did)
	defer h.Unsubscribe(did, ch)

	h.Broadcast(did, Event{Type: "stage_started", Stage: "spec"})

	select {
	case e := <-ch:
		assert.Equal(t, "stage_started", e.Type)
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestBroadcastDoesNotReachOtherDelivery(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	a := uuid.New()
	b := uuid.New()
	chA := h.Subscribe(a)
	defer h.Unsubscribe(a, chA)
	_ = h.Subscribe(b)

	h.Broadcast(b, Event{Type: "x"})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); time.Sleep(100 * time.Millisecond) }()
	wg.Wait()

	select {
	case e := <-chA:
		t.Fatalf("delivery A should not get B's event, got %+v", e)
	default:
		// good
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

```bash
go test ./internal/realtime/ -v
```
Expected: FAIL / 编译错误

- [ ] **Step 4: 实现 Broadcaster 接口 + Event 类型**

`server/internal/realtime/realtime.go`:
```go
package realtime

import "github.com/google/uuid"

// Event 是推给前端的一次时间线事件。
type Event struct {
	Type      string `json:"type"`       // 对应 timeline event_type
	Stage     string `json:"stage"`
	DeliveryID string `json:"delivery_id"`
}

// Broadcaster 由 service 调用：写完 timeline 后广播。Hub 实现它。
type Broadcaster interface {
	Broadcast(deliveryID uuid.UUID, e Event)
}
```

- [ ] **Step 5: 实现 Hub**

`server/internal/realtime/hub.go`:
```go
package realtime

import (
	"sync"

	"github.com/google/uuid"
)

type Hub struct {
	mu      sync.RWMutex
	subs    map[uuid.UUID]map[chan Event]struct{}
	stopCh  chan struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[uuid.UUID]map[chan Event]struct{}{}, stopCh: make(chan struct{})}
}

// Run 占位（当前为同步广播）；保留以兼容未来异步分发。
func (h *Hub) Run() { <-h.stopCh }

func (h *Hub) Stop() { close(h.stopCh) }

func (h *Hub) Subscribe(deliveryID uuid.UUID) chan Event {
	ch := make(chan Event, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[deliveryID] == nil {
		h.subs[deliveryID] = map[chan Event]struct{}{}
	}
	h.subs[deliveryID][ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(deliveryID uuid.UUID, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if clients, ok := h.subs[deliveryID]; ok {
		delete(clients, ch)
		close(ch)
		if len(clients) == 0 {
			delete(h.subs, deliveryID)
		}
	}
}

// Broadcast 实现 Broadcaster。非阻塞：消费者慢则丢弃（MVP 可接受）。
func (h *Hub) Broadcast(deliveryID uuid.UUID, e Event) {
	e.DeliveryID = deliveryID.String()
	h.mu.RLock()
	clients := h.subs[deliveryID]
	h.mu.RUnlock()
	for ch := range clients {
		select {
		case ch <- e:
		default:
			// 消费者落后，丢这条（避免阻塞整个 hub）
		}
	}
}
```

- [ ] **Step 6: 跑测试 + 提交**

```bash
go test ./internal/realtime/ -v
git add server/internal/realtime/ server/go.mod server/go.sum
git commit -m "feat(realtime): websocket hub and broadcaster"
```

---

## Task 2: service 注入 Broadcaster，timeline 后广播

**Files:**
- Modify: `server/internal/service/delivery.go`

- [ ] **Step 1: DeliveryService 加 broadcaster**

结构加字段、加链式方法：
```go
type DeliveryService struct {
	q           *generated.Queries
	executor    *ExecuteService
	testRunner  testrunner.Runner
	broadcaster realtime.Broadcaster // 可为 nil
}

func (s *DeliveryService) WithBroadcaster(b realtime.Broadcaster) *DeliveryService {
	s.broadcaster = b
	return s
}
```

- [ ] **Step 2: timeline() 写完后广播**

修改 `timeline` 方法末尾，在 `CreateTimelineEvent` 之后加：
```go
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(deliveryID, realtime.Event{Type: eventType, Stage: stageName})
	}
```

> 顶部 import 加 `"github.com/tokfinity/infera/internal/realtime"`。`ExecuteService` 若也直接写 timeline（P2），给它同样加 broadcaster 字段并在 `ExecuteStage` 写 event 后广播——或统一让所有 timeline 写入走 `DeliveryService.timeline`。MVP 简化：只让 `DeliveryService.timeline` 广播；`ExecuteService` 写的 `agent_output` 事件由调用方（Advance）之后补一条广播，或把 ExecuteService 的 timeline 写入也委托给一个共享 helper。最小改动：在 `ExecuteService` 也注入同一个 broadcaster，写完 event 后广播。

- [ ] **Step 3: main 装配**

`server/cmd/server/main.go`：
```go
	hub := realtime.NewHub()
	go hub.Run()
	deliverySvc := service.New(pool).
		WithExecutor(executor).
		WithTestRunner(testRunner).
		WithBroadcaster(hub)
	executor.WithBroadcaster(hub) // 若 ExecuteService 也加了该字段
```

- [ ] **Step 4: 编译 + 提交**

```bash
go build ./...
git add server/internal/service/ server/cmd/server/
git commit -m "feat(server): broadcast timeline events"
```

---

## Task 3: /ws handler + 路由

**Files:**
- Create: `server/internal/handler/ws.go`
- Modify: `server/internal/handler/router.go`

- [ ] **Step 1: 实现 ws handler**

`server/internal/handler/ws.go`:
```go
package handler

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/tokfinity/infera/internal/realtime"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // MVP：放开跨域
}

func WS(hub *realtime.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("delivery")
		deliveryID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid delivery id", http.StatusBadRequest)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		ch := hub.Subscribe(deliveryID)
		defer hub.Unsubscribe(deliveryID, ch)

		for e := range ch {
			if err := conn.WriteJSON(e); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 2: 注册路由**

`router.go` 把 `NewRouter` 加 `hub *realtime.Hub` 参数，并注册：
```go
	r.Get("/ws", WS(hub))
```
main 里 `handler.NewRouter(pool, deliverySvc, hub)`。

- [ ] **Step 3: 编译 + 提交**

```bash
go build ./...
git add server/internal/handler/ server/cmd/server/
git commit -m "feat(server): websocket endpoint for delivery events"
```

---

## Task 4: 前端 useDeliveryEvents hook

**Files:**
- Create: `apps/web/lib/useDeliveryEvents.ts`
- Modify: `apps/web/next.config.mjs`（/ws 不走 rewrite）

- [ ] **Step 1: next.config 去掉 /ws 的 rewrite**

`apps/web/next.config.mjs` 的 rewrites 加 `/ws` 直连后端（WebSocket 不走 fetch rewrite）：
```js
      { source: "/ws", destination: "ws://localhost:8080/ws", has: [{ type: "query", key: "delivery" }] },
```

- [ ] **Step 2: hook**

`apps/web/lib/useDeliveryEvents.ts`:
```ts
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

export function useDeliveryEvents(deliveryId: string) {
  const qc = useQueryClient();
  useEffect(() => {
    const ws = new WebSocket(`ws://localhost:8080/ws?delivery=${deliveryId}`);
    ws.onmessage = () => {
      // 收到任何事件：让 delivery 与 timeline 重拉
      qc.invalidateQueries({ queryKey: ["delivery", deliveryId] });
      qc.invalidateQueries({ queryKey: ["gate", deliveryId] });
    };
    return () => ws.close();
  }, [deliveryId, qc]);
}
```

- [ ] **Step 3: 提交**

```bash
git add apps/web/next.config.mjs apps/web/lib/useDeliveryEvents.ts
git commit -m "feat(web): useDeliveryEvents websocket hook"
```

---

## Task 5: 详情页接入实时更新

**Files:**
- Modify: `apps/web/app/deliveries/[id]/page.tsx`

- [ ] **Step 1: 在详情页调用 hook**

`apps/web/app/deliveries/[id]/page.tsx` 顶部组件内加：
```tsx
import { useDeliveryEvents } from "@/lib/useDeliveryEvents";
// ...
export default function DeliveryDetailPage() {
  const params = useParams<{ id: string }>();
  useDeliveryEvents(params.id); // 收到事件自动 invalidate → 重拉 timeline
  // ...其余不变
}
```

- [ ] **Step 2: 端到端验证**

```bash
# 后端 + 前端起着
# 1. 打开详情页（标签 A）
# 2. 在另一个标签/终端 advance 这条 delivery（curl POST /advance）
# 3. 标签 A 的 timeline 应自动新增事件，无需手点刷新
```
Expected：A 页 timeline 实时增长

- [ ] **Step 3: 提交**

```bash
git add apps/web/app/deliveries/[id]/page.tsx
git commit -m "feat(web): realtime timeline on delivery detail"
```

---

## P6 完成标准

- [ ] `realtime.Hub`：按 deliveryID 订阅/退订/广播，测试覆盖（订阅收到、跨 delivery 隔离）
- [ ] `DeliveryService`（及 `ExecuteService`）写 timeline 后广播
- [ ] `/ws?delivery=<id>` 升级连接、推送该 delivery 事件
- [ ] 前端 `useDeliveryEvents` 收到事件让相关 query 失效重拉
- [ ] 详情页 timeline 实时刷新，端到端验证通过
- [ ] 所有 `go test ./...` 通过

---

## 全部 6 个 Plan 完成后的整体状态

P1 地基 → P2 Agent 执行 → P3 loop → P4 GitHub → P5 Gate UI → P6 实时，按依赖顺序串起完整的 MVP：**提一句需求 → 多专职 Agent 接力（spec/测试/代码/审核）→ loop 自治迭代 → 真 PR → 人审批 → 部署，全程实时可见**。

每个 plan 末尾的"接口约定"标注了它给下一个 plan 暴露的接缝，可独立实现、独立测试。
