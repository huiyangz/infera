/**
 * 「统计」页 API client（契约冻结于 L202608251850-1-T01：
 * GET /api/stats?hours=168&tz=<IANA时区> → WorkspaceStatsResponse）。
 * hours 缺省 168（1..720）；tz 缺省取浏览器时区——上游口径：逐小时分桶
 * 按该时区本地小时归桶。错误形态沿用 lib/infera-api 的 ApiError。
 */
import { ApiError } from '@/lib/infera-api'
import type { WorkspaceStatsResponse } from './types'

/** 浏览器时区（IANA 名），Intl 不可用/解析失败时回退 UTC（后端缺省） */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

export async function getWorkspaceStats(
  params: { hours?: number; tz?: string } = {}
): Promise<WorkspaceStatsResponse> {
  const hours = params.hours ?? 168
  const tz = params.tz ?? browserTimeZone()
  const r = await fetch(
    `/api/stats?hours=${hours}&tz=${encodeURIComponent(tz)}`
  )
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`
    )
  }
  return r.json() as Promise<WorkspaceStatsResponse>
}
