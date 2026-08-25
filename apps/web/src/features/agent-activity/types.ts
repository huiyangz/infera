/**
 * 「Agent 执行时序」面的类型（契约冻结于 INFERA-253：
 * server/internal/store 的 store.AgentActivitySeries / AgentActivityPoint，
 * JSON 标签即契约，逐字段镜像，不得静默变更）。
 */

/** 单个时间桶：T 为桶起点（RFC3339），Count 为该桶内 started_at 落桶的执行次数 */
export interface AgentActivityPoint {
  /** RFC3339 桶起点（Go time.Time 原样序列化，可带纳秒小数位） */
  t: string
  count: number
}

/** 单个 agent 的执行时序曲线（series 数组元素）；Points 覆盖窗口内全部桶（含 count=0，各曲线等长对齐） */
export interface AgentActivitySeries {
  /** 空 = 无绑定 stage 的运行归组 */
  agent_id: string
  /** unbound = 无绑定分组（按普通一条曲线渲染） */
  agent_name: string
  points: AgentActivityPoint[]
}

/** GET /api/agent-activity 响应载荷；series 按 agent_name 升序、窗口内零执行的 agent 不出现 */
export interface AgentActivityResponse {
  window: { from: string; to: string }
  bucket_minutes: number
  series: AgentActivitySeries[]
}
