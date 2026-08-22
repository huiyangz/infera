import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { getProjectPipeline, listAgents } from '@/lib/infera-api'
import type { Agent, ProjectPipeline } from '@/lib/infera-types'
import { gateHasLocalRole, parkedAtLocalNode, useLocalNodes } from './local-link'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    listAgents: vi.fn(),
    getProjectPipeline: vi.fn(),
  }
})

const base = { status: 'active', pending_gate: null, split_mode: false }

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: 'a1',
    name: 'agent',
    runner: 'cli',
    config: {},
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    ...overrides,
  }
}

/** 探针：把 useLocalNodes 的结果渲染成逗号分隔节点串 */
function LocalNodesProbe({ projectId }: { projectId: string }) {
  const nodes = useLocalNodes(projectId)
  return (
    <div data-testid='local-nodes'>
      {nodes ? [...nodes].sort().join(',') : 'pending'}
    </div>
  )
}

describe('useLocalNodes（INFERA-181：按项目级唯一定义 {nodes, bindings} 计算）', () => {
  it('绑定 local runner agent 的节点入选，其它 runner 不入选', async () => {
    vi.mocked(listAgents).mockResolvedValue([
      makeAgent({ id: 'a1', name: '本机', runner: 'local' }),
      makeAgent({ id: 'a2', name: '远端', runner: 'cli' }),
    ])
    const pipeline: ProjectPipeline = {
      nodes: ['spec', 'code_gen', 'code_review'],
      bindings: { spec: 'a1', code_gen: 'a2', code_review: 'a1' },
    }
    vi.mocked(getProjectPipeline).mockResolvedValue(pipeline)

    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const screen = await render(
      <QueryClientProvider client={qc}>
        <LocalNodesProbe projectId='p1' />
      </QueryClientProvider>
    )
    await expect
      .element(screen.getByTestId('local-nodes'))
      .toHaveTextContent('code_review,spec')
    await cleanup()
  })
})

describe('parkedAtLocalNode', () => {
  it('五个 agent 节点（spec/design/tasks/test_gen/code_gen）绑定 local 都算本机停车', () => {
    const local = new Set(['spec', 'design', 'tasks', 'test_gen', 'code_gen'])
    for (const stage of ['spec', 'design', 'tasks', 'test_gen', 'code_gen']) {
      expect(
        parkedAtLocalNode({ ...base, current_stage: stage }, local),
        `${stage} 应算本机停车（engine local 停车与 helper stageContract 都支持）`
      ).toBe(true)
    }
  })

  it('非 agent 节点（门禁/审查角色）不算本机停车', () => {
    const local = new Set(['code_review', 'spec_conformance', 'code_quality'])
    expect(parkedAtLocalNode({ ...base, current_stage: 'code_review' }, local)).toBe(false)
  })

  it('节点未绑 local / 非活跃 / 挂门禁 / 拆分父停 code_gen 不算', () => {
    expect(parkedAtLocalNode({ ...base, current_stage: 'design' }, new Set(['spec']))).toBe(false)
    expect(
      parkedAtLocalNode({ ...base, current_stage: 'design' }, null)
    ).toBe(false)
    expect(
      parkedAtLocalNode(
        { ...base, current_stage: 'design', pending_gate: 'design_approval' },
        new Set(['design'])
      )
    ).toBe(false)
    expect(
      parkedAtLocalNode({ ...base, current_stage: 'design', status: 'blocked' }, new Set(['design']))
    ).toBe(false)
    expect(
      parkedAtLocalNode(
        { ...base, current_stage: 'code_gen', split_mode: true },
        new Set(['code_gen'])
      )
    ).toBe(false)
  })
})

describe('gateHasLocalRole', () => {
  it('code_review 门禁的本机承担角色（含双道审查）', () => {
    expect(gateHasLocalRole('code_review', new Set(['code_review']))).toBe(true)
    expect(gateHasLocalRole('code_review', new Set(['spec_conformance']))).toBe(true)
    expect(gateHasLocalRole('code_review', new Set(['code_quality']))).toBe(true)
    expect(gateHasLocalRole('code_review', new Set(['spec']))).toBe(false)
    expect(gateHasLocalRole('spec_approval', new Set(['spec']))).toBe(false)
    expect(gateHasLocalRole('code_review', null)).toBe(false)
  })
})
