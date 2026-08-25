import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { MoonStar } from 'lucide-react'
import { Header } from '@/components/layout/header'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { getWorkspaceStats } from './api'
import {
  buildHourlyOption,
  NIGHT_END_HOUR,
  NIGHT_START_HOUR,
} from './chart-option'
import { formatDuration } from './format'
import { StatsChart } from './stats-chart'

/** 窗口切换：24 小时 / 7 天（后端缺省 168）/ 30 天（后端上限 720） */
const WINDOWS = [
  { hours: 24, label: '24 小时', caption: '最近 24 小时' },
  { hours: 168, label: '7 天', caption: '最近 7 天' },
  { hours: 720, label: '30 天', caption: '最近 30 天' },
] as const

/**
 * chart 单色系主题：从 CSS 变量解析 --chart-1..5 与轴/legend 文字色，
 * 暗色主题下同一段代码取到反转灰阶（与 agent-activity-panel 同款做法；
 * 该 hook 未从彼处导出，此处就地复制一份 10 行小函数）。
 */
function useChartStyle() {
  return useMemo(() => {
    const cs = getComputedStyle(document.documentElement)
    const v = (name: string, fallback: string) =>
      cs.getPropertyValue(name).trim() || fallback
    return {
      palette: [1, 2, 3, 4, 5].map((i) => v(`--chart-${i}`, '#676f7b')),
      labelColor: v('--muted-foreground', '#676f7b'),
    }
  }, [])
}

/** 数字卡片：label 供测试定位（data-card-label），value 与 hint 同卡展示 */
function StatCard({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint?: string
}) {
  return (
    <Card className='flex flex-col gap-1 px-4 py-3'>
      <p
        data-card-label={label}
        className='text-xs font-medium tracking-wider text-muted-foreground uppercase'
      >
        {label}
      </p>
      <p className='text-2xl font-semibold tabular-nums'>{value}</p>
      {hint ? (
        <p className='text-xs text-muted-foreground'>{hint}</p>
      ) : null}
    </Card>
  )
}

/**
 * 「统计」页（INFERA-274）：跨项目任务状态分布 + 执行时段分布直方图，
 * 数据全部来自 GET /api/stats（L202608251850-1-T01 冻结契约，不另开入口）。
 * 口径：任务状态为全量快照（不受窗口影响）；执行统计与逐小时分桶只算
 * 窗口内，按浏览器时区归桶，夜间（22:00–06:00）柱体高亮。
 */
export function StatsPage() {
  const [hours, setHours] = useState<number>(168)
  const style = useChartStyle()

  const query = useQuery({
    queryKey: ['workspace-stats', hours],
    queryFn: () => getWorkspaceStats({ hours }),
  })

  const option = useMemo(
    () =>
      query.data
        ? buildHourlyOption(query.data, {
            palette: style.palette,
            labelColor: style.labelColor,
          })
        : null,
    [query.data, style]
  )

  const win = WINDOWS.find((w) => w.hours === hours) ?? WINDOWS[1]
  const ts = query.data?.task_status
  const ex = query.data?.execution

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-lg font-semibold tracking-[-0.2px]'>统计</h1>
            <p className='text-sm text-muted-foreground'>
              跨项目任务状态分布与执行时段分布
            </p>
          </div>
        </div>
      </Header>

      <div className='p-6'>
        {query.isPending ? (
          <Skeleton className='h-96 rounded-lg' />
        ) : query.isError ? (
          <Card className='flex items-center justify-between gap-3 px-4 py-3'>
            <p className='text-sm text-muted-foreground'>统计数据加载失败</p>
            <Button size='sm' variant='ghost' onClick={() => query.refetch()}>
              重试
            </Button>
          </Card>
        ) : !query.data ? null : (
          <div className='space-y-4'>
            {/* 数字卡片：任务状态分布（全量快照口径） */}
            <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5'>
              <StatCard label='任务总数' value={String(ts?.total ?? 0)} />
              <StatCard label='已完成' value={String(ts?.done ?? 0)} />
              <StatCard label='进行中' value={String(ts?.in_progress ?? 0)} />
              <StatCard label='待办' value={String(ts?.todo ?? 0)} />
              <StatCard label='已取消' value={String(ts?.cancelled ?? 0)} />
            </div>
            {/* 执行维度基础统计（窗口内） */}
            <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
              <StatCard
                label='执行次数'
                value={String(ex?.runs_total ?? 0)}
                hint={`进行中 ${ex?.running ?? 0} · 失败 ${ex?.failed ?? 0}`}
              />
              <StatCard
                label='累计时长'
                value={formatDuration(ex?.duration_ms_total ?? 0)}
                hint='仅计已收尾执行（running 不计时长）'
              />
            </div>

            {/* 执行时段分布直方图 */}
            <Card className='space-y-3 px-4 py-4'>
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <div className='flex flex-col gap-1'>
                  <p className='font-medium'>执行时段分布</p>
                  <p className='text-sm text-muted-foreground'>
                    {win.caption}，按{' '}
                    <span className='font-medium'>
                      {query.data.timezone}
                    </span>{' '}
                    本地小时分桶
                  </p>
                </div>
                <div className='flex shrink-0 items-center gap-1'>
                  {WINDOWS.map((w) => (
                    <Button
                      key={w.hours}
                      size='sm'
                      variant={hours === w.hours ? 'secondary' : 'ghost'}
                      aria-pressed={hours === w.hours}
                      onClick={() => setHours(w.hours)}
                    >
                      {w.label}
                    </Button>
                  ))}
                </div>
              </div>

              {ex && ex.runs_total === 0 ? (
                <div className='flex flex-col items-center gap-3 p-12 text-center'>
                  <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
                    <MoonStar className='size-6 text-muted-foreground' />
                  </div>
                  <div>
                    <p className='font-medium'>窗口内没有执行记录</p>
                    <p className='mt-1 text-sm text-muted-foreground'>
                      换个时间窗口，或等流水线跑起来后再看
                    </p>
                  </div>
                </div>
              ) : (
                option && <StatsChart option={option} aria-label='执行时段分布图表' />
              )}

              <p className='text-xs text-muted-foreground'>
                夜间（{String(NIGHT_START_HOUR).padStart(2, '0')}:00–
                {String(NIGHT_END_HOUR).padStart(2, '0')}:00）时段柱体高亮；
                跨小时执行整段计入起始小时桶。
              </p>
              <p className='text-xs text-muted-foreground'>
                口径：任务状态为全量快照（不受窗口选择影响）；执行统计与时段分布只算所选窗口内，running
                计次不计时长。
              </p>
            </Card>
          </div>
        )}
      </div>
    </>
  )
}
