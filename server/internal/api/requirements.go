// 需求流转面（INFERA-11 T05 / FR-2、FR-4、FR-5、FR-6、FR-8）：发起需求、
// 需求读取（大节点 + 待处理卡 + 深链）、卡片代理动作、审计查询、
// 项目级合并策略设置。全部走既有密码门 session 鉴权。
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/reqservice"
)

// RequirementsAPI 是 api 层对 reqservice 的最小依赖（*reqservice.Service
// 天然满足）。NewServer 允许 nil（未装配 → 需求路由 503），SetRequirements
// 在 main 里注入（与 SetEngine 同款后置注入模式）。
type RequirementsAPI interface {
	Create(ctx context.Context, in reqservice.CreateInput) (*reqservice.Requirement, error)
	List(ctx context.Context) ([]reqservice.RequirementListItem, error)
	Get(ctx context.Context, id string) (*reqservice.RequirementDetail, error)
	Approve(ctx context.Context, requirementID, cardID string) error
	Reject(ctx context.Context, requirementID, cardID, feedback string) error
	Decide(ctx context.Context, requirementID, cardID, choice, text string) error
	Merge(ctx context.Context, requirementID, cardID string) (github.MergeResult, error)
	Rework(ctx context.Context, requirementID, cardID, feedback string) error
	ListAudit(ctx context.Context, requirementID string) ([]reqservice.AuditEntry, error)
	GetMergePolicy(ctx context.Context, projectID string) (flow.MergePolicy, error)
	SetMergePolicy(ctx context.Context, projectID string, p flow.MergePolicy) (flow.MergePolicy, error)
}

// SetRequirements 注入需求编排服务（main 装配期调用）。
func (s *Server) SetRequirements(r RequirementsAPI) { s.req = r }

// requireReq 未装配时统一 503（需求路由组入口校验）。
func (s *Server) requireReq(w http.ResponseWriter) bool {
	if s.req == nil {
		writeError(w, http.StatusServiceUnavailable, "需求服务未装配")
		return false
	}
	return true
}

// writeReqErr 把 reqservice 错误映射为 HTTP：
// ErrInvalid→400；ErrNotFound→404；ErrConflict / ErrMergeBlocked→409
// （合并阻塞文案面向"稍后重试"）；其余（multica/github/db 故障）→502。
func writeReqErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, reqservice.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, reqservice.ErrNotFound):
		writeError(w, http.StatusNotFound, "资源不存在")
	case errors.Is(err, reqservice.ErrMergeBlocked):
		writeError(w, http.StatusConflict, "PR 当前不可合并（状态或分支保护阻止），请稍后重试")
	case errors.Is(err, reqservice.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadGateway, "上游服务暂时不可用")
	}
}

func (s *Server) handleCreateRequirement(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	var body struct {
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		AcceptanceCriteria string   `json:"acceptance_criteria"`
		Source             string   `json:"source"`
		Priority           string   `json:"priority"`
		Acceptors          []string `json:"acceptors"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	got, err := s.req.Create(r.Context(), reqservice.CreateInput{
		Title:              body.Title,
		Description:        body.Description,
		AcceptanceCriteria: body.AcceptanceCriteria,
		Source:             body.Source,
		Priority:           body.Priority,
		Acceptors:          body.Acceptors,
	})
	if err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, got)
}

func (s *Server) handleListRequirements(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	rows, err := s.req.List(r.Context())
	if err != nil {
		writeReqErr(w, err)
		return
	}
	if rows == nil {
		rows = []reqservice.RequirementListItem{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleGetRequirement(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	d, err := s.req.Get(r.Context(), id)
	if err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleCardAction 是评论类动作（approve / reject / decide / rework）的共享
// 形态：路径 (requirementID, cardID) 都是 UUID，body 各自解码。
func (s *Server) cardIDs(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	reqID := chi.URLParam(r, "id")
	cardID := chi.URLParam(r, "cardID")
	if !validID(w, reqID) || !validID(w, cardID) {
		return "", "", false
	}
	return reqID, cardID, true
}

func (s *Server) handleCardApprove(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	reqID, cardID, ok := s.cardIDs(w, r)
	if !ok {
		return
	}
	if err := s.req.Approve(r.Context(), reqID, cardID); err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCardReject(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	reqID, cardID, ok := s.cardIDs(w, r)
	if !ok {
		return
	}
	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if err := s.req.Reject(r.Context(), reqID, cardID, body.Feedback); err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCardDecide(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	reqID, cardID, ok := s.cardIDs(w, r)
	if !ok {
		return
	}
	var body struct {
		Choice string `json:"choice"`
		Text   string `json:"text"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if err := s.req.Decide(r.Context(), reqID, cardID, body.Choice, body.Text); err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCardMerge(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	reqID, cardID, ok := s.cardIDs(w, r)
	if !ok {
		return
	}
	res, err := s.req.Merge(r.Context(), reqID, cardID)
	if err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCardRework(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	reqID, cardID, ok := s.cardIDs(w, r)
	if !ok {
		return
	}
	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if err := s.req.Rework(r.Context(), reqID, cardID, body.Feedback); err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRequirementAudit(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	entries, err := s.req.ListAudit(r.Context(), id)
	if err != nil {
		writeReqErr(w, err)
		return
	}
	if entries == nil {
		entries = []reqservice.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleGetMergePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	p, err := s.req.GetMergePolicy(r.Context(), id)
	if err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mergePolicyJSON{Mode: p.Mode, DiffLineThreshold: p.DiffLineThreshold})
}

// mergePolicyJSON 是策略端点的响应形态（与 flow.MergePolicy 字段一致，
// 显式 JSON 标签冻结契约）。
type mergePolicyJSON struct {
	Mode              flow.MergePolicyMode `json:"mode"`
	DiffLineThreshold int                  `json:"diff_line_threshold"`
}

func (s *Server) handleSetMergePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.requireReq(w) {
		return
	}
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	var body mergePolicyJSON
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	p, err := s.req.SetMergePolicy(r.Context(), id, flow.MergePolicy{
		Mode:              body.Mode,
		DiffLineThreshold: body.DiffLineThreshold,
	})
	if err != nil {
		writeReqErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mergePolicyJSON{Mode: p.Mode, DiffLineThreshold: p.DiffLineThreshold})
}
