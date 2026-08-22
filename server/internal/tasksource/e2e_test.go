//go:build e2e

// 本文件所有测试默认不参与编译执行（go test ./... 时不存在），需要显式
// -tags=e2e 才会构建。双重门禁：构建标签 + TASK_SYNC_* 环境变量两者齐备才真实执行。
package tasksource

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestE2ELocalLoop 对本地任务平台（默认 http://localhost:8088）打真实闭环：
// 创建 → 指派 agent → 轮询终态 → 拉产物评论。
//
// 门禁（不硬编码任何凭据）：TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID / TASK_SYNC_AGENT_ID
// 全部就绪才跑；本地任务平台不可达时显式 SKIP 并打印原因，绝不假绿。
// 凭据误配（可达但 401/403 等）则如实失败——那是错误配置，不是"不可达"。
//
// 成本控制（已随计划批准的默认决策）：issue 标题带 [infera-e2e] 前缀、描述注明
// 自动化测试并要求一行简短回复；断言产物后以 suppress_run 置 cancelled 收尾，
// 避免唤醒 assignee 的下一次 run（坑3）。
func TestE2ELocalLoop(t *testing.T) {
	token := os.Getenv("TASK_SYNC_TOKEN")
	wsID := os.Getenv("TASK_SYNC_WORKSPACE_ID")
	agentID := os.Getenv("TASK_SYNC_AGENT_ID")
	if token == "" || wsID == "" || agentID == "" {
		t.Skipf("需构建标签（go test -tags=e2e）+ TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID / TASK_SYNC_AGENT_ID 两者齐备才执行；当前变量未全设置（token=%t ws=%t agent=%t），跳过 E2E 闭环",
			token != "", wsID != "", agentID != "")
	}
	// E2E 默认打本地实例；TASK_SYNC_SERVER_URL 可覆盖（生产配置本身无默认值，坑4）。
	baseURL := os.Getenv("TASK_SYNC_SERVER_URL")
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
		t.Skipf("本地任务平台不可达（%s）: %v — 跳过 E2E 闭环", baseURL, err)
	}
	_ = resp.Body.Close()

	c, err := New(baseURL, token, wsID)
	require.NoError(t, err)
	ctx := context.Background()

	// 1. 创建（backlog 不触发 run；标题前缀 + 描述注明自动化测试）。
	title := "[infera-e2e] server 薄 client 闭环冒烟 " + time.Now().Format("0102-150405")
	desc := "这是 infera server tasksource 薄 client 的自动化 E2E 测试 issue，测试结束会被自动置为 cancelled，请忽略。\n\n" +
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

// TestE2ESmokeListSurface 对本地任务平台打拉取面的真冒烟（T01）：纯只读——
// ListProjects / ListIssues 拉全 workspace，再过一遍映射函数验证真实响应
// 形状能完整解码、父子关系在映射后自洽。不创建、不改任何实体，无收尾。
//
// 门禁与跳过语义同 TestE2ELocalLoop（只需 token + workspace，无需 agent）。
func TestE2ESmokeListSurface(t *testing.T) {
	token := os.Getenv("TASK_SYNC_TOKEN")
	wsID := os.Getenv("TASK_SYNC_WORKSPACE_ID")
	if token == "" || wsID == "" {
		t.Skipf("需构建标签（go test -tags=e2e）+ TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID 两者齐备才执行；当前变量未设置（token=%t ws=%t），跳过拉取面冒烟",
			token != "", wsID != "")
	}
	baseURL := os.Getenv("TASK_SYNC_SERVER_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8088"
	}
	probe, err := http.NewRequest(http.MethodGet, baseURL+"/api/issue-statuses", nil)
	require.NoError(t, err)
	probe.Header.Set("Authorization", "Bearer "+token)
	probe.Header.Set("X-Workspace-Id", wsID)
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Do(probe)
	if err != nil {
		t.Skipf("本地任务平台不可达（%s）: %v — 跳过拉取面冒烟", baseURL, err)
	}
	_ = resp.Body.Close()

	c, err := New(baseURL, token, wsID)
	require.NoError(t, err)
	ctx := context.Background()

	// 1. 项目拉全：当前 workspace 至少存在本流水线所属项目。
	projects, err := c.ListProjects(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, projects, "workspace 应至少有一个项目")
	t.Logf("拉到 %d 个项目", len(projects))
	projSnapshots := make([]ProjectSnapshot, len(projects))
	for i, p := range projects {
		require.NotEmpty(t, p.ID)
		require.NotEmpty(t, p.Title)
		projSnapshots[i] = MapProject(p)
	}

	// 2. issue 拉全（分页聚合），真实字段面解码 + 映射自洽。
	issues, err := c.ListIssues(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, issues, "workspace 应至少有一个 issue")
	t.Logf("拉到 %d 个 issue", len(issues))
	seen := make(map[string]bool, len(issues))
	issueSnapshots := make([]IssueSnapshot, len(issues))
	for i, is := range issues {
		require.NotEmpty(t, is.ID)
		require.NotEmpty(t, is.Identifier, "列表响应必须带人读键 identifier")
		require.NotEmpty(t, is.Status)
		require.False(t, seen[is.ID], "分页聚合不得重复同一条 issue: %s", is.ID)
		seen[is.ID] = true
		issueSnapshots[i] = MapIssue(is)
	}
	// 父子自洽：子的 ParentExternalID 若非空，必须指向拉到的某个 issue。
	for _, snap := range issueSnapshots {
		if snap.ParentExternalID != "" {
			require.True(t, seen[snap.ParentExternalID],
				"子的父引用 %s 必须在拉全的 issue 集合内", snap.ParentExternalID)
		}
	}
	// 项目归属自洽：issue 挂的项目必须出现在项目列表里（两级拉面的交叉验证）。
	projIDs := make(map[string]bool, len(projects))
	for _, p := range projects {
		projIDs[p.ID] = true
	}
	for _, snap := range issueSnapshots {
		if snap.ProjectExternalID != "" {
			require.True(t, projIDs[snap.ProjectExternalID],
				"issue 挂的项目 %s 必须在项目列表内", snap.ProjectExternalID)
		}
	}
}

// TestE2ESmokeProxySurface 对本地任务平台打新增面的真冒烟（不派发 agent，
// 秒级完成）：自建测试 issue → 代发评论 → GetIssue 读状态（uuid 与 key 两条
// 路径）→ ListCommentsSince 增量游标不漏不重。收尾 suppress_run 置 cancelled。
//
// 门禁与跳过语义同 TestE2ELocalLoop；绝不在真实业务 issue 上刷评论。
func TestE2ESmokeProxySurface(t *testing.T) {
	token := os.Getenv("TASK_SYNC_TOKEN")
	wsID := os.Getenv("TASK_SYNC_WORKSPACE_ID")
	if token == "" || wsID == "" {
		t.Skipf("需构建标签（go test -tags=e2e）+ TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID 两者齐备才执行；当前变量未设置（token=%t ws=%t），跳过代发面冒烟",
			token != "", wsID != "")
	}
	baseURL := os.Getenv("TASK_SYNC_SERVER_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8088"
	}
	probe, err := http.NewRequest(http.MethodGet, baseURL+"/api/issue-statuses", nil)
	require.NoError(t, err)
	probe.Header.Set("Authorization", "Bearer "+token)
	probe.Header.Set("X-Workspace-Id", wsID)
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Do(probe)
	if err != nil {
		t.Skipf("本地任务平台不可达（%s）: %v — 跳过代发面冒烟", baseURL, err)
	}
	_ = resp.Body.Close()

	c, err := New(baseURL, token, wsID)
	require.NoError(t, err)
	ctx := context.Background()

	// 1. 自建测试 issue（backlog 不触发 run；收尾 cancelled）。
	issue, err := c.CreateIssue(ctx, CreateIssueInput{
		Title:       "[infera-e2e] 代发面冒烟 " + time.Now().Format("0102-150405"),
		Description: "infera 任务同步 client 新增面（代发评论/状态读取/增量游标）的自动化冒烟，测试结束自动置 cancelled，请忽略。",
		Status:      "backlog",
	})
	require.NoError(t, err)
	require.NotEmpty(t, issue.ID)
	t.Cleanup(func() {
		if err := c.SetStatus(context.Background(), issue.ID, "cancelled", true); err != nil {
			t.Errorf("清理失败：置 cancelled（suppress_run）: %v", err)
		}
	})

	// 2. 代发评论（服务身份）。
	posted, err := c.PostComment(ctx, issue.ID, "[infera-e2e-smoke] 代发评论 1")
	require.NoError(t, err)
	require.NotEmpty(t, posted.ID, "201 回显应带评论 id（增量游标字段面）")

	// 3. 状态读取：uuid 与 key 同端点两条路径。
	got, err := c.GetIssue(ctx, issue.ID)
	require.NoError(t, err)
	require.Equal(t, issue.ID, got.ID)
	require.Equal(t, "backlog", got.Status, "大节点映射轮询至少要拿到 status")
	byKey, err := c.GetIssue(ctx, issue.Identifier)
	require.NoError(t, err)
	require.Equal(t, issue.ID, byKey.ID, "key 解析同端点（spike 实证）")

	// 4. 增量游标：零值全量 → 游标推进 → 空轮不重 → 新评论不漏（含同秒边界，
	//    两评论大概率落在同一秒内，恰好实测该路径）。
	first, cur, err := c.ListCommentsSince(ctx, issue.ID, CommentCursor{})
	require.NoError(t, err)
	require.Len(t, first, 1, "新 issue 首轮全量应恰有刚代发的 1 条: %+v", first)
	require.Equal(t, posted.ID, first[0].ID)

	empty, cur2, err := c.ListCommentsSince(ctx, issue.ID, cur)
	require.NoError(t, err)
	require.Empty(t, empty, "无新增时游标轮必须为空（不重）")
	require.Equal(t, cur, cur2)

	_, err = c.PostComment(ctx, issue.ID, "[infera-e2e-smoke] 代发评论 2")
	require.NoError(t, err)
	second, _, err := c.ListCommentsSince(ctx, issue.ID, cur)
	require.NoError(t, err)
	require.Len(t, second, 1, "新增 1 条后游标轮必须恰好命中它（不漏不重）: %+v", second)
	require.Contains(t, second[0].Content, "代发评论 2")
}
