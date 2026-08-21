package gatepoll

// 闸门轮询器单测（全 fake，无 DB、无网络）。验收映射（INFERA-15 / T04）：
//   - 状态跃迁推进        → TestPollOnceAdvancesNode / TestPollOnceStatusRegressionHolds
//   - 三类卡生成          → TestPollOnceGeneratesGateCards / TestPollOnceVerdictCardCarriesBody
//   - 两条兜底路径        → TestPollOnceFallbackRuleOne / TestPollOnceFallbackRuleTwo
//   - 评论去重            → TestPollOnceDedupsRedeliveredComments
//   - 三档合并策略        → TestMergePolicyManual / TestMergePolicyAutoPass /
//                            TestMergePolicyAutoPassFailVerdictNoMerge /
//                            TestMergePolicyThresholdUnder / TestMergePolicyThresholdOver
//   - 自动合并审计写入    → TestMergePolicyAutoPass（audit 断言）
//   - 重试/转人工         → TestAutoMergeBlockedRetriesNextCycle / TestAutoMergeHardErrorGoesManual /
//                            TestAutoMergeClosedPRConverges
//   - PR 引用             → TestPollOnceStoresFirstPRURL / TestVerdictWithoutPRURLNoMerge
//   - 错误隔离            → TestPollOnceIsolatesRequirementErrors
//   - 生命周期            → TestPollerLifecycle / TestNewIntervalValidation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
)

func TestPollOnceAdvancesNode(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "todo")
	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	ctx := context.Background()

	// 首轮：todo → dispatched 同节点，不推进；游标记录状态。
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeDispatched, st.reqs["r1"].Node)
	require.Equal(t, "todo", st.cursors["r1"].LastStatus)
	st.mu.Unlock()

	// in_progress → 执行中。
	mc.setStatus("issue-1", "in_progress")
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeInProgress, st.reqs["r1"].Node)
	st.mu.Unlock()

	// in_review → 待验收。
	mc.setStatus("issue-1", "in_review")
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeInReview, st.reqs["r1"].Node)
	st.mu.Unlock()

	// done → 已交付（终态）。
	mc.setStatus("issue-1", "done")
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeDelivered, st.reqs["r1"].Node)
	st.mu.Unlock()

	// 已交付后不再出现在途清单：即使状态再变，节点不动。
	mc.setStatus("issue-1", "in_progress")
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeDelivered, st.reqs["r1"].Node)
	st.mu.Unlock()
}

func TestPollOnceStatusBlockedAndRegressionHold(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	ctx := context.Background()

	// blocked 不映射：节点保持。
	mc.addIssue("issue-1", "blocked")
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeDispatched, st.reqs["r1"].Node, "blocked 状态不推进")
	st.mu.Unlock()

	// 推到待验收后 Multica 状态回退 in_progress：infera 是单一状态源，节点不回退。
	mc.setStatus("issue-1", "in_review")
	require.NoError(t, p.PollOnce(ctx))
	mc.setStatus("issue-1", "in_progress")
	require.NoError(t, p.PollOnce(ctx))
	st.mu.Lock()
	require.Equal(t, flow.NodeInReview, st.reqs["r1"].Node, "Multica 状态回退不导致节点回退")
	st.mu.Unlock()
}

func TestPollOnceGeneratesGateCards(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_progress")
	mc.addComment("issue-1", "c1", "待批准：分批执行计划\n\n正文", testNow.Add(1*time.Minute))
	mc.addComment("issue-1", "c2", "需要决策：上游 API 不稳定，重试还是跳过？", testNow.Add(2*time.Minute))
	mc.addComment("issue-1", "c3", "verdict: PASS\n\n行级评审摘要", testNow.Add(3*time.Minute))

	p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	cards := st.cardsOf("r1")
	require.Len(t, cards, 3)
	byComment := map[string]flow.GateCard{}
	for _, c := range cards {
		byComment[c.CommentID] = c
	}
	require.Equal(t, flow.GateApproval, byComment["c1"].Kind)
	require.Equal(t, "待批准：分批执行计划\n\n正文", byComment["c1"].Payload)
	require.Equal(t, flow.GateDecision, byComment["c2"].Kind)
	require.Equal(t, flow.GateMerge, byComment["c3"].Kind)
	require.Equal(t, flow.CardPending, byComment["c3"].Status, "manual 档合并卡留待人处理")
	for _, c := range cards {
		require.Equal(t, flow.CardPending, c.Status)
	}
}

func TestPollOnceFallbackRuleOne(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_progress")
	// 无任何已知前缀的普通评论 → 中性"有新动态"卡。
	mc.addComment("issue-1", "c1", "进度过半，接口已通", testNow.Add(1*time.Minute))
	// 相近干扰变体（半角冒号）同样落兜底，不伪装成闸门。
	mc.addComment("issue-1", "c2", "待批准: 假闸门", testNow.Add(2*time.Minute))

	p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	cards := st.cardsOf("r1")
	require.Len(t, cards, 2)
	for _, c := range cards {
		require.Equal(t, flow.GateUpdate, c.Kind)
		require.NotEmpty(t, c.CommentID)
	}
}

func TestPollOnceFallbackRuleTwo(t *testing.T) {
	t.Run("跃入 in_review 未见 verdict → 有新动态卡", func(t *testing.T) {
		st := newMemStore()
		mc := newFakeMultica()
		req := newTestReq("r1", "issue-1")
		st.addReq(req)
		mc.addIssue("issue-1", "in_progress")
		p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
		require.NoError(t, err)
		ctx := context.Background()
		require.NoError(t, p.PollOnce(ctx)) // 首轮：in_progress

		mc.setStatus("issue-1", "in_review")
		require.NoError(t, p.PollOnce(ctx))

		cards := st.cardsOf("r1")
		require.Len(t, cards, 1)
		require.Equal(t, flow.GateUpdate, cards[0].Kind)
		require.Empty(t, cards[0].CommentID, "状态类兜底卡无评论溯源")
		require.NotEmpty(t, cards[0].Payload)
	})

	t.Run("同一轮里 verdict 评论先到 → 不弹兜底卡", func(t *testing.T) {
		st := newMemStore()
		mc := newFakeMultica()
		req := newTestReq("r1", "issue-1")
		st.addReq(req)
		mc.addIssue("issue-1", "in_review") // 首轮直接 in_review（from 空视同跃入）
		mc.addComment("issue-1", "c1", "verdict: PASS", testNow.Add(1*time.Minute))
		p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
		require.NoError(t, err)
		require.NoError(t, p.PollOnce(context.Background()))

		cards := st.cardsOf("r1")
		require.Len(t, cards, 1, "只应有合并卡，无兜底卡")
		require.Equal(t, flow.GateMerge, cards[0].Kind)
	})

	t.Run("停在 in_review 不算跃迁 → 不再弹", func(t *testing.T) {
		st := newMemStore()
		mc := newFakeMultica()
		req := newTestReq("r1", "issue-1")
		st.addReq(req)
		mc.addIssue("issue-1", "in_review")
		p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
		require.NoError(t, err)
		ctx := context.Background()
		// 首轮 from 为空：契约视同跃入，弹一张（宁可多弹不漏闸门）。
		require.NoError(t, p.PollOnce(ctx))
		require.Equal(t, 1, st.cardCount())
		// 之后停在 in_review（from==to）：不再弹。
		require.NoError(t, p.PollOnce(ctx))
		require.Equal(t, 1, st.cardCount(), "from==to 不触发兜底二")
	})

	t.Run("已见过 verdict → 不弹", func(t *testing.T) {
		st := newMemStore()
		mc := newFakeMultica()
		req := newTestReq("r1", "issue-1")
		st.addReq(req)
		mc.addIssue("issue-1", "in_progress")
		mc.addComment("issue-1", "c1", "verdict: FAIL", testNow.Add(1*time.Minute))
		p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
		require.NoError(t, err)
		ctx := context.Background()
		require.NoError(t, p.PollOnce(ctx)) // 消费 verdict
		st.mu.Lock()
		require.True(t, st.cursors["r1"].SeenVerdict)
		st.mu.Unlock()

		mc.setStatus("issue-1", "in_review")
		require.NoError(t, p.PollOnce(ctx))
		require.Equal(t, 1, st.cardCount(), "seen verdict 后跃入 in_review 不再弹兜底二")
	})
}

func TestPollOnceStoresFirstPRURL(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_progress")
	mc.addComment("issue-1", "c1", "PR 就绪 https://github.com/acme/app/pull/42 请评审", testNow.Add(1*time.Minute))
	mc.addComment("issue-1", "c2", "补一个链接 https://github.com/acme/app/pull/99", testNow.Add(2*time.Minute))

	p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	st.mu.Lock()
	defer st.mu.Unlock()
	require.Equal(t, "https://github.com/acme/app/pull/42", st.reqs["r1"].PRURL, "首条 PR 引用为准，后到的覆盖不了")
}

func TestPollOnceDedupsRedeliveredComments(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	mc.redeliverAnchor = true // 模拟服务端秒级截断：锚点评论重复下发
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_progress")
	mc.addComment("issue-1", "c1", "待批准：计划", testNow)
	mc.addComment("issue-1", "c2", "需要决策：A 还是 B", testNow.Add(1*time.Minute))

	p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, p.PollOnce(ctx))
	require.Equal(t, 2, st.cardCount())

	// 第二轮（重启后锚点重发场景）：c2 与 since 相等被重复下发，不得重复建卡。
	require.NoError(t, p.PollOnce(ctx))
	require.Equal(t, 2, st.cardCount(), "重复下发的评论按评论 id 去重")
}

func TestMergePolicyManual(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeManual}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	require.Equal(t, 1, st.cardCount(), "manual 档：合并卡照常生成")
	cards := st.cardsOf("r1")
	require.Equal(t, flow.GateMerge, cards[0].Kind)
	require.Equal(t, flow.CardPending, cards[0].Status, "manual 档：卡留待人点合并")
	require.Empty(t, gh.mergeCalls, "manual 档不自动合并")
	require.Equal(t, 0, st.auditCount())
	st.mu.Lock()
	require.Equal(t, flow.NodeInReview, st.reqs["r1"].Node, "manual 档节点停在待验收")
	st.mu.Unlock()
}

func TestMergePolicyAutoPass(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeAutoPass}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	// 立即经 github client 合并。
	require.Equal(t, []string{"acme/app/42"}, gh.mergeCalls)
	// 卡收口 + 节点直达已交付。
	cards := st.cardsOf("r1")
	require.Len(t, cards, 1)
	require.Equal(t, flow.CardResolved, cards[0].Status)
	st.mu.Lock()
	require.Equal(t, flow.NodeDelivered, st.reqs["r1"].Node, "auto_pass：节点直达已交付")
	st.mu.Unlock()
	// 自动动作写审计：actor=system。
	require.Equal(t, 1, st.auditCount())
	st.mu.Lock()
	audit := st.audits[0]
	st.mu.Unlock()
	require.Equal(t, "system", audit.Actor)
	require.Equal(t, "merge", audit.Action)
	require.Equal(t, "r1", audit.RequirementID)
	require.NotEmpty(t, audit.Detail)
}

func TestMergePolicyAutoPassFailVerdictNoMerge(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: FAIL https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeAutoPass}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	require.Empty(t, gh.mergeCalls, "FAIL 不自动合并")
	require.Equal(t, 0, st.auditCount())
	cards := st.cardsOf("r1")
	require.Len(t, cards, 1)
	require.Equal(t, flow.CardPending, cards[0].Status, "FAIL 合并卡留待人处理（拒绝并返工）")
}

func TestMergePolicyThresholdUnder(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")
	gh.setDiffStats("acme", "app", 42, 50) // ≤ 阈值 100

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeThreshold, DiffLineThreshold: 100}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	require.Equal(t, []string{"acme/app/42"}, gh.mergeCalls, "diff 行数 ≤ 阈值 → 自动合并")
	require.Equal(t, 1, st.auditCount())
	st.mu.Lock()
	require.Equal(t, flow.NodeDelivered, st.reqs["r1"].Node)
	st.mu.Unlock()
}

func TestMergePolicyThresholdOver(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")
	gh.setDiffStats("acme", "app", 42, 200) // > 阈值 100

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeThreshold, DiffLineThreshold: 100}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	require.Empty(t, gh.mergeCalls, "diff 行数超阈值 → 不自动合并")
	require.Equal(t, 0, st.auditCount())
	cards := st.cardsOf("r1")
	require.Len(t, cards, 1)
	require.Equal(t, flow.CardPending, cards[0].Status, "超阈值 → 弹卡由人合并")
}

func TestAutoMergeBlockedRetriesNextCycle(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")
	// 可重试阻塞：405（分支保护 / CI 未过）。
	gh.mergeErr = &github.APIError{Method: "PUT", Path: "merge", StatusCode: 405, Message: "Required status checks not met"}

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeAutoPass}))
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, p.PollOnce(ctx))
	require.Equal(t, []string{"acme/app/42"}, gh.mergeCalls, "首轮已尝试合并")
	require.Equal(t, 0, st.auditCount(), "合并被阻塞不写审计")
	cards := st.cardsOf("r1")
	require.Equal(t, flow.CardPending, cards[0].Status, "阻塞期间卡保持待处理")
	st.mu.Lock()
	require.Equal(t, flow.NodeInReview, st.reqs["r1"].Node)
	st.mu.Unlock()

	// 阻塞解除（CI 绿了）：下一轮扫到 pending PASS 合并卡，重试成功——
	// 不依赖评论重发。
	gh.mergeErr = nil
	require.NoError(t, p.PollOnce(ctx))
	require.Len(t, gh.mergeCalls, 2, "第二轮经 pending 卡清扫重试合并")
	require.Equal(t, 1, st.auditCount())
	st.mu.Lock()
	require.Equal(t, flow.NodeDelivered, st.reqs["r1"].Node)
	st.mu.Unlock()
}

func TestAutoMergeHardErrorGoesManual(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	gh.addPR("acme", "app", 42, "open")
	// 硬错误：401 鉴权失败——不是可重试阻塞，转人工（卡留待处理）。
	gh.mergeErr = &github.APIError{Method: "PUT", Path: "merge", StatusCode: 401, Message: "Bad credentials"}

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeAutoPass}))
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, p.PollOnce(ctx))
	require.Equal(t, 0, st.auditCount())
	cards := st.cardsOf("r1")
	require.Equal(t, flow.CardPending, cards[0].Status, "硬错误 → 转人工，卡留待处理")
	st.mu.Lock()
	require.Equal(t, flow.NodeInReview, st.reqs["r1"].Node)
	st.mu.Unlock()
}

func TestAutoMergeClosedPRConverges(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS https://github.com/acme/app/pull/42", testNow)
	// PR 已 closed（合并成功后进程崩溃、收口丢失的场景）。
	gh.addPR("acme", "app", 42, "closed")

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeAutoPass}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	require.Empty(t, gh.mergeCalls, "closed PR 不再发 merge 调用")
	require.Equal(t, 1, st.auditCount(), "收敛收口仍写审计")
	st.mu.Lock()
	require.Equal(t, flow.NodeDelivered, st.reqs["r1"].Node)
	st.mu.Unlock()
	cards := st.cardsOf("r1")
	require.Len(t, cards, 1)
	require.Equal(t, flow.CardResolved, cards[0].Status)
}

func TestVerdictWithoutPRURLNoMerge(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	gh := newFakeGitHub()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_review")
	mc.addComment("issue-1", "c1", "verdict: PASS", testNow) // 无 PR 链接

	p, err := newTestPoller(st, mc, gh, StaticPolicy(flow.MergePolicy{Mode: flow.MergeAutoPass}))
	require.NoError(t, err)
	require.NoError(t, p.PollOnce(context.Background()))

	require.Empty(t, gh.mergeCalls)
	cards := st.cardsOf("r1")
	require.Len(t, cards, 1)
	require.Equal(t, flow.CardPending, cards[0].Status, "无 PR 引用 → 合并卡留待人处理")
	st.mu.Lock()
	require.Equal(t, flow.NodeInReview, st.reqs["r1"].Node)
	st.mu.Unlock()
}

func TestPollOnceIsolatesRequirementErrors(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	req1 := newTestReq("r1", "issue-1")
	req2 := newTestReq("r2", "issue-2")
	st.addReq(req1)
	st.addReq(req2)
	mc.addIssue("issue-1", "in_progress")
	mc.addIssue("issue-2", "in_progress")
	mc.getErr["issue-1"] = errors.New("multica down")
	mc.addComment("issue-2", "c1", "待批准：计划", testNow)

	p, err := newTestPoller(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()))
	require.NoError(t, err)
	err = p.PollOnce(context.Background())
	require.Error(t, err, "失败需求的上抛不吞")
	require.Len(t, st.cardsOf("r2"), 1, "单个需求失败不影响其余需求处理")
}

func TestNewIntervalValidation(t *testing.T) {
	policy := StaticPolicy(flow.DefaultMergePolicy())
	_, err := New(newMemStore(), newFakeMultica(), newFakeGitHub(), policy, 0)
	require.Error(t, err)
	_, err = New(newMemStore(), newFakeMultica(), newFakeGitHub(), policy, -time.Second)
	require.Error(t, err)
	_, err = New(newMemStore(), newFakeMultica(), newFakeGitHub(), policy, 61*time.Second)
	require.Error(t, err, "超过 60s 的间隔不满足 AC-3 的 2 分钟反映口径")
	_, err = New(newMemStore(), newFakeMultica(), newFakeGitHub(), policy, 60*time.Second)
	require.NoError(t, err)
}

func TestPollerLifecycle(t *testing.T) {
	st := newMemStore()
	mc := newFakeMultica()
	req := newTestReq("r1", "issue-1")
	st.addReq(req)
	mc.addIssue("issue-1", "in_progress")

	p, err := New(st, mc, newFakeGitHub(), StaticPolicy(flow.DefaultMergePolicy()), 20*time.Millisecond)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, p.Start(ctx))
	require.Error(t, p.Start(ctx), "重复启动报错")

	// 启动即立即执行一轮 + 按 ticker 周期执行。
	deadline := time.Now().Add(2 * time.Second)
	for {
		st.mu.Lock()
		n := st.listCalls
		st.mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("3 轮轮询超时未发生（listCalls=%d）", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	p.Stop()
	st.mu.Lock()
	frozen := st.listCalls
	st.mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	st.mu.Lock()
	after := st.listCalls
	st.mu.Unlock()
	require.LessOrEqual(t, after, frozen+1, "Stop 后不再发起轮询")
	p.Stop() // 幂等
}

func TestParsePRRef(t *testing.T) {
	cases := []struct {
		url       string
		wantOK    bool
		wantOwner string
		wantRepo  string
		wantNum   int
	}{
		{"https://github.com/acme/app/pull/42", true, "acme", "app", 42},
		{"https://github.com/huiyangz/infera/pull/7", true, "huiyangz", "infera", 7},
		{"https://gitlab.com/acme/app/pull/42", false, "", "", 0},
		{"https://github.com/acme/app/pulls/42", false, "", "", 0},
		{"https://github.com/acme/app/pull/abc", false, "", "", 0},
		{"https://github.com/acme/pull/42", false, "", "", 0},
		{"", false, "", "", 0},
		{"https://github.com/acme/app/pull/0", false, "", "", 0},
	}
	for _, tc := range cases {
		owner, repo, num, ok := parsePRRef(tc.url)
		require.Equal(t, tc.wantOK, ok, "url=%q", tc.url)
		if tc.wantOK {
			require.Equal(t, tc.wantOwner, owner, "url=%q", tc.url)
			require.Equal(t, tc.wantRepo, repo, "url=%q", tc.url)
			require.Equal(t, tc.wantNum, num, "url=%q", tc.url)
		}
	}
}
