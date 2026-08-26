import { type RequirementStats } from '@/lib/infera-types'
import { dateTime } from '@/lib/time'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

/** 状态桶展示口径（与 StatusBadge 一致的中文名；cancelled 殿后） */
const STATUS_BUCKETS: Array<{
  key: keyof RequirementStats['by_status']
  label: string
}> = [
  { key: 'active', label: '进行中' },
  { key: 'queued', label: '未启动' },
  { key: 'completed', label: '已完成' },
  { key: 'blocked', label: '已阻塞' },
  { key: 'cancelled', label: '已取消' },
]

/**
 * 分布条灰阶映射（DESIGN.md：无信号色，靠墨阶层次表达）。
 * blocked = 墨底（最强层级，同 StatusBadge 的墨底白字）；cancelled 最弱。
 * 基于 foreground 的透明度阶梯，深浅色主题下均可读。
 */
const BAR_CLASS: Record<keyof RequirementStats['by_status'], string> = {
  active: 'bg-foreground/70',
  queued: 'bg-foreground/45',
  completed: 'bg-foreground/25',
  blocked: 'bg-foreground',
  cancelled: 'bg-foreground/15',
}

/** 分布条分母取五桶计数之和（而非 requirement_total），保证分段恒和为 100% */
function bucketSum(stats: RequirementStats): number {
  return STATUS_BUCKETS.reduce((acc, { key }) => acc + stats.by_status[key], 0)
}

/**
 * dashboard 头部统计区（INFERA-243）：KPI 瓦片 + 状态分布条。
 * 与旧版「三张数字卡 + 下方逐行重复计数」不同——数字只讲总量与
 * 可行动量（待决策/已交付），状态构成由占比条一图承载，不再单列。
 * stats 未就绪时渲染骨架（页面在查询出错时另行渲染错误态，不走这里）。
 */
export function KpiHeader({ stats }: { stats?: RequirementStats }) {
  const sum = stats ? bucketSum(stats) : 0

  return (
    <div className='space-y-3'>
      <div data-kpi-grid className='grid grid-cols-2 gap-3 xl:grid-cols-4'>
        <KpiTile label='任务总数' loading={!stats}>
          {stats ? (
            stats.requirement_total
          ) : (
            <Skeleton className='h-7 w-10' />
          )}
        </KpiTile>
        <KpiTile label='待决策' loading={!stats}>
          {stats ? stats.pending_decisions : <Skeleton className='h-7 w-10' />}
        </KpiTile>
        <KpiTile label='已交付' loading={!stats}>
          {stats ? stats.delivered : <Skeleton className='h-7 w-10' />}
        </KpiTile>
        <KpiTile label='最近活动' loading={!stats} valueClass='text-xl'>
          {stats ? (
            stats.last_synced_at ? (
              dateTime(stats.last_synced_at)
            ) : (
              '暂无活动'
            )
          ) : (
            <Skeleton className='h-7 w-24' />
          )}
        </KpiTile>
      </div>

      <Card className='gap-3 px-4 py-4'>
        <p className='text-xs text-muted-foreground'>状态分布</p>
        {stats ? (
          <>
            <div
              data-statusbar
              role='img'
              aria-label='任务状态分布'
              className='flex h-2.5 w-full overflow-hidden rounded-full bg-muted'
            >
              {STATUS_BUCKETS.map(({ key, label }) => {
                const count = stats.by_status[key]
                if (count === 0) return null
                const pct = sum > 0 ? (count / sum) * 100 : 0
                return (
                  <div
                    key={key}
                    data-seg={key}
                    title={`${label} ${count}（${Math.round(pct)}%）`}
                    className={cn('h-full', BAR_CLASS[key])}
                    style={{ width: `${pct.toFixed(3)}%` }}
                  />
                )
              })}
            </div>
            <ul className='flex flex-wrap gap-x-4 gap-y-1.5'>
              {STATUS_BUCKETS.map(({ key, label }) => {
                const count = stats.by_status[key]
                const pct = sum > 0 ? Math.round((count / sum) * 100) : 0
                return (
                  <li
                    key={key}
                    data-legend={key}
                    className='flex items-center gap-1.5 text-xs text-muted-foreground'
                  >
                    <span
                      className={cn('size-2.5 shrink-0 rounded-[2px]', BAR_CLASS[key])}
                    />
                    <span>{label}</span>
                    <span className='tabular-nums'>{count}</span>
                    <span className='tabular-nums opacity-70'>{pct}%</span>
                  </li>
                )
              })}
            </ul>
          </>
        ) : (
          <div className='space-y-3'>
            <Skeleton className='h-2.5 w-full rounded-full' />
            <div className='flex gap-4'>
              {STATUS_BUCKETS.map(({ key }) => (
                <Skeleton key={key} className='h-3.5 w-16' />
              ))}
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}

/**
 * 单张 KPI 瓦片：label 恒占位（加载中不显示 label 文本，避免半成品排版）。
 * label/value 外层用 <div> 而非 <p>——加载态两者都要包 Skeleton（渲染为
 * <div>），<p> 嵌套 <div> 属非法 DOM，React 会报嵌套错误（INFERA-301）。
 */
function KpiTile({
  label,
  loading,
  valueClass = 'text-2xl',
  children,
}: {
  label: string
  loading: boolean
  valueClass?: string
  children: React.ReactNode
}) {
  return (
    <Card className='gap-1 px-4 py-4'>
      <div className='text-xs text-muted-foreground'>
        {loading ? <Skeleton className='h-3.5 w-12' /> : label}
      </div>
      <div
        className={cn(
          'font-semibold tracking-[-0.3px] tabular-nums',
          valueClass
        )}
      >
        {children}
      </div>
    </Card>
  )
}
