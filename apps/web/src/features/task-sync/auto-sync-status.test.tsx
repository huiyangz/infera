import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import {
  getTaskSyncStatus,
  triggerTaskSync,
  type TaskSyncResult,
  type TaskSyncStatus,
} from './api'
import { AutoSyncStatus } from './auto-sync-status'

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
    getTaskSyncStatus: vi.fn(),
    triggerTaskSync: vi.fn(),
  }
})

function result(over: Partial<TaskSyncResult> = {}): TaskSyncResult {
  return {
    started_at: '2026-08-23T03:00:00Z',
    finished_at: '2026-08-23T03:00:05Z',
    projects_imported: 2,
    issues_imported: 5,
    issues_skipped: 1,
    skips: [{ external_issue_id: 'm9', issue_key: 'AUTO-1', reason: 'smoke' }],
    error: '',
    ...over,
  }
}

const T1 = '2026-08-23T03:00:05Z'
const T2 = '2026-08-23T04:00:05Z'
function statusOf(over: Partial<TaskSyncStatus> = {}): TaskSyncStatus {
  return { lastSyncAt: T1, status: 'success', error: '', ...over }
}

// 失效探针：挂在同步影响的视图查询 key 上，断言新一轮同步完成后列表被刷新
const probeProjects = vi.fn().mockResolvedValue([])
const probeTaskGroups = vi.fn().mockResolvedValue([])
const probeRequirements = vi.fn().mockResolvedValue([])
const probeDecisions = vi.fn().mockResolvedValue([])
function Probes() {
  useQuery({ queryKey: ['projects'], queryFn: probeProjects })
  useQuery({ queryKey: ['project-task-groups', 'p1'], queryFn: probeTaskGroups })
  useQuery({ queryKey: ['requirements'], queryFn: probeRequirements })
  useQuery({ queryKey: ['pending-decisions'], queryFn: probeDecisions })
  return null
}

async function mount() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return await render(
    <QueryClientProvider client={qc}>
      {/* pollMs 压到 50ms：让「轮询发现新一轮完成 → 自动刷新」在测试里秒级可观察 */}
      <AutoSyncStatus pollMs={50} />
      <Probes />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getTaskSyncStatus).mockResolvedValue(statusOf())
})

afterEach(async () => {
  await cleanup()
})

describe('AutoSyncStatus 中性展示（INFERA-194：不暴露同步机制，时间类信息以「最近活动」呈现）', () => {
  it('无任何手动触发即自动展示「最近活动 …」，页面无「同步」字样，且不发 POST', async () => {
    const screen = await mount()
    await expect
      .element(screen.getByText(/最近活动/))
      .toBeInTheDocument()
    expect(await screen.getByText(/同步|multica/i).query()).toBeNull()
    await vi.waitFor(() => expect(getTaskSyncStatus).toHaveBeenCalled())
    expect(triggerTaskSync).not.toHaveBeenCalled()
  })

  it('lastSyncAt=null（从未完成过同步）展示「暂无活动」', async () => {
    vi.mocked(getTaskSyncStatus).mockResolvedValue(
      statusOf({ lastSyncAt: null, status: 'idle' }),
    )
    const screen = await mount()
    await expect
      .element(screen.getByText('暂无活动'))
      .toBeInTheDocument()
  })

  it('status=running 展示「正在更新…」，刷新入口禁用防重复触发', async () => {
    vi.mocked(getTaskSyncStatus).mockResolvedValue(
      statusOf({ status: 'running' }),
    )
    const screen = await mount()
    await expect
      .element(screen.getByText('正在更新…'))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('button', { name: '刷新数据' }))
      .toBeDisabled()
  })

  it('status=error 展示「上次更新失败」，后端原始错误文案（含上游细节）不外露', async () => {
    vi.mocked(getTaskSyncStatus).mockResolvedValue(
      statusOf({ status: 'error', error: '上游 502: 拉取任务列表失败' }),
    )
    const screen = await mount()
    await expect
      .element(screen.getByText('上次更新失败'))
      .toBeInTheDocument()
    expect(
      await screen.getByText('上游 502: 拉取任务列表失败').query(),
    ).toBeNull()
  })

  it('轮询发现新一轮同步完成（lastSyncAt 变化）→ 自动失效 projects / requirements 等视图查询', async () => {
    vi.mocked(getTaskSyncStatus)
      .mockResolvedValueOnce(statusOf({ lastSyncAt: T1 }))
      .mockResolvedValue(statusOf({ lastSyncAt: T2 }))
    await mount()
    // 初始挂载 1 次；轮询拿到 T2（新完成一轮）→ 失效 → 探针重拉（2 次）
    await vi.waitFor(() => expect(probeProjects).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(probeRequirements).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(probeDecisions).toHaveBeenCalledTimes(2))
    await vi.waitFor(() => expect(probeTaskGroups).toHaveBeenCalledTimes(2))
    // 全程无任何手动触发
    expect(triggerTaskSync).not.toHaveBeenCalled()
  })

  it('低调手动刷新补充入口：点击 POST，成功反馈更新计数并刷新数据视图', async () => {
    vi.mocked(getTaskSyncStatus)
      .mockResolvedValueOnce(statusOf({ lastSyncAt: T1 }))
      .mockResolvedValue(statusOf({ lastSyncAt: T2 }))
    vi.mocked(triggerTaskSync).mockResolvedValue(result())
    const screen = await mount()
    await screen.getByRole('button', { name: /刷新/ }).click()
    await vi.waitFor(() => expect(triggerTaskSync).toHaveBeenCalledTimes(1))
    await vi.waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        expect.stringContaining('数据已更新'),
      ),
    )
    expect(toastSuccess).toHaveBeenCalledWith(
      expect.stringContaining('2 个项目 / 5 条任务'),
    )
    expect(toastSuccess).toHaveBeenCalledWith(
      expect.stringContaining('跳过 1 条'),
    )
    await vi.waitFor(() => expect(probeProjects).toHaveBeenCalledTimes(2))
  })

  it('手动刷新失败：toast 给中性失败提示，不透传后端同步相关文案', async () => {
    vi.mocked(triggerTaskSync).mockRejectedValue(
      new Error('任务同步未装配（需配置 TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）'),
    )
    const screen = await mount()
    await screen.getByRole('button', { name: /刷新/ }).click()
    await vi.waitFor(() =>
      expect(toastError).toHaveBeenCalledWith('刷新失败，请稍后重试'),
    )
    expect(toastError).not.toHaveBeenCalledWith(
      expect.stringContaining('未装配'),
    )
    expect(toastSuccess).not.toHaveBeenCalled()
  })
})
