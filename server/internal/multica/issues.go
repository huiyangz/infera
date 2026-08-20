package multica

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Issue 是创建后我们消费的最小字段面。
type Issue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"` // 如 INFERA-5
	Status     string `json:"status"`
}

// Comment 是 agent 产物（issue 评论）的最小字段面。
type Comment struct {
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
type CreateIssueInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
}

// CreateIssue 创建 issue（POST /api/issues）。
func (c *Client) CreateIssue(ctx context.Context, in CreateIssueInput) (Issue, error) {
	var issue Issue
	if err := c.do(ctx, http.MethodPost, "/api/issues", in, &issue); err != nil {
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

// IsTerminal 报告 task 生命周期是否终态。完成判定只认 task-runs 的
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
		return TaskRun{}, fmt.Errorf("multica: WaitForTerminal 要求显式设置 maxWait，got %s", maxWait)
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
			return TaskRun{}, fmt.Errorf("multica: 等待 issue %s 的 task 终态超时（%s，最后状态 %q）", issueID, maxWait, lastStatus)
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
