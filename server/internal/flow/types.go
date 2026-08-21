package flow

import (
	"errors"
	"time"
)

// Requirement 是 infera 侧的需求聚合根。业务元数据（描述、验收标准、来源、
// 优先级、验收人）只存 infera——Multica 侧只承载执行，不回存这些字段。
type Requirement struct {
	ID                 string
	Title              string
	Description        string   // 业务描述（只存 infera）
	AcceptanceCriteria string   // 验收标准（只存 infera）
	Source             string   // 来源
	Priority           string   // 优先级
	Acceptors          []string // 验收人
	MulticaIssueID     string   // Multica issue id 映射
	MulticaIssueKey    string   // 如 INFERA-31
	Node               Node     // 大节点（单一状态源）
	PRURL              string   // 评论中提取的 github PR 引用
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// CardStatus 是闸门卡的待处理状态。
type CardStatus string

const (
	CardPending  CardStatus = "pending"  // 待处理
	CardResolved CardStatus = "resolved" // 已处理（用户动作已代发）
)

// GateCard 是闸门卡（审批 / 决策 / 合并 / 有新动态）的领域形态，
// 对应 gate_cards 表：Payload 是渲染用正文，CommentID 溯源触发评论
// （状态类兜底卡无评论，为空）。
type GateCard struct {
	ID            string
	RequirementID string
	Kind          GateKind
	Status        CardStatus
	Payload       string
	CommentID     string
	CreatedAt     time.Time
	ResolvedAt    time.Time // 零值 = 未处理
}

// AuditEntry 是代理动作审计：谁、何时、做了什么。只增不改。
type AuditEntry struct {
	ID            string
	RequirementID string
	Actor         string // 谁（用户标识）
	Action        string // 做了什么（approve / reject / decide / rework / merge / ...）
	Detail        string // 补充（如驳回反馈原文）
	At            time.Time
}

// MergePolicyMode 是项目级合并策略档位。
type MergePolicyMode string

const (
	MergeManual    MergePolicyMode = "manual"    // 手动合并（默认）：合并卡出现，人点
	MergeAutoPass  MergePolicyMode = "auto_pass" // Reviewer verdict PASS 即自动合并
	MergeThreshold MergePolicyMode = "threshold" // diff 行数低于阈值自动合，超过弹卡
)

// MergePolicy 是项目级合并策略（FR-6）。DiffLineThreshold 仅 threshold 档有意义。
type MergePolicy struct {
	Mode              MergePolicyMode
	DiffLineThreshold int
}

// DefaultMergePolicy 返回默认策略：手动档。
func DefaultMergePolicy() MergePolicy {
	return MergePolicy{Mode: MergeManual}
}

// Validate 校验档位与阈值的组合语义。
func (p MergePolicy) Validate() error {
	switch p.Mode {
	case MergeManual, MergeAutoPass:
		if p.DiffLineThreshold != 0 {
			return errors.New("flow: manual/auto_pass 档不携带 diff 行数阈值")
		}
	case MergeThreshold:
		if p.DiffLineThreshold <= 0 {
			return errors.New("flow: threshold 档要求正的 diff 行数阈值")
		}
	default:
		return errors.New("flow: 未知合并策略档位 " + string(p.Mode))
	}
	return nil
}

// PollCursor 是一个在途需求的轮询位置（持久化在 requirements 行上）：
// 增量评论游标 + 上次见到的 Multica 状态 + 是否见过 verdict。
// LastCommentAt 之后的评论才算新（严格大于——同一时刻的评论不重复消费）。
type PollCursor struct {
	RequirementID  string
	MulticaIssueID string
	LastCommentAt  time.Time // since 游标（零值 = 全量拉取）
	LastStatus     string    // 上次见到的 Multica 父 issue 状态
	SeenVerdict    bool      // 是否已见过任何 verdict 评论（兜底规则二的输入）
	UpdatedAt      time.Time
}
