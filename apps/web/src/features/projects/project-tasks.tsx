import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronDown, ChevronRight, Inbox } from 'lucide-react'
import { getProject, listProjectDeliveries } from '@/lib/infera-api'
import { type Delivery, stageLabel } from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { assigneeLabel } from '@/features/multica-sync/display'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * 项目任务列表页（L202608221241-2-T04）：以父子结构只读展示项目下的
 * 需求与任务（父需求 + 子任务，沿用 GET /api/projects/{id}/deliveries），
 * 每条可点击进入需求详情；不提供任何创建/审批等操作。
 */
export function ProjectTasks({ projectId }: { projectId: string }) {
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })
  const { data: deliveries, isLoading } = useQuery({
    queryKey: ['project-deliveries', projectId],
    queryFn: () => listProjectDeliveries(projectId),
  })
  // 拆分父子树的折叠态（per-parent，默认展开）
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

  const parents = (deliveries ?? []).filter((d) => !d.parent_id)
  const childrenOf = (pid: string) =>
    (deliveries ?? []).filter((d) => d.parent_id === pid)

  return (
    <>
      <Header fixed>
        <div className='flex w-full min-w-0 flex-col gap-1'>
          <div className='flex items-center gap-2 text-sm text-muted-foreground'>
            <Link to='/' className='hover:text-foreground'>
              项目
            </Link>
            <span>/</span>
            <Link
              to='/projects/$id'
              params={{ id: projectId }}
              className='truncate hover:text-foreground'
            >
              {proj?.name ?? <Skeleton className='h-4 w-24' />}
            </Link>
            <span>/</span>
            <span className='truncate font-medium text-foreground'>任务</span>
          </div>
          <p className='text-sm text-muted-foreground'>
            本项目的父需求与子任务（只读）
          </p>
        </div>
      </Header>

      <div className='mx-auto w-full max-w-4xl p-6'>
        {isLoading ? (
          <div className='space-y-2'>
            <Skeleton className='h-14 w-full' />
            <Skeleton className='h-14 w-full' />
          </div>
        ) : !parents.length ? (
          <div className='flex flex-col items-center gap-2 p-16 text-center'>
            <Inbox className='size-5 text-muted-foreground' />
            <p className='text-sm text-muted-foreground'>还没有任务</p>
          </div>
        ) : (
          <div className='border-t'>
            {parents.map((p) => {
              const kids = childrenOf(p.id)
              const isCollapsed = collapsed.has(p.id)
              return (
                <div key={p.id}>
                  <TaskRow
                    d={p}
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
                  {kids.length > 0 &&
                    !isCollapsed &&
                    kids.map((c) => <ChildTaskRow key={c.id} d={c} />)}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </>
  )
}

function TaskRow({
  d,
  childrenDone,
  conflict,
  expandable,
  expanded,
  onToggleExpand,
}: {
  d: Delivery
  /** 拆分父：「已完成子任务/全部子任务」 */
  childrenDone?: string
  conflict?: boolean
  expandable?: boolean
  expanded?: boolean
  onToggleExpand?: () => void
}) {
  const assignee = assigneeLabel(d.assignee)
  return (
    <div className='relative block w-full border-b transition-colors hover:bg-accent/50'>
      <div className='flex items-start'>
        {expandable ? (
          <button
            type='button'
            aria-label={expanded ? '收起子任务' : '展开子任务'}
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
        <Link
          to='/deliveries/$id'
          params={{ id: d.id }}
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
            {childrenDone && <span>· 子任务 {childrenDone} 完成</span>}
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
        </Link>
      </div>
    </div>
  )
}

/** 子任务行：缩进 + 「子」chip，样式更轻。 */
function ChildTaskRow({ d }: { d: Delivery }) {
  return (
    <Link
      to='/deliveries/$id'
      params={{ id: d.id }}
      className={cn(
        'relative block w-full border-b px-4 py-2 pl-10 text-start transition-colors hover:bg-accent/50',
      )}
    >
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
    </Link>
  )
}
