import { afterEach, describe, expect, it, vi } from 'vitest'
import { listProjectTaskGroups } from '@/lib/infera-api'
import {
  STAGE_META,
  stageLabel,
  type TaskChild,
  type TaskGroupRow,
} from '@/lib/infera-types'

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

function makeChild(overrides: Partial<TaskChild> = {}): TaskChild {
  return {
    id: 'c1',
    title: '子任务',
    stage: 1,
    status: 'active',
    current_stage: 'code_gen',
    pending_gate: '',
    external_issue_id: '',
    external_issue_key: '',
    assignee: '',
    priority: '',
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

function makeGroup(overrides: Partial<TaskGroupRow> = {}): TaskGroupRow {
  return {
    id: 'g1',
    project_id: 'p1',
    title: '父任务',
    description: '',
    status: 'active',
    current_stage: 'spec',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
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
    child_total: 0,
    child_completed: 0,
    stages: [],
    ...overrides,
  }
}

describe('listProjectTaskGroups（契约冻结于 L202608221704-1-T01：server/internal/api/taskgroups.go）', () => {
  it('GET /api/projects/{id}/task-groups，原样返回「父任务 + 子任务按阶段分组」载荷', async () => {
    const body: TaskGroupRow[] = [
      makeGroup(),
      makeGroup({
        id: 'g2',
        title: '同步父任务',
        status: 'queued',
        current_stage: '',
        external_issue_id: 'mi-2',
        external_issue_key: 'INFERA-77',
        child_total: 3,
        child_completed: 1,
        stages: [
          {
            stage: 1,
            tasks: [
              makeChild({
                id: 'c3',
                title: '同步子任务甲',
                status: 'completed',
                current_stage: '',
                external_issue_id: 'mi-3',
                external_issue_key: 'INFERA-78',
              }),
              makeChild({ id: 'c4', title: '同步子任务乙', status: 'queued' }),
            ],
          },
          {
            stage: 2,
            tasks: [
              makeChild({ id: 'c5', title: '第二批子任务', stage: 2 }),
            ],
          },
        ],
      }),
    ]
    const { calls } = stubFetch({ body })

    const out = await listProjectTaskGroups('p1')

    expect(calls).toHaveLength(1)
    expect(calls[0].url).toBe('/api/projects/p1/task-groups')
    expect(calls[0].init).toBeUndefined()
    expect(out).toEqual(body)
  })

  it('空项目返回 []（顶层即数组，不包信封）', async () => {
    stubFetch({ body: [] })
    const out = await listProjectTaskGroups('p2')
    expect(out).toEqual([])
  })
})

describe('口径统一：任务页可见的阶段 label 不出现「需求」', () => {
  it('STAGE_META 全部阶段 label（含 intake）不含「需求」', () => {
    for (const key of Object.keys(STAGE_META)) {
      expect(stageLabel(key), `stage ${key}`).not.toContain('需求')
    }
  })
})
