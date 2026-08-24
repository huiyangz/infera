// 标签面（INFERA-218 T01 冻结契约）：
//   - GET    /api/labels                        标签库列表
//   - POST   /api/deliveries/{id}/labels        挂标签   body {"label_id": uuid}
//   - DELETE /api/deliveries/{id}/labels/{labelID}  摘标签
//
// 交付响应里的 labels 字段形状在此冻结：[{name, color}]——name 是标签名、
// color 是上游 hex 原值（如 #22c55e），不带内部 id。后续层（同步摄取 T02 /
// 前端展示 T03）读本文件对齐，不得另立平行字段。
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
)

// labelJSON 交付响应里单个标签的形状（冻结：只有 name + color）。
type labelJSON struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// labelsJSON 把 store 的标签行投影为冻结契约形状（按 name 升序保持查询序）。
func labelsJSON(ls []store.Label) []labelJSON {
	out := make([]labelJSON, 0, len(ls))
	for _, l := range ls {
		out = append(out, labelJSON{Name: l.Name, Color: l.Color})
	}
	return out
}

// deliveryJSON 交付对象 + 挂的标签：内嵌 store.Delivery 使既有字段保持平铺
// （对旧客户端只增不改），labels 恒为数组（未挂 = 空数组）。
type deliveryJSON struct {
	store.Delivery
	Labels []labelJSON `json:"labels"`
}

// deliveryWithLabels 装配单个交付的响应形态；读标签失败返回错误（不静默按
// 无标签降级——标签读不出与标签被摘掉对调用方是两种事实）。
func (s *Server) deliveryWithLabels(r *http.Request, d *store.Delivery) (deliveryJSON, error) {
	ls, err := s.st.ListDeliveryLabels(r.Context(), d.ID)
	if err != nil {
		return deliveryJSON{}, err
	}
	return deliveryJSON{Delivery: *d, Labels: labelsJSON(ls)}, nil
}

// handleListLabels 标签库列表（全库，按 name 升序）。
func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := s.st.ListLabels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取标签库失败")
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

// handleAttachDeliveryLabel 给交付挂标签（幂等：重复挂不产生重复关联），
// 返回挂标后的完整标签清单。
func (s *Server) handleAttachDeliveryLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	var body struct {
		LabelID string `json:"label_id"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if !validID(w, body.LabelID) {
		return
	}
	if _, err := s.st.GetDelivery(r.Context(), id); err != nil {
		writeStoreErr(w, err, "交付不存在", "读取交付失败")
		return
	}
	if err := s.st.AttachLabel(r.Context(), id, body.LabelID); err != nil {
		writeStoreErr(w, err, "交付或标签不存在", "挂标签失败")
		return
	}
	s.writeDeliveryLabels(w, r, id)
}

// handleDetachDeliveryLabel 摘除交付的标签，返回摘除后的标签清单。
func (s *Server) handleDetachDeliveryLabel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	labelID := chi.URLParam(r, "labelID")
	if !validID(w, id) || !validID(w, labelID) {
		return
	}
	if err := s.st.DetachLabel(r.Context(), id, labelID); err != nil {
		writeStoreErr(w, err, "交付未挂该标签", "摘标签失败")
		return
	}
	s.writeDeliveryLabels(w, r, id)
}

// writeDeliveryLabels 写回交付当前标签清单（挂/摘端点共用响应形态）。
func (s *Server) writeDeliveryLabels(w http.ResponseWriter, r *http.Request, deliveryID string) {
	labels, err := s.st.ListDeliveryLabels(r.Context(), deliveryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取标签失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labelsJSON(labels)})
}
