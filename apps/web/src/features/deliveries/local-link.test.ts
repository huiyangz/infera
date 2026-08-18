import { describe, expect, it } from 'vitest'
import { gateHasLocalRole, parkedAtLocalNode } from './local-link'

const base = { status: 'active', pending_gate: null, split_mode: false }

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
