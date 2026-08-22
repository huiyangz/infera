// 待人工决策需求列表（INFERA-108 T01）：跨项目取全部停在审批门的需求，
// 供前端「需要决策」路由直接渲染；行内 id 即需求详情跳转键。
package api

import (
	"net/http"

	"github.com/tokfinity/infera/internal/store"
)

// handlePendingDecisions 响应形状冻结于 store.PendingDecision 行数组
// （updated_at 降序；空结果为 []）。Layer 2 前端唯一契约，不得静默变更。
func (s *Server) handlePendingDecisions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.ListPendingDecisions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取待决策列表失败")
		return
	}
	if rows == nil {
		rows = []store.PendingDecision{}
	}
	writeJSON(w, http.StatusOK, rows)
}
