import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, updateDeliveryDescription } from '@/lib/infera-api'
import type { Delivery } from '@/lib/infera-types'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象（口径同 child-progress-api）。
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

/** 保存成功响应的 200 载荷（与详情 delivery 同形，labels 恒为数组） */
function savedDelivery(): Delivery {
  return {
    id: 'd1',
    project_id: 'p1',
    title: '演示任务',
    description: '## 改后的目标',
    status: 'active',
    current_stage: 'code_gen',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-20T10:00:00Z',
    updated_at: '2026-08-26T10:00:00Z',
    external_issue_id: '',
    external_issue_key: '',
    assignee: '',
    priority: '',
    external_synced_at: null,
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
    labels: [],
  }
}

describe('updateDeliveryDescription（契约冻结于 INFERA-298：PATCH /api/deliveries/{id}/description）', () => {
  it('PATCH 描述端点，body 携带 {"description": …}，原样返回 200 的 Delivery', async () => {
    const body = savedDelivery()
    const { calls } = stubFetch({ body })

    const out = await updateDeliveryDescription('d1', '## 改后的目标')

    expect(out).toEqual(body)
    expect(calls).toEqual([
      {
        url: '/api/deliveries/d1/description',
        init: {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ description: '## 改后的目标' }),
        },
      },
    ])
  })

  it('并发冲突（409）抛 ApiError(409) 并透传后端 error 文案', async () => {
    stubFetch({ status: 409, body: { error: '交付已被并发修改，请刷新后重试' } })

    await expect(
      updateDeliveryDescription('d1', '草稿')
    ).rejects.toMatchObject({
      status: 409,
      message: '交付已被并发修改，请刷新后重试',
    })
    await expect(
      updateDeliveryDescription('d1', '草稿')
    ).rejects.toBeInstanceOf(ApiError)
  })
})
