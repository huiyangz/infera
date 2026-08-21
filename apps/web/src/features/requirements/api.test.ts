import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/infera-api'
import {
  approveCard,
  createRequirement,
  decideCard,
  getMergePolicy,
  getRequirement,
  getRequirementPRReview,
  listRequirementAudit,
  listRequirements,
  mergeCard,
  rejectCard,
  reworkCard,
  setMergePolicy,
} from './api'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象。
 * 浏览器模式下跨模块调用的 stub 返回值不走原型链，用普通对象
 * （ok/status/json）而非 new Response——客户端只依赖这三个成员。
 */
function stubFetch(
  replies: { status?: number; body: unknown } | Array<{ status?: number; body: unknown }>
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

const REQ = {
  id: 'r1',
  title: '登录页适配深色模式',
  description: '描述',
  acceptance_criteria: 'AC',
  source: 'web',
  priority: 'P1',
  acceptors: ['张三'],
  multica_issue_id: 'm1',
  multica_issue_key: 'INFERA-31',
  multica_issue_url: 'http://m/issues/m1',
  pr_url: 'https://github.com/acme/repo/pull/7',
  node: 'in_review',
  created_at: '2026-08-21T00:00:00Z',
  updated_at: '2026-08-21T01:00:00Z',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('requirements API client（契约冻结于 T05）', () => {
  it('createRequirement POST /api/requirements，body 携带全部业务字段', async () => {
    const { calls } = stubFetch({ status: 201, body: REQ })
    const out = await createRequirement({
      title: '登录页适配深色模式',
      description: '描述',
      acceptance_criteria: 'AC',
      source: 'web',
      priority: 'P1',
      acceptors: ['张三'],
    })
    expect(calls[0].url).toBe('/api/requirements')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      title: '登录页适配深色模式',
      description: '描述',
      acceptance_criteria: 'AC',
      source: 'web',
      priority: 'P1',
      acceptors: ['张三'],
    })
    expect(out.id).toBe('r1')
  })

  it('listRequirements GET /api/requirements，空列表返回 []', async () => {
    const { calls } = stubFetch({ body: [] })
    const out = await listRequirements()
    expect(calls[0]).toEqual({ url: '/api/requirements', init: undefined })
    expect(out).toEqual([])
  })

  it('getRequirement GET /api/requirements/{id}，pending_cards 直传', async () => {
    const { calls } = stubFetch({
      body: {
        ...REQ,
        pending_cards: [
          {
            id: 'c1',
            requirement_id: 'r1',
            kind: 'approval',
            status: 'pending',
            payload: '待批准：计划正文',
            comment_id: 'cm1',
            created_at: '2026-08-21T02:00:00Z',
            resolved_at: null,
          },
        ],
      },
    })
    const out = await getRequirement('r1')
    expect(calls[0].url).toBe('/api/requirements/r1')
    expect(out.pending_cards[0]?.kind).toBe('approval')
    expect(out.pending_cards[0]?.resolved_at).toBeNull()
  })

  it('卡片动作各打各的路径与 body', async () => {
    const { calls } = stubFetch({ body: { ok: true } })
    await approveCard('r1', 'c1')
    await rejectCard('r1', 'c1', '理由不够充分')
    await decideCard('r1', 'c1', 'custom', '自定义回复文本')
    await mergeCard('r1', 'c1')
    await reworkCard('r1', 'c1', '返工反馈')
    const base = '/api/requirements/r1/cards/c1/'
    expect(calls[0]?.url).toBe(base + 'approve')
    expect(calls[1]?.url).toBe(base + 'reject')
    expect(JSON.parse(String(calls[1]?.init?.body))).toEqual({ feedback: '理由不够充分' })
    expect(calls[2]?.url).toBe(base + 'decide')
    expect(JSON.parse(String(calls[2]?.init?.body))).toEqual({
      choice: 'custom',
      text: '自定义回复文本',
    })
    expect(calls[3]?.url).toBe(base + 'merge')
    expect(calls[4]?.url).toBe(base + 'rework')
    expect(JSON.parse(String(calls[4]?.init?.body))).toEqual({ feedback: '返工反馈' })
  })

  it('mergeCard 返回 MergeResult；audit 返回条目数组', async () => {
    stubFetch([
      { body: { merged: true, sha: 'abc123', message: '' } },
      { body: [{ id: 'a1', requirement_id: 'r1', actor: 'user', action: 'approve', detail: 'approved', at: '2026-08-21T03:00:00Z' }] },
    ])
    const res = await mergeCard('r1', 'c1')
    expect(res.merged).toBe(true)
    expect(res.sha).toBe('abc123')
    const audit = await listRequirementAudit('r1')
    expect(audit[0]?.action).toBe('approve')
  })

  it('合并策略 GET/PUT /api/projects/{id}/merge-policy，body 字段名冻结', async () => {
    const { calls } = stubFetch([
      { body: { mode: 'threshold', diff_line_threshold: 200 } },
      { body: { mode: 'manual', diff_line_threshold: 0 } },
    ])
    const got = await getMergePolicy('p1')
    expect(calls[0]?.url).toBe('/api/projects/p1/merge-policy')
    expect(got.mode).toBe('threshold')
    expect(got.diff_line_threshold).toBe(200)
    const put = await setMergePolicy('p1', { mode: 'manual', diff_line_threshold: 0 })
    expect(calls[1]?.init?.method).toBe('PUT')
    expect(JSON.parse(String(calls[1]?.init?.body))).toEqual({
      mode: 'manual',
      diff_line_threshold: 0,
    })
    expect(put.mode).toBe('manual')
  })

  it('getRequirementPRReview GET /api/requirements/{id}/pr-review，直传行级评论与 diff 概要', async () => {
    const { calls } = stubFetch({
      body: {
        pr_url: 'https://github.com/acme/repo/pull/7',
        comments: [
          {
            id: 11,
            path: 'server/main.go',
            line: 42,
            original_line: 42,
            side: 'RIGHT',
            body: '这里缺超时控制',
            author: 'reviewer-bot',
            in_reply_to_id: 0,
            created_at: '2026-08-21T03:00:00Z',
          },
        ],
        diff: { files: 4, additions: 120, deletions: 8, changes: 128 },
      },
    })
    const out = await getRequirementPRReview('r1')
    expect(calls[0]).toEqual({ url: '/api/requirements/r1/pr-review', init: undefined })
    expect(out.pr_url).toBe('https://github.com/acme/repo/pull/7')
    expect(out.comments[0]?.path).toBe('server/main.go')
    expect(out.comments[0]?.author).toBe('reviewer-bot')
    expect(out.diff).toEqual({ files: 4, additions: 120, deletions: 8, changes: 128 })
  })

  it('非 2xx 抛 ApiError 且透传后端 error 文案', async () => {
    stubFetch({ status: 409, body: { error: 'PR 当前不可合并（状态或分支保护阻止），请稍后重试' } })
    await expect(mergeCard('r1', 'c1')).rejects.toSatisfy((e: unknown) => {
      const err = e as ApiError
      return (
        err instanceof ApiError &&
        err.status === 409 &&
        err.message.includes('不可合并')
      )
    })
  })
})
