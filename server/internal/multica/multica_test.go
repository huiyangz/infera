package multica

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewValidation：坑1 + 坑4 的入口防线。
// 坑1：X-Workspace-Id 隐式必需——workspace 缺失必须在构造期报清晰错误，不能等到 400。
// 坑4：BaseURL 必须显式配置、不内置默认值；指向云端 multica.ai 的误配要能检出来。
func TestNewValidation(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		token   string
		wsID    string
		wantErr string
	}{
		{"BaseURL 为空（坑4：不内置默认值）", "", "mul_t", "ws-1", "ServerURL 必须显式配置"},
		{"BaseURL 指向云端（坑4：误配可检出）", "https://api.multica.ai", "mul_t", "ws-1", "云端"},
		{"BaseURL 非 http(s) scheme", "ftp://localhost:8088", "mul_t", "ws-1", "http"},
		{"Token 缺失", "http://localhost:8088", "", "ws-1", "Token"},
		{"WorkspaceID 缺失（坑1）", "http://localhost:8088", "mul_t", "", "WorkspaceID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.baseURL, tc.token, tc.wsID)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}

	t.Run("合法参数构造成功", func(t *testing.T) {
		_, err := New("http://localhost:8088/", "mul_t", "ws-1")
		require.NoError(t, err)
	})
}

// TestRequestHeadersInjected：每个请求统一注入 Bearer 认证与隐式必需的
// X-Workspace-Id 头（坑1）——CLI 不带 workspace 进 body 也能建卡，全靠这个头。
func TestRequestHeadersInjected(t *testing.T) {
	var gotPath string
	var gotAuth, gotWS, gotCT string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_test-token", "ws-uuid-1")
	require.NoError(t, err)

	comments, err := c.ListComments(context.Background(), "issue-1")
	require.NoError(t, err)
	require.Empty(t, comments)

	require.Equal(t, "/api/issues/issue-1/comments", gotPath)
	require.Equal(t, "Bearer mul_test-token", gotAuth)
	require.Equal(t, "ws-uuid-1", gotWS, "X-Workspace-Id 头必须随每个请求注入（坑1）")
	require.Equal(t, "application/json", gotCT)
}

// TestNon2xxError：非 2xx 返回带状态码与截断响应体的错误（沿用 agent.HTTPRunner 风格）。
func TestNon2xxError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"workspace_id or workspace_slug is required"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.ListComments(context.Background(), "issue-1")
	require.ErrorContains(t, err, "400")
	require.ErrorContains(t, err, "workspace_id or workspace_slug is required")
}

// TestListCommentsDecodesFields：评论字段解码（产物拉取的最小面）。
func TestListCommentsDecodesFields(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"author_type":"agent","author_id":"a-1","content":"e2e-ok 产物","created_at":"2026-08-21T00:00:00Z"},
			{"author_type":"member","author_id":"m-1","content":"人类补充","created_at":"2026-08-21T01:00:00Z"}
		]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	comments, err := c.ListComments(context.Background(), "issue-1")
	require.NoError(t, err)
	require.Len(t, comments, 2)
	require.Equal(t, "agent", comments[0].AuthorType)
	require.Equal(t, "e2e-ok 产物", comments[0].Content)
	require.Equal(t, "member", comments[1].AuthorType)
}

// decodeBody 是测试侧的哑解码助手（仅为让 handler 读取请求体用）。
func decodeBody(t *testing.T, r *http.Request, into any) {
	t.Helper()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(into); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}
