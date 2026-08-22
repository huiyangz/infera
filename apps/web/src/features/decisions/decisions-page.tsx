import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CircleSlash2 } from 'lucide-react'
import { timeAgo } from '@/lib/time'
import { stageLabel } from '@/lib/infera-types'
import { Header } from '@/components/layout/header'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { listPendingDecisions } from './api'

/**
 * 「需要决策」列表页：跨项目展示全部停在人工审批门的需求
 * （GET /api/pending-decisions，INFERA-108 T01 冻结契约），
 * 行内 id 即 delivery ID，点击进入既有需求详情页处理。
 */
export function DecisionsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['pending-decisions'],
    queryFn: () => listPendingDecisions(),
  })

  const rows = data ?? []

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-lg font-semibold tracking-[-0.2px]'>
              需要决策
            </h1>
            <p className='text-sm text-muted-foreground'>
              全项目停在人工审批门的需求，点行进入详情处理后流水线继续
            </p>
          </div>
        </div>
      </Header>

      <div className='p-6'>
        {isLoading ? (
          <Skeleton className='h-64 rounded-lg' />
        ) : !rows.length ? (
          <div className='flex flex-col items-center gap-3 p-16 text-center'>
            <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
              <CircleSlash2 className='size-6 text-muted-foreground' />
            </div>
            <div>
              <p className='font-medium'>当前没有等待决策的需求</p>
              <p className='mt-1 text-sm text-muted-foreground'>
                需求停在审批门时会出现在这里
              </p>
            </div>
          </div>
        ) : (
          <div className='overflow-hidden rounded-md border'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='border-b bg-muted/50 text-left'>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    需求
                  </th>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    项目
                  </th>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    待决策门
                  </th>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    更新
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} className='border-b last:border-b-0'>
                    <td className='max-w-80 px-4 py-2.5'>
                      <Link
                        to='/deliveries/$id'
                        params={{ id: r.id }}
                        className='font-medium underline-offset-4 hover:underline'
                      >
                        {r.title}
                      </Link>
                      {r.multica_issue_key && (
                        <span className='ms-2 font-mono text-xs text-muted-foreground'>
                          {r.multica_issue_key}
                        </span>
                      )}
                    </td>
                    <td className='px-4 py-2.5 text-muted-foreground'>
                      {r.project_name}
                    </td>
                    <td className='px-4 py-2.5'>
                      <Badge variant='outline' className='font-normal'>
                        {stageLabel(r.pending_gate)}
                      </Badge>
                    </td>
                    <td className='px-4 py-2.5 text-xs text-muted-foreground tabular-nums'>
                      {timeAgo(r.updated_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </>
  )
}
