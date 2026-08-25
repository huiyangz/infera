package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedAgentActivityAPI 铺数据：项目 spec 阶段绑定 agent「SDD」+ 一条需求 +
// 三次运行（spec×2 已收尾、code_gen×1 未绑定）。StartStageRun 落 now()，
// 请求窗口 to 也取 now → 全部落最后一桶。
func seedAgentActivityAPI(t *testing.T, st *store.Memory) (proj store.Project, agent store.Agent) {
	t.Helper()
	ctx := context.Background()
	proj = store.Project{Name: "时序项目"}
	require.NoError(t, st.CreateProject(ctx, &proj))
	agent = store.Agent{Name: "SDD", Runner: "local"}
	require.NoError(t, st.CreateAgent(ctx, &agent))
	require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{ProjectID: proj.ID, Node: "spec", AgentID: agent.ID}))
	d := store.Delivery{ProjectID: proj.ID, Title: "需求", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, &d))

	start := func(stage string, attempt int) {
		r := &store.StageRun{DeliveryID: d.ID, Stage: stage, Attempt: attempt, Status: "running"}
		require.NoError(t, st.StartStageRun(ctx, r))
		require.NoError(t, st.FinishStageRun(ctx, r.ID, "done"))
	}
	start("spec", 1)
	start("spec", 2) // attempt 各计一次
	start("code_gen", 1)
	return proj, agent
}

// getAgentActivity 登录态请求并返回 (状态码, 响应体字节)。
func getAgentActivity(t *testing.T, tsURL, query string) (int, []byte) {
	t.Helper()
	c := login(t, tsURL)
	r, err := c.Get(tsURL + "/api/agent-activity" + query)
	require.NoError(t, err)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return r.StatusCode, body
}

func TestAgentActivityEndpoint(t *testing.T) {
	ts, st := newServer(t)
	_, agent := seedAgentActivityAPI(t, st)

	status, body := getAgentActivity(t, ts.URL, "")
	require.Equal(t, 200, status)

	// 契约冻结：顶层键集合与默认参数（缺省 24h / 30m）。
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.ElementsMatch(t, []string{"window", "bucket_minutes", "series"}, keys(raw))
	require.Equal(t, float64(30), raw["bucket_minutes"])

	var got struct {
		Window struct {
			From time.Time `json:"from"`
			To   time.Time `json:"to"`
		} `json:"window"`
		BucketMinutes int `json:"bucket_minutes"`
		Series        []struct {
			AgentID   string `json:"agent_id"`
			AgentName string `json:"agent_name"`
			Points    []struct {
				T     time.Time `json:"t"`
				Count int       `json:"count"`
			} `json:"points"`
		} `json:"series"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, 30, got.BucketMinutes)
	require.Equal(t, 24*time.Hour, got.Window.To.Sub(got.Window.From), "缺省窗口 24h")
	require.WithinDuration(t, time.Now().UTC(), got.Window.To, time.Minute, "窗口右端为当前时刻")

	// series 按 agent_name 排序；无绑定 stage 的运行归 "unbound" 分组（响应可见）。
	require.Len(t, got.Series, 2)
	require.Equal(t, "SDD", got.Series[0].AgentName)
	require.Equal(t, agent.ID, got.Series[0].AgentID)
	require.Equal(t, "unbound", got.Series[1].AgentName, "code_gen 未绑定 → unbound 分组")
	require.Empty(t, got.Series[1].AgentID, "unbound 无真实 agent → agent_id 空串")

	// 每条曲线覆盖窗口内全部桶（24h/30m = 48，含 count=0），等长对齐；
	// 全部运行落在最后一桶（铺数即当前时刻）。
	for _, s := range got.Series {
		require.Len(t, s.Points, 48, "%s 覆盖全部桶", s.AgentName)
		for i, p := range s.Points {
			require.Equal(t, got.Window.From.Add(time.Duration(i)*30*time.Minute), p.T, "%s 第 %d 桶起点", s.AgentName, i)
		}
	}
	require.Equal(t, 2, got.Series[0].Points[47].Count, "spec 两次 attempt 各计一次")
	require.Equal(t, 0, got.Series[0].Points[46].Count, "空桶补零")
	require.Equal(t, 1, got.Series[1].Points[47].Count)

	// points 元素键集合（契约冻结）。
	var shape struct {
		Series []struct {
			Points []map[string]any `json:"points"`
		} `json:"series"`
	}
	require.NoError(t, json.Unmarshal(body, &shape))
	require.ElementsMatch(t, []string{"t", "count"}, keys(shape.Series[0].Points[0]))

	// 显式参数：hours=2&bucket_minutes=5 → 窗口 2h、24 桶。
	status, body = getAgentActivity(t, ts.URL, "?hours=2&bucket_minutes=5")
	require.Equal(t, 200, status)
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, 5, got.BucketMinutes)
	require.Equal(t, 2*time.Hour, got.Window.To.Sub(got.Window.From))
	for _, s := range got.Series {
		require.Len(t, s.Points, 24)
	}
	require.Equal(t, 2, got.Series[0].Points[23].Count)
}

func TestAgentActivityEmpty(t *testing.T) {
	ts, _ := newServer(t) // 无任何执行

	status, body := getAgentActivity(t, ts.URL, "")
	require.Equal(t, 200, status)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.NotNil(t, raw["series"], "空窗口 series 是 [] 不是 null")
	require.Empty(t, raw["series"])
	require.Equal(t, float64(30), raw["bucket_minutes"])
}

func TestAgentActivityBadParams(t *testing.T) {
	ts, _ := newServer(t)
	for _, q := range []string{
		"?hours=abc", "?hours=0", "?hours=-1", "?hours=169", "?hours=1.5",
		"?bucket_minutes=7", "?bucket_minutes=0", "?bucket_minutes=-30", "?bucket_minutes=abc", "?bucket_minutes=90",
	} {
		status, body := getAgentActivity(t, ts.URL, q)
		require.Equal(t, 400, status, "参数 %q 应 400", q)
		var e map[string]string
		require.NoError(t, json.Unmarshal(body, &e))
		require.Equal(t, "invalid_request", e["code"])
		require.NotEmpty(t, e["error"])
	}

	// 边界合法值：hours 上限 168、最小桶宽 5。
	for _, q := range []string{"?hours=168", "?hours=1&bucket_minutes=5", "?hours=168&bucket_minutes=60"} {
		status, body := getAgentActivity(t, ts.URL, q)
		require.Equal(t, 200, status, "参数 %q 应 200", q)
		require.NotEmpty(t, bytes.TrimSpace(body))
	}
}

func TestAgentActivityAuth(t *testing.T) {
	ts, _ := newServer(t)
	r, err := http.Get(ts.URL + "/api/agent-activity")
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, 401, r.StatusCode, "路由挂在认证组内")
}
