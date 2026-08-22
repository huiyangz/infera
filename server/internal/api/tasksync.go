// 任务同步面（INFERA-80 T03 / INFERA-169）：POST 触发一轮全量同步，
// GET 最近一次结果，GET status 查自动同步状态。
// 契约（冻结供前端消费）：
//
//	POST /api/task-sync        → 200 syncsvc.Result（触发后同步执行，完成即回）
//	                               409 已有同步在进行；502 上游拉取/落库失败；
//	                               503 未装配（TASK_SYNC_* 未配置）
//	GET  /api/task-sync        → 200 {"running": bool, "last": Result|null}
//	GET  /api/task-sync/status → 200 {"lastSyncAt": time|null, "status": "idle|
//	                               running|success|error", "error": string}
//	                               （INFERA-169 冻结：自动同步状态面，字段语义
//	                               见 syncsvc.Status；未装配同 503）
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/tokfinity/infera/internal/syncsvc"
)

// TaskSyncAPI 是 api 层对 syncsvc 的最小依赖（*syncsvc.Service 天然满足）。
// NewServer 允许 nil（未装配 → 同步路由 503），SetTaskSync 在 main 里后置注入。
type TaskSyncAPI interface {
	SyncNow(ctx context.Context) (syncsvc.Result, error)
	Running() bool
	Last() *syncsvc.Result
	Status() syncsvc.Status
}

func (s *Server) SetTaskSync(svc TaskSyncAPI) { s.taskSync = svc }

// handleTaskSyncTrigger 立即执行一轮同步并返回结果。
func (s *Server) handleTaskSyncTrigger(w http.ResponseWriter, r *http.Request) {
	if s.taskSync == nil {
		writeError(w, http.StatusServiceUnavailable, "任务同步未装配（需配置 TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）")
		return
	}
	res, err := s.taskSync.SyncNow(r.Context())
	if errors.Is(err, syncsvc.ErrSyncRunning) {
		writeError(w, http.StatusConflict, "已有同步在进行，稍后用 GET 查看结果")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "同步失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleTaskSyncState 返回运行中标志与最近一轮结果（从未同步过 last=null）。
func (s *Server) handleTaskSyncState(w http.ResponseWriter, r *http.Request) {
	if s.taskSync == nil {
		writeError(w, http.StatusServiceUnavailable, "任务同步未装配（需配置 TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"running": s.taskSync.Running(),
		"last":    s.taskSync.Last(),
	})
}

// handleTaskSyncStatus 自动同步状态面（INFERA-169 冻结契约）：
// lastSyncAt / status / error，供前端展示「自动同步 · 上次同步时间」
// 与失败提示。
func (s *Server) handleTaskSyncStatus(w http.ResponseWriter, r *http.Request) {
	if s.taskSync == nil {
		writeError(w, http.StatusServiceUnavailable, "任务同步未装配（需配置 TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）")
		return
	}
	writeJSON(w, http.StatusOK, s.taskSync.Status())
}
