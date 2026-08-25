package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedStageRunsAPI 铺数据：项目（spec 阶段绑定 agent、code_gen 未绑）+ 两条
// 需求 + 三次运行（done → done → running 最新），外加一个空项目。
// 启动/收尾间 sleep 保证 started_at 严格递增且耗时 ≥5ms。
func seedStageRunsAPI(t *testing.T, st *store.Memory) (mainP, empty store.Project, d1, d2 store.Delivery) {
	t.Helper()
	ctx := context.Background()
	mainP = store.Project{Name: "主项目"}
	require.NoError(t, st.CreateProject(ctx, &mainP))
	empty = store.Project{Name: "空项目"}
	require.NoError(t, st.CreateProject(ctx, &empty))

	a := store.Agent{Name: "规格 agent", Runner: "local"}
	require.NoError(t, st.CreateAgent(ctx, &a))
	require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{ProjectID: mainP.ID, Node: "spec", AgentID: a.ID}))

	d1 = store.Delivery{ProjectID: mainP.ID, Title: "需求一", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, &d1))
	d2 = store.Delivery{ProjectID: mainP.ID, Title: "本地需求", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, &d2))

	start := func(d store.Delivery, stage string, attempt int) *store.StageRun {
		r := &store.StageRun{DeliveryID: d.ID, Stage: stage, Attempt: attempt, Status: "running"}
		require.NoError(t, st.StartStageRun(ctx, r))
		time.Sleep(5 * time.Millisecond)
		return r
	}
	r1 := start(d1, "spec", 1)
	require.NoError(t, st.FinishStageRun(ctx, r1.ID, "done"))
	time.Sleep(5 * time.Millisecond)
	r2 := start(d2, "code_gen", 1)
	require.NoError(t, st.FinishStageRun(ctx, r2.ID, "done"))
	time.Sleep(5 * time.Millisecond)
	_ = start(d1, "spec", 2) // 留 running：finished_at/duration_ms 为 null
	return mainP, empty, d1, d2
}

func TestProjectStageRunsEndpoint(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	mainP, empty, d1, d2 := seedStageRunsAPI(t, st)

	r, _ := c.Get(ts.URL + "/api/projects/" + mainP.ID + "/stage-runs")
	require.Equal(t, 200, r.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	// 契约冻结：顶层键集合（解码进 map 断言键集，防形状漂移）。
	require.ElementsMatch(t, []string{"project_id", "runs", "by_stage"}, keys(body))
	require.Equal(t, mainP.ID, body["project_id"])

	// 明细：started_at 倒序——running 的 spec#2 最新在前，其次 code_gen，最旧 spec#1。
	runs := body["runs"].([]any)
	require.Len(t, runs, 3)
	first := runs[0].(map[string]any)
	require.ElementsMatch(t, []string{
		"id", "delivery_id", "title", "external_issue_key", "stage", "attempt",
		"status", "agent_name", "started_at", "finished_at", "duration_ms",
	}, keys(first))
	require.Equal(t, d1.ID, first["delivery_id"])
	require.Equal(t, "需求一", first["title"])
	require.Equal(t, "spec", first["stage"])
	require.Equal(t, float64(2), first["attempt"])
	require.Equal(t, "running", first["status"])
	require.Equal(t, "规格 agent", first["agent_name"], "spec 已绑定 → agent_name 非 null")
	require.Nil(t, first["finished_at"], "running 未收尾 → finished_at null")
	require.Nil(t, first["duration_ms"], "running 未收尾 → duration_ms null")

	second := runs[1].(map[string]any)
	require.Equal(t, d2.ID, second["delivery_id"])
	require.Equal(t, "code_gen", second["stage"])
	require.Equal(t, "done", second["status"])
	require.Nil(t, second["agent_name"], "code_gen 未绑定 → agent_name null")
	require.NotNil(t, second["finished_at"])
	require.GreaterOrEqual(t, second["duration_ms"].(float64), float64(5))

	third := runs[2].(map[string]any)
	require.Equal(t, "spec", third["stage"])
	require.Equal(t, float64(1), third["attempt"])
	require.Equal(t, "done", third["status"])

	// 聚合：stage 字典序（code_gen < spec），键集与口径。
	byStage := body["by_stage"].([]any)
	require.Len(t, byStage, 2)
	codegen := byStage[0].(map[string]any)
	require.Equal(t, "code_gen", codegen["stage"])
	require.ElementsMatch(t, []string{"stage", "total", "done", "failed", "running", "avg_ms", "p95_ms"}, keys(codegen))
	require.Equal(t, float64(1), codegen["total"])
	require.Equal(t, float64(1), codegen["done"])
	require.Equal(t, float64(0), codegen["failed"])
	require.Equal(t, float64(0), codegen["running"])
	require.Greater(t, codegen["avg_ms"].(float64), float64(0))

	spec := byStage[1].(map[string]any)
	require.Equal(t, "spec", spec["stage"])
	require.Equal(t, float64(2), spec["total"])
	require.Equal(t, float64(1), spec["done"])
	require.Equal(t, float64(1), spec["running"])

	// 空项目：runs/by_stage 为空数组（JSON []）而非 null。
	r, _ = c.Get(ts.URL + "/api/projects/" + empty.ID + "/stage-runs")
	require.Equal(t, 200, r.StatusCode)
	body = map[string]any{}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.NotNil(t, body["runs"], "空项目 runs 是 [] 不是 null")
	require.Empty(t, body["runs"])
	require.NotNil(t, body["by_stage"], "空项目 by_stage 是 [] 不是 null")
	require.Empty(t, body["by_stage"])
}

func TestProjectStageRunsNotFound(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, _ := c.Get(ts.URL + "/api/projects/0b7ddc6e-0000-4000-8000-000000000000/stage-runs")
	require.Equal(t, 404, r.StatusCode)
	var e map[string]string
	require.NoError(t, json.NewDecoder(r.Body).Decode(&e))
	require.Equal(t, "项目不存在", e["error"])

	// 未登录 401（路由挂在认证组内）。
	r, _ = http.Get(ts.URL + "/api/projects/0b7ddc6e-0000-4000-8000-000000000000/stage-runs")
	require.Equal(t, 401, r.StatusCode)
}
