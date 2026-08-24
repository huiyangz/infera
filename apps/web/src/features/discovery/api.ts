/**
 * 需求发现 API client（契约冻结于 INFERA-225：
 * GET /api/discovery-tasks[?agent=mining|analysis] → discoveryTaskRow 行数组）。
 * agent 省略 = 两类并集；可重复传参显式并集；未知取值后端 400。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'
import type { DiscoveryAgentType, DiscoveryTaskRow } from './types'

export async function listDiscoveryTasks(
  agents?: DiscoveryAgentType[]
): Promise<DiscoveryTaskRow[]> {
  // 空数组等价省略：不拼查询串，后端按两类并集取回
  const q = agents?.length
    ? `?${agents.map((a) => `agent=${a}`).join('&')}`
    : ''
  const r = await fetch(`/api/discovery-tasks${q}`)
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`
    )
  }
  return r.json() as Promise<DiscoveryTaskRow[]>
}
