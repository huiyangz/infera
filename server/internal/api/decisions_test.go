package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/reqservice"
	"github.com/tokfinity/infera/internal/store"
)

func TestPendingDecisionsEndpoint(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "项目一"}
	require.NoError(t, st.CreateProject(ctx, p))
	gated := &store.Delivery{ProjectID: p.ID, Title: "规格审批中", Status: "active", PendingGate: "spec_approval", CurrentStage: "spec"}
	require.NoError(t, st.CreateDelivery(ctx, gated))
	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{ProjectID: p.ID, Title: "无门", Status: "active"}))
	time.Sleep(2 * time.Millisecond) // 保证 updated_at 降序可判
	newer := &store.Delivery{ProjectID: p.ID, Title: "任务审批中", Status: "active", PendingGate: "tasks_approval"}
	require.NoError(t, st.CreateDelivery(ctx, newer))

	r, _ := c.Get(ts.URL + "/api/pending-decisions")
	require.Equal(t, 200, r.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	require.Len(t, rows, 2, "只返回 pending_gate 非空的行")

	// 契约冻结：行键集合（解码进 map 断言键集，防形状漂移）。
	// INFERA-267 加法扩展后 = 原 12 键 + source（唯一新增序列化键）。
	require.ElementsMatch(t,
		[]string{"id", "project_id", "project_name", "title", "status", "pending_gate", "current_stage",
			"external_issue_key", "assignee", "priority", "source", "created_at", "updated_at"},
		keys(rows[0]))

	// updated_at 降序：新卡门的行在前。
	require.Equal(t, newer.ID, rows[0]["id"])
	require.Equal(t, "项目一", rows[0]["project_name"])
	require.Equal(t, "tasks_approval", rows[0]["pending_gate"])
	require.Equal(t, gated.ID, rows[1]["id"])
	require.Equal(t, "", rows[1]["external_issue_key"])
	// 需求服务未装配：source 降级为 ''，不 503（决策页可用性不挂在需求服务上）。
	require.Equal(t, "", rows[0]["source"])
}

// newDecisionsServer 构造可按需注入 RequirementsAPI 的登录态服务器
// （decisions source enrichment 用）；fake 为 nil 即未装配。
func newDecisionsServer(t *testing.T, fake RequirementsAPI) (*httptest.Server, *store.Memory, *http.Client) {
	t.Helper()
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	if fake != nil {
		srv.SetRequirements(fake)
	}
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, st, login(t, ts.URL)
}

// seedSourceChain 铺 source 解析链（INFERA-267）：同步父（external mi-root，
// 有门）+ 拆分子（parent=父，无 external，有门）+ 本地行（无父无 external，有门）。
func seedSourceChain(t *testing.T, st *store.Memory) (root, child, local store.Delivery) {
	t.Helper()
	ctx := context.Background()
	p := &store.Project{Name: "来源项目"}
	require.NoError(t, st.CreateProject(ctx, p))

	root = store.Delivery{ProjectID: p.ID, Title: "同步父", Status: "active", PendingGate: "spec_approval", CurrentStage: "spec", ExternalIssueID: "mi-root", ExternalIssueKey: "INFERA-260"}
	require.NoError(t, st.UpsertDeliveryByExternalID(ctx, &root))
	child = store.Delivery{ProjectID: p.ID, Title: "拆分子", Status: "active", PendingGate: "tasks_approval", CurrentStage: "tasks", ParentID: root.ID}
	require.NoError(t, st.CreateDelivery(ctx, &child))
	local = store.Delivery{ProjectID: p.ID, Title: "本地行", Status: "active", PendingGate: "design_approval", CurrentStage: "design"}
	require.NoError(t, st.CreateDelivery(ctx, &local))
	return root, child, local
}

// decisionsRows 取回列表并转 id→行 map（断言不依赖排序）。
func decisionsRows(t *testing.T, c *http.Client, base string) map[string]map[string]any {
	t.Helper()
	r, err := c.Get(base + "/api/pending-decisions")
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&rows))
	out := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		out[row["id"].(string)] = row
	}
	return out
}

func TestPendingDecisionsSourceEnrichment(t *testing.T) {
	// 需求服务带回 mi-root→web；mi-other 无决策行指向它（干扰项）。
	fake := &fakeReq{list: []reqservice.RequirementListItem{
		{Requirement: reqservice.Requirement{ID: "r-1", ExternalIssueID: "mi-root", Source: "web"}},
		{Requirement: reqservice.Requirement{ID: "r-2", ExternalIssueID: "mi-other", Source: "客户"}},
	}}
	ts, st, c := newDecisionsServer(t, fake)
	root, child, local := seedSourceChain(t, st)

	rows := decisionsRows(t, c, ts.URL)
	require.Len(t, rows, 3)
	// 命中：普通行的链根=自身 external id → 源头需求 source。
	require.Equal(t, "web", rows[root.ID]["source"])
	// 拆分子：经链根（父）解析到同一份来源。
	require.Equal(t, "web", rows[child.ID]["source"])
	// 链根无映射（本地行）：''=不可解析，前端回退 —。
	require.Equal(t, "", rows[local.ID]["source"])
}

func TestPendingDecisionsSourceDegradesOnRequirementsError(t *testing.T) {
	fake := &fakeReq{err: errors.New("需求服务不可用")}
	ts, st, c := newDecisionsServer(t, fake)
	root, child, _ := seedSourceChain(t, st)

	// 需求列表读取失败：降级为 ''，决策列表本身照常 200。
	rows := decisionsRows(t, c, ts.URL)
	require.Len(t, rows, 3)
	require.Equal(t, "", rows[root.ID]["source"])
	require.Equal(t, "", rows[child.ID]["source"])
}

func TestPendingDecisionsEmptyIsArray(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, _ := c.Get(ts.URL + "/api/pending-decisions")
	require.Equal(t, 200, r.StatusCode)
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	// 空结果必须是 [] 而非 null（前端可直接 .map）。
	require.Equal(t, "[]", strings.TrimSpace(string(b)))
}

func TestPendingDecisionsUnauthenticated(t *testing.T) {
	ts, _ := newServer(t)
	r, _ := http.Get(ts.URL + "/api/pending-decisions")
	require.Equal(t, 401, r.StatusCode)
}
