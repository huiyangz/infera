/**
 * 「需要决策」API client（契约冻结于 INFERA-108 T01：
 * GET /api/pending-decisions → store.PendingDecision 行数组）。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'
import type { PendingDecisionRow } from './types'

export async function listPendingDecisions(): Promise<PendingDecisionRow[]> {
  const r = await fetch('/api/pending-decisions')
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`
    )
  }
  return r.json() as Promise<PendingDecisionRow[]>
}
