import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { listProjects } from '@/lib/infera-api'
import type { Project } from '@/lib/infera-types'
import { getMergePolicy, setMergePolicy } from './api'
import { MergePolicySettings } from './merge-policy'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return { ...actual, listProjects: vi.fn() }
})

vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return {
    ...actual,
    getMergePolicy: vi.fn(),
    setMergePolicy: vi.fn(),
  }
})

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

async function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return await render(
    <QueryClientProvider client={qc}>
      <MergePolicySettings />
    </QueryClientProvider>
  )
}

/** 打开对话框并选择项目（默认选第一个可选项） */
async function openAndPick(screen: Awaited<ReturnType<typeof mount>>, name: string) {
  await screen.getByRole('button', { name: '合并策略' }).click()
  await screen.getByRole('combobox').click()
  await screen.getByRole('option', { name }).click()
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listProjects).mockResolvedValue([
    makeProject('p1', '官网'),
    makeProject('p2', '后台'),
  ])
})

afterEach(async () => {
  await cleanup()
})

describe('MergePolicySettings 项目合并策略设置（FR-6）', () => {
  it('读取：选定项目后展示后端当前策略（threshold 200）', async () => {
    vi.mocked(getMergePolicy).mockResolvedValue({
      mode: 'threshold',
      diff_line_threshold: 200,
    })
    const screen = await mount()
    await openAndPick(screen, '官网')
    await vi.waitFor(() => expect(getMergePolicy).toHaveBeenCalledWith('p1'))
    await expect
      .element(screen.getByText('阈值混合', { exact: true }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByRole('spinbutton', { name: 'diff 行数阈值' }))
      .toHaveValue(200)
  })

  it('写入：切到手动档保存 → PUT {manual, 0}（manual 档不携带阈值）', async () => {
    vi.mocked(getMergePolicy).mockResolvedValue({
      mode: 'auto_pass',
      diff_line_threshold: 0,
    })
    vi.mocked(setMergePolicy).mockResolvedValue({
      mode: 'manual',
      diff_line_threshold: 0,
    })
    const screen = await mount()
    await openAndPick(screen, '官网')
    await expect
      .element(screen.getByText('PASS 自动', { exact: true }))
      .toBeInTheDocument()
    await screen.getByRole('radio', { name: /手动合并/ }).click()
    await screen.getByRole('button', { name: '保存策略' }).click()
    await vi.waitFor(() =>
      expect(setMergePolicy).toHaveBeenCalledWith('p1', {
        mode: 'manual',
        diff_line_threshold: 0,
      })
    )
  })

  it('threshold 档校验：阈值为 0 时保存禁用，改为正数后 PUT 携带阈值', async () => {
    vi.mocked(getMergePolicy).mockResolvedValue({
      mode: 'manual',
      diff_line_threshold: 0,
    })
    vi.mocked(setMergePolicy).mockResolvedValue({
      mode: 'threshold',
      diff_line_threshold: 150,
    })
    const screen = await mount()
    await openAndPick(screen, '官网')
    await screen.getByRole('radio', { name: /阈值混合/ }).click()
    // 未填阈值（0）时不可保存
    const save = screen.getByRole('button', { name: '保存策略' })
    await expect.element(save).toBeDisabled()
    await screen.getByRole('spinbutton', { name: 'diff 行数阈值' }).fill('150')
    await expect.element(save).toBeEnabled()
    await screen.getByRole('button', { name: '保存策略' }).click()
    await vi.waitFor(() =>
      expect(setMergePolicy).toHaveBeenCalledWith('p1', {
        mode: 'threshold',
        diff_line_threshold: 150,
      })
    )
  })

  it('未选项目时保存禁用（策略按项目生效）', async () => {
    const screen = await mount()
    await screen.getByRole('button', { name: '合并策略' }).click()
    await expect
      .element(screen.getByRole('button', { name: '保存策略' }))
      .toBeDisabled()
  })
})
