/**
 * 「Agent 执行时序」API client（契约冻结于 INFERA-253：
 * GET /api/agent-activity → AgentActivityResponse）。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'
import type { AgentActivityResponse } from './types'

/** 缺省窗口 = L1 接口默认参数：最近 24h、30 分钟桶 */
export async function getAgentActivity(
  params: { hours?: number; bucketMinutes?: number } = {}
): Promise<AgentActivityResponse> {
  const hours = params.hours ?? 24
  const bucketMinutes = params.bucketMinutes ?? 30
  const r = await fetch(
    `/api/agent-activity?hours=${hours}&bucket_minutes=${bucketMinutes}`
  )
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`
    )
  }
  return r.json() as Promise<AgentActivityResponse>
}
