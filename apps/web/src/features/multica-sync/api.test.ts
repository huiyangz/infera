import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import { getMulticaSyncStatus, triggerMulticaSync } from './api'

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

const RESULT = {
  started_at: '2026-08-22T03:00:00Z',
  finished_at: '2026-08-22T03:00:05Z',
  projects_imported: 2,
  issues_imported: 5,
  issues_skipped: 1,
  skips: [
    { multica_issue_id: 'm9', issue_key: 'AUTO-1', reason: 'smoke' },
  ],
  error: '',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('multica 同步 API client（契约冻结于 T03：server/internal/api/multicasync.go）', () => {
  it('triggerMulticaSync POST /api/multica/sync，返回本轮 Result（含 skips 明细）', async () => {
    const { calls } = stubFetch({ body: RESULT })
    const out = await triggerMulticaSync()
    expect(calls[0]?.url).toBe('/api/multica/sync')
    expect(calls[0]?.init?.method).toBe('POST')
    expect(out.projects_imported).toBe(2)
    expect(out.issues_imported).toBe(5)
    expect(out.skips?.[0]?.reason).toBe('smoke')
  })

  it('getMulticaSyncStatus GET /api/multica/sync，last 为 null = 从未同步', async () => {
    const { calls } = stubFetch({ body: { running: false, last: null } })
    const out = await getMulticaSyncStatus()
    expect(calls[0]).toEqual({ url: '/api/multica/sync', init: undefined })
    expect(out.running).toBe(false)
    expect(out.last).toBeNull()
  })

  it('409/502/503 抛 ApiError 并透传后端 error 文案（409 运行中 / 502 上游失败 / 503 未装配）', async () => {
    stubFetch({
      status: 503,
      body: {
        error:
          'multica 同步未装配（需配置 MULTICA_SERVER_URL / MULTICA_TOKEN / MULTICA_WORKSPACE_ID）',
      },
    })
    await expect(triggerMulticaSync()).rejects.toSatisfy((e: unknown) => {
      const err = e as ApiError
      return (
        err instanceof ApiError &&
        err.status === 503 &&
        err.message.includes('未装配')
      )
    })
    stubFetch({ status: 409, body: { error: '已有同步在进行，稍后用 GET 查看结果' } })
    await expect(triggerMulticaSync()).rejects.toSatisfy((e: unknown) => {
      const err = e as ApiError
      return err instanceof ApiError && err.status === 409
    })
  })
})
