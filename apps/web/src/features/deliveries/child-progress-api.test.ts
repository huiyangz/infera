import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getChildProgress } from '@/lib/infera-api'
import type { ChildProgress } from '@/lib/infera-types'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象（口径同 stage-runs-api）。
 * 浏览器模式下跨模块调用的 stub 返回值不走原型链，用普通对象
 * （ok/status/json）而非 new Response——客户端只依赖这三个成员。
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

afterEach(() => {
  vi.unstubAllGlobals()
})

/** 冻结契约的最小真实形状（server store.ChildProgress 逐字段镜像） */
function progressFixture(): ChildProgress {
  return {
    delivery_id: 'd1',
    active_stage: 2,
    total: 5,
    done: 2,
    in_progress: 1,
    in_review: 1,
    blocked: 0,
    todo: 1,
    cancelled: 0,
    by_status: {
      active: 2,
      queued: 1,
      completed: 2,
      blocked: 0,
      cancelled: 0,
    },
    stages: [
      {
        stage: 1,
        total: 2,
        done: 2,
        in_progress: 0,
        in_review: 0,
        blocked: 0,
        todo: 0,
        cancelled: 0,
        by_status: {
          active: 0,
          queued: 0,
          completed: 2,
          blocked: 0,
          cancelled: 0,
        },
      },
      {
        stage: 2,
        total: 3,
        done: 0,
        in_progress: 1,
        in_review: 1,
        blocked: 0,
        todo: 1,
        cancelled: 0,
        by_status: {
          active: 2,
          queued: 1,
          completed: 0,
          blocked: 0,
          cancelled: 0,
        },
      },
    ],
  }
}

describe('getChildProgress（契约冻结于 L202608260142-1-T01：GET /api/deliveries/{id}/progress）', () => {
  it('GET /api/deliveries/{id}/progress，原样返回冻结形状载荷', async () => {
    const body = progressFixture()
    const { calls } = stubFetch({ body })

    const out = await getChildProgress('d1')

    expect(out).toEqual(body)
    expect(calls).toEqual([{ url: '/api/deliveries/d1/progress', init: undefined }])
  })

  it('交付不存在（404）抛 ApiError(404) 并透传后端 error 文案', async () => {
    stubFetch({ status: 404, body: { error: '交付不存在' } })

    await expect(getChildProgress('nope')).rejects.toMatchObject({
      status: 404,
      message: '交付不存在',
    })
    await expect(getChildProgress('nope')).rejects.toBeInstanceOf(ApiError)
  })
})
