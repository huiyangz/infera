import { render, cleanup } from 'vitest-browser-react'
import { afterEach, describe, expect, it } from 'vitest'
import type { StageRunStageStats } from '@/lib/infera-types'
import { StageStatsTable } from './stage-stats'

function row(overrides: Partial<StageRunStageStats>): StageRunStageStats {
  return {
    stage: 'spec',
    total: 2,
    done: 2,
    failed: 0,
    running: 0,
    avg_ms: 252_375,
    p95_ms: 252_375,
    ...overrides,
  }
}

afterEach(async () => {
  await cleanup()
})

describe('StageStatsTable（分 stage 聚合健康度）', () => {
  it('by_stage 按流水线阶段序重排（后端字典序 → 阶段序），未知阶段殿后', async () => {
    // 输入故意是字典序：code_gen < spec < tasks_approval < zz_custom
    await render(
      <StageStatsTable
        byStage={[
          row({ stage: 'code_gen', total: 3, done: 2, failed: 1 }),
          row({ stage: 'spec' }),
          row({ stage: 'tasks_approval', total: 1, done: 1, avg_ms: 0, p95_ms: 0 }),
          row({ stage: 'zz_custom', total: 1, done: 1, avg_ms: 0, p95_ms: 0 }),
        ]}
      />
    )

    const stages = Array.from(
      document.querySelectorAll('[data-stage]')
    ).map((el) => el.getAttribute('data-stage'))
    // 流水线序（STAGE_ORDER 全 11 阶段）：spec → tasks_approval → code_gen；
    // 未知 zz_custom 殿后
    expect(stages).toEqual(['spec', 'tasks_approval', 'code_gen', 'zz_custom'])
  })

  it('行内呈现阶段中文名与运行/失败/进行中计数，平均与 P95 走中文紧凑时长', async () => {
    await render(
      <StageStatsTable
        byStage={[
          row({
            stage: 'code_gen',
            total: 3,
            done: 1,
            failed: 1,
            running: 1,
            avg_ms: 252_375,
            p95_ms: 366_000,
          }),
        ]}
      />
    )

    const tr = document.querySelector("[data-stage='code_gen']")
    expect(tr?.textContent).toContain('实现')
    expect(tr?.textContent).toContain('3')
    expect(tr?.textContent).toContain('4 分 12 秒')
    expect(tr?.textContent).toContain('6 分 6 秒')
    // 有失败的行强调失败计数
    const cells = tr?.querySelectorAll('td') ?? []
    expect(cells[2]?.className).toContain('font-medium')
  })

  it('无已收尾运行（avg_ms/p95_ms 为 0 契约值）显示「—」而非误导性 0 秒', async () => {
    await render(
      <StageStatsTable
        byStage={[row({ stage: 'unit_test', total: 1, running: 1, done: 0, avg_ms: 0, p95_ms: 0 })]}
      />
    )
    const tr = document.querySelector("[data-stage='unit_test']")
    expect(tr?.textContent).toContain('—')
    expect(tr?.textContent).not.toContain('0 秒')
  })

  it('空聚合（by_stage 为空数组）不渲染表格', async () => {
    const screen = await render(<StageStatsTable byStage={[]} />)
    expect(document.querySelector('[data-stage-stats]')).toBeNull()
    expect(await screen.getByRole('table').query()).toBeNull()
  })
})
