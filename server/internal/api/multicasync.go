// Multica 同步面（INFERA-80 T03）：POST 触发一轮全量同步 / GET 最近一次结果。
// 契约（冻结供 T04/T05 消费）：
//
//	POST /api/multica/sync  → 200 syncsvc.Result（触发后同步执行，完成即回）
//	                           409 已有同步在进行；502 上游拉取/落库失败；
//	                           503 未装配（MULTICA_* 未配置）
//	GET  /api/multica/sync  → 200 {"running": bool, "last": Result|null}
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/tokfinity/infera/internal/syncsvc"
)

// MulticaSyncAPI 是 api 层对 syncsvc 的最小依赖（*syncsvc.Service 天然满足）。
// NewServer 允许 nil（未装配 → 同步路由 503），SetMulticaSync 在 main 里后置注入。
type MulticaSyncAPI interface {
	SyncNow(ctx context.Context) (syncsvc.Result, error)
	Running() bool
	Last() *syncsvc.Result
}

func (s *Server) SetMulticaSync(svc MulticaSyncAPI) { s.multicaSync = svc }

// handleMulticaSyncTrigger 立即执行一轮同步并返回结果。
func (s *Server) handleMulticaSyncTrigger(w http.ResponseWriter, r *http.Request) {
	if s.multicaSync == nil {
		writeError(w, http.StatusServiceUnavailable, "multica 同步未装配（需配置 MULTICA_SERVER_URL / MULTICA_TOKEN / MULTICA_WORKSPACE_ID）")
		return
	}
	res, err := s.multicaSync.SyncNow(r.Context())
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

// handleMulticaSyncStatus 返回运行中标志与最近一轮结果（从未同步过 last=null）。
func (s *Server) handleMulticaSyncStatus(w http.ResponseWriter, r *http.Request) {
	if s.multicaSync == nil {
		writeError(w, http.StatusServiceUnavailable, "multica 同步未装配（需配置 MULTICA_SERVER_URL / MULTICA_TOKEN / MULTICA_WORKSPACE_ID）")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"running": s.multicaSync.Running(),
		"last":    s.multicaSync.Last(),
	})
}
