package tasksource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// 本文件覆盖 labels 面（L202608230412-1-T01，本地 capture 实证——官方 CLI
// `issue label add <issue-id> <label-id>` / `label list` 的同一协议）：
//
//	GET  /api/labels                     → workspace 标签列表
//	POST /api/issues/{id}/labels         → 载荷 {"label_id": <uuid>}
//
// 新端点同样必须走统一认证与 X-Workspace-Id 注入通道（坑1）。

func TestListLabels(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotWS string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"lbl-auto","name":"auto"},
			{"id":"lbl-bug","name":"bug"}
		]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	labels, err := c.ListLabels(context.Background())
	require.NoError(t, err)
	require.Equal(t, "GET", gotMethod)
	require.Equal(t, "/api/labels", gotPath)
	require.Equal(t, "Bearer mul_t", gotAuth)
	require.Equal(t, "ws-1", gotWS, "X-Workspace-Id 头必须随新端点注入（坑1）")
	require.Len(t, labels, 2)
	require.Equal(t, "lbl-auto", labels[0].ID)
	require.Equal(t, "auto", labels[0].Name)
}

// TestListLabelsDecodesColor：标签库元素的颜色解码（INFERA-219 T02，对真实
// 服务端实测：GET /api/labels 的元素带 color hex 原值，如 #22c55e）。裸数组与
// 包裹两形都要解出颜色——同步链路按它落库"名称+颜色与 Multica 一致"。
func TestListLabelsDecodesColor(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		id    string
		color string
	}{
		{"裸数组", `[{"id":"lbl-auto","name":"auto","color":"#22c55e"}]`, "lbl-auto", "#22c55e"},
		{"包裹", `{"labels":[{"id":"lbl-cand","name":"候选","color":"#a855f7"}],"total":1}`, "lbl-cand", "#a855f7"},
	}
	for _, tc := range cases {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(tc.body))
		}))
		c, err := New(ts.URL, "mul_t", "ws-1")
		require.NoError(t, err)

		labels, err := c.ListLabels(context.Background())
		require.NoError(t, err, tc.name)
		require.Len(t, labels, 1, tc.name)
		require.Equal(t, tc.id, labels[0].ID, tc.name)
		require.Equal(t, tc.color, labels[0].Color, "%s：color hex 原值必须解出", tc.name)
	}
}

// TestListLabelsWrappedShape：标签列表的裸数组/包裹双形兼容。CLI 会消费
// GET /api/labels（capture 实证），但服务端裸数组还是 {"labels":[...]}
// 包裹未在本机裸验过——按两种形状都解，任一命中即用，避免形状赌注。
func TestListLabelsWrappedShape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"labels":[{"id":"lbl-auto","name":"auto"}],"total":1}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	labels, err := c.ListLabels(context.Background())
	require.NoError(t, err)
	require.Len(t, labels, 1)
	require.Equal(t, "lbl-auto", labels[0].ID)
	require.Equal(t, "auto", labels[0].Name)
}

// TestAddIssueLabel：issue 打标（POST /api/issues/{id}/labels，载荷
// {"label_id": <uuid>}，capture 实证）。响应体不消费——创建编排只关心成败。
func TestAddIssueLabel(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotWS string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		decodeBody(t, r, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	require.NoError(t, c.AddIssueLabel(context.Background(), "i-1", "lbl-auto"))
	require.Equal(t, "POST", gotMethod)
	require.Equal(t, "/api/issues/i-1/labels", gotPath)
	require.Equal(t, "Bearer mul_t", gotAuth)
	require.Equal(t, "ws-1", gotWS, "X-Workspace-Id 头必须随新端点注入（坑1）")
	require.Equal(t, "lbl-auto", gotBody["label_id"])
}
