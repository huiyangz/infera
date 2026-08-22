package syncsvc

import "time"

// Status 取值（GET /api/task-sync/status 的 status 字段，语义冻结）。
const (
	StatusIdle    = "idle"    // 从未完成过任何一轮同步
	StatusRunning = "running" // 一轮同步进行中
	StatusSuccess = "success" // 最近一轮成功
	StatusError   = "error"   // 最近一轮失败（Error 字段带原因）
)

// Status 同步状态快照（GET /api/task-sync/status 的载荷，INFERA-169 冻结
// 契约：字段名 lastSyncAt / status / error 与取值语义由本任务冻结，前端
// 按此对接，不得另立字段）。
type Status struct {
	// LastSyncAt 最近一轮完成时间；null = 从未完成过。running 期间不改写——
	// 它始终描述最近完成的一轮。
	LastSyncAt *time.Time `json:"lastSyncAt"`
	Status     string     `json:"status"` // idle|running|success|error
	Error      string     `json:"error"`  // 最近完成一轮的失败原因；"" = 无
}

// Status 返回当前同步状态：running 优先于一切；否则按最近完成一轮推导
// idle（从未同步）/ success / error。
func (s *Service) Status() Status {
	st := Status{Status: StatusIdle}
	if last := s.Last(); last != nil {
		fin := last.FinishedAt
		st.LastSyncAt = &fin
		st.Error = last.Error
		st.Status = StatusSuccess
		if last.Error != "" {
			st.Status = StatusError
		}
	}
	if s.Running() {
		st.Status = StatusRunning
	}
	return st
}
