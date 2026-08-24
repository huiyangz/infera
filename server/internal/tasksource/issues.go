package tasksource

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Issue 是 issue 的最小字段面。GET /api/issues 与 GET /api/issues/{id}
// 返回同一 IssueResponse 形状，共用此类型；拉取面（T01）在其上补齐映射
// 消费的字段——可空字段按指针保真（未填 = null），归一交给 MapIssue。
type Issue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"` // 如 INFERA-5
	Status     string `json:"status"`

	// —— 拉取面（GET /api/issues）补齐的映射消费字段（T01）——
	Title         string    `json:"title"`
	Description   *string   `json:"description"`     // 可空：未填为 null
	Priority      string    `json:"priority"`        // urgent/high/medium/low/none
	AssigneeType  *string   `json:"assignee_type"`   // 负责人类型（member|agent|squad），可空
	AssigneeID    *string   `json:"assignee_id"`     // 负责人 id，可空
	ParentIssueID *string   `json:"parent_issue_id"` // 父子关系：父 issue id，可空（顶层）
	ProjectID     *string   `json:"project_id"`      // 归属项目，可空
	Stage         int       `json:"stage"`           // 子任务所属阶段（1..N；顶层/未带 = 0）
	Labels        []Label   `json:"labels"`          // 逐 issue 标签（完整对象 id/name/color；未带 = nil）
	UpdatedAt     time.Time `json:"updated_at"`
}

// Comment 是 agent 产物（issue 评论）的最小字段面。
type Comment struct {
	ID         string    `json:"id"`          // 游标协议字段面：增量拉取必须能定位每条评论
	AuthorType string    `json:"author_type"` // agent | member
	AuthorID   string    `json:"author_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// TaskRun 是 issue 的一次 agent 执行（GET /api/issues/{id}/task-runs 的元素）。
type TaskRun struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
	Status  string `json:"status"` // pending/queued/running → 终态见 IsTerminal
	Error   string `json:"error"`
}

// CreateIssueInput 是 POST /api/issues 的载荷面；Status 传 "backlog"
// 不触发任何 run（spike 实证），指派时再置 todo 唤醒 agent。
// Priority/Assignee* 是创建载荷扩展面（L202608230412-1-T01，本地 capture
// 实证——官方 CLI `issue create --priority --assignee-id` 的同一载荷形状）；
// 内联指派 + status=todo 由服务端入队该 agent 的 run（平台文档语义）。
type CreateIssueInput struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`    // 固定 project（FR-2 项目固定）；空则整个省略
	Priority     string `json:"priority,omitempty"`      // urgent/high/medium/low/none；空则整个省略
	AssigneeType string `json:"assignee_type,omitempty"` // 负责人类型（agent|member|squad）
	AssigneeID   string `json:"assignee_id,omitempty"`   // 负责人 id；与 AssigneeType 成对
}

// CreateIssue 创建 issue（POST /api/issues）。
func (c *Client) CreateIssue(ctx context.Context, in CreateIssueInput) (Issue, error) {
	var issue Issue
	if err := c.do(ctx, http.MethodPost, "/api/issues", in, &issue); err != nil {
		return Issue{}, err
	}
	return issue, nil
}

// GetIssue 读取 issue（GET /api/issues/{id 或 key}——key 解析本身就是这个
// 端点，无需单独 resolve API，spike 实证）。大节点映射轮询至少消费 Status。
func (c *Client) GetIssue(ctx context.Context, idOrKey string) (Issue, error) {
	var issue Issue
	if err := c.do(ctx, http.MethodGet, "/api/issues/"+idOrKey, nil, &issue); err != nil {
		return Issue{}, err
	}
	return issue, nil
}

// AssignAgent 把 issue 指派给 agent 并置 todo（PUT /api/issues/{id}）。
// 载荷不带 suppress_run——不带该字段服务端才入队该 agent 的 run（spike 实证路径）。
func (c *Client) AssignAgent(ctx context.Context, issueID, agentID string) error {
	body := struct {
		AssigneeType string `json:"assignee_type"`
		AssigneeID   string `json:"assignee_id"`
		Status       string `json:"status"`
	}{AssigneeType: "agent", AssigneeID: agentID, Status: "todo"}
	return c.do(ctx, http.MethodPut, "/api/issues/"+issueID, body, nil)
}

// SetStatus 变更 issue 状态（PUT /api/issues/{id}）。改状态默认会唤醒 assignee
// （坑3）：编排方收尾清理必须传 suppressRun=true，否则会再触发一次 agent run。
func (c *Client) SetStatus(ctx context.Context, issueID, status string, suppressRun bool) error {
	body := struct {
		Status      string `json:"status"`
		SuppressRun bool   `json:"suppress_run,omitempty"` // false 省略，语义等同不带
	}{Status: status, SuppressRun: suppressRun}
	return c.do(ctx, http.MethodPut, "/api/issues/"+issueID, body, nil)
}

// ListTaskRuns 拉取 issue 的 task-runs（GET /api/issues/{id}/task-runs），
// 服务端按时间排序，末位为最新一次执行。
func (c *Client) ListTaskRuns(ctx context.Context, issueID string) ([]TaskRun, error) {
	var runs []TaskRun
	if err := c.do(ctx, http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

// issuePageSize 是 GET /api/issues 的翻页页大小：服务端上限 100（limit>100
// 会被压回 100，接入 spike 实证），取满上限让拉全量的请求次数最少。
const issuePageSize = 100

// maxIssuePages 是翻页不收敛防御的上限（100 页 × 100 条 = 1 万条 issue，
// 远超单 workspace 量级）。正常路径永远到不了这里；到达即服务端 offset
// 语义异常（恒返满页且 total 谎报），大声报错好过死循环或静默截断。
const maxIssuePages = 100

// ListIssues 拉取当前 workspace 全部 issue（GET /api/issues?limit=100&offset=N
// 逐页拉全，聚合保序返回）。
//
// 翻页协议（接入 spike ListIssues 实证）：
//   - 服务端按 limit/offset 切页，limit 上限 100；排序键确定
//     （position + created_at/id 决胜），跨页稳定不重不漏；
//   - 响应 {"issues": [...], "total": N}，total 是同 WHERE 的 COUNT 真实值
//     （计数失败时服务端退化为当页条数）；
//   - 收敛三条件任一满足即停：累计条数达 total / 当页短于请求页大小 /
//     （兜底）超过 maxIssuePages 报"不收敛"——前两条覆盖正常路径，
//     第三条兜住病态服务端（如恒返第一页）。
//
// 客户端既有增量游标（CommentCursor）只属于评论面；issue 列表端点没有
// 游标语义，这里按服务端原生 offset 协议翻页，不发明新机制。
func (c *Client) ListIssues(ctx context.Context) ([]Issue, error) {
	var all []Issue
	lastTotal := 0
	for page := 0; ; page++ {
		if page >= maxIssuePages {
			return nil, fmt.Errorf("tasksource: ListIssues 翻页不收敛：已拉 %d 页（%d 条）仍未满足 total=%d——服务端 offset 语义异常", page, len(all), lastTotal)
		}
		var resp struct {
			Issues []Issue `json:"issues"`
			Total  int     `json:"total"`
		}
		path := fmt.Sprintf("/api/issues?limit=%d&offset=%d", issuePageSize, len(all))
		if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Issues...)
		lastTotal = resp.Total
		if resp.Total > 0 && len(all) >= resp.Total {
			return all, nil
		}
		if len(resp.Issues) < issuePageSize {
			return all, nil
		}
	}
}

// completed/failed/timeout/cancelled（坑2）——issue status 不算数：agent 完成时
// 通常自己把 issue 挪到 in_review，那是设计行为而非编排方的完成信号。
func IsTerminal(status string) bool {
	switch status {
	case "completed", "failed", "timeout", "cancelled":
		return true
	default:
		return false
	}
}

// WaitForTerminal 轮询 task-runs 直到最新一次执行到达终态，返回该 TaskRun
// （Status 由调用方判读——failed 也是"到达终态"，处置归编排方）。
// 尚无 run（agent 未派发）时继续等待；超过 maxWait 报错并带最后看到的状态。
func (c *Client) WaitForTerminal(ctx context.Context, issueID string, interval, maxWait time.Duration) (TaskRun, error) {
	if maxWait <= 0 {
		return TaskRun{}, fmt.Errorf("tasksource: WaitForTerminal 要求显式设置 maxWait，got %s", maxWait)
	}
	if interval <= 0 {
		interval = 5 * time.Second // spike 实测小任务最佳间隔
	}
	deadline := time.Now().Add(maxWait)
	lastStatus := "（尚无 run）"
	for {
		runs, err := c.ListTaskRuns(ctx, issueID)
		if err != nil {
			return TaskRun{}, err
		}
		if len(runs) > 0 {
			latest := runs[len(runs)-1]
			lastStatus = latest.Status
			if IsTerminal(latest.Status) {
				return latest, nil
			}
		}
		if !time.Now().Before(deadline) {
			return TaskRun{}, fmt.Errorf("tasksource: 等待 issue %s 的 task 终态超时（%s，最后状态 %q）", issueID, maxWait, lastStatus)
		}
		select {
		case <-ctx.Done():
			return TaskRun{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// ListComments 拉取 issue 的全部评论（GET /api/issues/{id}/comments）。
func (c *Client) ListComments(ctx context.Context, issueID string) ([]Comment, error) {
	var comments []Comment
	if err := c.do(ctx, http.MethodGet, "/api/issues/"+issueID+"/comments", nil, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}
