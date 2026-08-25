package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedWorkspaceStatsAPI 铺数据：一项目 + 五种状态各一条需求 + 四次运行
// （done×2、failed×1、running×1）。StartStageRun 落 now()，各行的
// StartedAt 回读后用于推导期望桶号——不受「跨小时边界」抖动影响。
func seedWorkspaceStatsAPI(t *testing.T, st *store.Memory) (started []time.Time) {
	t.Helper()
	ctx := context.Background()
	proj := store.Project{Name: "统计项目"}
	require.NoError(t, st.CreateProject(ctx, &proj))
	for _, status := range []string{"completed", "active", "queued", "blocked", "cancelled"} {
		require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{ProjectID: proj.ID, Title: "需求", Status: status}))
	}
	runs := []struct {
		stage string
		fin   string // "" = 留 running
	}{
		{"spec", "done"},
		{"code_gen", "done"},
		{"review", "failed"},
		{"code_gen", ""}, // attempt 各计一次
	}
	for i, r := range runs {
		run := &store.StageRun{DeliveryID: proj.ID, Stage: r.stage, Attempt: i + 1, Status: "running"}
		require.NoError(t, st.StartStageRun(ctx, run))
		started = append(started, run.StartedAt)
		if r.fin != "" {
			require.NoError(t, st.FinishStageRun(ctx, run.ID, r.fin))
		}
	}
	return started
}

// getWorkspaceStats 登录态请求并返回 (状态码, 响应体字节)。
func getWorkspaceStats(t *testing.T, tsURL, query string) (int, []byte) {
	t.Helper()
	c := login(t, tsURL)
	r, err := c.Get(tsURL + "/api/stats" + query)
	require.NoError(t, err)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return r.StatusCode, body
}

func TestWorkspaceStatsEndpoint(t *testing.T) {
	ts, st := newServer(t)
	started := seedWorkspaceStatsAPI(t, st)

	status, body := getWorkspaceStats(t, ts.URL, "")
	require.Equal(t, 200, status)

	// 契约冻结：顶层键集合与缺省参数（窗口 168h / UTC）。
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.ElementsMatch(t, []string{"window", "timezone", "task_status", "execution", "hourly"}, keys(raw))
	require.Equal(t, "UTC", raw["timezone"])

	var got struct {
		Window struct {
			From time.Time `json:"from"`
			To   time.Time `json:"to"`
		} `json:"window"`
		Timezone  string `json:"timezone"`
		TaskStats struct {
			Total      int            `json:"total"`
			Done       int            `json:"done"`
			InProgress int            `json:"in_progress"`
			Todo       int            `json:"todo"`
			Cancelled  int            `json:"cancelled"`
			ByStatus   map[string]int `json:"by_status"`
		} `json:"task_status"`
		Execution struct {
			RunsTotal       int   `json:"runs_total"`
			Running         int   `json:"running"`
			Done            int   `json:"done"`
			Failed          int   `json:"failed"`
			DurationMSTotal int64 `json:"duration_ms_total"`
		} `json:"execution"`
		Hourly []struct {
			Hour       int   `json:"hour"`
			Runs       int   `json:"runs"`
			DurationMS int64 `json:"duration_ms"`
		} `json:"hourly"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "UTC", got.Timezone)
	require.Equal(t, 168*time.Hour, got.Window.To.Sub(got.Window.From), "缺省窗口 168h")
	require.WithinDuration(t, time.Now().UTC(), got.Window.To, time.Minute, "窗口右端为当前时刻")

	// 状态分布：五类归并 + 原始状态计数（Multica 口径）。
	require.Equal(t, 5, got.TaskStats.Total)
	require.Equal(t, 1, got.TaskStats.Done)
	require.Equal(t, 1, got.TaskStats.InProgress)
	require.Equal(t, 2, got.TaskStats.Todo, "queued + blocked 归待办")
	require.Equal(t, 1, got.TaskStats.Cancelled)
	require.Equal(t, map[string]int{"active": 1, "queued": 1, "completed": 1, "blocked": 1, "cancelled": 1}, got.TaskStats.ByStatus)

	// 执行统计：计数不分状态，running 不计时长（时长精确值由 store 层测试覆盖）。
	require.Equal(t, 4, got.Execution.RunsTotal)
	require.Equal(t, 2, got.Execution.Done)
	require.Equal(t, 1, got.Execution.Failed)
	require.Equal(t, 1, got.Execution.Running)
	require.GreaterOrEqual(t, got.Execution.DurationMSTotal, int64(0))

	// 逐小时分桶：恒 24 桶按 hour 升序；期望桶号由铺数行的 StartedAt 推出。
	require.Len(t, got.Hourly, 24)
	expected := map[int]int{}
	for _, s := range started {
		expected[s.UTC().Hour()]++
	}
	seen := 0
	for i, b := range got.Hourly {
		require.Equal(t, i, b.Hour)
		require.Equal(t, expected[i], b.Runs, "%d 点桶次数", i)
		require.GreaterOrEqual(t, b.DurationMS, int64(0))
		seen += b.Runs
	}
	require.Equal(t, 4, seen, "窗口覆盖全部铺数行")

	// 子对象键集合（契约冻结）。
	var shape struct {
		TaskStatus map[string]any   `json:"task_status"`
		Execution  map[string]any   `json:"execution"`
		Hourly     []map[string]any `json:"hourly"`
	}
	require.NoError(t, json.Unmarshal(body, &shape))
	require.ElementsMatch(t, []string{"total", "done", "in_progress", "todo", "cancelled", "by_status"}, keys(shape.TaskStatus))
	require.ElementsMatch(t, []string{"runs_total", "running", "done", "failed", "duration_ms_total"}, keys(shape.Execution))
	require.ElementsMatch(t, []string{"hour", "runs", "duration_ms"}, keys(shape.Hourly[0]))
}

func TestWorkspaceStatsTimezone(t *testing.T) {
	ts, st := newServer(t)
	started := seedWorkspaceStatsAPI(t, st)

	status, body := getWorkspaceStats(t, ts.URL, "?tz=Asia/Shanghai")
	require.Equal(t, 200, status)

	var got struct {
		Timezone string `json:"timezone"`
		Hourly   []struct {
			Hour int `json:"hour"`
			Runs int `json:"runs"`
		} `json:"hourly"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "Asia/Shanghai", got.Timezone)
	require.Len(t, got.Hourly, 24)

	sh, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	expected := map[int]int{}
	for _, s := range started {
		expected[s.In(sh).Hour()]++
	}
	for _, b := range got.Hourly {
		require.Equal(t, expected[b.Hour], b.Runs, "%d 点桶按请求时区归桶", b.Hour)
	}
}

func TestWorkspaceStatsEmpty(t *testing.T) {
	ts, _ := newServer(t) // 无任何项目/需求/执行

	status, body := getWorkspaceStats(t, ts.URL, "")
	require.Equal(t, 200, status)
	var got struct {
		TaskStatus struct {
			Total int `json:"total"`
		} `json:"task_status"`
		Execution struct {
			RunsTotal int `json:"runs_total"`
		} `json:"execution"`
		Hourly []struct {
			Hour int `json:"hour"`
			Runs int `json:"runs"`
		} `json:"hourly"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Zero(t, got.TaskStatus.Total)
	require.Zero(t, got.Execution.RunsTotal)
	require.NotNil(t, got.Hourly, "空库 hourly 是 24 个零桶（非 null）")
	require.Len(t, got.Hourly, 24)
	for i, b := range got.Hourly {
		require.Equal(t, i, b.Hour)
		require.Zero(t, b.Runs)
	}
}

func TestWorkspaceStatsBadParams(t *testing.T) {
	ts, _ := newServer(t)
	for _, q := range []string{
		"?hours=abc", "?hours=0", "?hours=-1", "?hours=721", "?hours=1.5",
		"?tz=Nowhere/Bogus", "?tz=utc", "?tz=%2F",
	} {
		status, body := getWorkspaceStats(t, ts.URL, q)
		require.Equal(t, 400, status, "参数 %q 应 400", q)
		var e map[string]string
		require.NoError(t, json.Unmarshal(body, &e))
		require.Equal(t, "invalid_request", e["code"])
		require.NotEmpty(t, e["error"])
	}

	// 边界合法值：hours 上下限、tz 缺省与显式合法时区名。
	for _, q := range []string{"?hours=1", "?hours=720", "?tz=Asia/Shanghai", "?tz=UTC", "?hours=24&tz=Asia/Shanghai"} {
		status, body := getWorkspaceStats(t, ts.URL, q)
		require.Equal(t, 200, status, "参数 %q 应 200", q)
		require.True(t, json.Valid(body))
	}
}

func TestWorkspaceStatsAuth(t *testing.T) {
	ts, _ := newServer(t)
	r, err := http.Get(ts.URL + "/api/stats")
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, 401, r.StatusCode, "路由挂在认证组内")
}
