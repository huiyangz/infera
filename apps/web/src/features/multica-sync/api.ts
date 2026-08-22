/**
 * Multica 同步 API client（契约冻结于 T03：server/internal/api/multicasync.go）：
 * POST /api/multica/sync 触发并返回本轮 Result（409 运行中 / 502 上游失败 /
 * 503 未装配）；GET /api/multica/sync 返回 {running, last}（last=null = 从未
 * 同步——同步结果仅进程内存，服务重启即空，前端按「从未同步」处理）。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'

/** 一条被跳过的 issue（不落库，计数可审计；reason: smoke|no_project|parent_cycle） */
export interface MulticaSyncSkip {
  multica_issue_id: string
  issue_key: string
  reason: string
}

/** 一轮同步的结果（syncsvc.Result 的前端形态；error 非空 = 本轮中途失败） */
export interface MulticaSyncResult {
  started_at: string
  finished_at: string
  projects_imported: number
  issues_imported: number
  issues_skipped: number
  skips: MulticaSyncSkip[] | null
  error: string
}

/** GET /api/multica/sync 的载荷 */
export interface MulticaSyncStatus {
  running: boolean
  last: MulticaSyncResult | null
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
export async function triggerMulticaSync(): Promise<MulticaSyncResult> {
  return json<MulticaSyncResult>(
    await fetch('/api/multica/sync', { method: 'POST' })
  )
}

/** 运行中标志 + 最近一轮结果（未装配 → 503 ApiError） */
export async function getMulticaSyncStatus(): Promise<MulticaSyncStatus> {
  return json<MulticaSyncStatus>(await fetch('/api/multica/sync'))
}
