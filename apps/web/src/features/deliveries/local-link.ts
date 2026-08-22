/**
 * 本机交互通道（R4）：网页「在本地处理此阶段」按钮 → 本机 infera-link 守护进程
 * （helper/，默认 127.0.0.1:8788）。守护进程经 infera MCP 服务拉取交付上下文，
 * 在 workdir 拉起带 MCP 配置与初始提示的本机 CLI（claude/codex）；产出由 CLI
 * 经 submit_stage_output 交回，流水线自动推进。
 */
import { useQuery } from '@tanstack/react-query'
import { getProjectPipeline, listAgents } from '@/lib/infera-api'

/** 本机守护进程地址（默认与 helper 默认 --listen 一致；可用 VITE_INFERA_LINK_URL 覆盖） */
const LINK_HELPER_URL: string =
  import.meta.env.VITE_INFERA_LINK_URL || 'http://127.0.0.1:8788'

interface HandleResult {
  ok: boolean
  node: string
  workdir: string
  cli: string
  terminal: string
}

/** 触发本机处理。连接失败（helper 未装/未启动）抛带安装指引的错误。 */
export async function handleLocally(deliveryId: string): Promise<HandleResult> {
  let r: Response
  try {
    r = await fetch(`${LINK_HELPER_URL}/handle`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ delivery_id: deliveryId }),
    })
  } catch {
    throw new Error(
      '无法连接本机 infera-link 守护进程——请先安装并启动（见仓库 helper/README.md）'
    )
  }
  const body = await r.json().catch(() => ({}))
  if (!r.ok) {
    throw new Error((body as { error?: string }).error || `HTTP ${r.status}`)
  }
  return body as HandleResult
}

/**
 * 项目编排里绑定为 local runner agent 的节点集合（项目级唯一定义 {nodes, bindings}）。
 * null = 加载中或查询失败（按钮退化为不显示，不影响页面其余功能）。
 */
export function useLocalNodes(projectId: string | undefined) {
  const { data } = useQuery({
    queryKey: ['local-nodes', projectId],
    enabled: !!projectId,
    staleTime: 30_000,
    retry: false,
    queryFn: async () => {
      const [agents, project] = await Promise.all([
        listAgents(),
        getProjectPipeline(projectId!),
      ])
      const runnerOf = new Map(agents.map((a) => [a.id, a.runner]))
      const nodes = new Set<string>()
      for (const [node, agentId] of Object.entries(project.bindings)) {
        if (runnerOf.get(agentId) === 'local') nodes.add(node)
      }
      return nodes
    },
  })
  return data ?? null
}

/** 流水线 agent 节点里可绑 local runner 的停车点（server BindableNodes 的 agent
 * 子集；engine local 停车（submitLocalStage）与 helper stageContract 都覆盖这五个） */
const LOCAL_STAGE_NODES = new Set(['spec', 'design', 'tasks', 'test_gen', 'code_gen'])

/** code_review 门禁可由本机承担的角色（门禁前置预审 + R10 双道审查） */
const LOCAL_REVIEW_NODES = ['code_review', 'spec_conformance', 'code_quality']

/** 交付是否停在本机绑定节点（详情页按钮条件；与 engine.LocalPrompt 的判定对齐） */
export function parkedAtLocalNode(
  delivery: {
    status: string
    current_stage: string
    pending_gate: string | null
    split_mode: boolean
  },
  localNodes: Set<string> | null
): boolean {
  if (!localNodes || delivery.status !== 'active' || delivery.pending_gate)
    return false
  // 拆分父停在 code_gen 是「等子需求/合并」语义，不是本机交互停车
  if (delivery.split_mode && delivery.current_stage === 'code_gen') return false
  return (
    LOCAL_STAGE_NODES.has(delivery.current_stage) &&
    localNodes.has(delivery.current_stage)
  )
}

/** code_review 门禁是否有本机承担的角色（门禁页按钮条件） */
export function gateHasLocalRole(
  gate: string,
  localNodes: Set<string> | null
): boolean {
  if (!localNodes || gate !== 'code_review') return false
  return LOCAL_REVIEW_NODES.some((n) => localNodes.has(n))
}
