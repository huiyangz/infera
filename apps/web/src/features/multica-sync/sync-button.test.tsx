import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import {
  getMulticaSyncStatus,
  triggerMulticaSync,
  type MulticaSyncResult,
  type MulticaSyncStatus,
} from './api'
import { MulticaSyncButton } from './sync-button'

const toastSuccess = vi.fn()
const toastError = vi.fn()
vi.mock('sonner', () => ({
  toast: {
    success: (...args: unknown[]) => toastSuccess(...args),
    error: (...args: unknown[]) => toastError(...args),
  },
}))

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    getMulticaSyncStatus: vi.fn(),
    triggerMulticaSync: vi.fn(),
  }
})

function result(over: Partial<MulticaSyncResult> = {}): MulticaSyncResult {
  return {
    started_at: '2026-08-22T03:00:00Z',
    finished_at: '2026-08-22T03:00:05Z',
    projects_imported: 2,
    issues_imported: 5,
    issues_skipped: 1,
    skips: [{ multica_issue_id: 'm9', issue_key: 'AUTO-1', reason: 'smoke' }],
    error: '',
    ...over,
  }
}

function status(over: Partial<MulticaSyncStatus> = {}): MulticaSyncStatus {
  return { running: false, last: null, ...over }
}

// 失效探针：挂在同于页面的 query key 上，断言同步成功后列表查询被刷新
const probeProjects = vi.fn().mockResolvedValue([])
const probeDeliveries = vi.fn().mockResolvedValue([])
function Probes() {
  useQuery({ queryKey: ['projects'], queryFn: probeProjects })
  useQuery({ queryKey: ['project-deliveries'], queryFn: probeDeliveries })
  return null
}

async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return await render(
    <QueryClientProvider client={qc}>
      <MulticaSyncButton />
      <Probes />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getMulticaSyncStatus).mockResolvedValue(status())
})

afterEach(async () => {
  await cleanup()
})

describe('MulticaSyncButton 同步入口', () => {
  it('空闲态：提示「从未同步」（GET last=null 的既定语义），按钮可点', async () => {
    const screen = await mount()
    await expect.element(screen.getByText('从未同步')).toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '从 Multica 同步' }))
      .toBeEnabled()
  })

  it('进行中可感知：GET running=true 时按钮禁用并显示进行中', async () => {
    vi.mocked(getMulticaSyncStatus).mockResolvedValue(status({ running: true }))
    const screen = await mount()
    const btn = screen.getByRole('button', { name: /同步进行中/ })
    await expect.element(btn).toBeDisabled()
  })

  it('点击触发 POST：成功反馈导入计数（含跳过），并失效 projects / project-deliveries 刷新列表', async () => {
    vi.mocked(triggerMulticaSync).mockResolvedValue(result())
    const screen = await mount()
    await screen.getByRole('button', { name: '从 Multica 同步' }).click()
    await vi.waitFor(() => expect(triggerMulticaSync).toHaveBeenCalledTimes(1))
    await vi.waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        expect.stringContaining('导入 2 个项目 / 5 条任务')
      )
    )
    expect(toastSuccess).toHaveBeenCalledWith(
      expect.stringContaining('跳过 1 条')
    )
    // 两个列表查询 key 被失效 → 活跃探针重新拉取（初始 1 次 + 失效 1 次）
    await vi.waitFor(() => expect(probeProjects).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(probeDeliveries).toHaveBeenCalledTimes(2))
  })

  it('成功后展示最近一轮结果（上次同步计数）', async () => {
    vi.mocked(getMulticaSyncStatus).mockResolvedValue(
      status({ last: result({ issues_skipped: 0, skips: null }) })
    )
    const screen = await mount()
    await expect
      .element(screen.getByText(/上次同步/))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText(/导入 2 个项目 \/ 5 条任务/))
      .toBeInTheDocument()
  })

  it('失败反馈：透传后端错误文案（409 运行中 / 502 上游 / 503 未装配同道）', async () => {
    vi.mocked(triggerMulticaSync).mockRejectedValue(
      new Error('multica 同步未装配（需配置 MULTICA_SERVER_URL / MULTICA_TOKEN / MULTICA_WORKSPACE_ID）')
    )
    const screen = await mount()
    await screen.getByRole('button', { name: '从 Multica 同步' }).click()
    await vi.waitFor(() =>
      expect(toastError).toHaveBeenCalledWith(
        expect.stringContaining('未装配')
      )
    )
    expect(toastSuccess).not.toHaveBeenCalled()
  })
})
