import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import { listPendingDecisions } from './api'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象（与 requirements/api.test
 * 同构——浏览器模式 stub 不走原型链，普通对象即可）。
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

describe('decisions API client（契约冻结于 INFERA-108 T01）', () => {
  it('listPendingDecisions GET /api/pending-decisions，按行原样返回', async () => {
    const rows = [
      {
        id: '9f2f9f34-1a2b-4c3d-8e9f-000000000001',
        project_id: 'd2836d4e-3b90-4808-adf4-c30be224eb1e',
        project_name: '自动闭环',
        title: '示例：卡在规格审批的需求',
        status: 'active',
        pending_gate: 'spec_approval',
        current_stage: 'spec',
        external_issue_key: 'INFERA-108',
        assignee: 'agent:7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
        priority: '',
        source: 'web', // INFERA-267 加法扩展：''=无来源/不可解析
        created_at: '2026-08-22T05:00:00Z',
        updated_at: '2026-08-22T05:10:00Z',
      },
    ]
    const { calls } = stubFetch({ body: rows })
    const out = await listPendingDecisions()
    expect(calls[0].url).toBe('/api/pending-decisions')
    expect(calls[0].init?.method).toBeUndefined() // GET 无显式 method
    expect(out).toEqual(rows)
  })

  it('空结果返回 []（后端冻结口径：nil 也序列化为 []）', async () => {
    stubFetch({ body: [] })
    await expect(listPendingDecisions()).resolves.toEqual([])
  })

  it('非 2xx 抛 ApiError（携带后端 error 文案）', async () => {
    stubFetch({ status: 500, body: { error: '读取待决策列表失败' } })
    await expect(listPendingDecisions()).rejects.toThrow(ApiError)
    await expect(listPendingDecisions()).rejects.toThrow('读取待决策列表失败')
  })
})
