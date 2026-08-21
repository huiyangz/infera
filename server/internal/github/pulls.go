package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// PullRequest 是合并闸门消费的最小字段面。
type PullRequest struct {
	Number  int    `json:"number"`
	State   string `json:"state"` // open | closed（closed 含已合并，merged 单看 merge 卡点）
	Title   string `json:"title"`
	Draft   bool   `json:"draft"`
	HTMLURL string `json:"html_url"` // 深链逃生口（FR-8）
	// Merged 报告 PR 是否已被合并进 base。state 只有 open/closed 两值——
	// closed 既可能是合并成功也可能是被驳回关闭，收口判定必须单看本字段
	//（closed+merged=false 是驳回形态）。
	Merged bool `json:"merged"`

	// Mergeable 是懒计算字段：GitHub 未算完时为 null（nil），语义是"未知"
	// 而非"不可合并"——调用方应重查而不是误判冲突。
	Mergeable *bool `json:"mergeable"`
	// MergeableState 更细粒度：clean / dirty / blocked / draft / unstable / unknown。
	MergeableState string `json:"mergeable_state"`

	Head PullBranch `json:"head"`
	Base PullBranch `json:"base"`
}

// PullBranch 是 PR 的 head/base 侧最小字段面。
type PullBranch struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// GetPullRequest 拉取 PR 元数据（GET /repos/{owner}/{repo}/pulls/{n}）。
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var pr PullRequest
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.do(ctx, http.MethodGet, path, nil, &pr); err != nil {
		return PullRequest{}, err
	}
	return pr, nil
}

// User 是评论/评审作者的最小字段面。
type User struct {
	Login string `json:"login"`
}

// ReviewComment 是 PR 行级评审评论（GET /repos/{owner}/{repo}/pulls/{n}/comments
// 的元素），合并卡右侧栏渲染的数据源（FR-4）。
type ReviewComment struct {
	ID int64 `json:"id"`
	// Path + Line 定位评论挂的文件行。删除行上的评论 Line 为 0、行号在
	// OriginalLine（GitHub 的 original_* 语义：评论提交时的原始行号）。
	Path         string    `json:"path"`
	Line         int       `json:"line"`
	OriginalLine int       `json:"original_line"`
	Side         string    `json:"side"` // RIGHT | LEFT
	Body         string    `json:"body"`
	User         User      `json:"user"`
	CreatedAt    time.Time `json:"created_at"`
	InReplyToID  int64     `json:"in_reply_to_id"` // 0 = 顶层评论，非 0 = 回复
	DiffHunk     string    `json:"diff_hunk"`      // 评论处的 diff 上下文片段
}

// ListReviewComments 拉取 PR 的全部行级评审评论，自动翻页（per_page=100，
// 满页继续、不满即止）。
func (c *Client) ListReviewComments(ctx context.Context, owner, repo string, number int) ([]ReviewComment, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", url.PathEscape(owner), url.PathEscape(repo), number)
	return listPages[ReviewComment](c, ctx, path)
}

// FileStat 是 GET /repos/{owner}/{repo}/pulls/{n}/files 的单文件统计面。
type FileStat struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added | modified | removed | renamed ...
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"` // additions + deletions
}

// DiffStats 是 PR 全量文件的行数汇总，供阈值合并档位判断
// （FR-6：diff 行数低于阈值自动合并）。
type DiffStats struct {
	Files     int
	Additions int
	Deletions int
	Changes   int // additions + deletions 总和
}

// GetDiffStats 拉取 PR 文件级 diff 统计并求和（GET /repos/{owner}/{repo}/pulls/{n}/files，
// 自动翻页）。
func (c *Client) GetDiffStats(ctx context.Context, owner, repo string, number int) (DiffStats, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", url.PathEscape(owner), url.PathEscape(repo), number)
	files, err := listPages[FileStat](c, ctx, path)
	if err != nil {
		return DiffStats{}, err
	}
	var stats DiffStats
	for _, f := range files {
		stats.Files++
		stats.Additions += f.Additions
		stats.Deletions += f.Deletions
		stats.Changes += f.Changes
	}
	return stats, nil
}

// MergeMethod 是 GitHub 支持的合并方法。
type MergeMethod string

const (
	// MergeCommit 默认：普通 merge commit（保留全部分支历史）。
	MergeCommit MergeMethod = "merge"
	// MergeSquash 压缩为单 commit。
	MergeSquash MergeMethod = "squash"
	// MergeRebase 变基合入。
	MergeRebase MergeMethod = "rebase"
)

// valid 报告方法是否为 GitHub 认可的三选一。
func (m MergeMethod) valid() bool {
	return m == "" || m == MergeCommit || m == MergeSquash || m == MergeRebase
}

// MergeInput 是 merge 载荷面；Method 留空 → 默认 merge commit。
type MergeInput struct {
	Method        MergeMethod
	CommitTitle   string
	CommitMessage string
}

// MergeResult 是 PUT .../merge 的成功响应面。
type MergeResult struct {
	Merged  bool   `json:"merged"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
}

// ErrMergeRefused 归因"服务端拒绝本次合并"（HTTP 200 但 merged=false——
// 合并窗口内 PR 状态竞态，如刚变为不可合并/已被他人合并）。
// 调用方 errors.Is(err, ErrMergeRefused) 识别。
var ErrMergeRefused = errors.New("github: merge 被服务端拒绝（merged=false）")

// MergePullRequest 合并 PR（PUT /repos/{owner}/{repo}/pulls/{n}/merge）。
// 失败归因：405/409 → *APIError；200+merged=false → 包装 ErrMergeRefused；
// 两者均使 IsMergeBlocked(err) == true。
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int, in MergeInput) (MergeResult, error) {
	if !in.Method.valid() {
		return MergeResult{}, fmt.Errorf("github: 非法合并方法 %q（只接受 merge/squash/rebase，留空默认 merge）", in.Method)
	}
	if in.Method == "" {
		in.Method = MergeCommit
	}
	body := struct {
		MergeMethod   MergeMethod `json:"merge_method"`
		CommitTitle   string      `json:"commit_title,omitempty"`
		CommitMessage string      `json:"commit_message,omitempty"`
	}{MergeMethod: in.Method, CommitTitle: in.CommitTitle, CommitMessage: in.CommitMessage}

	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", url.PathEscape(owner), url.PathEscape(repo), number)
	var res MergeResult
	if err := c.do(ctx, http.MethodPut, path, body, &res); err != nil {
		return MergeResult{}, err
	}
	if !res.Merged {
		return MergeResult{}, fmt.Errorf("%w: %s", ErrMergeRefused, res.Message)
	}
	return res, nil
}

// IsMergeBlocked 报告 err 是否属于"当前不可合并"类失败：
// 405（不可合并/分支保护）、409（base/head 竞态）、200+merged=false（ErrMergeRefused）。
// 与鉴权失败、网络错误区分开——自动合并档位据此决定重试/转人工，而不是误重试 401。
func IsMergeBlocked(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMergeRefused) {
		return true
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusMethodNotAllowed || apiErr.StatusCode == http.StatusConflict
	}
	return false
}

// listPages 是翻页循环：per_page=100，页满（==100 条）继续，不满即止；
// 超过 maxListPages 页（2 万条）报错——数量异常通常意味着上游坏了，不静默截断。
func listPages[T any](c *Client, ctx context.Context, path string) ([]T, error) {
	const perPage = 100
	const maxListPages = 200
	var all []T
	for page := 1; page <= maxListPages; page++ {
		q := url.Values{"per_page": []string{strconv.Itoa(perPage)}, "page": []string{strconv.Itoa(page)}}
		var batch []T
		if err := c.do(ctx, http.MethodGet, path+"?"+q.Encode(), nil, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			return all, nil
		}
	}
	return nil, fmt.Errorf("github: %s 翻页超过 %d 页（每页 %d 条），疑似异常数据量，拒绝继续", path, maxListPages, perPage)
}
