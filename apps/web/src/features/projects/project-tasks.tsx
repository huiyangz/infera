import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  CircleAlert,
  CircleCheck,
  CircleDashed,
  CircleOff,
  ChevronDown,
  ChevronRight,
  Inbox,
  LoaderCircle,
  type LucideIcon,
} from 'lucide-react'
import { getProject, listProjectTaskGroups } from '@/lib/infera-api'
import {
  type DeliveryLabel,
  type DeliveryStatus,
  type TaskChild,
  type TaskGroupRow,
  type TaskStageGroup,
  stageLabel,
} from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { assigneeLabel } from '@/features/task-sync/display'
import { LabelChipRow } from '@/components/label-chip'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import { Skeleton } from '@/components/ui/skeleton'
import { ProjectTabs } from './dashboard/project-tabs'
import { CreateRequirementDialog } from './requirement-create-dialog'

/**
 * 本视图不渲染的标签名（INFERA-261）：「情报」「候选」是需求挖掘域的分类
 * 口径，属于发现页；项目任务页只讲任务，两类标签在这里是噪音，直接不渲染
 * （不加开关/配置项）。按名匹配，任务本体与计数不受影响。
 */
const HIDDEN_LABEL_NAMES = new Set(['情报', '候选'])

/** 过滤掉挖掘域标签后的可见标签：全被滤掉时给空数组（chip 行渲染 null，不占位） */
function visibleLabels(labels?: DeliveryLabel[]): DeliveryLabel[] {
  return (labels ?? []).filter((l) => l && !HIDDEN_LABEL_NAMES.has(l.name))
}

/**
 * 项目任务列表页（INFERA-173 左右分栏 master-detail）：左栏为主/子任务树
 * （INFERA-229）——每个父任务一条（含无子任务的独立任务），其子任务以
 * 缩进 + 竖向连线的紧凑行组挂在父行之下；主/子以图标区分（父行状态图标
 * 嵌于 hairline 方形标识位，子行为裸图标），父子行均带状态图标。点击任一
 * 行选中其父任务（aria-current 高亮），右栏渲染选中父任务的父子任务树——
 * 父卡片 + 子任务按阶段分组的纵向列表（「子任务 n/n」进度头、「阶段 N」
 * 分组标题（stage 0 =「无阶段」）、缩进的单行子任务行：状态图标 + 粗体
 * issue key）。默认选中第一个父任务。
 * 滚动骨架（INFERA-261）：整页锁一屏，只有左右两栏各自内部滚动——tab 头
 * 与右栏不随左侧列表滚动移动。标签口径同需求：本视图不渲染「情报」「候选」。
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
    // 页面骨架（INFERA-261）：整页锁定一屏（头部 → tab 头 → 内容区），文档
    // 不再滚动；滚动收敛进左右两栏各自内部，tab 头与右栏不随列表滚动移动。
    // 锁高方式（INFERA-271）：根节点 data-layout='fixed' 触发
    // AuthenticatedLayout 的 SidebarInset 高度锁（has-data-[layout=fixed]:
    // h-svh；inset 变体下 calc(100svh - 1rem) 抵掉 peer m-2 的上下 16px
    // 边距），本根节点以 h-full 填满锁高——页自身再写死 h-svh 会与 inset
    // 边距叠加撑出 16px 文档级滚动（应用默认 DEFAULT_VARIANT = 'inset'）
    <div data-layout='fixed' className='flex h-full flex-col'>
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

      {/* 项目域页内一级导航（INFERA-248）：任务页与总览页共用同一条 tab，
          「项目任务」为当前页——否则任务页无法切回总览。
          骨架内位于内容区之前，随页钉住不参与滚动 */}
      <ProjectTabs projectId={projectId} active='tasks' />

      {/* 内容区：窄屏（<lg 两栏堆叠）整区滚动；lg 起锁高，滚动只发生在两栏内部 */}
      <div className='min-h-0 flex-1 overflow-y-auto lg:overflow-hidden'>
        <div className='mx-auto w-full max-w-6xl p-6 lg:h-full'>
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
            <div className='flex flex-col gap-6 lg:h-full lg:flex-row'>
              {/* 左栏：主/子任务树（master）。窄屏堆叠为上列表下详情；lg 起
                  栏内滚动——列表再长也只滚自己，不推走 tab 头与右栏 */}
              <aside
                data-slot='task-master-list'
                className='w-full shrink-0 lg:w-72 lg:overflow-y-auto lg:border-e lg:pe-6'
              >
                <ul className='space-y-1'>
                  {rows.map((g) => {
                    // 阶段分组语义由右栏承载；左栏按 stages 顺序展平成紧凑树
                    const children = g.stages.flatMap((s) => s.tasks)
                    return (
                      <li key={g.id}>
                        <ParentListItem
                          g={g}
                          selected={selected?.id === g.id}
                          onSelect={() => setSelectedId(g.id)}
                        />
                        {children.length > 0 && (
                          <ChildTaskList
                            tasks={children}
                            active={selected?.id === g.id}
                            onSelect={() => setSelectedId(g.id)}
                          />
                        )}
                      </li>
                    )
                  })}
                </ul>
              </aside>
              {/* 右栏：选中父任务的父子任务树（detail），只含该父任务及其子任务；
                  不随左栏滚动移动，仅自身内容溢出时栏内滚动 */}
              <div
                data-slot='task-detail-pane'
                className='min-w-0 flex-1 lg:overflow-y-auto'
              >
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
      </div>
    </div>
  )
}

/**
 * 左栏父任务（主任务）行：方形标识位内的状态图标 + 标题 + 子任务进度计数
 * （无子任务的独立任务不显示计数）。标识位（hairline 方框 + 内嵌状态图标）
 * 是主任务与子任务裸图标区分层级的关键，也让父行自身状态一眼可辨。
 * 选中态以 aria-current + 背景 infill 标识；阶段/来源等详情由右栏选中
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
        'flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-start transition-colors hover:bg-accent/50',
        selected ? 'bg-accent' : undefined
      )}
    >
      {/* 主任务标识位：状态图标嵌于 hairline 方框（DESIGN.md rounded.xs） */}
      <span className='flex size-6 shrink-0 items-center justify-center rounded-sm border'>
        <TaskStatusIcon status={g.status} />
      </span>
      <span className='min-w-0 flex-1'>
        <span className='block truncate text-sm font-medium'>{g.title}</span>
        {g.child_total > 0 && (
          <span className='block truncate text-xs tabular-nums text-muted-foreground'>
            子任务 {g.child_completed}/{g.child_total}
          </span>
        )}
      </span>
    </button>
  )
}

/**
 * 左栏子任务组：挂在父行之下的缩进行列表。竖向 hairline 连线（border-s，
 * 起点对齐父行标识位中心）与缩进共同表达父子层级；行 = 裸状态图标 + 标题
 * （比父行小一档、灰一档）。选中父任务的组整体激活——标题转墨色、连线加深。
 * 点击任一子行选中其父任务（右栏切到该父任务的父子树）。
 */
function ChildTaskList({
  tasks,
  active,
  onSelect,
}: {
  tasks: TaskChild[]
  active: boolean
  onSelect: () => void
}) {
  return (
    <ul
      data-slot='task-child-group'
      className={cn(
        'ms-6 mt-0.5 space-y-px border-s ps-2',
        active ? 'border-foreground/25' : 'border-border'
      )}
    >
      {tasks.map((c) => (
        <li key={c.id}>
          <button
            type='button'
            onClick={onSelect}
            className='flex w-full items-center gap-2 rounded-md px-1.5 py-1 text-start transition-colors hover:bg-accent/50'
          >
            <TaskStatusIcon status={c.status} />
            <span
              className={cn(
                'min-w-0 flex-1 truncate text-xs',
                active ? 'text-foreground' : 'text-muted-foreground'
              )}
            >
              {c.title}
            </span>
          </button>
        </li>
      ))}
    </ul>
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
  // 本视图可见标签（挖掘域两类被滤掉）：为空且无徽标时整行不渲染，不留空壳
  const labels = visibleLabels(g.labels)
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
          {(labels.length ||
            g.pending_gate ||
            (g.split_mode && g.merge_state === 'conflict')) && (
            <span className='mt-1.5 flex flex-wrap items-center gap-1.5'>
              {/* 标签 chip（INFERA-220）：Multica hex 原值底色，空标签不占位。
                  挖掘域「情报」「候选」在本视图不渲染（INFERA-261） */}
              <LabelChipRow labels={labels} />
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
      {/* 单行式行内标签：保持单行，超长名由 chip 截断（INFERA-220）；
          挖掘域「情报」「候选」在本视图不渲染（INFERA-261） */}
      <LabelChipRow labels={visibleLabels(d.labels)} nowrap />
      <span className='shrink-0 text-[11px] tabular-nums text-muted-foreground'>
        {timeAgo(d.updated_at)}
      </span>
    </Link>
  )
}

/**
 * 子任务行状态图标五态查表（单色语言，语义与 StatusBadge 一致）：
 * 已完成=墨色实勾；进行中=旋转加载圈；已阻塞=墨色警示圈；未启动=灰虚线圈；
 * 已取消=灰色禁用圈（INFERA-233，中性弱化）。
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
  cancelled: {
    icon: CircleOff,
    label: '已取消',
    className: 'size-3.5 shrink-0 text-muted-foreground',
  },
}

/** 子任务行状态图标：按 status 查表单一渲染点（a11y 与样式口径见表项） */
function TaskStatusIcon({ status }: { status: DeliveryStatus }) {
  const { icon: Icon, label, className } = TASK_STATUS_ICON[status]
  return <Icon role='img' aria-label={label} className={className} />
}
