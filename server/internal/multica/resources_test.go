package multica

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListProjectResources：项目资源拉取（GET /api/projects/{id}/resources）。
// 响应面是 {"resources": [...], "total": N}（与 /api/projects 同款包裹，真机
// 实证）；resource_ref 按 resource_type 多态（github_repo 带 url，
// local_directory 带 local_path/execution_mode/daemon_id/label），统一结构体
// 按字段名松解码——Go 缺省忽略未知字段，两类条目共用 ResourceRef。
// 新端点同样必须走统一认证与 X-Workspace-Id 通道（坑1）。
func TestListProjectResources(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotWS string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"resources": [
				{
					"id":"r-1","project_id":"p-1","workspace_id":"ws-1",
					"resource_type":"github_repo",
					"resource_ref":{"url":"git@github.com:huiyangz/infera.git"},
					"label":"infera 仓库","position":1,
					"created_at":"2026-08-20T18:04:06+08:00","created_by":"m-1"
				},
				{
					"id":"r-2","project_id":"p-1","workspace_id":"ws-1",
					"resource_type":"local_directory",
					"resource_ref":{"label":"infera","daemon_id":"d-1","local_path":"/Users/x/tokfinity/infera","execution_mode":"worktree"},
					"label":null,"position":0,
					"created_at":"2026-08-20T18:04:06+08:00","created_by":"m-1"
				}
			],
			"total": 2
		}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	resources, err := c.ListProjectResources(context.Background(), "p-1")
	require.NoError(t, err)
	require.Equal(t, "GET", gotMethod)
	require.Equal(t, "/api/projects/p-1/resources", gotPath)
	require.Equal(t, "Bearer mul_t", gotAuth)
	require.Equal(t, "ws-1", gotWS, "X-Workspace-Id 头必须随新端点注入（坑1）")
	require.Len(t, resources, 2)

	first := resources[0]
	require.Equal(t, "r-1", first.ID)
	require.Equal(t, "p-1", first.ProjectID)
	require.Equal(t, "github_repo", first.ResourceType)
	require.Equal(t, "git@github.com:huiyangz/infera.git", first.Ref.URL)
	require.Empty(t, first.Ref.LocalPath, "github_repo 条目不带 local_path——字段面按资源类型多态")
	require.Equal(t, 1, first.Position)

	second := resources[1]
	require.Equal(t, "local_directory", second.ResourceType)
	require.Equal(t, "/Users/x/tokfinity/infera", second.Ref.LocalPath)
	require.Equal(t, "worktree", second.Ref.ExecutionMode)
	require.Equal(t, "d-1", second.Ref.DaemonID)
	require.Equal(t, "infera", second.Ref.Label)
	require.Empty(t, second.Ref.URL)
	require.Zero(t, second.Position)
}

// TestListProjectResourcesEmpty：项目无资源 → 空列表（非错误）——"解析不出绑定"
// 是合法同步状态，由消费方决定保留现值。
func TestListProjectResourcesEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resources":[],"total":0}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	resources, err := c.ListProjectResources(context.Background(), "p-none")
	require.NoError(t, err)
	require.Empty(t, resources)
}

// TestListProjectResourcesServerError：端点非 2xx → 错误带状态码上抛，
// 不得吞成空列表（空列表有"无绑定"语义，混淆即覆写规则失真）。
func TestListProjectResourcesServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"denied"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.ListProjectResources(context.Background(), "p-1")
	require.ErrorContains(t, err, "403")
	require.ErrorContains(t, err, "denied")
}
