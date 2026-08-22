import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronDown, ChevronRight, Inbox } from 'lucide-react'
import { getProject, listProjectTaskGroups } from '@/lib/infera-api'
import {
  type TaskChild,
  type TaskGroupRow,
  type TaskStageGroup,
  stageLabel,
} from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { assigneeLabel } from '@/features/multica-sync/display'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * 项目任务列表页（L202608221704-2-T02）：父任务卡片 + 子任务按阶段分组，
 * Multica 式父子展示。唯一数据源 GET /api/projects/{id}/task-groups
 * （契约冻结于 server/internal/api/taskgroups.go）；只读，每条可点击进入
 * 任务详情。口径统一为「任务/父任务/子任务」，界面不出现「需求」。
 */
export function ProjectTasks({ projectId }: { projectId: string }) {
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })
  const { data: groups, isLoading } = useQuery({
    queryKey: ['project-task-groups', projectId],
    queryFn: () => listProjectTaskGroups(projectId),
  })
  // 每个父任务卡片的子任务折叠态（默认展开）
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

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
            本项目的父任务与子任务，子任务按阶段分组（只读）
          </p>
        </div>
      </Header>

      <div className='mx-auto w-full max-w-4xl space-y-4 p-6'>
        {isLoading ? (
          <div className='space-y-4'>
            <Skeleton className='h-24 w-full rounded-lg' />
            <Skeleton className='h-24 w-full rounded-lg' />
          </div>
        ) : !groups?.length ? (
          <div className='flex flex-col items-center gap-2 p-16 text-center'>
            <Inbox className='size-5 text-muted-foreground' />
            <p className='text-sm text-muted-foreground'>还没有任务</p>
          </div>
        ) : (
          groups.map((g) => {
            const isCollapsed = collapsed.has(g.id)
            return (
              <ParentTaskCard
                key={g.id}
                g={g}
                expanded={!isCollapsed}
                onToggle={
                  g.child_total
                    ? () =>
                        setCollapsed((prev) => {
                          const next = new Set(prev)
                          if (next.has(g.id)) next.delete(g.id)
                          else next.add(g.id)
                          return next
                        })
                    : undefined
                }
              />
            )
          })
        )}
      </div>
    </>
  )
}

/** 来源标识小徽：multica 同步行专用 */
function MulticaChip() {
  return (
    <span className='shrink-0 rounded-full border px-1.5 text-[10px] leading-4 text-muted-foreground'>
      Multica
    </span>
  )
}

/**
 * 父任务卡片：卡片头（标题链接 + 状态徽标 + 阶段/负责人/时间）+
 * 子任务区（进度条 + 按阶段分组的子任务列表），可整卡收起子任务。
 */
function ParentTaskCard({
  g,
  expanded,
  onToggle,
}: {
  g: TaskGroupRow
  expanded: boolean
  onToggle?: () => void
}) {
  const assignee = assigneeLabel(g.assignee)
  const hasChildren = g.child_total > 0
  return (
    <article className='rounded-lg border bg-card'>
      <div className='flex items-start'>
        {hasChildren ? (
          <button
            type='button'
            aria-label={expanded ? '收起子任务' : '展开子任务'}
            className='flex size-9 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground'
            onClick={onToggle}
          >
            {expanded ? (
              <ChevronDown className='size-4' />
            ) : (
              <ChevronRight className='size-4' />
            )}
          </button>
        ) : (
          <span className='w-9 shrink-0' />
        )}
        <Link
          to='/deliveries/$id'
          params={{ id: g.id }}
          className='min-w-0 flex-1 py-3 pe-4 text-start'
        >
          <span className='flex items-center justify-between gap-2'>
            <span className='flex min-w-0 items-center gap-1.5'>
              {g.multica_issue_id && <MulticaChip />}
              <span className='truncate text-sm font-medium'>{g.title}</span>
            </span>
            <StatusBadge status={g.status} />
          </span>
          <span className='mt-1 flex items-center gap-2 text-xs text-muted-foreground'>
            {/* 同步镜像无 current_stage：issue key 顶替阶段位展示 */}
            <span>
              {stageLabel(g.current_stage) || g.multica_issue_key || '—'}
            </span>
            {assignee && <span>· {assignee}</span>}
            <span className='ms-auto tabular-nums'>
              {timeAgo(g.updated_at)}
            </span>
          </span>
          {(g.pending_gate || (g.split_mode && g.merge_state === 'conflict')) && (
            <span className='mt-1.5 flex items-center gap-1.5'>
              {g.pending_gate && (
                <span className='inline-block rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground'>
                  待审批
                </span>
              )}
              {g.split_mode && g.merge_state === 'conflict' && (
                <span className='inline-block rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground'>
                  合并冲突
                </span>
              )}
            </span>
          )}
        </Link>
      </div>

      {hasChildren && (
        <div className='border-t px-4 py-3'>
          {/* 子任务进度：hairline 轨道 + 墨色填充（DESIGN.md 单色语言） */}
          <div className='flex items-center gap-3'>
            <div className='h-1 min-w-16 flex-1 overflow-hidden rounded-full bg-muted'>
              <div
                className='h-full rounded-full bg-foreground'
                style={{
                  width: `${Math.round((g.child_completed / g.child_total) * 100)}%`,
                }}
              />
            </div>
            <span className='shrink-0 text-xs tabular-nums text-muted-foreground'>
              子任务 {g.child_completed}/{g.child_total}
            </span>
          </div>

          {expanded &&
            g.stages.map((s) => <StageGroup key={s.stage} group={s} />)}
        </div>
      )}
    </article>
  )
}

/** 一个阶段（批次）组：组头（阶段号 + 任务计数）+ 组内子任务行列表 */
function StageGroup({ group }: { group: TaskStageGroup }) {
  return (
    <section className='mt-3'>
      <h4 className='text-[11px] font-medium tracking-wide text-muted-foreground'>
        阶段 {group.stage} · {group.tasks.length} 个子任务
      </h4>
      <ul className='mt-1.5 overflow-hidden rounded-md border'>
        {group.tasks.map((t) => (
          <li key={t.id}>
            <ChildTaskRow d={t} />
          </li>
        ))}
      </ul>
    </section>
  )
}

/** 子任务行：列表行式（hairline 分隔），状态徽标 + 阶段位 + 时间 */
function ChildTaskRow({ d }: { d: TaskChild }) {
  const assignee = assigneeLabel(d.assignee)
  return (
    <Link
      to='/deliveries/$id'
      params={{ id: d.id }}
      className={cn(
        'flex items-center justify-between gap-2 border-b px-3 py-2 text-start',
        'transition-colors last:border-b-0 hover:bg-accent/50'
      )}
    >
      <span className='flex min-w-0 flex-col gap-0.5'>
        <span className='flex min-w-0 items-center gap-1.5'>
          {d.multica_issue_id && <MulticaChip />}
          <span className='truncate text-xs font-medium'>{d.title}</span>
        </span>
        <span className='flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground'>
          {/* 有 current_stage 展示阶段 label；同步镜像以 issue key 顶替 */}
          <span>
            {stageLabel(d.current_stage) || d.multica_issue_key || '—'}
          </span>
          {d.pending_gate && <span>· 待审批</span>}
          {assignee && <span className='truncate'>· {assignee}</span>}
        </span>
      </span>
      <span className='flex shrink-0 items-center gap-2'>
        <span className='text-[11px] tabular-nums text-muted-foreground'>
          {timeAgo(d.updated_at)}
        </span>
        <StatusBadge status={d.status} />
      </span>
    </Link>
  )
}
