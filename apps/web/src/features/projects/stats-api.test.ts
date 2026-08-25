import { afterEach, describe, expect, it, vi } from 'vitest'
import { getProjectStats } from '@/lib/infera-api'
import type { RequirementStats } from '@/lib/infera-types'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象。
 * 浏览器模式下跨模块调用的 stub 返回值不走原型链，用普通对象
 * （ok/status/json）而非 new Response——客户端只依赖这三个成员。
 */
function stubFetch(
  replies:
    | { status?: number; body: unknown }
    | Array<{ status?: number; body: unknown }>
) {
  const calls: Array<{ url: string; init?: RequestInit }> = []
  const script = Array.isArray(replies) ? replies : [replies]
  let i = 0
  const fetchMock = vi.fn(async (url: string | URL, init?: RequestInit) => {
    calls.push({ url: String(url), init })
    const r = script[Math.min(i++, script.length - 1)]
    const status = r.status ?? 200
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => r.body,
    } as Response
  })
  vi.stubGlobal('fetch', fetchMock)
  return { calls, fetchMock }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('getProjectStats（契约冻结于 INFERA-108 T01：server/internal/store store.RequirementStats）', () => {
  it('GET /api/projects/{id}/stats，原样返回冻结形状载荷', async () => {
    const body: RequirementStats = {
      project_id: 'p1',
      requirement_total: 7,
      by_status: { active: 2, queued: 1, completed: 3, blocked: 1, cancelled: 1 },
      pending_decisions: 2,
      delivered: 3,
      last_synced_at: '2026-08-22T03:00:05Z',
    }
    const { calls } = stubFetch({ body })

    const out = await getProjectStats('p1')

    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/api/projects/p1/stats')
    expect(calls[0].init).toBeUndefined()
    expect(out).toEqual(body)
  })

  it('last_synced_at 为 JSON null 时保持 null（从未同步口径）', async () => {
    stubFetch({
      body: {
        project_id: 'p2',
        requirement_total: 0,
        by_status: { active: 0, queued: 0, completed: 0, blocked: 0, cancelled: 0 },
        pending_decisions: 0,
        delivered: 0,
        last_synced_at: null,
      },
    })

    const out = await getProjectStats('p2')

    expect(out.last_synced_at).toBeNull()
  })
})
