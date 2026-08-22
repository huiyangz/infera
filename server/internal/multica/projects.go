package multica

import (
	"context"
	"net/http"
	"time"
)

// Project 是拉取面消费的最小字段面（GET /api/projects 元素）。
// 可空字段按指针保真（未填 = nil），归一交给映射层 MapProject。
type Project struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description *string   `json:"description"` // 可空：未填为 null
	Status      string    `json:"status"`      // planned/in_progress/paused/completed/cancelled
	Priority    string    `json:"priority"`    // urgent/high/medium/low/none
	LeadType    *string   `json:"lead_type"`   // 负责人类型（member|agent），可空
	LeadID      *string   `json:"lead_id"`     // 负责人 id，可空
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListProjects 拉取当前 workspace 全部项目（GET /api/projects）。
//
// 该端点无分页：服务端 ListProjects 不带 limit/offset，一次响应返回全量
// （multica-src 实证）——因此客户端不翻页，也不为其发明分页机制；若未来
// 服务端引入分页，此处需按 ListIssues 的翻页协议改造。
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var resp struct {
		Projects []Project `json:"projects"`
		Total    int       `json:"total"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/projects", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Projects, nil
}
