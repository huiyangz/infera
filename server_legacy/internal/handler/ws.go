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

// WS 升级为 WebSocket，订阅该 delivery 的事件并推送。
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
