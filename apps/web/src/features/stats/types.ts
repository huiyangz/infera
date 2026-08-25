/** GET /api/stats 响应载荷（L202608251850-1-T01 冻结契约，见
 *  server/internal/api/workspacestats.go —— 形状不得静默变更）。 */

export interface StatsWindow {
  from: string
  to: string
}

/** 任务状态分布：全量快照（当前态，不受 hours 窗口影响）。
 *  五类归并对齐 Multica 口径：done←completed、in_progress←active、
 *  todo←queued+blocked、cancelled←cancelled；by_status 为 infera 原始
 *  状态计数（恒含五键，无行为 0）。 */
export interface WorkspaceTaskStatus {
  total: number
  done: number
  in_progress: number
  todo: number
  cancelled: number
  by_status: Record<string, number>
}

/** 窗口内执行维度基础统计：runs_total 计窗口内全部 stage_runs（attempt
 *  各计一次、不分状态，含 in-flight）；duration_ms_total 只累计已收尾
 *  （finished_at 非空）行的毫秒时长，running 不计。 */
export interface WorkspaceExecution {
  runs_total: number
  running: number
  done: number
  failed: number
  duration_ms_total: number
}

/** 执行时段分布单桶：hour 0..23 为 started_at 换算到查询时区后的本地
 *  小时；duration_ms 为该小时启动的已收尾执行累计耗时——整段计入起始
 *  桶，跨小时收尾不拆分。 */
export interface WorkspaceHourBucket {
  hour: number
  runs: number
  duration_ms: number
}

export interface WorkspaceStatsResponse {
  window: StatsWindow
  timezone: string
  task_status: WorkspaceTaskStatus
  execution: WorkspaceExecution
  hourly: WorkspaceHourBucket[]
}
