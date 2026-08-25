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

// seedDiscovery 铺「需求发现」数据面（与 store 层 discovery_test 同谱系）：
// 两项目、三标签；d1 情报（挖掘产出待分析）、d2 情报+候选（已晋级）、
// d3 候选（跨项目）、d4 无关标签、d5 裸卡。创建序 d1→d5，updated_at 递增。
func seedDiscovery(t *testing.T, st *store.Memory) (d1, d2, d3 store.Delivery) {
	t.Helper()
	ctx := context.Background()
	pa := &store.Project{Name: "项目甲"}
	require.NoError(t, st.CreateProject(ctx, pa))
	pb := &store.Project{Name: "项目乙"}
	require.NoError(t, st.CreateProject(ctx, pb))

	labelIDs := map[string]string{}
	for _, name := range []string{"情报", "候选", "其他"} {
		l := &store.Label{Name: name, Color: "#3b82f6"}
		require.NoError(t, st.CreateLabel(ctx, l))
		labelIDs[name] = l.ID
	}
	mk := func(p *store.Project, title, key string) store.Delivery {
		d := store.Delivery{ProjectID: p.ID, Title: title, Status: "queued", ExternalIssueID: "mi-" + key,
			ExternalIssueKey: "INFERA-" + key, Assignee: "agent:600d08c2", Priority: "high"}
		require.NoError(t, st.CreateDelivery(ctx, &d))
		time.Sleep(2 * time.Millisecond) // updated_at 严格递增，排序可判
		return d
	}
	attach := func(d store.Delivery, names ...string) {
		for _, n := range names {
			require.NoError(t, st.AttachLabel(ctx, d.ID, labelIDs[n]))
		}
	}

	d1 = mk(pa, "情报卡", "1")
	d2 = mk(pa, "已分析情报", "2")
	d3 = mk(pb, "候选卡", "3")
	d4 := mk(pa, "普通卡", "4")
	mk(pa, "裸卡", "5")
	attach(d1, "情报")
	attach(d2, "情报", "候选")
	attach(d3, "候选")
	attach(d4, "其他")
	return d1, d2, d3
}

// discoveryIDs 解出响应行的 id 序列。
func discoveryIDs(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r["id"].(string))
	}
	return out
}

// TestDiscoveryTasksEndpoint：合并取回（缺省）+ 两类分别取回 + 重复参数并集；
// 每行携带 agent_types（该卡命中的 agent 类型全集）/project_name/labels，
// 交付本体复用现有 Delivery 模型（键集冻结）。
func TestDiscoveryTasksEndpoint(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	d1, d2, d3 := seedDiscovery(t, st)

	// 合并取回（无参数 = 两类并集）：updated_at 降序，无关标签/裸卡不进结果。
	r, _ := c.Get(ts.URL + "/api/discovery-tasks")
	require.Equal(t, 200, r.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	require.Equal(t, []string{d3.ID, d2.ID, d1.ID}, discoveryIDs(rows), "两类并集，updated_at 降序")

	// 行键集冻结：Delivery 全字段平铺 + agent_types/project_name/labels。
	deliveryKeys := []string{"id", "project_id", "title", "description", "status", "current_stage", "pending_gate",
		"fail_count", "base_commit", "reject_reason", "workspace_ready", "parent_id", "wave", "split_mode",
		"merge_state", "complexity", "external_issue_id", "external_issue_key", "assignee", "priority",
		"external_synced_at", "created_at", "updated_at"}
	require.ElementsMatch(t, append(deliveryKeys, "agent_types", "project_name", "labels"), keys(rows[0]))

	// 逐行断言（合并视图）：agent_types 报该卡命中的类型全集（含未过滤类型）。
	require.Equal(t, []any{"analysis"}, rows[0]["agent_types"])
	require.Equal(t, "项目乙", rows[0]["project_name"])
	require.Equal(t, []any{map[string]any{"name": "候选", "color": "#3b82f6"}}, rows[0]["labels"])
	require.Equal(t, []any{"mining", "analysis"}, rows[1]["agent_types"], "双标签卡两类都命中")
	require.Equal(t, []any{map[string]any{"name": "候选", "color": "#3b82f6"}, map[string]any{"name": "情报", "color": "#3b82f6"}}, rows[1]["labels"], "labels 按 name 升序（字节序：候选 < 情报）")
	require.Equal(t, []any{"mining"}, rows[2]["agent_types"])
	require.Equal(t, "INFERA-1", rows[2]["external_issue_key"], "同步展示字段透传")
	require.Equal(t, "agent:600d08c2", rows[2]["assignee"])
	require.Equal(t, "high", rows[2]["priority"])

	// 需求挖掘类（情报标签）：分别取回。
	r, _ = c.Get(ts.URL + "/api/discovery-tasks?agent=mining")
	require.Equal(t, 200, r.StatusCode)
	rows = nil
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	require.Equal(t, []string{d2.ID, d1.ID}, discoveryIDs(rows))
	require.Equal(t, []any{"mining", "analysis"}, rows[0]["agent_types"], "行内 agent_types 仍报全集")

	// 需求分析类（候选标签）。
	r, _ = c.Get(ts.URL + "/api/discovery-tasks?agent=analysis")
	require.Equal(t, 200, r.StatusCode)
	rows = nil
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	require.Equal(t, []string{d3.ID, d2.ID}, discoveryIDs(rows))

	// 重复参数 = 显式并集（与缺省等价）。
	r, _ = c.Get(ts.URL + "/api/discovery-tasks?agent=mining&agent=analysis")
	require.Equal(t, 200, r.StatusCode)
	rows = nil
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	require.Equal(t, []string{d3.ID, d2.ID, d1.ID}, discoveryIDs(rows))
}

// TestDiscoveryTasksInvalidAgent：未知 agent 取值 400（invalid_request）。
func TestDiscoveryTasksInvalidAgent(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, _ := c.Get(ts.URL + "/api/discovery-tasks?agent=both")
	require.Equal(t, 400, r.StatusCode)
	var e map[string]string
	require.NoError(t, json.NewDecoder(r.Body).Decode(&e))
	require.Equal(t, "invalid_request", e["code"])
}

// TestDiscoveryTasksCancelledPassthrough（INFERA-232，回归钉）：上游「放弃」的
// 挖掘/分析行以 cancelled 原样出现在需求发现列表——这是前端「候选 / 已放弃」
// 分栏的数据前提：放弃行靠 status==='cancelled' 识别，不得被折叠成 completed。
func TestDiscoveryTasksCancelledPassthrough(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	_, _, _ = seedDiscovery(t, st)

	ctx := context.Background()
	givenUp := store.Delivery{ProjectID: func() string {
		ps, err := st.ListProjects(ctx)
		require.NoError(t, err)
		return ps[0].ID
	}(), Title: "判定无价值的情报", Status: "cancelled", ExternalIssueID: "mi-giveup",
		ExternalIssueKey: "INFERA-99"}
	require.NoError(t, st.CreateDelivery(ctx, &givenUp))

	// 需求发现列表按【情报/候选】标签取数：放弃卡是被需求分析晋级过【候选】
	// 再放弃的行，标签仍在（任务同步保留标签），不带标签进不了本列表。
	for _, l := range func() []store.Label {
		ls, err := st.ListLabels(ctx)
		require.NoError(t, err)
		return ls
	}() {
		if l.Name == "候选" {
			require.NoError(t, st.AttachLabel(ctx, givenUp.ID, l.ID))
		}
	}

	r, _ := c.Get(ts.URL + "/api/discovery-tasks")
	require.Equal(t, 200, r.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))

	var got map[string]any
	for _, row := range rows {
		if row["id"] == givenUp.ID {
			got = row
			break
		}
	}
	require.NotNil(t, got, "放弃行须出现在需求发现列表")
	require.Equal(t, "cancelled", got["status"], "cancelled 原样透传，不折叠为 completed")
}

// TestDiscoveryTasksEmptyIsArray：无命中结果是 [] 而非 null（前端可直接 .map）。
func TestDiscoveryTasksEmptyIsArray(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, _ := c.Get(ts.URL + "/api/discovery-tasks")
	require.Equal(t, 200, r.StatusCode)
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	require.Equal(t, "[]", strings.TrimSpace(string(b)))
}

// TestDiscoveryTasksAuth：路由挂在认证组内，未登录 401。
func TestDiscoveryTasksAuth(t *testing.T) {
	ts, _ := newServer(t)
	r, _ := http.Get(ts.URL + "/api/discovery-tasks")
	require.Equal(t, 401, r.StatusCode)
}
