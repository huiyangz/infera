package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedStatsAPI 铺数据：同步来源项目两条需求（一条卡门）+ 本地空项目。
func seedStatsAPI(t *testing.T, st *store.Memory) (synced, empty store.Project) {
	t.Helper()
	ctx := context.Background()
	synced = store.Project{Name: "同步项目", ExternalProjectID: "mp-1"}
	require.NoError(t, st.UpsertProjectByExternalID(ctx, &synced))
	empty = store.Project{Name: "本地项目"}
	require.NoError(t, st.CreateProject(ctx, &empty))
	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{
		ProjectID: synced.ID, Title: "卡在规格审批", Status: "active", PendingGate: "spec_approval",
	}))
	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{
		ProjectID: synced.ID, Title: "已交付", Status: "completed",
	}))
	return synced, empty
}

func TestProjectRequirementStatsEndpoint(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	synced, empty := seedStatsAPI(t, st)

	r, _ := c.Get(ts.URL + "/api/projects/" + synced.ID + "/stats")
	require.Equal(t, 200, r.StatusCode)

	// 契约冻结：顶层键集合与取值（解码进 map 断言键集，防形状漂移）。
	var body map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.ElementsMatch(t,
		[]string{"project_id", "requirement_total", "by_status", "pending_decisions", "delivered", "last_synced_at"},
		keys(body))
	require.Equal(t, synced.ID, body["project_id"])
	require.Equal(t, float64(2), body["requirement_total"])
	require.Equal(t, map[string]any{"active": float64(1), "queued": float64(0), "completed": float64(1), "blocked": float64(0)},
		body["by_status"])
	require.Equal(t, float64(1), body["pending_decisions"])
	require.Equal(t, float64(1), body["delivered"])
	require.NotNil(t, body["last_synced_at"], "同步来源项目最近同步时间非 null")

	// 从未同步的空项目：last_synced_at 为 JSON null（解码后 nil）。
	r, _ = c.Get(ts.URL + "/api/projects/" + empty.ID + "/stats")
	require.Equal(t, 200, r.StatusCode)
	body = map[string]any{}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Equal(t, float64(0), body["requirement_total"])
	require.Nil(t, body["last_synced_at"])
}

func TestProjectRequirementStatsNotFound(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, _ := c.Get(ts.URL + "/api/projects/0b7ddc6e-0000-4000-8000-000000000000/stats")
	require.Equal(t, 404, r.StatusCode)
	var e map[string]string
	require.NoError(t, json.NewDecoder(r.Body).Decode(&e))
	require.Equal(t, "项目不存在", e["error"])

	// 未登录 401（路由挂在认证组内）。
	r, _ = http.Get(ts.URL + "/api/projects/0b7ddc6e-0000-4000-8000-000000000000/stats")
	require.Equal(t, 401, r.StatusCode)
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
