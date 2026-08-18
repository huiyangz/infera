package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// --- 复杂度分岔 ---

// TestApproveComplexityFork：spec 门裁复杂度——small 直跳 test_gen、large 进 design，
// 老数据 ”（无裁定无建议）按 small 走；complexity_set 事件随裁定落盘。
func TestApproveComplexityFork(t *testing.T) {
	ctx := context.Background()

	t.Run("explicit small", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		d := driveToSpecApproval(t, e, st)
		require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Complexity: ComplexitySmall}))
		got := get(t, st, d.ID)
		require.Equal(t, "test_gen", got.CurrentStage)
		require.Equal(t, ComplexitySmall, got.Complexity)
		require.Empty(t, got.PendingGate)
		// 小需求不进设计/任务阶段。
		require.Equal(t, []string{"spec"}, ar.roles())
		require.Contains(t, eventTypes(t, st, d.ID), "complexity_set")
	})

	t.Run("explicit large", func(t *testing.T) {
		e, st, _, _ := newEnv(t, passTR{})
		d := driveToSpecApproval(t, e, st)
		require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Complexity: ComplexityLarge}))
		got := get(t, st, d.ID)
		require.Equal(t, "design", got.CurrentStage, "large → design")
		require.Equal(t, ComplexityLarge, got.Complexity)
	})

	t.Run("suggestion fallback", func(t *testing.T) {
		// 无人工裁定：取 spec 末尾 infera-complexity 块的建议。
		e, st, _, _ := newEnv(t, passTR{})
		e.ar = &fakeRunner{specOutput: "# 规格\n\n```infera-complexity\nlarge\n```\n"}
		d := driveToSpecApproval(t, e, st)
		require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
		got := get(t, st, d.ID)
		require.Equal(t, "design", got.CurrentStage, "建议 large → design")
		require.Equal(t, ComplexityLarge, got.Complexity)
	})

	t.Run("old data empty complexity walks small", func(t *testing.T) {
		// 老数据：complexity ''、spec 无建议块 → 批准后按 small 走 test_gen。
		e, st, _, _ := newEnv(t, passTR{})
		d := driveToSpecApproval(t, e, st)
		require.Empty(t, get(t, st, d.ID).Complexity)
		require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
		got := get(t, st, d.ID)
		require.Equal(t, "test_gen", got.CurrentStage)
		require.Equal(t, ComplexitySmall, got.Complexity)
	})

	t.Run("invalid value rejected", func(t *testing.T) {
		e, st, _, _ := newEnv(t, passTR{})
		d := driveToSpecApproval(t, e, st)
		_, err := e.Approve(ctx, d.ID, store.ApproveOpts{Complexity: "huge"})
		require.ErrorContains(t, err, "invalid complexity")
		got := get(t, st, d.ID)
		require.Equal(t, "spec_approval", got.PendingGate, "非法值不消费门禁")
		require.Empty(t, got.Complexity)
	})

	t.Run("complexity only at spec_approval", func(t *testing.T) {
		e, st, _, _ := newEnv(t, passTR{})
		d := driveToSpecApproval(t, e, st)
		require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Complexity: ComplexitySmall}))
		require.NoError(t, e.Continue(ctx, d.ID))
		require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
		_, err := e.Approve(ctx, d.ID, store.ApproveOpts{Complexity: ComplexitySmall})
		require.ErrorContains(t, err, "only allowed at spec_approval")
		require.Equal(t, "code_review", get(t, st, d.ID).PendingGate, "错门不消费门禁")
	})
}

// TestLargePipelineThroughTasksGate：大需求全门链路——
// spec → design → tasks → test_gen → code_gen → unit_test → code_review → 完成，
// 各阶段产物齐全、design prompt 带规格、complexity_set 落事件。
func TestLargePipelineThroughTasksGate(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	ctx := context.Background()
	d := driveToDesignApproval(t, e, st)

	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → tasks
	require.NoError(t, e.Continue(ctx, d.ID))                      // tasks → tasks_approval
	require.Equal(t, "tasks_approval", get(t, st, d.ID).PendingGate)
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → test_gen
	require.NoError(t, e.Continue(ctx, d.ID))                      // → code_review 门（预审）
	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)

	// 全序角色：spec → design → tasks → test_gen → code_gen ×2（逐任务）→ code_review。
	// 全序角色：spec → design → tasks → test_gen → code_gen ×2（逐任务）→ code_review → 双道审查。
	require.Equal(t, []string{"spec", "design", "tasks", "test_gen", "code_gen", "code_gen", "code_review", "spec_conformance", "code_quality"}, ar.roles())
	// 产物：design/tests 逐门落盘；tasks 为解析后的清单 JSON；summary 为任务完成合成摘要。
	require.Equal(t, "# 设计正文", artifactByKind(t, st, d.ID, "design").Content)
	require.Equal(t, `[{"title":"任务A","detail":"做 A"},{"title":"任务B","detail":"做 B"}]`, artifactByKind(t, st, d.ID, "tasks").Content)
	require.NotNil(t, artifactByKind(t, st, d.ID, "tests"))
	require.Equal(t, "按任务清单完成 2 项实现：任务A、任务B", artifactByKind(t, st, d.ID, "summary").Content)
	require.Contains(t, eventTypes(t, st, d.ID), "task_done")
	// design prompt 带规格正文；tasks prompt 带上一轮（design 门）无反馈但有规格。
	require.Contains(t, ar.calls[1].Prompt, "# 规格正文")
	require.Contains(t, ar.calls[2].Prompt, "# 规格正文")

	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → DONE
	require.Equal(t, StatusCompleted, get(t, st, d.ID).Status)
	types := eventTypes(t, st, d.ID)
	require.Contains(t, types, "complexity_set")
	require.Contains(t, types, "delivery_completed")
}

// --- 打回路径 ---

// TestRejectDesignApprovalLoopsToDesign：设计门打回 → 回 design 重写；
// 重跑 prompt 带人打回意见（消费一次后清空），再次停设计门。拆与不拆同路径（RejectTo 静态）。
func TestRejectDesignApprovalLoopsToDesign(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	ctx := context.Background()
	d := driveToDesignApproval(t, e, st)

	require.NoError(t, e.Reject(ctx, d.ID, "模块边界不清"))
	got := get(t, st, d.ID)
	require.Equal(t, "design", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, "模块边界不清", got.RejectReason)
	require.Contains(t, eventTypes(t, st, d.ID), "gate_rejected")

	require.NoError(t, e.Continue(ctx, d.ID)) // design 重写 → 设计门
	got = get(t, st, d.ID)
	require.Equal(t, "design_approval", got.PendingGate)
	require.Empty(t, got.RejectReason, "意见消费一次后清空")
	require.Len(t, ar.roles(), 3) // spec, design, design(重写)
	require.Contains(t, ar.calls[2].Prompt, "人打回：模块边界不清")
}

// TestRejectTasksApprovalLoopsToTasks：任务门打回 → 回 tasks 重列，意见注入重跑 prompt。
func TestRejectTasksApprovalLoopsToTasks(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	ctx := context.Background()
	d := driveToTasksApproval(t, e, st)

	require.NoError(t, e.Reject(ctx, d.ID, "任务粒度太粗"))
	got := get(t, st, d.ID)
	require.Equal(t, "tasks", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Equal(t, "任务粒度太粗", got.RejectReason)

	require.NoError(t, e.Continue(ctx, d.ID)) // tasks 重列 → 任务门
	require.Equal(t, "tasks_approval", get(t, st, d.ID).PendingGate)
	require.Empty(t, get(t, st, d.ID).RejectReason)
	require.Contains(t, ar.calls[3].Prompt, "人打回：任务粒度太粗") // spec, design, tasks, tasks(重列)
}

// --- 图契约（冻结）：阶段枚举与序、静态下一跳 ---

// TestGraphContract：11 阶段全序 + 静态邻接 + 打回目标。
// spec_approval→design（large）与 design_approval→code_gen（拆分）是运行期分岔，
// 分别由 nextAfterGate / approveSplit 承担，不在静态 Next 里。
func TestGraphContract(t *testing.T) {
	require.Len(t, StageOrder, 11)
	require.Equal(t, []string{
		"intake", "spec", "spec_approval",
		"design", "design_approval", "tasks", "tasks_approval",
		"test_gen", "code_gen", "unit_test", "code_review",
	}, StageOrder)

	for stage, want := range map[string]Node{
		"intake":          {Kind: KindCommand, Next: "spec"},
		"spec":            {Kind: KindAgent, Next: "spec_approval", ArtifactKind: "spec"},
		"spec_approval":   {Kind: KindGate, Next: "test_gen", RejectTo: "spec"},
		"design":          {Kind: KindAgent, Next: "design_approval", ArtifactKind: "design"},
		"design_approval": {Kind: KindGate, Next: "tasks", RejectTo: "design"},
		"tasks":           {Kind: KindAgent, Next: "tasks_approval", ArtifactKind: "tasks"},
		"tasks_approval":  {Kind: KindGate, Next: "test_gen", RejectTo: "tasks"},
		"test_gen":        {Kind: KindAgent, Next: "code_gen", ArtifactKind: "tests"},
		"code_gen":        {Kind: KindAgent, Next: "unit_test", ArtifactKind: "summary"},
		"unit_test":       {Kind: KindCommand, Next: "code_review", OnFail: "code_gen"},
		"code_review":     {Kind: KindGate, Next: "DONE", ReviewRole: "code_review", RejectTo: "code_gen", Persist: true},
	} {
		got, ok := Graph[stage]
		require.True(t, ok, "stage %s missing from graph", stage)
		require.Equal(t, stage, got.Stage)
		require.Equal(t, want.Kind, got.Kind, "%s kind", stage)
		require.Equal(t, want.Next, got.Next, "%s next", stage)
		require.Equal(t, want.OnFail, got.OnFail, "%s onfail", stage)
		require.Equal(t, want.RejectTo, got.RejectTo, "%s rejectto", stage)
		require.Equal(t, want.ArtifactKind, got.ArtifactKind, "%s artifact kind", stage)
		require.Equal(t, want.ReviewRole, got.ReviewRole, "%s review role", stage)
		require.Equal(t, want.Persist, got.Persist, "%s persist", stage)
	}

	// 复杂度分岔：large→design，small 与老数据 ''→test_gen。
	large := &store.Delivery{Complexity: ComplexityLarge}
	small := &store.Delivery{Complexity: ComplexitySmall}
	legacy := &store.Delivery{Complexity: ""}
	require.Equal(t, "design", nextAfterGate(large, Graph["spec_approval"]))
	require.Equal(t, "test_gen", nextAfterGate(small, Graph["spec_approval"]))
	require.Equal(t, "test_gen", nextAfterGate(legacy, Graph["spec_approval"]))
}

// TestParseComplexitySuggestion：infera-complexity fenced block 解析（无/坏块 = 空串）。
func TestParseComplexitySuggestion(t *testing.T) {
	require.Equal(t, "large", ParseComplexitySuggestion("# 规格\n\n```infera-complexity\nlarge\n```\n"))
	require.Equal(t, "small", ParseComplexitySuggestion("```infera-complexity\nsmall\n```"))
	require.Empty(t, ParseComplexitySuggestion("# 无块规格"))
	require.Empty(t, ParseComplexitySuggestion("```infera-complexity\nhuge\n```"))
	require.Empty(t, ParseComplexitySuggestion("```infera-complexity\n\n```"))
}
