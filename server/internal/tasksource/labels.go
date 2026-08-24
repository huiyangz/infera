package tasksource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Label 是 workspace 标签的最小字段面（GET /api/labels 元素；issue 载荷内嵌
// 的标签对象同形，INFERA-219 T02 对真实服务端实测均带 color hex 原值）。
// 创建编排只按 Name 找目标标签（如 auto）再拿 ID 打标；同步链路额外消费
// Color 落库"名称+颜色与上游一致"。
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ListLabels 拉取 workspace 标签列表（GET /api/labels）。
//
// 响应形状双形兼容：CLI 会消费本端点（本地 capture 实证），但裸数组还是
// {"labels": [...]} 包裹未裸验过——先按裸数组解、失败再按包裹解，任一命中
// 即用，不把形状赌注埋进运行期。
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/labels", nil, &raw); err != nil {
		return nil, err
	}
	var labels []Label
	if err := json.Unmarshal(raw, &labels); err == nil {
		return labels, nil
	}
	var wrapped struct {
		Labels []Label `json:"labels"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		return wrapped.Labels, nil
	}
	return nil, fmt.Errorf("tasksource: 解码 /api/labels 响应: 既非裸数组也非 {\"labels\":[...]} 包裹: %s", truncate(raw))
}

// AddIssueLabel 给 issue 打标（POST /api/issues/{id}/labels，载荷
// {"label_id": <uuid>}——官方 CLI `issue label add` 同款，capture 实证）。
// 响应体不消费：调用方只关心成败。
func (c *Client) AddIssueLabel(ctx context.Context, issueID, labelID string) error {
	body := struct {
		LabelID string `json:"label_id"`
	}{LabelID: labelID}
	return c.do(ctx, http.MethodPost, "/api/issues/"+issueID+"/labels", body, nil)
}

// truncate 截断原始响应用于错误信息（回显截断，沿用 do 的 512 上限风格）。
func truncate(raw json.RawMessage) string {
	const max = 256
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "…"
}
