package reqservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tokfinity/infera/internal/flow"
	"github.com/tokfinity/infera/internal/github"
)

// Approve 批准审批卡：代发评论 approved → 卡片处理 + 审计落库（FR-4/FR-5）。
// 代发失败则卡保持待处理、不留审计（失败动作不算动作）。
func (s *Service) Approve(ctx context.Context, requirementID, cardID string) error {
	return s.postAndResolve(ctx, requirementID, cardID, flow.GateApproval, "approved", "approve", "approved", nil)
}

// Reject 驳回并反馈：用户输入文本原样代发。
func (s *Service) Reject(ctx context.Context, requirementID, cardID, feedback string) error {
	if strings.TrimSpace(feedback) == "" {
		return fmt.Errorf("%w: 驳回反馈不能为空", ErrInvalid)
	}
	return s.postAndResolve(ctx, requirementID, cardID, flow.GateApproval, feedback, "reject", feedback, nil)
}

// 决策卡的固定选项（请求体 choice 的取值，冻结）。
const (
	DecisionRetry  = "retry"  // 重试
	DecisionSkip   = "skip"   // 跳过
	DecisionAbort  = "abort"  // 中止
	DecisionCustom = "custom" // 自定义回复（文本原样代发）
)

// decisionText 决策选项 → 代发文本（固定三项按字面代发，Multica 侧 agent 按字面识别）。
func decisionText(choice, custom string) (string, error) {
	switch choice {
	case DecisionRetry:
		return "重试", nil
	case DecisionSkip:
		return "跳过", nil
	case DecisionAbort:
		return "中止", nil
	case DecisionCustom:
		if custom == "" {
			return "", fmt.Errorf("%w: 自定义回复需要文本", ErrInvalid)
		}
		return custom, nil
	default:
		return "", fmt.Errorf("%w: 未知决策选项 %q（retry/skip/abort/custom）", ErrInvalid, choice)
	}
}

// Decide 处理决策卡：重试 / 跳过 / 中止（固定文本）/ 自定义回复（原样代发）。
//
// T08 出口接线：决策成功后代发评论、卡 resolved、审计与节点返回同一事务。
// 返回语义（经 flow.CanTransition，只从 needs_decision 起跳）：
//   - 重试 / 跳过 / 自定义是"继续执行"的决策 → 回活跃节点（执行中；
//     决策打断的是执行，恢复即执行，节点与 Multica 执行态的偏差由后续
//     轮询按父 issue 状态一轮内校正——单一状态源，不越权猜测执行态）；
//   - 中止 → 直达已交付：flow 大节点集没有 cancelled，中止即不再执行，
//     需求须退出在途，唯一出路是终态；CanTransition 冻结注释明确
//     needs_decision"可回活跃或直达 delivered"，中止正取直达出口。
//
// 节点不在 needs_decision（T08 前存量卡）：只代发 + 收卡，不跃迁。
func (s *Service) Decide(ctx context.Context, requirementID, cardID, choice, text string) error {
	content, err := decisionText(choice, text)
	if err != nil {
		return err
	}
	detail := choice
	if choice == DecisionCustom {
		detail = "custom: " + text
	}
	r, err := s.getRequirement(ctx, requirementID)
	if err != nil {
		return err
	}
	var advance *flow.Node
	if r.Node == flow.NodeNeedsDecision {
		target := flow.NodeInProgress
		if choice == DecisionAbort {
			target = flow.NodeDelivered
		}
		if flow.CanTransition(r.Node, target) {
			advance = &target
		}
	}
	return s.postAndResolve(ctx, requirementID, cardID, flow.GateDecision, content, "decide", detail, advance)
}

// Rework 拒绝合并并返工：返工反馈原样代发。
func (s *Service) Rework(ctx context.Context, requirementID, cardID, feedback string) error {
	if strings.TrimSpace(feedback) == "" {
		return fmt.Errorf("%w: 返工反馈不能为空", ErrInvalid)
	}
	return s.postAndResolve(ctx, requirementID, cardID, flow.GateMerge, feedback, "rework", feedback, nil)
}

// postAndResolve 是评论类代理动作的共享骨架：定位需求与卡 → 校验类型与
// 待处理态 → 代发评论 → 单事务内（处理卡 + 写审计 [+ 节点返回]）。
// advance 非 nil 时（决策动作的 needs_decision 出口）节点返回与卡收口同
// 事务；其余动作传 nil。代发失败直接返回，不动 DB——卡保持待处理可重试。
func (s *Service) postAndResolve(ctx context.Context, requirementID, cardID string,
	kind flow.GateKind, content, action, detail string, advance *flow.Node) error {
	r, err := s.getRequirement(ctx, requirementID)
	if err != nil {
		return err
	}
	if err := s.checkCard(ctx, requirementID, cardID, kind); err != nil {
		return err
	}
	if _, err := s.mc.PostComment(ctx, r.MulticaIssueID, content); err != nil {
		return fmt.Errorf("代发评论失败: %w", err)
	}
	return s.resolveCardWithAudit(ctx, requirementID, cardID, action, detail, advance)
}

// checkCard 校验卡片存在、归属该需求、类型匹配且待处理。
func (s *Service) checkCard(ctx context.Context, requirementID, cardID string, want flow.GateKind) error {
	var kind, status string
	err := s.pool.QueryRow(ctx,
		`SELECT kind, status FROM gate_cards WHERE id = $1 AND requirement_id = $2`,
		cardID, requirementID).Scan(&kind, &status)
	if err != nil {
		return mapErr(err)
	}
	if flow.GateKind(kind) != want {
		return fmt.Errorf("%w: 该动作不适用于 %s 卡", ErrConflict, kind)
	}
	if status != string(flow.CardPending) {
		return fmt.Errorf("%w: 卡片已处理", ErrConflict)
	}
	return nil
}

// resolveCardWithAudit 单事务：卡置 resolved + 审计只增一行 [+ 节点返回]。
// 卡行数为 0 说明并发下已被处理，按冲突回退。advance 非 nil 时再经
// flow 状态机出口更新需求节点——以 node 仍是 needs_decision 为前提
//（并发下节点已被移动则整体按冲突回退），任何一步失败全部回滚。
func (s *Service) resolveCardWithAudit(ctx context.Context, requirementID, cardID, action, detail string, advance *flow.Node) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `
		UPDATE gate_cards SET status = 'resolved', resolved_at = now()
		WHERE id = $1 AND requirement_id = $2 AND status = 'pending'`, cardID, requirementID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w: 卡片已处理", ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_log (id, requirement_id, actor, action, detail)
		VALUES ($1, $2, $3, $4, $5)`,
		newID(), requirementID, ActorUser, action, detail)
	if err != nil {
		return err
	}
	if advance != nil {
		ct, err := tx.Exec(ctx, `
			UPDATE requirements SET node = $2, updated_at = now()
			WHERE id = $1 AND node = $3`,
			requirementID, string(*advance), string(flow.NodeNeedsDecision))
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return fmt.Errorf("%w: 需求节点已离开 needs_decision", ErrConflict)
		}
	}
	return tx.Commit(ctx)
}

// ErrMergeBlocked 是"当前不可合并"类失败的归因（405/409/merged=false）：
// 与鉴权、网络等硬错误区分，API 层给"稍后重试"文案而不是"系统故障"。
var ErrMergeBlocked = errors.New("reqservice: PR 当前不可合并")

// prRef 是从 requirement.pr_url 解析出的 GitHub PR 定位。
type prRef struct {
	Owner  string
	Repo   string
	Number int
}

// parsePRRef 解析规范形 PR URL（https://github.com/{owner}/{repo}/pull/{n}，
// 与 flow.ExtractPRURL 提取的形态一致）。非规范形按冲突处理——数据来自
// 本服务落库的规范提取，走歪说明状态异常。
func parsePRRef(raw string) (prRef, error) {
	m := prURLRe.FindStringSubmatch(raw)
	if m == nil {
		return prRef{}, fmt.Errorf("%w: PR 引用 %q 不是规范 github PR URL", ErrConflict, raw)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil {
		return prRef{}, fmt.Errorf("%w: PR 号非法 %q", ErrConflict, m[3])
	}
	return prRef{Owner: m[1], Repo: m[2], Number: n}, nil
}

// prURLRe 与 flow 包的提取正则同构（flow 冻结不 import 正则导出，这里只解析）。
var prURLRe = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/([0-9]+)$`)

// Merge 终审合并：经 gh API 合并 requirement 关联的 PR → 处理卡 + 审计。
// 不推进大节点——单一状态源纪律：节点由轮询器按 Multica 父 issue 状态推进
// （FR-1/FR-3；自动合并档位的直达已交付是 gatepoll 的显式例外）。
// 合并被阻塞（IsMergeBlocked）时以 ErrMergeBlocked 归因返回，卡保持待处理。
func (s *Service) Merge(ctx context.Context, requirementID, cardID string) (github.MergeResult, error) {
	r, err := s.getRequirement(ctx, requirementID)
	if err != nil {
		return github.MergeResult{}, err
	}
	if err := s.checkCard(ctx, requirementID, cardID, flow.GateMerge); err != nil {
		return github.MergeResult{}, err
	}
	if strings.TrimSpace(r.PRURL) == "" {
		return github.MergeResult{}, fmt.Errorf("%w: 需求尚未关联 GitHub PR（等轮询器从评论提取）", ErrConflict)
	}
	ref, err := parsePRRef(r.PRURL)
	if err != nil {
		return github.MergeResult{}, err
	}
	res, err := s.gh.MergePullRequest(ctx, ref.Owner, ref.Repo, ref.Number, github.MergeInput{})
	if err != nil {
		if github.IsMergeBlocked(err) {
			return github.MergeResult{}, fmt.Errorf("%w: %v", ErrMergeBlocked, err)
		}
		return github.MergeResult{}, fmt.Errorf("GitHub 合并失败: %w", err)
	}
	if err := s.resolveCardWithAudit(ctx, requirementID, cardID, "merge",
		fmt.Sprintf("%s#%d sha=%s", ref.Repo, ref.Number, res.SHA), nil); err != nil {
		return github.MergeResult{}, err
	}
	return res, nil
}
