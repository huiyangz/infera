/**
 * 交付状态词表（契约冻结于 INFERA-232 / T01：server/internal/syncsvc
 * translateStatus 与 engine 状态常量）——cancelled 是上游「放弃」的独立
 * 终态，不折叠为 completed（放弃 ≠ 交付）。
 */
export type DeliveryStatus =
  | 'active'
  | 'completed'
  | 'blocked'
  | 'queued'
  | 'cancelled'

/**
 * 交付所挂标签（契约冻结于 INFERA-218：server/internal/api/labels.go 的
 * labelJSON）——仅 name + color，color 是上游（Multica）hex 原值如 #22c55e，
 * 不做色彩换算，也不带内部 id。
 */
export interface DeliveryLabel {
  name: string
  color: string
}

export interface Delivery {
  id: string
  project_id: string
  title: string
  description: string
  status: DeliveryStatus
  current_stage: string
  pending_gate: string | null
  fail_count: number
  /**
   * 流水线内部字段（server store.Delivery 内联返回，INFERA-228 对齐
   * task-groups 冻结键集）：列表展示不消费；声明为可选，本地构造的
   * 测试替身可省。
   */
  base_commit?: string
  reject_reason?: string
  workspace_ready?: boolean
  created_at: string
  updated_at: string
  /** 外部任务源映射：issue ID 空 = 非同步来源；key 为展示键（如 INFERA-79） */
  external_issue_id: string
  external_issue_key: string
  /** 同步进来的展示数据（"type:id" 引用串，非同步行为空串） */
  assignee: string
  priority: string
  /** null = 从未同步 */
  external_synced_at: string | null
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
  /**
   * 挂的标签。任务分组列表 / 任务详情 / 合并恢复响应恒为数组（未挂 = 空数组）；
   * 新建需求 201 响应是裸 Delivery 不带该字段，故为可选，展示层按空处理。
   */
  labels?: DeliveryLabel[]
}

/** 拆分子需求条目（设计审批时的拆分方案行） */
export interface ChildSpec {
  title: string
  description: string
  wave: number
}

// —— 项目任务分组列表（契约冻结于 L202608221704-1-T01：server/internal/api/taskgroups.go；
// INFERA-228 / L202608241931-1-T01 复核冻结左侧列表口径：顶层行即父任务
// （parent_id 空串），子任务嵌于父行 stages[].tasks[]（父子关系由结构表达），
// 每个任务项带 id / title / status，子行另带 stage（=wave，0=无阶段），
// 父行带 current_stage —— 三类信息齐备，无需旁路取数） ——

/** 子任务行：阶段组内的展示字段（stage=所属阶段，即拆分批次 wave 1..N） */
export interface TaskChild {
  id: string
  title: string
  stage: number
  status: DeliveryStatus
  current_stage: string
  pending_gate: string
  external_issue_id: string
  external_issue_key: string
  assignee: string
  priority: string
  /** 挂的标签（task-groups 契约恒为数组，未挂 = 空数组） */
  labels: DeliveryLabel[]
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

// —— 子任务真实进度聚合（契约冻结于 L202608260142-1-T01：
// GET /api/deliveries/{id}/progress ← server/internal/store
// store.ChildProgress / ChildStageProgress / ChildProgressCounts，
// 逐字段镜像，不得静默变更） ——

/**
 * 单一维度（总体或单个阶段组）的子任务真实状态计数。
 * 六个展示计数互斥且恰好覆盖五个已知状态（in_progress + in_review 拆完
 * active），可直接相加不去重；by_status 恒含五键（无行为 0），未知状态只
 * 计入 total。
 */
export interface ChildProgressCounts {
  total: number
  done: number
  in_progress: number
  in_review: number
  blocked: number
  todo: number
  cancelled: number
  by_status: {
    active: number
    queued: number
    completed: number
    blocked: number
    cancelled: number
  }
}

/** 一个阶段组的子任务聚合：stage = wave（0 = 无阶段组，编号组之后垫底） */
export interface ChildStageProgress extends ChildProgressCounts {
  stage: number
}

/**
 * 父任务子任务真实进度整体。active_stage = 当前活跃阶段（最小编号、仍存在
 * 非终态子任务者）；全部完结或只有无阶段子任务 → null。无子任务时 stages
 * 为 []（非 null）。
 */
export interface ChildProgress extends ChildProgressCounts {
  delivery_id: string
  active_stage: number | null
  stages: ChildStageProgress[]
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

interface ProjectStats {
  active: number
  pending: number
  last_activity: string
}

/**
 * 项目维度需求统计（契约冻结于 INFERA-108 T01：
 * GET /api/projects/{id}/stats ← server/internal/store store.RequirementStats）。
 * by_status 恒含五个固定键（无行时为 0；cancelled 为 INFERA-232 新增的
 * 放弃终态）；delivered 与 by_status.completed 同源，cancelled 不计入。
 * last_synced_at null = 从未同步。
 */
export interface RequirementStats {
  project_id: string
  requirement_total: number
  by_status: {
    active: number
    queued: number
    completed: number
    blocked: number
    cancelled: number
  }
  pending_decisions: number
  delivered: number
  last_synced_at: string | null
}

// —— 项目 agent 执行时序（契约冻结于 INFERA-234 T01：
// GET /api/projects/{id}/stage-runs ← server/internal/store
// store.ProjectStageRuns / StageRunDetail / StageRunStageStats，
// 逐字段镜像，不得静默变更） ——

/** 一次 stage 运行的状态域（store.StageRunDetail.Status） */
export type StageRunStatus = 'running' | 'done' | 'failed'

/** 时序明细行：项目内各 delivery 的 stage_run（runs 数组元素） */
export interface StageRunDetail {
  id: string
  delivery_id: string
  title: string
  /** 空 = 本地需求（非同步来源） */
  external_issue_key: string
  stage: string
  /** 重试序号（1 起） */
  attempt: number
  status: StageRunStatus
  /**
   * 绑定到该 stage 的 agent 名（pipeline_bindings: node=stage）；
   * 未配置绑定、或门禁/命令节点 → null。
   */
  agent_name: string | null
  /** RFC3339（Go time.Time 原样序列化，可带纳秒小数位） */
  started_at: string
  /** running 未收尾 → null */
  finished_at: string | null
  /** finished_at - started_at 的毫秒数；running 未收尾 → null */
  duration_ms: number | null
}

/** 分 stage 聚合行（by_stage 数组元素） */
export interface StageRunStageStats {
  stage: string
  total: number
  done: number
  failed: number
  running: number
  /** 只统计已收尾（finished_at 非空）的运行；无已收尾运行时为 0（非 null） */
  avg_ms: number
  /** 最近邻位法 p95；口径同 avg_ms */
  p95_ms: number
}

/**
 * 项目 agent 执行时序整体载荷。runs 按 started_at 倒序，最多 200 条
 * （stageRunsDetailLimit），by_stage 聚合同一窗口；空项目两者为空数组。
 * 注意 by_stage 按 stage **字典序**返回——不是流水线阶段序，消费方须按
 * 自身的阶段序重排（见 STAGE_ORDER），此处不假设也不改写顺序。
 */
export interface ProjectStageRuns {
  project_id: string
  runs: StageRunDetail[]
  by_stage: StageRunStageStats[]
}

export interface Project {
  id: string
  name: string
  repo_url: string
  default_branch: string
  pinned: boolean
  created_at: string
  updated_at: string
  /** 外部任务源映射：项目 ID 空 = 非同步来源 */
  external_project_id: string
  /** null = 从未同步 */
  external_synced_at: string | null
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
const STAGE_ORDER = [
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

type AgentRunner = 'cli' | 'http' | 'docker' | 'local'

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

/**
 * GET/PUT /api/projects/:id/pipeline —— 项目级唯一绑定定义
 * （契约冻结于 INFERA-180：server/internal/api/orchestration.go，
 * 全局默认编排已删除，无 defaults/overrides/effective/from 字段）。
 */
export interface ProjectPipeline {
  nodes: string[]
  bindings: BindingMap
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
