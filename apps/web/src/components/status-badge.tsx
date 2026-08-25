import { Badge } from '@/components/ui/badge'

/**
 * 单色状态徽标（DESIGN.md：无信号色，靠灰阶层次表达）。
 * 进行中 = 描边 + 墨点；已完成 = hairline infill；阻塞 = 墨底白字（最强层级）；
 * 已取消 = 描边 + 弱化文字 + 灰点（放弃退出主视线，弱于未启动的墨字描边）。
 */
export function StatusBadge({
  status,
  className,
}: {
  status: string
  className?: string
}) {
  if (status === 'active')
    return (
      <Badge variant='outline' className={`gap-1.5 ${className ?? ''}`}>
        <span className='size-1.5 animate-pulse rounded-full bg-foreground' />
        进行中
      </Badge>
    )
  if (status === 'queued')
    return (
      <Badge variant='outline' className={className ?? ''}>
        未启动
      </Badge>
    )
  if (status === 'blocked')
    return (
      <Badge
        className={`gap-1.5 bg-primary text-primary-foreground ${className ?? ''}`}
      >
        已阻塞
      </Badge>
    )
  if (status === 'cancelled')
    return (
      <Badge
        variant='outline'
        className={`gap-1.5 text-muted-foreground ${className ?? ''}`}
      >
        <span className='size-1.5 rounded-full bg-foreground/40' />
        已取消
      </Badge>
    )
  return (
    <Badge variant='secondary' className={`gap-1.5 ${className ?? ''}`}>
      <span className='size-1.5 rounded-full bg-foreground/40' />
      已完成
    </Badge>
  )
}
