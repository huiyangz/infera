package tasksource

import (
	"context"
	"net/http"
)

// ProjectResource 是项目资源条目的最小字段面（GET /api/projects/{id}/resources
// 元素，真机实证）。resource_ref 按 resource_type 多态：github_repo 带 url，
// local_directory 带 local_path/execution_mode/daemon_id/label——两类共用
// ResourceRef 结构体按字段名松解码（Go 缺省忽略未知字段），不用的字段为零值。
type ProjectResource struct {
	ID           string      `json:"id"`
	ProjectID    string      `json:"project_id"`
	ResourceType string      `json:"resource_type"` // github_repo | local_directory
	Ref          ResourceRef `json:"resource_ref"`
	Position     int         `json:"position"` // 同类型多条时的择一序（小者胜）
}

// ResourceRef 是资源载荷面：github_repo 消费 URL；local_directory 消费
// LocalPath（git 可克隆本地路径）。Label 两类都可能有，仅展示用。
type ResourceRef struct {
	URL           string `json:"url"`            // github_repo：仓库地址
	LocalPath     string `json:"local_path"`     // local_directory：本地路径
	ExecutionMode string `json:"execution_mode"` // local_directory：worktree 等（infera 侧不消费，透传保真）
	DaemonID      string `json:"daemon_id"`      // local_directory：守护进程 id（同上）
	Label         string `json:"label"`          // 展示名
}

// ListProjectResources 拉取单个项目的资源列表（GET /api/projects/{id}/resources）。
//
// 该端点与 /api/projects 同款包裹（{"resources": [...], "total": N}）且无分页
// ——资源是项目的少量附属数据，一次响应返回全量，客户端不翻页、不发明分页。
func (c *Client) ListProjectResources(ctx context.Context, projectID string) ([]ProjectResource, error) {
	var resp struct {
		Resources []ProjectResource `json:"resources"`
		Total     int               `json:"total"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/projects/"+projectID+"/resources", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Resources, nil
}
