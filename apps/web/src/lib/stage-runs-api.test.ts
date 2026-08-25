import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getProjectStageRuns } from '@/lib/infera-api'
import type { ProjectStageRuns } from '@/lib/infera-types'

/**
 * fetch 替身：记录调用并按脚本返回 response 形对象。
 * 浏览器模式下跨模块调用的 stub 返回值不走原型链，用普通对象
 * （ok/status/json）而非 new Response——客户端只依赖这三个成员。
 * body 省略 = 响应体不是合法 JSON（json() 抛错，走 catch 兜底分支）。
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

describe('getProjectStageRuns（契约冻结于 INFERA-234 T01：server/internal/store store.ProjectStageRuns）', () => {
  it('GET /api/projects/{id}/stage-runs，原样返回冻结形状载荷', async () => {
    const body: ProjectStageRuns = {
      project_id: 'p1',
      runs: [
        {
          id: 'sr-2',
          delivery_id: 'd-1',
          title: '补一个设置页',
          external_issue_key: 'INFERA-79',
          stage: 'code_gen',
          attempt: 2,
          status: 'done',
          agent_name: 'coder',
          started_at: '2026-08-22T03:00:00.125Z',
          finished_at: '2026-08-22T03:04:12.5Z',
          duration_ms: 252375,
        },
        {
          id: 'sr-1',
          delivery_id: 'd-1',
          title: '补一个设置页',
          external_issue_key: 'INFERA-79',
          stage: 'unit_test',
          attempt: 1,
          status: 'running',
          agent_name: null,
          started_at: '2026-08-22T03:05:00Z',
          finished_at: null,
          duration_ms: null,
        },
      ],
      by_stage: [
        { stage: 'code_gen', total: 1, done: 1, failed: 0, running: 0, avg_ms: 252375, p95_ms: 252375 },
      ],
    }
    const { calls } = stubFetch({ body })

    const out = await getProjectStageRuns('p1')

    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/api/projects/p1/stage-runs')
    expect(calls[0].init).toBeUndefined()
    expect(out).toEqual(body)
  })

  it('可空字段保持 JSON null：未绑定 agent 与 running 未收尾（agent_name / finished_at / duration_ms）', async () => {
    stubFetch({
      body: {
        project_id: 'p2',
        runs: [
          {
            id: 'sr-9',
            delivery_id: 'd-2',
            title: '本地需求',
            external_issue_key: '',
            stage: 'spec_approval',
            attempt: 1,
            status: 'running',
            agent_name: null,
            started_at: '2026-08-22T03:00:00Z',
            finished_at: null,
            duration_ms: null,
          },
        ],
        by_stage: [
          // 门禁/无已收尾运行：avg_ms / p95_ms 为 0（非 null）
          { stage: 'spec_approval', total: 1, done: 0, failed: 0, running: 1, avg_ms: 0, p95_ms: 0 },
        ],
      },
    })

    const out = await getProjectStageRuns('p2')

    const run = out.runs[0]
    expect(run.agent_name).toBeNull()
    expect(run.finished_at).toBeNull()
    expect(run.duration_ms).toBeNull()
  })

  it('空项目 runs / by_stage 为空数组（非 null）', async () => {
    stubFetch({
      body: { project_id: 'p3', runs: [], by_stage: [] },
    })

    const out = await getProjectStageRuns('p3')

    expect(out.runs).toEqual([])
    expect(out.by_stage).toEqual([])
  })

  it('by_stage 原样透传不重排（后端按 stage 字典序返回，阶段序重排是 UI 层职责）', async () => {
    // 故意给成字典序而非流水线阶段序（spec < code_gen < tasks_approval）：
    // 字典序在这里恰好与阶段序不同，能暴露任何隐含的排序假设。
    const byStage = [
      { stage: 'spec', total: 2, done: 2, failed: 0, running: 0, avg_ms: 1000, p95_ms: 1200 },
      { stage: 'code_gen', total: 3, done: 2, failed: 1, running: 0, avg_ms: 2000, p95_ms: 2500 },
      { stage: 'tasks_approval', total: 1, done: 1, failed: 0, running: 0, avg_ms: 0, p95_ms: 0 },
    ]
    stubFetch({
      body: { project_id: 'p4', runs: [], by_stage: byStage },
    })

    const out = await getProjectStageRuns('p4')

    expect(out.by_stage).toEqual(byStage)
    expect(out.by_stage.map((s) => s.stage)).toEqual([
      'spec',
      'code_gen',
      'tasks_approval',
    ])
  })

  it('项目不存在（404）抛 ApiError，携带 status 与后端 error 文案', async () => {
    stubFetch({ status: 404, body: { error: '项目不存在' } })

    await expect(getProjectStageRuns('nope')).rejects.toSatisfy(
      (e: unknown) => {
        const err = e as ApiError
        return (
          err instanceof ApiError &&
          err.status === 404 &&
          err.message === '项目不存在'
        )
      }
    )
  })

  it('响应体非 JSON 时回退 HTTP <status> 文案', async () => {
    stubFetch({ status: 500 })

    await expect(getProjectStageRuns('p5')).rejects.toSatisfy(
      (e: unknown) => {
        const err = e as ApiError
        return err instanceof ApiError && err.message === 'HTTP 500'
      }
    )
  })
})
