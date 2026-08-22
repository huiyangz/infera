import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  ChevronDown,
  ChevronRight,
  GitBranch,
  Inbox,
  Plus,
  SlidersHorizontal,
} from 'lucide-react'
import { toast } from 'sonner'
import {
  createDelivery,
  getProject,
  getProjectPipeline,
  listAgents,
  listProjectDeliveries,
  putProjectPipeline,
} from '@/lib/infera-api'
import {
  type BindingMap,
  type Delivery,
  stageLabel,
} from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { DeliveryDetail } from '@/features/deliveries/delivery-detail'
import { BindingEditor } from '@/features/pipeline/binding-editor'
import { assigneeLabel } from '@/features/multica-sync/display'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * 项目详情 = 主从布局：左侧本项目需求列表（可选中），右侧选中需求详情。
 * 选中态走 URL search param（?d=），可分享、可后退；移动端只显示一栏。
 */
export function ProjectDetail({
  projectId,
  selectedDeliveryId,
}: {
  projectId: string
  selectedDeliveryId?: string
}) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })
  const { data: deliveries, isLoading } = useQuery({
    queryKey: ['project-deliveries', projectId],
    queryFn: () => listProjectDeliveries(projectId),
  })
  const [title, setTitle] = useState('')
  // 拆分父子树的折叠态（per-parent，默认展开）
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

  const create = useMutation({
    mutationFn: () => createDelivery(projectId, { title }),
    onSuccess: (d) => {
      setTitle('')
      toast.success('需求已提交，流水线即将启动')
      qc.invalidateQueries({ queryKey: ['project-deliveries', projectId] })
      navigate({ to: '/projects/$id', params: { id: projectId }, search: { d: d.id } })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  // URL 未指定时默认选中第一条（仅影响渲染，不写 URL）
  const effectiveId = selectedDeliveryId ?? deliveries?.[0]?.id
  const waiting = deliveries?.filter((d) => d.pending_gate).length ?? 0

  return (
    <>
      <Header fixed>
        <div className='flex w-full min-w-0 items-center justify-between gap-4'>
          <div className='flex min-w-0 flex-col gap-1'>
            <div className='flex items-center gap-2 text-sm text-muted-foreground'>
              <Link to='/' className='hover:text-foreground'>
                项目
              </Link>
              <span>/</span>
              <span className='truncate font-medium text-foreground'>
                {proj?.name ?? <Skeleton className='h-4 w-24' />}
              </span>
            </div>
            <p className='flex items-center gap-1.5 font-mono text-xs text-muted-foreground'>
              <GitBranch className='size-3.5 shrink-0' />
              <span className='truncate' title={proj?.repo_url || undefined}>
                {proj?.repo_url || '（未绑仓库）'}
              </span>
              <span className='shrink-0 text-border'>·</span>
              {proj?.default_branch && (
                <span className='shrink-0'>{proj.default_branch}</span>
              )}
            </p>
          </div>
          <div className='flex shrink-0 items-center gap-2'>
            {proj?.repo_url ? (
              <form
                className='flex items-center gap-2'
                onSubmit={(e) => {
                  e.preventDefault()
                  if (title.trim()) create.mutate()
                }}
              >
                <Input
                  className='w-64'
                  placeholder='一句话需求，回车提交…'
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                />
                <Button
                  type='submit'
                  size='lg'
                  disabled={!title.trim() || create.isPending}
                >
                  <Plus /> 新建交付
                </Button>
              </form>
            ) : (
              <p className='text-sm text-muted-foreground'>
                项目未绑定仓库，暂时不支持提交需求
              </p>
            )}
            {proj && (
              <OrchestrationDialog
                projectId={projectId}
                projectName={proj.name}
              />
            )}
          </div>
        </div>
      </Header>

      <div className='flex h-[calc(100svh-4rem)]'>
        {/* 左：需求列表（通栏，hairline 分隔，不做卡片盒） */}
        <aside
          className={cn(
            'flex w-80 shrink-0 flex-col border-r',
            effectiveId && 'max-lg:hidden',
          )}
        >
          <div className='flex h-9 items-center justify-between border-b px-4'>
            <span className='text-xs font-medium uppercase tracking-wider text-muted-foreground'>
              需求
              {deliveries?.length ? (
                <span className='ml-1.5'>
                  {deliveries.length}
                </span>
              ) : null}
            </span>
            {waiting > 0 && (
              <span className='text-xs text-foreground'>{waiting} 个待审批</span>
            )}
          </div>
          <div className='flex-1 overflow-y-auto'>
            {isLoading ? (
              <div className='space-y-2 p-3'>
                <Skeleton className='h-14 w-full' />
                <Skeleton className='h-14 w-full' />
              </div>
            ) : !deliveries?.length ? (
              <div className='flex flex-col items-center gap-2 p-8 text-center'>
                <Inbox className='size-5 text-muted-foreground' />
                <p className='text-sm text-muted-foreground'>
                  还没有需求，右上角输入一句话提交
                </p>
              </div>
            ) : (
              (() => {
                const parents = deliveries.filter((d) => !d.parent_id)
                const childrenOf = (pid: string) =>
                  deliveries.filter((d) => d.parent_id === pid)
                const select = (id: string) =>
                  navigate({
                    to: '/projects/$id',
                    params: { id: projectId },
                    search: { d: id === effectiveId ? undefined : id },
                  })
                return parents.map((p) => {
                  const kids = childrenOf(p.id)
                  const isCollapsed = collapsed.has(p.id)
                  return (
                    <div key={p.id}>
                      <DeliveryRow
                        d={p}
                        active={p.id === effectiveId}
                        onSelect={select}
                        childrenDone={
                          kids.length
                            ? `${kids.filter((c) => c.status === 'completed').length}/${kids.length}`
                            : undefined
                        }
                        conflict={p.split_mode && p.merge_state === 'conflict'}
                        expandable={kids.length > 0}
                        expanded={!isCollapsed}
                        onToggleExpand={
                          kids.length
                            ? () =>
                                setCollapsed((prev) => {
                                  const next = new Set(prev)
                                  if (next.has(p.id)) next.delete(p.id)
                                  else next.add(p.id)
                                  return next
                                })
                            : undefined
                        }
                      />
                      {kids.length > 0 && !isCollapsed &&
                        kids.map((c) => (
                          <ChildDeliveryRow
                            key={c.id}
                            d={c}
                            active={c.id === effectiveId}
                            onSelect={select}
                          />
                        ))}
                    </div>
                  )
                })
              })()
            )}
          </div>
        </aside>

        {/* 右：详情面板（通栏阅读面） */}
        <main
          className={cn(
            'min-w-0 flex-1 overflow-y-auto',
            !effectiveId && 'max-lg:hidden',
          )}
        >
          {effectiveId ? (
            <DeliveryDetail deliveryId={effectiveId} embedded />
          ) : null}
        </main>
      </div>
    </>
  )
}

function DeliveryRow({
  d,
  active,
  onSelect,
  childrenDone,
  conflict,
  expandable,
  expanded,
  onToggleExpand,
}: {
  d: Delivery
  active: boolean
  onSelect: (id: string) => void
  /** 拆分父：「已完成子需求/全部子需求」 */
  childrenDone?: string
  conflict?: boolean
  expandable?: boolean
  expanded?: boolean
  onToggleExpand?: () => void
}) {
  const assignee = assigneeLabel(d.assignee)
  return (
    <div
      className={cn(
        'relative block w-full border-b transition-colors',
        active ? 'bg-accent' : 'hover:bg-accent/50',
      )}
    >
      {active && (
        <span className='absolute inset-y-0 start-0 w-0.5 bg-foreground' />
      )}
      <div className='flex items-start'>
        {expandable ? (
          <button
            type='button'
            aria-label={expanded ? '收起子需求' : '展开子需求'}
            className='flex size-7 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground'
            onClick={(e) => {
              e.stopPropagation()
              onToggleExpand?.()
            }}
          >
            {expanded ? (
              <ChevronDown className='size-3.5' />
            ) : (
              <ChevronRight className='size-3.5' />
            )}
          </button>
        ) : (
          <span className='w-7 shrink-0' />
        )}
        <button
          type='button'
          onClick={() => onSelect(d.id)}
          className='min-w-0 flex-1 py-2.5 pe-4 text-start'
        >
          <span className='flex items-center justify-between gap-2'>
            <span className='flex min-w-0 items-center gap-1.5'>
              {d.multica_issue_id && (
                <span className='shrink-0 rounded-full border px-1.5 text-[10px] leading-4 text-muted-foreground'>
                  Multica
                </span>
              )}
              <span className='truncate text-sm font-medium'>{d.title}</span>
            </span>
            <StatusBadge status={d.status} />
          </span>
          <span className='mt-1 flex items-center gap-2 text-xs text-muted-foreground'>
            {/* 同步镜像无 current_stage：issue key 顶替阶段位展示 */}
            <span>{stageLabel(d.current_stage) || d.multica_issue_key}</span>
            {assignee && <span>· {assignee}</span>}
            {childrenDone && <span>· 子需求 {childrenDone} 完成</span>}
            <span className='ms-auto tabular-nums'>{timeAgo(d.updated_at)}</span>
          </span>
          <span className='mt-1.5 flex items-center gap-1.5'>
            {d.pending_gate && (
              <span className='inline-block rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground'>
                待审批
              </span>
            )}
            {conflict && (
              <span className='inline-block rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground'>
                合并冲突
              </span>
            )}
          </span>
        </button>
      </div>
    </div>
  )
}

/** 子需求行：缩进 + 「子」chip，样式更轻。 */
function ChildDeliveryRow({
  d,
  active,
  onSelect,
}: {
  d: Delivery
  active: boolean
  onSelect: (id: string) => void
}) {
  return (
    <button
      type='button'
      onClick={() => onSelect(d.id)}
      className={cn(
        'relative block w-full border-b px-4 py-2 pl-10 text-start transition-colors',
        active ? 'bg-accent' : 'hover:bg-accent/50',
      )}
    >
      {active && (
        <span className='absolute inset-y-0 start-0 w-0.5 bg-foreground' />
      )}
      <span className='flex items-center justify-between gap-2'>
        <span className='flex min-w-0 items-center gap-1.5'>
          <span className='shrink-0 rounded-full border px-1.5 text-[10px] leading-4 text-muted-foreground'>
            子
          </span>
          {d.multica_issue_id && (
            <span className='shrink-0 rounded-full border px-1.5 text-[10px] leading-4 text-muted-foreground'>
              Multica
            </span>
          )}
          <span className='truncate text-xs font-medium'>{d.title}</span>
        </span>
        <StatusBadge status={d.status} />
      </span>
      <span className='mt-1 flex items-center justify-between text-[11px] text-muted-foreground'>
        <span>
          {d.current_stage
            ? `批次 ${d.wave || 1} · ${stageLabel(d.current_stage)}`
            : d.multica_issue_key}
        </span>
        <span className='tabular-nums'>{timeAgo(d.updated_at)}</span>
      </span>
    </button>
  )
}

/**
 * 项目编排对话框：展示当前生效绑定（默认 + 项目覆盖），可整体另存为项目覆盖，
 * 或「恢复默认」清空全部覆盖（PUT {}）回退全局默认。
 */
function OrchestrationDialog({
  projectId,
  projectName,
}: {
  projectId: string
  projectName: string
}) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  // sel=null 表示未编辑，展示后端当前生效值；用户改动后暂存本地，保存成功即复位
  const [sel, setSel] = useState<BindingMap | null>(null)

  const { data: pipe } = useQuery({
    queryKey: ['project-pipeline', projectId],
    queryFn: () => getProjectPipeline(projectId),
    enabled: open,
  })
  const { data: agents } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(),
    enabled: open,
  })

  // 当前生效值（项目覆盖 ?? 默认）
  const effectiveMap = pipe
    ? Object.fromEntries(
        Object.values(pipe.effective).map((e) => [e.node, e.agent_id]),
      )
    : {}
  const value = sel ?? effectiveMap

  // 只提交与默认不同的节点（未改动的留空 → 继续跟随全局默认）
  const diffOverrides = (bindings: BindingMap): BindingMap =>
    Object.fromEntries(
      Object.entries(bindings).filter(
        ([node, agentId]) => pipe?.defaults[node] !== agentId,
      ),
    )

  const save = useMutation({
    mutationFn: (bindings: BindingMap) =>
      putProjectPipeline(projectId, diffOverrides(bindings)),
    onSuccess: (_d, bindings) => {
      toast.success(
        Object.keys(bindings).length ? '项目编排已保存' : '已恢复默认编排',
      )
      setSel(null)
      qc.invalidateQueries({ queryKey: ['project-pipeline', projectId] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const nodes = pipe?.nodes ?? []
  const fromMap = pipe
    ? Object.fromEntries(
        Object.values(pipe.effective).map((e) => [e.node, e.from]),
      )
    : undefined
  const allChosen = nodes.every((n) => value[n])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant='ghost' size='icon' aria-label='编排'>
          <SlidersHorizontal />
        </Button>
      </DialogTrigger>
      <DialogContent className='max-w-2xl'>
        <DialogHeader>
          <DialogTitle>流水线编排 · {projectName}</DialogTitle>
          <DialogDescription>
            为本项目各节点指定执行 Agent；未保存的节点沿用全局默认
          </DialogDescription>
        </DialogHeader>
        <BindingEditor
          nodes={nodes}
          agents={agents ?? []}
          value={value}
          onChange={setSel}
          showFrom={fromMap}
        />
        <DialogFooter className='sm:justify-between'>
          <Button
            variant='ghost'
            disabled={save.isPending}
            onClick={() => save.mutate({})}
          >
            恢复默认
          </Button>
          <Button
            disabled={save.isPending || !nodes.length || !allChosen}
            onClick={() => save.mutate(value)}
          >
            {save.isPending ? '保存中…' : '保存项目编排'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
