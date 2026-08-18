// Package deliverylock 提供 per-delivery 互斥锁的进程内共享注册表。
// api（HTTP 后台 driver、approve/reject 簿记）与 mcp（MCP 客户端的
// approve/reject/submit 簿记）是两条独立驾驶面，但引擎自身无并发保护——
// 两面对同一 delivery 的操作必须经同一份锁串行化，否则并发进引擎会
// 双写 UpdateDelivery / 双 advance / 事件乱序。main 建一份注入两处
// （api.NewServer 自建 + DeliveryLocks 导出，mcp 经 SetLocks 接入）。
package deliverylock

import "sync"

// Locks deliveryID → *sync.Mutex（惰性创建，永不删除：进程生命周期内
// delivery 数有限，条目是单指针）。
type Locks struct {
	m sync.Map
}

// New 建共享锁注册表。
func New() *Locks { return &Locks{} }

// For 取/建该 delivery 的互斥锁。
func (l *Locks) For(deliveryID string) *sync.Mutex {
	v, _ := l.m.LoadOrStore(deliveryID, &sync.Mutex{})
	return v.(*sync.Mutex)
}
