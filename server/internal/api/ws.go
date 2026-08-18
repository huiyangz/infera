package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsWriteWait 单帧写超时：对端停读（标签页挂起/网络半断）时内核缓冲写满，
// 无超时的 WriteMessage 会永久阻塞——Publish 是引擎的同步通知路径，
// 一条僵死连接就能反压引擎。var 供测试收紧（生产恒 10s）。
var wsWriteWait = 10 * time.Second

// wsClient 包一层 per-conn 写锁：gorilla 的 conn 不允许并发 WriteMessage
// （会 panic/损坏帧），而 Publish 可能被多个引擎 goroutine 同时触发。
type wsClient struct {
	conn *websocket.Conn
	wmu  sync.Mutex
}

// write 串行写一帧文本消息，带写超时；失败（超时/断开）立即关闭连接——
// 读循环随之退出并 unsubscribe，僵死连接不会长期占用订阅。
func (w *wsClient) write(mt int, b []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	if err := w.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		_ = w.conn.Close()
		return err
	}
	if err := w.conn.WriteMessage(mt, b); err != nil {
		_ = w.conn.Close()
		return err
	}
	return nil
}

// hub 按 delivery 订阅分发；Publish 供引擎 Notify 注入调用（main 组装）。
type hub struct {
	mu   sync.Mutex
	subs map[string]map[*wsClient]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[*wsClient]struct{}{}} }

func (h *hub) subscribe(deliveryID string, c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.subs[deliveryID]
	if !ok {
		set = map[*wsClient]struct{}{}
		h.subs[deliveryID] = set
	}
	set[c] = struct{}{}
}

func (h *hub) unsubscribe(deliveryID string, c *wsClient) {
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

// Publish 向某 delivery 的所有订阅者广播 {stage, event} JSON。
// hub.mu 只保护订阅表快照；对同一 conn 的写由 per-conn 写锁串行化。
func (h *hub) Publish(deliveryID, stage, eventType string) {
	b, _ := json.Marshal(map[string]string{"stage": stage, "event": eventType})
	h.mu.Lock()
	conns := make([]*wsClient, 0, len(h.subs[deliveryID]))
	for c := range h.subs[deliveryID] {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		_ = c.write(websocket.TextMessage, b)
	}
}

// sameOrigin 校验 Origin 与请求 Host 同源（大小写不敏感；语义同 gorilla
// 默认 checkSameOrigin）。无 Origin 的非浏览器客户端放行；浏览器跨站连接拒绝。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

var upgrader = websocket.Upgrader{CheckOrigin: sameOrigin}

// handleWS GET /ws?delivery=<id> —— 校验 delivery 存在后升级连接并保持
// （读循环只为检测断开）。挂在 requireAuth 组：事件流是登录面内容。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	deliveryID := r.URL.Query().Get("delivery")
	if deliveryID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if _, err := s.st.GetDelivery(r.Context(), deliveryID); err != nil {
		writeStoreErr(w, err, "delivery 不存在", "读取 delivery 失败")
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &wsClient{conn: c}
	s.hub.subscribe(deliveryID, client)
	defer func() {
		s.hub.unsubscribe(deliveryID, client)
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
