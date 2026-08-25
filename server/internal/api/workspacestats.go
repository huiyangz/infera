// 跨项目统计聚合（L202608251850-1-T01 冻结契约）：GET /api/stats?hours=168&tz=UTC，
// 前端「统计」页唯一数据源——任务状态分布 + 执行维度基础统计 + 逐小时分桶。
package api

import (
	"net/http"
	"time"

	"github.com/tokfinity/infera/internal/store"
)

// 参数口径（冻结契约）：hours 缺省 168（7 天）、1..720（30 天）；tz 缺省
// UTC、取 IANA 时区名（如 Asia/Shanghai，逐小时分桶的归桶时区），非法 → 400。
const (
	workspaceStatsDefaultHours = 168
	workspaceStatsMaxHours     = 720
	workspaceStatsDefaultTZ    = "UTC"
)

// statsWindow 查询窗口（to = 当前时刻 UTC，from = to - hours），RFC3339。
type statsWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// workspaceStatsResponse 响应载荷（冻结契约，形状不得静默变更）：task_status
// 为全量快照（状态是当前态，不受 hours 窗口影响）；execution 与 hourly 只
// 统计窗口内的 stage_runs，聚合口径冻结于 store.WorkspaceStats。
type workspaceStatsResponse struct {
	Window     statsWindow                 `json:"window"`
	Timezone   string                      `json:"timezone"`
	TaskStatus store.WorkspaceTaskStatus   `json:"task_status"`
	Execution  store.WorkspaceExecution    `json:"execution"`
	Hourly     []store.WorkspaceHourBucket `json:"hourly"`
}

// handleWorkspaceStats 只读聚合：窗口右端取当前时刻，聚合全部在 store 内存
// 完成（无写路径、无 schema 变更）。
func (s *Server) handleWorkspaceStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hours, ok := queryInt(q, "hours", workspaceStatsDefaultHours)
	if !ok || hours < 1 || hours > workspaceStatsMaxHours {
		writeError(w, http.StatusBadRequest, "参数不合法：hours 需为 1..720 的整数")
		return
	}
	tzName := q.Get("tz")
	if tzName == "" {
		tzName = workspaceStatsDefaultTZ
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "参数不合法：tz 需为 IANA 时区名（如 Asia/Shanghai）")
		return
	}
	to := time.Now().UTC()
	from := to.Add(-time.Duration(hours) * time.Hour)
	st, err := s.st.WorkspaceStats(r.Context(), from, to, loc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取统计失败")
		return
	}
	writeJSON(w, http.StatusOK, workspaceStatsResponse{
		Window:     statsWindow{From: from, To: to},
		Timezone:   tzName,
		TaskStatus: st.TaskStatus,
		Execution:  st.Execution,
		Hourly:     st.Hourly,
	})
}
