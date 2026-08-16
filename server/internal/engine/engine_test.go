package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// fakeRunner 记录每次调用的 role，按 role 返回固定产物。
type fakeRunner struct {
	calls []agent.Request
}

func (f *fakeRunner) Run(_ context.Context, req agent.Request) (agent.Result, error) {
	f.calls = append(f.calls, req)
	switch req.Role {
	case "spec":
		return agent.Result{Output: "# 规格正文"}, nil
	case "test_gen":
		return agent.Result{Output: "tests: a_test.go"}, nil
	case "code_gen":
		return agent.Result{Output: "改了 2 个文件"}, nil
	case "code_review":
		return agent.Result{Output: "review ok"}, nil
	}
	return agent.Result{Output: "ok: " + req.Role}, nil
}

func (f *fakeRunner) roles() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Role)
	}
	return out
}

// errRunner agent 一律失败（验证 blocked 路径）。
type errRunner struct{}

func (errRunner) Run(context.Context, agent.Request) (agent.Result, error) {
	return agent.Result{}, errors.New("agent crashed")
}

// FakeWS 记录 Acquire/Release 是否发生。
type FakeWS struct {
	acquireCalled bool
	acquireCount  int
	released      bool
}

func (f *FakeWS) Acquire(_ context.Context, deliveryID, _, _ string) (string, string, error) {
	f.acquireCalled = true
	f.acquireCount++
	return "/tmp/infera-ws/" + deliveryID, strings.Repeat("a", 40), nil
}

func (f *FakeWS) Path(deliveryID string) string { return "/tmp/infera-ws/" + deliveryID }

func (f *FakeWS) Release(string) { f.released = true }

// passTR / failTR 可注入的 TestRunner。
type passTR struct{}

func (passTR) RunTests(context.Context, string) (bool, string, error) {
	return true, "ok 2 tests\nPASS", nil
}

type failTR struct{}

func (failTR) RunTests(context.Context, string) (bool, string, error) {
	return false, "--- FAIL: TestAdd\nFAIL", nil
}

func newEnv(t *testing.T, tr TestRunner) (*Engine, *store.Memory, *FakeWS, *fakeRunner) {
	t.Helper()
	st := store.NewMemory()
	ar := &fakeRunner{}
	ws := &FakeWS{}
	return New(st, ar, ws, tr), st, ws, ar
}

func seed(t *testing.T, st *store.Memory) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	p := &store.Project{Name: "demo", RepoURL: "https://github.com/example/repo.git", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	d := &store.Delivery{
		ProjectID:    p.ID,
		Title:        "加法函数",
		Description:  "实现 add(a,b)",
		Status:       StatusActive,
		CurrentStage: "intake",
	}
	require.NoError(t, st.CreateDelivery(ctx, d))
	return d
}

func get(t *testing.T, st *store.Memory, id string) *store.Delivery {
	t.Helper()
	d, err := st.GetDelivery(context.Background(), id)
	require.NoError(t, err)
	return d
}

// artifactByKind 取指定 kind 的最新产物。
func artifactByKind(t *testing.T, st *store.Memory, deliveryID, kind string) *store.Artifact {
	t.Helper()
	arts, err := st.ListArtifacts(context.Background(), deliveryID)
	require.NoError(t, err)
	var latest *store.Artifact
	for i := range arts {
		if arts[i].Kind == kind {
			latest = &arts[i]
		}
	}
	require.NotNil(t, latest, "artifact kind %s not found", kind)
	return latest
}

func eventTypes(t *testing.T, st *store.Memory, deliveryID string) []string {
	t.Helper()
	evs, err := st.ListEvents(context.Background(), deliveryID)
	require.NoError(t, err)
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.EventType
	}
	return out
}

func TestPipelineHappyPath(t *testing.T) {
	e, st, ws, ar := newEnv(t, passTR{})
	var notified []string
	e.Notify = func(_, _, eventType string) { notified = append(notified, eventType) }
	d := seed(t, st)
	ctx := context.Background()

	// Start：Acquire workspace → intake → spec → 停在 spec_approval 门禁。
	require.NoError(t, e.Start(ctx, d.ID))
	got := get(t, st, d.ID)
	require.Equal(t, "spec_approval", got.CurrentStage)
	require.Equal(t, "spec_approval", got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Len(t, got.BaseCommit, 40)
	require.Equal(t, []string{"spec"}, ar.roles())
	require.Equal(t, "# 规格正文", artifactByKind(t, st, d.ID, "spec").Content)

	// Approve 规格 → test_gen → code_gen → unit_test（过）→ 停在 code_review 门禁。
	require.NoError(t, e.Approve(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, []string{"spec", "test_gen", "code_gen"}, ar.roles())
	require.Equal(t, "tests: a_test.go", artifactByKind(t, st, d.ID, "tests").Content)
	require.Equal(t, "改了 2 个文件", artifactByKind(t, st, d.ID, "diff").Content)
	require.NotNil(t, artifactByKind(t, st, d.ID, "test_output"))

	// 下游 agent 的 prompt 携带 spec，workdir 指向 workspace。
	require.Contains(t, ar.calls[1].Prompt, "# 规格正文")
	require.Contains(t, ar.calls[2].Prompt, "# 规格正文")
	require.Equal(t, ws.Path(d.ID), ar.calls[1].Workdir)

	// Approve 终审 → completed + 释放 workspace。
	require.NoError(t, e.Approve(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, StatusCompleted, got.Status)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.True(t, ws.released)
	require.Contains(t, notified, "delivery_completed")

	types := eventTypes(t, st, d.ID)
	require.Equal(t, "workspace_ready", types[0])
	require.Contains(t, types, "stage_started")
	require.Contains(t, types, "gate_pending")
	require.Contains(t, types, "gate_approved")
	require.Contains(t, types, "delivery_completed")
}

func TestUnitTestLoopAndBlocked(t *testing.T) {
	e, st, ws, _ := newEnv(t, failTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, e.Approve(ctx, d.ID)) // test_gen → code_gen → unit_test 失败

	// 第 1 次失败：回环 code_gen，FailCount=1，仍 active、无门禁。
	got := get(t, st, d.ID)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, 1, got.FailCount)
	require.Contains(t, eventTypes(t, st, d.ID), "test_failed")

	// 第 2 轮：code_gen 重跑（attempt 递增）→ unit_test 再败。
	require.NoError(t, e.Start(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, 2, got.FailCount)
	require.Equal(t, "code_gen", got.CurrentStage)
	r, err := st.LatestStageRun(ctx, d.ID, "code_gen")
	require.NoError(t, err)
	require.Equal(t, 2, r.Attempt)

	// 第 3 次连续失败 → blocked + 释放 workspace（设计内终态，Start 不报错）。
	require.NoError(t, e.Start(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.Equal(t, 3, got.FailCount)
	require.True(t, ws.released)

	ut, err := st.LatestStageRun(ctx, d.ID, "unit_test")
	require.NoError(t, err)
	require.Equal(t, "failed", ut.Status)

	// base_commit 已记录，重跑不重复 Acquire。
	require.Equal(t, 1, ws.acquireCount)
}

func TestRejectLoopsBack(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID)) // 停在 spec_approval

	// 驳回规格 → 回到 spec 重写，门禁清空。
	require.NoError(t, e.Reject(ctx, d.ID, "验收标准缺失"))
	got := get(t, st, d.ID)
	require.Equal(t, "spec", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Contains(t, eventTypes(t, st, d.ID), "gate_rejected")

	// 重跑后再次停在规格门禁。
	require.NoError(t, e.Start(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "spec_approval", got.PendingGate)

	// 一路放行到 code_review，驳回 → 回到 code_gen。
	require.NoError(t, e.Approve(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.NoError(t, e.Reject(ctx, d.ID, "实现遗漏边界"))
	got = get(t, st, d.ID)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Empty(t, got.PendingGate)
}

func TestStartAcquiresWorkspace(t *testing.T) {
	e, st, ws, _ := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.False(t, ws.acquireCalled)
	require.NoError(t, e.Start(ctx, d.ID))
	require.True(t, ws.acquireCalled)

	require.NoError(t, e.Approve(ctx, d.ID))
	require.NoError(t, e.Approve(ctx, d.ID))
	require.True(t, ws.released)
	require.Equal(t, StatusCompleted, get(t, st, d.ID).Status)
}

func TestApproveRejectWithoutGate(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.Error(t, e.Approve(ctx, d.ID))
	require.Error(t, e.Reject(ctx, d.ID, "nope"))

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, e.Reject(ctx, d.ID, "重写"))
	// 驳回后门禁已清空，再 Approve 应报错。
	require.Error(t, e.Approve(ctx, d.ID))
}

func TestAgentFailureBlocks(t *testing.T) {
	st := store.NewMemory()
	ws := &FakeWS{}
	e := New(st, errRunner{}, ws, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	// spec agent 失败 → stage_failed + blocked + 释放 workspace，错误上抛。
	require.Error(t, e.Start(ctx, d.ID))
	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.True(t, ws.released)
	require.Contains(t, eventTypes(t, st, d.ID), "stage_failed")

	r, err := st.LatestStageRun(ctx, d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, "failed", r.Status)
}
