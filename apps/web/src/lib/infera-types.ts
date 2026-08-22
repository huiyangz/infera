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
  /** multica 来源映射：issue ID 空 = 非 multica 同步；key 为展示键（如 INFERA-79） */
  multica_issue_id: string
  multica_issue_key: string
  /** 同步进来的展示数据（"type:id" 引用串，非同步行为空串） */
  assignee: string
  priority: string
  /** null = 从未同步 */
  multica_synced_at: string | null
  /** 拆分子需求指向父 delivery；父/普通需求为空串 */
  parent_id: string
  /** 拆分批次号 1..N（父/普通需求为 0） */
  wave: number
  /** 父在设计审批选择了拆分 */
  split_mode: boolean
  /** 父合并状态：'' | 'conflict' */
  merge_state: '' | 'conflict'
  /** 需求复杂度：''（老数据，按 small 走）| small | large（规格审批门裁定） */
  complexity: '' | 'small' | 'large'
}

/** 拆分子需求条目（设计审批时的拆分方案行） */
export interface ChildSpec {
  title: string
  description: string
  wave: number
}

// —— 项目任务分组列表（契约冻结于 L202608221704-1-T01：server/internal/api/taskgroups.go） ——

/** 子任务行：阶段组内的展示字段（stage=所属阶段，即拆分批次 wave 1..N） */
export interface TaskChild {
  id: string
  title: string
  stage: number
  status: DeliveryStatus
  current_stage: string
  pending_gate: string
  multica_issue_id: string
  multica_issue_key: string
  assignee: string
  priority: string
  created_at: string
  updated_at: string
}

/** 一个阶段（批次）下的子任务集合：tasks 按创建时间升序 */
export interface TaskStageGroup {
  stage: number
  tasks: TaskChild[]
}

/** 父任务卡片行：Delivery 全字段内联 + 子任务分组摘要（无子任务时 stages 为 []） */
export interface TaskGroupRow extends Delivery {
  child_total: number
  child_completed: number
  stages: TaskStageGroup[]
}

/** 任务清单条目（tasks agent 产出 / 任务审批门可编辑覆盖） */
export interface TaskSpec {
  title: string
  detail: string
}

/** 单条结构化审查意见（R10 双道审查契约，与 server store.Finding 对齐） */
export interface Finding {
  /** 关联任务序号（1-based；0=整体意见，不关联具体任务） */
  task_index: number
  /** critical | major | minor | info（未知值已由引擎归一为 info） */
  severity: 'critical' | 'major' | 'minor' | 'info'
  message: string
  /** 证据引用（file:line / 函数名 / 代码片段） */
  evidence: string
}

/** code_review 门禁响应里的单道审查（findings 引用 + 内容） */
export interface GateReview {
  review: 'spec_conformance' | 'code_quality'
  /** 该道是否已产出（本机交互占位跳过时 false） */
  present: boolean
  /** 规格符合性审查是否按任务清单逐项核验 */
  task_based: boolean
  artifact_id?: string
  findings: Finding[] | null
  /** agent 原始输出（畸形块时人工兜底阅读） */
  raw?: string
}

export interface ProjectStats {
  active: number
  pending: number
  last_activity: string
}

/**
 * 项目维度需求统计（契约冻结于 INFERA-108 T01：
 * GET /api/projects/{id}/stats ← server/internal/store store.RequirementStats）。
 * by_status 恒含四个固定键（无行时为 0）；last_synced_at null = 从未同步。
 */
export interface RequirementStats {
  project_id: string
  requirement_total: number
  by_status: {
    active: number
    queued: number
    completed: number
    blocked: number
  }
  pending_decisions: number
  delivered: number
  last_synced_at: string | null
}

export interface Project {
  id: string
  name: string
  repo_url: string
  default_branch: string
  pinned: boolean
  created_at: string
  updated_at: string
  /** multica 来源映射：项目 ID 空 = 非 multica 同步 */
  multica_project_id: string
  /** null = 从未同步 */
  multica_synced_at: string | null
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

/** 全 11 阶段全序（与 server engine.StageOrder 对齐） */
export const STAGE_ORDER = [
  'intake',
  'spec',
  'spec_approval',
  'design',
  'design_approval',
  'tasks',
  'tasks_approval',
  'test_gen',
  'code_gen',
  'unit_test',
  'code_review',
] as const

/** small / 老数据（complexity=''）走的 7 阶段子集 */
const STAGES = [
  'intake',
  'spec',
  'spec_approval',
  'test_gen',
  'code_gen',
  'unit_test',
  'code_review',
] as const

export type StageName = (typeof STAGE_ORDER)[number]

export const GATES = new Set([
  'spec_approval',
  'design_approval',
  'tasks_approval',
  'code_review',
])

/** 拆分父跳过的阶段：任务拆解与测试由子需求各自承担，父合并后统一跑单元测试 */
export const SPLIT_PARENT_SKIPPED = new Set([
  'tasks',
  'tasks_approval',
  'test_gen',
])

/**
 * 按交付模式派生展示阶段：large → 全 11（拆分父必为 large，其
 * tasks/tasks_approval/test_gen 由展示层标跳过态）；small 与老数据（''）→ 7，
 * 老拆分父仍按 7 阶段展示、仅 test_gen 跳过（同旧版行为）。
 */
export function stagesForDelivery(d: {
  complexity: string
}): readonly StageName[] {
  return d.complexity === 'large' ? STAGE_ORDER : STAGES
}

/**
 * 阶段元数据的唯一权威来源（label + hint）。
 * 用 Record<string, ...> 而非 Record<StageName, ...>：后端新增阶段时前端只缺翻译、不崩。
 */
export const STAGE_META: Record<string, { label: string; hint: string }> = {
  intake: { label: '任务受理', hint: '记录任务原文，建立交付档案' },
  spec: {
    label: '规格生成',
    hint: 'Spec Agent 依据任务与仓库代码撰写规格说明',
  },
  spec_approval: {
    label: '规格审批',
    hint: '人工门禁：确认规格无误后流水线继续',
  },
  test_gen: { label: '测试生成', hint: 'Test Agent 依据规格生成测试用例' },
  code_gen: { label: '实现', hint: 'Coder Agent 在仓库工作区内实现任务' },
  unit_test: {
    label: '单元测试',
    hint: '在容器中运行测试，失败自动回环至实现',
  },
  code_review: {
    label: '审查与交付',
    hint: 'Reviewer Agent 预审，人工确认后开出 PR',
  },
  // R10 双道审查（code_review 门禁前置；非流水线阶段，仅供编排绑定处展示）
  spec_conformance: {
    label: '规格符合性审查',
    hint: 'code_review 门禁前置：按任务清单/规格逐项核验实现',
  },
  code_quality: {
    label: '代码质量审查',
    hint: 'code_review 门禁前置：质量维度独立审查',
  },
  // SDD 节点（large 复杂度路径）
  design: { label: '设计生成', hint: 'Design Agent 依据规格产出实现设计' },
  design_approval: { label: '设计审批', hint: '人工门禁：确认设计后继续' },
  tasks: { label: '任务生成', hint: 'Task Agent 按设计拆解任务清单' },
  tasks_approval: {
    label: '任务审批',
    hint: '人工门禁：确认任务清单后开始实现',
  },
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
  /** spec_approval：AI 复杂度建议（'small' | 'large'；无/坏建议为空串，前端按 small 预选） */
  complexity_suggestion?: string
  /** design_approval：AI 从 design 推荐的拆分方案（无建议为 null） */
  split_plan?: ChildSpec[] | null
  /** tasks_approval：引擎解析后的任务清单（坏内容为 null） */
  tasks?: TaskSpec[] | null
  /** code_review：门禁挂起前固化的真 diff（无则空串） */
  diff?: string
  /** code_review：两道门禁前置审查的 findings 报告（R10） */
  reviews?: GateReview[]
}
