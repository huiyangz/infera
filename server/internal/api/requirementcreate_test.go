package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/syncsvc"
	"github.com/tokfinity/infera/internal/tasksource"
)

var errBoom = errors.New("上游打雷")

// 本文件覆盖「创建需求」端点（L202608230412-1-T01 冻结契约）：
//
//	POST /api/projects/{id}/requirements
//	  body {title, description, status(backlog|todo), priority, auto_merge, agent_id}
//	  → 201 store.Delivery（同步侧已有数据形状）
//
// 行为语义（AC：默认 assignee=Tech Lead、autoMerge→auto label、状态/优先级
// 透传、项目缺省映射）经真 syncsvc.Creator + 假上游 client 在 HTTP 面验证。

// fakeUpstream 是 syncsvc.IssueCreator 的测试替身（api 侧看不见 syncsvc
// 包内替身，这里独立一份最小实现）。
type fakeUpstream struct {
	createIn       tasksource.CreateIssueInput
	createErr      error
	labels         []tasksource.Label
	addLabelCalled bool
}

func (f *fakeUpstream) CreateIssue(_ context.Context, in tasksource.CreateIssueInput) (tasksource.Issue, error) {
	f.createIn = in
	if f.createErr != nil {
		return tasksource.Issue{}, f.createErr
	}
	return tasksource.Issue{ID: "i-9", Identifier: "INFERA-9", Title: in.Title, Priority: in.Priority, Status: in.Status}, nil
}

func (f *fakeUpstream) ListLabels(context.Context) ([]tasksource.Label, error) {
	return f.labels, nil
}

func (f *fakeUpstream) AddIssueLabel(_ context.Context, _, _ string) error {
	f.addLabelCalled = true
	return nil
}

// reflowStub 模拟同步回流：把上游建出的卡落进 store（真同步的落库效果），
// 让响应走「同步读回」路径（infera 侧 id 非空）。
type reflowStub struct {
	st  store.Store
	pid string
}

func (r reflowStub) SyncNow(ctx context.Context) (syncsvc.Result, error) {
	now := time.Now().UTC()
	return syncsvc.Result{}, r.st.UpsertDeliveryByExternalID(ctx, &store.Delivery{
		ProjectID: r.pid, Title: "同步镜像", Status: "queued",
		ExternalIssueID: "i-9", ExternalIssueKey: "INFERA-9",
		Assignee: "agent:lead-1", ExternalSyncedAt: &now,
	})
}

// newCreateServer 装配测试服务器：种子项目（带上游映射）+ 真 Creator + 假上游。
// 返回 (服务器, 假上游, 种子项目 infera 侧 id)。
func newCreateServer(t *testing.T, mutate ...func(*fakeUpstream)) (*httptest.Server, *fakeUpstream, string) {
	t.Helper()
	st := store.NewMemory()
	p := &store.Project{Name: "自动闭环", ExternalProjectID: "ext-prj-1"}
	require.NoError(t, st.UpsertProjectByExternalID(context.Background(), p))

	up := &fakeUpstream{labels: []tasksource.Label{{ID: "lbl-auto", Name: "auto"}}}
	for _, m := range mutate {
		m(up)
	}
	creator, err := syncsvc.NewCreator(up, reflowStub{st: st, pid: p.ID}, st, syncsvc.CreatorOptions{TechLeadAgentID: "lead-1"})
	require.NoError(t, err)

	srv := NewServer(st, "secret-pass", nil)
	srv.SetRequirementCreator(creator)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, up, p.ID
}

// postCreate 登录后发一次创建请求。
func postCreate(t *testing.T, ts *httptest.Server, projID, body string) *http.Response {
	t.Helper()
	c := login(t, ts.URL)
	r, err := c.Post(ts.URL+"/api/projects/"+projID+"/requirements", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	return r
}

// TestCreateRequirementAuthAndAssembly：认证门与装配门——未登录 401；
// 未装配（TASK_SYNC_*/Tech Lead 未配置）503。
func TestCreateRequirementAuthAndAssembly(t *testing.T) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	ts := httptest.NewServer(srv.Mux())
	defer ts.Close()

	r, _ := http.Post(ts.URL+"/api/projects/00000000-0000-0000-0000-000000000000/requirements",
		"application/json", strings.NewReader(`{"title":"t"}`))
	require.Equal(t, 401, r.StatusCode)

	c := login(t, ts.URL)
	r, err := c.Post(ts.URL+"/api/projects/00000000-0000-0000-0000-000000000000/requirements",
		"application/json", strings.NewReader(`{"title":"t"}`))
	require.NoError(t, err)
	require.Equal(t, 503, r.StatusCode)
}

// TestCreateRequirementDefaults（AC 四件套）：默认 assignee=Tech Lead、
// 项目缺省映射、状态/优先级透传、响应为同步侧形状。
func TestCreateRequirementDefaults(t *testing.T) {
	ts, up, projID := newCreateServer(t)

	r := postCreate(t, ts, projID, `{"title":"新需求","description":"正文","status":"todo","priority":"high"}`)
	require.Equal(t, http.StatusCreated, r.StatusCode)

	// 上游载荷：四项 AC 全落在这一份断言里。
	require.Equal(t, "lead-1", up.createIn.AssigneeID, "默认 assignee 必须解析为 Tech Lead")
	require.Equal(t, "agent", up.createIn.AssigneeType)
	require.Equal(t, "ext-prj-1", up.createIn.ProjectID, "项目缺省走当前项目的上游映射")
	require.Equal(t, "todo", up.createIn.Status, "状态透传")
	require.Equal(t, "high", up.createIn.Priority, "优先级透传")

	// 响应：同步侧形状（store.Delivery 字段），且是读回行（infera id 非空）。
	var got store.Delivery
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	require.NotEmpty(t, got.ID)
	require.Equal(t, "i-9", got.ExternalIssueID)
	require.Equal(t, "INFERA-9", got.ExternalIssueKey)
	require.Equal(t, projID, got.ProjectID)
	require.False(t, up.addLabelCalled, "auto_merge 未开不得打标")
}

// TestCreateRequirementAutoMerge（AC）：auto_merge=true → 上游打 auto 标。
func TestCreateRequirementAutoMerge(t *testing.T) {
	ts, up, projID := newCreateServer(t)

	r := postCreate(t, ts, projID, `{"title":"自动合并","auto_merge":true}`)
	require.Equal(t, http.StatusCreated, r.StatusCode)
	require.True(t, up.addLabelCalled, "auto_merge=true 必须打 auto label")
}

// TestCreateRequirementStatusDefaultAndValidation：状态缺省 backlog；超出
// 两档 400；空标题 400。
func TestCreateRequirementStatusDefaultAndValidation(t *testing.T) {
	ts, up, projID := newCreateServer(t)

	r := postCreate(t, ts, projID, `{"title":"缺省状态"}`)
	require.Equal(t, http.StatusCreated, r.StatusCode)
	require.Equal(t, "backlog", up.createIn.Status, "状态缺省 backlog")

	for _, body := range []string{
		`{"title":"t","status":"done"}`,
		`{"title":"  "}`,
		`{not-json`,
	} {
		r := postCreate(t, ts, projID, body)
		require.Equal(t, http.StatusBadRequest, r.StatusCode, "body %s", body)
	}
}

// TestCreateRequirementProjectErrors：项目不存在 404；项目无上游映射 409。
func TestCreateRequirementProjectErrors(t *testing.T) {
	ts, _, _ := newCreateServer(t)

	r := postCreate(t, ts, "11111111-2222-3333-4444-555555555555", `{"title":"t"}`)
	require.Equal(t, http.StatusNotFound, r.StatusCode)

	// 纯本地项目（无映射）：单独装配一台服务器落种子。
	st := store.NewMemory()
	p := &store.Project{Name: "纯本地项目"}
	require.NoError(t, st.CreateProject(context.Background(), p))
	up := &fakeUpstream{}
	creator, err := syncsvc.NewCreator(up, reflowStub{st: st, pid: p.ID}, st, syncsvc.CreatorOptions{TechLeadAgentID: "lead-1"})
	require.NoError(t, err)
	srv := NewServer(st, "secret-pass", nil)
	srv.SetRequirementCreator(creator)
	ts2 := httptest.NewServer(srv.Mux())
	defer ts2.Close()
	c := login(t, ts2.URL)
	r, err = c.Post(ts2.URL+"/api/projects/"+p.ID+"/requirements", "application/json", strings.NewReader(`{"title":"t"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, r.StatusCode)
}

// TestCreateRequirementUpstreamFailure：上游建卡失败 → 502。
func TestCreateRequirementUpstreamFailure(t *testing.T) {
	ts, _, projID := newCreateServer(t, func(up *fakeUpstream) {
		up.createErr = errBoom
	})

	r := postCreate(t, ts, projID, `{"title":"t"}`)
	require.Equal(t, http.StatusBadGateway, r.StatusCode)
}
