import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { page } from 'vitest/browser'
import '@/styles/index.css'
import { listProjects, me } from '@/lib/infera-api'
import { routeTree } from '@/routeTree.gen'
import { getWorkspaceStats } from './api'
import type { WorkspaceStatsResponse } from './types'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  // me：路由守卫；listProjects：起始路由 / 渲染项目列表（与
  // discovery-routing.test 同构，避免真实 fetch 打到未起的后端）
  return { ...actual, me: vi.fn(), listProjects: vi.fn() }
})

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return { ...actual, getWorkspaceStats: vi.fn() }
})

/** 路由接线集成测试（与 discovery-routing.test 同构）：挂真实 routeTree.gen
 *  生成的 router，验证侧边栏「统计」入口点击可达 /stats 独立路由并渲染统计页。
 */
async function renderApp(initialPath: string) {
  vi.mocked(me).mockResolvedValue({ logged_in: true })
  vi.mocked(listProjects).mockResolvedValue([])
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const router = createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  })
  const screen = await render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  return { screen, router }
}

function payload(): WorkspaceStatsResponse {
  return {
    window: { from: '2026-08-18T12:00:00Z', to: '2026-08-25T12:00:00Z' },
    timezone: 'Asia/Shanghai',
    task_status: {
      total: 12,
      done: 5,
      in_progress: 3,
      todo: 2,
      cancelled: 2,
      by_status: { active: 3, queued: 1, blocked: 1, completed: 5, cancelled: 2 },
    },
    execution: {
      runs_total: 6,
      running: 1,
      done: 4,
      failed: 1,
      duration_ms_total: 45_340_000,
    },
    hourly: Array.from({ length: 24 }, (_, hour) => ({
      hour,
      runs: hour === 2 ? 3 : 0,
      duration_ms: hour === 2 ? 5_400_000 : 0,
    })),
  }
}

// 桌面视口：侧边栏常驻渲染（窄屏下 AppSidebar 走 Sheet 门户，链接不可达）
beforeAll(async () => {
  await page.viewport(1280, 720)
})

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getWorkspaceStats).mockResolvedValue(payload())
})

afterEach(async () => {
  await cleanup()
})

describe('统计页路由接线（AC：从主导航可进入「统计」页）', () => {
  it('侧边栏含「统计」入口，指向 /stats，点击进入独立路由页', async () => {
    const { screen, router } = await renderApp('/')

    const entry = screen.getByRole('link', { name: '统计' })
    await expect.element(entry).toBeInTheDocument()
    expect((await entry.element()).getAttribute('href')).toBe('/stats')

    await router.navigate({ to: '/stats' })

    await vi.waitFor(() => {
      expect(screen.container.querySelector('[role="img"] svg')).not.toBeNull()
    })
    await expect.element(screen.getByText('任务总数')).toBeInTheDocument()
  })

  it('直开 /stats 同样渲染统计页（非仅导航可达）', async () => {
    const { screen } = await renderApp('/stats')

    await expect.element(screen.getByText('任务总数')).toBeInTheDocument()
  })
})
