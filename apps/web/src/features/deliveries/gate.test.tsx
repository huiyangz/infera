import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { getDelivery, getGate } from '@/lib/infera-api'
import type { GateInfo } from '@/lib/infera-types'
import { GatePage } from './gate'

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return {
    ...actual,
    getGate: vi.fn(),
    getDelivery: vi.fn(),
    approveGate: vi.fn(),
    // useLocalNodes 依赖的两条查询（空绑定即可，不触发本机预审入口）
    listAgents: vi.fn().mockResolvedValue([]),
    getProjectPipeline: vi.fn().mockResolvedValue({ nodes: [], bindings: {} }),
  }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  const MockLink = ({
    children,
    to,
    params,
    ...props
  }: React.ComponentProps<'a'> & {
    to?: string
    params?: Record<string, string>
  }) => (
    <a href={(to ?? '#').replace('$id', params?.id ?? '')} {...props}>
      {children}
    </a>
  )
  return {
    ...actual,
    Link: MockLink,
    useNavigate: vi.fn(() => vi.fn()),
  }
})

function gateInfo(over: Partial<GateInfo> = {}): GateInfo {
  return {
    delivery_id: 'd1',
    gate: 'spec_approval',
    agent_output: {
      agent: 'spec-agent',
      output: '## 规格\n\n- 目标一\n- 目标二',
    },
    pr_url: '',
    complexity_suggestion: '',
    ...over,
  }
}

async function mountGate(info: GateInfo) {
  vi.mocked(getGate).mockResolvedValue(info)
  // GatePage 附带拉取 delivery（本机交互通道判定），给最小可用形状
  vi.mocked(getDelivery).mockResolvedValue({
    delivery: {
      id: 'd1',
      project_id: 'p1',
      title: '演示任务',
      description: '',
      status: 'active',
      current_stage: 'spec_approval',
      pending_gate: 'spec_approval',
      fail_count: 0,
      created_at: '2026-08-20T10:00:00Z',
      updated_at: '2026-08-22T10:00:00Z',
      external_issue_id: '',
      external_issue_key: '',
      assignee: '',
      priority: '',
      external_synced_at: null,
      parent_id: '',
      wave: 0,
      split_mode: false,
      merge_state: '',
      complexity: '',
    },
    timeline: [],
    artifacts: [],
    children: [],
  })
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const { SidebarInset, SidebarProvider } = await import(
    '@/components/ui/sidebar'
  )
  return await render(
    <QueryClientProvider client={qc}>
      <SidebarProvider>
        <SidebarInset>
          <GatePage deliveryId='d1' />
        </SidebarInset>
      </SidebarProvider>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await cleanup()
})

describe('GatePage Agent 产出 Markdown 接入（INFERA-296）', () => {
  it('AC1: Agent 产出（Spec/设计文档）按 Markdown 渲染，不再以源码形式平铺', async () => {
    const screen = await mountGate(gateInfo())

    await expect
      .element(screen.getByRole('heading', { level: 2, name: '规格' }))
      .toBeInTheDocument()
    await expect
      .element(screen.getByText('目标一', { exact: true }))
      .toBeInTheDocument()
    // 旧版 <pre> 原样显示源码：字面标题标记不再出现
    expect(await screen.getByText('## 规格', { exact: true }).query()).toBeNull()
  })

  it('Scope: Agent 产出只读——无预览/源码切换、无 Markdown 编辑框、无保存按钮', async () => {
    const screen = await mountGate(gateInfo())

    expect(await screen.getByRole('button', { name: '源码' }).query()).toBeNull()
    expect(
      document.querySelector("textarea[aria-label='Markdown 源码']")
    ).toBeNull()
    expect(await screen.getByRole('button', { name: '保存' }).query()).toBeNull()
  })

  it('无 Agent 产出时保留占位文案，不渲染空文档', async () => {
    const screen = await mountGate(
      gateInfo({ agent_output: null })
    )

    await expect
      .element(screen.getByText('（无 Agent 产出）', { exact: true }))
      .toBeInTheDocument()
  })
})
