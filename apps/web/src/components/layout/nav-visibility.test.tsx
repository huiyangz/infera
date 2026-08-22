import { useEffect } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, type RenderResult } from 'vitest-browser-react'
import { page } from 'vitest/browser'
import { me } from '@/lib/infera-api'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider, useSearch } from '@/context/search-provider'
import { ThemeProvider } from '@/context/theme-provider'
import { SidebarProvider } from '@/components/ui/sidebar'
import { AppSidebar } from './app-sidebar'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return { ...actual, me: vi.fn() }
})

/**
 * 导航入口可见性（INFERA-119 AC2）：「需求」入口从侧边栏与命令菜单移除。
 * 命令菜单与侧边栏共用 sidebarData；此处按用户可见面（组件渲染）断言。
 */

/** 挂载即打开命令菜单（等价用户 ⌘K，避免键盘事件 flake） */
function OpenSearchOnMount() {
  const { setOpen } = useSearch()
  useEffect(() => {
    setOpen(true)
  }, [setOpen])
  return null
}

/** 被测组件放进 root 路由组件内，才有 router 上下文（RouterProvider 不渲染 children） */
async function renderInRouter(node: React.ReactNode): Promise<RenderResult> {
  vi.mocked(me).mockResolvedValue({ logged_in: true })
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>{node}</ThemeProvider>
      </QueryClientProvider>
    ),
  })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  return await render(<RouterProvider router={router} />)
}

function sidebarNode() {
  return (
    <LayoutProvider>
      <SidebarProvider defaultOpen>
        <AppSidebar />
      </SidebarProvider>
    </LayoutProvider>
  )
}

beforeAll(async () => {
  await page.viewport(1280, 720)
})

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await cleanup()
})

describe('导航入口可见性（INFERA-119 AC2）', () => {
  it('AC2-1: 侧边栏无「需求」入口，其余入口不受影响', async () => {
    const screen = await renderInRouter(sidebarNode())

    expect(await screen.getByText('需求', { exact: true }).query()).toBeNull()
    // 其余入口仍在（防止误删整组）
    await expect
      .element(screen.getByText('项目', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('需要决策', { exact: true }))
      .toBeInTheDocument()
  })

  it('AC2-2: 命令菜单无「需求」项', async () => {
    const screen = await renderInRouter(
      <SearchProvider>
        <OpenSearchOnMount />
      </SearchProvider>
    )

    // 菜单已打开的标志：任一导航项出现
    await expect
      .element(screen.getByText('项目', { exact: true }))
      .toBeInTheDocument()

    expect(await screen.getByText('需求', { exact: true }).query()).toBeNull()
  })
})
