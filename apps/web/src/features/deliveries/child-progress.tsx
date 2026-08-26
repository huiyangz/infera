import { useQuery } from '@tanstack/react-query'
import { getChildProgress } from '@/lib/infera-api'
import type { ChildProgressCounts } from '@/lib/infera-types'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'

/**
 * 子任务进度区（L202608260142-3-T01）：以只读聚合接口
 * GET /api/deliveries/{id}/progress 为唯一数据源（契约冻结于
 * L202608260142-1-T01），按 stage 分组展示各维度真实状态计数——
 * 运行中 / 待审批 / 已阻塞 / 未启动 / 已完成 / 已取消 互斥可直加，
 * 不在前端另算平行进度。无子任务（total=0）或聚合未就绪时不渲染。
 */

/** 六类展示计数键（互斥可直加；by_status 是原始五值计数，不在展示序里） */
type ProgressCountKey =
  | 'in_progress'
  | 'in_review'
  | 'blocked'
  | 'todo'
  | 'done'
  | 'cancelled'

/** 六类展示计数的展示序与文案（label 对齐全站状态词表，不发明新词） */
const STATUS_CHIPS: Array<{ key: ProgressCountKey; label: string }> = [
  { key: 'in_progress', label: '运行中' },
  { key: 'in_review', label: '待审批' },
  { key: 'blocked', label: '已阻塞' },
  { key: 'todo', label: '未启动' },
  { key: 'done', label: '已完成' },
  { key: 'cancelled', label: '已取消' },
]

/** 完成度百分比（done/total，四舍五入；无子任务为 0） */
function percent(c: { done: number; total: number }): number {
  return c.total > 0 ? Math.round((c.done / c.total) * 100) : 0
}

/** 单色进度条（DESIGN.md：墨色填充 + 灰阶轨道，无信号色） */
function ProgressBar({ label, value }: { label: string; value: number }) {
  return (
    <div
      role='progressbar'
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={100}
      className='h-1.5 w-full overflow-hidden rounded-full bg-muted'
    >
      <div
        className='h-full rounded-full bg-primary'
        style={{ width: `${value}%` }}
      />
    </div>
  )
}

/** 非零状态计数行：零计数不占位（阻塞等状态存在即可见） */
function StatusChips({ counts }: { counts: ChildProgressCounts }) {
  return (
    <div className='mt-2 flex flex-wrap gap-x-3 gap-y-1'>
      {STATUS_CHIPS.filter((m) => counts[m.key] > 0).map((m) => (
        <span key={m.key} className='text-xs text-muted-foreground'>
          {m.label} {counts[m.key]}
        </span>
      ))}
    </div>
  )
}

export function ChildProgressCard({ deliveryId }: { deliveryId: string }) {
  const { data } = useQuery({
    queryKey: ['delivery-progress', deliveryId],
    queryFn: () => getChildProgress(deliveryId),
  })
  // 空态：无子任务 / 加载中 / 端点异常都不渲染（不摆装饰性数字）
  if (!data || data.total === 0) return null

  return (
    <Card data-slot='child-progress'>
      <CardHeader className='pb-0'>
        <CardTitle className='text-sm font-medium'>子任务进度</CardTitle>
        <span className='text-xs tabular-nums text-muted-foreground'>
          {data.done} / {data.total} 完成 · {percent(data)}%
        </span>
      </CardHeader>
      <CardContent>
        <ProgressBar label='子任务完成度' value={percent(data)} />
        <StatusChips counts={data} />

        <Separator className='my-5' />
        <ul className='space-y-4'>
          {data.stages.map((s) => {
            const title = s.stage === 0 ? '无阶段' : `阶段 ${s.stage}`
            const active = data.active_stage === s.stage
            return (
              <li key={s.stage} data-stage={s.stage}>
                <div className='flex items-center justify-between gap-3'>
                  <span className='flex items-center gap-2'>
                    <span className='text-xs font-medium'>{title}</span>
                    {active && (
                      <Badge
                        variant='outline'
                        className='px-1.5 text-[10px]'
                      >
                        当前
                      </Badge>
                    )}
                  </span>
                  <span className='text-xs tabular-nums text-muted-foreground'>
                    {s.done} / {s.total}
                  </span>
                </div>
                <div className='mt-2'>
                  <ProgressBar label={`${title} 完成度`} value={percent(s)} />
                </div>
                <StatusChips counts={s} />
              </li>
            )
          })}
        </ul>
      </CardContent>
    </Card>
  )
}
