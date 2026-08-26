// 任务描述编辑面（INFERA-298 冻结契约）：任务详情页描述编辑的 HTTP 入口。
// 语义归 syncsvc.Editor（上游优先：先写上游、本地随读回落库），本文件只做
// 路由、DTO 与错误码映射。
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/syncsvc"
)

// DescriptionEditorAPI 是 api 层对描述编辑编排的最小依赖（*syncsvc.Editor
// 天然满足）。NewServer 允许 nil（未装配 → 端点 503），SetDescriptionEditor
// 在 main 里后置注入（与 SetTaskSync / SetRequirementCreator 同款模式）。
type DescriptionEditorAPI interface {
	UpdateDeliveryDescription(ctx context.Context, deliveryID, description string) (store.Delivery, error)
}

// SetDescriptionEditor 注入描述编辑编排服务（main 装配期调用）。
func (s *Server) SetDescriptionEditor(e DescriptionEditorAPI) { s.descEditor = e }

// handleUpdateDeliveryDescription：PATCH /api/deliveries/{id}/description。
// 请求体 {"description": string}（空白/超长 400，上限见
// syncsvc.MaxDescriptionBytes）；响应 200 + deliveryJSON（与其余交付变更
// 端点同形，labels 恒为数组）。
func (s *Server) handleUpdateDeliveryDescription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	if s.descEditor == nil {
		writeError(w, http.StatusServiceUnavailable, "描述编辑未装配（需配置 TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）")
		return
	}
	var body struct {
		Description string `json:"description"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	d, err := s.descEditor.UpdateDeliveryDescription(r.Context(), id, body.Description)
	switch {
	case err == nil:
	case errors.Is(err, syncsvc.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, syncsvc.ErrNotMirrored):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "交付不存在")
		return
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "交付已被并发修改，请刷新后重试")
		return
	default:
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	dj, err := s.deliveryWithLabels(r, &d)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取标签失败")
		return
	}
	writeJSON(w, http.StatusOK, dj)
}
