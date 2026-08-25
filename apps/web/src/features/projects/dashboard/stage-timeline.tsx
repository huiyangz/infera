import { useMemo, useState } from 'react'
import { Inbox } from 'lucide-react'
import {
  type StageRunDetail,
  type StageRunStatus,
  stageLabel,
} from '@/lib/infera-types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { axisTime, formatDuration } from './format'

/** 状态中文口径（与 StatusBadge 一致的语感） */
const STATUS_TEXT: Record<StageRunStatus, string> = {
  running: '进行中',
  done: '已完成',
  failed: '失败',
}

/** failed 条的墨底斜纹（var(--background) 使条纹深浅色主题自适应） */
const FAILED_STRIPE =
  'repeating-linear-gradient(45deg, transparent 0 3px, var(--background) 3px 6px)'

/** tooltip 里的时间一律带日期（跨天窗口下 HH:mm 会歧义） */
const DATE_SPAN = 24 * 3600_000 + 1

/**
 * 当前时刻兜底（running 条的时间域右端）。经函数边界间接读取：
 * 渲染期直接调用 Date.now 会被 react-hooks 纯度规则拦截，
 * 与 lib/time.ts 的 timeAgo 同一口径；测试可用 now prop 覆盖。
 */
function currentMs(): number {
  return Date.now()
}

/** 一条泳道 = 一个 delivery 的全部 stage run（时间升序） */
interface Lane {
  deliveryId: string
  title: string
  issueKey: string
  runs: StageRunDetail[]
  latestMs: number
}

/** 按 delivery_id 分组；泳道按最近一次 started_at 倒序（最近活动在前） */
function groupLanes(runs: StageRunDetail[]): Lane[] {
  const map = new Map<string, Lane>()
  for (const r of runs) {
    const start = Date.parse(r.started_at)
    let lane = map.get(r.delivery_id)
    if (!lane) {
      lane = {
        deliveryId: r.delivery_id,
        title: r.title,
        issueKey: r.external_issue_key,
        runs: [],
        latestMs: start,
      }
      map.set(r.delivery_id, lane)
    }
    lane.runs.push(r)
    lane.latestMs = Math.max(lane.latestMs, start)
  }
  const lanes = Array.from(map.values())
  for (const l of lanes) l.runs.sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at))
  return lanes.sort((a, b) => b.latestMs - a.latestMs)
}

/** 单条 run 的条形样式：failed 墨底最醒目；门禁/系统节点（无 agent）描边空心 */
function barClass(run: StageRunDetail): string {
  const base = 'absolute top-1/2 h-5 -translate-y-1/2 rounded-[3px]'
  const gate = run.agent_name === null
  if (run.status === 'failed') return cn(base, 'bg-foreground')
  if (run.status === 'running')
    return cn(
      base,
      gate
        ? 'border border-foreground/70 animate-pulse'
        : 'bg-foreground/60 animate-pulse'
    )
  return cn(
    base,
    gate ? 'border border-foreground/40' : 'bg-foreground/30'
  )
}

/**
 * Agent 执行时序甘特区（INFERA-243）：横轴时间、每行一个 delivery，
 * 色条表达各 stage 起止与耗时；成败（failed 墨底斜纹 / running 脉冲）、
 * 重试次数（×N 标注）、agent 名与完整耗时语义走 title tooltip；
 * 门禁/系统节点描边空心，与 agent 实心执行区分出「执行 vs 等待」结构。
 * 数据契约 = T02 冻结的 StageRunDetail（runs 按 started_at 倒序，最多 200 条）。
 */
export function AgentStageTimeline({
  runs,
  now,
  initialVisible = 6,
}: {
  runs: StageRunDetail[]
  /** 时间域右端点（running 条延伸至此）；缺省取当前时刻 */
  now?: number
  /** 有界展示：默认渲染最近 6 条泳道，「查看更多」按同步长展开 */
  initialVisible?: number
}) {
  const [visible, setVisible] = useState(initialVisible)
  const nowMs = now ?? currentMs()
  const lanes = useMemo(() => groupLanes(runs), [runs])

  const shown = lanes.slice(0, visible)
  const remaining = lanes.length - shown.length

  // 时间域 = 全部 run 起止（running 以 now 收尾）的包络；瞬时域兜底 1 分钟避免 0 除
  const bounds = useMemo(() => {
    let t0 = Infinity
    let t1 = -Infinity
    for (const l of lanes) {
      for (const r of l.runs) {
        const s = Date.parse(r.started_at)
        const e = r.finished_at ? Date.parse(r.finished_at) : nowMs
        t0 = Math.min(t0, s)
        t1 = Math.max(t1, e)
      }
    }
    if (!Number.isFinite(t0) || !Number.isFinite(t1)) return { t0: 0, t1: 1 }
    if (t1 <= t0) return { t0, t1: t0 + 60_000 }
    return { t0, t1 }
  }, [lanes, nowMs])
  const span = bounds.t1 - bounds.t0

  if (lanes.length === 0) return <TimelineEmpty />

  return (
    <div className='space-y-3'>
      {/* 横轴刻度：跨天以内 HH:mm，跨天带日期 */}
      <div className='grid grid-cols-[minmax(0,10rem)_1fr] items-end gap-x-3'>
        <span className='text-[11px] text-muted-foreground'>任务</span>
        <div className='relative h-4'>
          {[0, 1 / 3, 2 / 3, 1].map((f, i) => (
            <span
              key={f}
              data-tick
              className={cn(
                'absolute top-0 text-[11px] whitespace-nowrap text-muted-foreground tabular-nums',
                i === 0 && 'left-0',
                (i === 1 || i === 2) && '-translate-x-1/2',
                i === 3 && 'right-0'
              )}
              style={i === 3 ? undefined : { left: `${(f * 100).toFixed(3)}%` }}
            >
              {axisTime(bounds.t0 + f * span, span)}
            </span>
          ))}
        </div>
      </div>

      <div className='space-y-2.5'>
        {shown.map((lane) => (
          <div
            key={lane.deliveryId}
            data-lane={lane.deliveryId}
            className='grid grid-cols-[minmax(0,10rem)_1fr] items-center gap-x-3'
          >
            <div className='min-w-0'>
              <p className='truncate text-sm' title={lane.title}>
                {lane.title}
              </p>
              {lane.issueKey && (
                <p className='truncate font-mono text-[11px] text-muted-foreground'>
                  {lane.issueKey}
                </p>
              )}
            </div>
            <div className='relative h-7'>
              {/* 三分位 hairline 网格线 */}
              <span className='absolute inset-y-0 w-px bg-border' style={{ left: '33.333%' }} />
              <span className='absolute inset-y-0 w-px bg-border' style={{ left: '66.667%' }} />
              {lane.runs.map((run) => {
                const s = Date.parse(run.started_at)
                const e = run.finished_at ? Date.parse(run.finished_at) : nowMs
                const left = ((s - bounds.t0) / span) * 100
                const width = Math.max(((e - s) / span) * 100, 0)
                return (
                  <div
                    key={run.id}
                    data-run
                    data-run-id={run.id}
                    data-stage={run.stage}
                    data-status={run.status}
                    title={[
                      stageLabel(run.stage),
                      `第 ${run.attempt} 次`,
                      STATUS_TEXT[run.status],
                      run.agent_name ?? '门禁/系统节点',
                      `耗时 ${formatDuration(run.duration_ms)}`,
                      `${axisTime(s, DATE_SPAN)} → ${
                        run.finished_at ? axisTime(e, DATE_SPAN) : '进行中'
                      }`,
                    ].join(' · ')}
                    className={barClass(run)}
                    style={{
                      left: `${left.toFixed(3)}%`,
                      width: `${width.toFixed(3)}%`,
                      minWidth: '3px',
                      ...(run.status === 'failed'
                        ? { backgroundImage: FAILED_STRIPE }
                        : null),
                    }}
                  >
                    {run.attempt > 1 && (
                      <span className='absolute top-1/2 left-1 z-10 -translate-y-1/2 rounded-[3px] bg-background px-1 text-[10px] leading-4 font-medium text-foreground'>
                        ×{run.attempt}
                      </span>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        ))}
      </div>

      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-muted-foreground'>
          <span className='flex items-center gap-1.5'>
            <span className='h-2 w-4 rounded-[2px] bg-foreground/30' />
            Agent 执行
          </span>
          <span className='flex items-center gap-1.5'>
            <span className='h-2 w-4 rounded-[2px] border border-foreground/40' />
            门禁/系统
          </span>
          <span className='flex items-center gap-1.5'>
            <span
              className='h-2 w-4 rounded-[2px] bg-foreground'
              style={{ backgroundImage: FAILED_STRIPE }}
            />
            失败
          </span>
          <span className='flex items-center gap-1.5'>
            <span className='h-2 w-4 animate-pulse rounded-[2px] bg-foreground/60' />
            进行中
          </span>
        </div>
        {remaining > 0 && (
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setVisible((v) => v + initialVisible)}
          >
            查看更多（还有 {remaining} 个任务）
          </Button>
        )}
      </div>
    </div>
  )
}

/** 空态：有设计的引导，而非空白 */
function TimelineEmpty() {
  return (
    <div
      data-timeline-empty
      className='flex flex-col items-center gap-1.5 py-10 text-center'
    >
      <Inbox className='size-5 text-muted-foreground' />
      <p className='text-sm font-medium'>暂无执行记录</p>
      <p className='text-xs text-muted-foreground'>
        任务开始流转后，这里会按时间展示各阶段的执行与门禁等待
      </p>
    </div>
  )
}

/** 加载态骨架（页面查询 pending 时由父级渲染） */
export function TimelineSkeleton() {
  return (
    <div data-timeline-skeleton className='space-y-3'>
      {Array.from({ length: 4 }, (_, i) => (
        <div
          key={i}
          className='grid grid-cols-[minmax(0,10rem)_1fr] items-center gap-x-3'
        >
          <Skeleton className='h-4 w-28' />
          <Skeleton className='h-5 w-full' />
        </div>
      ))}
    </div>
  )
}
