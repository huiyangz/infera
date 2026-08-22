import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import type { Delivery } from '@/lib/infera-types'
import { createProjectRequirement } from './api'

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

function makeDelivery(overrides: Partial<Delivery> = {}): Delivery {
  return {
    id: 'd1',
    project_id: 'p1',
    title: '登录页改版',
    description: '支持手机号登录',
    status: 'queued',
    current_stage: '',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-23T00:00:00Z',
    updated_at: '2026-08-23T00:00:00Z',
    external_issue_id: 'mi-9',
    external_issue_key: 'INFERA-99',
    assignee: 'agent:tech-lead',
    priority: 'high',
    external_synced_at: '2026-08-23T00:00:00Z',
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
    ...overrides,
  }
}

describe('createProjectRequirement（契约冻结于 L202608230412-1-T01：server/internal/api/requirementcreate.go）', () => {
  it('POST /api/projects/{id}/requirements：全字段载荷原样上送，201 返回同步侧 Delivery', async () => {
    const body = makeDelivery()
    const { calls } = stubFetch({ status: 201, body })

    const out = await createProjectRequirement('p1', {
      title: '登录页改版',
      description: '支持手机号登录',
      status: 'todo',
      priority: 'high',
      auto_merge: true,
      agent_id: 'agent-x',
    })

    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/api/projects/p1/requirements')
    expect(calls[0].init?.method).toBe('POST')
    const headers = calls[0].init?.headers as Record<string, string>
    expect(headers['Content-Type']).toBe('application/json')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      title: '登录页改版',
      description: '支持手机号登录',
      status: 'todo',
      priority: 'high',
      auto_merge: true,
      agent_id: 'agent-x',
    })
    expect(out).toEqual(body)
  })

  it('可选字段缺省：载荷只含显式给出的字段（缺省由服务端解析）', async () => {
    const { calls } = stubFetch({ status: 201, body: makeDelivery() })

    await createProjectRequirement('p2', { title: '仅标题' })

    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/api/projects/p2/requirements')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ title: '仅标题' })
  })

  it('非 2xx：抛 ApiError，携带后端 error 文案与 status（400 空标题）', async () => {
    stubFetch({ status: 400, body: { error: 'syncsvc: 输入不合法: 标题不能为空' } })

    const err = await createProjectRequirement('p1', { title: '' }).catch(
      (e: unknown) => e,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(400)
    expect((err as ApiError).message).toContain('标题不能为空')
  })
})
