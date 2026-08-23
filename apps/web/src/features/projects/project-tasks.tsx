import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  CircleAlert,
  CircleCheck,
  CircleDashed,
  ChevronDown,
  ChevronRight,
  Inbox,
  LoaderCircle,
  type LucideIcon,
} from 'lucide-react'
import { getProject, listProjectTaskGroups } from '@/lib/infera-api'
import {
  type DeliveryStatus,
  type TaskChild,
  type TaskGroupRow,
  type TaskStageGroup,
  stageLabel,
} from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { assigneeLabel } from '@/features/task-sync/display'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import { Skeleton } from '@/components/ui/skeleton'
import { CreateRequirementDialog } from './requirement-create-dialog'

/**
 * 项目任务列表页（INFERA-173 左右分栏 master-detail）：左栏为父级任务列表
 * （每个父任务一条，含无子任务的独立任务；点击选中，aria-current 高亮），
 * 右栏仅渲染选中父任务的父子任务树——父卡片 + 子任务按阶段分组的纵向列表
 * （「子任务 n/n」进度头、「阶段 N」分组标题（stage 0 =「无阶段」）、
 * 缩进的单行子任务行：状态图标 + 粗体 issue key）。默认选中第一个父任务。
 * 唯一数据源 GET /api/projects/{id}/task-groups（契约冻结于
 * server/internal/api/taskgroups.go）；只读，每条可点击进入任务详情。
 * 口径统一为「任务/父任务/子任务」。
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
  // 选中父任务 id（null = 未选择，回退列表第一项）；每个父任务卡片的
  // 子任务折叠态（默认展开）
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())

  const rows = groups ?? []
  const selected = rows.find((g) => g.id === selectedId) ?? rows[0] ?? null

  return (
    <>
      <Header fixed>
        <div className='flex w-full min-w-0 items-start justify-between gap-4'>
          <div className='flex min-w-0 flex-col gap-1'>
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
          <CreateRequirementDialog projectId={projectId} />
        </div>
      </Header>

      <div className='mx-auto w-full max-w-6xl p-6'>
        {isLoading ? (
          <div className='space-y-4'>
            <Skeleton className='h-24 w-full rounded-lg' />
            <Skeleton className='h-24 w-full rounded-lg' />
          </div>
        ) : !rows.length ? (
          <div className='flex flex-col items-center gap-2 p-16 text-center'>
            <Inbox className='size-5 text-muted-foreground' />
            <p className='text-sm text-muted-foreground'>还没有任务</p>
          </div>
        ) : (
          <div className='flex flex-col gap-6 lg:flex-row'>
            {/* 左栏：父任务列表（master）。窄屏堆叠为上列表下详情 */}
            <aside className='w-full shrink-0 lg:w-72 lg:border-e lg:pe-6'>
              <ul className='space-y-1'>
                {rows.map((g) => (
                  <li key={g.id}>
                    <ParentListItem
                      g={g}
                      selected={selected?.id === g.id}
                      onSelect={() => setSelectedId(g.id)}
                    />
                  </li>
                ))}
              </ul>
            </aside>
            {/* 右栏：选中父任务的父子任务树（detail），只含该父任务及其子任务 */}
            <div className='min-w-0 flex-1'>
              {selected && (
                <ParentTaskCard
                  g={selected}
                  expanded={!collapsed.has(selected.id)}
                  onToggle={
                    selected.child_total
                      ? () =>
                          setCollapsed((prev) => {
                            const next = new Set(prev)
                            if (next.has(selected.id)) next.delete(selected.id)
                            else next.add(selected.id)
                            return next
                          })
                      : undefined
                  }
                />
              )}
            </div>
          </div>
        )}
      </div>
    </>
  )
}

/**
 * 左栏父任务列表项：标题 + 子任务进度计数（无子任务的独立任务不显示计数）。
 * 选中态以 aria-current + 背景高亮标识；状态/阶段/来源等信息由右栏选中
 * 卡片承载，列表保持最简墨水（DESIGN.md 单色语言）。
 */
function ParentListItem({
  g,
  selected,
  onSelect,
}: {
  g: TaskGroupRow
  selected: boolean
  onSelect: () => void
}) {
  return (
    <button
      type='button'
      aria-current={selected ? 'true' : undefined}
      onClick={onSelect}
      className={cn(
        'flex w-full flex-col gap-1 rounded-md px-3 py-2 text-start transition-colors hover:bg-accent/50',
        selected ? 'bg-accent' : undefined
      )}
    >
      <span className='truncate text-sm'>{g.title}</span>
      {g.child_total > 0 && (
        <span className='text-xs tabular-nums text-muted-foreground'>
          子任务 {g.child_completed}/{g.child_total}
        </span>
      )}
    </button>
  )
}

/**
 * 父任务卡片：卡片头（标题链接 + 状态徽标 + 阶段/负责人/时间）+
 * 子任务区（「子任务 n/n」进度头 + 按阶段分组的缩进行列表），可整卡收起子任务。
 * 来源徽标已移除（INFERA-194）：同步来源不做用户可见展示，
 * 子任务行仍以粗体 issue key 标识。
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
              <span className='truncate text-sm font-medium'>{g.title}</span>
            </span>
            <StatusBadge status={g.status} />
          </span>
          <span className='mt-1 flex items-center gap-2 text-xs text-muted-foreground'>
            {/* 同步镜像无 current_stage：issue key 顶替阶段位展示 */}
            <span>
              {stageLabel(g.current_stage) || g.external_issue_key || '—'}
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
          {/* 子任务进度头：标签 + n/n 计数；hairline 轨道 + 墨色填充（DESIGN.md 单色语言） */}
          <div className='flex items-baseline justify-between'>
            <span className='text-xs font-medium'>子任务</span>
            <span className='text-xs tabular-nums text-muted-foreground'>
              {g.child_completed}/{g.child_total}
            </span>
          </div>
          <div className='mt-2 h-1 overflow-hidden rounded-full bg-muted'>
            <div
              className='h-full rounded-full bg-foreground'
              style={{
                width: `${Math.round((g.child_completed / g.child_total) * 100)}%`,
              }}
            />
          </div>

          {expanded &&
            g.stages.map((s, i) => (
              <StageGroup key={s.stage} group={s} separated={i > 0} />
            ))}
        </div>
      )}
    </article>
  )
}

/** 一个阶段（批次）组：组头（「阶段 N」标题；stage 0 = 同步镜像无阶段子任务，
 *  后端契约排于编号阶段之后）+ 组内缩进的子任务行列表 */
function StageGroup({
  group,
  separated,
}: {
  group: TaskStageGroup
  separated?: boolean
}) {
  return (
    <section className={cn('mt-3', separated && 'border-t pt-3')}>
      <h4 className='text-xs font-medium'>
        {group.stage === 0 ? '无阶段' : `阶段 ${group.stage}`}
      </h4>
      <ul className='mt-1 space-y-0.5'>
        {group.tasks.map((t) => (
          <li key={t.id}>
            <ChildTaskRow d={t} />
          </li>
        ))}
      </ul>
    </section>
  )
}

/**
 * 子任务行：单行式（对齐参考图）——状态图标 + 粗体 issue key + 标题 +
 * 相对时间；行内容缩进于阶段标题之下（ps-6）。
 */
function ChildTaskRow({ d }: { d: TaskChild }) {
  const key = d.external_issue_key
  return (
    <Link
      to='/deliveries/$id'
      params={{ id: d.id }}
      className='flex items-center gap-2 rounded-md px-2 py-1.5 ps-6 transition-colors hover:bg-accent/50'
    >
      <TaskStatusIcon status={d.status} />
      <span
        className={cn(
          'min-w-0 flex-1 truncate text-xs',
          key ? 'text-muted-foreground' : undefined
        )}
      >
        {key && (
          <span className='font-medium text-foreground'>{key}&nbsp;</span>
        )}
        <span>{d.title}</span>
        {d.pending_gate && <span> · 待审批</span>}
      </span>
      <span className='shrink-0 text-[11px] tabular-nums text-muted-foreground'>
        {timeAgo(d.updated_at)}
      </span>
    </Link>
  )
}

/**
 * 子任务行状态图标四态查表（单色语言，语义与 StatusBadge 一致）：
 * 已完成=墨色实勾；进行中=旋转加载圈；已阻塞=墨色警示圈；未启动=灰虚线圈。
 * 图标/文案/样式一处声明，消除逐态重复 JSX；Record 按状态并集穷尽，无兜底分支。
 */
const TASK_STATUS_ICON: Record<
  DeliveryStatus,
  { icon: LucideIcon; label: string; className: string }
> = {
  completed: {
    icon: CircleCheck,
    label: '已完成',
    className: 'size-3.5 shrink-0 text-foreground',
  },
  active: {
    icon: LoaderCircle,
    label: '进行中',
    className: 'size-3.5 shrink-0 animate-spin text-foreground',
  },
  blocked: {
    icon: CircleAlert,
    label: '已阻塞',
    className: 'size-3.5 shrink-0 text-foreground',
  },
  queued: {
    icon: CircleDashed,
    label: '未启动',
    className: 'size-3.5 shrink-0 text-muted-foreground',
  },
}

/** 子任务行状态图标：按 status 查表单一渲染点（a11y 与样式口径见表项） */
function TaskStatusIcon({ status }: { status: DeliveryStatus }) {
  const { icon: Icon, label, className } = TASK_STATUS_ICON[status]
  return <Icon role='img' aria-label={label} className={className} />
}
