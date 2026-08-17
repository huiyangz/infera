import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Check, ChevronLeft, Plus, Undo2, X } from 'lucide-react'
import { toast } from 'sonner'
import { approveGate, getGate, rejectGate } from '@/lib/infera-api'
import type { ChildSpec } from '@/lib/infera-types'
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
import { Header } from '@/components/layout/header'

/** 门禁类型 → 展示配置；未知门禁回退通用文案，不崩。 */
const GATE_META: Record<
  string,
  { title: string; approveHint: string; artifactLabel: string; showPR: boolean }
> = {
  spec_approval: {
    title: 'Spec 审批',
    approveHint: '批准后进入测试生成；打回则 Spec Agent 重写',
    artifactLabel: 'Spec 内容',
    showPR: false,
  },
  code_review: {
    title: '代码审查',
    approveHint: '批准后合入（PR 已就绪）；打回则 Coder Agent 重做',
    artifactLabel: 'Reviewer 意见',
    showPR: true,
  },
}

export function GatePage({ deliveryId }: { deliveryId: string }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['gate', deliveryId],
    queryFn: () => getGate(deliveryId),
  })
  const [reason, setReason] = useState('')
  // 拆分方案行（spec_approval 专属）：AI 建议预填，可增删改
  const [rows, setRows] = useState<ChildSpec[] | null>(null)

  const back = () =>
    navigate({ to: '/deliveries/$id', params: { id: deliveryId } })

  const approve = useMutation({
    mutationFn: (split?: ChildSpec[]) => approveGate(deliveryId, split),
    onSuccess: (_d, split) => {
      qc.invalidateQueries()
      toast.success(
        split?.length
          ? `已拆分为 ${split.length} 个子需求，流水线调度中`
          : '已批准，流水线继续',
      )
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

  const meta = GATE_META[data.gate] ?? {
    title: '审批',
    approveHint: '批准后流水线继续',
    artifactLabel: '产出内容',
    showPR: false,
  }

  const isSpec = data.gate === 'spec_approval'
  // 懒播种：首次渲染用 AI 建议（或空）填充，之后完全由用户编辑
  const splitRows = rows ?? (data.split_plan ?? [])
  const setSplitRows = (next: ChildSpec[]) => setRows(next)
  const splitValid =
    splitRows.length > 0 && splitRows.every((r) => r.title.trim().length > 0)

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
          <span className='text-foreground'>{meta.title}</span>
        </div>
      </Header>

      <div className='mx-auto max-w-3xl p-6'>
        <Card>
          <CardHeader>
            <CardTitle>{meta.title}</CardTitle>
            <CardDescription>{meta.approveHint}</CardDescription>
          </CardHeader>
          <CardContent className='space-y-5'>
            <div>
              <h3 className='mb-2 text-sm font-medium'>{meta.artifactLabel}</h3>
              <pre className='min-h-24 rounded-lg border bg-muted/50 p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap'>
                {data.agent_output?.output || '（无 Agent 产出）'}
              </pre>
            </div>

            {meta.showPR && data.pr_url && (
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

            {isSpec && (
              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <h3 className='text-sm font-medium'>拆分执行（可选）</h3>
                  {data.split_plan?.length ? (
                    <span className='text-xs text-muted-foreground'>
                      AI 建议已预填，可清空
                    </span>
                  ) : null}
                </div>
                {splitRows.map((r, i) => (
                  <div key={i} className='flex items-center gap-2'>
                    <Input
                      className='w-40 shrink-0'
                      placeholder='子需求标题'
                      value={r.title}
                      onChange={(e) =>
                        setSplitRows(
                          splitRows.map((row, j) =>
                            j === i ? { ...row, title: e.target.value } : row,
                          ),
                        )
                      }
                    />
                    <Input
                      className='flex-1'
                      placeholder='补充描述…'
                      value={r.description}
                      onChange={(e) =>
                        setSplitRows(
                          splitRows.map((row, j) =>
                            j === i
                              ? { ...row, description: e.target.value }
                              : row,
                          ),
                        )
                      }
                    />
                    <Input
                      type='number'
                      min={1}
                      className='w-16 shrink-0 tabular-nums'
                      value={r.wave}
                      onChange={(e) =>
                        setSplitRows(
                          splitRows.map((row, j) =>
                            j === i
                              ? {
                                  ...row,
                                  wave: Math.max(1, Number(e.target.value) || 1),
                                }
                              : row,
                          ),
                        )
                      }
                    />
                    <Button
                      variant='ghost'
                      size='icon'
                      className='shrink-0'
                      aria-label='删除该子需求'
                      onClick={() =>
                        setSplitRows(splitRows.filter((_, j) => j !== i))
                      }
                    >
                      <X />
                    </Button>
                  </div>
                ))}
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() =>
                    setSplitRows([
                      ...splitRows,
                      { title: '', description: '', wave: 1 },
                    ])
                  }
                >
                  <Plus /> 添加子需求
                </Button>
              </div>
            )}

            <div className='flex items-center gap-2'>
              {isSpec ? (
                <>
                  <Button
                    size='lg'
                    disabled={approve.isPending}
                    onClick={() => approve.mutate(undefined)}
                  >
                    <Check /> 批准（不拆分）
                  </Button>
                  <Button
                    size='lg'
                    disabled={!splitValid || approve.isPending}
                    onClick={() => approve.mutate(splitRows)}
                  >
                    <Check /> 批准并拆分
                  </Button>
                </>
              ) : (
                <Button
                  size='lg'
                  disabled={approve.isPending}
                  onClick={() => approve.mutate(undefined)}
                >
                  <Check /> 批准
                </Button>
              )}
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
