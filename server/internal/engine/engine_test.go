package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/persist"
	"github.com/tokfinity/infera/internal/store"
)

// fakeRunner 记录每次调用的 role，按 role 返回固定产物；specOutput/tasksOutput 可注入
// （复杂度建议块 / 任务清单块测试）。
type fakeRunner struct {
	calls       []agent.Request
	specOutput  string
	tasksOutput string
}

func (f *fakeRunner) Run(_ context.Context, req agent.Request) (agent.Result, error) {
	f.calls = append(f.calls, req)
	switch req.Role {
	case "spec":
		out := f.specOutput
		if out == "" {
			out = "# 规格正文"
		}
		return agent.Result{Output: out}, nil
	case "design":
		return agent.Result{Output: "# 设计正文"}, nil
	case "tasks":
		out := f.tasksOutput
		if out == "" {
			out = "任务清单：\n\n```infera-tasks\n[{\"title\":\"任务A\",\"detail\":\"做 A\"},{\"title\":\"任务B\",\"detail\":\"做 B\"}]\n```"
		}
		return agent.Result{Output: out}, nil
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

func (f *FakeWS) Acquire(_ context.Context, deliveryID, repoURL, _ string) (string, string, error) {
	f.acquireCalled = true
	f.acquireCount++
	base := strings.Repeat("a", 40)
	if repoURL == "" {
		base = "" // 绿地项目：无仓库，base 为空（与真实 workspace.Manager 一致）
	}
	return "/tmp/infera-ws/" + deliveryID, base, nil
}

func (f *FakeWS) Path(deliveryID string) string { return "/tmp/infera-ws/" + deliveryID }

func (f *FakeWS) Release(string) { f.released = true }

// errAcquireWS 的 Acquire 一律失败（验证 workspace 获取失败 → blocked 路径），其余行为同 FakeWS。
type errAcquireWS struct{ FakeWS }

func (f *errAcquireWS) Acquire(_ context.Context, deliveryID, _, _ string) (string, string, error) {
	f.acquireCalled = true
	f.acquireCount++
	return "", "", errors.New("git clone: repository not found")
}

// passTR / failTR 可注入的 TestRunner。
type passTR struct{}

func (passTR) RunTests(context.Context, string) (bool, string, error) {
	return true, "ok 2 tests\nPASS", nil
}

type failTR struct{}

func (failTR) RunTests(context.Context, string) (bool, string, error) {
	return false, "--- FAIL: TestAdd\nFAIL", nil
}

// seqTR 按调用序返回：前 fail 次失败，之后通过（验证 FailCount 在通过时清零）。
type seqTR struct {
	fail  int
	calls int
}

func (s *seqTR) RunTests(_ context.Context, _ string) (bool, string, error) {
	s.calls++
	if s.calls <= s.fail {
		return false, "--- FAIL: TestAdd\nFAIL", nil
	}
	return true, "ok 2 tests\nPASS", nil
}

func newEnv(t *testing.T, tr TestRunner) (*Engine, *store.Memory, *FakeWS, *fakeRunner) {
	t.Helper()
	st := store.NewMemory()
	ar := &fakeRunner{}
	ws := &FakeWS{}
	return New(st, ar, ws, tr), st, ws, ar
}

// approve 门禁批准的测试便捷封装（丢弃返回的子需求，只留 error 供断言）。
func approve(ctx context.Context, e *Engine, deliveryID string, opts store.ApproveOpts) error {
	_, err := e.Approve(ctx, deliveryID, opts)
	return err
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
	require.True(t, got.WorkspaceReady)
	require.Equal(t, []string{"spec"}, ar.roles())
	require.Equal(t, "# 规格正文", artifactByKind(t, st, d.ID, "spec").Content)

	// Approve 只做门禁簿记：清 gate、推进到下一阶段，不跑 agent。
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	got = get(t, st, d.ID)
	require.Equal(t, "test_gen", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, []string{"spec"}, ar.roles()) // 无 agent 被同步驱动

	// Continue：test_gen → code_gen → unit_test（过）→ code_review 前置预审
	// → R10 双道审查 → 停门禁。
	require.NoError(t, e.Continue(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, []string{"spec", "test_gen", "code_gen", "code_review", "spec_conformance", "code_quality"}, ar.roles())
	require.Equal(t, "tests: a_test.go", artifactByKind(t, st, d.ID, "tests").Content)
	require.Equal(t, "改了 2 个文件", artifactByKind(t, st, d.ID, "summary").Content)
	require.NotNil(t, artifactByKind(t, st, d.ID, "test_output"))
	// 门禁前置预审产出 agent_output artifact（门禁页有内容可审）。
	require.Equal(t, "review ok", artifactByKind(t, st, d.ID, "agent_output").Content)

	// 下游 agent 的 prompt 携带 spec，workdir 指向 workspace；预审 prompt 也带 spec。
	require.Contains(t, ar.calls[1].Prompt, "# 规格正文")
	require.Contains(t, ar.calls[2].Prompt, "# 规格正文")
	require.Contains(t, ar.calls[3].Prompt, "# 规格正文")
	require.Equal(t, ws.Path(d.ID), ar.calls[1].Workdir)
	require.Equal(t, ws.Path(d.ID), ar.calls[3].Workdir)

	// Approve 终审（Next=DONE）→ completed + 释放 workspace。
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
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
	e, st, ws, ar := newEnv(t, failTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // 门禁簿记 → test_gen
	require.NoError(t, e.Continue(ctx, d.ID))                      // test_gen → code_gen → unit_test 失败

	// 第 1 次失败：回环 code_gen，FailCount=1，仍 active、无门禁。
	got := get(t, st, d.ID)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, 1, got.FailCount)
	require.Contains(t, eventTypes(t, st, d.ID), "test_failed")

	// 第 2 轮：code_gen 重跑（attempt 递增）→ unit_test 再败。
	// 重跑 prompt 带上一轮 unit_test 失败输出（反馈闭环）。
	require.NoError(t, e.Start(ctx, d.ID))
	require.Contains(t, ar.calls[3].Prompt, "上一轮 unit_test 未过：")
	require.Contains(t, ar.calls[3].Prompt, "--- FAIL: TestAdd")
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
	e, st, _, ar := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID)) // 停在 spec_approval

	// 驳回规格 → 回到 spec 重写，门禁清空，驳回意见落盘。
	require.NoError(t, e.Reject(ctx, d.ID, "验收标准缺失"))
	got := get(t, st, d.ID)
	require.Equal(t, "spec", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, "验收标准缺失", got.RejectReason)
	require.Contains(t, eventTypes(t, st, d.ID), "gate_rejected")

	// 重跑：spec 的 prompt 带人打回意见；消费一次后 RejectReason 清空。
	require.NoError(t, e.Start(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "spec_approval", got.PendingGate)
	require.Empty(t, got.RejectReason)
	require.Contains(t, ar.calls[1].Prompt, "人打回：验收标准缺失")

	// 一路放行到 code_review，驳回 → 回到 code_gen，意见同样落盘并注入重跑 prompt。
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.NoError(t, e.Continue(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.NoError(t, e.Reject(ctx, d.ID, "实现遗漏边界"))
	got = get(t, st, d.ID)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, "实现遗漏边界", got.RejectReason)
	require.NoError(t, e.Continue(ctx, d.ID)) // code_gen 重跑（带反馈）→ unit_test 过 → code_review 门
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Empty(t, got.RejectReason)
	// calls: spec, spec(重写), test_gen, code_gen, code_review, 双道审查, code_gen(重跑)
	// —— 重跑的 code_gen prompt 带人打回意见（取最后一次 code_gen 调用，不依赖序号）。
	cg := codeGenCalls(ar)
	require.Contains(t, ar.calls[cg[len(cg)-1]].Prompt, "人打回：实现遗漏边界")
}

func TestStartAcquiresWorkspace(t *testing.T) {
	e, st, ws, _ := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.False(t, ws.acquireCalled)
	require.NoError(t, e.Start(ctx, d.ID))
	require.True(t, ws.acquireCalled)

	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → test_gen
	require.NoError(t, e.Continue(ctx, d.ID))                      // → code_review 门禁
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → DONE
	require.True(t, ws.released)
	require.Equal(t, StatusCompleted, get(t, st, d.ID).Status)
}

func TestApproveRejectWithoutGate(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	require.Error(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.Error(t, e.Reject(ctx, d.ID, "nope"))

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, e.Reject(ctx, d.ID, "重写"))
	// 驳回后门禁已清空，再 Approve 应报错。
	require.Error(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
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

func TestWorkspaceAcquireFailureBlocks(t *testing.T) {
	st := store.NewMemory()
	ws := &errAcquireWS{}
	ar := &fakeRunner{}
	e := New(st, ar, ws, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	// Acquire 失败 → stage_failed(intake) + blocked，错误上抛（同 agent 失败约定）。
	err := e.Start(ctx, d.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "repository not found")

	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.Empty(t, got.BaseCommit)
	require.True(t, ws.acquireCalled)
	require.True(t, ws.released) // block 的 Release：acquire 已失败，幂等无害
	require.Empty(t, ar.calls)   // 引擎未推进到任何 agent 节点

	// stage_failed 事件：stage=intake，payload 携带错误信息；随后 delivery_blocked。
	evs, err := st.ListEvents(ctx, d.ID)
	require.NoError(t, err)
	var stageFailed, blocked *store.Event
	for i := range evs {
		switch evs[i].EventType {
		case "stage_failed":
			stageFailed = &evs[i]
		case "delivery_blocked":
			blocked = &evs[i]
		}
	}
	require.NotNil(t, stageFailed, "stage_failed event not found")
	require.Equal(t, "intake", stageFailed.Stage)
	require.Contains(t, string(stageFailed.Payload), "repository not found")
	require.NotNil(t, blocked, "delivery_blocked event not found")
}

func TestStartOnCompletedDeliveryErrors(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	// 一路放行到 completed。
	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.NoError(t, e.Continue(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.Equal(t, StatusCompleted, get(t, st, d.ID).Status)

	// 已完成的 delivery 不可再驱动："not active" 守卫报错（Start 与 Continue 一致）。
	err := e.Start(ctx, d.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
	err = e.Continue(ctx, d.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

func TestStartWhileGatePendingIsNoop(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	d := seed(t, st)
	ctx := context.Background()

	// 停在 spec_approval 门禁后再次 Start：安全 no-op。
	require.NoError(t, e.Start(ctx, d.ID))
	before := get(t, st, d.ID)
	eventsBefore := eventTypes(t, st, d.ID)
	callsBefore := len(ar.calls)

	require.NoError(t, e.Start(ctx, d.ID))

	require.Equal(t, before, get(t, st, d.ID))              // delivery 原样
	require.Equal(t, eventsBefore, eventTypes(t, st, d.ID)) // 无新事件
	require.Equal(t, callsBefore, len(ar.calls))            // agent 未被重跑
}

func TestFailCountResetsOnPass(t *testing.T) {
	tr := &seqTR{fail: 2} // 前 2 次 unit_test 失败，第 3 次通过
	e, st, _, _ := newEnv(t, tr)
	d := seed(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // 门禁簿记 → test_gen
	require.NoError(t, e.Continue(ctx, d.ID))                      // → unit_test 失败 #1，回环 code_gen
	got := get(t, st, d.ID)
	require.Equal(t, 1, got.FailCount)
	require.Equal(t, "code_gen", got.CurrentStage)

	require.NoError(t, e.Start(ctx, d.ID)) // code_gen 重跑 → unit_test 失败 #2
	got = get(t, st, d.ID)
	require.Equal(t, 2, got.FailCount)
	require.Equal(t, "code_gen", got.CurrentStage)

	// 第 3 次通过：FailCount 清零，抵达 code_review 门禁。
	require.NoError(t, e.Start(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, 0, got.FailCount)
	require.Equal(t, StatusActive, got.Status)
}

// fakePersister 记录每次输入；err 非空时模拟固化失败；diff 可注入（截断测试）。
type fakePersister struct {
	calls []persist.Input
	err   error
	diff  string
}

func (f *fakePersister) Persist(_ context.Context, in persist.Input) (persist.Result, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return persist.Result{}, f.err
	}
	diff := f.diff
	if diff == "" {
		diff = "diff --git a/hello.txt b/hello.txt"
	}
	return persist.Result{
		Diff:   diff,
		PRURL:  "https://github.com/example/repo/pull/7",
		Branch: "infera/abcd1234",
	}, nil
}

// driveToCodeReview 一路推进到 code_review 门禁（固化发生点）。
func driveToCodeReview(t *testing.T, e *Engine, st *store.Memory) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	d := seed(t, st)
	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.NoError(t, e.Continue(ctx, d.ID))
	return d
}

// TestPersistAtCodeReviewGate：门禁到达时恰好固化一次，真 diff/pr 落为 artifact；
// DONE 放行不再重复固化。
func TestPersistAtCodeReviewGate(t *testing.T) {
	e, st, ws, _ := newEnv(t, passTR{})
	fp := &fakePersister{}
	e.WithPersister(fp)
	ctx := context.Background()
	d := driveToCodeReview(t, e, st)

	require.Len(t, fp.calls, 1)
	in := fp.calls[0]
	require.Equal(t, d.ID, in.DeliveryID)
	require.Equal(t, "https://github.com/example/repo.git", in.RepoURL)
	require.Equal(t, "main", in.BaseBranch)
	require.Len(t, in.BaseCommit, 40)
	require.Equal(t, ws.Path(d.ID), in.Workdir)
	require.Equal(t, "加法函数", in.Title)

	require.Equal(t, "diff --git a/hello.txt b/hello.txt", artifactByKind(t, st, d.ID, "diff").Content)
	require.Equal(t, "https://github.com/example/repo/pull/7", artifactByKind(t, st, d.ID, "pr").Content)
	require.Contains(t, eventTypes(t, st, d.ID), "persist_done")

	// 门禁照常挂起；放行 → completed，不再重复固化。
	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.Equal(t, StatusCompleted, get(t, st, d.ID).Status)
	require.Len(t, fp.calls, 1)
}

// TestPersistFailureBlocksKeepsWorkdir：固化失败（commit/push 错误）→
// persist_failed 事件 + blocked 且不释放 workdir（数据安全不变量：产出还在里面）。
func TestPersistFailureBlocksKeepsWorkdir(t *testing.T) {
	e, st, ws, _ := newEnv(t, passTR{})
	fp := &fakePersister{err: errors.New("git push: denied")}
	e.WithPersister(fp)
	ctx := context.Background()
	d := seed(t, st)
	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))

	err := e.Continue(ctx, d.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "push: denied")

	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.Empty(t, got.PendingGate)
	require.False(t, ws.released, "固化失败必须保留 workdir 供人工救援")
	require.Contains(t, eventTypes(t, st, d.ID), "persist_failed")
	require.Contains(t, eventTypes(t, st, d.ID), "delivery_blocked")

	// 固化失败不产出 diff/pr artifact。
	arts, err := st.ListArtifacts(ctx, d.ID)
	require.NoError(t, err)
	for _, a := range arts {
		require.NotEqual(t, "diff", a.Kind)
		require.NotEqual(t, "pr", a.Kind)
	}
}

// TestPersistAgainAfterReject：驳回重做后再到门禁 → 第二次固化
// （实现层 force 推同一分支，此处只验证引擎会再调）。
func TestPersistAgainAfterReject(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	fp := &fakePersister{}
	e.WithPersister(fp)
	ctx := context.Background()
	d := driveToCodeReview(t, e, st)
	require.Len(t, fp.calls, 1)

	require.NoError(t, e.Reject(ctx, d.ID, "边界遗漏"))
	require.NoError(t, e.Continue(ctx, d.ID)) // code_gen 重跑 → unit_test 过 → 再到门禁

	require.Len(t, fp.calls, 2)
	require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
}

// 绿地项目（无仓库）base_commit 恒为空：WorkspaceReady 标志保证
// 反复驱动只 Acquire 一次、workspace_ready 事件只发一次。
func TestGreenfieldWorkspaceReadyOnce(t *testing.T) {
	e, st, ws, _ := newEnv(t, passTR{})
	ctx := context.Background()
	p := &store.Project{Name: "greenfield", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	d := &store.Delivery{ProjectID: p.ID, Title: "绿地", Status: StatusActive, CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, d))

	require.NoError(t, e.Start(ctx, d.ID))
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)
	require.NoError(t, e.Reject(ctx, d.ID, "重写"))
	require.NoError(t, e.Start(ctx, d.ID)) // 第二次完整驱动
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)

	require.Equal(t, 1, ws.acquireCount)
	require.Empty(t, get(t, st, d.ID).BaseCommit)
	count := 0
	for _, et := range eventTypes(t, st, d.ID) {
		if et == "workspace_ready" {
			count++
		}
	}
	require.Equal(t, 1, count, "workspace_ready 应只发一次")
}
