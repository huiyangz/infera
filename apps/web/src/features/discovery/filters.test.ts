import { describe, expect, it } from 'vitest'
import type { DeliveryStatus } from '@/lib/infera-types'
import type { DiscoveryTaskRow } from './types'
import {
  AGENT_ORDER,
  STATUS_LABEL,
  STATUS_ORDER,
  filterDiscoveryTasks,
  groupDiscoveryTasks,
  splitColumnsByCancelled,
} from './filters'

/** 最小可用行：仅筛选/分组用到的字段（其余字段不影响纯逻辑） */
function row(over: Partial<DiscoveryTaskRow>): DiscoveryTaskRow {
  return {
    id: 'd-1',
    project_id: 'p1',
    title: '示例任务',
    description: '',
    status: 'active',
    current_stage: 'intake',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-24T00:00:00Z',
    updated_at: '2026-08-24T00:00:00Z',
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
    agent_types: ['mining'],
    project_name: '自动闭环',
    labels: [],
    ...over,
  }
}

describe('filterDiscoveryTasks 页内筛选', () => {
  const rows = [
    row({ id: 'a', agent_types: ['mining'], status: 'active' }),
    row({ id: 'b', agent_types: ['analysis'], status: 'completed' }),
    // 双标签卡：两类都命中（后端 agent_types 报全集）
    row({ id: 'c', agent_types: ['mining', 'analysis'], status: 'blocked' }),
  ]

  it('agent=all + status=all：全量透传', () => {
    expect(filterDiscoveryTasks(rows, 'all', 'all').map((r) => r.id)).toEqual([
      'a',
      'b',
      'c',
    ])
  })

  it('agent 筛选用 agent_types 全集：双标签卡在 mining 与 analysis 下都保留', () => {
    expect(
      filterDiscoveryTasks(rows, 'mining', 'all').map((r) => r.id)
    ).toEqual(['a', 'c'])
    expect(
      filterDiscoveryTasks(rows, 'analysis', 'all').map((r) => r.id)
    ).toEqual(['b', 'c'])
  })

  it('status 精确匹配', () => {
    expect(
      filterDiscoveryTasks(rows, 'all', 'completed').map((r) => r.id)
    ).toEqual(['b'])
  })

  it('cancelled 是合法筛选值：精确匹配放弃行（INFERA-233）', () => {
    const withCancelled = [
      ...rows,
      row({ id: 'x', status: 'cancelled' }),
    ]
    expect(
      filterDiscoveryTasks(withCancelled, 'all', 'cancelled').map((r) => r.id)
    ).toEqual(['x'])
  })

  it('agent 与 status 叠加取交集', () => {
    expect(filterDiscoveryTasks(rows, 'mining', 'blocked').map((r) => r.id)).toEqual(
      ['c']
    )
    expect(filterDiscoveryTasks(rows, 'analysis', 'active')).toEqual([])
  })
})

describe('groupDiscoveryTasks 页内分组', () => {
  it("mode='none'：单一无标签组原样承载全部行", () => {
    const rows = [row({ id: 'a' }), row({ id: 'b' })]
    const groups = groupDiscoveryTasks(rows, 'none')
    expect(groups).toHaveLength(1)
    expect(groups[0].label).toBe('')
    expect(groups[0].rows.map((r) => r.id)).toEqual(['a', 'b'])
  })

  it("mode='agent'：按类型全集分组（双标签卡两组都出现），组序 mining→analysis", () => {
    const rows = [
      row({ id: 'a', agent_types: ['mining'] }),
      row({ id: 'b', agent_types: ['mining', 'analysis'] }),
      row({ id: 'c', agent_types: ['analysis'] }),
    ]
    const groups = groupDiscoveryTasks(rows, 'agent')
    expect(groups.map((g) => g.key)).toEqual(['mining', 'analysis'])
    expect(groups.map((g) => g.label)).toEqual(['需求挖掘', '需求分析'])
    expect(groups[0].rows.map((r) => r.id)).toEqual(['a', 'b'])
    expect(groups[1].rows.map((r) => r.id)).toEqual(['b', 'c'])
  })

  it("mode='agent'：空组不占位（无 analysis 行时不渲染该组）", () => {
    const groups = groupDiscoveryTasks(
      [row({ id: 'a', agent_types: ['mining'] })],
      'agent'
    )
    expect(groups.map((g) => g.key)).toEqual(['mining'])
  })

  it("mode='status'：按状态分组，组序固定 active→blocked→queued→completed", () => {
    const rows = [
      row({ id: 'a', status: 'completed' }),
      row({ id: 'b', status: 'active' }),
      row({ id: 'c', status: 'active' }),
      row({ id: 'd', status: 'queued' }),
    ]
    const groups = groupDiscoveryTasks(rows, 'status')
    expect(groups.map((g) => g.key)).toEqual(['active', 'queued', 'completed'])
    expect(groups.map((g) => g.label)).toEqual(['进行中', '未启动', '已完成'])
    expect(groups[0].rows.map((r) => r.id)).toEqual(['b', 'c'])
  })

  it("mode='status'：cancelled 独立成组，排在 completed 之后殿后（INFERA-233）", () => {
    const groups = groupDiscoveryTasks(
      [
        row({ id: 'a', status: 'cancelled' }),
        row({ id: 'b', status: 'completed' }),
        row({ id: 'c', status: 'active' }),
      ],
      'status'
    )
    expect(groups.map((g) => g.key)).toEqual([
      'active',
      'completed',
      'cancelled',
    ])
    expect(groups.map((g) => g.label)).toEqual(['进行中', '已完成', '已取消'])
    expect(groups[2].rows.map((r) => r.id)).toEqual(['a'])
  })

  it('空输入：任何模式都产出零个有行分组（none 出一个空组）', () => {
    expect(groupDiscoveryTasks([], 'agent')).toEqual([])
    expect(groupDiscoveryTasks([], 'status')).toEqual([])
    expect(groupDiscoveryTasks([], 'none')).toEqual([
      { key: 'all', label: '', rows: [] },
    ])
  })
})

describe('固定序常量', () => {
  it('AGENT_ORDER 与后端 discoveryAgentTypes 对齐（mining → analysis）', () => {
    expect(AGENT_ORDER).toEqual(['mining', 'analysis'])
  })

  it('STATUS_ORDER 穷尽 DeliveryStatus 五态（T01 冻结词表）', () => {
    const all: DeliveryStatus[] = [
      'active',
      'completed',
      'blocked',
      'queued',
      'cancelled',
    ]
    expect([...STATUS_ORDER].sort()).toEqual([...all].sort())
    expect(new Set(STATUS_ORDER).size).toBe(all.length)
  })

  it('STATUS_ORDER 里 cancelled 排在 completed 之后殿后', () => {
    expect(STATUS_ORDER[STATUS_ORDER.length - 1]).toBe('cancelled')
    expect(STATUS_ORDER.indexOf('cancelled')).toBeGreaterThan(
      STATUS_ORDER.indexOf('completed')
    )
  })

  it('STATUS_LABEL 覆盖 cancelled，文案「已取消」', () => {
    expect(STATUS_LABEL.cancelled).toBe('已取消')
  })
})

describe('splitColumnsByCancelled 左右双栏拆分（INFERA-233）', () => {
  it('逐组拆两栏：候选栏收全部非 cancelled 行，已放弃栏收 cancelled 行，组键/组头沿用', () => {
    const groups = [
      { key: 'active', label: '进行中', rows: [row({ id: 'a' })] },
      {
        key: 'completed',
        label: '已完成',
        rows: [
          row({ id: 'b', status: 'completed' }),
          row({ id: 'x', status: 'cancelled' }),
        ],
      },
      { key: 'cancelled', label: '已取消', rows: [row({ id: 'y', status: 'cancelled' })] },
    ]
    const cols = splitColumnsByCancelled(groups)
    expect(cols.candidates.map((g) => g.key)).toEqual(['active', 'completed'])
    expect(cols.candidates[1].rows.map((r) => r.id)).toEqual(['b'])
    expect(cols.cancelled.map((g) => g.key)).toEqual(['completed', 'cancelled'])
    expect(cols.cancelled[0].rows.map((r) => r.id)).toEqual(['x'])
    expect(cols.cancelled[1].rows.map((r) => r.id)).toEqual(['y'])
    // 组头沿用原组标签
    expect(cols.cancelled[0].label).toBe('已完成')
  })

  it('空侧不产出：全 cancelled 时候选栏为空数组，反之已放弃栏为空数组', () => {
    const allCancelled = [
      { key: 'all', label: '', rows: [row({ id: 'x', status: 'cancelled' })] },
    ]
    expect(splitColumnsByCancelled(allCancelled)).toEqual({
      candidates: [],
      cancelled: allCancelled,
    })
    const noneCancelled = [
      { key: 'all', label: '', rows: [row({ id: 'a' })] },
    ]
    expect(splitColumnsByCancelled(noneCancelled)).toEqual({
      candidates: noneCancelled,
      cancelled: [],
    })
  })

  it('空输入：两栏皆空', () => {
    expect(splitColumnsByCancelled([])).toEqual({
      candidates: [],
      cancelled: [],
    })
  })

  it('行序保持不变（不因拆栏重排）', () => {
    const cols = splitColumnsByCancelled([
      {
        key: 'all',
        label: '',
        rows: [
          row({ id: 'c1' }),
          row({ id: 'x1', status: 'cancelled' }),
          row({ id: 'c2' }),
          row({ id: 'x2', status: 'cancelled' }),
        ],
      },
    ])
    expect(cols.candidates[0].rows.map((r) => r.id)).toEqual(['c1', 'c2'])
    expect(cols.cancelled[0].rows.map((r) => r.id)).toEqual(['x1', 'x2'])
  })
})
