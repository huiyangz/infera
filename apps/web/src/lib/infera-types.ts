export type DeliveryStatus = 'active' | 'completed' | 'blocked' | 'queued'

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
  /** 拆分子需求指向父 delivery；父/普通需求为空串 */
  parent_id: string
  /** 拆分批次号 1..N（父/普通需求为 0） */
  wave: number
  /** 父在规格审批选择了拆分 */
  split_mode: boolean
  /** 父合并状态：'' | 'conflict' */
  merge_state: '' | 'conflict'
}

/** 规格审批时的拆分子需求条目 */
export interface ChildSpec {
  title: string
  description: string
  wave: number
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
  /** 拆分父的子需求列表（仅 split 父返回） */
  children?: Delivery[]
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
  // 未来 SDD 节点（本批先补 label，接入后即正确显示）
  design: { label: '设计生成', hint: 'Design Agent 产出实现设计' },
  design_approval: { label: '设计审批', hint: '人工门禁：确认设计后继续' },
  tasks: { label: '任务生成', hint: 'Task Agent 拆解任务清单' },
  tasks_approval: { label: '任务审批', hint: '人工门禁：确认任务后继续' },
}

/** 阶段名 → 中文 label；未知阶段回退原文，永不崩。 */
export function stageLabel(s: string): string {
  return STAGE_META[s]?.label ?? s
}

// —— agent 编排 ——

export type AgentRunner = 'cli' | 'http' | 'docker' | 'local'

/** 注册的执行者（本批前端只读展示，CRUD 走后端/seed） */
export interface Agent {
  id: string
  name: string
  runner: AgentRunner
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

/** 节点 → agent_id 绑定表 */
export type BindingMap = Record<string, string>

/** GET /api/pipeline */
export interface PipelineInfo {
  nodes: string[]
  agents: Agent[]
  bindings: BindingMap
}

/** GET /api/projects/:id/pipeline —— effective 是 node 键的对象（后端 map 序列化） */
export interface ProjectPipeline {
  nodes: string[]
  defaults: BindingMap
  overrides: BindingMap
  effective: Record<
    string,
    { node: string; agent_id: string; from: 'default' | 'project' }
  >
}

export interface GateInfo {
  delivery_id: string
  gate: string
  agent_output: { agent?: string; output?: string } | null
  pr_url: string
  /** spec_approval：AI 从 spec 推荐的拆分方案（无建议为 null） */
  split_plan: ChildSpec[] | null
}
