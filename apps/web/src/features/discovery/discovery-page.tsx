import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Inbox } from 'lucide-react'
import { timeAgo } from '@/lib/time'
import { stageLabel } from '@/lib/infera-types'
import { cn } from '@/lib/utils'
import { assigneeLabel } from '@/features/task-sync/display'
import { LabelChipRow } from '@/components/label-chip'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { listDiscoveryTasks } from './api'
import {
  AGENT_LABEL,
  AGENT_ORDER,
  STATUS_LABEL,
  STATUS_ORDER,
  filterDiscoveryTasks,
  groupDiscoveryTasks,
  type AgentFilter,
  type GroupMode,
  type StatusFilter,
} from './filters'
import type { DiscoveryTaskRow } from './types'

/**
 * 需求发现页（INFERA-226）：需求分析与需求挖掘两类 agent 任务的集中
 * 独立路由视图。一次并集拉取（GET /api/discovery-tasks，省略 agent），
 * 按 agent / 状态的筛选与分组在客户端完成（行内已带 status 与
 * agent_types 全集）；卡片风格对齐主看板任务卡（单色、hairline、无阴影）。
 */
export function DiscoveryPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['discovery-tasks'],
    queryFn: () => listDiscoveryTasks(),
  })
  const [agent, setAgent] = useState<AgentFilter>('all')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [groupBy, setGroupBy] = useState<GroupMode>('none')

  const all = data ?? []
  const rows = filterDiscoveryTasks(all, agent, status)
  const groups = groupDiscoveryTasks(rows, groupBy)

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-lg font-semibold tracking-[-0.2px]'>
              需求发现
            </h1>
            <p className='text-sm text-muted-foreground'>
              需求分析与需求挖掘两类 agent 任务集中视图
            </p>
          </div>
          {/* 页内筛选：类型 / 状态 / 分组（单色描边控件，不抢卡片主体） */}
          <div className='flex flex-wrap items-center gap-2'>
            <Select
              value={agent}
              onValueChange={(v) => setAgent(v as AgentFilter)}
            >
              <SelectTrigger aria-label='类型' className='w-28'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>全部</SelectItem>
                {AGENT_ORDER.map((t) => (
                  <SelectItem key={t} value={t}>
                    {AGENT_LABEL[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={status}
              onValueChange={(v) => setStatus(v as StatusFilter)}
            >
              <SelectTrigger aria-label='状态' className='w-28'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>全部</SelectItem>
                {STATUS_ORDER.map((s) => (
                  <SelectItem key={s} value={s}>
                    {STATUS_LABEL[s]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={groupBy}
              onValueChange={(v) => setGroupBy(v as GroupMode)}
            >
              <SelectTrigger aria-label='分组' className='w-28'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>不分组</SelectItem>
                <SelectItem value='agent'>按类型</SelectItem>
                <SelectItem value='status'>按状态</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </Header>

      <div className='mx-auto w-full max-w-6xl p-6'>
        {isLoading ? (
          <div className='space-y-3'>
            <Skeleton className='h-20 w-full rounded-lg' />
            <Skeleton className='h-20 w-full rounded-lg' />
          </div>
        ) : !all.length ? (
          <div className='flex flex-col items-center gap-2 p-16 text-center'>
            <Inbox className='size-5 text-muted-foreground' />
            <p className='text-sm text-muted-foreground'>还没有需求发现任务</p>
          </div>
        ) : !rows.length ? (
          <div className='flex flex-col items-center gap-2 p-16 text-center'>
            <Inbox className='size-5 text-muted-foreground' />
            <p className='text-sm text-muted-foreground'>
              没有匹配当前筛选的任务
            </p>
          </div>
        ) : (
          groups.map((g) => (
            <section key={g.key} className={cn(g.label && 'mb-6 last:mb-0')}>
              {g.label && (
                <div className='mb-2 flex items-baseline gap-2'>
                  <h3 className='text-xs font-medium'>{g.label}</h3>
                  <span className='text-xs tabular-nums text-muted-foreground'>
                    {g.rows.length}
                  </span>
                </div>
              )}
              <div className='grid gap-3 md:grid-cols-2'>
                {g.rows.map((r) => (
                  <DiscoveryTaskCard key={r.id} row={r} />
                ))}
              </div>
            </section>
          ))
        )}
      </div>
    </>
  )
}

/**
 * 需求发现任务卡：对齐主看板任务卡的卡片语言（hairline 描边、单色徽标、
 * 标签 chip 原值底色），差异仅在元信息行——跨项目列表以项目名打头，
 * 同步镜像无 current_stage 时 issue key 顶替阶段位展示。
 */
function DiscoveryTaskCard({ row }: { row: DiscoveryTaskRow }) {
  const assignee = assigneeLabel(row.assignee)
  return (
    <article className='rounded-lg border bg-card'>
      <Link
        to='/deliveries/$id'
        params={{ id: row.id }}
        className='block min-w-0 px-4 py-3'
      >
        <span className='flex items-center justify-between gap-2'>
          <span className='truncate text-sm font-medium'>{row.title}</span>
          <StatusBadge status={row.status} />
        </span>
        <span className='mt-1 flex min-w-0 items-center gap-2 text-xs text-muted-foreground'>
          <span className='shrink-0'>{row.project_name || '—'}</span>
          <span className='truncate'>
            · {stageLabel(row.current_stage) || row.external_issue_key || '—'}
          </span>
          {assignee && <span className='truncate'>· {assignee}</span>}
          <span className='ms-auto shrink-0 tabular-nums'>
            {timeAgo(row.updated_at)}
          </span>
        </span>
        <LabelChipRow labels={row.labels} className='mt-1.5' />
      </Link>
    </article>
  )
}
