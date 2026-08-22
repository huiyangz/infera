/**
 * 需求流转面的类型与常量（契约冻结于 T05 提交 07df987：
 * server/internal/api/requirements.go + server/internal/reqservice）。
 */

/** 业务大节点（单一状态源）；needs_decision 是异常节点，不在线性推进序列里 */
export type FlowNode =
  | 'intake'
  | 'dispatched'
  | 'in_progress'
  | 'in_review'
  | 'delivered'
  | 'needs_decision'

/** 闸门卡类型（值即 gate_cards.kind） */
export type GateKind = 'approval' | 'decision' | 'merge' | 'update'

/** 卡片待处理状态 */
export type CardStatus = 'pending' | 'resolved'

/** 决策卡固定选项（POST decide 的 choice 取值，冻结） */
export type DecisionChoice = 'retry' | 'skip' | 'abort' | 'custom'

/** 合并策略档位 */
export type MergePolicyMode = 'manual' | 'auto_pass' | 'threshold'

/** 需求行（GET 详情 / 列表共有字段；含 Multica / GitHub 深链） */
export interface Requirement {
  id: string
  title: string
  description: string
  acceptance_criteria: string
  source: string
  priority: string
  acceptors: string[]
  multica_issue_id: string
  multica_issue_key: string
  /** Multica issue 深链（空串 = 未派发） */
  multica_issue_url: string
  /** 评论中提取的 GitHub PR URL（空串 = 尚未关联） */
  pr_url: string
  node: FlowNode
  created_at: string
  updated_at: string
}

/** 需求列表行：需求行 + 待处理卡计数 */
export interface RequirementListItem extends Requirement {
  pending_card_count: number
}

/** 闸门卡（payload 为渲染用正文——触发评论原文） */
export interface GateCard {
  id: string
  requirement_id: string
  kind: GateKind
  status: CardStatus
  payload: string
  comment_id: string
  created_at: string
  /** null = 未处理 */
  resolved_at: string | null
}

/** 需求详情：需求行 + 待处理闸门卡 */
export interface RequirementDetail extends Requirement {
  pending_cards: GateCard[]
}

/** 代理动作审计行（谁、何时、做了什么；只增不改） */
export interface AuditEntry {
  id: string
  requirement_id: string
  actor: string
  action: string
  detail: string
  at: string
}

/** 项目合并策略（FR-6；threshold 档要求正阈值，manual/auto_pass 档为 0） */
export interface MergePolicy {
  mode: MergePolicyMode
  diff_line_threshold: number
}

/** POST merge 卡动作的响应（github.MergeResult） */
export interface MergeResult {
  merged: boolean
  sha: string
  message: string
}

/** PR 行级评审评论（GET pr-review 的元素；删除行评论 line=0、行号在 original_line） */
export interface PRReviewComment {
  id: number
  path: string
  line: number
  original_line: number
  /** RIGHT = 新增行，LEFT = 删除行 */
  side: 'RIGHT' | 'LEFT'
  body: string
  author: string
  /** 0 = 顶层评论，非 0 = 回复 */
  in_reply_to_id: number
  created_at: string
}

/** PR diff 概要（文件数与 +/- 行数） */
export interface PRDiffStats {
  files: number
  additions: number
  deletions: number
  changes: number
}

/** PR 评审面（GET /api/requirements/{id}/pr-review，T09 加法扩展）：行级评论 + diff 概要 */
export interface PRReview {
  pr_url: string
  comments: PRReviewComment[]
  diff: PRDiffStats
}

/** 发起需求输入面（title 必填，服务端 400 校验） */
export interface CreateRequirementInput {
  title: string
  description: string
  acceptance_criteria: string
  source: string
  priority: string
  acceptors: string[]
}

/** 线性推进的大节点序列（4+1 的主线；needs_decision 异常态单独渲染） */
export const NODE_SEQUENCE: readonly FlowNode[] = [
  'intake',
  'dispatched',
  'in_progress',
  'in_review',
  'delivered',
]

/** 大节点 → 中文标签；未知节点回退原文，永不崩 */
export const NODE_META: Record<string, { label: string; hint: string }> = {
  intake: { label: '任务受理', hint: '任务已录入，尚未派发' },
  dispatched: { label: '已派发', hint: '已交由 Agent 团队执行' },
  in_progress: { label: '执行中', hint: 'Agent 正在实现' },
  in_review: { label: '待验收', hint: '实现完成，等待验收' },
  delivered: { label: '已交付', hint: '已合并交付' },
  needs_decision: { label: '需决策', hint: '执行遇到异常，等待人工决策' },
}

export function nodeLabel(n: string): string {
  return NODE_META[n]?.label ?? n
}
