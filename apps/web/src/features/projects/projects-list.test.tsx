import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { listProjects } from '@/lib/infera-api'
import type { Project } from '@/lib/infera-types'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import {
  getMulticaSyncStatus,
  triggerMulticaSync,
} from '@/features/multica-sync/api'
import { ProjectsList } from './projects-list'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    listProjects: vi.fn(),
    patchProjectPinned: vi.fn(),
    createProject: vi.fn(),
    getPipeline: vi.fn(),
    putPipeline: vi.fn(),
  }
})

vi.mock('@/features/multica-sync/api', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/features/multica-sync/api')>()
  return {
    ...actual,
    getMulticaSyncStatus: vi.fn(),
    triggerMulticaSync: vi.fn(),
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
    multica_project_id: '',
    multica_synced_at: null,
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
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listProjects).mockResolvedValue([])
  vi.mocked(getMulticaSyncStatus).mockResolvedValue({
    running: false,
    last: null,
  })
})

afterEach(async () => {
  await cleanup()
})

describe('ProjectsList 来源标识与同步入口', () => {
  it('multica 同步进来的项目带「Multica」徽标，本地项目不带', async () => {
    vi.mocked(listProjects).mockResolvedValue([
      makeProject({ id: 'p1', name: '本地项目' }),
      makeProject({
        id: 'p2',
        name: '同步项目',
        repo_url: '',
        multica_project_id: 'mp-2',
        multica_synced_at: '2026-08-22T03:00:05Z',
      }),
    ])
    const screen = await mount()
    await expect.element(screen.getByText('同步项目')).toBeInTheDocument()
    // 只有一张卡（同步进来的那张）带来源徽标（exact：排除「从 Multica 同步」按钮的子串匹配）
    const badges = await screen.getByText('Multica', { exact: true }).all()
    expect(badges).toHaveLength(1)
  })

  it('点「从 Multica 同步」触发 POST，成功后列表刷新出新数据', async () => {
    const before = [makeProject({ id: 'p1', name: '本地项目' })]
    const after = [
      ...before,
      makeProject({
        id: 'p2',
        name: '刚同步的项目',
        repo_url: '',
        multica_project_id: 'mp-2',
        multica_synced_at: '2026-08-22T03:00:05Z',
      }),
    ]
    vi.mocked(listProjects)
      .mockResolvedValueOnce(before)
      .mockResolvedValueOnce(after)
    vi.mocked(triggerMulticaSync).mockResolvedValue({
      started_at: '2026-08-22T03:00:00Z',
      finished_at: '2026-08-22T03:00:05Z',
      projects_imported: 1,
      issues_imported: 3,
      issues_skipped: 0,
      skips: null,
      error: '',
    })
    const screen = await mount()
    await expect
      .element(screen.getByText('本地项目'))
      .toBeInTheDocument()
    await screen.getByRole('button', { name: '从 Multica 同步' }).click()
    await vi.waitFor(() =>
      expect(triggerMulticaSync).toHaveBeenCalledTimes(1)
    )
    await expect
      .element(screen.getByText('刚同步的项目'))
      .toBeInTheDocument()
  })
})
