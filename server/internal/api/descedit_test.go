package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/syncsvc"
	"github.com/tokfinity/infera/internal/tasksource"
)

// 本文件覆盖「任务描述编辑」端点（INFERA-298 冻结契约，供第 4 层前端接线
// 对齐）：
//
//	PATCH /api/deliveries/{id}/description
//	  body {"description": string}          ← 空白/超长 400
//	  → 200 deliveryJSON（同其余交付变更端点的响应形状）
//	  → 401 未登录 / 404 交付不存在（畸形 id 同口径）/ 400 校验失败
//	  → 409 交付无上游映射 / 502 上游写失败 / 503 未装配
//
// 行为语义（上游优先：先写上游再读回落库）经真 syncsvc.Editor + 假上游
// client 在 HTTP 面验证。

// fakeDescEditor 是 syncsvc.IssueEditor 的测试替身（api 侧看不见 syncsvc
// 包内替身，这里独立一份最小实现）：记录上游写，descs 持有各 issue 的当前
// 描述供读回，错误可注入。
type fakeDescEditor struct {
	descs  map[string]string // issueID → 上游当前描述
	putErr error
	getErr error
	puts   []struct{ issueID, description string }
}

func (f *fakeDescEditor) UpdateIssueDescription(_ context.Context, issueID, description string) error {
	f.puts = append(f.puts, struct{ issueID, description string }{issueID, description})
	if f.putErr != nil {
		return f.putErr
	}
	f.descs[issueID] = description
	return nil
}

func (f *fakeDescEditor) GetIssue(_ context.Context, idOrKey string) (tasksource.Issue, error) {
	if f.getErr != nil {
		return tasksource.Issue{}, f.getErr
	}
	d := f.descs[idOrKey]
	return tasksource.Issue{
		ID: idOrKey, Identifier: "INFERA-78", Title: "任务标题",
		Description: &d, Status: "in_progress", UpdatedAt: time.Now(),
	}, nil
}

// newDescServer 装配测试服务器：种子项目 + 一条同步来源交付 + 真 Editor +
// 假上游。返回 (服务器, 假上游, 交付 infera 侧 id)。
func newDescServer(t *testing.T, mutate ...func(*fakeDescEditor)) (*httptest.Server, *fakeDescEditor, string) {
	t.Helper()
	st := store.NewMemory()
	p := &store.Project{Name: "自动闭环", ExternalProjectID: "ext-prj-1"}
	require.NoError(t, st.UpsertProjectByExternalID(context.Background(), p))

	now := time.Now().UTC()
	d := &store.Delivery{
		ProjectID: p.ID, Title: "任务标题", Description: "旧描述", Status: "queued",
		ExternalIssueID: "iss-1", ExternalIssueKey: "INFERA-78", ExternalSyncedAt: &now,
	}
	require.NoError(t, st.UpsertDeliveryByExternalID(context.Background(), d))

	up := &fakeDescEditor{descs: map[string]string{"iss-1": "旧描述"}}
	for _, m := range mutate {
		m(up)
	}
	ed, err := syncsvc.NewEditor(up, st)
	require.NoError(t, err)

	srv := NewServer(st, "secret-pass", nil)
	srv.SetDescriptionEditor(ed)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, up, d.ID
}

// patchDesc 登录后发一次描述编辑请求，返回状态码与响应体。
func patchDesc(t *testing.T, ts *httptest.Server, id, body string) (int, string) {
	t.Helper()
	return doJSON(t, login(t, ts.URL), http.MethodPatch, ts.URL+"/api/deliveries/"+id+"/description", body)
}

// TestUpdateDescriptionAuthAndAssembly：认证门与装配门——未登录 401；
// 未装配（TASK_SYNC_* 未配置）503。口径对齐既有写端点。
func TestUpdateDescriptionAuthAndAssembly(t *testing.T) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	ts := httptest.NewServer(srv.Mux())
	defer ts.Close()

	const id = "00000000-0000-0000-0000-000000000001"
	url := ts.URL + "/api/deliveries/" + id + "/description"

	r, _ := http.NewRequest(http.MethodPatch, url, strings.NewReader(`{"description":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode, "未登录一律 401")

	code, body := doJSON(t, login(t, ts.URL), http.MethodPatch, url, `{"description":"x"}`)
	require.Equal(t, 503, code, "未装配（TASK_SYNC_* 未配置）")
	require.Contains(t, body, "unavailable")
}

// TestUpdateDescriptionSavesUpstreamFirst（AC）：编辑经上游生效，本地随读回
// 生效；响应为交付形状（含 labels 恒为数组）。
func TestUpdateDescriptionSavesUpstreamFirst(t *testing.T) {
	ts, up, id := newDescServer(t)

	code, body := patchDesc(t, ts, id, `{"description":"## 编辑后\n\n- 新验收项"}`)
	require.Equal(t, 200, code, body)

	var got deliveryJSON
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.Equal(t, "## 编辑后\n\n- 新验收项", got.Description)
	require.NotNil(t, got.Labels, "labels 恒为数组（未挂 = 空数组）")
	require.Equal(t, id, got.ID)
	require.Equal(t, "INFERA-78", got.ExternalIssueKey)

	// 上游确实收到了这次写。
	require.Len(t, up.puts, 1)
	require.Equal(t, "iss-1", up.puts[0].issueID)
	require.Equal(t, "## 编辑后\n\n- 新验收项", up.puts[0].description)

	// 读面立即看到新值（不等下一轮同步）。
	c := login(t, ts.URL)
	gcode, gbody := doJSON(t, c, http.MethodGet, ts.URL+"/api/deliveries/"+id, "")
	require.Equal(t, 200, gcode, gbody)
	require.Contains(t, gbody, "## 编辑后\\n\\n- 新验收项", "GET 读面立即反映编辑")
}

// TestUpdateDescriptionValidation：空描述 / 超长 / 坏 body → 400，且不产生
// 上游写。
func TestUpdateDescriptionValidation(t *testing.T) {
	ts, up, id := newDescServer(t)

	cases := []struct {
		name string
		body string
	}{
		{"空描述", `{"description":""}`},
		{"纯空白", `{"description":"   \n\t "}`},
		{"超长", `{"description":"` + strings.Repeat("a", syncsvc.MaxDescriptionBytes+1) + `"}`},
		{"坏 JSON", `{not-json`},
		{"缺字段", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := patchDesc(t, ts, id, tc.body)
			require.Equal(t, 400, code, body)
			require.Contains(t, body, "invalid_request")
		})
	}
	require.Empty(t, up.puts, "校验失败不得打上游")
}

// TestUpdateDescriptionNotFound：畸形 id 与未知 id 都是 404（对齐既有端点：
// 畸形 id 不进 store，避免数据库驱动错误泄漏成 500）。
func TestUpdateDescriptionNotFound(t *testing.T) {
	ts, _, _ := newDescServer(t)

	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-00000000dead"} {
		t.Run(id, func(t *testing.T) {
			code, body := patchDesc(t, ts, id, `{"description":"x"}`)
			require.Equal(t, 404, code, body)
			require.Contains(t, body, "not_found")
		})
	}
}

// TestUpdateDescriptionNotMirrored：非同步来源的交付无上游对象可写 → 409。
func TestUpdateDescriptionNotMirrored(t *testing.T) {
	st := store.NewMemory()
	p := &store.Project{Name: "自动闭环"}
	require.NoError(t, st.CreateProject(context.Background(), p))
	local := &store.Delivery{ProjectID: p.ID, Title: "本地建", Description: "本地描述", Status: "queued"}
	require.NoError(t, st.CreateDelivery(context.Background(), local))

	up := &fakeDescEditor{descs: map[string]string{}}
	ed, err := syncsvc.NewEditor(up, st)
	require.NoError(t, err)
	srv := NewServer(st, "secret-pass", nil)
	srv.SetDescriptionEditor(ed)
	ts := httptest.NewServer(srv.Mux())
	defer ts.Close()

	code, body := patchDesc(t, ts, local.ID, `{"description":"改不动"}`)
	require.Equal(t, 409, code, body)
	require.Contains(t, body, "conflict")
	require.Empty(t, up.puts)
}

// TestUpdateDescriptionUpstreamFailure：上游写失败 → 502，本地描述保持原值
// （不落半截状态）。
func TestUpdateDescriptionUpstreamFailure(t *testing.T) {
	ts, _, id := newDescServer(t, func(f *fakeDescEditor) { f.putErr = errBoom })

	code, body := patchDesc(t, ts, id, `{"description":"新描述"}`)
	require.Equal(t, 502, code, body)
	require.Contains(t, body, "bad_gateway")

	c := login(t, ts.URL)
	_, gbody := doJSON(t, c, http.MethodGet, ts.URL+"/api/deliveries/"+id, "")
	require.Contains(t, gbody, "旧描述", "上游没写成，本地不得先行变更")
}
