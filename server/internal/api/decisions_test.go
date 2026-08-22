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
	require.ElementsMatch(t,
		[]string{"id", "project_id", "project_name", "title", "status", "pending_gate", "current_stage",
			"multica_issue_key", "assignee", "priority", "created_at", "updated_at"},
		keys(rows[0]))

	// updated_at 降序：新卡门的行在前。
	require.Equal(t, newer.ID, rows[0]["id"])
	require.Equal(t, "项目一", rows[0]["project_name"])
	require.Equal(t, "tasks_approval", rows[0]["pending_gate"])
	require.Equal(t, gated.ID, rows[1]["id"])
	require.Equal(t, "", rows[1]["multica_issue_key"])
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
