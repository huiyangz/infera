package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// TestParseTasksBlock：infera-tasks fenced block 解析（畸形块容错 → nil）。
func TestParseTasksBlock(t *testing.T) {
	withBlock := "任务清单：\n\n```infera-tasks\n[{\"title\":\"任务A\",\"detail\":\"做 A\"},{\"title\":\"任务B\",\"detail\":\"做 B，验收 b\"}]\n```\n"
	got := ParseTasksBlock(withBlock)
	require.Equal(t, []store.TaskSpec{
		{Title: "任务A", Detail: "做 A"},
		{Title: "任务B", Detail: "做 B，验收 b"},
	}, got)

	// 无块 → nil。
	require.Nil(t, ParseTasksBlock("# 只有正文，没有块"))
	// 坏 JSON → nil。
	require.Nil(t, ParseTasksBlock("```infera-tasks\n{not json}\n```"))
	// 非数组 JSON → nil。
	require.Nil(t, ParseTasksBlock("```infera-tasks\n{\"title\":\"x\"}\n```"))
	// 空数组 / 全空标题 → nil（按无可执行清单处理）。
	require.Nil(t, ParseTasksBlock("```infera-tasks\n[]\n```"))
	require.Nil(t, ParseTasksBlock("```infera-tasks\n[{\"title\":\"\",\"detail\":\"x\"}]\n```"))
	// 空标题条目被过滤，其余保留。
	require.Equal(t, []store.TaskSpec{{Title: "任务A", Detail: "做 A"}},
		ParseTasksBlock("```infera-tasks\n[{\"title\":\"  \",\"detail\":\"垃圾\"},{\"title\":\"任务A\",\"detail\":\"做 A\"}]\n```"))
	// 多行 pretty JSON 也能解析。
	require.Equal(t, []store.TaskSpec{{Title: "T", Detail: "D"}},
		ParseTasksBlock("```infera-tasks\n[\n  {\"title\": \"T\", \"detail\": \"D\"}\n]\n```"))
}

// TestTasksStageStoresParsedJSON：tasks agent 的 fenced block 解析为 JSON 落盘
// （kind=tasks 的 content 恒为清单 JSON）；畸形块容错为空清单 "[]"，照常进门。
func TestTasksStageStoresParsedJSON(t *testing.T) {
	t.Run("block parsed to json", func(t *testing.T) {
		e, st, _, _ := newEnv(t, passTR{})
		d := driveToTasksApproval(t, e, st)
		require.Equal(t, "tasks_approval", get(t, st, d.ID).PendingGate)
		var got []store.TaskSpec
		require.NoError(t, json.Unmarshal([]byte(artifactByKind(t, st, d.ID, "tasks").Content), &got))
		require.Equal(t, []store.TaskSpec{{Title: "任务A", Detail: "做 A"}, {Title: "任务B", Detail: "做 B"}}, got)
	})

	t.Run("malformed block tolerates as empty list", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		ar.tasksOutput = "任务清单（无块）"
		d := driveToTasksApproval(t, e, st)
		require.Equal(t, "tasks_approval", get(t, st, d.ID).PendingGate, "畸形块照常进门（人可覆盖/打回）")
		require.Equal(t, "[]", artifactByKind(t, st, d.ID, "tasks").Content)
	})
}

// codeGenCalls 过出 code_gen 角色的调用序号（prompt 断言用）。
func codeGenCalls(ar *fakeRunner) []int {
	out := []int{}
	for i, c := range ar.calls {
		if c.Role == "code_gen" {
			out = append(out, i)
		}
	}
	return out
}

// taskDoneContents 全部 task_done artifact 的 content 列表（按落盘顺序）。
func taskDoneContents(t *testing.T, st *store.Memory, deliveryID string) []string {
	t.Helper()
	arts, err := st.ListArtifacts(context.Background(), deliveryID)
	require.NoError(t, err)
	out := []string{}
	for _, a := range arts {
		if a.Kind == "task_done" {
			out = append(out, a.Content)
		}
	}
	return out
}

// TestCodeGenPerTaskLoop：有 tasks artifact 的交付逐任务实现——每任务一次 agent 调用
// （prompt = 基底 + 当前任务 i/N + 标题/详情），task_done artifact/事件齐落，
// 全部完成存合成 summary 后前进。
func TestCodeGenPerTaskLoop(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	ctx := context.Background()
	d := driveToTasksApproval(t, e, st)

	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → test_gen
	require.NoError(t, e.Continue(ctx, d.ID))                      // → code_review 门

	// 全序角色：spec → design → tasks → test_gen → code_gen ×2（逐任务）→ code_review → 双道审查。
	require.Equal(t, []string{"spec", "design", "tasks", "test_gen", "code_gen", "code_gen", "code_review", "spec_conformance", "code_quality"}, ar.roles())
	cg := codeGenCalls(ar)
	require.Len(t, cg, 2)
	require.Contains(t, ar.calls[cg[0]].Prompt, "当前任务 1/2：任务A")
	require.Contains(t, ar.calls[cg[0]].Prompt, "任务详情：做 A")
	require.Contains(t, ar.calls[cg[1]].Prompt, "当前任务 2/2：任务B")
	require.Contains(t, ar.calls[cg[1]].Prompt, "任务详情：做 B")
	for _, i := range cg {
		require.Contains(t, ar.calls[i].Prompt, "# 规格正文", "任务 prompt 携带规格基底")
		require.NotContains(t, ar.calls[i].Prompt, "上一轮反馈", "无反馈时反馈行省略")
	}

	// task_done 标记与事件：两任务各一条。
	require.Equal(t, []string{"1", "2"}, taskDoneContents(t, st, d.ID))
	require.Equal(t, 2, countEvents(t, st, d.ID, "task_done"))

	// 全部完成：合成 summary 落盘后前进。
	require.Equal(t, "按任务清单完成 2 项实现：任务A、任务B", artifactByKind(t, st, d.ID, "summary").Content)
	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)
}

// TestUnitTestLoopWithTasks：unit_test 失败回环——任务全部完成时由单次修复调用
// 承载失败反馈（不做任务级重跑）；修复后通过、FailCount 清零；审查打回同样走修复调用。
func TestUnitTestLoopWithTasks(t *testing.T) {
	tr := &seqTR{fail: 1} // 第 1 次 unit_test 失败，之后通过
	e, st, _, ar := newEnv(t, tr)
	ctx := context.Background()
	d := driveToTasksApproval(t, e, st)

	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.NoError(t, e.Continue(ctx, d.ID)) // 逐任务（2 次）→ unit_test 失败 #1 → 回环 code_gen
	got := get(t, st, d.ID)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Equal(t, 1, got.FailCount)

	require.NoError(t, e.Start(ctx, d.ID)) // 修复调用 → unit_test 过 → code_review 门
	got = get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, 0, got.FailCount)

	cg := codeGenCalls(ar)
	require.Len(t, cg, 3, "2 次任务 + 1 次修复")
	fix := ar.calls[cg[2]]
	require.Contains(t, fix.Prompt, "上一轮 unit_test 未过：")
	require.Contains(t, fix.Prompt, "--- FAIL: TestAdd")
	require.NotContains(t, fix.Prompt, "当前任务", "修复调用不做任务定位")
	// task_done 进度持久：仍是两任务各一条（修复不新增）。
	require.Equal(t, []string{"1", "2"}, taskDoneContents(t, st, d.ID))

	// 审查打回 → code_gen 重入：无剩余任务 + 打回意见 → 修复调用承载意见。
	require.NoError(t, e.Reject(ctx, d.ID, "边界遗漏"))
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
	cg = codeGenCalls(ar)
	require.Len(t, cg, 4)
	require.Contains(t, ar.calls[cg[3]].Prompt, "人打回：边界遗漏")
	require.NotContains(t, ar.calls[cg[3]].Prompt, "当前任务")
}

// TestCodeGenResumesOnlyRemainingTasks：task_done 持久进度——中断恢复 / 回环重入时
// 只重跑剩余任务（已完成的任务不再调用 agent）。
func TestCodeGenResumesOnlyRemainingTasks(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	ctx := context.Background()
	parent := seed(t, st) // 复用建项目/交付，随后改造成「已部分完成任务」的老状态
	d := get(t, st, parent.ID)
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "tasks", Kind: "tasks",
		Content: `[{"title":"任务A","detail":"做 A"},{"title":"任务B","detail":"做 B"},{"title":"任务C","detail":"做 C"}]`,
	}))
	for _, idx := range []string{"1", "2"} { // 模拟中断前已完成任务 1、2
		require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
			DeliveryID: d.ID, Stage: "code_gen", Kind: "task_done", Content: idx,
		}))
	}
	d.CurrentStage = "code_gen"
	d.WorkspaceReady = true // 已有 workdir（Acquire 幂等由 WorkspaceReady 守卫）
	require.NoError(t, st.UpdateDelivery(ctx, d))

	require.NoError(t, e.Start(ctx, d.ID))

	// 只跑任务 3：code_gen 一次调用 + 预审一次，prompt 定位 3/3。
	cg := codeGenCalls(ar)
	require.Len(t, cg, 1)
	require.Contains(t, ar.calls[cg[0]].Prompt, "当前任务 3/3：任务C")
	require.Equal(t, []string{"1", "2", "3"}, taskDoneContents(t, st, d.ID))
	require.Equal(t, "按任务清单完成 3 项实现：任务A、任务B、任务C", artifactByKind(t, st, d.ID, "summary").Content)
	require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
}

// TestCodeGenWithoutTasksSingleShot：无 tasks artifact（老数据）与空清单（"[]"）
// 都走单次整体实现，行为与旧路径一致（summary = agent 输出原文）。
func TestCodeGenWithoutTasksSingleShot(t *testing.T) {
	ctx := context.Background()

	t.Run("no tasks artifact", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		d := seed(t, st)
		require.NoError(t, e.Start(ctx, d.ID))
		require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // small → test_gen
		require.NoError(t, e.Continue(ctx, d.ID))
		require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
		require.Len(t, codeGenCalls(ar), 1)
		require.Equal(t, "改了 2 个文件", artifactByKind(t, st, d.ID, "summary").Content)
		require.Empty(t, taskDoneContents(t, st, d.ID))
	})

	t.Run("empty task list", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		d := seed(t, st)
		require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
			DeliveryID: d.ID, Stage: "tasks", Kind: "tasks", Content: "[]",
		}))
		d.CurrentStage = "code_gen"
		d.WorkspaceReady = true
		require.NoError(t, st.UpdateDelivery(ctx, d))
		require.NoError(t, e.Start(ctx, d.ID))
		require.Len(t, codeGenCalls(ar), 1)
		require.Equal(t, "改了 2 个文件", artifactByKind(t, st, d.ID, "summary").Content)
		require.Empty(t, taskDoneContents(t, st, d.ID))
	})
}

// countEvents 统计指定类型事件数。
func countEvents(t *testing.T, st *store.Memory, deliveryID, eventType string) int {
	t.Helper()
	n := 0
	for _, et := range eventTypes(t, st, deliveryID) {
		if et == eventType {
			n++
		}
	}
	return n
}

// TestApproveTasksOverride：任务门批准带 {"tasks":[...]} 覆盖清单（同 split 编辑器
// 模式）——覆盖清单落为新 tasks artifact + tasks_overridden 事件，放行后 code_gen
// 按覆盖后的清单逐任务实现。
func TestApproveTasksOverride(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	ctx := context.Background()
	d := driveToTasksApproval(t, e, st)

	override := []store.TaskSpec{{Title: "人工任务一", Detail: "人工拆的"}, {Title: "人工任务二", Detail: ""}, {Title: "人工任务三", Detail: "第三个"}}
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Tasks: override}))

	// 覆盖清单落为最新 tasks artifact；事件落盘；门禁推进。
	require.Equal(t, `[{"title":"人工任务一","detail":"人工拆的"},{"title":"人工任务二","detail":""},{"title":"人工任务三","detail":"第三个"}]`,
		artifactByKind(t, st, d.ID, "tasks").Content)
	require.Equal(t, 1, countEvents(t, st, d.ID, "tasks_overridden"))
	got := get(t, st, d.ID)
	require.Equal(t, "test_gen", got.CurrentStage)
	require.Empty(t, got.PendingGate)

	// 放行后逐任务实现的是覆盖后的清单。
	require.NoError(t, e.Continue(ctx, d.ID))
	require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)
	cg := codeGenCalls(ar)
	require.Len(t, cg, 3)
	require.Contains(t, ar.calls[cg[0]].Prompt, "当前任务 1/3：人工任务一")
	require.Contains(t, ar.calls[cg[2]].Prompt, "当前任务 3/3：人工任务三")
	require.Equal(t, "按任务清单完成 3 项实现：人工任务一、人工任务二、人工任务三",
		artifactByKind(t, st, d.ID, "summary").Content)
}

// TestApproveTasksOverrideGateValidation：tasks 覆盖只对 tasks_approval 门生效；
// 空标题报错。两种失败都不消费门禁。
func TestApproveTasksOverrideGateValidation(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	ctx := context.Background()

	// spec_approval 门带 tasks → 报错，门禁不被消费。
	d1 := driveToSpecApproval(t, e, st)
	_, err := e.Approve(ctx, d1.ID, store.ApproveOpts{Tasks: []store.TaskSpec{{Title: "t", Detail: "d"}}})
	require.ErrorContains(t, err, "only allowed at tasks_approval")
	require.Equal(t, "spec_approval", get(t, st, d1.ID).PendingGate)

	// design_approval 门同理。
	d2 := driveToDesignApproval(t, e, st)
	_, err = e.Approve(ctx, d2.ID, store.ApproveOpts{Tasks: []store.TaskSpec{{Title: "t", Detail: "d"}}})
	require.ErrorContains(t, err, "only allowed at tasks_approval")
	require.Equal(t, "design_approval", get(t, st, d2.ID).PendingGate)

	// tasks_approval 门带空标题 → 报错，门禁不被消费。
	d3 := driveToTasksApproval(t, e, st)
	_, err = e.Approve(ctx, d3.ID, store.ApproveOpts{Tasks: []store.TaskSpec{{Title: "  ", Detail: "d"}}})
	require.ErrorContains(t, err, "empty title")
	require.Equal(t, "tasks_approval", get(t, st, d3.ID).PendingGate)
	require.NotContains(t, eventTypes(t, st, d3.ID), "tasks_overridden")
}

// TestSubDeliveryParentContextInjection：子需求（parent_id 非空）跑 spec 时，
// prompt 注入父的 spec + design artifact 作为约束段（"作为约束参考，不要重写"）；
// 普通需求不注入；父暂无产物时也不注入。
func TestSubDeliveryParentContextInjection(t *testing.T) {
	ctx := context.Background()

	newChild := func(t *testing.T, st *store.Memory, parentID string) *store.Delivery {
		t.Helper()
		d := seed(t, st) // 独立项目/交付，改造为子需求
		d.ParentID = parentID
		require.NoError(t, st.UpdateDelivery(ctx, d))
		return d
	}

	t.Run("child spec prompt carries parent spec+design", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		parent := seed(t, st)
		require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{DeliveryID: parent.ID, Stage: "spec", Kind: "spec", Content: "# 父规格正文"}))
		require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{DeliveryID: parent.ID, Stage: "design", Kind: "design", Content: "# 父设计正文"}))
		child := newChild(t, st, parent.ID)

		require.NoError(t, e.Start(ctx, child.ID))
		require.Equal(t, "spec_approval", get(t, st, child.ID).PendingGate)
		prompt := ar.calls[0].Prompt
		require.Contains(t, prompt, "以下是父需求规格/设计，作为约束参考，不要重写")
		require.Contains(t, prompt, "父需求规格：\n# 父规格正文")
		require.Contains(t, prompt, "父需求设计：\n# 父设计正文")
	})

	t.Run("only spec when parent has no design", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		parent := seed(t, st)
		require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{DeliveryID: parent.ID, Stage: "spec", Kind: "spec", Content: "# 只有规格"}))
		child := newChild(t, st, parent.ID)

		require.NoError(t, e.Start(ctx, child.ID))
		prompt := ar.calls[0].Prompt
		require.Contains(t, prompt, "父需求规格：\n# 只有规格")
		require.NotContains(t, prompt, "父需求设计")
	})

	t.Run("no injection for non-child and empty parent", func(t *testing.T) {
		e, st, _, ar := newEnv(t, passTR{})
		plain := seed(t, st)
		require.NoError(t, e.Start(ctx, plain.ID))
		require.NotContains(t, ar.calls[0].Prompt, "父需求")

		emptyParent := seed(t, st) // 父尚无任何产物
		child := newChild(t, st, emptyParent.ID)
		require.NoError(t, e.Start(ctx, child.ID))
		require.NotContains(t, ar.calls[1].Prompt, "父需求")
	})
}

// TestTasksPromptIncludesDesign：tasks agent 吃规格(+设计)——大需求路径 tasks prompt
// 注入设计文档作为约束参考。
func TestTasksPromptIncludesDesign(t *testing.T) {
	e, st, _, ar := newEnv(t, passTR{})
	driveToTasksApproval(t, e, st)

	prompt := ar.calls[2].Prompt // spec, design, tasks
	require.Contains(t, prompt, "# 规格正文")
	require.Contains(t, prompt, "设计文档（约束参考）：\n# 设计正文")
}

