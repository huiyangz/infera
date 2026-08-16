import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Check, ChevronLeft, Undo2 } from 'lucide-react'
import { toast } from 'sonner'
import { approveGate, getGate, rejectGate } from '@/lib/infera-api'
import { Header } from '@/components/layout/header'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'

export function GatePage({ deliveryId }: { deliveryId: string }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['gate', deliveryId],
    queryFn: () => getGate(deliveryId),
  })
  const [reason, setReason] = useState('')

  const back = () => navigate({ to: '/deliveries/$id', params: { id: deliveryId } })

  const approve = useMutation({
    mutationFn: () => approveGate(deliveryId),
    onSuccess: () => {
      qc.invalidateQueries()
      toast.success('已批准，流水线继续')
      back()
    },
    onError: (e: Error) => toast.error(e.message),
  })
  const reject = useMutation({
    mutationFn: () => rejectGate(deliveryId, reason),
    onSuccess: () => {
      qc.invalidateQueries()
      toast.success('已打回，Agent 将重新处理')
      back()
    },
    onError: (e: Error) => toast.error(e.message),
  })

  if (isLoading || !data)
    return (
      <>
        <Header fixed>
          <Skeleton className='h-4 w-40' />
        </Header>
        <div className='mx-auto max-w-3xl p-6'>
          <Skeleton className='h-64 w-full rounded-xl' />
        </div>
      </>
    )

  const isSpec = data.gate === 'spec_approval'

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center gap-1 text-sm text-muted-foreground'>
          <ChevronLeft className='size-4' />
          <Link
            to='/deliveries/$id'
            params={{ id: deliveryId }}
            className='hover:text-foreground'
          >
            返回详情
          </Link>
          <span>/</span>
          <span className='text-foreground'>
            {isSpec ? '规格审批' : '代码审查'}
          </span>
        </div>
      </Header>

      <div className='mx-auto max-w-3xl p-6'>
        <Card>
          <CardHeader>
            <CardTitle>{isSpec ? 'Spec 审批' : '代码审查'}</CardTitle>
            <CardDescription>
              {isSpec
                ? '批准后进入测试生成；打回则 Spec Agent 重写'
                : '批准后开出 PR；打回则 Coder Agent 重做'}
            </CardDescription>
          </CardHeader>
          <CardContent className='space-y-5'>
            <div>
              <h3 className='mb-2 text-sm font-medium'>
                {isSpec ? 'Spec 内容' : 'Reviewer 意见'}
              </h3>
              <pre className='min-h-24 whitespace-pre-wrap rounded-lg border bg-muted/50 p-4 font-mono text-xs leading-relaxed'>
                {data.agent_output?.output || '（无 Agent 产出）'}
              </pre>
            </div>

            {!isSpec && data.pr_url && (
              <p className='text-sm'>
                PR：
                <a
                  className='underline underline-offset-4 hover:text-primary'
                  href={data.pr_url}
                  target='_blank'
                  rel='noreferrer'
                >
                  {data.pr_url}
                </a>
              </p>
            )}

            <div className='flex items-center gap-2'>
              <Button
                size='lg'
                disabled={approve.isPending}
                onClick={() => approve.mutate()}
              >
                <Check /> 批准
              </Button>
              <Input
                className='flex-1'
                placeholder='打回理由…'
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
              <Button
                size='lg'
                variant='destructive'
                disabled={!reason.trim() || reject.isPending}
                onClick={() => reject.mutate()}
              >
                <Undo2 /> 打回
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </>
  )
}
