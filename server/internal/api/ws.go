package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// hub 按 delivery 订阅分发；Publish 供引擎 Notify 注入调用（main 组装）。
type hub struct {
	mu   sync.Mutex
	subs map[string]map[*websocket.Conn]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[*websocket.Conn]struct{}{}} }

func (h *hub) subscribe(deliveryID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.subs[deliveryID]
	if !ok {
		set = map[*websocket.Conn]struct{}{}
		h.subs[deliveryID] = set
	}
	set[c] = struct{}{}
}

func (h *hub) unsubscribe(deliveryID string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.subs[deliveryID]
	if !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(h.subs, deliveryID)
	}
}

// Publish 向某 delivery 的所有订阅者广播 {stage, event} JSON；
// 慢/断开连接写失败静默丢弃——读循环检测到断开后会 unsubscribe 清理。
func (h *hub) Publish(deliveryID, stage, eventType string) {
	b, _ := json.Marshal(map[string]string{"stage": stage, "event": eventType})
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.subs[deliveryID]))
	for c := range h.subs[deliveryID] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		_ = c.WriteMessage(websocket.TextMessage, b)
	}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

// handleWS GET /ws?delivery=<id> —— 升级连接并保持（读循环只为检测断开）。
// MVP 挂公开组：前端带 cookie 连接即可，后续可加 requireAuth。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	deliveryID := r.URL.Query().Get("delivery")
	if deliveryID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.subscribe(deliveryID, c)
	defer func() {
		s.hub.unsubscribe(deliveryID, c)
		_ = c.Close()
	}()
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
	}
}

// Publish 导出转发，main 可将 engine.Notify 直接接到 Server.Publish
// 而无需引用未导出的 hub 类型。
func (s *Server) Publish(deliveryID, stage, event string) {
	s.hub.Publish(deliveryID, stage, event)
}
