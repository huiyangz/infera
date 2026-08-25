// 待人工决策需求列表（INFERA-108 T01）：跨项目取全部停在审批门的需求，
// 供前端「需要决策」路由直接渲染；行内 id 即需求详情跳转键。
package api

import (
	"context"
	"net/http"

	"github.com/tokfinity/infera/internal/store"
)

// handlePendingDecisions 响应形状冻结于 store.PendingDecision 行数组
// （updated_at 降序；空结果为 []）。Layer 2 前端唯一契约，不得静默变更。
// INFERA-267 加法扩展：行新增序列化键仅 source（''=无来源/不可解析，
// 前端回退 —），取值为源头需求的 requirements.source——store 行带链根
// external_issue_id（json:"-" 不上线），此处经需求服务解析回填；未装配
// 或读取失败整体降级为 ''，决策页可用性不挂在需求服务上。
func (s *Server) handlePendingDecisions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.ListPendingDecisions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取待决策列表失败")
		return
	}
	if rows == nil {
		rows = []store.PendingDecision{}
	}
	s.fillDecisionSources(r.Context(), rows)
	writeJSON(w, http.StatusOK, rows)
}

// fillDecisionSources 回填决策行来源（INFERA-267）：链根 external_issue_id ↔
// requirements.external_issue_id 取源头需求 source（复用需求服务 List 行，
// 零新接口面）。链根非同步来源或无匹配需求行 → 保持 ''。
func (s *Server) fillDecisionSources(ctx context.Context, rows []store.PendingDecision) {
	if s.req == nil {
		return
	}
	reqs, err := s.req.List(ctx)
	if err != nil {
		return
	}
	byExt := make(map[string]string, len(reqs))
	for _, rq := range reqs {
		if rq.ExternalIssueID != "" {
			byExt[rq.ExternalIssueID] = rq.Source
		}
	}
	for i := range rows {
		rows[i].Source = byExt[rows[i].RootExternalIssueID]
	}
}
