package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// seedLabels 建一个项目、一个交付和两个标签（模拟同步链路落库后的状态），
// 返回 (delivery, labelA, labelB)。
func seedLabels(t *testing.T, st *store.Memory) (store.Delivery, store.Label, store.Label) {
	t.Helper()
	ctx := context.Background()
	proj := &store.Project{Name: "demo", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, proj))
	d := &store.Delivery{ProjectID: proj.ID, Title: "需求A", Status: "active", PendingGate: "spec_approval"}
	require.NoError(t, st.CreateDelivery(ctx, d))
	a := &store.Label{Name: "auto", Color: "#22c55e", ExternalLabelID: "m-1"}
	require.NoError(t, st.UpsertLabelByExternalID(ctx, a))
	b := &store.Label{Name: "情报", Color: "#3b82f6", ExternalLabelID: "m-2"}
	require.NoError(t, st.UpsertLabelByExternalID(ctx, b))
	return *d, *a, *b
}

// TestLabelsList 标签库列表：鉴权门 + 全库返回（name/color/external id）。
func TestLabelsList(t *testing.T) {
	ts, st := newServer(t)
	_, a, b := seedLabels(t, st)

	// 未登录 → 401
	r, _ := http.Get(ts.URL + "/api/labels")
	require.Equal(t, 401, r.StatusCode)

	c := login(t, ts.URL)
	r, _ = c.Get(ts.URL + "/api/labels")
	require.Equal(t, 200, r.StatusCode)
	var got []store.Label
	require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
	require.Len(t, got, 2)
	require.Equal(t, "auto", got[0].Name)
	require.Equal(t, "#22c55e", got[0].Color)
	require.Equal(t, a.ExternalLabelID, got[0].ExternalLabelID)
	require.Equal(t, "情报", got[1].Name)
	require.Equal(t, b.ID, got[1].ID)
}

// TestDeliveryAttachDetachLabel 挂/摘标端点：幂等挂标、摘标、错误码。
func TestDeliveryAttachDetachLabel(t *testing.T) {
	ts, st := newServer(t)
	d, a, b := seedLabels(t, st)

	// 未登录 → 401（标签端点在鉴权组内）
	r, _ := http.Post(ts.URL+"/api/deliveries/"+d.ID+"/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"`+a.ID+`"}`))
	require.Equal(t, 401, r.StatusCode)

	c := login(t, ts.URL)

	// 挂标 → 200，回当前标签清单（name + color）
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"`+a.ID+`"}`))
	require.Equal(t, 200, r.StatusCode)
	var body struct {
		Labels []map[string]any `json:"labels"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Len(t, body.Labels, 1)
	require.Equal(t, "auto", body.Labels[0]["name"])
	require.Equal(t, "#22c55e", body.Labels[0]["color"])
	require.Len(t, body.Labels[0], 2, "冻结契约：labels 只含 name + color")

	// 重复挂同一标签 → 幂等，仍是一条
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"`+a.ID+`"}`))
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Len(t, body.Labels, 1)

	// 挂第二个
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"`+b.ID+`"}`))
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Len(t, body.Labels, 2)

	// 摘标 → 剩一个
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/deliveries/"+d.ID+"/labels/"+a.ID, nil)
	r, _ = c.Do(req)
	require.Equal(t, 200, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	require.Len(t, body.Labels, 1)
	require.Equal(t, "情报", body.Labels[0]["name"])

	// 再摘同一标签 → 404（关联已不存在）
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/deliveries/"+d.ID+"/labels/"+a.ID, nil)
	r, _ = c.Do(req)
	require.Equal(t, 404, r.StatusCode)

	// 交付不存在 / 标签不存在 / 畸形 id → 404
	r, _ = c.Post(ts.URL+"/api/deliveries/00000000-0000-0000-0000-000000000000/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"`+a.ID+`"}`))
	require.Equal(t, 404, r.StatusCode)
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"00000000-0000-0000-0000-000000000000"}`))
	require.Equal(t, 404, r.StatusCode)
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/labels", "application/json",
		bytes.NewBufferString(`{"label_id":"not-a-uuid"}`))
	require.Equal(t, 404, r.StatusCode)
}

// TestDeliveryResponseCarriesLabels 交付响应携带标签（冻结契约：name + color）：
// 交付详情（含拆分子需求）与任务分组视图都在交付对象上带 labels。
func TestDeliveryResponseCarriesLabels(t *testing.T) {
	ts, st := newServer(t)
	ctx := context.Background()
	d, a, _ := seedLabels(t, st)
	require.NoError(t, st.AttachLabel(ctx, d.ID, a.ID))
	// 拆分子需求也挂一个，验证 children 路径。
	child := &store.Delivery{ProjectID: d.ProjectID, Title: "子需求", Status: "queued", ParentID: d.ID, Wave: 1}
	require.NoError(t, st.CreateDelivery(ctx, child))
	parent := d
	parent.SplitMode = true
	require.NoError(t, st.UpdateDelivery(ctx, &parent))

	c := login(t, ts.URL)

	// GET /api/deliveries/{id}：delivery.labels 在位且形状冻结。
	r, _ := c.Get(ts.URL + "/api/deliveries/" + d.ID)
	require.Equal(t, 200, r.StatusCode)
	var detail struct {
		Delivery struct {
			Labels []map[string]any `json:"labels"`
		} `json:"delivery"`
		Children []struct {
			Labels []map[string]any `json:"labels"`
		} `json:"children"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&detail))
	require.Len(t, detail.Delivery.Labels, 1)
	require.Equal(t, "auto", detail.Delivery.Labels[0]["name"])
	require.Equal(t, "#22c55e", detail.Delivery.Labels[0]["color"])
	require.Len(t, detail.Delivery.Labels[0], 2, "冻结契约：labels 只含 name + color")
	require.NotNil(t, detail.Children, "拆分父附子需求清单")
	require.Empty(t, detail.Children[0].Labels, "未挂标签的交付返回空数组（非 null）")

	// GET /api/projects/{id}/task-groups：顶层行与子任务都带 labels。
	r, _ = c.Get(ts.URL + "/api/projects/" + d.ProjectID + "/task-groups")
	require.Equal(t, 200, r.StatusCode)
	var groups []struct {
		Labels []map[string]any `json:"labels"`
		Stages []struct {
			Tasks []struct {
				Labels []map[string]any `json:"labels"`
			} `json:"tasks"`
		} `json:"stages"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&groups))
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Labels, 1)
	require.Equal(t, "auto", groups[0].Labels[0]["name"])
	require.Len(t, groups[0].Stages[0].Tasks, 1)
	require.NotNil(t, groups[0].Stages[0].Tasks[0].Labels, "子任务行恒含 labels 字段（空=未挂）")
	require.Empty(t, groups[0].Stages[0].Tasks[0].Labels)
}
