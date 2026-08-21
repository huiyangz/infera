package gatepoll

// PgStore 落库测试：沿用现有 db 测试引导模式（TEST_DATABASE_URL，未设置跳过；
// 测试前 TRUNCATE 清库——全量 go test 必须带 -p 1，见 INFERA-12 交付说明）。

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/flow"
)

func testPgStore(t *testing.T) *PgStore {
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
	return NewPgStore(pool)
}

// insertRequirement 直插一行需求（绕过 Store：需求的创建属 reqservice 的职责，
// gatepoll 只读消费 + 推进）。
func insertRequirement(t *testing.T, s *PgStore, req flow.Requirement) {
	t.Helper()
	acceptors := "{}"
	if len(req.Acceptors) > 0 {
		acceptors = "{"
		for i, a := range req.Acceptors {
			if i > 0 {
				acceptors += ","
			}
			acceptors += a
		}
		acceptors += "}"
	}
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO requirements (id, title, multica_issue_id, multica_issue_key, node, pr_url)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		req.ID, req.Title, req.MulticaIssueID, req.MulticaIssueKey, string(req.Node), req.PRURL)
	require.NoError(t, err)
}

func TestPgStoreListInFlight(t *testing.T) {
	s := testPgStore(t)
	ctx := context.Background()

	insertRequirement(t, s, newTestReq("11111111-1111-1111-1111-111111111111", "issue-1")) // dispatched → 在途
	insertRequirement(t, s, newTestReq("22222222-2222-2222-2222-222222222222", "issue-2")) // 同上
	delivered := newTestReq("33333333-3333-3333-3333-333333333333", "issue-3")             //
	delivered.Node = flow.NodeDelivered                                                    // 已交付 → 不在途
	insertRequirement(t, s, delivered)
	noIssue := newTestReq("44444444-4444-4444-4444-444444444444", "") // 未建卡 → 不在途
	insertRequirement(t, s, noIssue)
	needsDecision := newTestReq("55555555-5555-5555-5555-555555555555", "issue-5")
	needsDecision.Node = flow.NodeNeedsDecision // 决策中 → 仍在途
	insertRequirement(t, s, needsDecision)

	got, err := s.ListInFlight(ctx)
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.Req.ID] = true
		require.Equal(t, r.Req.MulticaIssueID, r.Cursor.MulticaIssueID, "游标携带 issue 映射")
		require.True(t, r.Cursor.LastCommentAt.IsZero(), "尚未轮询过：游标时间为零值（首轮全量）")
		require.Empty(t, r.Cursor.LastStatus)
	}
	require.True(t, ids["11111111-1111-1111-1111-111111111111"])
	require.True(t, ids["22222222-2222-2222-2222-222222222222"])
	require.True(t, ids["55555555-5555-5555-5555-555555555555"])
	require.False(t, ids["33333333-3333-3333-3333-333333333333"], "已交付不在途")
	require.False(t, ids["44444444-4444-4444-4444-444444444444"], "未建卡不在途")
}

func TestPgStoreSavePollStateRoundtrip(t *testing.T) {
	s := testPgStore(t)
	ctx := context.Background()
	req := newTestReq("11111111-1111-1111-1111-111111111111", "issue-1")
	insertRequirement(t, s, req)

	at := time.Date(2026, 8, 21, 12, 30, 5, 0, time.UTC)
	cur := flow.PollCursor{
		RequirementID:  req.ID,
		MulticaIssueID: req.MulticaIssueID,
		LastCommentAt:  at,
		LastStatus:     "in_review",
		SeenVerdict:    true,
	}
	require.NoError(t, s.SavePollState(ctx, req.ID, flow.NodeInReview, "https://github.com/acme/app/pull/42", cur))

	got, err := s.ListInFlight(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, flow.NodeInReview, got[0].Req.Node)
	require.Equal(t, "https://github.com/acme/app/pull/42", got[0].Req.PRURL)
	require.Equal(t, "in_review", got[0].Cursor.LastStatus)
	require.True(t, got[0].Cursor.SeenVerdict)
	require.True(t, got[0].Cursor.LastCommentAt.Equal(at), "游标时间戳往返不丢（重启续读的根基）")
}

func TestPgStoreInsertCardIfNewAndPending(t *testing.T) {
	s := testPgStore(t)
	ctx := context.Background()
	req := newTestReq("11111111-1111-1111-1111-111111111111", "issue-1")
	insertRequirement(t, s, req)

	created, err := s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateApproval, Payload: "待批准：x", CommentID: "c1"})
	require.NoError(t, err)
	require.True(t, created)
	created, err = s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateApproval, Payload: "待批准：x", CommentID: "c1"})
	require.NoError(t, err)
	require.False(t, created, "同评论 id 二次投递不重复建卡")

	// 状态类兜底卡（无评论溯源）不去重：可多次出现。
	created, err = s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateUpdate, Payload: "有新动态"})
	require.NoError(t, err)
	require.True(t, created)
	created, err = s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateUpdate, Payload: "有新动态"})
	require.NoError(t, err)
	require.True(t, created)

	pending, err := s.ListPendingMergeCards(ctx, req.ID)
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateMerge, Payload: "verdict: PASS", CommentID: "c2"})
	require.NoError(t, err)
	_, err = s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateDecision, Payload: "需要决策：x", CommentID: "c3"})
	require.NoError(t, err)
	pending, err = s.ListPendingMergeCards(ctx, req.ID)
	require.NoError(t, err)
	require.Len(t, pending, 1, "只列 pending 的合并卡")
	require.Equal(t, flow.GateMerge, pending[0].Kind)
	require.Equal(t, "c2", pending[0].CommentID)
	require.NotEmpty(t, pending[0].ID, "落库生成卡片 id")
}

func TestPgStoreCompleteAutoMergeAtomic(t *testing.T) {
	s := testPgStore(t)
	ctx := context.Background()
	req := newTestReq("11111111-1111-1111-1111-111111111111", "issue-1")
	insertRequirement(t, s, req)

	_, err := s.InsertCardIfNew(ctx, flow.GateCard{RequirementID: req.ID, Kind: flow.GateMerge, Payload: "verdict: PASS https://github.com/acme/app/pull/42", CommentID: "c1"})
	require.NoError(t, err)
	pending, err := s.ListPendingMergeCards(ctx, req.ID)
	require.NoError(t, err)
	require.Len(t, pending, 1)

	cur := flow.PollCursor{RequirementID: req.ID, MulticaIssueID: req.MulticaIssueID, LastStatus: "in_review", SeenVerdict: true}
	audit := flow.AuditEntry{RequirementID: req.ID, Actor: "system", Action: "merge", Detail: "auto merge https://github.com/acme/app/pull/42 (policy=auto_pass)"}
	require.NoError(t, s.CompleteAutoMerge(ctx, pending[0].ID, flow.NodeDelivered, "https://github.com/acme/app/pull/42", cur, audit))

	// 卡收口。
	pending, err = s.ListPendingMergeCards(ctx, req.ID)
	require.NoError(t, err)
	require.Empty(t, pending)
	var status string
	var resolvedAt *time.Time
	require.NoError(t, s.pool.QueryRow(ctx,
		`SELECT status, resolved_at FROM gate_cards WHERE id=$1`, pending0ID(t, s, req.ID)).Scan(&status, &resolvedAt))
	require.Equal(t, "resolved", status)
	require.NotNil(t, resolvedAt)

	// 审计落库。
	var actor, action string
	require.NoError(t, s.pool.QueryRow(ctx,
		`SELECT actor, action FROM audit_log WHERE requirement_id=$1`, req.ID).Scan(&actor, &action))
	require.Equal(t, "system", actor)
	require.Equal(t, "merge", action)

	// 节点 + 游标一次性收口。
	got, err := s.ListInFlight(ctx)
	require.NoError(t, err)
	require.Empty(t, got, "已交付后不在途")
}

// pending0ID 取该需求任意一张卡（CompleteAutoMerge 后 ListPendingMergeCards 为空，
// 用 SQL 直查那张 merge 卡的 id）。
func pending0ID(t *testing.T, s *PgStore, reqID string) string {
	t.Helper()
	var id string
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT id FROM gate_cards WHERE requirement_id=$1 AND kind='merge' LIMIT 1`, reqID).Scan(&id))
	return id
}

// TestCursorPersistenceAcrossRestart：AC——游标持久化，进程重启后从上次位置续读，
// 不重放旧评论、不漏新评论。真实 pg store + fake client，两个 Poller 实例模拟重启
// （AfterID 不持久化：服务端秒级截断会让锚点评论重发——按评论 id 幂等去重兜住，
// 与 multica.ListCommentsSince 的调用方契约一致）。
func TestCursorPersistenceAcrossRestart(t *testing.T) {
	s := testPgStore(t)
	mc := newFakeMultica()
	mc.redeliverAnchor = true // 重启后锚点评论会重发（真实截断语义）
	req := newTestReq("11111111-1111-1111-1111-111111111111", "issue-1")
	insertRequirement(t, s, req)
	mc.addIssue("issue-1", "in_progress")
	mc.addComment("issue-1", "c1", "待批准：第一轮计划", time.Date(2026, 8, 21, 12, 0, 1, 0, time.UTC))
	mc.addComment("issue-1", "c2", "进度正常", time.Date(2026, 8, 21, 12, 0, 2, 0, time.UTC))

	ctx := context.Background()
	p1, err := newTestPoller(s, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	require.NoError(t, p1.PollOnce(ctx))
	require.Equal(t, 2, cardCountDB(t, s, req.ID))

	// "重启"：新进程新实例，同一持久层。新增一条评论 + 状态跃迁。
	mc.addComment("issue-1", "c3", "需要决策：A 还是 B", time.Date(2026, 8, 21, 12, 0, 3, 0, time.UTC))
	mc.setStatus("issue-1", "in_review")
	p2, err := newTestPoller(s, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	require.NoError(t, p2.PollOnce(ctx))

	// 不重放：c1/c2 不再建卡；不漏：c3 建了决策卡；in_review 未见 verdict → 兜底二。
	rows, err := s.pool.Query(ctx, `SELECT kind, comment_id FROM gate_cards WHERE requirement_id=$1 ORDER BY created_at, id`, req.ID)
	require.NoError(t, err)
	defer rows.Close()
	seenComments := map[string]bool{}
	updateCount := 0
	for rows.Next() {
		var kind, commentID string
		require.NoError(t, rows.Scan(&kind, &commentID))
		if commentID != "" {
			require.False(t, seenComments[commentID], "评论 %s 被重放建卡", commentID)
			seenComments[commentID] = true
		}
		if kind == "update" && commentID == "" {
			updateCount++
		}
	}
	require.Len(t, seenComments, 3, "c1/c2/c3 各一张卡，无重放")
	require.Equal(t, 1, updateCount, "兜底二恰触发一次")
	require.True(t, seenComments["c3"], "重启后新评论不漏")

	// 节点已推进、游标已落。
	got, err := s.ListInFlight(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, flow.NodeInReview, got[0].Req.Node)
	require.Equal(t, "in_review", got[0].Cursor.LastStatus)
}

func cardCountDB(t *testing.T, s *PgStore, reqID string) int {
	t.Helper()
	var n int64
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM gate_cards WHERE requirement_id=$1`, reqID).Scan(&n))
	return int(n)
}
