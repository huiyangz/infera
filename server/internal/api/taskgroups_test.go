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
		MulticaIssueID: "mi-9", MulticaIssueKey: "INFERA-9", Assignee: "小鱼儿", Priority: "high"}
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
		"merge_state", "complexity", "multica_issue_id", "multica_issue_key", "assignee", "priority",
		"multica_synced_at", "created_at", "updated_at"}
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
	require.Equal(t, "INFERA-9", synced["multica_issue_key"])
	require.Equal(t, "小鱼儿", synced["assignee"])
	require.Equal(t, "high", synced["priority"])

	// 子任务行键集冻结（AC：子任务带 stage 与 status 字段）。
	require.ElementsMatch(t,
		[]string{"id", "title", "stage", "status", "current_stage", "pending_gate", "multica_issue_id",
			"multica_issue_key", "assignee", "priority", "created_at", "updated_at"},
		keys(first))

	// 普通需求：无子任务 → stages 空数组（非 null）、计数 0。
	require.Equal(t, []any{}, rows[1]["stages"])
	require.Equal(t, float64(0), rows[1]["child_total"])
	require.Equal(t, float64(0), rows[1]["child_completed"])
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
