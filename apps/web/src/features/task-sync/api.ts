/**
 * 任务同步 API client（契约冻结于 INFERA-169：server/internal/api/tasksync.go）：
 * POST /api/task-sync 触发并返回本轮 Result（409 运行中 / 502 上游失败 /
 * 503 未装配）；GET /api/task-sync/status 返回 {lastSyncAt, status, error}
 * （lastSyncAt=null 即从未完成过同步；status idle|running|success|error）。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'

/** 一条被跳过的 issue（不落库，计数可审计；reason: smoke|no_project|parent_cycle） */
interface TaskSyncSkip {
  external_issue_id: string
  issue_key: string
  reason: string
}

/** 一轮同步的结果（syncsvc.Result 的前端形态；error 非空 = 本轮中途失败） */
export interface TaskSyncResult {
  started_at: string
  finished_at: string
  projects_imported: number
  issues_imported: number
  issues_skipped: number
  skips: TaskSyncSkip[] | null
  error: string
}

/** 自动同步状态面（INFERA-169 冻结契约，字段不得增改） */
export interface TaskSyncStatus {
  /** 最近一轮完成时间；null = 从未完成过 */
  lastSyncAt: string | null
  status: 'idle' | 'running' | 'success' | 'error'
  /** 最近完成一轮的失败原因；'' = 无 */
  error: string
}

async function json<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`
    )
  }
  return r.json() as Promise<T>
}

/** 触发一轮全量同步；同步执行，完成即回（运行中 → 409 ApiError） */
export async function triggerTaskSync(): Promise<TaskSyncResult> {
  return json<TaskSyncResult>(
    await fetch('/api/task-sync', { method: 'POST' })
  )
}

/** 自动同步状态（未装配 → 503 ApiError） */
export async function getTaskSyncStatus(): Promise<TaskSyncStatus> {
  return json<TaskSyncStatus>(await fetch('/api/task-sync/status'))
}
