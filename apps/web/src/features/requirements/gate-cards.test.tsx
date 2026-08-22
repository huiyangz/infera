import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, type RenderResult } from 'vitest-browser-react'
import {
  approveCard,
  decideCard,
  getRequirementPRReview,
  mergeCard,
  rejectCard,
  reworkCard,
} from './api'
import type { GateCard, PRReview, Requirement } from './types'
import {
  ApprovalCard,
  DecisionCard,
  MergeCard,
  UpdateCard,
} from './gate-cards'

vi.mock('./api', () => ({
  approveCard: vi.fn().mockResolvedValue({ ok: true }),
  rejectCard: vi.fn().mockResolvedValue({ ok: true }),
  decideCard: vi.fn().mockResolvedValue({ ok: true }),
  mergeCard: vi.fn().mockResolvedValue({ merged: true, sha: 'abc', message: '' }),
  reworkCard: vi.fn().mockResolvedValue({ ok: true }),
  getRequirementPRReview: vi.fn(),
}))

const REQ: Requirement = {
  id: 'r1',
  title: '深色模式',
  description: '',
  acceptance_criteria: '',
  source: 'web',
  priority: 'P1',
  acceptors: [],
  external_issue_id: 'm1',
  external_issue_key: 'INFERA-31',
  external_issue_url: 'http://tasks.local/infera/issues/m1',
  pr_url: 'https://github.com/acme/repo/pull/7',
  node: 'in_review',
  created_at: '2026-08-21T00:00:00Z',
  updated_at: '2026-08-21T01:00:00Z',
}

function card(over: Partial<GateCard>): GateCard {
  return {
    id: 'c1',
    requirement_id: 'r1',
    kind: 'approval',
    status: 'pending',
    payload: '',
    comment_id: 'cm1',
    created_at: '2026-08-21T02:00:00Z',
    resolved_at: null,
    ...over,
  }
}

async function mount(ui: React.ReactNode): Promise<RenderResult> {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return await render(
    <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await cleanup()
})

describe('ApprovalCard 审批卡', () => {
  it('渲染计划正文；批准回调 approveCard(r1,c1)', async () => {
    const screen = await mount(
      <ApprovalCard
        card={card({ kind: 'approval', payload: '待批准：实现计划正文…' })}
        requirement={REQ}
      />
    )
    await expect
      .element(screen.getByText('待批准：实现计划正文…'))
      .toBeInTheDocument()
    await screen.getByRole('button', { name: '批准' }).click()
    await vi.waitFor(() => expect(approveCard).toHaveBeenCalledWith('r1', 'c1'))
  })

  it('驳回需反馈文本：空时禁用，填写后回调 rejectCard(r1,c1,反馈)', async () => {
    const screen = await mount(
      <ApprovalCard card={card({ payload: '待批准：计划' })} requirement={REQ} />
    )
    const reject = screen.getByRole('button', { name: '驳回并反馈' })
    await expect.element(reject).toBeDisabled()
    await screen
      .getByPlaceholder('驳回反馈：说明需要调整的地方…')
      .fill('方案缺少迁移回滚，请补充')
    await screen.getByRole('button', { name: '驳回并反馈' }).click()
    await vi.waitFor(() =>
      expect(rejectCard).toHaveBeenCalledWith('r1', 'c1', '方案缺少迁移回滚，请补充')
    )
  })
})

describe('DecisionCard 决策卡', () => {
  it('渲染 escalate 内容；重试直接回调 decideCard(r1,c1,retry,空文本)', async () => {
    const screen = await mount(
      <DecisionCard
        card={card({ kind: 'decision', payload: '需要决策：单测在 CI 上超时失败，是否重试？' })}
        requirement={REQ}
      />
    )
    await expect
      .element(screen.getByText(/单测在 CI 上超时失败/))
      .toBeInTheDocument()
    await screen.getByRole('button', { name: '重试' }).click()
    await vi.waitFor(() =>
      expect(decideCard).toHaveBeenCalledWith('r1', 'c1', 'retry', '')
    )
  })

  it('跳过 / 中止各自回调对应 choice', async () => {
    const screen = await mount(
      <DecisionCard card={card({ kind: 'decision', payload: '需要决策：X' })} requirement={REQ} />
    )
    await screen.getByRole('button', { name: '跳过' }).click()
    await vi.waitFor(() =>
      expect(decideCard).toHaveBeenCalledWith('r1', 'c1', 'skip', '')
    )
    await screen.getByRole('button', { name: '中止' }).click()
    await vi.waitFor(() =>
      expect(decideCard).toHaveBeenCalledWith('r1', 'c1', 'abort', '')
    )
  })

  it('自定义回复需文本：空时禁用，填写后回调 custom + 文本', async () => {
    const screen = await mount(
      <DecisionCard card={card({ kind: 'decision', payload: '需要决策：X' })} requirement={REQ} />
    )
    const send = screen.getByRole('button', { name: '发送自定义回复' })
    await expect.element(send).toBeDisabled()
    await screen.getByPlaceholder('自定义回复内容…').fill('先跳过 lint，后续单独修')
    await screen.getByRole('button', { name: '发送自定义回复' }).click()
    await vi.waitFor(() =>
      expect(decideCard).toHaveBeenCalledWith('r1', 'c1', 'custom', '先跳过 lint，后续单独修')
    )
  })
})

describe('MergeCard 合并卡', () => {
  const PAYLOAD = [
    'verdict: PASS',
    '评审结论：整体符合规格，行级评论见下方。',
  ].join('\n')

  // 评审面 mock：字段值刻意区别于 payload 文本，断言的是真实渲染而非正文匹配。
  const REVIEW: PRReview = {
    pr_url: 'https://github.com/acme/repo/pull/7',
    comments: [
      {
        id: 11,
        path: 'server/internal/api/requirements.go',
        line: 88,
        original_line: 88,
        side: 'RIGHT',
        body: '建议补充鉴权校验的边界用例',
        author: 'reviewer-ai',
        in_reply_to_id: 0,
        created_at: '2026-08-21T03:00:00Z',
      },
      {
        id: 12,
        path: 'apps/web/src/features/requirements/gate-cards.tsx',
        line: 0,
        original_line: 201,
        side: 'LEFT',
        body: '删除行上的评论，行号取 original_line',
        author: 'reviewer-lead',
        in_reply_to_id: 0,
        created_at: '2026-08-21T03:01:00Z',
      },
    ],
    diff: { files: 3, additions: 87, deletions: 15, changes: 102 },
  }

  it('渲染 verdict、行级评审评论（path:line/side/author/body）与 diff 概要（文件数与 +/- 行数），深链指向外部任务源 issue 与 PR（新窗口）', async () => {
    vi.mocked(getRequirementPRReview).mockResolvedValue(REVIEW)
    const screen = await mount(
      <MergeCard card={card({ kind: 'merge', payload: PAYLOAD })} requirement={REQ} />
    )
    await expect.element(screen.getByText('PASS', { exact: true })).toBeInTheDocument()
    // 真实渲染的行级评论（非 payload 正文）
    await expect
      .element(screen.getByText(/server\/internal\/api\/requirements\.go/))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText(/gate-cards\.tsx/))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('建议补充鉴权校验的边界用例'))
      .toBeInTheDocument()
    await expect.element(screen.getByText('reviewer-ai', { exact: true })).toBeInTheDocument()
    // 删除行评论行号取 original_line（line=0）
    await expect.element(screen.getByText(/:201/)).toBeInTheDocument()
    // diff 概要：文件数与 +/- 行数
    await expect
      .element(screen.getByText(/3 个文件/))
      .toBeInTheDocument()
    await expect.element(screen.getByText(/\+87/)).toBeInTheDocument()
    await expect.element(screen.getByText(/-15/)).toBeInTheDocument()
    const issue = screen.getByRole('link', { name: '查看完整时间线' })
    expect((await issue.element()).getAttribute('href')).toBe(REQ.external_issue_url)
    expect((await issue.element()).getAttribute('target')).toBe('_blank')
    const pr = screen.getByRole('link', { name: /PR/ })
    expect((await pr.element()).getAttribute('href')).toBe(REQ.pr_url)
    expect(getRequirementPRReview).toHaveBeenCalledWith('r1')
  })

  it('需求未关联 PR（pr_url 为空）时不请求评审面，verdict 与动作照常', async () => {
    const screen = await mount(
      <MergeCard
        card={card({ kind: 'merge', payload: PAYLOAD })}
        requirement={{ ...REQ, pr_url: '' }}
      />
    )
    await expect.element(screen.getByText('PASS', { exact: true })).toBeInTheDocument()
    expect(getRequirementPRReview).not.toHaveBeenCalled()
  })

  it('评审面加载失败不拖垮卡片：verdict 与动作照常，不渲染评审区', async () => {
    vi.mocked(getRequirementPRReview).mockRejectedValue(new Error('上游服务暂时不可用'))
    const screen = await mount(
      <MergeCard card={card({ kind: 'merge', payload: PAYLOAD })} requirement={REQ} />
    )
    await expect.element(screen.getByText('PASS', { exact: true })).toBeInTheDocument()
    await vi.waitFor(() => {
      // 查询已失败（retry: false），评审区不出现
      expect(getRequirementPRReview).toHaveBeenCalled()
    })
    expect(screen.container.querySelector('[data-pr-review]')).toBeNull()
    await expect.element(screen.getByText(/评审结论/)).toBeInTheDocument()
  })

  it('FAIL verdict 呈现未通过；合并回调 mergeCard，返工需反馈后回调 reworkCard', async () => {
    const screen = await mount(
      <MergeCard
        card={card({ kind: 'merge', payload: 'verdict: FAIL\n回归用例两处失败' })}
        requirement={REQ}
      />
    )
    await expect.element(screen.getByText('FAIL', { exact: true })).toBeInTheDocument()
    await screen.getByRole('button', { name: '合并' }).click()
    await vi.waitFor(() => expect(mergeCard).toHaveBeenCalledWith('r1', 'c1'))
    const rework = screen.getByRole('button', { name: '拒绝并返工' })
    await expect.element(rework).toBeDisabled()
    await screen.getByPlaceholder('返工反馈：说明拒绝合并的原因…').fill('存在回归，请修复')
    await screen.getByRole('button', { name: '拒绝并返工' }).click()
    await vi.waitFor(() =>
      expect(reworkCard).toHaveBeenCalledWith('r1', 'c1', '存在回归，请修复')
    )
  })
})

describe('UpdateCard 兜底卡', () => {
  it('中性呈现新动态正文与深链，无任何动作按钮', async () => {
    const screen = await mount(
      <UpdateCard
        card={card({ kind: 'update', payload: '进度更新：单元测试全部通过' })}
        requirement={REQ}
      />
    )
    await expect
      .element(screen.getByText('进度更新：单元测试全部通过'))
      .toBeInTheDocument()
    const issue = screen.getByRole('link', { name: '查看完整时间线' })
    expect((await issue.element()).getAttribute('href')).toBe(REQ.external_issue_url)
    expect(screen.container.querySelector('button')).toBeNull()
  })
})
