package realtime

import "github.com/google/uuid"

// Event 是推给前端的一次时间线事件。
type Event struct {
	Type       string `json:"type"` // 对应 timeline event_type
	Stage      string `json:"stage"`
	DeliveryID string `json:"delivery_id"`
}

// Broadcaster 由 service 调用：写完 timeline 后广播。Hub 实现它。
type Broadcaster interface {
	Broadcast(deliveryID uuid.UUID, e Event)
}
