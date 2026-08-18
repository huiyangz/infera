package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// localResolver 把 nodes 里的节点绑定为本机（ErrLocalRunner），其余回退真 runner。
func localResolver(nodes ...string) func(context.Context, string, string) (agent.Runner, error) {
	local := map[string]bool{}
	for _, n := range nodes {
		local[n] = true
	}
	return func(_ context.Context, _, node string) (agent.Runner, error) {
		if local[node] {
			return nil, orchestration.ErrLocalRunner
		}
		return nil, nil
	}
}

// parkAt 把交付直接摆到某阶段（本文件聚焦 SubmitLocal/LocalPrompt 的行为，
// 不经引擎驱动路径造停车状态）。
func parkAt(t *testing.T, st *store.Memory, d *store.Delivery, stage string) {
	t.Helper()
	d.CurrentStage = stage
	d.WorkspaceReady = true
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
}

// localEngine 建 engine 并按 localResolver 绑定指定节点。
func localEngine(st *store.Memory, nodes ...string) *Engine {
	e := New(st, &markRunner{mark: "x"}, &FakeWS{}, passTR{})
	e.ResolveRunner = localResolver(nodes...)
	return e
}

func TestSubmitLocalSavesSpecArtifactAndAdvances(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "spec")
	parkAt(t, st, d, "spec")
	ctx := context.Background()

	require.NoError(t, e.SubmitLocal(ctx, d.ID, "# 规格\n实现 add(a,b)"))

	got := get(t, st, d.ID)
	require.Equal(t, "spec_approval", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	a := artifactByKind(t, st, d.ID, "spec")
	require.Equal(t, "# 规格\n实现 add(a,b)", a.Content)
	require.Equal(t, "spec", a.Stage)
	require.Contains(t, eventTypes(t, st, d.ID), "local_stage_submitted")
}

func TestSubmitLocalTasksParsesBlockToJSON(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "tasks")
	parkAt(t, st, d, "tasks")
	ctx := context.Background()

	out := "清单：\n```infera-tasks\n[{\"title\":\"任务A\",\"detail\":\"做A\"}]\n```"
	require.NoError(t, e.SubmitLocal(ctx, d.ID, out))

	require.Equal(t, "tasks_approval", get(t, st, d.ID).CurrentStage)
	a := artifactByKind(t, st, d.ID, "tasks")
	require.JSONEq(t, `[{"title":"任务A","detail":"做A"}]`, a.Content)
}

func TestSubmitLocalCodeGenMarksRemainingTasksAndAdvances(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	require.NoError(t, st.SaveArtifact(context.Background(), &store.Artifact{
		DeliveryID: d.ID, Stage: "tasks", Kind: "tasks",
		Content: `[{"title":"A","detail":"a"},{"title":"B","detail":"b"}]`,
	}))
	e := localEngine(st, "code_gen")
	parkAt(t, st, d, "code_gen")
	ctx := context.Background()

	require.NoError(t, e.SubmitLocal(ctx, d.ID, "全部实现完成"))

	got := get(t, st, d.ID)
	require.Equal(t, "unit_test", got.CurrentStage)
	require.Equal(t, "全部实现完成", artifactByKind(t, st, d.ID, "summary").Content)
	// 剩余任务全部打上 task_done 标记（1、2 两条）
	done, err := e.doneTasks(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, map[int]bool{1: true, 2: true}, done)
}

func TestSubmitLocalGateReviewWritesAgentOutputWithoutAdvancing(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "code_review")
	d.CurrentStage = "code_review"
	d.PendingGate = "code_review"
	d.WorkspaceReady = true
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
	ctx := context.Background()

	require.NoError(t, e.SubmitLocal(ctx, d.ID, "预审意见：整体可合"))

	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate) // 门禁不动，放行仍走 Approve
	a := artifactByKind(t, st, d.ID, "agent_output")
	require.Equal(t, "预审意见：整体可合", a.Content)
	require.Equal(t, "code_review", a.Stage)
}

func TestSubmitLocalRejectsNonLocalNode(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st) // 无 local 绑定
	parkAt(t, st, d, "spec")

	err := e.SubmitLocal(context.Background(), d.ID, "x")
	require.ErrorContains(t, err, "未绑定本机")
	require.Equal(t, "spec", get(t, st, d.ID).CurrentStage) // 状态未动
}

func TestSubmitLocalRejectsGatePendingOnAgentNode(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "spec")
	// spec 本机绑定，但交付挂在门禁上：本机通道的门禁形态只认前置审查
	// （code_review 的 ReviewRole），spec_approval 门禁没有 ReviewRole → 拒绝
	//（放行走 Approve 单入口）。
	d.CurrentStage = "spec_approval"
	d.PendingGate = "spec_approval"
	require.NoError(t, st.UpdateDelivery(context.Background(), d))

	err := e.SubmitLocal(context.Background(), d.ID, "x")
	require.ErrorContains(t, err, "门禁")
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)
}

func TestSubmitLocalRejectsSplitParent(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	d.SplitMode = true
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
	e := localEngine(st, "code_gen")
	parkAt(t, st, d, "code_gen")

	err := e.SubmitLocal(context.Background(), d.ID, "x")
	require.ErrorContains(t, err, "拆分")
	require.Equal(t, "code_gen", get(t, st, d.ID).CurrentStage)
}

func TestSubmitLocalRejectsNotActive(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	d.Status = StatusBlocked
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
	e := localEngine(st, "spec")

	err := e.SubmitLocal(context.Background(), d.ID, "x")
	require.ErrorContains(t, err, "not active")
}

func TestLocalPromptReadonlyFeedback(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "spec")
	d.RejectReason = "补充边界情况"
	parkAt(t, st, d, "spec")

	role, p, err := e.LocalPrompt(context.Background(), d.ID)
	require.NoError(t, err)
	require.Equal(t, "spec", role)
	require.Contains(t, p, "实现 add(a,b)")     // 需求描述
	require.Contains(t, p, "补充边界情况")       // 只读反馈
	require.Contains(t, p, "infera-complexity") // 角色模板

	// 只读：RejectReason 不被消费
	require.Equal(t, "补充边界情况", get(t, st, d.ID).RejectReason)
}

func TestLocalPromptEmptyWhenNotLocal(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st)
	parkAt(t, st, d, "spec")

	role, p, err := e.LocalPrompt(context.Background(), d.ID)
	require.NoError(t, err)
	require.Empty(t, role)
	require.Empty(t, p)
}

func TestLocalPromptGateReview(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "code_review")
	d.CurrentStage = "code_review"
	d.PendingGate = "code_review"
	require.NoError(t, st.UpdateDelivery(context.Background(), d))

	role, p, err := e.LocalPrompt(context.Background(), d.ID)
	require.NoError(t, err)
	require.Equal(t, "code_review", role)
	require.Contains(t, p, "审查") // code_review 角色模板
}

func TestParseSplitPlan(t *testing.T) {
	require.Nil(t, ParseSplitPlan("无块"))
	require.Nil(t, ParseSplitPlan("```infera-split\n{bad json}\n```"))
	plan := ParseSplitPlan("```infera-split\n[{\"title\":\"子\",\"description\":\"范围\",\"wave\":1}]\n```")
	require.Len(t, plan, 1)
	require.Equal(t, "子", plan[0].Title)
	require.Equal(t, 1, plan[0].Wave)
}
