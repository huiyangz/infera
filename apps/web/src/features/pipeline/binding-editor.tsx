import { type Agent, type BindingMap, stageLabel } from '@/lib/infera-types'
import { cn } from '@/lib/utils'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * 节点→Agent 绑定编辑表。全局默认编排与项目编排共用：
 * 调用方负责 fetch 与保存（PUT 全量替换），本组件只管选择态与展示。
 *
 * GATE 类节点（spec_approval 等）不可绑定 agent，由后端 BindableNodes 决定——
 * 这里只渲染传入的 nodes。
 */
export function BindingEditor({
  nodes,
  agents,
  value,
  onChange,
  showFrom,
  disabled,
}: {
  nodes: string[]
  agents: Agent[]
  /** 当前选择态（保存时全量提交） */
  value: BindingMap
  onChange?: (next: BindingMap) => void
  /** 来源标注：node → 'default' | 'project'（仅项目编排传入） */
  showFrom?: Record<string, 'default' | 'project'>
  disabled?: boolean
}) {
  return (
    <div className='overflow-hidden rounded-md border'>
      <table className='w-full text-sm'>
        <thead>
          <tr className='border-b bg-muted/50 text-left'>
            <th className='px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground'>
              流水线节点
            </th>
            <th className='px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground'>
              类型
            </th>
            <th className='px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground'>
              执行 Agent
            </th>
            {showFrom && (
              <th className='px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground'>
                来源
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {nodes.map((node) => {
            const from = showFrom?.[node]
            return (
              <tr key={node} className='border-b last:border-b-0'>
                <td className='px-4 py-2.5 font-medium'>{stageLabel(node)}</td>
                <td className='px-4 py-2.5'>
                  <span
                    className={cn(
                      'inline-block rounded-full border px-2 py-0.5 text-[11px] leading-4',
                      'text-muted-foreground',
                    )}
                  >
                    AGENT
                  </span>
                </td>
                <td className='px-4 py-2.5'>
                  {disabled && !value[node] ? (
                    <Skeleton className='h-8 w-40' />
                  ) : (
                    <Select
                      value={value[node] || undefined}
                      disabled={disabled}
                      onValueChange={(id) =>
                        onChange?.({ ...value, [node]: id })
                      }
                    >
                      <SelectTrigger size='sm' className='w-52'>
                        <SelectValue placeholder='选择 Agent' />
                      </SelectTrigger>
                      <SelectContent>
                        {agents.map((a) => (
                          <SelectItem key={a.id} value={a.id}>
                            {a.name}
                            <span className='ms-1.5 font-mono text-xs text-muted-foreground'>
                              {a.runner}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                </td>
                {showFrom && (
                  <td className='px-4 py-2.5'>
                    {from === 'project' ? (
                      <span className='inline-block rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground'>
                        项目覆盖
                      </span>
                    ) : (
                      <span className='text-xs text-muted-foreground'>
                        默认
                      </span>
                    )}
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>
      {!nodes.length && (
        <p className='px-4 py-6 text-center text-sm text-muted-foreground'>
          加载中…
        </p>
      )}
    </div>
  )
}
