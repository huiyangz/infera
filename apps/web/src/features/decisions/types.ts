/**
 * 「需要决策」面的类型（契约冻结于 INFERA-108 T01：
 * server/internal/store 的 store.PendingDecision，JSON 标签即契约）。
 */

/** 待人工决策的需求行（GET /api/pending-decisions 行；updated_at 降序） */
export interface PendingDecisionRow {
  /** delivery ID —— 直接跳既有需求详情页 /deliveries/$id */
  id: string
  project_id: string
  /** 由查询 JOIN projects 带回的项目名（跨项目全局列表标注用） */
  project_name: string
  title: string
  status: string
  /** 停在的审批门（spec_approval / design_approval / tasks_approval…） */
  pending_gate: string
  current_stage: string
  /** '' = 本地需求（非同步来源） */
  external_issue_key: string
  /** 同步展示数据；'' = 无 */
  assignee: string
  priority: string
  created_at: string
  updated_at: string
}
