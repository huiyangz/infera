package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/syncsvc"
	"github.com/tokfinity/infera/internal/tasksource"
)

func ptr[T any](v T) *T { return &v }

// stubSync 是 TaskSyncAPI 的测试替身：可注入运行结果与错误。
type stubSync struct {
	res syncsvc.Result
	err error

	running bool
	last    *syncsvc.Result
	status  syncsvc.Status
}

func (s *stubSync) SyncNow(context.Context) (syncsvc.Result, error) { return s.res, s.err }
func (s *stubSync) Running() bool                                   { return s.running }
func (s *stubSync) Last() *syncsvc.Result                           { return s.last }
func (s *stubSync) Status() syncsvc.Status                          { return s.status }

// newSyncServer 建一个带（可选）同步装配的测试服务器。
func newSyncServer(t *testing.T, svc TaskSyncAPI) *httptest.Server {
	t.Helper()
	srv := NewServer(store.NewMemory(), "secret-pass", nil)
	if svc != nil {
		srv.SetTaskSync(svc)
	}
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts
}

// --- 认证门：同步面在登录组内 ---

func TestTaskSyncRequiresAuth(t *testing.T) {
	ts := newSyncServer(t, &stubSync{})
	r, _ := http.Post(ts.URL+"/api/task-sync", "application/json", nil)
	require.Equal(t, 401, r.StatusCode)
	r, _ = http.Get(ts.URL + "/api/task-sync")
	require.Equal(t, 401, r.StatusCode)
}

// --- 未装配（TASK_SYNC_* 未配置）：统一 503，同需求路由 ---

func TestTaskSyncUnconfiguredServiceUnavailable(t *testing.T) {
	ts := newSyncServer(t, nil)
	c := login(t, ts.URL)
	r, _ := c.Post(ts.URL+"/api/task-sync", "application/json", nil)
	require.Equal(t, 503, r.StatusCode)
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Equal(t, "unavailable", body.Code)

	r, _ = c.Get(ts.URL + "/api/task-sync")
	require.Equal(t, 503, r.StatusCode)
	r, _ = c.Get(ts.URL + "/api/task-sync/status")
	require.Equal(t, 503, r.StatusCode)
}

// --- GET /api/task-sync/status：冻结契约 lastSyncAt / status / error ---

func TestTaskSyncStatusContract(t *testing.T) {
	ts := newSyncServer(t, &stubSync{status: syncsvc.Status{
		Status: syncsvc.StatusSuccess, Error: "",
	}})
	c := login(t, ts.URL)
	r, err := c.Get(ts.URL + "/api/task-sync/status")
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)

	// 字段名逐字校验：契约由本任务冻结，拼错即破坏前端对接。
	var body map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	for _, key := range []string{"lastSyncAt", "status", "error"} {
		require.Contains(t, body, key, "响应必须含字段 %q", key)
	}
}

func TestTaskSyncStatusNeverSyncedIsIdle(t *testing.T) {
	ts := newSyncServer(t, syncsvc.New(&e2eSyncFetch{}, store.NewMemory()))
	c := login(t, ts.URL)
	r, err := c.Get(ts.URL + "/api/task-sync/status")
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var st syncsvc.Status
	require.NoError(t, json.NewDecoder(r.Body).Decode(&st))
	require.Equal(t, syncsvc.StatusIdle, st.Status)
	require.Nil(t, st.LastSyncAt)
	require.Empty(t, st.Error)
}

func TestTaskSyncStatusEndToEndSuccessAndFailure(t *testing.T) {
	// 成功：同步一轮后 lastSyncAt 更新、status=success。
	st := store.NewMemory()
	ts := newSyncServer(t, syncsvc.New(&e2eSyncFetch{
		projects: []tasksource.Project{{ID: "m-prj-1", Title: "自动闭环"}},
	}, st))
	c := login(t, ts.URL)
	_, err := c.Post(ts.URL+"/api/task-sync", "application/json", nil)
	require.NoError(t, err)

	r, err := c.Get(ts.URL + "/api/task-sync/status")
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var got syncsvc.Status
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	require.Equal(t, syncsvc.StatusSuccess, got.Status)
	require.NotNil(t, got.LastSyncAt, "成功后 lastSyncAt 更新")
	require.Empty(t, got.Error)

	// 失败：上游错误 → POST 502，但服务不崩，status 面如实反映 error。
	ts2 := newSyncServer(t, syncsvc.New(errFetch{err: errors.New("upstream 500")}, store.NewMemory()))
	c2 := login(t, ts2.URL)
	r2, err := c2.Post(ts2.URL+"/api/task-sync", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, 502, r2.StatusCode)

	r3, err := c2.Get(ts2.URL + "/api/task-sync/status")
	require.NoError(t, err)
	require.Equal(t, 200, r3.StatusCode, "同步失败后状态接口必须仍可用（服务不崩）")
	var got2 syncsvc.Status
	require.NoError(t, json.NewDecoder(r3.Body).Decode(&got2))
	require.Equal(t, syncsvc.StatusError, got2.Status)
	require.Contains(t, got2.Error, "upstream 500")
}

// errFetch 恒失败的拉取面替身。
type errFetch struct{ err error }

func (f errFetch) ListProjects(context.Context) ([]tasksource.Project, error) { return nil, f.err }
func (f errFetch) ListIssues(context.Context) ([]tasksource.Issue, error)     { return nil, f.err }
func (f errFetch) ListProjectResources(_ context.Context, _ string) ([]tasksource.ProjectResource, error) {
	return nil, f.err
}

// --- POST 触发：返回本轮 Result；GET：running + last 形状 ---

func TestTaskSyncTriggerAndStatus(t *testing.T) {
	res := syncsvc.Result{
		ProjectsImported: 1,
		IssuesImported:   2,
		IssuesSkipped:    1,
		Skips:            []syncsvc.Skip{{ExternalIssueID: "m-smoke", IssueKey: "INFERA-90", Reason: "smoke"}},
	}
	ts := newSyncServer(t, &stubSync{res: res, last: &res})
	c := login(t, ts.URL)

	r, err := c.Post(ts.URL+"/api/task-sync", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var got syncsvc.Result
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	require.Equal(t, 1, got.ProjectsImported)
	require.Equal(t, 2, got.IssuesImported)
	require.Equal(t, 1, got.IssuesSkipped)
	require.Len(t, got.Skips, 1)
	require.Equal(t, "smoke", got.Skips[0].Reason)

	r, err = c.Get(ts.URL + "/api/task-sync")
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var status struct {
		Running bool            `json:"running"`
		Last    *syncsvc.Result `json:"last"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&status))
	require.False(t, status.Running)
	require.NotNil(t, status.Last)
	require.Equal(t, 2, status.Last.IssuesImported)
}

// --- GET 从未同步过：last 为 null ---

func TestTaskSyncStatusNeverRan(t *testing.T) {
	ts := newSyncServer(t, &stubSync{})
	c := login(t, ts.URL)
	r, err := c.Get(ts.URL + "/api/task-sync")
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var status struct {
		Running bool            `json:"running"`
		Last    json.RawMessage `json:"last"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&status))
	require.False(t, status.Running)
	require.Equal(t, "null", string(status.Last), "从未同步过 last 为 JSON null")
}

// --- 错误映射：运行中 → 409；上游失败 → 502 ---

func TestTaskSyncErrorMapping(t *testing.T) {
	ts := newSyncServer(t, &stubSync{err: syncsvc.ErrSyncRunning, running: true})
	c := login(t, ts.URL)
	r, _ := c.Post(ts.URL+"/api/task-sync", "application/json", nil)
	require.Equal(t, 409, r.StatusCode)
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Equal(t, "conflict", body.Code)

	ts2 := newSyncServer(t, &stubSync{err: errors.New("tasksource: HTTP 401")})
	c2 := login(t, ts2.URL)
	r, _ = c2.Post(ts2.URL+"/api/task-sync", "application/json", nil)
	require.Equal(t, 502, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Equal(t, "bad_gateway", body.Code)
}

// --- 端到端：真 Service（fake 拉取面）经 HTTP 触发落库 ---

// e2eSyncFetch 实现 syncsvc.Fetcher 的最小拉取面替身（上游原始形状）。
type e2eSyncFetch struct {
	projects []tasksource.Project
	issues   []tasksource.Issue
}

func (f *e2eSyncFetch) ListProjects(context.Context) ([]tasksource.Project, error) {
	return f.projects, nil
}
func (f *e2eSyncFetch) ListIssues(context.Context) ([]tasksource.Issue, error) { return f.issues, nil }
func (f *e2eSyncFetch) ListProjectResources(_ context.Context, _ string) ([]tasksource.ProjectResource, error) {
	return nil, nil // 资源面默认无绑定（repo_url 保留现值）
}

func TestTaskSyncEndToEnd(t *testing.T) {
	st := store.NewMemory()
	f := &e2eSyncFetch{
		projects: []tasksource.Project{{ID: "m-prj-1", Title: "自动闭环"}},
		issues:   []tasksource.Issue{{ID: "m-iss-1", Identifier: "INFERA-1", Title: "需求", Status: "todo", ProjectID: ptr("m-prj-1")}},
	}
	svc := syncsvc.New(f, st)
	ts := newSyncServer(t, svc)
	c := login(t, ts.URL)

	r, err := c.Post(ts.URL+"/api/task-sync", "application/json", nil)
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var got syncsvc.Result
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	require.Equal(t, 1, got.ProjectsImported)
	require.Equal(t, 1, got.IssuesImported)

	projs, err := st.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, projs, 1)
	require.Equal(t, "m-prj-1", projs[0].ExternalProjectID)
}
