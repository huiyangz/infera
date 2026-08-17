package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Memory) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, st
}

// login 返回带 cookie jar 的 client（模拟浏览器存 session cookie）。
// 注意：http.Client{} 无 jar 时会丢弃 Set-Cookie，cookie 会话无法通过。
func login(t *testing.T, base string) *http.Client {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}
	r, err := client.Post(base+"/api/login", "application/json",
		bytes.NewBufferString(`{"password":"secret-pass"}`))
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	return client
}

func TestAuthGate(t *testing.T) {
	ts, _ := newServer(t)
	r, _ := http.Get(ts.URL + "/api/projects")
	require.Equal(t, 401, r.StatusCode)

	r, _ = http.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"wrong"}`))
	require.Equal(t, 401, r.StatusCode)

	c := login(t, ts.URL)
	r, _ = c.Get(ts.URL + "/api/projects")
	require.Equal(t, 200, r.StatusCode)

	var me struct {
		LoggedIn bool `json:"logged_in"`
	}
	r, _ = c.Get(ts.URL + "/api/me")
	require.NoError(t, json.NewDecoder(r.Body).Decode(&me))
	require.True(t, me.LoggedIn)
}

func TestProjectsPinnedAndStats(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	r, _ := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"demo","repo_url":"","default_branch":"main"}`))
	require.Equal(t, 200, r.StatusCode)
	var p store.Project
	require.NoError(t, json.NewDecoder(r.Body).Decode(&p))

	req, _ := http.NewRequest("PATCH", ts.URL+"/api/projects/"+p.ID, bytes.NewBufferString(`{"pinned":true}`))
	req.Header.Set("Content-Type", "application/json")
	r, _ = c.Do(req)
	require.Equal(t, 200, r.StatusCode)
	var patched store.Project
	require.NoError(t, json.NewDecoder(r.Body).Decode(&patched))
	require.True(t, patched.Pinned)

	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{ProjectID: p.ID, Title: "x", Status: "active", PendingGate: "spec_approval"}))

	r, _ = c.Get(ts.URL + "/api/projects?include=stats")
	var list []struct {
		store.Project
		Stats *store.ProjectStats `json:"stats"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&list))
	require.Len(t, list, 1)
	require.True(t, list[0].Pinned)
	require.NotNil(t, list[0].Stats)
	require.Equal(t, 1, list[0].Stats.Active)
	require.Equal(t, 1, list[0].Stats.Pending)

	// 404
	r, _ = c.Get(ts.URL + "/api/projects/00000000-0000-0000-0000-000000000000")
	require.Equal(t, 404, r.StatusCode)
}

// --- deliveries ---

// fakeEngine 记录调用但不改 store（真实引擎会推进状态），
// 因此断言只依赖 handler 自身行为；内部加锁避免异步 driver 的数据竞争。
type fakeEngine struct {
	mu        sync.Mutex
	started   []string
	continued []string
	approved  []string
	rejected  []string
	failStart bool
}

func (f *fakeEngine) Start(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failStart {
		return errors.New("engine boom")
	}
	f.started = append(f.started, id)
	return nil
}

func (f *fakeEngine) Continue(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.continued = append(f.continued, id)
	return nil
}

func (f *fakeEngine) Approve(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved = append(f.approved, id)
	return nil
}

func (f *fakeEngine) Reject(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected = append(f.rejected, id)
	return nil
}

func (f *fakeEngine) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

func (f *fakeEngine) startedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.started...)
}

func (f *fakeEngine) continuedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.continued...)
}

func (f *fakeEngine) approvedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.approved...)
}

func (f *fakeEngine) rejectedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.rejected...)
}

func newServerWithEngine(t *testing.T) (*httptest.Server, *store.Memory, *fakeEngine) {
	st := store.NewMemory()
	fe := &fakeEngine{}
	srv := NewServer(st, "secret-pass", fe)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, st, fe
}

func TestDeliveryLifecycleAPI(t *testing.T) {
	ts, st, fe := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "p"}
	require.NoError(t, st.CreateProject(ctx, p))

	r, _ := c.Post(ts.URL+"/api/projects/"+p.ID+"/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"需求A","description":"描述"}`))
	require.Equal(t, 200, r.StatusCode)
	var d store.Delivery
	require.NoError(t, json.NewDecoder(r.Body).Decode(&d))
	require.Equal(t, "intake", d.CurrentStage)
	require.Equal(t, "active", d.Status)
	require.NotEmpty(t, d.ID)
	// 引擎被异步触发（至少一次 Start）
	require.Eventually(t, func() bool { return fe.startCount() >= 1 }, 2*time.Second, 20*time.Millisecond)

	// 列表
	r, _ = c.Get(ts.URL + "/api/projects/" + p.ID + "/deliveries")
	require.Equal(t, 200, r.StatusCode)
	var list []store.Delivery
	require.NoError(t, json.NewDecoder(r.Body).Decode(&list))
	require.Len(t, list, 1)
	require.Equal(t, "需求A", list[0].Title)

	// 尚无门禁 → gate 400
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 400, r.StatusCode)

	// 引擎跑到 spec 门禁（直接改 store 模拟引擎推进）
	got, _ := st.GetDelivery(ctx, d.ID)
	got.CurrentStage, got.PendingGate = "spec_approval", "spec_approval"
	require.NoError(t, st.UpdateDelivery(ctx, got))
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "# spec 正文"}))
	require.NoError(t, st.AppendEvent(ctx, &store.Event{DeliveryID: d.ID, Stage: "spec", EventType: "stage_done", Payload: []byte(`{}`)}))

	// 详情：delivery + timeline + artifacts
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID)
	require.Equal(t, 200, r.StatusCode)
	var detail struct {
		Delivery  store.Delivery   `json:"delivery"`
		Timeline  []store.Event    `json:"timeline"`
		Artifacts []store.Artifact `json:"artifacts"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&detail))
	require.Equal(t, "spec_approval", detail.Delivery.PendingGate)
	require.Len(t, detail.Timeline, 2) // delivery_created（handler 记录）+ stage_done（模拟引擎）
	require.Equal(t, "delivery_created", detail.Timeline[0].EventType)
	require.Equal(t, "stage_done", detail.Timeline[1].EventType)
	require.Len(t, detail.Artifacts, 1)

	// gate：spec 全文
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
	var gate struct {
		DeliveryID  string `json:"delivery_id"`
		Gate        string `json:"gate"`
		AgentOutput *struct {
			Agent  string `json:"agent"`
			Output string `json:"output"`
		} `json:"agent_output"`
		PRURL string `json:"pr_url"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&gate))
	require.Equal(t, d.ID, gate.DeliveryID)
	require.Equal(t, "spec_approval", gate.Gate)
	require.NotNil(t, gate.AgentOutput)
	require.Equal(t, "spec", gate.AgentOutput.Agent)
	require.Contains(t, gate.AgentOutput.Output, "spec 正文")

	// approve：fake 不改 store，只记录调用并返回 200（真实引擎会清 gate 并继续推进）
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/approve", "", nil)
	require.Equal(t, 200, r.StatusCode)
	require.Equal(t, []string{d.ID}, fe.approvedIDs())

	// 推进到 code_review 门禁再 reject
	got, _ = st.GetDelivery(ctx, d.ID)
	got.CurrentStage, got.PendingGate = "code_review", "code_review"
	require.NoError(t, st.UpdateDelivery(ctx, got))
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/reject", "application/json", bytes.NewBufferString(`{"reason":"x"}`))
	require.Equal(t, 200, r.StatusCode)
	require.Equal(t, []string{d.ID}, fe.rejectedIDs())

	// 404：详情与 gate
	r, _ = c.Get(ts.URL + "/api/deliveries/00000000-0000-0000-0000-000000000000")
	require.Equal(t, 404, r.StatusCode)
	r, _ = c.Get(ts.URL + "/api/deliveries/00000000-0000-0000-0000-000000000000/gate")
	require.Equal(t, 404, r.StatusCode)
	// fake 未清 gate → gate 仍 200（真实引擎 Reject 会清 gate 并回退）
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
}

// 引擎 Start 报错不拖垮创建：异步 driver 吞掉错误（状态由引擎自己承载）。
func TestDeliveryEngineStartErrorSwallowed(t *testing.T) {
	ts, st, fe := newServerWithEngine(t)
	fe.failStart = true
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "p"}
	require.NoError(t, st.CreateProject(ctx, p))

	r, _ := c.Post(ts.URL+"/api/projects/"+p.ID+"/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"需求B"}`))
	require.Equal(t, 200, r.StatusCode)
	var d store.Delivery
	require.NoError(t, json.NewDecoder(r.Body).Decode(&d))
	require.Equal(t, "active", d.Status)

	// 缺 title → 400
	r, _ = c.Post(ts.URL+"/api/projects/"+p.ID+"/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"  "}`))
	require.Equal(t, 400, r.StatusCode)
	// 项目不存在 → 404
	r, _ = c.Post(ts.URL+"/api/projects/00000000-0000-0000-0000-000000000000/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"x"}`))
	require.Equal(t, 404, r.StatusCode)
}

// 畸形 id（非 UUID）一律 404：不能把无效 UUID 传进 store 后以 500 泄漏驱动内部错误。
func TestMalformedIDReturns404(t *testing.T) {
	ts, _, _ := newServerWithEngine(t)
	c := login(t, ts.URL)

	for _, path := range []string{
		"/api/projects/not-a-uuid",
		"/api/projects/not-a-uuid/deliveries",
		"/api/deliveries/not-a-uuid",
		"/api/deliveries/not-a-uuid/gate",
	} {
		r, _ := c.Get(ts.URL + path)
		require.Equal(t, 404, r.StatusCode, "GET %s", path)
	}

	req, _ := http.NewRequest("PATCH", ts.URL+"/api/projects/not-a-uuid", bytes.NewBufferString(`{"pinned":true}`))
	req.Header.Set("Content-Type", "application/json")
	r, _ := c.Do(req)
	require.Equal(t, 404, r.StatusCode)

	r, _ = c.Post(ts.URL+"/api/projects/not-a-uuid/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"x"}`))
	require.Equal(t, 404, r.StatusCode)

	r, _ = c.Post(ts.URL+"/api/deliveries/not-a-uuid/approve", "", nil)
	require.Equal(t, 404, r.StatusCode)
	r, _ = c.Post(ts.URL+"/api/deliveries/not-a-uuid/reject", "application/json",
		bytes.NewBufferString(`{"reason":"x"}`))
	require.Equal(t, 404, r.StatusCode)
}

// 重启恢复：ResumeActive 只对 active 交付点火后台驱动——
// gate-parked 的被驱动循环状态检查直接停车（零引擎调用），中断在半路的被重新驱动，终态不动。
func TestResumeActive(t *testing.T) {
	st := store.NewMemory()
	fe := &fakeEngine{}
	srv := NewServer(st, "secret-pass", fe)
	ctx := context.Background()

	p := &store.Project{Name: "p"}
	require.NoError(t, st.CreateProject(ctx, p))
	mk := func(title, status, gate string) *store.Delivery {
		d := &store.Delivery{ProjectID: p.ID, Title: title, Status: status, CurrentStage: "code_gen", PendingGate: gate}
		require.NoError(t, st.CreateDelivery(ctx, d))
		return d
	}
	gated := mk("gated", "active", "spec_approval") // 停在门禁
	mid := mk("mid", "active", "")                  // 中断在半路（workspace 未就绪 → Start 路径）
	done := mk("done", "completed", "")             // 终态

	srv.ResumeActive(ctx)

	// 中断在半路的：驱动点火（WorkspaceReady=false → Start 负责 Acquire）
	require.Eventually(t, func() bool {
		return slices.Contains(fe.startedIDs(), mid.ID)
	}, 2*time.Second, 10*time.Millisecond)
	// gated（门禁停车零引擎调用）与 done（不在 active 列表）：Start/Continue 均不被调用
	require.Never(t, func() bool {
		return slices.Contains(fe.startedIDs(), gated.ID) || slices.Contains(fe.continuedIDs(), gated.ID) ||
			slices.Contains(fe.startedIDs(), done.ID) || slices.Contains(fe.continuedIDs(), done.ID)
	}, 200*time.Millisecond, 20*time.Millisecond)
}
