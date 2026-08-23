import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { listProjects } from '@/lib/infera-api'
import type { Project } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { ProjectsList } from './projects-list'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    listProjects: vi.fn(),
    patchProjectPinned: vi.fn(),
    createProject: vi.fn(),
  }
})

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}))

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@tanstack/react-router')>()
  // Link 脱离 Router 上下文无法渲染，用 <a> 替身
  const MockLink = ({
    children,
    to,
    ...props
  }: React.ComponentProps<'a'> & { to?: string }) => (
    <a href={to ?? '#'} {...props}>
      {children}
    </a>
  )
  return { ...actual, Link: MockLink }
})

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: 'p1',
    name: '演示项目',
    repo_url: 'git@github.com:acme/repo.git',
    default_branch: 'main',
    pinned: false,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    external_project_id: '',
    external_synced_at: null,
    ...overrides,
  }
}

async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <ProjectsList />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listProjects).mockResolvedValue([])
})

afterEach(async () => {
  await cleanup()
})

describe('ProjectsList 默认编排入口移除（INFERA-181）', () => {
  it('页面没有「默认编排」按钮/文案——项目绑定是唯一编排定义', async () => {
    const screen = await mount()
    expect(
      await screen.getByRole('button', { name: /默认编排/ }).query()
    ).toBeNull()
    expect(await screen.getByText(/默认编排/).query()).toBeNull()
  })
})

describe('ProjectsList 来源标识与自动加载', () => {
  it('项目卡不带任何「已同步」来源徽标——同步信息不可见（INFERA-194）', async () => {
    vi.mocked(listProjects).mockResolvedValue([
      makeProject({ id: 'p1', name: '本地项目' }),
      makeProject({
        id: 'p2',
        name: '外部项目',
        repo_url: '',
        external_project_id: 'mp-2',
        external_synced_at: '2026-08-22T03:00:05Z',
      }),
    ])
    const screen = await mount()
    await expect.element(screen.getByText('外部项目')).toBeInTheDocument()
    // 带 external_project_id 的项目也不渲染来源徽标
    expect(
      await screen.getByText('已同步', { exact: true }).query(),
    ).toBeNull()
  })

  it('无任何手动触发：挂载即自动加载项目数据，页面上没有同步按钮门槛', async () => {
    vi.mocked(listProjects).mockResolvedValue([
      makeProject({ id: 'p1', name: '自动出现的项目' }),
    ])
    const screen = await mount()
    await expect
      .element(screen.getByText('自动出现的项目'))
      .toBeInTheDocument()
    // 手动门槛已移除：列表页不再有任何「同步」按钮（立即同步入口在全局侧栏）
    expect(
      await screen.getByRole('button', { name: /同步/ }).query(),
    ).toBeNull()
  })
})
