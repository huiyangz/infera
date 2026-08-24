import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import { listDiscoveryTasks } from './api'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象（与 decisions/api.test
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

describe('discovery API client（契约冻结于 INFERA-225：GET /api/discovery-tasks）', () => {
  it('省略参数 = 两类并集：裸 GET /api/discovery-tasks，按行原样返回', async () => {
    const rows = [
      {
        id: 'd-1',
        title: '情报：支付渠道调研',
        agent_types: ['mining'],
        project_name: '自动闭环',
      },
    ]
    const { calls } = stubFetch({ body: rows })
    const out = await listDiscoveryTasks()
    expect(calls[0].url).toBe('/api/discovery-tasks')
    expect(calls[0].init?.method).toBeUndefined() // GET 无显式 method
    expect(out).toEqual(rows)
  })

  it('传 agents = 可重复 agent 参数显式并集（mining+analysis 两参数）', async () => {
    const { calls } = stubFetch({ body: [] })
    await listDiscoveryTasks(['mining', 'analysis'])
    expect(calls[0].url).toBe('/api/discovery-tasks?agent=mining&agent=analysis')
  })

  it('空数组等价省略：不拼查询串', async () => {
    const { calls } = stubFetch({ body: [] })
    await listDiscoveryTasks([])
    expect(calls[0].url).toBe('/api/discovery-tasks')
  })

  it('非 2xx 抛 ApiError（携带后端 error 文案）', async () => {
    stubFetch({ status: 400, body: { error: 'agent 只支持 mining|analysis' } })
    await expect(listDiscoveryTasks()).rejects.toThrow(ApiError)
    await expect(listDiscoveryTasks()).rejects.toThrow(
      'agent 只支持 mining|analysis'
    )
  })
})
