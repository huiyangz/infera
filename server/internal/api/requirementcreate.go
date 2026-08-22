// 创建需求面（L202608230412-1-T01 冻结契约）：项目任务列表/详情页
// 「新建需求」的 HTTP 入口。语义归 syncsvc.Creator（映射解析/缺省智能体/
// auto label/回流），本文件只做路由、DTO 与错误码映射。
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/syncsvc"
)

// RequirementCreatorAPI 是 api 层对创建编排的最小依赖（*syncsvc.Creator
// 天然满足）。NewServer 允许 nil（未装配 → 端点 503），SetRequirementCreator
// 在 main 里注入（与 SetTaskSync 同款后置注入模式）。
type RequirementCreatorAPI interface {
	CreateProjectRequirement(ctx context.Context, projectID string, in syncsvc.CreateRequirementInput) (store.Delivery, error)
}

// SetRequirementCreator 注入需求创建编排服务（main 装配期调用）。
func (s *Server) SetRequirementCreator(c RequirementCreatorAPI) { s.reqCreator = c }

// handleCreateProjectRequirement：POST /api/projects/{id}/requirements。
// 请求体字段即冻结 DTO：title / description / status（backlog|todo，缺省
// backlog）/ priority / auto_merge / agent_id（缺省 Tech Lead）。
// 响应 201 + 同步侧已有数据形状（store.Delivery）。
func (s *Server) handleCreateProjectRequirement(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	if s.reqCreator == nil {
		writeError(w, http.StatusServiceUnavailable, "需求创建未装配（需配置 TASK_SYNC_* 与 TASK_SYNC_TECH_LEAD_AGENT_ID）")
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
		Priority    string `json:"priority"`
		AutoMerge   bool   `json:"auto_merge"`
		AgentID     string `json:"agent_id"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	got, err := s.reqCreator.CreateProjectRequirement(r.Context(), projectID, syncsvc.CreateRequirementInput{
		Title:       body.Title,
		Description: body.Description,
		Status:      body.Status,
		Priority:    body.Priority,
		AgentID:     body.AgentID,
		AutoMerge:   body.AutoMerge,
	})
	switch {
	case err == nil:
	case errors.Is(err, syncsvc.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, syncsvc.ErrProjectNotMapped):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "项目不存在")
		return
	default:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, got)
}
