package multica

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPostComment：代发评论（POST /api/issues/{id}/comments，请求体 content——
// multica-src CreateComment 实证；201 回显完整评论对象）。审批/决策/返工代理
// 动作全走这里，同样必须经过统一认证与 X-Workspace-Id 通道（坑1）。
func TestPostComment(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotWS string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		decodeBody(t, r, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"c-9","author_type":"agent","author_id":"svc-1","content":"approved","created_at":"2026-08-21T05:00:00Z"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	posted, err := c.PostComment(context.Background(), "i-1", "approved")
	require.NoError(t, err)
	require.Equal(t, "POST", gotMethod)
	require.Equal(t, "/api/issues/i-1/comments", gotPath)
	require.Equal(t, "approved", gotBody["content"])
	require.Equal(t, "Bearer mul_t", gotAuth)
	require.Equal(t, "ws-1", gotWS, "X-Workspace-Id 头必须随代发评论注入（坑1）")
	require.Equal(t, "c-9", posted.ID)
	require.Equal(t, "approved", posted.Content)
}

// TestPostCommentEmptyContent：空内容在客户端就地拒绝、不发请求——与服务端
// 语义对齐（multica-src 对空 content 回 400 "content is required"），
// 这里只是把同一规则前移，省一次注定失败的往返。
func TestPostCommentEmptyContent(t *testing.T) {
	var requests atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.PostComment(context.Background(), "i-1", "")
	require.ErrorContains(t, err, "content")
	require.Equal(t, int64(0), requests.Load(), "空内容必须客户端就地拒绝，不发出请求")
}

// TestPostCommentServerError：服务端拒绝（非 2xx）→ 错误带状态码与响应体，
// 代发动作失败不得被吞。
func TestPostCommentServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"issue not found"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.PostComment(context.Background(), "i-404", "approved")
	require.ErrorContains(t, err, "404")
	require.ErrorContains(t, err, "issue not found")
}

// TestListCommentsSinceZeroCursor：零值游标 → 不带 since 参数（首轮全量拉取），
// 走的是既有 GET /api/issues/{id}/comments 平面；返回的 next 游标锚定最末一条。
func TestListCommentsSinceZeroCursor(t *testing.T) {
	var gotRawQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"c-1","content":"待批准：计划正文","created_at":"2026-08-21T05:00:00Z"},{"id":"c-2","content":"approved","created_at":"2026-08-21T05:01:00Z"}]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	comments, next, err := c.ListCommentsSince(context.Background(), "i-1", CommentCursor{})
	require.NoError(t, err)
	require.Empty(t, gotRawQuery, "零值游标不带 since 参数——首轮全量")
	require.Len(t, comments, 2)
	require.Equal(t, "c-1", comments[0].ID, "增量评论必须携带 id（游标协议的字段面）")
	require.Equal(t, "c-2", next.AfterID, "next 游标锚定本轮最末一条")
	require.Equal(t, "2026-08-21T05:01:00Z", next.Since.Format(time.RFC3339),
		"next.Since 取该条评论的（秒级）created_at")
}

// TestListCommentsSinceSendsCursor：非零游标 → since=<RFC3339Nano UTC>。
func TestListCommentsSinceSendsCursor(t *testing.T) {
	var gotSince string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSince = r.URL.Query().Get("since")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	cur := CommentCursor{
		AfterID: "c-2",
		Since:   time.Date(2026, 8, 21, 5, 0, 0, 123456000, time.UTC),
	}
	_, _, err = c.ListCommentsSince(context.Background(), "i-1", cur)
	require.NoError(t, err)
	require.Equal(t, "2026-08-21T05:00:00.123456Z", gotSince,
		"游标以 RFC3339Nano UTC 串行化（服务端按此格式优先解析，multica-src 实证）")
}

// fakeCommentServer 是有状态的假服务器，按服务端真实保真度模拟：
//   - DB 精度：created_at 微秒（内部保留全精度）；
//   - 响应序列化：RFC3339 秒级截断（multica-src util.TimestampToString 实证——
//     正是这个截断让纯时间戳游标在同一秒内重复/漏发，本地冒烟实测踩中）；
//   - since 过滤：DB 精度严格大于；
//   - 响应顺序：恒按时间升序（服务端文档化不变量）。
type fakeCommentServer struct {
	mu    sync.Mutex
	items []fakeComment
}

type fakeComment struct {
	id   string
	at   time.Time // DB 精度（微秒）
	body string
}

func (f *fakeCommentServer) add(cs ...fakeComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, cs...)
}

func (f *fakeCommentServer) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		since = t
	}
	type wire struct {
		ID        string `json:"id"`
		Content   string `json:"content"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]wire, 0, len(f.items))
	for _, it := range f.items { // f.items 恒为升序（只追加）
		if !since.IsZero() && !it.at.After(since) {
			continue // 服务端语义：DB 精度严格大于 since
		}
		out = append(out, wire{ID: it.id, Content: it.body, CreatedAt: it.at.Format(time.RFC3339)}) // 秒级截断，真实保真
	}
	b, _ := json.Marshal(out)
	_, _ = w.Write(b)
}

// TestListCommentsSinceIncremental：常规增量——两轮轮询 + 中途新增，
// 不漏不重，next 游标可链式推进。
func TestListCommentsSinceIncremental(t *testing.T) {
	fake := &fakeCommentServer{}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)
	ctx := context.Background()

	t0 := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	fake.add(
		fakeComment{id: "c-1", at: t0, body: "待批准：计划正文"},
		fakeComment{id: "c-2", at: t0.Add(time.Minute), body: "approved"},
	)

	first, cur, err := c.ListCommentsSince(ctx, "i-1", CommentCursor{})
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, "c-2", cur.AfterID)

	fake.add(
		fakeComment{id: "c-3", at: t0.Add(2 * time.Hour), body: "需要决策：分支冲突"},
		fakeComment{id: "c-4", at: t0.Add(2*time.Hour + time.Second), body: "进度播报（无前缀，应触发兜底）"},
	)

	second, cur2, err := c.ListCommentsSince(ctx, "i-1", cur)
	require.NoError(t, err)
	require.Len(t, second, 2, "不漏：新增两条都必须返回")
	require.Equal(t, "c-3", second[0].ID)
	require.Equal(t, "c-4", second[1].ID)
	require.Equal(t, "c-4", cur2.AfterID)

	third, cur3, err := c.ListCommentsSince(ctx, "i-1", cur2)
	require.NoError(t, err)
	require.Empty(t, third, "无新增 → 空结果")
	require.Equal(t, cur2, cur3, "无新增时 next 游标原地不动")
}

// TestListCommentsSinceSameSecondBoundary：同一秒内的边界回归（本地冒烟
// 实测踩中的坑）——响应 created_at 是秒级截断，纯时间戳游标既会重复边界秒
// 的旧评论，也会漏掉边界秒内更晚的新评论。游标必须锚定评论 id 在响应中的
// 位置：since 只负责收窄服务端窗口（边界秒整组返回），客户端在响应内按
// AfterID 切位。
func TestListCommentsSinceSameSecondBoundary(t *testing.T) {
	fake := &fakeCommentServer{}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)
	ctx := context.Background()

	sec := time.Date(2026, 8, 21, 5, 0, 33, 0, time.UTC)
	fake.add(
		fakeComment{id: "c-1", at: sec.Add(100 * time.Microsecond), body: "待批准：计划正文"},
		fakeComment{id: "c-2", at: sec.Add(200 * time.Microsecond), body: "approved"}, // 与 c-1 同一显示秒
	)

	first, cur, err := c.ListCommentsSince(ctx, "i-1", CommentCursor{})
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, "c-2", cur.AfterID)
	require.Equal(t, sec, cur.Since, "Since 为该评论秒级截断的 created_at")

	// 无新增的跟进轮：边界秒整组会被服务端再次返回（µs > 秒截断值），
	// 客户端必须按 AfterID 切掉整组——空结果，不重。
	again, _, err := c.ListCommentsSince(ctx, "i-1", cur)
	require.NoError(t, err)
	require.Empty(t, again, "同秒边界跟进轮必须为空：边界秒整组虽被服务端返回，但都在游标锚点之前/即是锚点")

	// 边界秒内再新增一条（c-3 在 c-2 之后、同一显示秒）：必须不漏。
	fake.add(fakeComment{id: "c-3", at: sec.Add(300 * time.Microsecond), body: "verdict: PASS"})
	third, cur3, err := c.ListCommentsSince(ctx, "i-1", cur)
	require.NoError(t, err)
	require.Len(t, third, 1, "同秒内的新评论必须命中：不漏")
	require.Equal(t, "c-3", third[0].ID)
	require.Equal(t, "c-3", cur3.AfterID)

	// 下一秒的新评论照常命中。
	fake.add(fakeComment{id: "c-4", at: sec.Add(1500 * time.Millisecond), body: "进度播报"})
	fourth, _, err := c.ListCommentsSince(ctx, "i-1", cur3)
	require.NoError(t, err)
	require.Len(t, fourth, 1)
	require.Equal(t, "c-4", fourth[0].ID)
}

// TestListCommentsSinceAnchorDeleted：游标锚点评论被删（平台有 DELETE
// /api/comments/{id}）→ 位置不可知，退化为返回 since 窗口内全部评论
// （宁可重发不漏发），调用方按 id 幂等去重；文档化的兜底路径。
func TestListCommentsSinceAnchorDeleted(t *testing.T) {
	fake := &fakeCommentServer{}
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)
	ctx := context.Background()

	t0 := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	fake.add(fakeComment{id: "c-1", at: t0, body: "待批准：计划正文"})
	_, cur, err := c.ListCommentsSince(ctx, "i-1", CommentCursor{})
	require.NoError(t, err)
	require.Equal(t, "c-1", cur.AfterID)

	fake.add(fakeComment{id: "c-2", at: t0.Add(time.Minute), body: "需要决策：分支冲突"})
	// 锚点 c-1 被删除（fake 里模拟：移除后服务端不再返回它）。
	fake.mu.Lock()
	fake.items = fake.items[1:]
	fake.mu.Unlock()

	got, next, err := c.ListCommentsSince(ctx, "i-1", cur)
	require.NoError(t, err)
	require.Len(t, got, 1, "锚点删除 → 退化为窗口全量（本例仅剩 c-2）")
	require.Equal(t, "c-2", got[0].ID)
	require.Equal(t, "c-2", next.AfterID)
}
