import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Inbox, Plus } from 'lucide-react'
import { toast } from 'sonner'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { Header } from '@/components/layout/header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { createRequirement, listRequirements } from './api'
import { MergePolicySettings } from './merge-policy'
import { nodeLabel, type RequirementListItem } from './types'

/** 大节点徽标：异常节点以实底突出，其余描边（DESIGN.md：无信号色，用 tonal step） */
function NodeBadge({ node }: { node: RequirementListItem['node'] }) {
  const abnormal = node === 'needs_decision'
  return (
    <Badge
      variant='outline'
      className={cn(abnormal && 'border-transparent bg-primary text-primary-foreground')}
    >
      {nodeLabel(node)}
    </Badge>
  )
}

/** 验收人输入 → 数组（中英文逗号 / 顿号 / 空白皆可为分隔符） */
function parseAcceptors(raw: string): string[] {
  return raw
    .split(/[，,、\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

/**
 * 需求列表页（FR-2）：发起需求入口 + 需求行（大节点 / 待处理卡计数）。
 * 轮询刷新（FR-7）：react-query refetchInterval 周期拉取——用户停在页面即
 * 自动看到新卡片与节点推进；pollMs 可注入供测试加速。
 */
export function RequirementsList({ pollMs = 10_000 }: { pollMs?: number }) {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['requirements'],
    queryFn: () => listRequirements(),
    refetchInterval: pollMs,
  })

  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({
    title: '',
    description: '',
    acceptance_criteria: '',
    source: '',
    priority: '',
    acceptors: '',
  })
  const set = (k: keyof typeof form) => (v: string) =>
    setForm((f) => ({ ...f, [k]: v }))

  const create = useMutation({
    mutationFn: () =>
      createRequirement({
        title: form.title.trim(),
        description: form.description,
        acceptance_criteria: form.acceptance_criteria,
        source: form.source,
        priority: form.priority,
        acceptors: parseAcceptors(form.acceptors),
      }),
    onSuccess: (r) => {
      setOpen(false)
      setForm({
        title: '',
        description: '',
        acceptance_criteria: '',
        source: '',
        priority: '',
        acceptors: '',
      })
      toast.success(`任务「${r.title}」已派发`)
      qc.invalidateQueries({ queryKey: ['requirements'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const rows = data ?? []

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-lg font-semibold tracking-[-0.2px]'>任务</h1>
            <p className='text-sm text-muted-foreground'>
              在这里发起任务并跟进交付：批准计划、决策异常、确认合并
            </p>
          </div>
          <div className='flex items-center gap-1'>
            <MergePolicySettings />
            <Dialog open={open} onOpenChange={setOpen}>
              <DialogTrigger asChild>
                <Button size='lg'>
                  <Plus /> 发起任务
                </Button>
              </DialogTrigger>
              <DialogContent className='sm:max-w-lg'>
                <DialogHeader>
                  <DialogTitle>发起任务</DialogTitle>
                  <DialogDescription>
                    提交即派发给 Agent 团队执行；描述与验收标准只保存在
                    infera，不会外发
                  </DialogDescription>
                </DialogHeader>
                <form
                  className='grid gap-4'
                  onSubmit={(e) => {
                    e.preventDefault()
                    if (form.title.trim()) create.mutate()
                  }}
                >
                  <div className='grid gap-2'>
                    <Label htmlFor='req-title'>标题</Label>
                    <Input
                      id='req-title'
                      value={form.title}
                      onChange={(e) => set('title')(e.target.value)}
                      placeholder='一句话说清要做什么'
                    />
                  </div>
                  <div className='grid gap-2'>
                    <Label htmlFor='req-desc'>描述</Label>
                    <Textarea
                      id='req-desc'
                      value={form.description}
                      onChange={(e) => set('description')(e.target.value)}
                      placeholder='背景、期望行为、约束…'
                    />
                  </div>
                  <div className='grid gap-2'>
                    <Label htmlFor='req-ac'>验收标准</Label>
                    <Textarea
                      id='req-ac'
                      value={form.acceptance_criteria}
                      onChange={(e) => set('acceptance_criteria')(e.target.value)}
                      placeholder='满足什么条件即算完成…'
                    />
                  </div>
                  <div className='grid grid-cols-2 gap-4'>
                    <div className='grid gap-2'>
                      <Label htmlFor='req-source'>来源</Label>
                      <Input
                        id='req-source'
                        value={form.source}
                        onChange={(e) => set('source')(e.target.value)}
                        placeholder='如 web / 客户 / 内部'
                      />
                    </div>
                    <div className='grid gap-2'>
                      <Label htmlFor='req-priority'>优先级</Label>
                      <Input
                        id='req-priority'
                        value={form.priority}
                        onChange={(e) => set('priority')(e.target.value)}
                        placeholder='P1'
                      />
                    </div>
                  </div>
                  <div className='grid gap-2'>
                    <Label htmlFor='req-acceptors'>验收人</Label>
                    <Input
                      id='req-acceptors'
                      value={form.acceptors}
                      onChange={(e) => set('acceptors')(e.target.value)}
                      placeholder='多人用顿号或逗号分隔'
                    />
                  </div>
                  <DialogFooter>
                    <Button
                      type='submit'
                      disabled={!form.title.trim() || create.isPending}
                    >
                      提交任务
                    </Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
          </div>
        </div>
      </Header>

      <div className='p-6'>
        {isLoading ? (
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
            <Skeleton className='h-28 rounded-lg' />
            <Skeleton className='h-28 rounded-lg' />
            <Skeleton className='h-28 rounded-lg' />
          </div>
        ) : !rows.length ? (
          <div className='flex flex-col items-center gap-3 p-16 text-center'>
            <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
              <Inbox className='size-6 text-muted-foreground' />
            </div>
            <div>
              <p className='font-medium'>还没有任务</p>
              <p className='mt-1 text-sm text-muted-foreground'>
                点右上角「发起任务」，提交后会自动派发给 Agent 团队
              </p>
            </div>
          </div>
        ) : (
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
            {rows.map((r) => (
              <Card key={r.id} className='group relative gap-3 py-5 transition-colors hover:bg-accent/50'>
                <CardHeader className='px-5'>
                  <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
                    <Link
                      to='/requirements/$id'
                      params={{ id: r.id }}
                      className='after:absolute after:inset-0'
                    >
                      {r.title}
                    </Link>
                  </CardTitle>
                  <CardAction className='relative z-10'>
                    <NodeBadge node={r.node} />
                  </CardAction>
                </CardHeader>
                <CardContent className='grid gap-2 px-5'>
                  <div className='flex min-h-6 flex-wrap items-center gap-1.5'>
                    {r.pending_card_count > 0 && (
                      <Badge>
                        <Link
                          to='/requirements/$id'
                          params={{ id: r.id }}
                          className='after:absolute after:inset-0'
                        >
                          {r.pending_card_count} 张待处理卡
                        </Link>
                      </Badge>
                    )}
                    {r.pending_card_count === 0 && (
                      <span className='text-xs text-muted-foreground'>
                        暂无待处理事项
                      </span>
                    )}
                  </div>
                  <p className='text-xs tabular-nums text-muted-foreground'>
                    活动 {timeAgo(r.updated_at)}
                    {r.multica_issue_key && ` · ${r.multica_issue_key}`}
                  </p>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </div>
    </>
  )
}
