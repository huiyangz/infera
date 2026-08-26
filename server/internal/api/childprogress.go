// 子任务真实进度只读聚合面（L202608260142-1-T01 冻结契约）：
//
//	GET /api/deliveries/{id}/progress → 200 store.ChildProgress
//	                                 → 404 交付不存在（含非法 ID）
//
// 只读：不驱动引擎、不改任务状态、不写任何行。取数走既有只读查询
// ListChildDeliveries，聚合口径冻结在 store.AssembleChildProgress——
// 前端任务详情页进度区（L202608260142-3-T01）以本端点为唯一数据源，
// 不得另开并行入口。
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
)

// handleChildProgress 子任务真实进度聚合：不区分拆分父与任务同步父，只要
// 有 parent_id 指向它的子任务即可聚合（空集合同样返回全零 + 空分组）。
func (s *Server) handleChildProgress(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	if _, err := s.st.GetDelivery(r.Context(), id); err != nil {
		writeStoreErr(w, err, "交付不存在", "读取交付失败")
		return
	}
	children, err := s.st.ListChildDeliveries(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取子任务失败")
		return
	}
	writeJSON(w, http.StatusOK, store.AssembleChildProgress(id, children))
}
