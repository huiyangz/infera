package store

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// childProgressFixture 混合状态、多阶段的标准铺数（L202608260142-1-T01）：
// 阶段 1 全部完结（completed×2）、阶段 2 混态（active 无门禁 / active 停门禁 /
// blocked / queued）、阶段 3 待启动（queued）、无阶段组 1 条 cancelled。
// 预期：active_stage=2（最小未完结编号阶段）、总体 8 条 done=2。
func childProgressFixture() []Delivery {
	return []Delivery{
		{ID: "c1", ParentID: "p1", Wave: 1, Status: "completed", CurrentStage: "code_review"},
		{ID: "c2", ParentID: "p1", Wave: 1, Status: "completed", CurrentStage: "code_review"},
		{ID: "c3", ParentID: "p1", Wave: 2, Status: "active", CurrentStage: "code_gen"},
		{ID: "c4", ParentID: "p1", Wave: 2, Status: "active", CurrentStage: "spec", PendingGate: "spec_approval"},
		{ID: "c5", ParentID: "p1", Wave: 2, Status: "blocked", CurrentStage: "unit_test"},
		{ID: "c6", ParentID: "p1", Wave: 2, Status: "queued", CurrentStage: "intake"},
		{ID: "c7", ParentID: "p1", Wave: 3, Status: "queued", CurrentStage: "intake"},
		{ID: "c8", ParentID: "p1", Wave: 0, Status: "cancelled", CurrentStage: "intake"},
	}
}

func TestAssembleChildProgressMixedStages(t *testing.T) {
	got := AssembleChildProgress("p1", childProgressFixture())

	require.Equal(t, "p1", got.DeliveryID)
	require.Equal(t, 8, got.Total, "全部子任务计入 total")
	require.Equal(t, 2, got.Done)
	require.Equal(t, 1, got.InProgress, "active 且未停门禁")
	require.Equal(t, 1, got.InReview, "active 且停在门禁待人工验收")
	require.Equal(t, 1, got.Blocked)
	require.Equal(t, 2, got.Todo)
	require.Equal(t, 1, got.Cancelled)
	require.Equal(t, map[string]int{
		"active": 2, "queued": 2, "completed": 2, "blocked": 1, "cancelled": 1,
	}, got.ByStatus, "原始状态恒含五个固定键")

	// 阶段屏障语义：最小编号阶段已全部完结 → 活跃阶段推进到 2。
	require.NotNil(t, got.ActiveStage)
	require.Equal(t, 2, *got.ActiveStage)

	// 分组顺序：编号阶段升序，无阶段（0）垫底。
	require.Len(t, got.Stages, 4)
	require.Equal(t, []int{1, 2, 3, 0}, []int{got.Stages[0].Stage, got.Stages[1].Stage, got.Stages[2].Stage, got.Stages[3].Stage})
}

func TestAssembleChildProgressStageGroups(t *testing.T) {
	got := AssembleChildProgress("p1", childProgressFixture())

	s1, s2, s3, s0 := got.Stages[0], got.Stages[1], got.Stages[2], got.Stages[3]

	require.Equal(t, 2, s1.Total)
	require.Equal(t, 2, s1.Done)
	require.Equal(t, map[string]int{"active": 0, "queued": 0, "completed": 2, "blocked": 0, "cancelled": 0}, s1.ByStatus)

	require.Equal(t, 4, s2.Total)
	require.Equal(t, 0, s2.Done)
	require.Equal(t, 1, s2.InProgress)
	require.Equal(t, 1, s2.InReview, "组内 in_review 也按停门禁拆分")
	require.Equal(t, 1, s2.Blocked)
	require.Equal(t, 1, s2.Todo)
	require.Equal(t, map[string]int{"active": 2, "queued": 1, "completed": 0, "blocked": 1, "cancelled": 0}, s2.ByStatus)

	require.Equal(t, 1, s3.Total)
	require.Equal(t, 1, s3.Todo)
	require.Equal(t, 0, s3.Done)

	require.Equal(t, 1, s0.Total, "无阶段子任务单独成组")
	require.Equal(t, 1, s0.Cancelled)
}

// TestAssembleChildProgressEveryStatus 每个状态值至少一条子任务，逐一核验
// 归并口径（单一阶段内铺满，规避分组影响）。
func TestAssembleChildProgressEveryStatus(t *testing.T) {
	children := []Delivery{
		{ID: "c1", Wave: 1, Status: "queued"},
		{ID: "c2", Wave: 1, Status: "active"},
		{ID: "c3", Wave: 1, Status: "active", PendingGate: "tasks_approval"},
		{ID: "c4", Wave: 1, Status: "blocked"},
		{ID: "c5", Wave: 1, Status: "completed"},
		{ID: "c6", Wave: 1, Status: "cancelled"},
	}
	got := AssembleChildProgress("p1", children)

	require.Equal(t, 6, got.Total)
	require.Equal(t, 1, got.Todo, "queued → 待办")
	require.Equal(t, 1, got.InProgress, "active 无门禁 → 进行中")
	require.Equal(t, 1, got.InReview, "active 停门禁 → 待验收")
	require.Equal(t, 1, got.Blocked, "blocked → 阻塞")
	require.Equal(t, 1, got.Done, "completed → 已完成")
	require.Equal(t, 1, got.Cancelled, "cancelled → 已取消")
	require.Equal(t, got.ByStatus["active"], got.InProgress+got.InReview, "in_progress+in_review 恰好拆完 active")
}

// TestAssembleChildProgressEmpty 无子任务：零计数、空分组（非 null）、
// 无活跃阶段。
func TestAssembleChildProgressEmpty(t *testing.T) {
	got := AssembleChildProgress("p1", nil)

	require.Equal(t, "p1", got.DeliveryID)
	require.Equal(t, 0, got.Total)
	require.Equal(t, 0, got.Done)
	require.Equal(t, 0, got.InProgress)
	require.Equal(t, 0, got.InReview)
	require.Equal(t, 0, got.Blocked)
	require.Equal(t, 0, got.Todo)
	require.Equal(t, 0, got.Cancelled)
	require.Equal(t, map[string]int{"active": 0, "queued": 0, "completed": 0, "blocked": 0, "cancelled": 0}, got.ByStatus)
	require.Nil(t, got.ActiveStage)
	require.NotNil(t, got.Stages, "空分组输出空数组（非 null）")
	require.Empty(t, got.Stages)
}

// TestAssembleChildProgressAllTerminal 全部子任务终态（completed/cancelled）
// → 无活跃阶段。
func TestAssembleChildProgressAllTerminal(t *testing.T) {
	got := AssembleChildProgress("p1", []Delivery{
		{ID: "c1", Wave: 1, Status: "completed"},
		{ID: "c2", Wave: 2, Status: "cancelled"},
	})
	require.Nil(t, got.ActiveStage)
	require.Equal(t, 1, got.Done)
	require.Equal(t, 1, got.Cancelled)
}

// TestAssembleChildProgressStageZeroNotActiveStage 无阶段（wave 0）子任务
// 即使未完结也不构成「活跃阶段」——与引擎批次调度跳过 wave<=0 的口径一致。
func TestAssembleChildProgressStageZeroNotActiveStage(t *testing.T) {
	got := AssembleChildProgress("p1", []Delivery{
		{ID: "c1", Wave: 0, Status: "active"},
		{ID: "c2", Wave: 0, Status: "queued"},
	})
	require.Nil(t, got.ActiveStage, "无编号阶段 → 无活跃阶段")
	require.Equal(t, 1, got.InProgress, "未完结工作仍计入计数")
	require.Len(t, got.Stages, 1)
	require.Equal(t, 0, got.Stages[0].Stage)
}

// TestAssembleChildProgressUnknownStatus 未知状态：计入 total，不进五键、
// 不进任何归并桶（与 WorkspaceStats 口径一致）。
func TestAssembleChildProgressUnknownStatus(t *testing.T) {
	got := AssembleChildProgress("p1", []Delivery{
		{ID: "c1", Wave: 1, Status: "completed"},
		{ID: "c2", Wave: 1, Status: "weird"},
	})
	require.Equal(t, 2, got.Total)
	require.Equal(t, map[string]int{"active": 0, "queued": 0, "completed": 1, "blocked": 0, "cancelled": 0}, got.ByStatus)
	require.Equal(t, 1, got.Done)
	require.Equal(t, 0, got.InProgress+got.InReview+got.Blocked+got.Todo+got.Cancelled)
	require.NotNil(t, got.ActiveStage, "未知状态非终态，阶段仍未完结")
}

// TestAssembleChildProgressJSONShape 冻结契约：顶层与阶段行的 JSON 键集合。
func TestAssembleChildProgressJSONShape(t *testing.T) {
	raw, err := json.Marshal(AssembleChildProgress("p1", childProgressFixture()))
	require.NoError(t, err)

	var top map[string]any
	require.NoError(t, json.Unmarshal(raw, &top))
	require.ElementsMatch(t, []string{
		"delivery_id", "total", "done", "in_progress", "in_review", "blocked",
		"todo", "cancelled", "by_status", "active_stage", "stages",
	}, keysOf(top))

	var stages []map[string]any
	require.NoError(t, json.Unmarshal(raw, &struct {
		Stages *[]map[string]any `json:"stages"`
	}{Stages: &stages}))
	require.NotEmpty(t, stages)
	require.ElementsMatch(t, []string{
		"stage", "total", "done", "in_progress", "in_review", "blocked",
		"todo", "cancelled", "by_status",
	}, keysOf(stages[0]))
}

// keysOf map 键集合（api 层同名 helper 不导出，store 测试自带一份）。
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
