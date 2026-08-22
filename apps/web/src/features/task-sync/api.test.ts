import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import { getTaskSyncStatus, triggerTaskSync } from './api'

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
    { external_issue_id: 'm9', issue_key: 'AUTO-1', reason: 'smoke' },
  ],
  error: '',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('任务同步 API client（契约冻结于 INFERA-169：server/internal/api/tasksync.go）', () => {
  it('triggerTaskSync POST /api/task-sync，返回本轮 Result（含 skips 明细）', async () => {
    const { calls } = stubFetch({ body: RESULT })
    const out = await triggerTaskSync()
    expect(calls[0]?.url).toBe('/api/task-sync')
    expect(calls[0]?.init?.method).toBe('POST')
    expect(out.projects_imported).toBe(2)
    expect(out.issues_imported).toBe(5)
    expect(out.skips?.[0]?.reason).toBe('smoke')
    expect(out.skips?.[0]?.external_issue_id).toBe('m9')
  })

  it('getTaskSyncStatus GET /api/task-sync/status，返回 {lastSyncAt, status, error}', async () => {
    const { calls } = stubFetch({
      body: { lastSyncAt: '2026-08-22T03:00:05Z', status: 'success', error: '' },
    })
    const out = await getTaskSyncStatus()
    expect(calls[0]).toEqual({ url: '/api/task-sync/status', init: undefined })
    expect(out.lastSyncAt).toBe('2026-08-22T03:00:05Z')
    expect(out.status).toBe('success')
    expect(out.error).toBe('')
  })

  it('lastSyncAt=null + idle 表示从未完成过同步', async () => {
    stubFetch({ body: { lastSyncAt: null, status: 'idle', error: '' } })
    const out = await getTaskSyncStatus()
    expect(out.lastSyncAt).toBeNull()
    expect(out.status).toBe('idle')
  })

  it('409/502/503 抛 ApiError 并透传后端 error 文案（409 运行中 / 502 上游失败 / 503 未装配）', async () => {
    stubFetch({
      status: 503,
      body: {
        error:
          '任务同步未装配（需配置 TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）',
      },
    })
    await expect(triggerTaskSync()).rejects.toSatisfy((e: unknown) => {
      const err = e as ApiError
      return (
        err instanceof ApiError &&
        err.status === 503 &&
        err.message.includes('未装配')
      )
    })
    stubFetch({ status: 409, body: { error: '已有同步在进行，稍后用 GET 查看结果' } })
    await expect(triggerTaskSync()).rejects.toSatisfy((e: unknown) => {
      const err = e as ApiError
      return err instanceof ApiError && err.status === 409
    })
  })
})
