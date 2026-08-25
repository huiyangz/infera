import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import { getAgentActivity } from './api'
import type { AgentActivityResponse } from './types'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象（与 decisions/api.test
 * 同构——浏览器模式 stub 不走原型链，普通对象即可）。body 省略 = 响应体
 * 不是合法 JSON（json() 抛错，走 catch 兜底分支）。
 */
function stubFetch(
  replies:
    | { status?: number; body?: unknown }
    | Array<{ status?: number; body?: unknown }>
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
      json: async () => {
        if (r.body === undefined) {
          throw new SyntaxError('Unexpected token < in JSON')
        }
        return r.body
      },
    } as Response
  })
  vi.stubGlobal('fetch', fetchMock)
  return { calls, fetchMock }
}

/** INFERA-253 冻结契约的最小真实载荷：两条曲线 + unbound 分组 */
function payload(over: Partial<AgentActivityResponse>): AgentActivityResponse {
  return {
    window: { from: '2026-08-25T04:00:00Z', to: '2026-08-25T08:00:00Z' },
    bucket_minutes: 30,
    series: [
      {
        agent_id: '7bc775bc-db05-47bc-8f45-5c3baecc3fe3',
        agent_name: 'SDD',
        points: [
          { t: '2026-08-25T04:00:00Z', count: 1 },
          { t: '2026-08-25T04:30:00Z', count: 0 },
        ],
      },
      {
        agent_id: '',
        agent_name: 'unbound',
        points: [
          { t: '2026-08-25T04:00:00Z', count: 2 },
          { t: '2026-08-25T04:30:00Z', count: 0 },
        ],
      },
    ],
    ...over,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('agent-activity API client（契约冻结于 INFERA-253：server/internal/api handleAgentActivity）', () => {
  it('getAgentActivity GET /api/agent-activity 带 hours/bucket_minutes，原样返回冻结形状载荷', async () => {
    const body = payload({})
    const { calls } = stubFetch({ body })

    const out = await getAgentActivity({ hours: 24, bucketMinutes: 30 })

    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/api/agent-activity?hours=24&bucket_minutes=30')
    expect(calls[0].init).toBeUndefined()
    expect(out).toEqual(body)
  })

  it('窗口参数缺省 = L1 接口默认（24h / 30 分钟桶），query 仍显式携带', async () => {
    const { calls } = stubFetch({ body: payload({ series: [] }) })

    await getAgentActivity()

    expect(calls[0].url).toBe('/api/agent-activity?hours=24&bucket_minutes=30')
  })

  it('窗口切换只改 hours（12h / 6h 复用同一入口，不自造平行请求）', async () => {
    const { calls } = stubFetch({ body: payload({ series: [] }) })

    await getAgentActivity({ hours: 12 })
    await getAgentActivity({ hours: 6, bucketMinutes: 30 })

    expect(calls[0].url).toBe('/api/agent-activity?hours=12&bucket_minutes=30')
    expect(calls[1].url).toBe('/api/agent-activity?hours=6&bucket_minutes=30')
  })

  it('空窗口 series 为空数组（200，非错误）——空态判定输入', async () => {
    stubFetch({ body: payload({ series: [] }) })

    const out = await getAgentActivity({ hours: 24 })

    expect(out.series).toEqual([])
  })

  it('非 2xx 抛 ApiError：携带后端 error 文案与 status', async () => {
    stubFetch({ status: 400, body: { error: '参数不合法：hours 需为 1..168 的整数' } })

    const err = await getAgentActivity({ hours: 0 }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(400)
    expect((err as ApiError).message).toBe('参数不合法：hours 需为 1..168 的整数')
  })

  it('非 JSON 错误响应体兜底为 HTTP status 文案', async () => {
    stubFetch({ status: 500 })

    const err = await getAgentActivity({ hours: 24 }).catch((e: unknown) => e)

    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).message).toBe('HTTP 500')
  })
})
