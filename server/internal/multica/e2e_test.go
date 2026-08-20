package multica

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestE2ELocalLoop 对本地 Multica（默认 http://localhost:8088）打真实闭环：
// 创建 → 指派 agent → 轮询终态 → 拉产物评论。
//
// 门禁（不硬编码任何凭据）：MULTICA_TOKEN / MULTICA_WORKSPACE_ID / MULTICA_AGENT_ID
// 全部就绪才跑；本地 Multica 不可达时显式 SKIP 并打印原因，绝不假绿。
// 凭据误配（可达但 401/403 等）则如实失败——那是错误配置，不是"不可达"。
//
// 成本控制（已随计划批准的默认决策）：issue 标题带 [infera-e2e] 前缀、描述注明
// 自动化测试并要求一行简短回复；断言产物后以 suppress_run 置 cancelled 收尾，
// 避免唤醒 assignee 的下一次 run（坑3）。
func TestE2ELocalLoop(t *testing.T) {
	token := os.Getenv("MULTICA_TOKEN")
	wsID := os.Getenv("MULTICA_WORKSPACE_ID")
	agentID := os.Getenv("MULTICA_AGENT_ID")
	if token == "" || wsID == "" || agentID == "" {
		t.Skipf("MULTICA_TOKEN / MULTICA_WORKSPACE_ID / MULTICA_AGENT_ID 未全部设置（token=%t ws=%t agent=%t），跳过 E2E 闭环",
			token != "", wsID != "", agentID != "")
	}
	// E2E 默认打本地实例；MULTICA_SERVER_URL 可覆盖（生产配置本身无默认值，坑4）。
	baseURL := os.Getenv("MULTICA_SERVER_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8088"
	}

	// 可达性探测：连接层失败才算"不可达"（SKIP）；有 HTTP 应答则继续，
	// 凭据问题留给真实调用如实失败。
	probe, err := http.NewRequest(http.MethodGet, baseURL+"/api/issue-statuses", nil)
	require.NoError(t, err)
	probe.Header.Set("Authorization", "Bearer "+token)
	probe.Header.Set("X-Workspace-Id", wsID)
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Do(probe)
	if err != nil {
		t.Skipf("本地 Multica 不可达（%s）: %v — 跳过 E2E 闭环", baseURL, err)
	}
	_ = resp.Body.Close()

	c, err := New(baseURL, token, wsID)
	require.NoError(t, err)
	ctx := context.Background()

	// 1. 创建（backlog 不触发 run；标题前缀 + 描述注明自动化测试）。
	title := "[infera-e2e] server 薄 client 闭环冒烟 " + time.Now().Format("0102-150405")
	desc := "这是 infera server multica 薄 client 的自动化 E2E 测试 issue，测试结束会被自动置为 cancelled，请忽略。\n\n" +
		"任务：读仓库根 README.md 的第一段，回复一行评论——以 e2e-ok 开头，加一句话概括。除此之外不要做任何事，不要改代码。"
	issue, err := c.CreateIssue(ctx, CreateIssueInput{Title: title, Description: desc, Status: "backlog"})
	require.NoError(t, err)
	require.NotEmpty(t, issue.ID)
	t.Logf("created %s (%s)", issue.Identifier, issue.ID)

	// 收尾清理（坑3）：suppress_run=true 置 cancelled，绝不唤醒 assignee 再跑一轮。
	t.Cleanup(func() {
		if err := c.SetStatus(context.Background(), issue.ID, "cancelled", true); err != nil {
			t.Errorf("清理失败：置 cancelled（suppress_run）: %v", err)
		}
	})

	// 2. 指派（不带 suppress_run → 服务端入队 agent run）。
	require.NoError(t, c.AssignAgent(ctx, issue.ID, agentID))

	// 3. 轮询终态——只认 task-runs 生命周期（坑2），issue 状态迁移不参与判定。
	run, err := c.WaitForTerminal(ctx, issue.ID, 5*time.Second, 8*time.Minute)
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status, "agent run 未成功完成: %+v", run)

	// 4. 拉产物并断言内容：agent 的 e2e-ok 回复。
	comments, err := c.ListComments(ctx, issue.ID)
	require.NoError(t, err)
	var artifact string
	for _, cm := range comments {
		if cm.AuthorType == "agent" && strings.Contains(cm.Content, "e2e-ok") {
			artifact = cm.Content
			break
		}
	}
	require.NotEmpty(t, artifact, "未找到包含 e2e-ok 的 agent 评论，全部评论: %+v", comments)
	t.Logf("产物评论: %s", artifact)
}
