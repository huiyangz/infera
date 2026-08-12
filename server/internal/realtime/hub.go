package realtime

import (
	"sync"

	"github.com/google/uuid"
)

// Hub 维护 deliveryID → 客户端通道 的进程内订阅表。
type Hub struct {
	mu     sync.RWMutex
	subs   map[uuid.UUID]map[chan Event]struct{}
	stopCh chan struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[uuid.UUID]map[chan Event]struct{}{}, stopCh: make(chan struct{})}
}

// Run 阻塞到 Stop（当前为同步广播，保留以兼容未来异步分发）。
func (h *Hub) Run() { <-h.stopCh }

func (h *Hub) Stop() { close(h.stopCh) }

// Subscribe 订阅某 delivery 的事件，返回一个缓冲通道。
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

// Unsubscribe 退订并关闭通道（ws handler 的 range 随之退出）。
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

// Broadcast 实现 Broadcaster。全程持 RLock，避免与 Unsubscribe 的 close 竞争；
// 消费者慢则丢弃该条（MVP 可接受）。
func (h *Hub) Broadcast(deliveryID uuid.UUID, e Event) {
	e.DeliveryID = deliveryID.String()
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[deliveryID] {
		select {
		case ch <- e:
		default:
			// 消费者落后，丢这条（避免阻塞整个 hub）
		}
	}
}
