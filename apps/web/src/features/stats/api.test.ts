import { afterEach, describe, expect, it, vi } from 'vitest'
import { browserTimeZone, getWorkspaceStats } from './api'
import type { WorkspaceStatsResponse } from './types'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象（与 agent-activity/api.test
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

/** L202608251850-1-T01 冻结契约的最小真实载荷（24 桶补零） */
function payload(): WorkspaceStatsResponse {
  const hourly = Array.from({ length: 24 }, (_, hour) => ({
    hour,
    runs: hour === 2 ? 3 : 0,
    duration_ms: hour === 2 ? 5_400_000 : 0,
  }))
  return {
    window: { from: '2026-08-18T12:00:00Z', to: '2026-08-25T12:00:00Z' },
    timezone: 'Asia/Shanghai',
    task_status: {
      total: 12,
      done: 5,
      in_progress: 3,
      todo: 2,
      cancelled: 2,
      by_status: { active: 3, queued: 1, blocked: 1, completed: 5, cancelled: 2 },
    },
    execution: { runs_total: 9, running: 1, done: 7, failed: 1, duration_ms_total: 45_340_000 },
    hourly,
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('getWorkspaceStats 统计聚合 API client（GET /api/stats）', () => {
  it('缺省参数：hours=168 + 浏览器时区（上游口径：分桶按浏览器本地小时）', async () => {
    const { calls } = stubFetch({ body: payload() })

    await getWorkspaceStats()

    expect(calls[0].url).toBe(
      `/api/stats?hours=168&tz=${encodeURIComponent(browserTimeZone())}`
    )
  })

  it('显式参数：hours 与 tz 原样透传（tz 含 / 需 URL 编码）', async () => {
    const { calls } = stubFetch({ body: payload() })

    await getWorkspaceStats({ hours: 720, tz: 'Asia/Shanghai' })

    expect(calls[0].url).toBe(
      '/api/stats?hours=720&tz=Asia%2FShanghai'
    )
  })

  it('非 2xx：抛 ApiError（status + 后端 error 文案）', async () => {
    stubFetch({ status: 400, body: { error: '参数不合法：tz 需为 IANA 时区名（如 Asia/Shanghai）' } })

    await expect(getWorkspaceStats({ tz: 'Not/AZone' })).rejects.toMatchObject({
      name: 'ApiError',
      status: 400,
      message: '参数不合法：tz 需为 IANA 时区名（如 Asia/Shanghai）',
    })
  })

  it('响应体原样解析为 WorkspaceStatsResponse（不改形状）', async () => {
    const body = payload()
    stubFetch({ body })

    await expect(getWorkspaceStats()).resolves.toEqual(body)
  })
})

describe('browserTimeZone 浏览器时区解析', () => {
  it('返回非空字符串（Intl 解析失败时回退 UTC）', () => {
    const tz = browserTimeZone()
    expect(typeof tz).toBe('string')
    expect(tz.length).toBeGreaterThan(0)
  })

  it('Intl 抛错/给空时回退 UTC', () => {
    const spy = vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => {
      throw new Error('intl unavailable')
    })
    expect(browserTimeZone()).toBe('UTC')
    spy.mockRestore()
  })
})
