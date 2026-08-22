package tasksource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreateIssue(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		decodeBody(t, r, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"i-1","identifier":"INFERA-9","status":"backlog"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issue, err := c.CreateIssue(context.Background(), CreateIssueInput{
		Title:       "[infera-e2e] 冒烟",
		Description: "自动化测试",
		Status:      "backlog", // backlog 不触发任何 run（spike 实证）
	})
	require.NoError(t, err)
	require.Equal(t, "POST", gotMethod)
	require.Equal(t, "/api/issues", gotPath)
	require.Equal(t, "[infera-e2e] 冒烟", gotBody["title"])
	require.Equal(t, "自动化测试", gotBody["description"])
	require.Equal(t, "backlog", gotBody["status"])
	require.Equal(t, "i-1", issue.ID)
	require.Equal(t, "INFERA-9", issue.Identifier)
}

// TestCreateIssueProjectID：创建父 issue 需要固定 project（FR-2 项目固定）。
// project_id 是 POST /api/issues 的可选字段（spike 实证）：设置时随请求发送，
// 未设置时必须整个省略——空值序列化成 ""/null 会覆盖服务端默认。
func TestCreateIssueProjectID(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeBody(t, r, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"i-1","identifier":"INFERA-20","status":"todo"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	t.Run("设置时随请求发送", func(t *testing.T) {
		gotBody = nil
		_, err := c.CreateIssue(context.Background(), CreateIssueInput{
			Title: "父需求", Description: "d", Status: "backlog", ProjectID: "proj-fixed-1",
		})
		require.NoError(t, err)
		require.Equal(t, "proj-fixed-1", gotBody["project_id"])
	})

	t.Run("未设置时整个省略", func(t *testing.T) {
		gotBody = nil
		_, err := c.CreateIssue(context.Background(), CreateIssueInput{
			Title: "父需求", Description: "d", Status: "backlog",
		})
		require.NoError(t, err)
		_, hasProject := gotBody["project_id"]
		require.False(t, hasProject, "空 project_id 必须省略字段（可选字段语义，spike 实证）")
	})
}

// TestGetIssue：issue 读取（GET /api/issues/{id 或 key}——key 解析本身就是这个
// 端点，spike 实证）。大节点映射轮询至少消费 status；新端点同样必须走统一
// 认证与 X-Workspace-Id 注入通道（坑1）。
func TestGetIssue(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotWS string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotWS = r.Header.Get("X-Workspace-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"i-1","identifier":"INFERA-13","status":"in_progress"}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	issue, err := c.GetIssue(context.Background(), "INFERA-13")
	require.NoError(t, err)
	require.Equal(t, "GET", gotMethod)
	require.Equal(t, "/api/issues/INFERA-13", gotPath, "key 与 uuid 同走此端点")
	require.Equal(t, "Bearer mul_t", gotAuth)
	require.Equal(t, "ws-1", gotWS, "X-Workspace-Id 头必须随新端点注入（坑1）")
	require.Equal(t, "i-1", issue.ID)
	require.Equal(t, "INFERA-13", issue.Identifier)
	require.Equal(t, "in_progress", issue.Status, "大节点映射轮询至少要拿到 status")
}

func TestAssignAgent(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		decodeBody(t, r, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	require.NoError(t, c.AssignAgent(context.Background(), "i-1", "agent-uuid-1"))
	require.Equal(t, "PUT", gotMethod)
	require.Equal(t, "/api/issues/i-1", gotPath)
	require.Equal(t, "agent", gotBody["assignee_type"])
	require.Equal(t, "agent-uuid-1", gotBody["assignee_id"])
	require.Equal(t, "todo", gotBody["status"], "置 todo 触发 assigned agent 的 run")
	_, hasSuppress := gotBody["suppress_run"]
	require.False(t, hasSuppress, "指派不带 suppress_run——不带该字段服务端才入队 agent run")
}

// TestSetStatusSuppressRun（坑3）：改状态默认会唤醒 assignee；
// 编排方收尾/清理必须能带上 suppress_run（对应 API 同名字段）。
func TestSetStatusSuppressRun(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodeBody(t, r, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	t.Run("suppress=true 时随请求发送", func(t *testing.T) {
		gotBody = nil
		require.NoError(t, c.SetStatus(context.Background(), "i-1", "cancelled", true))
		require.Equal(t, "cancelled", gotBody["status"])
		require.Equal(t, true, gotBody["suppress_run"])
	})

	t.Run("suppress=false 时字段省略", func(t *testing.T) {
		gotBody = nil
		require.NoError(t, c.SetStatus(context.Background(), "i-1", "done", false))
		require.Equal(t, "done", gotBody["status"])
		_, hasSuppress := gotBody["suppress_run"]
		require.False(t, hasSuppress, "false 语义等同不带该字段，省略以贴近实证载荷")
	})
}

func TestListTaskRuns(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"t-old","agent_id":"a-1","status":"completed"},
			{"id":"t-new","agent_id":"a-1","status":"running","error":""}
		]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	runs, err := c.ListTaskRuns(context.Background(), "i-1")
	require.NoError(t, err)
	require.Equal(t, "/api/issues/i-1/task-runs", gotPath)
	require.Len(t, runs, 2)
	require.Equal(t, "t-new", runs[1].ID)
	require.Equal(t, "running", runs[1].Status)
}

// TestIsTerminal（坑2）：终态判定只认 task-runs 生命周期
// completed/failed/timeout/cancelled；issue status（todo/in_review/done…）
// 一律不是 task 终态——agent 会自己迁移 issue，那是设计行为。
func TestIsTerminal(t *testing.T) {
	for _, s := range []string{"completed", "failed", "timeout", "cancelled"} {
		require.True(t, IsTerminal(s), "task 终态 %q 应为 true", s)
	}
	for _, s := range []string{"pending", "queued", "running", "", "todo", "in_progress", "in_review", "done"} {
		require.False(t, IsTerminal(s), "非 task 终态 %q（含 issue status）应为 false", s)
	}
}

// TestWaitForTerminalIgnoresIssueStatus（坑2 的行为面）：agent 已把 issue 挪到
// in_review、但 task 仍 running 时，轮询必须继续等待终态——全程不得读取 issue 本体。
func TestWaitForTerminalIgnoresIssueStatus(t *testing.T) {
	var polls, issueGets atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/issues/i-1": // 恰好等于 issue GET 路径（task-runs 带后缀不匹配）
			issueGets.Add(1) // 计数仅供断言：终态判定不得读取 issue 本体
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"i-1","status":"in_review"}`))
		case r.URL.Path == "/api/issues/i-1/task-runs":
			n := polls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if n < 3 {
				_, _ = w.Write([]byte(`[{"id":"t-1","agent_id":"a-1","status":"running"}]`))
			} else {
				_, _ = w.Write([]byte(`[{"id":"t-1","agent_id":"a-1","status":"completed"}]`))
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	run, err := c.WaitForTerminal(context.Background(), "i-1", 2*time.Millisecond, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)
	require.Equal(t, int64(3), polls.Load(), "running 期间必须继续轮询")
	require.Equal(t, int64(0), issueGets.Load(), "终态判定不得读取 issue status（坑2）")
}

// TestWaitForTerminalNoRunYet：尚无 run（agent 未派发）时继续等待而非报错。
func TestWaitForTerminalNoRunYet(t *testing.T) {
	var polls atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := polls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n < 2 {
			_, _ = w.Write([]byte(`[]`))
		} else {
			_, _ = w.Write([]byte(`[{"id":"t-1","status":"completed"}]`))
		}
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	run, err := c.WaitForTerminal(context.Background(), "i-1", time.Millisecond, 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)
}

// TestWaitForTerminalReturnsFailedRun：终态是 failed 也算"到达终态"——
// 薄 client 返回事实（run + 终态），失败如何处置由编排方决定。
func TestWaitForTerminalReturnsFailedRun(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"t-1","status":"failed","error":"agent exploded"}]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	run, err := c.WaitForTerminal(context.Background(), "i-1", time.Millisecond, time.Second)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "agent exploded", run.Error)
}

// TestWaitForTerminalTimeout：超过 MaxWait 仍无终态 → 报错并带上最后看到的状态。
func TestWaitForTerminalTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"t-1","status":"running"}]`))
	}))
	defer ts.Close()

	c, err := New(ts.URL, "mul_t", "ws-1")
	require.NoError(t, err)

	_, err = c.WaitForTerminal(context.Background(), "i-1", time.Millisecond, 30*time.Millisecond)
	require.ErrorContains(t, err, "超时")
	require.ErrorContains(t, err, "running")
}
