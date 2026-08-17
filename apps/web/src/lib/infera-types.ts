export type DeliveryStatus = 'active' | 'completed' | 'blocked'

export interface Delivery {
  id: string
  project_id: string
  title: string
  description: string
  status: DeliveryStatus
  current_stage: string
  pending_gate: string | null
  fail_count: number
  created_at: string
  updated_at: string
}

export interface ProjectStats {
  active: number
  pending: number
  last_activity: string
}

export interface Project {
  id: string
  name: string
  repo_url: string
  default_branch: string
  pinned: boolean
  created_at: string
  updated_at: string
  stats?: ProjectStats
}

export interface TimelineEvent {
  id: string
  delivery_id: string
  stage: string
  event_type: string
  payload: unknown
  created_at: string
}

export interface Artifact {
  id: string
  delivery_id: string
  stage: string
  kind: string
  content: string
  created_at: string
}

export interface DeliveryDetail {
  delivery: Delivery
  timeline: TimelineEvent[]
  artifacts: Artifact[]
}

export const STAGES = [
  'intake',
  'spec',
  'spec_approval',
  'test_gen',
  'code_gen',
  'unit_test',
  'code_review',
] as const

export type StageName = (typeof STAGES)[number]

export const GATES = new Set(['spec_approval', 'code_review'])

/**
 * 阶段元数据的唯一权威来源（label + hint）。
 * 用 Record<string, ...> 而非 Record<StageName, ...>：后端新增阶段时前端只缺翻译、不崩。
 */
export const STAGE_META: Record<string, { label: string; hint: string }> = {
  intake: { label: '需求受理', hint: '记录需求原文，建立交付档案' },
  spec: {
    label: '规格生成',
    hint: 'Spec Agent 依据需求与仓库代码撰写规格说明',
  },
  spec_approval: {
    label: '规格审批',
    hint: '人工门禁：确认规格无误后流水线继续',
  },
  test_gen: { label: '测试生成', hint: 'Test Agent 依据规格生成测试用例' },
  code_gen: { label: '实现', hint: 'Coder Agent 在仓库工作区内实现需求' },
  unit_test: {
    label: '单元测试',
    hint: '在容器中运行测试，失败自动回环至实现',
  },
  code_review: {
    label: '审查与交付',
    hint: 'Reviewer Agent 预审，人工确认后开出 PR',
  },
}

/** 阶段名 → 中文 label；未知阶段回退原文，永不崩。 */
export function stageLabel(s: string): string {
  return STAGE_META[s]?.label ?? s
}

export interface GateInfo {
  delivery_id: string
  gate: string
  agent_output: { agent?: string; output?: string } | null
  pr_url: string
}
