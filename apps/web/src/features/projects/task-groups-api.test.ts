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
    labels: [],
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
    // 与 server 冻结键集一致的响应样本（store.Delivery 内联全字段）
    base_commit: '',
    reject_reason: '',
    workspace_ready: false,
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

// —— 左侧列表契约冻结（INFERA-228 / L202608241931-1-T01）：唯一数据源
// GET /api/projects/{id}/task-groups 的响应须完整支撑父子层级 / stage / status
// 三类信息。第 2 层（project-tasks.tsx 左侧列表）只读本块对齐的字段。 ——

describe('左侧列表契约（INFERA-228）：父子层级 / stage / status 齐备', () => {
  /** 与 server 响应同构的契约样本：父任务（两阶段子任务）+ 无子独立任务 */
  function contractBody(): TaskGroupRow[] {
    return [
      makeGroup({
        id: 'g9',
        title: '父任务',
        status: 'active',
        current_stage: 'code_gen',
        base_commit: 'abc1204',
        workspace_ready: true,
        child_total: 3,
        child_completed: 1,
        stages: [
          {
            stage: 1,
            tasks: [
              makeChild({ id: 'c1', title: '子任务甲', stage: 1, status: 'completed', current_stage: 'unit_test' }),
              makeChild({ id: 'c2', title: '子任务乙', stage: 1, status: 'active', current_stage: 'code_gen' }),
            ],
          },
          {
            stage: 2,
            tasks: [makeChild({ id: 'c3', title: '子任务丙', stage: 2, status: 'blocked' })],
          },
        ],
      }),
      makeGroup({ id: 'g10', title: '独立任务', status: 'queued', current_stage: '' }),
    ]
  }

  it('每个任务项可取 id/title/status；顶层行即父任务（parent_id 空），子任务嵌于其 stages 下', async () => {
    stubFetch({ body: contractBody() })
    const rows = await listProjectTaskGroups('p9')

    // 左侧列表可派生的全部任务项：顶层父行 + 各阶段组内子行
    const items: Array<{
      id: string
      title: string
      status: string
      stage: number | null
      parentId: string | null
    }> = []
    for (const g of rows) {
      expect(g.parent_id).toBe('')
      items.push({ id: g.id, title: g.title, status: g.status, stage: null, parentId: null })
      for (const s of g.stages) {
        for (const t of s.tasks) {
          // 父子关系由结构表达：子行嵌于父行 stages 下，父行 id 即其归属
          items.push({ id: t.id, title: t.title, status: t.status, stage: t.stage, parentId: g.id })
        }
      }
    }
    expect(items.map((i) => i.id)).toEqual(['g9', 'c1', 'c2', 'c3', 'g10'])
    // status 四态词表内逐项可判（样本覆盖 active/completed/queued/blocked）
    const vocab = new Set(['active', 'completed', 'blocked', 'queued'])
    for (const i of items) expect(vocab.has(i.status)).toBe(true)
    // stage：子行 = 所在分组编号（wave），父行/无阶段 = null 由 UI 自行口径
    expect(items.find((i) => i.id === 'c3')?.stage).toBe(2)
  })

  it('父行子任务集合完备：child_total = 各阶段组 tasks 数之和', async () => {
    stubFetch({ body: contractBody() })
    const rows = await listProjectTaskGroups('p9')
    for (const g of rows) {
      expect(g.child_total).toBe(g.stages.reduce((n, s) => n + s.tasks.length, 0))
    }
    expect(rows[0].child_completed).toBe(1)
    expect(rows[1].stages).toEqual([])
  })

  it('类型层：TaskGroupRow / TaskChild 覆盖 server 冻结键集（tsc -b 兜底，vitest 不查类型）', () => {
    const g: TaskGroupRow = makeGroup()
    // Delivery 内联全字段在类型上可寻址（INFERA-228 对齐 server 顶层键集）
    const rowFields = [
      g.id, g.title, g.status, g.current_stage, g.parent_id, g.wave,
      g.base_commit, g.reject_reason, g.workspace_ready, g.stages,
    ]
    const c: TaskChild = makeChild()
    const childFields = [c.id, c.title, c.status, c.stage, c.current_stage]
    expect(rowFields).toHaveLength(10)
    expect(childFields).toHaveLength(5)
  })
})
