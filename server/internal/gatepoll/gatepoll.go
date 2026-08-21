// Package gatepoll 是闸门检测轮询器（INFERA-11 FR-3 / FR-6）：
// 后台按分钟级间隔轮询在途需求，驱动 Multica 状态 → infera 大节点推进、
// 增量评论 → 闸门卡生成，并按项目合并策略档位执行自动合并。
//
// 本包不内嵌任何 AI：轮询与判定全部确定性。对 multica / github client 与
// 持久层均通过窄接口（Go 鸭型）消费，单测用 fake，不碰真服务。
package gatepoll

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/multica"
)

// MulticaClient 是轮询器消费的 multica 薄 client 最小面（真 *multica.Client
// 结构化满足，无需适配器）。
type MulticaClient interface {
	GetIssue(ctx context.Context, idOrKey string) (multica.Issue, error)
	ListCommentsSince(ctx context.Context, issueID string, cur multica.CommentCursor) ([]multica.Comment, multica.CommentCursor, error)
}

// GitHubClient 是合并闸门消费的 github client 最小面。
type GitHubClient interface {
	GetPullRequest(ctx context.Context, owner, repo string, number int) (github.PullRequest, error)
	GetDiffStats(ctx context.Context, owner, repo string, number int) (github.DiffStats, error)
	MergePullRequest(ctx context.Context, owner, repo string, number int, in github.MergeInput) (github.MergeResult, error)
}

// InFlight 是一次轮询取回的在途需求：需求聚合 + 挂在其行上的轮询游标。
type InFlight struct {
	Req    flow.Requirement
	Cursor flow.PollCursor
}

// Store 是轮询器的持久层最小面（PgStore 实现落 requirements / gate_cards /
// audit_log 三张表；需求的创建不在此——那是 reqservice 的职责，本包只读消费 + 推进）。
type Store interface {
	ListInFlight(ctx context.Context) ([]InFlight, error)
	InsertCardIfNew(ctx context.Context, card flow.GateCard) (bool, error)
	ListPendingMergeCards(ctx context.Context, requirementID string) ([]flow.GateCard, error)
	CompleteAutoMerge(ctx context.Context, cardID string, node flow.Node, prURL string, cur flow.PollCursor, audit flow.AuditEntry) error
	SavePollState(ctx context.Context, requirementID string, node flow.Node, prURL string, cur flow.PollCursor) error
}

// MergePolicyResolver 按需求解析项目级合并策略（FR-6）。requirements 表与
// infera 项目的关联属 reqservice 的装配范畴，本包只消费解析结果。
type MergePolicyResolver interface {
	MergePolicy(ctx context.Context, req flow.Requirement) (flow.MergePolicy, error)
}

// StaticPolicy 是固定档位解析器（单策略部署 / 测试用）。
type StaticPolicy flow.MergePolicy

// MergePolicy 实现 MergePolicyResolver。
func (s StaticPolicy) MergePolicy(_ context.Context, _ flow.Requirement) (flow.MergePolicy, error) {
	return flow.MergePolicy(s), nil
}

// auditActorSystem 是自动动作的审计署名（任务契约：自动动作写审计，actor=system）。
const auditActorSystem = "system"

// ruleTwoPayload 是兜底规则二的中性卡正文（FR-3：防 agent 格式跑偏漏合并闸门，
// 宁可多弹一张中性卡）。
const ruleTwoPayload = "需求已进入待验收（in_review），但尚未收到合并评审结论（verdict）——请查看执行平台进展。"

// Poller 是闸门轮询器：一次构造，Start/Stop 控制后台循环；PollOnce 供装配层
// 与测试直接驱动。
type Poller struct {
	store       Store
	mc          MulticaClient
	gh          GitHubClient
	policy      MergePolicyResolver
	interval    time.Duration
	mergeMethod github.MergeMethod // 留空 = merge commit（github client 默认）

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// New 构造轮询器。interval 必须在 (0, 60s]——AC-3 要求状态变化 2 分钟内反映，
// 超 60s 的间隔在构造期直接挡掉。
func New(store Store, mc MulticaClient, gh GitHubClient, policy MergePolicyResolver, interval time.Duration) (*Poller, error) {
	if interval <= 0 || interval > 60*time.Second {
		return nil, errors.New("gatepoll: interval 必须在 (0, 60s]")
	}
	return &Poller{store: store, mc: mc, gh: gh, policy: policy, interval: interval}, nil
}

// Start 启动后台循环：先立即执行一轮（首屏快速反映），再按间隔周期执行。
// 循环随 ctx 取消或 Stop 结束。重复启动报错。
func (p *Poller) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return errors.New("gatepoll: 轮询器已在运行")
	}
	ctx, p.cancel = context.WithCancel(ctx)
	done := make(chan struct{})
	p.done = done
	p.running = true
	go func() {
		defer close(done)
		p.loop(ctx)
	}()
	return nil
}

// Stop 优雅停止：取消循环并等待在途一轮结束。幂等；未启动时为 no-op。
func (p *Poller) Stop() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	stopped := !p.running
	if p.running {
		p.running = false
	}
	p.mu.Unlock()
	if stopped || cancel == nil {
		return
	}
	cancel()
	<-done
}

func (p *Poller) loop(ctx context.Context) {
	// 启动即先跑一轮：不等首个 tick。
	if err := p.PollOnce(ctx); err != nil {
		log.Printf("gatepoll: 首轮轮询: %v", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.PollOnce(ctx); err != nil {
				log.Printf("gatepoll: 轮询: %v", err)
			}
		}
	}
}

// PollOnce 对全部在途需求执行一轮轮询：单个需求失败不阻断其余需求，
// 全部失败原因聚合上抛（供装配层观测；下一轮自然重试）。
func (p *Poller) PollOnce(ctx context.Context) error {
	inFlight, err := p.store.ListInFlight(ctx)
	if err != nil {
		return fmt.Errorf("gatepoll: 拉取在途需求: %w", err)
	}
	var errs []error
	for _, st := range inFlight {
		if err := p.pollRequirement(ctx, st); err != nil {
			log.Printf("gatepoll: 需求 %s 轮询失败: %v", st.Req.MulticaIssueKey, err)
			errs = append(errs, fmt.Errorf("需求 %s: %w", st.Req.ID, err))
		}
	}
	return errors.Join(errs...)
}

// pollRequirement 轮询单个需求，一次完成：
//
//  1. GetIssue 读 Multica 父 issue 状态；
//  2. 增量拉评论（游标重建自持久化的 LastCommentAt；AfterID 不持久化——
//     服务端秒级截断会让锚点秒旧评论重发，按 multica.ListCommentsSince 的
//     调用方契约以评论 id 幂等去重，见 InsertCardIfNew）；
//  3. 每条评论经 flow 解析器 → 闸门卡落库（含兜底一）；PR URL 首条存引用；
//  4. 兜底规则二：跃入 in_review 未见 verdict → 中性"有新动态"卡；
//  5. 状态经 flow 状态机推进大节点；
//  6. 合并策略档位清扫：PASS 合并卡在 auto_pass/threshold 档自动合并，
//     节点直达已交付并写审计（actor=system）；
//  7. 节点 + 游标落库（重启续读不重放的持久化根基）。
//
// 写序刻意"先卡后游标"：中途失败重跑最多多弹卡（评论 id 去重兜住绝大多数），
// 不会漏卡——与 client"宁可重发不漏发"的取舍同向。
func (p *Poller) pollRequirement(ctx context.Context, st InFlight) error {
	req := st.Req
	cur := st.Cursor
	seenVerdict := cur.SeenVerdict
	prURL := req.PRURL

	issue, err := p.mc.GetIssue(ctx, issueRef(req))
	if err != nil {
		return fmt.Errorf("读取 issue %s: %w", issueRef(req), err)
	}

	comments, next, err := p.mc.ListCommentsSince(ctx, req.MulticaIssueID, multica.CommentCursor{Since: cur.LastCommentAt})
	if err != nil {
		return fmt.Errorf("增量拉评论 %s: %w", req.MulticaIssueID, err)
	}
	for _, c := range comments {
		ev := flow.ParseComment(flow.CommentInput{
			ID: c.ID, AuthorType: c.AuthorType, AuthorID: c.AuthorID, Body: c.Content, CreatedAt: c.CreatedAt,
		})
		if ev.PRURL != "" && prURL == "" {
			prURL = ev.PRURL // 首条 PR 引用为准
		}
		if ev.Kind == flow.GateMerge {
			seenVerdict = true
		}
		if _, err := p.store.InsertCardIfNew(ctx, flow.GateCard{
			RequirementID: req.ID, Kind: ev.Kind, Payload: ev.Body, CommentID: ev.CommentID,
		}); err != nil {
			return fmt.Errorf("落库闸门卡（评论 %s）: %w", c.ID, err)
		}
	}

	newCur := flow.PollCursor{
		RequirementID:  req.ID,
		MulticaIssueID: req.MulticaIssueID,
		LastCommentAt:  next.Since, // 直接用 client 返回的 next 游标，不自拼时间戳
		LastStatus:     issue.Status,
		SeenVerdict:    seenVerdict,
	}

	// 兜底规则二在评论消费之后：同一轮到达的 verdict 先把 seenVerdict 置真，
	// 不误弹中性卡。
	if flow.InReviewWithoutVerdict(cur.LastStatus, issue.Status, seenVerdict) {
		if _, err := p.store.InsertCardIfNew(ctx, flow.GateCard{
			RequirementID: req.ID, Kind: flow.GateUpdate, Payload: ruleTwoPayload,
		}); err != nil {
			return fmt.Errorf("落库兜底卡: %w", err)
		}
	}

	req.Node = flow.Advance(req.Node, issue.Status)
	req.PRURL = prURL

	// 自动合并清扫（含本轮新建卡的首试与历史 pending 卡的重试）。
	if err := p.sweepAutoMerge(ctx, &req, newCur); err != nil {
		return err
	}

	if err := p.store.SavePollState(ctx, req.ID, req.Node, req.PRURL, newCur); err != nil {
		return fmt.Errorf("落库轮询状态: %w", err)
	}
	return nil
}

// sweepAutoMerge 对该需求的 pending PASS 合并卡执行合并策略档位（FR-6）：
//
//   - manual：不动——合并卡留待人点；
//   - auto_pass：立即合并，成功后节点直达已交付；
//   - threshold：diff 行数 ≤ 阈值自动合并，超过弹卡留人。
//
// 每轮都扫：可重试合并阻塞（IsMergeBlocked，如 CI 未过）下一轮自然重试；
// PR 已 closed（合并成功后收口丢失的崩溃窗口）直接收敛收口。失败路径一律
// 保持卡 pending——卡本身就是人合并的逃生口。
func (p *Poller) sweepAutoMerge(ctx context.Context, req *flow.Requirement, cur flow.PollCursor) error {
	policy, err := p.policy.MergePolicy(ctx, *req)
	if err != nil {
		return fmt.Errorf("解析合并策略: %w", err)
	}
	if policy.Mode == flow.MergeManual {
		return nil
	}
	cards, err := p.store.ListPendingMergeCards(ctx, req.ID)
	if err != nil {
		return fmt.Errorf("拉取 pending 合并卡: %w", err)
	}
	if len(cards) == 0 {
		return nil
	}

	owner, repo, num, prOK := parsePRRef(req.PRURL)
	if req.PRURL != "" && !prOK {
		// flow.ExtractPRURL 产物不会走到这；存储被外改时的防御。
		log.Printf("gatepoll: 需求 %s 的 PR 引用 %q 无法解析，自动合并跳过", req.MulticaIssueKey, req.PRURL)
		return nil
	}

	for _, card := range cards {
		if flow.ExtractVerdict(card.Payload) != flow.VerdictPass {
			continue // FAIL 结论：拒绝并返工是人决策，不自动动作
		}
		if !prOK {
			log.Printf("gatepoll: 需求 %s 无 PR 引用，PASS 合并卡留待人处理", req.MulticaIssueKey)
			return nil
		}
		if policy.Mode == flow.MergeThreshold {
			stats, err := p.gh.GetDiffStats(ctx, owner, repo, num)
			if err != nil {
				log.Printf("gatepoll: 需求 %s 拉 diff 统计: %v（下轮重试）", req.MulticaIssueKey, err)
				continue
			}
			if stats.Changes > policy.DiffLineThreshold {
				continue // 超阈值：卡留人
			}
		}
		pr, err := p.gh.GetPullRequest(ctx, owner, repo, num)
		if err != nil {
			log.Printf("gatepoll: 需求 %s 读 PR: %v（下轮重试）", req.MulticaIssueKey, err)
			continue
		}
		if pr.State != "open" {
			// closed：合并成功后收口丢失的收敛路径——视为已了结。
			return p.completeMerge(ctx, card.ID, req, cur, policy, "PR 已关闭，收敛收口")
		}
		if _, err := p.gh.MergePullRequest(ctx, owner, repo, num, github.MergeInput{Method: p.mergeMethod}); err != nil {
			if github.IsMergeBlocked(err) {
				log.Printf("gatepoll: 需求 %s 自动合并暂被阻塞: %v（下轮重试）", req.MulticaIssueKey, err)
			} else {
				log.Printf("gatepoll: 需求 %s 自动合并硬失败，转人工: %v", req.MulticaIssueKey, err)
			}
			continue
		}
		return p.completeMerge(ctx, card.ID, req, cur, policy, "")
	}
	return nil
}

// completeMerge 自动合并成功（或已 closed 收敛）后的一次性收口：卡置 resolved、
// 写审计（actor=system）、节点直达已交付、游标落库——单事务语义由 Store 保证。
func (p *Poller) completeMerge(ctx context.Context, cardID string, req *flow.Requirement, cur flow.PollCursor, policy flow.MergePolicy, note string) error {
	detail := fmt.Sprintf("auto merge %s (policy=%s)", req.PRURL, policy.Mode)
	if note != "" {
		detail += "，" + note
	}
	if err := p.store.CompleteAutoMerge(ctx, cardID, flow.NodeDelivered, req.PRURL, cur, flow.AuditEntry{
		RequirementID: req.ID, Actor: auditActorSystem, Action: "merge", Detail: detail,
	}); err != nil {
		return fmt.Errorf("自动合并收口: %w", err)
	}
	req.Node = flow.NodeDelivered
	return nil
}

// issueRef 返回 GetIssue 的定位符：优先 issue id，缺省回退 key。
func issueRef(req flow.Requirement) string {
	if req.MulticaIssueID != "" {
		return req.MulticaIssueID
	}
	return req.MulticaIssueKey
}

// parsePRRef 把规范形 PR URL（flow.ExtractPRURL 产物）拆成 owner / repo / number。
func parsePRRef(u string) (owner, repo string, number int, ok bool) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(u, prefix) {
		return "", "", 0, false
	}
	parts := strings.Split(strings.TrimPrefix(u, prefix), "/")
	if len(parts) != 4 || parts[2] != "pull" {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return "", "", 0, false
	}
	return parts[0], parts[1], n, true
}
