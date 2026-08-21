package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// prJSON 是 GET /repos/{owner}/{repo}/pulls/{n} 的最小真实字段面。
const prJSON = `{
	"number": 7,
	"state": "open",
	"title": "feat: 需求流转核心契约",
	"draft": false,
	"mergeable": true,
	"mergeable_state": "clean",
	"html_url": "https://github.com/huiyangz/infera/pull/7",
	"head": {"sha": "abc123", "ref": "task/plan-t01"},
	"base": {"ref": "main"}
}`

func TestGetPullRequest(t *testing.T) {
	var gotMethod, gotPath string
	var gotAuth, gotAccept, gotAPIVersion, gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotAPIVersion = r.Header.Get("X-GitHub-Api-Version")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(prJSON))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	pr, err := c.GetPullRequest(context.Background(), "huiyangz", "infera", 7)
	require.NoError(t, err)
	require.Equal(t, "GET", gotMethod)
	require.Equal(t, "/repos/huiyangz/infera/pulls/7", gotPath)
	require.Equal(t, "Bearer ghp_t", gotAuth)
	require.Equal(t, "application/vnd.github+json", gotAccept)
	require.Equal(t, "2022-11-28", gotAPIVersion)
	require.NotEmpty(t, gotUA, "GitHub 对无 User-Agent 的请求直接 403")

	require.Equal(t, 7, pr.Number)
	require.Equal(t, "open", pr.State)
	require.Equal(t, "feat: 需求流转核心契约", pr.Title)
	require.False(t, pr.Draft)
	require.Equal(t, "clean", pr.MergeableState)
	require.Equal(t, "https://github.com/huiyangz/infera/pull/7", pr.HTMLURL)
	require.Equal(t, "abc123", pr.Head.SHA)
	require.Equal(t, "task/plan-t01", pr.Head.Ref)
	require.NotNil(t, pr.Mergeable, "mergeable 已计算时应为非 nil")
	require.True(t, *pr.Mergeable)
}

// TestGetPullRequestDecodesMerged：merged 解码。GitHub 的 state 只有
// open/closed 两值——"closed" 既可能是合并成功也可能是被驳回关闭，
// 合并与否必须单看 merged。closed+merged=false 是驳回形态，调用方
// （gatepoll 自动合并收口）据此区分"已了结"与"转人工"。
func TestGetPullRequestDecodesMerged(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"closed 且已合并", `{"number":7,"state":"closed","merged":true}`, true},
		{"closed 未合并（驳回）", `{"number":7,"state":"closed","merged":false}`, false},
		{"open 时缺省为 false", `{"number":7,"state":"open"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()
			c, err := New("ghp_t", WithBaseURL(ts.URL))
			require.NoError(t, err)
			pr, err := c.GetPullRequest(context.Background(), "o", "r", 7)
			require.NoError(t, err)
			require.Equal(t, tc.want, pr.Merged)
		})
	}
}

// TestGetPullRequestMergeableNull（坑）：mergeable 是懒计算字段——PR 刚创建或
// GitHub 尚未算完时返回 null，语义是"未知"，不是 false。必须用 *bool 保留
// 三态，调用方（合并卡）据此决定重查而不是误判冲突。
func TestGetPullRequestMergeableNull(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":7,"state":"open","mergeable":null,"mergeable_state":"unknown"}`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	pr, err := c.GetPullRequest(context.Background(), "o", "r", 7)
	require.NoError(t, err)
	require.Nil(t, pr.Mergeable, "null 必须保留为 nil（未知 ≠ 不可合并）")
	require.Equal(t, "unknown", pr.MergeableState)
}

// TestGetPullRequestBaseURLTrailingSlash：BaseURL 带结尾斜杠时路径不双斜杠。
func TestGetPullRequestBaseURLTrailingSlash(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(prJSON))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL+"/"))
	require.NoError(t, err)

	_, err = c.GetPullRequest(context.Background(), "o", "r", 7)
	require.NoError(t, err)
	require.Equal(t, "/repos/o/r/pulls/7", gotPath)
}

// TestGetPullRequestNotFound：404 归因为 *APIError（带状态码与 GitHub message），
// 调用方可 errors.As 区分"PR 不存在"与网络/鉴权失败。
func TestGetPullRequestNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	_, err = c.GetPullRequest(context.Background(), "o", "r", 404)
	require.Error(t, err)
	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr), "错误必须可归因为 *APIError，got %T: %v", err, err)
	require.Equal(t, 404, apiErr.StatusCode)
	require.Equal(t, "Not Found", apiErr.Message)
	require.Contains(t, err.Error(), "/repos/o/r/pulls/404", "错误文本带 method+path 便于排查")
}

// TestDefaultBaseURL：不覆盖 BaseURL 时请求 api.github.com。
// 用注入的 RoundTripper 截获请求——不触网，离线可跑。
func TestDefaultBaseURL(t *testing.T) {
	var gotHost string
	c, err := New("ghp_t", WithHTTPClient(&http.Client{Transport: stubTransport{
		roundTrip: func(r *http.Request) (*http.Response, error) {
			gotHost = r.URL.Host
			return jsonResp(http.StatusOK, `{"number":1,"state":"open"}`), nil
		},
	}}))
	require.NoError(t, err)

	_, err = c.GetPullRequest(context.Background(), "o", "r", 1)
	require.NoError(t, err)
	require.Equal(t, "api.github.com", gotHost)
}

// TestListReviewComments：单页即收（<100 条）——路径、per_page、字段解码全断言，
// 且不得多发第二页请求。
func TestListReviewComments(t *testing.T) {
	var gotPath, gotPerPage, gotPage string
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotPath = r.URL.Path
		gotPerPage = r.URL.Query().Get("per_page")
		gotPage = r.URL.Query().Get("page")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"id": 1001,
				"path": "server/internal/flow/state.go",
				"line": 42,
				"side": "RIGHT",
				"body": "这里缺少 nil 检查",
				"user": {"login": "infera-reviewer"},
				"created_at": "2026-08-21T05:00:00Z",
				"in_reply_to_id": 1000,
				"diff_hunk": "@@ -40,6 +40,7 @@ func Advance("
			},
			{
				"id": 1002,
				"path": "server/internal/flow/gate.go",
				"line": 0,
				"original_line": 17,
				"side": "LEFT",
				"body": "删除的行上也有评论",
				"user": {"login": "infera-reviewer"},
				"created_at": "2026-08-21T05:01:00Z"
			}
		]`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	comments, err := c.ListReviewComments(context.Background(), "o", "r", 7)
	require.NoError(t, err)
	require.Equal(t, "/repos/o/r/pulls/7/comments", gotPath)
	require.Equal(t, "100", gotPerPage)
	require.Equal(t, "1", gotPage)
	require.Equal(t, 1, requests, "不足一页时不得再请求第二页")

	require.Len(t, comments, 2)
	first := comments[0]
	require.Equal(t, int64(1001), first.ID)
	require.Equal(t, "server/internal/flow/state.go", first.Path)
	require.Equal(t, 42, first.Line)
	require.Equal(t, "RIGHT", first.Side)
	require.Equal(t, "这里缺少 nil 检查", first.Body)
	require.Equal(t, "infera-reviewer", first.User.Login)
	require.Equal(t, int64(1000), first.InReplyToID)
	require.Contains(t, first.DiffHunk, "@@ -40,6 +40,7 @@")

	second := comments[1]
	require.Equal(t, 0, second.Line, "删除行评论的 line 为 0，行号在 original_line")
	require.Equal(t, 17, second.OriginalLine)
	require.Equal(t, "LEFT", second.Side)
}

// TestListReviewCommentsPagination：第一页恰好满页（100 条）时必须继续翻页，
// 直到某页不满；按页序拼接返回。
func TestListReviewCommentsPagination(t *testing.T) {
	var pages []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		w.Header().Set("Content-Type", "application/json")
		if page == "" || page == "1" {
			_, _ = w.Write([]byte("[" + repeatComments(1, 100) + "]")) // 满页 → 必须有下一页
			return
		}
		_, _ = w.Write([]byte("[" + repeatComments(101, 2) + "]")) // 不满 → 到此为止
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	comments, err := c.ListReviewComments(context.Background(), "o", "r", 7)
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, pages, "恰好两页：满页后继续，不满即停")
	require.Len(t, comments, 102)
	require.Equal(t, int64(101), comments[100].ID, "跨页按序拼接")
}

// repeatComments 生成 id 从 start 开始的 n 条 review comment JSON（分页测试用）。
func repeatComments(startID, n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"id":%d,"path":"f.go","line":1,"body":"c%d","user":{"login":"r"}}`, startID+i, startID+i)
	}
	return sb.String()
}

// TestGetDiffStats：diff 行数统计（GET /pulls/{n}/files 逐文件求和，自动翻页），
// 供阈值合并档位判断（FR-6：diff 行数低于阈值自动合）。
func TestGetDiffStats(t *testing.T) {
	var gotPath, gotPerPage string
	var pages []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPerPage = r.URL.Query().Get("per_page")
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		w.Header().Set("Content-Type", "application/json")
		if page == "" || page == "1" {
			_, _ = w.Write([]byte("[" + repeatFileStats(1, 100) + "]")) // 满页 → 翻页
			return
		}
		_, _ = w.Write([]byte(`[
			{"filename":"server/internal/flow/gate.go","status":"modified","additions":7,"deletions":3,"changes":10}
		]`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	stats, err := c.GetDiffStats(context.Background(), "o", "r", 7)
	require.NoError(t, err)
	require.Equal(t, "/repos/o/r/pulls/7/files", gotPath)
	require.Equal(t, "100", gotPerPage)
	require.Equal(t, []string{"1", "2"}, pages)
	// 第 1 页 100 个文件每个 +2/-1（changes 3），第 2 页 1 个文件 +7/-3（changes 10）。
	require.Equal(t, 101, stats.Files)
	require.Equal(t, 207, stats.Additions)
	require.Equal(t, 103, stats.Deletions)
	require.Equal(t, 310, stats.Changes)
}

// repeatFileStats 生成 n 个文件的 files 载荷（每个 additions=2 deletions=1）。
func repeatFileStats(start, n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"filename":"f%d.go","status":"modified","additions":2,"deletions":1,"changes":3}`, start+i)
	}
	return sb.String()
}

// TestGetDiffStatsEmpty：无文件差异（空 PR）→ 零值统计，不是错误。
func TestGetDiffStatsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	stats, err := c.GetDiffStats(context.Background(), "o", "r", 7)
	require.NoError(t, err)
	require.Equal(t, DiffStats{}, stats)
}

// TestMergePullRequest：PUT /repos/{owner}/{repo}/pulls/{n}/merge。
// Method 留空 → 默认 merge commit；显式 squash 透传；自定义标题/说明随载荷。
func TestMergePullRequest(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		decodeBody(t, r, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":true,"sha":"deadbeef","message":"Pull Request successfully merged"}`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	t.Run("默认 merge commit", func(t *testing.T) {
		gotBody = nil
		res, err := c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{})
		require.NoError(t, err)
		require.Equal(t, "PUT", gotMethod)
		require.Equal(t, "/repos/o/r/pulls/7/merge", gotPath)
		require.Equal(t, "merge", gotBody["merge_method"], "留空默认 merge commit")
		require.True(t, res.Merged)
		require.Equal(t, "deadbeef", res.SHA)
		require.Equal(t, "Pull Request successfully merged", res.Message)
	})

	t.Run("squash + 自定义标题", func(t *testing.T) {
		gotBody = nil
		_, err := c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{
			Method:        MergeSquash,
			CommitTitle:   "merge(infera): 需求 A 终审通过",
			CommitMessage: "由 infera 合并卡触发",
		})
		require.NoError(t, err)
		require.Equal(t, "squash", gotBody["merge_method"])
		require.Equal(t, "merge(infera): 需求 A 终审通过", gotBody["commit_title"])
		require.Equal(t, "由 infera 合并卡触发", gotBody["commit_message"])
	})

	t.Run("rebase 透传", func(t *testing.T) {
		gotBody = nil
		_, err := c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{Method: MergeRebase})
		require.NoError(t, err)
		require.Equal(t, "rebase", gotBody["merge_method"])
	})
}

// TestMergePullRequestInvalidMethod：非法合并方法在客户端就地报错，
// 不得发出任何 HTTP 请求。
func TestMergePullRequestInvalidMethod(t *testing.T) {
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	_, err = c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{Method: MergeMethod("bogus")})
	require.ErrorContains(t, err, "bogus")
	require.Equal(t, 0, requests, "非法方法必须构造期后、发请求前拦下")
}

// TestMergePullRequestNotMergeable（405 路径）：GitHub 对不可合并 PR 返回 405，
// 归因为 *APIError（状态码 + message），且 IsMergeBlocked 判真。
func TestMergePullRequestNotMergeable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"message":"Pull Request is not mergeable"}`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	_, err = c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{})
	require.Error(t, err)
	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, 405, apiErr.StatusCode)
	require.Equal(t, "Pull Request is not mergeable", apiErr.Message)
	require.True(t, IsMergeBlocked(err), "405 属于\"当前不可合并\"类失败")
}

// TestMergePullRequestConflict（409 路径）：base/head 在合并窗口内变化 → 409，
// 同样归因 IsMergeBlocked（调用方可选择重试）。
func TestMergePullRequestConflict(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"conflict: Base branch was modified"}`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	_, err = c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{})
	var apiErr *APIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, 409, apiErr.StatusCode)
	require.True(t, IsMergeBlocked(err))
}

// TestMergePullRequestMergedFalseRace：HTTP 200 但 merged=false（合并窗口内状态
// 竞态，GitHub 以 200 + merged=false 表达）→ 报错并归因 ErrMergeRefused。
func TestMergePullRequestMergedFalseRace(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"merged":false,"message":"Pull Request is not mergeable"}`))
	}))
	defer ts.Close()

	c, err := New("ghp_t", WithBaseURL(ts.URL))
	require.NoError(t, err)

	_, err = c.MergePullRequest(context.Background(), "o", "r", 7, MergeInput{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMergeRefused), "merged=false 必须可 errors.Is 归因，got %v", err)
	require.Contains(t, err.Error(), "Pull Request is not mergeable")
	require.True(t, IsMergeBlocked(err))
}

// TestIsMergeBlockedNegative：鉴权失败（401）与网络错误不是"不可合并"——
// 调用方（自动合并档位）对这两类的处置是报错转人工，不是重试。
func TestIsMergeBlockedNegative(t *testing.T) {
	require.False(t, IsMergeBlocked(&APIError{StatusCode: 401, Message: "Bad credentials"}))
	require.False(t, IsMergeBlocked(&APIError{StatusCode: 404, Message: "Not Found"}))
	require.False(t, IsMergeBlocked(context.DeadlineExceeded), "非本包错误类型不误判")
	require.False(t, IsMergeBlocked(nil))
}

// decodeBody 解码 JSON 请求体（断言载荷用）。
func decodeBody(t *testing.T, r *http.Request, out *map[string]any) {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	if len(b) == 0 {
		return
	}
	require.NoError(t, json.Unmarshal(b, out), "body: %s", b)
}
