import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, type RenderResult } from 'vitest-browser-react'
import { ApiError, listProjects } from '@/lib/infera-api'
import type { Project } from '@/lib/infera-types'
import { createProjectRequirement } from './api'
import { CreateRequirementDialog } from './requirement-create-dialog'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return { ...actual, listProjects: vi.fn() }
})

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return { ...actual, createProjectRequirement: vi.fn() }
})

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

function makeProject(id: string, name: string): Project {
  return {
    id,
    name,
    repo_url: '',
    default_branch: 'main',
    pinned: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    external_project_id: '',
    external_synced_at: null,
  }
}

async function mount(projectId = 'p1'): Promise<RenderResult> {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return await render(
    <QueryClientProvider client={qc}>
      <CreateRequirementDialog projectId={projectId} />
    </QueryClientProvider>,
  )
}

/** 打开对话框：点入口按钮，等表单字段出现（listProjects query 已 resolve） */
async function openDialog(screen: RenderResult) {
  await screen.getByRole('button', { name: '新建需求' }).click()
  await expect.element(screen.getByRole('dialog')).toBeInTheDocument()
  await expect.element(screen.getByLabelText('标题')).toBeInTheDocument()
}

/** 拉起对话框并填好标题（可选连同描述） */
async function openAndFillTitle(
  screen: RenderResult,
  title = '登录页改版',
  description?: string,
) {
  await openDialog(screen)
  await screen.getByLabelText('标题').fill(title)
  if (description !== undefined) {
    await screen.getByLabelText('描述').fill(description)
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listProjects).mockResolvedValue([
    makeProject('p1', '演示项目'),
    makeProject('p2', '后台'),
  ])
  vi.mocked(createProjectRequirement).mockResolvedValue({
    id: 'd1',
    project_id: 'p1',
    title: '登录页改版',
    description: '',
    status: 'queued',
    current_stage: '',
    pending_gate: null,
    fail_count: 0,
    created_at: '2026-08-23T00:00:00Z',
    updated_at: '2026-08-23T00:00:00Z',
    external_issue_id: 'mi-9',
    external_issue_key: 'INFERA-99',
    assignee: 'agent:tech-lead',
    priority: '',
    external_synced_at: '2026-08-23T00:00:00Z',
    parent_id: '',
    wave: 0,
    split_mode: false,
    merge_state: '',
    complexity: '',
  })
})

afterEach(async () => {
  await cleanup()
})

describe('CreateRequirementDialog 入口与字段（INFERA-178：项目详情/任务列表共享）', () => {
  it('入口按钮「新建需求」渲染，点击打开对话框', async () => {
    const screen = await mount()
    await openDialog(screen)
  })

  it('字段齐全：标题/描述/状态/优先级/智能体/项目/自动合并', async () => {
    const screen = await mount()
    await openDialog(screen)

    for (const name of ['标题', '描述']) {
      await expect.element(screen.getByLabelText(name)).toBeInTheDocument()
    }
    for (const name of ['状态', '优先级', '智能体', '项目']) {
      await expect.element(
        screen.getByRole('combobox', { name, exact: true }),
      ).toBeInTheDocument()
    }
    await expect.element(
      screen.getByRole('switch', { name: '自动合并' }),
    ).toBeInTheDocument()
  })

  it('默认值：状态=待规划、优先级=无、智能体=Tech Lead、项目=当前项目、自动合并=关', async () => {
    const screen = await mount('p1')
    await openDialog(screen)

    const cases: Array<[string, string]> = [
      ['状态', '待规划'],
      ['优先级', '无'],
      ['智能体', 'Tech Lead（默认）'],
      ['项目', '演示项目'],
    ]
    for (const [field, label] of cases) {
      const el = (await screen
        .getByRole('combobox', { name: field, exact: true })
        .element())!
      expect(el.textContent).toContain(label)
    }
    await expect
      .element(screen.getByRole('switch', { name: '自动合并' }))
      .not.toBeChecked()
  })

  it('标题为空时提交按钮禁用、不发起创建', async () => {
    const screen = await mount()
    await openDialog(screen)

    await expect
      .element(screen.getByRole('button', { name: '创建需求' }))
      .toBeDisabled()
    expect(vi.mocked(createProjectRequirement)).not.toHaveBeenCalled()
  })
})

describe('CreateRequirementDialog 提交（INFERA-178 AC3）', () => {
  it('成功：按缺省值上送创建载荷，对话框关闭并 toast 成功', async () => {
    const screen = await mount('p1')
    await openAndFillTitle(screen, '登录页改版', '支持手机号登录')
    await screen.getByRole('button', { name: '创建需求' }).click()

    await expect
      .element(screen.getByRole('dialog').query())
      .toBeNull()
    expect(vi.mocked(createProjectRequirement)).toHaveBeenCalledTimes(1)
    expect(vi.mocked(createProjectRequirement)).toHaveBeenCalledWith('p1', {
      title: '登录页改版',
      description: '支持手机号登录',
      status: 'backlog',
      priority: 'none',
      auto_merge: false,
      agent_id: '',
    })
    expect(vi.mocked(toast.success)).toHaveBeenCalledTimes(1)
  })

  it('失败：toast 展示后端错误文案，对话框保持打开可修改重试', async () => {
    vi.mocked(createProjectRequirement).mockRejectedValue(
      new ApiError(409, '项目未绑定上游映射'),
    )
    const screen = await mount()
    await openAndFillTitle(screen, '登录页改版')
    await screen.getByRole('button', { name: '创建需求' }).click()

    await expect
      .element(screen.getByRole('dialog'))
      .toBeInTheDocument()
    expect(vi.mocked(toast.error)).toHaveBeenCalledWith('项目未绑定上游映射')
  })

  it('改字段全量映射：状态=待办、优先级=高、自动合并开、智能体自定义、项目切换', async () => {
    const screen = await mount('p1')
    await openAndFillTitle(screen, '新支付渠道')

    // 状态 → 待办
    await screen.getByRole('combobox', { name: '状态', exact: true }).click()
    await screen.getByRole('option', { name: '待办', exact: true }).click()
    // 优先级 → 高
    await screen.getByRole('combobox', { name: '优先级', exact: true }).click()
    await screen.getByRole('option', { name: '高', exact: true }).click()
    // 智能体 → 自定义，填 agent id
    await screen.getByRole('combobox', { name: '智能体', exact: true }).click()
    await screen.getByRole('option', { name: '自定义…', exact: true }).click()
    await screen.getByLabelText('智能体 ID').fill('agent-9')
    // 项目 → 后台（p2）
    await screen.getByRole('combobox', { name: '项目', exact: true }).click()
    await screen.getByRole('option', { name: '后台', exact: true }).click()
    // 自动合并 → 开
    await screen.getByRole('switch', { name: '自动合并' }).click()

    await screen.getByRole('button', { name: '创建需求' }).click()

    await expect
      .element(screen.getByRole('dialog').query())
      .toBeNull()
    expect(vi.mocked(createProjectRequirement)).toHaveBeenCalledWith('p2', {
      title: '新支付渠道',
      description: '',
      status: 'todo',
      priority: 'high',
      auto_merge: true,
      agent_id: 'agent-9',
    })
  })

  it('智能体选自定义但未填 ID：提交禁用（不发出半配置指派）', async () => {
    const screen = await mount()
    await openAndFillTitle(screen, '登录页改版')
    await screen.getByRole('combobox', { name: '智能体', exact: true }).click()
    await screen.getByRole('option', { name: '自定义…', exact: true }).click()

    await expect
      .element(screen.getByRole('button', { name: '创建需求' }))
      .toBeDisabled()
  })
})
