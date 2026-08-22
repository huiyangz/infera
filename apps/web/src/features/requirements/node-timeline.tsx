import { AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { NODE_META, NODE_SEQUENCE, nodeLabel, type FlowNode } from './types'

/**
 * 大节点时间线（FR-1）：主线 5 节点线性推进（受理→已派发→执行中→待验收→
 * 已交付）；needs_decision 是异常节点（⚠️），不在主线序列里——出现时主线
 * 保持 upcoming 中性态（当前行不携带历史节点，不猜测推进位置），异常横幅
 * 单独呈现，处理决策卡后由轮询恢复推进。
 * 节点行的 data-state 供测试与样式共用。
 */
export function NodeTimeline({ node }: { node: FlowNode }) {
  const anomaly = node === 'needs_decision'
  const currentIdx = NODE_SEQUENCE.indexOf(node) // 异常态为 -1

  return (
    <div className='space-y-3' aria-label='大节点时间线'>
      {anomaly && (
        <div
          data-anomaly
          className='flex items-center gap-2 rounded-lg border bg-muted/50 px-4 py-3 text-sm'
        >
          <AlertTriangle className='size-4 shrink-0' />
          <span className='font-medium'>任务需决策</span>
          <span className='text-muted-foreground'>
            {NODE_META.needs_decision.hint}，处理决策卡后恢复推进
          </span>
        </div>
      )}
      <ol className='flex items-start'>
        {NODE_SEQUENCE.map((n, i) => {
          // 终点节点抵达即 done（已交付是完成态）；途中当前节点 active
          const state = anomaly
            ? 'upcoming'
            : i < currentIdx || (i === currentIdx && n === 'delivered')
              ? 'done'
              : i === currentIdx
                ? 'active'
                : 'upcoming'
          return (
            <li
              key={n}
              data-node={n}
              data-state={state}
              className={cn(
                'flex min-w-0 flex-1 flex-col items-center gap-2 text-center',
                i > 0 && 'border-t border-hairline pt-3'
              )}
            >
              <span
                aria-hidden
                className={cn(
                  'flex size-3 items-center justify-center rounded-full border',
                  state === 'done' && 'border-foreground bg-foreground',
                  state === 'active' &&
                    'border-foreground bg-background ring-4 ring-foreground/10',
                  state === 'upcoming' && 'border-border bg-background'
                )}
              />
              <span
                className={cn(
                  'text-xs leading-tight',
                  state === 'active' && 'font-medium text-foreground',
                  state === 'done' && 'text-foreground',
                  state === 'upcoming' && 'text-muted-foreground'
                )}
              >
                {nodeLabel(n)}
              </span>
            </li>
          )
        })}
      </ol>
    </div>
  )
}
