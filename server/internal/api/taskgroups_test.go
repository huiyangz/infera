package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedTaskGroups 铺数据：拆分父（阶段1 两条：一完成一进行中；阶段2 一条同步镜像排队）
// + 普通需求（无子任务）。
func seedTaskGroups(t *testing.T, st *store.Memory) (parent, w1done, w2synced, plain store.Delivery) {
	t.Helper()
	ctx := context.Background()
	p := &store.Project{Name: "任务分组项目"}
	require.NoError(t, st.CreateProject(ctx, p))

	parent = store.Delivery{ProjectID: p.ID, Title: "父需求", Status: "active", CurrentStage: "code_gen", SplitMode: true}
	require.NoError(t, st.CreateDelivery(ctx, &parent))
	w1done = store.Delivery{ProjectID: p.ID, ParentID: parent.ID, Wave: 1, Title: "子任务A", Status: "completed", CurrentStage: "unit_test"}
	require.NoError(t, st.CreateDelivery(ctx, &w1done))
	time.Sleep(2 * time.Millisecond) // 组内 created_at 升序可判
	w1active := store.Delivery{ProjectID: p.ID, ParentID: parent.ID, Wave: 1, Title: "子任务B", Status: "active", CurrentStage: "code_gen", PendingGate: "code_review"}
	require.NoError(t, st.CreateDelivery(ctx, &w1active))
	time.Sleep(2 * time.Millisecond)
	w2synced = store.Delivery{ProjectID: p.ID, ParentID: parent.ID, Wave: 2, Title: "子任务C", Status: "queued",
		ExternalIssueID: "mi-9", ExternalIssueKey: "INFERA-9", Assignee: "小鱼儿", Priority: "high"}
	require.NoError(t, st.CreateDelivery(ctx, &w2synced))
	time.Sleep(2 * time.Millisecond)
	plain = store.Delivery{ProjectID: p.ID, Title: "普通需求", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, &plain))
	return parent, w1done, w2synced, plain
}

func TestProjectTaskGroupsEndpoint(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	parent, w1done, w2synced, plain := seedTaskGroups(t, st)

	r, _ := c.Get(ts.URL + "/api/projects/" + parent.ProjectID + "/task-groups")
	require.Equal(t, 200, r.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	require.Len(t, rows, 2, "子任务不作为顶层行，顶层只有父/普通需求")

	// 顶层行键集冻结：Delivery 全字段内联 + 分组三键（parent_id 区分父子归属）。
	deliveryKeys := []string{"id", "project_id", "title", "description", "status", "current_stage", "pending_gate",
		"fail_count", "base_commit", "reject_reason", "workspace_ready", "parent_id", "wave", "split_mode",
		"merge_state", "complexity", "external_issue_id", "external_issue_key", "assignee", "priority",
		"external_synced_at", "created_at", "updated_at"}
	require.ElementsMatch(t, append(deliveryKeys, "child_total", "child_completed", "stages"), keys(rows[0]))

	// 父行在前（顶层按 created_at 升序）。
	require.Equal(t, parent.ID, rows[0]["id"])
	require.Equal(t, plain.ID, rows[1]["id"])

	// 父行：子任务计数 + 按阶段分组。
	require.Equal(t, float64(3), rows[0]["child_total"])
	require.Equal(t, float64(1), rows[0]["child_completed"])
	stages := rows[0]["stages"].([]any)
	require.Len(t, stages, 2, "两阶段各一组")
	s1 := stages[0].(map[string]any)
	require.Equal(t, float64(1), s1["stage"])
	tasks1 := s1["tasks"].([]any)
	require.Len(t, tasks1, 2)
	// 组内按 created_at 升序：A 先建。
	first := tasks1[0].(map[string]any)
	require.Equal(t, w1done.ID, first["id"])
	require.Equal(t, float64(1), first["stage"], "子任务带 stage（=所属阶段）")
	require.Equal(t, "completed", first["status"])

	// 阶段 2：同步镜像子任务，展示字段透传。
	s2 := stages[1].(map[string]any)
	require.Equal(t, float64(2), s2["stage"])
	tasks2 := s2["tasks"].([]any)
	require.Len(t, tasks2, 1)
	synced := tasks2[0].(map[string]any)
	require.Equal(t, w2synced.ID, synced["id"])
	require.Equal(t, "INFERA-9", synced["external_issue_key"])
	require.Equal(t, "小鱼儿", synced["assignee"])
	require.Equal(t, "high", synced["priority"])

	// 子任务行键集冻结（AC：子任务带 stage 与 status 字段）。
	require.ElementsMatch(t,
		[]string{"id", "title", "stage", "status", "current_stage", "pending_gate", "external_issue_id",
			"external_issue_key", "assignee", "priority", "created_at", "updated_at"},
		keys(first))

	// 普通需求：无子任务 → stages 空数组（非 null）、计数 0。
	require.Equal(t, []any{}, rows[1]["stages"])
	require.Equal(t, float64(0), rows[1]["child_total"])
	require.Equal(t, float64(0), rows[1]["child_completed"])
}

// --- AC（INFERA-146）：同父下无阶段（wave 0）子任务独立分组，排在编号阶段之后 ---

// TestBuildTaskGroupsNoStageBucketLast：同父下既有编号阶段子任务（wave 1/2）
// 又有无阶段子任务（wave 0，任务同步镜像）时：wave 0 不混进「阶段 1」，
// 独立成组且位于全部编号阶段之后；JSON 仍输出 stage:0，编号阶段顺序不变。
func TestBuildTaskGroupsNoStageBucketLast(t *testing.T) {
	parent := store.Delivery{ID: "p1", ProjectID: "prj", Title: "父", Status: "active", SplitMode: true}
	kids := []store.Delivery{
		{ID: "k-w1", ProjectID: "prj", ParentID: "p1", Wave: 1, Title: "阶段1子任务", Status: "completed", CurrentStage: "unit_test"},
		{ID: "k-w0", ProjectID: "prj", ParentID: "p1", Wave: 0, Title: "无阶段子任务", Status: "queued",
			ExternalIssueID: "mi-0", ExternalIssueKey: "INFERA-0"},
		{ID: "k-w2", ProjectID: "prj", ParentID: "p1", Wave: 2, Title: "阶段2子任务", Status: "active", CurrentStage: "code_gen"},
	}
	// 顶层平表顺序刻意打乱：分组只由 wave 决定，与入序无关。
	rows := buildTaskGroups([]store.Delivery{kids[1], parent, kids[2], kids[0]})

	require.Len(t, rows, 1, "仅父为顶层行")
	stages := rows[0].Stages
	require.Len(t, stages, 3, "三个 wave 三个组")
	require.Equal(t, 1, stages[0].Stage, "编号阶段升序在前")
	require.Equal(t, 2, stages[1].Stage)
	require.Equal(t, 0, stages[2].Stage, "无阶段（wave 0）独立分组垫底")
	require.Equal(t, []string{"k-w1"}, childIDs(stages[0]))
	require.Equal(t, []string{"k-w2"}, childIDs(stages[1]))
	require.Equal(t, []string{"k-w0"}, childIDs(stages[2]), "wave 0 子任务不混进阶段 1")
	require.Equal(t, 3, rows[0].ChildTotal, "计数照常含无阶段子任务")
	require.Equal(t, 1, rows[0].ChildCompleted)
}

// TestBuildTaskGroupsOnlyNoStageChildren：全部子任务无阶段 → 只有「无阶段」一组。
func TestBuildTaskGroupsOnlyNoStageChildren(t *testing.T) {
	parent := store.Delivery{ID: "p1", ProjectID: "prj", Title: "父", Status: "active"}
	kids := []store.Delivery{
		{ID: "k-a", ProjectID: "prj", ParentID: "p1", Wave: 0, Title: "甲", Status: "queued"},
		{ID: "k-b", ProjectID: "prj", ParentID: "p1", Wave: 0, Title: "乙", Status: "completed", CurrentStage: "unit_test"},
	}
	rows := buildTaskGroups([]store.Delivery{parent, kids[0], kids[1]})
	stages := rows[0].Stages
	require.Len(t, stages, 1)
	require.Equal(t, 0, stages[0].Stage)
	require.Equal(t, []string{"k-a", "k-b"}, childIDs(stages[0]), "组内保持创建序")
}

// childIDs 提取组内子任务 ID 序列（断言分组归属与组内顺序）。
func childIDs(g taskStageGroup) []string {
	ids := make([]string, 0, len(g.Tasks))
	for _, c := range g.Tasks {
		ids = append(ids, c.ID)
	}
	return ids
}

func TestProjectTaskGroupsNotFoundAndAuth(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, _ := c.Get(ts.URL + "/api/projects/0b7ddc6e-0000-4000-8000-000000000000/task-groups")
	require.Equal(t, 404, r.StatusCode)
	var e map[string]string
	require.NoError(t, json.NewDecoder(r.Body).Decode(&e))
	require.Equal(t, "项目不存在", e["error"])

	// 未登录 401（路由挂在认证组内）。
	r, _ = http.Get(ts.URL + "/api/projects/0b7ddc6e-0000-4000-8000-000000000000/task-groups")
	require.Equal(t, 401, r.StatusCode)
}

func TestProjectTaskGroupsEmptyIsArray(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	p := &store.Project{Name: "空项目"}
	require.NoError(t, st.CreateProject(context.Background(), p))

	r, _ := c.Get(ts.URL + "/api/projects/" + p.ID + "/task-groups")
	require.Equal(t, 200, r.StatusCode)
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	// 空结果必须是 [] 而非 null（前端可直接 .map）。
	require.Equal(t, "[]", strings.TrimSpace(string(b)))
}
