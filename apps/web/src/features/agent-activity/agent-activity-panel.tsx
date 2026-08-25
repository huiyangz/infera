import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { getAgentActivity } from './api'
import { buildAgentActivityOption } from './chart-option'
import { ActivityChart } from './echarts-line-chart'

/** 窗口切换（加分项）：24 / 12 / 6 小时，桶宽保持 L1 默认 30 分钟 */
const WINDOWS = [24, 12, 6] as const
type WindowHours = (typeof WINDOWS)[number]

/**
 * chart 单色系主题：从 CSS 变量解析 --chart-1..5 与轴/legend 文字色，
 * 暗色主题下同一段代码取到反转灰阶（theme.css 两套 token）。
 * 每次挂载解析一次；主题中途翻转由下次进页/换窗口自然承接。
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

/**
 * 「Agent 执行时序」可视化主体（INFERA-259 自独立页迁入项目详情 tab）：
 * 跨项目各 agent 最近窗口内的执行次数多曲线（GET /api/agent-activity，
 * 契约冻结于 INFERA-253，工作区全局口径不按项目过滤）+ 窗口切换控件。
 * 三态：加载骨架 / 空数据提示（series=[]）/ 错误 + 重试。
 * 只做主体，不含页头——宿主页自带 Header 与页内导航。
 */
export function AgentActivityPanel() {
  const [hours, setHours] = useState<WindowHours>(24)
  const style = useChartStyle()

  const query = useQuery({
    queryKey: ['agent-activity', hours],
    queryFn: () => getAgentActivity({ hours }),
  })

  const option = useMemo(
    () =>
      query.data
        ? buildAgentActivityOption(query.data, style.palette, {
            labelColor: style.labelColor,
          })
        : null,
    [query.data, style]
  )

  return (
    <div className='space-y-4'>
      {/* 控件行：当前窗口说明 + 窗口切换（原独立页页头右侧控件的迁入位置） */}
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-sm text-muted-foreground'>
          最近 {hours} 小时各 agent 执行次数，30 分钟桶
        </p>
        <div className='flex shrink-0 items-center gap-1'>
          {WINDOWS.map((h) => (
            <Button
              key={h}
              size='sm'
              variant={hours === h ? 'secondary' : 'ghost'}
              aria-pressed={hours === h}
              onClick={() => setHours(h)}
            >
              {h} 小时
            </Button>
          ))}
        </div>
      </div>

      {query.isPending ? (
        <Skeleton className='h-80 rounded-lg' />
      ) : query.isError ? (
        <Card className='flex items-center justify-between gap-3 px-4 py-3'>
          <p className='text-sm text-muted-foreground'>时序数据加载失败</p>
          <Button size='sm' variant='ghost' onClick={() => query.refetch()}>
            重试
          </Button>
        </Card>
      ) : !query.data.series.length ? (
        <div className='flex flex-col items-center gap-3 p-16 text-center'>
          <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
            <Activity className='size-6 text-muted-foreground' />
          </div>
          <div>
            <p className='font-medium'>窗口内没有 agent 执行记录</p>
            <p className='mt-1 text-sm text-muted-foreground'>
              换个时间窗口，或等流水线跑起来后再看
            </p>
          </div>
        </div>
      ) : (
        <Card className='px-4 py-4'>
          <ActivityChart option={option!} aria-label='Agent 执行时序图表' />
        </Card>
      )}
    </div>
  )
}
