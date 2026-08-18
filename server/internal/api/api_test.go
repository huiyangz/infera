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
		bytes.NewBufferString(`{"name":"demo","repo_url":"https://github.com/x/y","default_branch":"main"}`))
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
	mu                  sync.Mutex
	started             []string
	continued           []string
	approved            []string
	approveSplits       map[string][]store.ChildSpec
	approveComplexities map[string]string
	approveTasks        map[string][]store.TaskSpec
	rejected            []string
	resumeMerges        []string
	droveParents        []string
	failStart           bool
	failResumeMerge     bool
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

func (f *fakeEngine) Approve(_ context.Context, id string, opts store.ApproveOpts) ([]store.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved = append(f.approved, id)
	if len(opts.Split) > 0 {
		if f.approveSplits == nil {
			f.approveSplits = map[string][]store.ChildSpec{}
		}
		f.approveSplits[id] = opts.Split
	}
	if opts.Complexity != "" {
		if f.approveComplexities == nil {
			f.approveComplexities = map[string]string{}
		}
		f.approveComplexities[id] = opts.Complexity
	}
	if len(opts.Tasks) > 0 {
		if f.approveTasks == nil {
			f.approveTasks = map[string][]store.TaskSpec{}
		}
		f.approveTasks[id] = opts.Tasks
	}
	return nil, nil
}

func (f *fakeEngine) Reject(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected = append(f.rejected, id)
	return nil
}

func (f *fakeEngine) ResumeMerge(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failResumeMerge {
		return errors.New("not in conflict")
	}
	f.resumeMerges = append(f.resumeMerges, id)
	return nil
}

func (f *fakeEngine) MaybeDriveParent(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.droveParents = append(f.droveParents, id)
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

func (f *fakeEngine) resumeMergeIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.resumeMerges...)
}

func (f *fakeEngine) droveParentIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.droveParents...)
}

func (f *fakeEngine) splitFor(id string) []store.ChildSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.approveSplits[id]
}

func (f *fakeEngine) complexityFor(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.approveComplexities[id]
}

func (f *fakeEngine) tasksFor(id string) []store.TaskSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.approveTasks[id]
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

	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
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

	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
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

	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))
	mk := func(title, status, gate string) *store.Delivery {
		d := &store.Delivery{ProjectID: p.ID, Title: title, Status: status, CurrentStage: "code_gen", PendingGate: gate}
		require.NoError(t, st.CreateDelivery(ctx, d))
		return d
	}
	gated := mk("gated", "active", "spec_approval") // 停在门禁
	mid := mk("mid", "active", "")                  // 中断在半路（workspace 未就绪 → Start 路径）
	done := mk("done", "completed", "")             // 终态
	// 拆分父停在 code_gen（等子需求/合并语义）：恢复时走 MaybeDriveParent，不走 agent 驱动。
	split := &store.Delivery{ProjectID: p.ID, Title: "split parent", Status: "active",
		CurrentStage: "code_gen", SplitMode: true}
	require.NoError(t, st.CreateDelivery(ctx, split))

	srv.ResumeActive(ctx)

	// 中断在半路的：驱动点火（WorkspaceReady=false → Start 负责 Acquire）
	require.Eventually(t, func() bool {
		return slices.Contains(fe.startedIDs(), mid.ID)
	}, 2*time.Second, 10*time.Millisecond)
	// 拆分父：MaybeDriveParent 被调用
	require.Eventually(t, func() bool {
		return slices.Contains(fe.droveParentIDs(), split.ID)
	}, 2*time.Second, 10*time.Millisecond)
	// gated（门禁停车零引擎调用）与 done（不在 active 列表）：Start/Continue 均不被调用
	require.Never(t, func() bool {
		return slices.Contains(fe.startedIDs(), gated.ID) || slices.Contains(fe.continuedIDs(), gated.ID) ||
			slices.Contains(fe.startedIDs(), done.ID) || slices.Contains(fe.continuedIDs(), done.ID)
	}, 200*time.Millisecond, 20*time.Millisecond)
	// 拆分父不走 agent 驱动（Start/Continue 都不出现）
	require.Never(t, func() bool {
		return slices.Contains(fe.startedIDs(), split.ID) || slices.Contains(fe.continuedIDs(), split.ID)
	}, 200*time.Millisecond, 20*time.Millisecond)
}

// --- split deliveries：approve 带选项 / gate 返回建议 / merge resume / children ---

// seedGate 建一个停在指定门禁、对应 artifact 已就位的交付
// （走到 design/tasks 门的交付按语义置 complexity=large）。
func seedGate(t *testing.T, st *store.Memory, projID, gate, kind, content string) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	complexity := ""
	if gate == "design_approval" || gate == "tasks_approval" {
		complexity = "large"
	}
	d := &store.Delivery{ProjectID: projID, Title: "父", Status: "active",
		CurrentStage: gate, PendingGate: gate, Complexity: complexity}
	require.NoError(t, st.CreateDelivery(ctx, d))
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: kind, Kind: kind, Content: content,
	}))
	return d
}

// seedSpecGate 停在 spec_approval 门禁、spec artifact 已就位。
func seedSpecGate(t *testing.T, st *store.Memory, projID, spec string) *store.Delivery {
	t.Helper()
	return seedGate(t, st, projID, "spec_approval", "spec", spec)
}

func TestApprovePassesBodyThrough(t *testing.T) {
	ts, st, fe := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))

	// spec 门：complexity 透传。
	d := seedSpecGate(t, st, p.ID, "# spec")
	r, _ := c.Post(ts.URL+"/api/deliveries/"+d.ID+"/approve", "application/json",
		bytes.NewBufferString(`{"complexity":"large"}`))
	require.Equal(t, 200, r.StatusCode)
	require.Equal(t, "large", fe.complexityFor(d.ID))

	// design 门：split 透传（门禁校验在引擎，handler 只透传）。
	d2 := seedGate(t, st, p.ID, "design_approval", "design", "# 设计")
	body := `{"split":[{"title":"子A","description":"写入 a.txt","wave":1},{"title":"子B","description":"写入 b.txt","wave":2}]}`
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d2.ID+"/approve", "application/json", bytes.NewBufferString(body))
	require.Equal(t, 200, r.StatusCode)

	split := fe.splitFor(d2.ID)
	require.NotNil(t, split, "split 应透传给引擎")
	require.Len(t, split, 2)
	require.Equal(t, "子A", split[0].Title)
	require.Equal(t, 2, split[1].Wave)

	// tasks 门：任务清单覆盖透传（门禁校验在引擎，handler 只透传）。
	d3 := seedGate(t, st, p.ID, "tasks_approval", "tasks", `[{"title":"任务A","detail":"做 A"}]`)
	tasksBody := `{"tasks":[{"title":"人工一","detail":"x"},{"title":"人工二","detail":"y"}]}`
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d3.ID+"/approve", "application/json", bytes.NewBufferString(tasksBody))
	require.Equal(t, 200, r.StatusCode)
	tasks := fe.tasksFor(d3.ID)
	require.NotNil(t, tasks, "tasks 应透传给引擎")
	require.Len(t, tasks, 2)
	require.Equal(t, "人工一", tasks[0].Title)
	require.Equal(t, "y", tasks[1].Detail)

	// 坏 JSON → 400
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d3.ID+"/approve", "application/json", bytes.NewBufferString("{bad"))
	require.Equal(t, 400, r.StatusCode)
}

// TestGateComplexitySuggestion：spec 门响应带 complexity_suggestion
// （spec 末尾 infera-complexity 块；无/坏块 = 空串）。
func TestGateComplexitySuggestion(t *testing.T) {
	ts, st, _ := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))

	withBlock := seedSpecGate(t, st, p.ID, "# 规格\n\n```infera-complexity\nlarge\n```\n")
	var gate struct {
		Gate                 string `json:"gate"`
		ComplexitySuggestion string `json:"complexity_suggestion"`
	}
	r, _ := c.Get(ts.URL + "/api/deliveries/" + withBlock.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&gate))
	require.Equal(t, "spec_approval", gate.Gate)
	require.Equal(t, "large", gate.ComplexitySuggestion)

	// 无 block / 坏值块 → 空串（前端按 small 预选）。
	for _, s := range []string{"# 普通 spec", "# s\n\n```infera-complexity\nhuge\n```"} {
		d2 := seedSpecGate(t, st, p.ID, s)
		var g2 struct {
			ComplexitySuggestion string `json:"complexity_suggestion"`
		}
		r, _ = c.Get(ts.URL + "/api/deliveries/" + d2.ID + "/gate")
		require.Equal(t, 200, r.StatusCode)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&g2))
		require.Empty(t, g2.ComplexitySuggestion)
	}
}

// TestGateReturnsSplitPlan：design 门响应带 split_plan（design 末尾 infera-split 块）。
func TestGateReturnsSplitPlan(t *testing.T) {
	ts, st, _ := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))

	design := "# 设计\n\n```infera-split\n[{\"title\":\"子A\",\"description\":\"写入 a.txt\",\"wave\":1},{\"title\":\"子C\",\"description\":\"写入 c.txt\",\"wave\":2}]\n```\n"
	d := seedGate(t, st, p.ID, "design_approval", "design", design)

	var gate struct {
		Gate      string             `json:"gate"`
		SplitPlan *[]store.ChildSpec `json:"split_plan"`
	}
	r, _ := c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&gate))
	require.Equal(t, "design_approval", gate.Gate)
	require.NotNil(t, gate.SplitPlan)
	require.Len(t, *gate.SplitPlan, 2)
	require.Equal(t, "子A", (*gate.SplitPlan)[0].Title)
	require.Equal(t, 1, (*gate.SplitPlan)[0].Wave)
	require.Equal(t, 2, (*gate.SplitPlan)[1].Wave)

	// 无 block → split_plan 为 null；坏 JSON 块同样按无建议处理。
	for _, s := range []string{"# 普通设计", "# d\n\n```infera-split\n{not json}\n```"} {
		d2 := seedGate(t, st, p.ID, "design_approval", "design", s)
		var g2 struct {
			SplitPlan *[]store.ChildSpec `json:"split_plan"`
		}
		r, _ = c.Get(ts.URL + "/api/deliveries/" + d2.ID + "/gate")
		require.Equal(t, 200, r.StatusCode)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&g2))
		require.Nil(t, g2.SplitPlan)
	}
}

// TestGateReturnsTasks：任务审批门响应带 tasks 清单（tasks artifact 为引擎
// 解析后的 JSON；坏内容 → null，前端按空清单渲染可人工覆盖）。
func TestGateReturnsTasks(t *testing.T) {
	ts, st, _ := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))

	d := seedGate(t, st, p.ID, "tasks_approval", "tasks",
		`[{"title":"任务A","detail":"做 A"},{"title":"任务B","detail":"做 B"}]`)
	var gate struct {
		Gate  string            `json:"gate"`
		Tasks *[]store.TaskSpec `json:"tasks"`
	}
	r, _ := c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&gate))
	require.Equal(t, "tasks_approval", gate.Gate)
	require.NotNil(t, gate.Tasks)
	require.Len(t, *gate.Tasks, 2)
	require.Equal(t, "任务A", (*gate.Tasks)[0].Title)
	require.Equal(t, "做 B", (*gate.Tasks)[1].Detail)

	// 空清单 / 坏内容 → tasks=null（畸形块容错后的空清单用 [] 表示，
	// 非法历史数据则不渲染清单）。
	for _, s := range []string{"[]", "任务清单（旧格式原始输出）"} {
		d2 := seedGate(t, st, p.ID, "tasks_approval", "tasks", s)
		var g2 struct {
			Tasks *[]store.TaskSpec `json:"tasks"`
		}
		r, _ = c.Get(ts.URL + "/api/deliveries/" + d2.ID + "/gate")
		require.Equal(t, 200, r.StatusCode)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&g2))
		if s == "[]" {
			require.NotNil(t, g2.Tasks)
			require.Empty(t, *g2.Tasks)
		} else {
			require.Nil(t, g2.Tasks)
		}
	}
}

// TestGateCodeReviewReturnsReviewsAndDiff：code_review 门响应带两道审查的
// findings 报告（引用 + 内容）与真 diff；未产出的道 present=false；坏 JSON 容错为原文。
func TestGateCodeReviewReturnsReviewsAndDiff(t *testing.T) {
	ts, st, _ := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))

	d := seedGate(t, st, p.ID, "code_review", "agent_output", "预审意见")
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "code_review", Kind: "diff", Content: "diff --git a/a.go b/a.go",
	}))
	report := store.FindingsReport{
		Review: "spec_conformance", TaskBased: true,
		Findings: []store.Finding{{TaskIndex: 1, Severity: "major", Message: "任务1缺少 Y", Evidence: "a.go:9"}},
		Raw:      "原始输出",
	}
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "code_review", Kind: store.KindSpecConformanceFindings, Content: string(raw),
	}))

	var gate struct {
		Diff    string `json:"diff"`
		Reviews []struct {
			Review     string          `json:"review"`
			Present    bool            `json:"present"`
			TaskBased  bool            `json:"task_based"`
			ArtifactID string          `json:"artifact_id"`
			Findings   []store.Finding `json:"findings"`
			Raw        string          `json:"raw"`
		} `json:"reviews"`
	}
	r, _ := c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&gate))
	require.Contains(t, gate.Diff, "diff --git a/a.go")
	require.Len(t, gate.Reviews, 2)

	sc := gate.Reviews[0]
	require.Equal(t, "spec_conformance", sc.Review)
	require.True(t, sc.Present)
	require.True(t, sc.TaskBased)
	require.NotEmpty(t, sc.ArtifactID)
	require.Len(t, sc.Findings, 1)
	require.Equal(t, 1, sc.Findings[0].TaskIndex)
	require.Equal(t, "任务1缺少 Y", sc.Findings[0].Message)
	require.Equal(t, "原始输出", sc.Raw)

	// 未产出的道（如 local 占位跳过）：present=false、无内容。
	cq := gate.Reviews[1]
	require.Equal(t, "code_quality", cq.Review)
	require.False(t, cq.Present)
	require.Empty(t, cq.Findings)
	require.Empty(t, cq.Raw)

	// 坏 JSON 的历史产物：present=true 但内容走原文兜底，不崩。
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "code_review", Kind: store.KindCodeQualityFindings, Content: "not-json",
	}))
	var g2 struct {
		Reviews []struct {
			Review   string          `json:"review"`
			Present  bool            `json:"present"`
			Findings []store.Finding `json:"findings"`
			Raw      string          `json:"raw"`
		} `json:"reviews"`
	}
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&g2))
	require.True(t, g2.Reviews[1].Present)
	require.Nil(t, g2.Reviews[1].Findings)
	require.Equal(t, "not-json", g2.Reviews[1].Raw)
}

func TestMergeResumeEndpoint(t *testing.T) {
	ts, st, fe := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))
	parent := &store.Delivery{ProjectID: p.ID, Title: "父", Status: "active",
		CurrentStage: "code_gen", SplitMode: true, MergeState: "conflict"}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	child := &store.Delivery{ProjectID: p.ID, Title: "子", Status: "completed",
		CurrentStage: "code_review", ParentID: parent.ID, Wave: 1}
	require.NoError(t, st.CreateDelivery(ctx, child))

	// happy：ResumeMerge 被调用，响应 200 + 当前 delivery。
	r, _ := c.Post(ts.URL+"/api/deliveries/"+parent.ID+"/merge/resume", "", nil)
	require.Equal(t, 200, r.StatusCode)
	var got store.Delivery
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	require.Equal(t, parent.ID, got.ID)
	require.Equal(t, []string{parent.ID}, fe.resumeMergeIDs())

	// 引擎报错（非 conflict）→ 400
	fe.failResumeMerge = true
	r, _ = c.Post(ts.URL+"/api/deliveries/"+parent.ID+"/merge/resume", "", nil)
	require.Equal(t, 400, r.StatusCode)

	// 畸形 id → 404
	r, _ = c.Post(ts.URL+"/api/deliveries/not-a-uuid/merge/resume", "", nil)
	require.Equal(t, 404, r.StatusCode)
}

func TestSplitParentDetailIncludesChildren(t *testing.T) {
	ts, st := newServer(t) // 无引擎：详情只读
	c := login(t, ts.URL)
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))
	parent := &store.Delivery{ProjectID: p.ID, Title: "父", Status: "active",
		CurrentStage: "code_gen", SplitMode: true}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{
		ProjectID: p.ID, Title: "子", Status: "queued", CurrentStage: "intake",
		ParentID: parent.ID, Wave: 2,
	}))

	var detail struct {
		Delivery store.Delivery    `json:"delivery"`
		Children *[]store.Delivery `json:"children"`
	}
	r, _ := c.Get(ts.URL + "/api/deliveries/" + parent.ID)
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&detail))
	require.True(t, detail.Delivery.SplitMode)
	require.NotNil(t, detail.Children)
	require.Len(t, *detail.Children, 1)
	require.Equal(t, parent.ID, (*detail.Children)[0].ParentID)

	// 非拆分交付不含 children 字段
	plain := &store.Delivery{ProjectID: p.ID, Title: "普通", Status: "active", CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, plain))
	var plainDetail struct {
		Children *[]store.Delivery `json:"children"`
	}
	r, _ = c.Get(ts.URL + "/api/deliveries/" + plain.ID)
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&plainDetail))
	require.Nil(t, plainDetail.Children)
}

func TestRepoRequired(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	// 建项目必须绑仓库
	r, _ := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"green"}`))
	require.Equal(t, 400, r.StatusCode)

	// 未绑仓库的存量项目：提需求被拒
	p := &store.Project{Name: "no-repo"}
	require.NoError(t, st.CreateProject(ctx, p))
	r, _ = c.Post(ts.URL+"/api/projects/"+p.ID+"/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"x"}`))
	require.Equal(t, 400, r.StatusCode)
	var e map[string]string
	_ = json.NewDecoder(r.Body).Decode(&e)
	require.Contains(t, e["error"], "未绑定仓库")
}
