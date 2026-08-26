import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { getChildProgress } from '@/lib/infera-api'
import type {
  ChildProgress,
  ChildStageProgress,
} from '@/lib/infera-types'
import { ChildProgressCard } from './child-progress'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getChildProgress: vi.fn(),
  }
})

/** 单维度计数（后端契约：by_status 恒含五键，六计数互斥可直加） */
function counts(overrides: Partial<ChildStageProgress> = {}) {
  return {
    total: 0,
    done: 0,
    in_progress: 0,
    in_review: 0,
    blocked: 0,
    todo: 0,
    cancelled: 0,
    by_status: {
      active: 0,
      queued: 0,
      completed: 0,
      blocked: 0,
      cancelled: 0,
    },
    ...overrides,
  }
}

/** 冻结契约形状的聚合 fixture */
function makeProgress(overrides: Partial<ChildProgress> = {}): ChildProgress {
  return {
    delivery_id: 'd1',
    active_stage: null,
    ...counts(),
    stages: [],
    ...overrides,
  }
}

/** 混合状态三阶段 fixture：阶段 1 全完、阶段 2 混合推进中、无阶段组垫底 */
function mixedStages(): ChildProgress {
  return makeProgress({
    active_stage: 2,
    ...counts({
      total: 6,
      done: 3,
      in_progress: 1,
      in_review: 1,
      blocked: 1,
      todo: 0,
      cancelled: 0,
      by_status: {
        active: 2,
        queued: 0,
        completed: 3,
        blocked: 1,
        cancelled: 0,
      },
    }),
    stages: [
      { stage: 1, ...counts({ total: 2, done: 2 }) },
      {
        stage: 2,
        ...counts({
          total: 3,
          done: 1,
          in_progress: 1,
          in_review: 1,
          by_status: { active: 2, queued: 0, completed: 1, blocked: 0, cancelled: 0 },
        }),
      },
      { stage: 0, ...counts({ total: 1, blocked: 1 }) },
    ],
  })
}

async function renderCard(progress?: ChildProgress | never) {
  const mock = vi.mocked(getChildProgress)
  if (progress !== undefined) mock.mockResolvedValue(progress)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const screen = await render(
    <QueryClientProvider client={queryClient}>
      <ChildProgressCard deliveryId='d1' />
    </QueryClientProvider>
  )
  return screen
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await cleanup()
})

describe('ChildProgressCard 子任务进度区（L202608260142-3-T01：消费只读聚合接口）', () => {
  it('AC1: 以聚合端点为唯一数据源——对 deliveryId 调用 getChildProgress', async () => {
    await renderCard(mixedStages())
    expect(vi.mocked(getChildProgress)).toHaveBeenCalledWith('d1')
  })

  it('AC2: 总体进度 = 真实状态聚合（done/total 与百分比），非零状态可见', async () => {
    const screen = await renderCard(mixedStages())
    await expect
      .element(screen.getByText('子任务进度', { exact: true }))
      .toBeInTheDocument()

    // 头部计数与百分比逐字来自聚合：3/6 完成 · 50%
    await expect
      .element(screen.getByText('3 / 6 完成 · 50%', { exact: true }))
      .toBeInTheDocument()
    // 总体进度条按聚合填充（可核验）
    const bar = screen.getByRole('progressbar', { name: '子任务完成度' })
    await expect.element(bar).toBeInTheDocument()
    expect((await bar.element()).getAttribute('aria-valuenow')).toBe('50')

    // 运行中 / 待审批 / 已阻塞 在总体区可见（非零才出现）
    for (const label of ['运行中 1', '待审批 1', '已阻塞 1', '已完成 3']) {
      await expect
        .element(screen.getByText(label, { exact: true }).first())
        .toBeInTheDocument()
    }
    // 零计数状态不占位
    expect(await screen.getByText('未启动 0', { exact: true }).query()).toBeNull()
    expect(await screen.getByText('已取消 0', { exact: true }).query()).toBeNull()
  })

  it('AC3: 六类状态计数逐一可见（含未启动与已取消）', async () => {
    const screen = await renderCard(
      makeProgress(
        counts({
          total: 6,
          done: 1,
          in_progress: 1,
          in_review: 1,
          blocked: 1,
          todo: 1,
          cancelled: 1,
        })
      )
    )
    await expect
      .element(screen.getByText('子任务进度', { exact: true }))
      .toBeInTheDocument()

    for (const label of [
      '运行中 1',
      '待审批 1',
      '已阻塞 1',
      '未启动 1',
      '已完成 1',
      '已取消 1',
    ]) {
      await expect
        .element(screen.getByText(label, { exact: true }).first())
        .toBeInTheDocument()
    }
  })

  it('AC4: 按 stage 分组展示（编号升序、0 垫底为「无阶段」），组内计数独立', async () => {
    const screen = await renderCard(mixedStages())
    await expect
      .element(screen.getByText('子任务进度', { exact: true }))
      .toBeInTheDocument()

    // 组标题：阶段 1 → 阶段 2 → 无阶段（后端契约顺序，前端不再排序）
    for (const title of ['阶段 1', '阶段 2', '无阶段']) {
      await expect
        .element(screen.getByText(title, { exact: true }))
        .toBeInTheDocument()
    }
    const s1 = await screen.getByText('阶段 1', { exact: true }).element()
    const s2 = await screen.getByText('阶段 2', { exact: true }).element()
    const s0 = await screen.getByText('无阶段', { exact: true }).element()
    expect(
      s1!.compareDocumentPosition(s2!) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      s2!.compareDocumentPosition(s0!) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()

    // 组内完成度独立可核验：阶段 1 = 2/2，阶段 2 = 1/3，无阶段 = 0/1
    const groupOf = (el: Element) => el.closest('[data-stage]')
    expect(groupOf(s1)!.textContent).toContain('2 / 2')
    expect(groupOf(s2)!.textContent).toContain('1 / 3')
    expect(groupOf(s0)!.textContent).toContain('0 / 1')
    const bar1 = screen.getByRole('progressbar', { name: '阶段 1 完成度' })
    const bar2 = screen.getByRole('progressbar', { name: '阶段 2 完成度' })
    expect((await bar1.element()).getAttribute('aria-valuenow')).toBe('100')
    expect((await bar2.element()).getAttribute('aria-valuenow')).toBe('33')
  })

  it('AC5: active_stage 标注当前阶段（当前组可辨认，其余组不标）', async () => {
    const screen = await renderCard(mixedStages())
    await expect
      .element(screen.getByText('子任务进度', { exact: true }))
      .toBeInTheDocument()

    const s2 = await screen.getByText('阶段 2', { exact: true }).element()
    const group2 = s2!.closest('[data-stage]')!
    expect(group2.textContent).toContain('当前')
    const s1 = await screen.getByText('阶段 1', { exact: true }).element()
    expect(s1!.closest('[data-stage]')!.textContent).not.toContain('当前')
  })

  it('AC6: 全部完结（active_stage 为 null）不标注当前阶段', async () => {
    const screen = await renderCard(
      makeProgress({
        active_stage: null,
        ...counts({
          total: 2,
          done: 2,
          by_status: { active: 0, queued: 0, completed: 2, blocked: 0, cancelled: 0 },
        }),
        stages: [
          { stage: 1, ...counts({ total: 2, done: 2 }) },
        ],
      })
    )
    await expect
      .element(screen.getByText('阶段 1', { exact: true }))
      .toBeInTheDocument()
    expect(await screen.getByText('当前', { exact: true }).query()).toBeNull()
  })

  it('AC7: 空态——无子任务（total=0、stages=[]）不渲染任何进度 UI', async () => {
    const screen = await renderCard(makeProgress())
    // 等 query settle：mock 已 resolve，card 自判空态返回 null
    await vi.waitFor(() =>
      expect(vi.mocked(getChildProgress).mock.results[0]).toBeTruthy()
    )
    expect(
      await screen.getByText('子任务进度', { exact: true }).query()
    ).toBeNull()
    expect(document.querySelector('[data-slot="child-progress"]')).toBeNull()
  })

  it('AC8: 聚合加载中不渲染占位 UI（数据落地后才出现）', async () => {
    // 永不 resolve：query 停在 pending
    vi.mocked(getChildProgress).mockReturnValue(
      new Promise(() => {}) as Promise<ChildProgress>
    )
    const screen = await renderCard()
    expect(
      await screen.getByText('子任务进度', { exact: true }).query()
    ).toBeNull()
  })
})
