package reqservice

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/multica"
)

// testEnv 是一个用例的全部依赖：真实测试 DB + fake multica/github client。
type testEnv struct {
	svc *Service
	mc  *fakeMultica
	gh  *fakeGithub
}

// newEnv 引导测试 DB（沿用 store/pg_test.go 模式：TEST_DATABASE_URL 未设跳过）
// 并构造 Service。fake client 的行为由各用例按需覆写。
func newEnv(t *testing.T) *testEnv {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(context.Background(),
		`TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)

	mc := &fakeMultica{issue: multica.Issue{ID: "m-issue-1", Identifier: "INFERA-31", Status: "backlog"}}
	gh := &fakeGithub{}
	svc, err := New(pool, mc, gh, Options{
		MulticaProjectID:     "proj-1",
		TechLeadAgentID:      "lead-1",
		MulticaServerURL:     "http://localhost:8088",
		MulticaWorkspaceSlug: "infera",
	})
	require.NoError(t, err)
	return &testEnv{svc: svc, mc: mc, gh: gh}
}

// fakeMultica 记录全部调用，可按需注入错误。
type fakeMultica struct {
	mu sync.Mutex

	issue multica.Issue // CreateIssue 的返回

	createErr error
	assignErr error
	postErr   error

	created  []multica.CreateIssueInput
	assigned []struct {
		issueID string
		agentID string
	}
	posted []struct {
		issueID string
		content string
	}
	setStatused []string
}

func (f *fakeMultica) CreateIssue(ctx context.Context, in multica.CreateIssueInput) (multica.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	if f.createErr != nil {
		return multica.Issue{}, f.createErr
	}
	return f.issue, nil
}

func (f *fakeMultica) AssignAgent(ctx context.Context, issueID, agentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assigned = append(f.assigned, struct {
		issueID string
		agentID string
	}{issueID, agentID})
	return f.assignErr
}

func (f *fakeMultica) PostComment(ctx context.Context, issueID, content string) (multica.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, struct {
		issueID string
		content string
	}{issueID, content})
	if f.postErr != nil {
		return multica.Comment{}, f.postErr
	}
	return multica.Comment{ID: "cmt-1", Content: content}, nil
}

func (f *fakeMultica) SetStatus(ctx context.Context, issueID, status string, suppressRun bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setStatused = append(f.setStatused, status)
	return nil
}

// fakeGithub 记录 merge 调用，可注入结果 / 错误。
type fakeGithub struct {
	mu sync.Mutex

	mergeErr error
	result   github.MergeResult

	merged []struct {
		owner  string
		repo   string
		number int
	}

	reviewComments []github.ReviewComment // ListReviewComments 的返回
	diffStats      github.DiffStats       // GetDiffStats 的返回
	listRevErr     error
	diffErr        error

	listedReviews []struct {
		owner  string
		repo   string
		number int
	}
}

func (f *fakeGithub) MergePullRequest(ctx context.Context, owner, repo string, number int, in github.MergeInput) (github.MergeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merged = append(f.merged, struct {
		owner  string
		repo   string
		number int
	}{owner, repo, number})
	if f.mergeErr != nil {
		return github.MergeResult{}, f.mergeErr
	}
	if f.result.Merged {
		return f.result, nil
	}
	return github.MergeResult{Merged: true, SHA: "abc123", Message: "Pull Request successfully merged"}, nil
}

func (f *fakeGithub) ListReviewComments(ctx context.Context, owner, repo string, number int) ([]github.ReviewComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listedReviews = append(f.listedReviews, struct {
		owner  string
		repo   string
		number int
	}{owner, repo, number})
	if f.listRevErr != nil {
		return nil, f.listRevErr
	}
	return f.reviewComments, nil
}

func (f *fakeGithub) GetDiffStats(ctx context.Context, owner, repo string, number int) (github.DiffStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.diffErr != nil {
		return github.DiffStats{}, f.diffErr
	}
	return f.diffStats, nil
}

// seedCard 直接落一张闸门卡（绕过 gatepoll），构造待处理状态。
func seedCard(t *testing.T, env *testEnv, reqID string, kind flow.GateKind) string {
	t.Helper()
	var id string
	err := envPool(env.svc).QueryRow(context.Background(),
		`INSERT INTO gate_cards (id, requirement_id, kind, status, payload, comment_id)
		 VALUES (gen_random_uuid(), $1, $2, 'pending', '正文', 'cmt-x') RETURNING id::text`,
		reqID, string(kind)).Scan(&id)
	require.NoError(t, err)
	return id
}

// envPool 取回 Service 持有的连接池（seed / 断言用）。
func envPool(env *Service) *pgxpool.Pool { return env.pool }

func TestCreateDispatchesToMultica(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	in := CreateInput{
		Title:              "支持需求流转",
		Description:        "业务描述",
		AcceptanceCriteria: "AC-1 全流程零跳出",
		Source:             "内部",
		Priority:           "high",
		Acceptors:          []string{"张三"},
	}
	got, err := env.svc.Create(ctx, in)
	require.NoError(t, err)

	// Multica 侧：固定 project 建 issue（backlog 起步，不触发 run）→ 指派 Tech Lead
	// （AssignAgent 置 todo 唤醒 agent）。
	require.Len(t, env.mc.created, 1)
	require.Equal(t, "proj-1", env.mc.created[0].ProjectID)
	require.Equal(t, "支持需求流转", env.mc.created[0].Title)
	require.Equal(t, "backlog", env.mc.created[0].Status)
	require.Empty(t, env.mc.created[0].Description, "需求描述不下发 Multica（FR-2）")
	require.Len(t, env.mc.assigned, 1)
	require.Equal(t, "m-issue-1", env.mc.assigned[0].issueID)
	require.Equal(t, "lead-1", env.mc.assigned[0].agentID)

	// infera 侧落库：映射 + 大节点=已派发 + 业务元数据只存本地。
	require.NotEmpty(t, got.ID)
	require.Equal(t, "m-issue-1", got.MulticaIssueID)
	require.Equal(t, "INFERA-31", got.MulticaIssueKey)
	require.Equal(t, flow.NodeDispatched, got.Node)
	require.Equal(t, "业务描述", got.Description)
	require.Equal(t, "AC-1 全流程零跳出", got.AcceptanceCriteria)
	require.Equal(t, "内部", got.Source)
	require.Equal(t, "high", got.Priority)
	require.Equal(t, []string{"张三"}, got.Acceptors)
	require.Equal(t, "http://localhost:8088/infera/issues/m-issue-1", got.MulticaIssueURL)
}

func TestNewValidatesOptions(t *testing.T) {
	pool := envPool(newEnv(t).svc)
	base := Options{
		MulticaProjectID:     "proj-1",
		TechLeadAgentID:      "lead-1",
		MulticaServerURL:     "http://localhost:8088",
		MulticaWorkspaceSlug: "infera",
	}
	cases := []struct {
		name string
		mut  func(*Options)
	}{
		{"缺项目", func(o *Options) { o.MulticaProjectID = "" }},
		{"缺 Tech Lead", func(o *Options) { o.TechLeadAgentID = "" }},
		{"缺 ServerURL", func(o *Options) { o.MulticaServerURL = "" }},
		{"缺 workspace slug", func(o *Options) { o.MulticaWorkspaceSlug = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := base
			tc.mut(&opts)
			_, err := New(pool, &fakeMultica{}, &fakeGithub{}, opts)
			require.Error(t, err)
		})
	}
	_, err := New(nil, &fakeMultica{}, &fakeGithub{}, base)
	require.Error(t, err)
}

func TestCreateRejectsEmptyTitle(t *testing.T) {
	env := newEnv(t)
	_, err := env.svc.Create(context.Background(), CreateInput{Title: "  "})
	require.ErrorIs(t, err, ErrInvalid)
	require.Empty(t, env.mc.created, "校验失败不应触碰 Multica")
}

func TestCreateMulticaFailureLeavesNoRow(t *testing.T) {
	env := newEnv(t)
	env.mc.createErr = errors.New("boom")
	_, err := env.svc.Create(context.Background(), CreateInput{Title: "t"})
	require.ErrorContains(t, err, "multica 建卡失败")

	var n int
	require.NoError(t, envPool(env.svc).QueryRow(context.Background(),
		`SELECT count(*) FROM requirements`).Scan(&n))
	require.Zero(t, n, "Multica 失败不得落库")
}

func TestCreateAssignFailureParksIssue(t *testing.T) {
	env := newEnv(t)
	env.mc.assignErr = errors.New("assign boom")
	_, err := env.svc.Create(context.Background(), CreateInput{Title: "t"})
	require.ErrorContains(t, err, "指派 Tech Lead 失败")
	require.Equal(t, []string{"backlog"}, env.mc.setStatused, "指派失败应把 issue 停回 backlog")

	var n int
	require.NoError(t, envPool(env.svc).QueryRow(context.Background(),
		`SELECT count(*) FROM requirements`).Scan(&n))
	require.Zero(t, n)
}

func TestGetReturnsPendingCardsAndDeepLinks(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r1"})
	require.NoError(t, err)

	approvalID := seedCard(t, env, created.ID, flow.GateApproval)
	seedCard(t, env, created.ID, flow.GateDecision)
	// 已处理的合并卡不得出现在待处理列表
	var resolvedID string
	require.NoError(t, envPool(env.svc).QueryRow(ctx, `
		INSERT INTO gate_cards (id, requirement_id, kind, status, resolved_at)
		VALUES (gen_random_uuid(), $1, 'merge', 'resolved', now()) RETURNING id::text`,
		created.ID).Scan(&resolvedID))

	detail, err := env.svc.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, detail.ID)
	require.Equal(t, "http://localhost:8088/infera/issues/m-issue-1", detail.MulticaIssueURL)
	require.Len(t, detail.PendingCards, 2)
	kinds := []flow.GateKind{detail.PendingCards[0].Kind, detail.PendingCards[1].Kind}
	require.Contains(t, kinds, flow.GateApproval)
	require.Contains(t, kinds, flow.GateDecision)
	for _, c := range detail.PendingCards {
		require.Equal(t, flow.CardPending, c.Status)
		require.Equal(t, created.ID, c.RequirementID)
		require.Equal(t, "正文", c.Payload)
		require.Nil(t, c.ResolvedAt)
	}
	_ = approvalID
}

func TestGetUnknownRequirement(t *testing.T) {
	env := newEnv(t)
	_, err := env.svc.Get(context.Background(), "0b8c4b0e-0000-4000-8000-000000000001")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestListRequirements(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	first, err := env.svc.Create(ctx, CreateInput{Title: "先"})
	require.NoError(t, err)
	second, err := env.svc.Create(ctx, CreateInput{Title: "后",
		Description: "d", AcceptanceCriteria: "ac", Source: "s", Priority: "p", Acceptors: []string{"a"}})
	require.NoError(t, err)
	seedCard(t, env, second.ID, flow.GateApproval)

	rows, err := env.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	// 新 → 旧
	require.Equal(t, second.ID, rows[0].ID)
	require.Equal(t, first.ID, rows[1].ID)
	require.Equal(t, 1, rows[0].PendingCardCount)
	require.Equal(t, 0, rows[1].PendingCardCount)
	require.Equal(t, "http://localhost:8088/infera/issues/m-issue-1", rows[0].MulticaIssueURL)
	require.Equal(t, []string{"a"}, rows[0].Acceptors)
}

func TestApprovePostsCommentResolvesCardWritesAudit(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateApproval)

	require.NoError(t, env.svc.Approve(ctx, created.ID, cardID))

	// 代发评论 approved 到映射的 issue
	require.Len(t, env.mc.posted, 1)
	require.Equal(t, "m-issue-1", env.mc.posted[0].issueID)
	require.Equal(t, "approved", env.mc.posted[0].content)

	// 卡片已处理
	var status string
	var resolvedAt pqT
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status, resolved_at FROM gate_cards WHERE id = $1`, cardID).Scan(&status, &resolvedAt.v))
	require.Equal(t, "resolved", status)
	require.False(t, resolvedAt.v.IsZero(), "resolved_at 必须落库")

	// 审计：谁、何时、批了什么
	var actor, action, detail string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT actor, action, detail FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&actor, &action, &detail))
	require.Equal(t, ActorUser, actor)
	require.Equal(t, "approve", action)
	require.Equal(t, "approved", detail)
}

// pqT 是可空时间戳的扫描桥（testify 断言用零值判断）。
type pqT struct{ v time.Time }

func TestApproveRejectsResolvedCard(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateApproval)
	require.NoError(t, env.svc.Approve(ctx, created.ID, cardID))

	err = env.svc.Approve(ctx, created.ID, cardID)
	require.ErrorIs(t, err, ErrConflict)
	require.Len(t, env.mc.posted, 1, "重复动作不得再代发")
}

func TestApproveRejectsWrongCardKind(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateDecision)

	err = env.svc.Approve(ctx, created.ID, cardID)
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, env.mc.posted)
}

func TestApproveUnknownCard(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	err = env.svc.Approve(ctx, created.ID, "0b8c4b0e-0000-4000-8000-000000000002")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestApproveMulticaFailureKeepsCardPending(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateApproval)
	env.mc.postErr = errors.New("post boom")

	err = env.svc.Approve(ctx, created.ID, cardID)
	require.ErrorContains(t, err, "代发评论失败")

	var status string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "pending", status, "代发失败卡必须保持待处理")
	var n int
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT count(*) FROM audit_log`).Scan(&n))
	require.Zero(t, n, "失败动作不得留审计")
}

func TestRejectPostsFeedbackVerbatim(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateApproval)

	require.NoError(t, env.svc.Reject(ctx, created.ID, cardID, "范围太大，先做核心链路"))
	require.Len(t, env.mc.posted, 1)
	require.Equal(t, "范围太大，先做核心链路", env.mc.posted[0].content)

	var action, detail string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT action, detail FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&action, &detail))
	require.Equal(t, "reject", action)
	require.Equal(t, "范围太大，先做核心链路", detail)
}

func TestRejectRequiresFeedback(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateApproval)

	err = env.svc.Reject(ctx, created.ID, cardID, "  ")
	require.ErrorIs(t, err, ErrInvalid)
	require.Empty(t, env.mc.posted)
}

func TestDecideFixedChoicesPostFixedTexts(t *testing.T) {
	cases := []struct {
		choice string
		text   string
	}{
		{DecisionRetry, "重试"},
		{DecisionSkip, "跳过"},
		{DecisionAbort, "中止"},
	}
	for _, tc := range cases {
		t.Run(tc.choice, func(t *testing.T) {
			env := newEnv(t)
			ctx := context.Background()
			created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
			require.NoError(t, err)
			cardID := seedCard(t, env, created.ID, flow.GateDecision)

			require.NoError(t, env.svc.Decide(ctx, created.ID, cardID, tc.choice, ""))
			require.Len(t, env.mc.posted, 1)
			require.Equal(t, tc.text, env.mc.posted[0].content)
			require.Equal(t, "m-issue-1", env.mc.posted[0].issueID)

			var action, detail string
			require.NoError(t, envPool(env.svc).QueryRow(ctx,
				`SELECT action, detail FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&action, &detail))
			require.Equal(t, "decide", action)
			require.Equal(t, tc.choice, detail)
		})
	}
}

func TestDecideCustomPostsTextVerbatim(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateDecision)

	require.NoError(t, env.svc.Decide(ctx, created.ID, cardID, DecisionCustom, "改用 B 方案"))
	require.Len(t, env.mc.posted, 1)
	require.Equal(t, "改用 B 方案", env.mc.posted[0].content)

	var action, detail string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT action, detail FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&action, &detail))
	require.Equal(t, "decide", action)
	require.Equal(t, "custom: 改用 B 方案", detail)
}

func TestDecideRejectsInvalidInput(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateDecision)

	err = env.svc.Decide(ctx, created.ID, cardID, "later", "")
	require.ErrorIs(t, err, ErrInvalid)
	err = env.svc.Decide(ctx, created.ID, cardID, DecisionCustom, "")
	require.ErrorIs(t, err, ErrInvalid)
	require.Empty(t, env.mc.posted, "非法输入不得代发")
}

func TestDecideRejectsWrongCardKind(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateApproval)

	err = env.svc.Decide(ctx, created.ID, cardID, DecisionRetry, "")
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, env.mc.posted)
}

func TestReworkPostsFeedbackAndResolves(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateMerge)

	require.NoError(t, env.svc.Rework(ctx, created.ID, cardID, "评审意见未处理，请返工"))
	require.Len(t, env.mc.posted, 1)
	require.Equal(t, "评审意见未处理，请返工", env.mc.posted[0].content)

	var action, detail string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT action, detail FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&action, &detail))
	require.Equal(t, "rework", action)
	require.Equal(t, "评审意见未处理，请返工", detail)

	var status string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "resolved", status)
}

func TestReworkRequiresFeedback(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateMerge)

	err = env.svc.Rework(ctx, created.ID, cardID, " ")
	require.ErrorIs(t, err, ErrInvalid)
	require.Empty(t, env.mc.posted)
}

// setPRURL 直接写需求的 PR 引用（模拟 gatepoll 从评论提取后落库）。
func setPRURL(t *testing.T, env *testEnv, reqID, prURL string) {
	t.Helper()
	_, err := envPool(env.svc).Exec(context.Background(),
		`UPDATE requirements SET pr_url = $1 WHERE id = $2`, prURL, reqID)
	require.NoError(t, err)
}

func TestMergePullsPRResolvesCardWritesAudit(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setPRURL(t, env, created.ID, "https://github.com/huiyangz/infera/pull/7")
	cardID := seedCard(t, env, created.ID, flow.GateMerge)

	res, err := env.svc.Merge(ctx, created.ID, cardID)
	require.NoError(t, err)
	require.True(t, res.Merged)

	require.Len(t, env.gh.merged, 1)
	require.Equal(t, "huiyangz", env.gh.merged[0].owner)
	require.Equal(t, "infera", env.gh.merged[0].repo)
	require.Equal(t, 7, env.gh.merged[0].number)

	var status string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "resolved", status)

	var actor, action string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT actor, action FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&actor, &action))
	require.Equal(t, ActorUser, actor)
	require.Equal(t, "merge", action)
}

func TestMergeWithoutPRURL(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	cardID := seedCard(t, env, created.ID, flow.GateMerge)

	_, err = env.svc.Merge(ctx, created.ID, cardID)
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, env.gh.merged)
}

// testReviewAt 是评审评论的固定时间戳（断言映射保真用）。
var testReviewAt = time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC)

// TestGetPRReviewReturnsCommentsAndDiff：合并卡渲染面（FR-4/FR-7）——经
// gh API 拉取行级评审评论与 diff 概要，只读，不落卡不落审计。
func TestGetPRReviewReturnsCommentsAndDiff(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setPRURL(t, env, created.ID, "https://github.com/huiyangz/infera/pull/7")
	env.gh.reviewComments = []github.ReviewComment{
		{ID: 11, Path: "server/main.go", Line: 42, Side: "RIGHT", Body: "这里缺超时控制",
			User: github.User{Login: "reviewer-bot"}, CreatedAt: testReviewAt},
		{ID: 12, Path: "apps/web/api.ts", Line: 0, OriginalLine: 8, Side: "LEFT", Body: "删除行评论",
			User: github.User{Login: "reviewer-bot"}, CreatedAt: testReviewAt},
	}
	env.gh.diffStats = github.DiffStats{Files: 4, Additions: 120, Deletions: 8, Changes: 128}

	rv, err := env.svc.GetPRReview(ctx, created.ID)
	require.NoError(t, err)

	require.Equal(t, "https://github.com/huiyangz/infera/pull/7", rv.PRURL)
	require.Len(t, rv.Comments, 2)
	c := rv.Comments[0]
	require.Equal(t, int64(11), c.ID)
	require.Equal(t, "server/main.go", c.Path)
	require.Equal(t, 42, c.Line)
	require.Equal(t, "RIGHT", c.Side)
	require.Equal(t, "这里缺超时控制", c.Body)
	require.Equal(t, "reviewer-bot", c.Author)
	require.Equal(t, testReviewAt, c.CreatedAt)
	// 删除行评论：line=0、行号在 original_line（GitHub original_* 语义）。
	require.Equal(t, 0, rv.Comments[1].Line)
	require.Equal(t, 8, rv.Comments[1].OriginalLine)
	require.Equal(t, PRDiffStats{Files: 4, Additions: 120, Deletions: 8, Changes: 128}, rv.Diff)

	// 消费的是需求关联的 PR（owner/repo/number 由 URL 解析）。
	require.Len(t, env.gh.listedReviews, 1)
	require.Equal(t, "huiyangz", env.gh.listedReviews[0].owner)
	require.Equal(t, "infera", env.gh.listedReviews[0].repo)
	require.Equal(t, 7, env.gh.listedReviews[0].number)

	// 只读：不落审计。
	var n int
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&n))
	require.Zero(t, n)
}

// TestGetPRReviewWithoutPRURL：需求尚未关联 PR（轮询器还没从评论提取到）——
// 状态冲突，与 Merge 的无 PR 语义一致。
func TestGetPRReviewWithoutPRURL(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)

	_, err = env.svc.GetPRReview(ctx, created.ID)
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, env.gh.listedReviews)
}

// TestGetPRReviewUnknownRequirement：需求不存在 → NotFound。
func TestGetPRReviewUnknownRequirement(t *testing.T) {
	env := newEnv(t)
	_, err := env.svc.GetPRReview(context.Background(), "0b8c4b0e-0000-4000-8000-00000000dead")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMergeBlockedKeepsCardPending(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setPRURL(t, env, created.ID, "https://github.com/huiyangz/infera/pull/7")
	cardID := seedCard(t, env, created.ID, flow.GateMerge)
	env.gh.mergeErr = &github.APIError{Method: "PUT", Path: "/pulls/7/merge",
		StatusCode: http.StatusMethodNotAllowed, Message: "Pull Request is not mergeable"}

	_, err = env.svc.Merge(ctx, created.ID, cardID)
	require.ErrorIs(t, err, ErrMergeBlocked, "阻塞类失败必须以 ErrMergeBlocked 归因")

	var status string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "pending", status)
	var n int
	require.NoError(t, envPool(env.svc).QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n))
	require.Zero(t, n)
}

func TestMergeOnWrongCardKind(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setPRURL(t, env, created.ID, "https://github.com/huiyangz/infera/pull/7")
	cardID := seedCard(t, env, created.ID, flow.GateApproval)

	_, err = env.svc.Merge(ctx, created.ID, cardID)
	require.ErrorIs(t, err, ErrConflict)
	require.Empty(t, env.gh.merged)
}

// ---------------------------------------------------------------------------
// needs_decision 出口接线（INFERA-40 T08）：决策动作 → 卡 resolved + 节点
// 从 needs_decision 经 flow 状态机返回。
// ---------------------------------------------------------------------------

// setNode 直接写需求节点（模拟 gatepoll 决策事件推进后的状态）。
func setNode(t *testing.T, env *testEnv, reqID string, node flow.Node) {
	t.Helper()
	_, err := envPool(env.svc).Exec(context.Background(),
		`UPDATE requirements SET node = $1 WHERE id = $2`, string(node), reqID)
	require.NoError(t, err)
}

// nodeOf 读需求当前节点（断言用）。
func nodeOf(t *testing.T, env *testEnv, reqID string) string {
	t.Helper()
	var node string
	require.NoError(t, envPool(env.svc).QueryRow(context.Background(),
		`SELECT node FROM requirements WHERE id = $1`, reqID).Scan(&node))
	return node
}

// TestDecideReturnsFromNeedsDecisionToActiveNode：重试/跳过/自定义都是
// "继续执行"的决策——卡 resolved + 节点回活跃节点（执行中）。
// 返回活跃节点的选择依据：决策打断的是执行，处理后恢复即执行；节点与
// Multica 执行态的偏差由后续轮询按父 issue 状态自行校正（单一状态源，
// 一轮内收敛），决策动作不越权猜测执行态。
func TestDecideReturnsFromNeedsDecisionToActiveNode(t *testing.T) {
	for _, choice := range []string{DecisionRetry, DecisionSkip, DecisionCustom} {
		t.Run(choice, func(t *testing.T) {
			env := newEnv(t)
			ctx := context.Background()
			created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
			require.NoError(t, err)
			setNode(t, env, created.ID, flow.NodeNeedsDecision)
			cardID := seedCard(t, env, created.ID, flow.GateDecision)

			text := ""
			if choice == DecisionCustom {
				text = "改用 B 方案"
			}
			require.NoError(t, env.svc.Decide(ctx, created.ID, cardID, choice, text))

			require.Equal(t, string(flow.NodeInProgress), nodeOf(t, env, created.ID), "决策后返回活跃节点")
			var status string
			require.NoError(t, envPool(env.svc).QueryRow(ctx,
				`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
			require.Equal(t, "resolved", status)
		})
	}
}

// TestDecideAbortGoesDelivered：中止 → 直达已交付。
// 选择依据：flow 大节点集没有 cancelled——中止即不再执行，需求必须退出
// 在途清单，唯一出路是终态 delivered；flow.CanTransition 冻结注释明确
// needs_decision"可回活跃或直达 delivered（决策后的去向由执行态决定）"，
// 中止正是取直达出口的那类决策。
func TestDecideAbortGoesDelivered(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setNode(t, env, created.ID, flow.NodeNeedsDecision)
	cardID := seedCard(t, env, created.ID, flow.GateDecision)

	require.NoError(t, env.svc.Decide(ctx, created.ID, cardID, DecisionAbort, ""))

	require.Equal(t, string(flow.NodeDelivered), nodeOf(t, env, created.ID), "中止直达已交付")
	var status string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "resolved", status)
	var n int
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE requirement_id = $1`, created.ID).Scan(&n))
	require.Equal(t, 1, n, "决策动作照常写审计")
}

// TestDecideOutsideNeedsDecisionKeepsNode：节点不在 needs_decision（如 T08
// 接线前遗留的存量决策卡）——决策照常代发 + 收卡 + 审计，但不触发节点
// 跃迁：needs_decision 的出口接线只从 needs_decision 起跳（经
// flow.CanTransition），不从任意节点横跳。
func TestDecideOutsideNeedsDecisionKeepsNode(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	// Create 落库即 dispatched（未经 gatepoll 推进到 needs_decision）。
	cardID := seedCard(t, env, created.ID, flow.GateDecision)

	require.NoError(t, env.svc.Decide(ctx, created.ID, cardID, DecisionRetry, ""))

	require.Equal(t, string(flow.NodeDispatched), nodeOf(t, env, created.ID), "非 needs_decision 不跃迁")
	require.Len(t, env.mc.posted, 1, "决策照常代发")
}

// TestDecideAtomicRollbackOnNodeFailure：原子性——代发成功后节点写失败，
// 卡收口与审计一并回滚（resolved + 审计 + 节点同事务，全有或全无）。
func TestDecideAtomicRollbackOnNodeFailure(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setNode(t, env, created.ID, flow.NodeNeedsDecision) // 触发器建好后再不能 UPDATE
	cardID := seedCard(t, env, created.ID, flow.GateDecision)

	pool := envPool(env.svc)
	_, err = pool.Exec(ctx,
		`CREATE FUNCTION t08_boom() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'boom'; END $$ LANGUAGE plpgsql`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`CREATE TRIGGER t08_boom BEFORE UPDATE ON requirements FOR EACH ROW EXECUTE FUNCTION t08_boom()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER t08_boom ON requirements`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION t08_boom()`)
	})

	err = env.svc.Decide(ctx, created.ID, cardID, DecisionRetry, "")
	require.Error(t, err, "节点写失败必须上抛")

	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "pending", status, "卡收口随事务回滚")
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n))
	require.Zero(t, n, "审计随事务回滚")
	require.Equal(t, string(flow.NodeNeedsDecision), nodeOf(t, env, created.ID))
}

// TestDecidePostFailureKeepsNeedsDecision：代发失败 → 卡保持 pending、节点
// 保持 needs_decision（失败动作不算动作，人可重试）。
func TestDecidePostFailureKeepsNeedsDecision(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	setNode(t, env, created.ID, flow.NodeNeedsDecision)
	cardID := seedCard(t, env, created.ID, flow.GateDecision)
	env.mc.postErr = errors.New("post boom")

	err = env.svc.Decide(ctx, created.ID, cardID, DecisionRetry, "")
	require.ErrorContains(t, err, "代发评论失败")

	var status string
	require.NoError(t, envPool(env.svc).QueryRow(ctx,
		`SELECT status FROM gate_cards WHERE id = $1`, cardID).Scan(&status))
	require.Equal(t, "pending", status)
	require.Equal(t, string(flow.NodeNeedsDecision), nodeOf(t, env, created.ID), "节点保持停驻")
}

// seedProject 落一行项目（project_settings 的 FK 目标）。
func seedProject(t *testing.T, env *testEnv) string {	t.Helper()
	var id string
	err := envPool(env.svc).QueryRow(context.Background(),
		`INSERT INTO projects (id, name) VALUES (gen_random_uuid(), 'demo') RETURNING id::text`).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestListAuditChronological(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	created, err := env.svc.Create(ctx, CreateInput{Title: "r"})
	require.NoError(t, err)
	approval := seedCard(t, env, created.ID, flow.GateApproval)
	decision := seedCard(t, env, created.ID, flow.GateDecision)
	require.NoError(t, env.svc.Approve(ctx, created.ID, approval))
	require.NoError(t, env.svc.Decide(ctx, created.ID, decision, DecisionSkip, ""))

	entries, err := env.svc.ListAudit(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "approve", entries[0].Action)
	require.Equal(t, ActorUser, entries[0].Actor)
	require.Equal(t, "approved", entries[0].Detail)
	require.False(t, entries[0].At.IsZero())
	require.Equal(t, "decide", entries[1].Action)
	require.Equal(t, DecisionSkip, entries[1].Detail)
}

func TestMergePolicyDefaultsToManual(t *testing.T) {
	env := newEnv(t)
	proj := seedProject(t, env)

	p, err := env.svc.GetMergePolicy(context.Background(), proj)
	require.NoError(t, err)
	require.Equal(t, flow.MergeManual, p.Mode)
	require.Zero(t, p.DiffLineThreshold)
}

func TestMergePolicyRoundtrip(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	proj := seedProject(t, env)

	// threshold 档带阈值
	got, err := env.svc.SetMergePolicy(ctx, proj, flow.MergePolicy{Mode: flow.MergeThreshold, DiffLineThreshold: 200})
	require.NoError(t, err)
	require.Equal(t, flow.MergeThreshold, got.Mode)
	require.Equal(t, 200, got.DiffLineThreshold)
	stored, err := env.svc.GetMergePolicy(ctx, proj)
	require.NoError(t, err)
	require.Equal(t, flow.MergePolicy{Mode: flow.MergeThreshold, DiffLineThreshold: 200}, stored)

	// 覆盖为 auto_pass（不带阈值）
	got, err = env.svc.SetMergePolicy(ctx, proj, flow.MergePolicy{Mode: flow.MergeAutoPass})
	require.NoError(t, err)
	require.Equal(t, flow.MergeAutoPass, got.Mode)
	require.Zero(t, got.DiffLineThreshold)
	stored, err = env.svc.GetMergePolicy(ctx, proj)
	require.NoError(t, err)
	require.Equal(t, flow.MergePolicy{Mode: flow.MergeAutoPass}, stored)
}

func TestSetMergePolicyValidates(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	proj := seedProject(t, env)

	cases := []flow.MergePolicy{
		{Mode: "yolo"},
		{Mode: flow.MergeThreshold, DiffLineThreshold: 0},
		{Mode: flow.MergeThreshold, DiffLineThreshold: -5},
		{Mode: flow.MergeManual, DiffLineThreshold: 100},
		{Mode: flow.MergeAutoPass, DiffLineThreshold: 100},
	}
	for _, p := range cases {
		_, err := env.svc.SetMergePolicy(ctx, proj, p)
		require.ErrorIs(t, err, ErrInvalid, "mode=%s threshold=%d 应拒绝", p.Mode, p.DiffLineThreshold)
	}
}

func TestSetMergePolicyUnknownProject(t *testing.T) {
	env := newEnv(t)
	_, err := env.svc.SetMergePolicy(context.Background(), "0b8c4b0e-0000-4000-8000-000000000003",
		flow.MergePolicy{Mode: flow.MergeManual})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestGetMergePolicyUnknownProject(t *testing.T) {
	env := newEnv(t)
	_, err := env.svc.GetMergePolicy(context.Background(), "0b8c4b0e-0000-4000-8000-000000000004")
	require.ErrorIs(t, err, ErrNotFound)
}
