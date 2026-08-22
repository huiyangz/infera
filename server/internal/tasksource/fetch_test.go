package tasksource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestListProjects：项目拉取（GET /api/projects）。响应面是
// {"projects": [...], "total": N}；服务端 ListProjects 无 limit/offset——
// 一次响应返回 workspace 全量项目（接入 spike 实证），因此客户端不翻页、
// 也不发明分页机制。新端点同样必须走统一认证与 X-Workspace-Id 通道（坑1）。
func TestListProjects(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotWS, gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"projects": [
				{"id":"p-1","title":"自动闭环","description":"infera 需求闭环","status":"in_progress","priority":"high","lead_type":"member","lead_id":"m-1","updated_at":"2026-08-22T05:00:00Z"},
				{"id":"p-2","title":"空项目","description":null,"status":"planned","priority":"none","lead_type":null,"lead_id":null,"updated_at":"2026-08-21T05:00:00Z"}
			],
			"total": 2
		}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	projects, err := c.ListProjects(context.Background())
	require.NoError(t, err)
	require.Equal(t, "GET", gotMethod)
	require.Equal(t, "/api/projects", gotPath)
	require.Empty(t, gotQuery, "项目端点无分页参数——服务端一次返回全量（实证），不发明机制")
	require.Equal(t, "Bearer mul_t", gotAuth)
	require.Equal(t, "ws-1", gotWS, "X-Workspace-Id 头必须随新端点注入（坑1）")
	require.Len(t, projects, 2)

	first := projects[0]
	require.Equal(t, "p-1", first.ID)
	require.Equal(t, "自动闭环", first.Title)
	require.NotNil(t, first.Description, "已填描述解码为非空指针")
	require.Equal(t, "infera 需求闭环", *first.Description)
	require.Equal(t, "in_progress", first.Status)
	require.Equal(t, "high", first.Priority)
	require.NotNil(t, first.LeadType)
	require.Equal(t, "member", *first.LeadType)
	require.Equal(t, time.Date(2026, 8, 22, 5, 0, 0, 0, time.UTC), first.UpdatedAt)

	second := projects[1]
	require.Nil(t, second.Description, "未填描述保持 nil（映射层负责归一）")
	require.Nil(t, second.LeadType)
	require.Nil(t, second.LeadID)
}

// fakeIssueListServer 是分页保真假服务器，模拟 GET /api/issues 的真实协议：
//   - limit/offset 切片翻页（服务端 limit 上限 100，超出压回 100）；
//   - 响应 {"issues": [...], "total": N}，total 为过滤后真实计数（COUNT 同 WHERE）；
//   - 可注入病态行为：忽略 offset（恒返第一页）/ 谎报 total=0，用于验证客户端
//     收敛保护。
type fakeIssueListServer struct {
	mu            sync.Mutex
	issues        []Issue  // 全量数据（升序分页序）
	totalOverride int      // 0 = 如实回报 len(issues)
	ignoreOffset  bool     // 病态：恒按 offset=0 返回第一页
	reqs          []string // 记录 "limit=L&offset=O" 请求序列
}

func (f *fakeIssueListServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	limit := 100
	if l := q.Get("limit"); l != "" {
		var v int
		_, _ = fmt.Sscanf(l, "%d", &v)
		if v > 0 {
			limit = v
		}
	}
	if limit > 100 {
		limit = 100 // 服务端上限（接入 spike 实证：>100 压回 100）
	}
	offset := 0
	if o := q.Get("offset"); o != "" {
		_, _ = fmt.Sscanf(o, "%d", &offset)
	}
	f.reqs = append(f.reqs, fmt.Sprintf("limit=%d&offset=%d", limit, offset))
	if f.ignoreOffset {
		offset = 0
	}
	total := len(f.issues)
	if f.totalOverride != 0 {
		total = f.totalOverride
	}

	start, end := offset, offset+limit
	if start > len(f.issues) {
		start = len(f.issues)
	}
	if end > len(f.issues) {
		end = len(f.issues)
	}
	page := f.issues[start:end]

	resp := struct {
		Issues []Issue `json:"issues"`
		Total  int     `json:"total"`
	}{Issues: page, Total: total}
	b, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// makeIssues 造 n 条仅 id 不同的 issue（分页聚合测试的数据面）。
func makeIssues(n int) []Issue {
	issues := make([]Issue, n)
	for i := range issues {
		issues[i] = Issue{ID: fmt.Sprintf("i-%03d", i+1)}
	}
	return issues
}

// TestListIssuesPaginatesUntilExhausted：issue 拉取的核心契约——
// GET /api/issues 以 limit=100（服务端上限）+ offset 翻页拉全 workspace 全量，
// 聚合保序不重不漏。237 条 → 0/100/200 三页，total 达成即停（无需再发空页请求）。
func TestListIssuesPaginatesUntilExhausted(t *testing.T) {
	fake := &fakeIssueListServer{issues: makeIssues(237)}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issues, err := c.ListIssues(context.Background())
	require.NoError(t, err)
	require.Len(t, issues, 237, "分页聚合必须拉全 237 条")
	require.Equal(t, "i-001", issues[0].ID, "聚合保序：首页首位在前")
	require.Equal(t, "i-237", issues[236].ID, "聚合保序：末页末位在末")
	require.Equal(t, []string{
		"limit=100&offset=0",
		"limit=100&offset=100",
		"limit=100&offset=200",
	}, fake.reqs, "翻页序列：237 条恰好三页，total 达成后不再请求")
}

// TestListIssuesSinglePage：单页即全量（<100 条）→ 恰好一次请求。
func TestListIssuesSinglePage(t *testing.T) {
	fake := &fakeIssueListServer{issues: makeIssues(3)}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issues, err := c.ListIssues(context.Background())
	require.NoError(t, err)
	require.Len(t, issues, 3)
	require.Equal(t, []string{"limit=100&offset=0"}, fake.reqs, "短页即末页，不再追加请求")
}

// TestListIssuesEmptyWorkspace：空 workspace → 一次请求返回空切片（非 nil 亦可，
// 调用方拿到的就是"拉全了，没有数据"）。
func TestListIssuesEmptyWorkspace(t *testing.T) {
	fake := &fakeIssueListServer{}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issues, err := c.ListIssues(context.Background())
	require.NoError(t, err)
	require.Empty(t, issues)
	require.Equal(t, []string{"limit=100&offset=0"}, fake.reqs)
}

// TestListIssuesStopsOnTotalAgainstBrokenOffset：服务端病态忽略 offset
// （恒返第一页）时，total 达成判定必须兜住循环——拉到 total 条即停、
// 结果不膨胀，不依赖短页/空页条件。
func TestListIssuesStopsOnTotalAgainstBrokenOffset(t *testing.T) {
	fake := &fakeIssueListServer{issues: makeIssues(100), ignoreOffset: true}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issues, err := c.ListIssues(context.Background())
	require.NoError(t, err, "total 达成即停——病态服务端不该拖垮调用方")
	require.Len(t, issues, 100, "total=100 首页即达成并收敛，不堆叠重复页")
	require.Equal(t, []string{"limit=100&offset=0"}, fake.reqs)
}

// TestListIssuesNonConvergentGuard：连 total 都不可信（谎报 0）且恒返满页的
// 服务端 → 客户端必须有最大页数防御，大声报错而不是死循环或静默截断。
func TestListIssuesNonConvergentGuard(t *testing.T) {
	fake := &fakeIssueListServer{issues: makeIssues(100), totalOverride: -1, ignoreOffset: true}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.ListIssues(context.Background())
	require.ErrorContains(t, err, "不收敛", "offset 翻页不收敛时必须报错而非死循环")
}

// TestListIssuesServerError：中途某页非 2xx → 错误带状态码上抛，不得吞成半份结果。
// 首页满页 + total=150，迫使客户端必然发起第二页请求。
func TestListIssuesServerError(t *testing.T) {
	firstPage := make([]Issue, 100) // 满页 + total 未达成，迫使第二页请求
	for i := range firstPage {
		firstPage[i] = Issue{ID: fmt.Sprintf("i-%03d", i+1)}
	}
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			b, _ := json.Marshal(struct {
				Issues []Issue `json:"issues"`
				Total  int     `json:"total"`
			}{Issues: firstPage, Total: 150})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.ListIssues(context.Background())
	require.ErrorContains(t, err, "500")
	require.ErrorContains(t, err, "boom")
}

// TestListIssuesDecodesFetchFields：拉取面字段解码——映射消费的最小字段面
// （标题/描述/状态/优先级/负责人/父子关系/项目归属/阶段），可空字段按指针保真。
func TestListIssuesDecodesFetchFields(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[
			{
				"id":"i-1","identifier":"INFERA-78","title":"T01 拉取面",
				"description":"薄 client 补拉取","status":"in_progress","priority":"high",
				"assignee_type":"agent","assignee_id":"a-1",
				"parent_issue_id":"i-0","project_id":"p-1",
				"stage":2,
				"updated_at":"2026-08-22T06:00:00Z"
			},
			{
				"id":"i-2","identifier":"INFERA-77","title":"父需求",
				"description":null,"status":"todo","priority":"none",
				"assignee_type":null,"assignee_id":null,
				"parent_issue_id":null,"project_id":null,
				"updated_at":"2026-08-22T07:00:00Z"
			}
		],"total":2}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issues, err := c.ListIssues(context.Background())
	require.NoError(t, err)
	require.Len(t, issues, 2)

	first := issues[0]
	require.Equal(t, "INFERA-78", first.Identifier)
	require.Equal(t, "T01 拉取面", first.Title)
	require.NotNil(t, first.Description)
	require.Equal(t, "薄 client 补拉取", *first.Description)
	require.Equal(t, "in_progress", first.Status)
	require.Equal(t, "high", first.Priority)
	require.NotNil(t, first.AssigneeType)
	require.Equal(t, "agent", *first.AssigneeType)
	require.Equal(t, "a-1", *first.AssigneeID)
	require.NotNil(t, first.ParentIssueID)
	require.Equal(t, "i-0", *first.ParentIssueID)
	require.NotNil(t, first.ProjectID)
	require.Equal(t, "p-1", *first.ProjectID)
	require.Equal(t, 2, first.Stage, "stage 解码进拉取面（子任务阶段沿同步链传递的起点）")
	require.Equal(t, time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC), first.UpdatedAt)

	second := issues[1]
	require.Nil(t, second.Description, "可空字段按指针保真，归一交给映射层")
	require.Nil(t, second.AssigneeID)
	require.Nil(t, second.ParentIssueID)
	require.Nil(t, second.ProjectID)
	require.Zero(t, second.Stage, "响应未带 stage（或 0）解码为 0，语义兜底归消费方")
}

// TestListIssuesSendsQueryShape：翻页请求的查询串形态——limit 恒取服务端
// 上限 100（单页越大请求越少），offset 随累计条数推进。
func TestListIssuesSendsQueryShape(t *testing.T) {
	var gotRawQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"id":"i-1"}],"total":1}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.ListIssues(context.Background())
	require.NoError(t, err)
	got, err := url.ParseQuery(gotRawQuery)
	require.NoError(t, err)
	require.Equal(t, "100", got.Get("limit"))
	require.Equal(t, "0", got.Get("offset"))
}
