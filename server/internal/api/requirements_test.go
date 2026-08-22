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

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/reqservice"
	"github.com/tokfinity/infera/internal/store"
)

// fakeReq 是 RequirementsAPI 的测试替身：记录调用、按需注入错误。
type fakeReq struct {
	err error // 通用注入错误（按用例设置）

	gotCreate    *reqservice.CreateInput
	gotGet       string
	gotApprove   string
	gotReject    feedbackCall
	gotDecide    decideCall
	gotRework    feedbackCall
	gotMerge     string
	gotAudit     string
	gotPolicySet string
	gotPolicyGet string
	gotPRReview  string
	policy       flow.MergePolicy
	mergeRes     github.MergeResult
	prReview     *reqservice.PRReview
}

type feedbackCall struct {
	reqID, cardID, feedback string
}

type decideCall struct {
	reqID, cardID, choice, text string
}

func (f *fakeReq) Create(ctx context.Context, in reqservice.CreateInput) (*reqservice.Requirement, error) {
	f.gotCreate = &in
	if f.err != nil {
		return nil, f.err
	}
	return &reqservice.Requirement{ID: "r-1", Title: in.Title, Node: flow.NodeDispatched,
		ExternalIssueKey: "INFERA-31", ExternalIssueURL: "http://m/i", Acceptors: []string{}}, nil
}

func (f *fakeReq) List(ctx context.Context) ([]reqservice.RequirementListItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []reqservice.RequirementListItem{}, nil
}

func (f *fakeReq) Get(ctx context.Context, id string) (*reqservice.RequirementDetail, error) {
	f.gotGet = id
	if f.err != nil {
		return nil, f.err
	}
	return &reqservice.RequirementDetail{PendingCards: []reqservice.GateCard{}}, nil
}

func (f *fakeReq) Approve(ctx context.Context, requirementID, cardID string) error {
	f.gotApprove = requirementID + "/" + cardID
	return f.err
}

func (f *fakeReq) Reject(ctx context.Context, requirementID, cardID, feedback string) error {
	f.gotReject = feedbackCall{requirementID, cardID, feedback}
	return f.err
}

func (f *fakeReq) Decide(ctx context.Context, requirementID, cardID, choice, text string) error {
	f.gotDecide = decideCall{requirementID, cardID, choice, text}
	return f.err
}

func (f *fakeReq) Merge(ctx context.Context, requirementID, cardID string) (github.MergeResult, error) {
	f.gotMerge = requirementID + "/" + cardID
	if f.err != nil {
		return github.MergeResult{}, f.err
	}
	return f.mergeRes, nil
}

func (f *fakeReq) Rework(ctx context.Context, requirementID, cardID, feedback string) error {
	f.gotRework = feedbackCall{requirementID, cardID, feedback}
	return f.err
}

func (f *fakeReq) ListAudit(ctx context.Context, requirementID string) ([]reqservice.AuditEntry, error) {
	f.gotAudit = requirementID
	if f.err != nil {
		return nil, f.err
	}
	return []reqservice.AuditEntry{}, nil
}

func (f *fakeReq) GetMergePolicy(ctx context.Context, projectID string) (flow.MergePolicy, error) {
	f.gotPolicyGet = projectID
	if f.err != nil {
		return flow.MergePolicy{}, f.err
	}
	return f.policy, nil
}

func (f *fakeReq) SetMergePolicy(ctx context.Context, projectID string, p flow.MergePolicy) (flow.MergePolicy, error) {
	f.gotPolicySet = projectID
	if f.err != nil {
		return flow.MergePolicy{}, f.err
	}
	f.policy = p
	return p, nil
}

func (f *fakeReq) GetPRReview(ctx context.Context, requirementID string) (*reqservice.PRReview, error) {
	f.gotPRReview = requirementID
	if f.err != nil {
		return nil, f.err
	}
	return f.prReview, nil
}

// newReqServer 构造注入 fakeReq 的登录态测试服务器（沿用 newServer/login 模式）。
func newReqServer(t *testing.T) (*httptest.Server, *fakeReq, *http.Client) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	fake := &fakeReq{}
	srv.SetRequirements(fake)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, fake, login(t, ts.URL)
}

// doJSON 发 JSON 请求并返回状态码与响应体。
func doJSON(t *testing.T, client *http.Client, method, url, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

const (
	testReqID  = "0b8c4b0e-0000-4000-8000-000000000010"
	testCardID = "0b8c4b0e-0000-4000-8000-000000000011"
	testProjID = "0b8c4b0e-0000-4000-8000-000000000012"
)

func TestRequirementsRoutesRequireAuth(t *testing.T) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	srv.SetRequirements(&fakeReq{})
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	routes := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/requirements"},
		{http.MethodGet, "/api/requirements"},
		{http.MethodGet, "/api/requirements/" + testReqID},
		{http.MethodPost, "/api/requirements/" + testReqID + "/cards/" + testCardID + "/approve"},
		{http.MethodPost, "/api/requirements/" + testReqID + "/cards/" + testCardID + "/reject"},
		{http.MethodPost, "/api/requirements/" + testReqID + "/cards/" + testCardID + "/decide"},
		{http.MethodPost, "/api/requirements/" + testReqID + "/cards/" + testCardID + "/merge"},
		{http.MethodPost, "/api/requirements/" + testReqID + "/cards/" + testCardID + "/rework"},
		{http.MethodGet, "/api/requirements/" + testReqID + "/audit"},
		{http.MethodGet, "/api/requirements/" + testReqID + "/pr-review"},
		{http.MethodGet, "/api/projects/" + testProjID + "/merge-policy"},
		{http.MethodPut, "/api/projects/" + testProjID + "/merge-policy"},
	}
	for _, rt := range routes {
		req, _ := http.NewRequest(rt.method, ts.URL+rt.path, nil)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s %s 未登录必须 401", rt.method, rt.path)
	}
}

func TestRequirementsServiceUnavailable(t *testing.T) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil) // 未 SetRequirements
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	c := login(t, ts.URL)

	code, _ := doJSON(t, c, http.MethodGet, ts.URL+"/api/requirements", "")
	require.Equal(t, http.StatusServiceUnavailable, code)
}

func TestCreateRequirementEndpoint(t *testing.T) {
	ts, fake, c := newReqServer(t)

	code, bodyStr := doJSON(t, c, http.MethodPost, ts.URL+"/api/requirements",
		`{"title":"支持流转","description":"d","acceptance_criteria":"ac","source":"s","priority":"high","acceptors":["zhang"]}`)
	require.Equal(t, http.StatusCreated, code)
	require.Equal(t, "支持流转", fake.gotCreate.Title)
	require.Equal(t, "d", fake.gotCreate.Description)
	require.Equal(t, "ac", fake.gotCreate.AcceptanceCriteria)
	require.Equal(t, []string{"zhang"}, fake.gotCreate.Acceptors)

	var got reqservice.Requirement
	require.NoError(t, json.Unmarshal([]byte(bodyStr), &got))
	require.Equal(t, "r-1", got.ID)
	require.Equal(t, "INFERA-31", got.ExternalIssueKey)
	require.Equal(t, "dispatched", string(got.Node))
	require.Equal(t, "http://m/i", got.ExternalIssueURL)

	// 反例：畸形 JSON
	code, _ = doJSON(t, c, http.MethodPost, ts.URL+"/api/requirements", "{oops")
	require.Equal(t, http.StatusBadRequest, code)
	// 反例：服务层校验失败（空标题）→ 400
	fake.err = reqservice.ErrInvalid
	code, _ = doJSON(t, c, http.MethodPost, ts.URL+"/api/requirements", `{"title":""}`)
	require.Equal(t, http.StatusBadRequest, code)
	// 反例：上游平台失败 → 502
	fake.err = context.DeadlineExceeded
	code, _ = doJSON(t, c, http.MethodPost, ts.URL+"/api/requirements", `{"title":"x"}`)
	require.Equal(t, http.StatusBadGateway, code)
}

func TestListAndGetRequirementEndpoints(t *testing.T) {
	ts, fake, c := newReqServer(t)

	code, bodyStr := doJSON(t, c, http.MethodGet, ts.URL+"/api/requirements", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "[]\n", bodyStr, "空列表序列化为 [] 而非 null")

	code, _ = doJSON(t, c, http.MethodGet, ts.URL+"/api/requirements/"+testReqID, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, testReqID, fake.gotGet)

	// 反例：畸形 UUID → 404（不透传服务层）
	code, _ = doJSON(t, c, http.MethodGet, ts.URL+"/api/requirements/not-a-uuid", "")
	require.Equal(t, http.StatusNotFound, code)
	// 反例：不存在 → 404
	fake.err = reqservice.ErrNotFound
	code, _ = doJSON(t, c, http.MethodGet, ts.URL+"/api/requirements/"+testReqID, "")
	require.Equal(t, http.StatusNotFound, code)
}

func TestCardActionEndpoints(t *testing.T) {
	ts, fake, c := newReqServer(t)
	base := ts.URL + "/api/requirements/" + testReqID + "/cards/" + testCardID

	// approve
	code, bodyStr := doJSON(t, c, http.MethodPost, base+"/approve", `{}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, `{"ok":true}`+"\n", bodyStr)
	require.Equal(t, testReqID+"/"+testCardID, fake.gotApprove)

	// reject 带反馈
	code, _ = doJSON(t, c, http.MethodPost, base+"/reject", `{"feedback":"太大"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "太大", fake.gotReject.feedback)

	// decide 自定义
	code, _ = doJSON(t, c, http.MethodPost, base+"/decide", `{"choice":"custom","text":"用 B"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, decideCall{testReqID, testCardID, "custom", "用 B"}, fake.gotDecide)

	// rework
	code, _ = doJSON(t, c, http.MethodPost, base+"/rework", `{"feedback":"返工"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "返工", fake.gotRework.feedback)

	// 错误映射：每类 sentinel → 状态码
	cases := []struct {
		err  error
		want int
	}{
		{reqservice.ErrInvalid, http.StatusBadRequest},
		{reqservice.ErrNotFound, http.StatusNotFound},
		{reqservice.ErrConflict, http.StatusConflict},
		{reqservice.ErrMergeBlocked, http.StatusConflict},
		{context.DeadlineExceeded, http.StatusBadGateway},
	}
	for _, tc := range cases {
		fake.err = tc.err
		code, _ = doJSON(t, c, http.MethodPost, base+"/approve", `{}`)
		require.Equal(t, tc.want, code, "err=%v", tc.err)
	}
	fake.err = nil

	// 反例：畸形 body
	code, _ = doJSON(t, c, http.MethodPost, base+"/reject", "{oops")
	require.Equal(t, http.StatusBadRequest, code)
	// 反例：畸形 UUID → 404
	code, _ = doJSON(t, c, http.MethodPost, ts.URL+"/api/requirements/x/cards/y/approve", `{}`)
	require.Equal(t, http.StatusNotFound, code)
}

func TestMergeEndpointReturnsResult(t *testing.T) {
	ts, fake, c := newReqServer(t)
	fake.mergeRes = github.MergeResult{Merged: true, SHA: "abc", Message: "merged"}
	path := ts.URL + "/api/requirements/" + testReqID + "/cards/" + testCardID + "/merge"

	code, bodyStr := doJSON(t, c, http.MethodPost, path, `{}`)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, bodyStr, `"merged":true`)
	require.Contains(t, bodyStr, "abc")

	// 阻塞类失败 → 409 且文案面向重试
	fake.err = reqservice.ErrMergeBlocked
	code, bodyStr = doJSON(t, c, http.MethodPost, path, `{}`)
	require.Equal(t, http.StatusConflict, code)
	require.Contains(t, bodyStr, "稍后重试")
}

func TestAuditEndpoint(t *testing.T) {
	ts, fake, c := newReqServer(t)

	code, bodyStr := doJSON(t, c, http.MethodGet, ts.URL+"/api/requirements/"+testReqID+"/audit", "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "[]\n", bodyStr)
	require.Equal(t, testReqID, fake.gotAudit)
}

// TestPRReviewEndpoint：PR 行级评审评论 + diff 概要只读端点（T09 加法
// 扩展，合并卡渲染数据源）。沿用既有错误码约定：缺 PR 关联 → 409。
func TestPRReviewEndpoint(t *testing.T) {
	ts, fake, c := newReqServer(t)
	fake.prReview = &reqservice.PRReview{
		PRURL: "https://github.com/huiyangz/infera/pull/7",
		Comments: []reqservice.PRReviewComment{
			{ID: 11, Path: "server/main.go", Line: 42, Side: "RIGHT",
				Body: "这里缺超时控制", Author: "reviewer-bot"},
		},
		Diff: reqservice.PRDiffStats{Files: 4, Additions: 120, Deletions: 8, Changes: 128},
	}
	path := ts.URL + "/api/requirements/" + testReqID + "/pr-review"

	code, bodyStr := doJSON(t, c, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, testReqID, fake.gotPRReview)
	require.Contains(t, bodyStr, `"pr_url":"https://github.com/huiyangz/infera/pull/7"`)
	require.Contains(t, bodyStr, "server/main.go")
	require.Contains(t, bodyStr, "reviewer-bot")
	require.Contains(t, bodyStr, `"files":4`)
	require.Contains(t, bodyStr, `"additions":120`)
	require.Contains(t, bodyStr, `"deletions":8`)

	// 需求尚未关联 PR → 冲突（与 merge 端点的无 PR 语义一致）
	fake.err = reqservice.ErrConflict
	code, _ = doJSON(t, c, http.MethodGet, path, "")
	require.Equal(t, http.StatusConflict, code)

	// 上游 github 故障 → 502（沿用 writeReqErr 默认归因）
	fake.err = errors.New("github: 502")
	code, _ = doJSON(t, c, http.MethodGet, path, "")
	require.Equal(t, http.StatusBadGateway, code)
}

func TestMergePolicyEndpoints(t *testing.T) {
	ts, fake, c := newReqServer(t)
	path := ts.URL + "/api/projects/" + testProjID + "/merge-policy"

	// 默认读取
	code, bodyStr := doJSON(t, c, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, bodyStr, `"mode"`)
	require.Equal(t, testProjID, fake.gotPolicyGet)

	// 写入往返
	code, bodyStr = doJSON(t, c, http.MethodPut, path, `{"mode":"threshold","diff_line_threshold":200}`)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, bodyStr, `"threshold"`)
	require.Contains(t, bodyStr, "200")
	require.Equal(t, flow.MergePolicy{Mode: flow.MergeThreshold, DiffLineThreshold: 200}, fake.policy)

	// 反例：非法档位语义 → 400
	fake.err = reqservice.ErrInvalid
	code, _ = doJSON(t, c, http.MethodPut, path, `{"mode":"manual","diff_line_threshold":5}`)
	require.Equal(t, http.StatusBadRequest, code)
	// 反例：项目不存在 → 404
	fake.err = reqservice.ErrNotFound
	code, _ = doJSON(t, c, http.MethodPut, path, `{"mode":"manual"}`)
	require.Equal(t, http.StatusNotFound, code)
	// 反例：畸形 UUID → 404
	code, _ = doJSON(t, c, http.MethodGet, ts.URL+"/api/projects/zzz/merge-policy", "")
	require.Equal(t, http.StatusNotFound, code)
}
