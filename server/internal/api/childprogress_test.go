package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedChildProgress 铺数：一个父任务 + 跨三阶段、混合状态的八条子任务
// （含一条无阶段子任务），另建一条无子任务的普通交付做空对照。
func seedChildProgress(t *testing.T, st *store.Memory) (parentID, plainID string) {
	t.Helper()
	ctx := context.Background()
	proj := store.Project{Name: "进度项目"}
	require.NoError(t, st.CreateProject(ctx, &proj))

	parent := &store.Delivery{ProjectID: proj.ID, Title: "父任务", Status: "active", SplitMode: true}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	for _, c := range []store.Delivery{
		{ProjectID: proj.ID, Title: "阶段1已完成a", Status: "completed", ParentID: parent.ID, Wave: 1, CurrentStage: "code_review"},
		{ProjectID: proj.ID, Title: "阶段1已完成b", Status: "completed", ParentID: parent.ID, Wave: 1, CurrentStage: "code_review"},
		{ProjectID: proj.ID, Title: "阶段2进行中", Status: "active", ParentID: parent.ID, Wave: 2, CurrentStage: "code_gen"},
		{ProjectID: proj.ID, Title: "阶段2待验收", Status: "active", ParentID: parent.ID, Wave: 2, CurrentStage: "spec", PendingGate: "spec_approval"},
		{ProjectID: proj.ID, Title: "阶段2阻塞", Status: "blocked", ParentID: parent.ID, Wave: 2, CurrentStage: "unit_test"},
		{ProjectID: proj.ID, Title: "阶段2待启动", Status: "queued", ParentID: parent.ID, Wave: 2, CurrentStage: "intake"},
		{ProjectID: proj.ID, Title: "阶段3待启动", Status: "queued", ParentID: parent.ID, Wave: 3, CurrentStage: "intake"},
		{ProjectID: proj.ID, Title: "无阶段已取消", Status: "cancelled", ParentID: parent.ID, Wave: 0, CurrentStage: "intake"},
	} {
		require.NoError(t, st.CreateDelivery(ctx, &c))
	}

	plain := &store.Delivery{ProjectID: proj.ID, Title: "无子任务", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, plain))
	return parent.ID, plain.ID
}

// getChildProgress 登录态请求聚合端点，返回 (状态码, 响应体字节)。
func getChildProgress(t *testing.T, tsURL, id string) (int, []byte) {
	t.Helper()
	c := login(t, tsURL)
	r, err := c.Get(tsURL + "/api/deliveries/" + id + "/progress")
	require.NoError(t, err)
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return r.StatusCode, body
}

func TestChildProgressEndpoint(t *testing.T) {
	ts, st := newServer(t)
	parentID, _ := seedChildProgress(t, st)

	status, body := getChildProgress(t, ts.URL, parentID)
	require.Equal(t, 200, status)

	// 契约冻结：顶层键集合。
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	require.ElementsMatch(t, []string{
		"delivery_id", "total", "done", "in_progress", "in_review", "blocked",
		"todo", "cancelled", "by_status", "active_stage", "stages",
	}, keys(raw))
	require.Equal(t, parentID, raw["delivery_id"])

	var got struct {
		Total       int            `json:"total"`
		Done        int            `json:"done"`
		InProgress  int            `json:"in_progress"`
		InReview    int            `json:"in_review"`
		Blocked     int            `json:"blocked"`
		Todo        int            `json:"todo"`
		Cancelled   int            `json:"cancelled"`
		ByStatus    map[string]int `json:"by_status"`
		ActiveStage *int           `json:"active_stage"`
		Stages      []struct {
			Stage      int            `json:"stage"`
			Total      int            `json:"total"`
			Done       int            `json:"done"`
			InProgress int            `json:"in_progress"`
			InReview   int            `json:"in_review"`
			Blocked    int            `json:"blocked"`
			Todo       int            `json:"todo"`
			Cancelled  int            `json:"cancelled"`
			ByStatus   map[string]int `json:"by_status"`
		} `json:"stages"`
	}
	require.NoError(t, json.Unmarshal(body, &got))

	require.Equal(t, 8, got.Total)
	require.Equal(t, 2, got.Done)
	require.Equal(t, 1, got.InProgress)
	require.Equal(t, 1, got.InReview, "active 停门禁 → 待验收")
	require.Equal(t, 1, got.Blocked)
	require.Equal(t, 2, got.Todo)
	require.Equal(t, 1, got.Cancelled)
	require.Equal(t, map[string]int{
		"active": 2, "queued": 2, "completed": 2, "blocked": 1, "cancelled": 1,
	}, got.ByStatus)
	require.NotNil(t, got.ActiveStage, "阶段1全部完结 → 活跃阶段推进到 2")
	require.Equal(t, 2, *got.ActiveStage)

	// 多阶段分组：编号升序、无阶段（0）垫底。
	require.Len(t, got.Stages, 4)
	require.Equal(t, []int{1, 2, 3, 0}, []int{
		got.Stages[0].Stage, got.Stages[1].Stage, got.Stages[2].Stage, got.Stages[3].Stage,
	})
	require.Equal(t, 2, got.Stages[0].Total)
	require.Equal(t, 2, got.Stages[0].Done)
	require.Equal(t, 4, got.Stages[1].Total)
	require.Equal(t, 1, got.Stages[1].InProgress)
	require.Equal(t, 1, got.Stages[1].InReview)
	require.Equal(t, 1, got.Stages[1].Blocked)
	require.Equal(t, 1, got.Stages[1].Todo)
	require.Equal(t, 1, got.Stages[2].Total)
	require.Equal(t, 1, got.Stages[2].Todo)
	require.Equal(t, 1, got.Stages[3].Total, "无阶段子任务单独成组")
	require.Equal(t, 1, got.Stages[3].Cancelled)
}

// TestChildProgressEndpointNoChildren 无子任务：200 + 全零计数 + 空分组，
// 不因非拆分父/镜像父而缺字段（任务同步父同样可聚合）。
func TestChildProgressEndpointNoChildren(t *testing.T) {
	ts, st := newServer(t)
	_, plainID := seedChildProgress(t, st)

	status, body := getChildProgress(t, ts.URL, plainID)
	require.Equal(t, 200, status)

	var got struct {
		DeliveryID  string `json:"delivery_id"`
		Total       int    `json:"total"`
		ActiveStage *int   `json:"active_stage"`
		Stages      []struct {
			Stage int `json:"stage"`
		} `json:"stages"`
		ByStatus map[string]int `json:"by_status"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, plainID, got.DeliveryID)
	require.Equal(t, 0, got.Total)
	require.Nil(t, got.ActiveStage)
	require.NotNil(t, got.Stages, "空分组输出空数组（非 null）")
	require.Empty(t, got.Stages)
	require.Equal(t, map[string]int{
		"active": 0, "queued": 0, "completed": 0, "blocked": 0, "cancelled": 0,
	}, got.ByStatus)
}

// TestChildProgressEndpointNotFound 交付不存在 → 404（与详情端点一致）。
func TestChildProgressEndpointNotFound(t *testing.T) {
	ts, _ := newServer(t)
	status, body := getChildProgress(t, ts.URL, "0d9f6d6e-0000-4000-8000-000000000000")
	require.Equal(t, 404, status)
	var e map[string]string
	require.NoError(t, json.Unmarshal(body, &e))
	require.Equal(t, "not_found", e["code"])
}

// TestChildProgressEndpointAuth 路由挂在认证组内：未登录 401。
func TestChildProgressEndpointAuth(t *testing.T) {
	ts, _ := newServer(t)
	r, err := http.Get(ts.URL + "/api/deliveries/0d9f6d6e-0000-4000-8000-000000000000/progress")
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, 401, r.StatusCode)
}

// TestChildProgressEndpointReadOnly 端点不产生任何写入：请求前后库内
// 交付行（含子任务状态与更新时间）完全不变。
func TestChildProgressEndpointReadOnly(t *testing.T) {
	ts, st := newServer(t)
	parentID, _ := seedChildProgress(t, st)
	ctx := context.Background()

	before, err := st.ListChildDeliveries(ctx, parentID)
	require.NoError(t, err)

	status, _ := getChildProgress(t, ts.URL, parentID)
	require.Equal(t, 200, status)

	after, err := st.ListChildDeliveries(ctx, parentID)
	require.NoError(t, err)
	require.Equal(t, before, after, "聚合端点不得改动子任务行")
}

// TestChildProgressBadID 非法 ID → 404（沿用详情端点 validID 的既有口径，
// 本任务不改既有行为）。
func TestChildProgressBadID(t *testing.T) {
	ts, _ := newServer(t)
	status, body := getChildProgress(t, ts.URL, "not-a-uuid")
	require.Equal(t, 404, status)
	var e map[string]string
	require.NoError(t, json.Unmarshal(body, &e))
	require.Equal(t, "not_found", e["code"])
}
