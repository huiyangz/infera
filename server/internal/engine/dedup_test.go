package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// countEventsOfType 统计指定类型事件条数。
func countEventsOfType(t *testing.T, st *store.Memory, deliveryID, eventType string) int {
	t.Helper()
	n := 0
	for _, et := range eventTypes(t, st, deliveryID) {
		if et == eventType {
			n++
		}
	}
	return n
}

// TestLocalStagePendingEmittedOncePerPark：本机停车事件幂等——
// 重复驱动（重启恢复 / driveLocked 回环）不再重复刷 local_stage_pending；
// 交回并真正离开该节点后再停车（新停车）会重新发。
func TestLocalStagePendingEmittedOncePerPark(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "spec")
	d.WorkspaceReady = true
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
	ctx := context.Background()

	// 第一次驱动：停在 spec（本机占位），发一条 local_stage_pending。
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, 1, countEventsOfType(t, st, d.ID, "local_stage_pending"))

	// 反复重驱动：交付没动过，不再重复广播。
	require.NoError(t, e.Continue(ctx, d.ID))
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, 1, countEventsOfType(t, st, d.ID, "local_stage_pending"))

	// 交回 → 推进挂门禁 → 打回 → 重新停在 spec：这是新停车，要重新发。
	require.NoError(t, e.SubmitLocal(ctx, d.ID, "# spec v2"))
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)
	require.NoError(t, e.Reject(ctx, d.ID, "重写"))
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, 2, countEventsOfType(t, st, d.ID, "local_stage_pending"))
}

// TestGateReviewLocalPendingEmittedOncePerPark：门禁前置审查的本机停车同样幂等；
// 门禁被打回后重新进入的停车是新停车，要重新发。
func TestGateReviewLocalPendingEmittedOncePerPark(t *testing.T) {
	st := store_Memory(t)
	d := seedEngine(t, st)
	e := localEngine(st, "code_review")
	d.CurrentStage = "code_review"
	d.PendingGate = "code_review"
	d.WorkspaceReady = true
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
	ctx := context.Background()

	// 直接驱动：run() 见 PendingGate 即停，事件由 step 路径产生——
	// 这里手动走到 step 的等价路径：通过 Continue 前先清门禁会跑 gate 流程，
	// 改为验证引擎进入门禁路径的停车：先跑一次完整流程不现实，
	// 直接调用 step 层等价逻辑（run 的 gate 分支）太深。
	// 简化：把交付摆到 code_review 前一刻，让 step() 走 gate 流程。
	d.PendingGate = ""
	d.CurrentStage = "unit_test"
	require.NoError(t, st.UpdateDelivery(context.Background(), d))
	require.NoError(t, e.Continue(ctx, d.ID)) // unit_test 过 → code_review 门禁（前置审查本机停车）
	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, 1, countEventsOfType(t, st, d.ID, "local_stage_pending"))

	// 门禁挂起后再驱动：run() 见 PendingGate 即停（零事件）。
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, 1, countEventsOfType(t, st, d.ID, "local_stage_pending"))

	// 打回 → code_gen 重跑 → unit_test → 再进 code_review：新停车要重新发。
	require.NoError(t, e.Reject(ctx, d.ID, "边界遗漏"))
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
	require.Equal(t, 2, countEventsOfType(t, st, d.ID, "local_stage_pending"))
}

// TestMergeQueuedEmittedOncePerChild：冲突暂停期反复进入 mergeLoop（其它子需求完成、
// 重启恢复都会再进），排队事件每个子需求只发一条——重发无信息量，只刷屏。
func TestMergeQueuedEmittedOncePerChild(t *testing.T) {
	e, st, proj := newRealEnv(t)
	ctx := context.Background()

	parent := &store.Delivery{ProjectID: proj.ID, Title: "父需求", Status: StatusActive, CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	require.NoError(t, e.Start(ctx, parent.ID))
	require.NoError(t, approve(ctx, e, parent.ID, store.ApproveOpts{Complexity: ComplexityLarge}))
	require.NoError(t, e.Continue(ctx, parent.ID)) // → design_approval
	_, err := e.Approve(ctx, parent.ID, store.ApproveOpts{Split: []store.ChildSpec{
		{Title: "子A", Wave: 1}, {Title: "子B", Wave: 1},
	}})
	require.NoError(t, err)
	children, err := st.ListChildDeliveries(ctx, parent.ID)
	require.NoError(t, err)
	a, b := children[0], children[1]

	// 子 A 先合入；子 B 改同一行 → 冲突。
	pushChildBranch(t, proj, a.ID, "same.txt", "from A\n")
	completeChild(t, st, a.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	pushChildBranch(t, proj, b.ID, "same.txt", "from B\n")
	completeChild(t, st, b.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	require.Equal(t, MergeStateConflict, get(t, st, parent.ID).MergeState)

	// 冲突暂停期反复驱动（子需求完成 / 重启恢复都会再进 mergeLoop）：
	// B 的排队事件只发一条。
	e.MaybeDriveParent(ctx, parent.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	require.Equal(t, 1, countEventsOfType(t, st, parent.ID, "merge_queued"))
}
