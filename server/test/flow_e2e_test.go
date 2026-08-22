// 需求流转本地环境 E2E（INFERA-11 T07 / AC-1～AC-4）：对真实本地任务平台、
// 真实 postgres、真实 GitHub API 打全链路——infera 侧只经 infera API 驱动
// （AC-1 口径），上游侧用服务 token 模拟 agent 行为（改父 issue 状态、
// 按协议前缀发评论），合并动作用仓库内一次性测试分支/PR（标题带 test/e2e
// 标识，验完关闭），全部经 API，不打开任何页面。
//
// 门禁（环境缺失时显式 skip，裸 `go test ./...` 保持绿）：
//
//	TEST_DATABASE_URL            独立测试库（勿用共享脏库 infera_test；
//	                             可复用同容器 infera_test_t08 或自建）
//	TASK_SYNC_SERVER_URL           默认 http://localhost:8088；不可达时 skip
//	TASK_SYNC_TOKEN                服务 token（模拟 agent 行为的身份）
//	TASK_SYNC_WORKSPACE_ID         workspace id
//	TASK_SYNC_PROJECT_ID           派发目标 上游项目 id
//	TASK_SYNC_TECH_LEAD_AGENT_ID   派发指派的 agent——应指向不会被真实唤醒执行
//	                             的 agent（专用测试 agent），指派会置 todo
//	TASK_SYNC_WORKSPACE_SLUG       深链工作区段
//	GITHUB_TOKEN                 合并动作 PAT（装配 reqservice 本身也需要）
//	E2E_GITHUB_REPO              合并策略实测的目标仓库，默认 huiyangz/infera
//	GITHUB_API_URL               可选，GitHub Enterprise 入口覆盖
//
// 全量跑法（-p 1：server/test 各 e2e 共享测试库，必须串行）：
//
//	cd server && go test ./test/ -run TestFlowE2E -v -p 1
package test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/gatepoll"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/reqservice"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// put 发 JSON PUT，断言 2xx；out 非 nil 时解码响应体（与 e2e_test.go 的
// post/get 同款纪律）。
func put(t *testing.T, c *http.Client, url, body string, out any) {
	t.Helper()
	r, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(body))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(r)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "PUT %s -> %d", url, resp.StatusCode)
	require.GreaterOrEqual(t, resp.StatusCode, 200, "PUT %s -> %d", url, resp.StatusCode)
	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
}

// 轮询节奏：harness 的闸门轮询器用 2s 间隔（构造契约 (0,60s] 内，远快于
// 默认 30s，让 AC-3 的"2 分钟内"有充分余量）；等待预算 60s，同样远小于
// AC-3 的 2 分钟上界——预算内等到即满足 AC。
const (
	flowPollInterval = 2 * time.Second
	flowWaitBudget   = 60 * time.Second
	// flowHoldTicks：验证"不动作"档位时的静默窗口（≥3 个轮询周期）。
	flowHoldTicks = 7 * time.Second
)

// flowEnv 是门禁环境变量的聚合。ok=false 时 reason 说明缺什么。
type flowEnv struct {
	serverURL, token, wsID, projectID, techLeadID, slug string
	ghToken, ghAPIURL, ghRepo                           string
}

func flowEnvGate(t *testing.T) *flowEnv {
	t.Helper()
	e := &flowEnv{
		serverURL:  os.Getenv("TASK_SYNC_SERVER_URL"),
		token:      os.Getenv("TASK_SYNC_TOKEN"),
		wsID:       os.Getenv("TASK_SYNC_WORKSPACE_ID"),
		projectID:  os.Getenv("TASK_SYNC_PROJECT_ID"),
		techLeadID: os.Getenv("TASK_SYNC_TECH_LEAD_AGENT_ID"),
		slug:       os.Getenv("TASK_SYNC_WORKSPACE_SLUG"),
		ghToken:    os.Getenv("GITHUB_TOKEN"),
		ghAPIURL:   os.Getenv("GITHUB_API_URL"),
		ghRepo:     os.Getenv("E2E_GITHUB_REPO"),
	}
	if e.serverURL == "" {
		e.serverURL = "http://localhost:8088"
	}
	if e.ghRepo == "" {
		e.ghRepo = "huiyangz/infera" // 任务卡指定本仓库；E2E_GITHUB_REPO 可覆盖
	}
	var missing []string
	for k, v := range map[string]string{
		"TEST_DATABASE_URL":            os.Getenv("TEST_DATABASE_URL"),
		"TASK_SYNC_TOKEN":              e.token,
		"TASK_SYNC_WORKSPACE_ID":       e.wsID,
		"TASK_SYNC_PROJECT_ID":         e.projectID,
		"TASK_SYNC_TECH_LEAD_AGENT_ID": e.techLeadID,
		"TASK_SYNC_WORKSPACE_SLUG":     e.slug,
		"GITHUB_TOKEN":                 e.ghToken,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Skipf("流转 e2e 环境缺失（%s）——显式跳过，见 flow_e2e_test.go 文件头", strings.Join(missing, ", "))
	}
	return e
}

// probeTaskSource 可达性探测：连接层失败 = 不可达（skip）；有 HTTP 应答则继续，
// 凭据问题留给真实调用如实失败（与 internal/tasksource e2e 同款语义）。
func probeTaskSource(t *testing.T, e *flowEnv) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.serverURL+"/api/issue-statuses", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("X-Workspace-Id", e.wsID)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Skipf("本地任务平台不可达（%s）: %v —— 跳过流转 e2e", e.serverURL, err)
	}
	_ = resp.Body.Close()
}

// flowHarness 装配被测系统：真实 pg + 真实 tasksource/github client + reqservice
// + gatepoll（SettingsPolicy）+ httptest HTTP 面。每次 TRUNCATE 流转相关表，
// 隔离出干净的 requirements / gate_cards / audit_log / project_settings。
type flowHarness struct {
	e      *flowEnv
	pool   *pgxpool.Pool
	mc     *tasksource.Client
	gh     *github.Client
	client *http.Client // 已登录（cookie jar）
	base   string       // httptest URL
	projID string       // infera 侧项目（merge-policy 落 project_settings 用）
}

func newFlowHarness(t *testing.T) *flowHarness {
	t.Helper()
	e := flowEnvGate(t)
	probeTaskSource(t, e)

	dbURL := os.Getenv("TEST_DATABASE_URL")
	require.NoError(t, db.Migrate(dbURL), "迁移测试库失败（库被占用？）")
	pool, err := db.Connect(context.Background(), dbURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(),
		`TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err, "TRUNCATE 清库失败（勿用共享脏库，见文件头）")

	mc, err := tasksource.New(e.serverURL, e.token, e.wsID)
	require.NoError(t, err)
	var ghOpts []github.Option
	if e.ghAPIURL != "" {
		ghOpts = append(ghOpts, github.WithBaseURL(e.ghAPIURL))
	}
	gh, err := github.New(e.ghToken, ghOpts...)
	require.NoError(t, err)

	reqSvc, err := reqservice.New(pool, mc, gh, reqservice.Options{
		TaskSyncProjectID:     e.projectID,
		TechLeadAgentID:       e.techLeadID,
		TaskSyncServerURL:     e.serverURL,
		TaskSyncWorkspaceSlug: e.slug,
	})
	require.NoError(t, err)

	poller, err := gatepoll.New(gatepoll.NewPgStore(pool), mc, gh,
		gatepoll.NewSettingsPolicy(pool), flowPollInterval)
	require.NoError(t, err)
	require.NoError(t, poller.Start(context.Background()))
	t.Cleanup(poller.Stop)

	st := store.NewPg(pool)
	srv := api.NewServer(st, "flow-e2e-pass", nil)
	srv.SetRequirements(reqSvc)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}
	r, err := client.Post(ts.URL+"/api/login", "application/json",
		bytes.NewBufferString(`{"password":"flow-e2e-pass"}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, r.StatusCode, "登录失败")
	_ = r.Body.Close()

	// infera 侧项目：merge-policy 的 FK 落点（本地裸库路径即可，不触发
	// LsRemote——harness 未注入 git checker）。
	var proj store.Project
	post(t, client, ts.URL+"/api/projects",
		fmt.Sprintf(`{"name":"flow-e2e","repo_url":%q,"default_branch":"main"}`, newBare(t)), &proj)

	return &flowHarness{e: e, pool: pool, mc: mc, gh: gh, client: client, base: ts.URL, projID: proj.ID}
}

// --- infera API 面 ---

// requirementJSON 是 GET /api/requirements/{id} 的响应面（断言用到的字段）。
type requirementJSON struct {
	ID               string     `json:"id"`
	Node             string     `json:"node"`
	ExternalIssueID  string     `json:"external_issue_id"`
	ExternalIssueKey string     `json:"external_issue_key"`
	ExternalIssueURL string     `json:"external_issue_url"`
	PRURL            string     `json:"pr_url"`
	PendingCards     []cardJSON `json:"pending_cards"`
}

// cardJSON 是闸门卡的响应面。CommentID 空 = 状态类兜底卡（无评论溯源）。
type cardJSON struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Payload   string `json:"payload"`
	CommentID string `json:"comment_id"`
}

type auditJSON struct {
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

func (h *flowHarness) createRequirement(t *testing.T, title string) requirementJSON {
	t.Helper()
	var r requirementJSON
	post(t, h.client, h.base+"/api/requirements",
		fmt.Sprintf(`{"title":%q,"description":"[infera-e2e] 流转 e2e 自动化需求，验完自动收尾","acceptance_criteria":"e2e"}`, title), &r)
	require.NotEmpty(t, r.ID)
	require.NotEmpty(t, r.ExternalIssueID, "派发应建 上游父 issue")
	// 收尾：suppress_run 置 cancelled，绝不唤醒 assignee 再跑（坑3）。
	t.Cleanup(func() {
		if err := h.mc.SetStatus(context.Background(), r.ExternalIssueID, "cancelled", true); err != nil {
			t.Errorf("清理失败：置 cancelled（suppress_run）: %v", err)
		}
	})
	return r
}

func (h *flowHarness) reqDetail(t *testing.T, id string) requirementJSON {
	t.Helper()
	var d requirementJSON
	get(t, h.client, h.base+"/api/requirements/"+id, &d)
	return d
}

// waitForNode 等待大节点变化并返回耗时（AC-3：预算 60s ≪ 2 分钟上界）。
func (h *flowHarness) waitForNode(t *testing.T, id, want string, msg string) time.Duration {
	t.Helper()
	start := time.Now()
	deadline := start.Add(flowWaitBudget)
	var last requirementJSON
	for {
		last = h.reqDetail(t, id)
		if last.Node == want {
			return time.Since(start)
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitForNode 超时（%s）：等待节点 %s，末态 node=%s cards=%+v —— %s",
				flowWaitBudget, want, last.Node, last.PendingCards, msg)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// waitForCard 等待指定 kind 的待处理卡出现；payloadContains 非空时进一步
// 要求正文包含该片段（占位 agent 可能产生无关的"有新动态"卡，按内容精确命中）。
func (h *flowHarness) waitForCard(t *testing.T, id, kind, payloadContains string) cardJSON {
	t.Helper()
	deadline := time.Now().Add(flowWaitBudget)
	for {
		d := h.reqDetail(t, id)
		for _, c := range d.PendingCards {
			if c.Kind == kind && c.Status == "pending" &&
				(payloadContains == "" || strings.Contains(c.Payload, payloadContains)) {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitForCard 超时（%s）：未见 %s 待处理卡（正文含 %q），pending=%+v",
				flowWaitBudget, kind, payloadContains, d.PendingCards)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (h *flowHarness) cardAction(t *testing.T, reqID, cardID, action, body string, out any) {
	t.Helper()
	post(t, h.client,
		fmt.Sprintf("%s/api/requirements/%s/cards/%s/%s", h.base, reqID, cardID, action), body, out)
}

func (h *flowHarness) audit(t *testing.T, reqID string) []auditJSON {
	t.Helper()
	var out []auditJSON
	get(t, h.client, h.base+"/api/requirements/"+reqID+"/audit", &out)
	return out
}

// setPolicy 设合并策略档位（SettingsPolicy 真实读表，PUT 端点设档）。
func (h *flowHarness) setPolicy(t *testing.T, mode string, threshold int) {
	t.Helper()
	var out struct {
		Mode string `json:"mode"`
	}
	body := fmt.Sprintf(`{"mode":%q,"diff_line_threshold":%d}`, mode, threshold)
	put(t, h.client, fmt.Sprintf("%s/api/projects/%s/merge-policy", h.base, h.projID), body, &out)
	require.Equal(t, mode, out.Mode, "设档响应应回显档位")
}

// --- 上游侧（服务 token 模拟 agent 行为）---

func (h *flowHarness) mcSetStatus(t *testing.T, issueID, status string) {
	t.Helper()
	require.NoError(t, h.mc.SetStatus(context.Background(), issueID, status, true),
		"模拟 agent 改父 issue 状态 %s", status)
}

func (h *flowHarness) mcPost(t *testing.T, issueID, content string) {
	t.Helper()
	_, err := h.mc.PostComment(context.Background(), issueID, content)
	require.NoError(t, err, "模拟 agent 发评论")
}

func (h *flowHarness) mcComments(t *testing.T, issueID string) []tasksource.Comment {
	t.Helper()
	cs, err := h.mc.ListComments(context.Background(), issueID)
	require.NoError(t, err)
	return cs
}

// --- GitHub 侧测试 PR（一次性分支，验完删）---

// ghDo 裸 REST 调用（测试装置用，产品面走 internal/github client）。
func (h *flowHarness) ghDo(t *testing.T, method, path string, body any, out any) {
	t.Helper()
	base := "https://api.github.com"
	if h.e.ghAPIURL != "" {
		base = strings.TrimSuffix(h.e.ghAPIURL, "/")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+h.e.ghToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "infera-e2e")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "%s %s", method, path)
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		t.Fatalf("gh %s %s → %d: %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	}
}

// createTestPR 在目标仓库建一次性测试分支与 PR：base 分支自默认分支尖拉出
// （合并落点不污染默认分支），head 分支加一个 lines 行的文件。返回规范形
// PR URL。收尾删两个分支（PR 随 head 删除自动关闭）。
func (h *flowHarness) createTestPR(t *testing.T, lines int) (prURL string, number int) {
	t.Helper()
	repo := h.e.ghRepo
	ts := time.Now().Format("0102-150405")
	baseRef, headRef := "test/e2e-base-"+ts, "test/e2e-head-"+ts

	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	h.ghDo(t, http.MethodGet, "/repos/"+repo, nil, &repoInfo)
	require.NotEmpty(t, repoInfo.DefaultBranch, "读默认分支")

	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	h.ghDo(t, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+repoInfo.DefaultBranch, nil, &ref)
	require.NotEmpty(t, ref.Object.SHA)

	for _, br := range []string{baseRef, headRef} {
		h.ghDo(t, http.MethodPost, "/repos/"+repo+"/git/refs", map[string]string{
			"ref": "refs/heads/" + br, "sha": ref.Object.SHA,
		}, nil)
	}
	var sb strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&sb, "e2e line %d\n", i)
	}
	h.ghDo(t, http.MethodPut, "/repos/"+repo+"/contents/e2e/test-"+ts+".md", map[string]string{
		"message": "test/e2e " + ts,
		"content": base64.StdEncoding.EncodeToString([]byte(sb.String())),
		"branch":  headRef,
	}, nil)

	var pr struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	h.ghDo(t, http.MethodPost, "/repos/"+repo+"/pulls", map[string]string{
		"title": "test/e2e " + ts,
		"head":  headRef,
		"base":  baseRef,
		"body":  "[infera-e2e] 合并策略自动化实测，验完即清理",
	}, &pr)
	require.NotEmpty(t, pr.HTMLURL)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = h.ghDeleteRef(ctx, headRef)
		_ = h.ghDeleteRef(ctx, baseRef)
	})
	return pr.HTMLURL, pr.Number
}

// ghDeleteRef 删测试分支（收尾尽力而为：已删/不存在返回 nil 语义由调用方忽略）。
func (h *flowHarness) ghDeleteRef(ctx context.Context, branch string) error {
	base := "https://api.github.com"
	if h.e.ghAPIURL != "" {
		base = strings.TrimSuffix(h.e.ghAPIURL, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		base+"/repos/"+h.e.ghRepo+"/git/refs/heads/"+branch, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.e.ghToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "infera-e2e")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnprocessableEntity {
		return fmt.Errorf("删除分支 %s → %d", branch, resp.StatusCode)
	}
	return nil
}

// prState 读 PR 状态（open / closed）与是否已合并。
func (h *flowHarness) prState(t *testing.T, number int) (state string, merged bool) {
	t.Helper()
	pr, err := h.gh.GetPullRequest(context.Background(),
		strings.SplitN(h.e.ghRepo, "/", 2)[0], strings.SplitN(h.e.ghRepo, "/", 2)[1], number)
	require.NoError(t, err)
	mergedPr := pr.State == "closed"
	// state=closed 涵盖"已合并"与"被关闭"；用 gh API 的 merged 字段区分。
	var detail struct {
		Merged bool `json:"merged"`
	}
	h.ghDo(t, http.MethodGet, "/repos/"+h.e.ghRepo+"/pulls/"+fmt.Sprint(number), nil, &detail)
	return pr.State, detail.Merged && mergedPr
}

// --- AC-2 + AC-3：闸门卡（审批/决策/兜底）与状态映射、决策节点进出 ---

// TestFlowE2ECardsAndNodes 对本地环境实测：三类前缀卡中的审批/决策 + 两条
// 兜底路径 + 四状态映射（AC-3 全档）+ 需决策节点停驻/恢复（T08）+ 深链 +
// 审计。合并卡在 TestFlowE2E*Merge 系列覆盖（三类闸门齐全）。
func TestFlowE2ECardsAndNodes(t *testing.T) {
	h := newFlowHarness(t)
	r := h.createRequirement(t, "[infera-e2e] 闸门卡与状态映射 "+time.Now().Format("0102-150405"))

	// 派发：dispatched + 上游父 issue 已建（backlog→指派 todo）+ 深链（FR-8）。
	require.Equal(t, "dispatched", r.Node, "发起后应停在已派发")
	require.Equal(t, fmt.Sprintf("%s/%s/issues/%s", h.e.serverURL, h.e.slug, r.ExternalIssueID),
		r.ExternalIssueURL, "深链逃生口应为 {server}/{slug}/issues/{id}")
	issue, err := h.mc.GetIssue(context.Background(), r.ExternalIssueKey)
	require.NoError(t, err)
	require.Equal(t, r.ExternalIssueID, issue.ID, "key 解析应定位同一 issue")

	// AC-3：in_progress 映射（预算内等到即满足"2 分钟内"）。
	h.mcSetStatus(t, r.ExternalIssueID, "in_progress")
	elapsed := h.waitForNode(t, r.ID, "in_progress", "AC-3 状态映射 in_progress")
	require.Less(t, elapsed, 120*time.Second, "AC-3：状态变化必须 2 分钟内反映")
	t.Logf("AC-3 in_progress 映射耗时 %s", elapsed)

	// AC-2 审批卡：待批准 → 卡出现 → infera 批准 → 代发 approved 落上游 + 审计。
	h.mcPost(t, r.ExternalIssueID, "待批准：计划——分两批执行，先后端后前端，风险可控。")
	approval := h.waitForCard(t, r.ID, "approval", "待批准：")
	h.cardAction(t, r.ID, approval.ID, "approve", "{}", nil)
	var approvedSeen bool
	for _, c := range h.mcComments(t, r.ExternalIssueID) {
		if c.Content == "approved" {
			approvedSeen = true
		}
	}
	require.True(t, approvedSeen, "批准应代发 approved 评论到上游（FR-5）")
	require.Contains(t, h.audit(t, r.ID)[0].Action, "approve", "代理动作应落审计")

	// AC-2 决策卡（T08）：需要决策 → 卡与节点 needs_decision 同事务出现。
	h.mcPost(t, r.ExternalIssueID, "需要决策：依赖的下游服务暂不可用，是否继续等待？")
	h.waitForCard(t, r.ID, "decision", "需要决策：")
	h.waitForNode(t, r.ID, "needs_decision", "决策事件应推进节点 needs_decision")

	// 停驻断言：needs_decision 期间 上游状态推进挂起（多轮轮询不动）。
	h.mcSetStatus(t, r.ExternalIssueID, "in_review")
	time.Sleep(flowHoldTicks)
	d := h.reqDetail(t, r.ID)
	require.Equal(t, "needs_decision", d.Node, "停驻期间状态推进必须挂起（infera 是单一状态源）")

	// 兜底规则二：跃入 in_review 未见 verdict → 中性"有新动态"卡（无评论溯源）。
	var ruleTwo *cardJSON
	for i := range d.PendingCards {
		if d.PendingCards[i].Kind == "update" && d.PendingCards[i].CommentID == "" {
			ruleTwo = &d.PendingCards[i]
		}
	}
	require.NotNil(t, ruleTwo, "跃入 in_review 未见 verdict 应弹中性兜底卡（comment_id 空）")

	// 决策恢复（retry）：节点回执行中，后续轮询按父 issue 状态一轮内校正到 in_review。
	decision := h.waitForCard(t, r.ID, "decision", "需要决策：")
	h.cardAction(t, r.ID, decision.ID, "decide", `{"choice":"retry"}`, nil)
	h.waitForNode(t, r.ID, "in_progress", "决策 retry 应回执行中")
	h.waitForNode(t, r.ID, "in_review", "恢复后应按父 issue 状态校正")
	var retrySeen bool
	for _, c := range h.mcComments(t, r.ExternalIssueID) {
		if c.Content == "重试" {
			retrySeen = true
		}
	}
	require.True(t, retrySeen, "决策应代发「重试」评论到上游")

	// AC-2 兜底规则一：无前缀评论 → "有新动态"卡（有评论溯源，与规则二区分）。
	h.mcPost(t, r.ExternalIssueID, "进展：后端联调完成，开始自测。")
	update := h.waitForCard(t, r.ID, "update", "后端联调完成")
	require.NotEmpty(t, update.CommentID, "无前缀评论的兜底卡应带评论溯源（区别于规则二的中性卡）")

	// AC-3：done → delivered（第四档状态）。
	h.mcSetStatus(t, r.ExternalIssueID, "done")
	elapsed = h.waitForNode(t, r.ID, "delivered", "AC-3 状态映射 done")
	require.Less(t, elapsed, 120*time.Second, "AC-3：状态变化必须 2 分钟内反映")
	t.Logf("AC-3 done 映射耗时 %s", elapsed)
}

// --- AC-4：三档合并策略（各验一遍）+ AC-1 端到端零跳出 ---

// TestFlowE2EManualMergeJourney：AC-1 全程零跳出（发起→审批→执行→终审合并→
// 已交付，只经 infera API）+ AC-4 手动档（PASS 卡不自动合并，人点合并）。
func TestFlowE2EManualMergeJourney(t *testing.T) {
	h := newFlowHarness(t)
	h.setPolicy(t, "manual", 0)
	r := h.createRequirement(t, "[infera-e2e] 手动档合并全程 "+time.Now().Format("0102-150405"))

	// 审批（infera 内完成）。
	h.mcPost(t, r.ExternalIssueID, "待批准：计划——单包小改，附 PR。")
	approval := h.waitForCard(t, r.ID, "approval", "待批准：")
	h.cardAction(t, r.ID, approval.ID, "approve", "{}", nil)

	// 执行（模拟 agent 推进 + 建测试 PR + verdict）。
	h.mcSetStatus(t, r.ExternalIssueID, "in_progress")
	h.waitForNode(t, r.ID, "in_progress", "AC-1 执行段")
	prURL, num := h.createTestPR(t, 3)
	h.mcPost(t, r.ExternalIssueID, "verdict: PASS\n行级评审无阻塞意见。\n"+prURL)
	merge := h.waitForCard(t, r.ID, "merge", "verdict:")
	h.mcSetStatus(t, r.ExternalIssueID, "in_review")
	h.waitForNode(t, r.ID, "in_review", "AC-1 待验收段")
	d := h.reqDetail(t, r.ID)
	require.Equal(t, prURL, d.PRURL, "verdict 评论中的 PR 引用应被提取（FR-7/FR-8 深链）")

	// AC-4 手动档：静默窗口后卡仍待处理、PR 未被自动合并。
	time.Sleep(flowHoldTicks)
	d = h.reqDetail(t, r.ID)
	stillPending := false
	for _, c := range d.PendingCards {
		if c.ID == merge.ID && c.Status == "pending" {
			stillPending = true
		}
	}
	require.True(t, stillPending, "手动档 PASS 合并卡必须留待人点，不得自动合并")
	state, merged := h.prState(t, num)
	require.Equal(t, "open", state, "手动档不得自动合并 PR")
	require.False(t, merged)

	// 终审合并（infera 内完成，经 gh API）。
	var res struct {
		Merged bool   `json:"merged"`
		SHA    string `json:"sha"`
	}
	h.cardAction(t, r.ID, merge.ID, "merge", "{}", &res)
	require.True(t, res.Merged)
	require.NotEmpty(t, res.SHA)
	state, merged = h.prState(t, num)
	require.Equal(t, "closed", state, "合并后 PR 应关闭")
	require.True(t, merged, "PR 应真实合并")

	// 已交付。
	h.mcSetStatus(t, r.ExternalIssueID, "done")
	h.waitForNode(t, r.ID, "delivered", "AC-1 已交付段")
	var mergeAudit bool
	for _, a := range h.audit(t, r.ID) {
		if a.Action == "merge" && a.Actor == "user" {
			mergeAudit = true
		}
	}
	require.True(t, mergeAudit, "终审合并应落审计（actor=user）")
}

// TestFlowE2EAutoPassMerge：AC-4 auto_pass——verdict PASS 即自动合并，节点
// 直达已交付，审计 actor=system。
func TestFlowE2EAutoPassMerge(t *testing.T) {
	h := newFlowHarness(t)
	h.setPolicy(t, "auto_pass", 0)
	r := h.createRequirement(t, "[infera-e2e] auto_pass 自动合并 "+time.Now().Format("0102-150405"))

	h.mcSetStatus(t, r.ExternalIssueID, "in_progress")
	h.waitForNode(t, r.ID, "in_progress", "执行段")
	prURL, num := h.createTestPR(t, 2)
	h.mcPost(t, r.ExternalIssueID, "verdict: PASS\n"+prURL)
	h.waitForCard(t, r.ID, "merge", "verdict:")

	// 自动合并：节点直达已交付（预算内，远小于 AC-3 的 2 分钟）。
	h.waitForNode(t, r.ID, "delivered", "auto_pass PASS 应自动合并并直达已交付")
	_, merged := h.prState(t, num)
	require.True(t, merged, "PR 应被自动合并")
	var systemMerge bool
	for _, a := range h.audit(t, r.ID) {
		if a.Action == "merge" && a.Actor == "system" {
			systemMerge = true
		}
	}
	require.True(t, systemMerge, "自动合并应落审计 actor=system")
}

// TestFlowE2EThresholdMerge：AC-4 threshold——同一 PR 验两分支：阈值 1 时
// diff 超阈留人；上调阈值后同一张 pending 卡被下一轮清扫自动合并（策略
// 档位经 PUT 端点真实读表，切换即时生效）。
func TestFlowE2EThresholdMerge(t *testing.T) {
	h := newFlowHarness(t)
	h.setPolicy(t, "threshold", 1) // PR diff 5 行 > 1 → 留人
	r := h.createRequirement(t, "[infera-e2e] threshold 阈值合并 "+time.Now().Format("0102-150405"))

	h.mcSetStatus(t, r.ExternalIssueID, "in_progress")
	h.waitForNode(t, r.ID, "in_progress", "执行段")
	prURL, num := h.createTestPR(t, 5)
	require.NotEmpty(t, prURL)
	h.mcPost(t, r.ExternalIssueID, "verdict: PASS\n"+prURL)
	merge := h.waitForCard(t, r.ID, "merge", "verdict:")

	// 超阈值：卡留人、PR 不动。
	time.Sleep(flowHoldTicks)
	d := h.reqDetail(t, r.ID)
	stillPending := false
	for _, c := range d.PendingCards {
		if c.ID == merge.ID && c.Status == "pending" {
			stillPending = true
		}
	}
	require.True(t, stillPending, "diff 超阈值的 PASS 卡应留人")
	state, _ := h.prState(t, num)
	require.Equal(t, "open", state, "超阈值不得自动合并")

	// 上调阈值：同一张 pending 卡下一轮自动合并。
	h.setPolicy(t, "threshold", 100)
	h.waitForNode(t, r.ID, "delivered", "阈值上调后同卡应自动合并并直达已交付")
	_, merged := h.prState(t, num)
	require.True(t, merged, "阈值内 PR 应被自动合并")
}
