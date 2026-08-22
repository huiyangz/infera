import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronLeft, ExternalLink, Inbox } from 'lucide-react'
import { ApiError } from '@/lib/infera-api'
import { dateTime } from '@/lib/time'
import { Header } from '@/components/layout/header'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { getRequirement, listRequirementAudit } from './api'
import { GateCardView } from './gate-cards'
import { NodeTimeline } from './node-timeline'
import { nodeLabel } from './types'

/** 审计动作 → 中文；未知动作回退原文 */
const ACTION_META: Record<string, string> = {
  approve: '批准',
  reject: '驳回',
  decide: '决策',
  merge: '合并',
  rework: '返工',
}

/** 页面级深链行（卡片内深链由各卡自带） */
function DeepLinks({
  issueKey,
  issueUrl,
  prUrl,
}: {
  issueKey: string
  issueUrl: string
  prUrl: string
}) {
  return (
    <div className='flex flex-wrap items-center gap-4 text-sm'>
      {issueUrl && (
        <a
          href={issueUrl}
          target='_blank'
          rel='noreferrer'
          className='inline-flex items-center gap-1 underline underline-offset-4 hover:text-primary'
        >
          <ExternalLink className='size-3.5' />
          {issueKey || '执行 issue'}
        </a>
      )}
      {prUrl && (
        <a
          href={prUrl}
          target='_blank'
          rel='noreferrer'
          className='inline-flex items-center gap-1 font-mono text-xs underline underline-offset-4 hover:text-primary'
        >
          <ExternalLink className='size-3.5' />
          {prUrl.replace('https://github.com/', '')}
        </a>
      )}
    </div>
  )
}

/**
 * 需求详情（FR-1/FR-4/FR-5/FR-8）：需求信息 + 大节点时间线 + 待处理闸门卡 +
 * 操作审计。轮询刷新与列表页同机制（refetchInterval，无 WS）。
 */
export function RequirementDetailPage({
  requirementId,
  pollMs = 10_000,
}: {
  requirementId: string
  pollMs?: number
}) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['requirement', requirementId],
    queryFn: () => getRequirement(requirementId),
    refetchInterval: pollMs,
    retry: false,
  })
  const { data: audit } = useQuery({
    queryKey: ['requirement-audit', requirementId],
    queryFn: () => listRequirementAudit(requirementId),
    refetchInterval: pollMs,
  })

  if (isLoading)
    return (
      <>
        <Header fixed>
          <Skeleton className='h-4 w-40' />
        </Header>
        <div className='mx-auto max-w-4xl space-y-4 p-6'>
          <Skeleton className='h-40 w-full rounded-xl' />
          <Skeleton className='h-24 w-full rounded-xl' />
        </div>
      </>
    )

  if (error || !data)
    return (
      <>
        <Header fixed>
          <div className='flex w-full items-center gap-1 text-sm text-muted-foreground'>
            <ChevronLeft className='size-4' />
            <Link to='/requirements' className='hover:text-foreground'>
              返回任务
            </Link>
          </div>
        </Header>
        <div className='mx-auto max-w-4xl p-6'>
          <Card>
            <CardContent className='py-8 text-center text-sm text-muted-foreground'>
              {error instanceof ApiError && error.status === 404
                ? '任务不存在或已删除'
                : `加载失败：${error?.message ?? '未知错误'}`}
            </CardContent>
          </Card>
        </div>
      </>
    )

  const cards = data.pending_cards ?? []

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center gap-2'>
          <div className='flex items-center gap-1 text-sm text-muted-foreground'>
            <ChevronLeft className='size-4' />
            <Link to='/requirements' className='hover:text-foreground'>
              返回任务
            </Link>
          </div>
          <span className='text-sm text-muted-foreground'>/</span>
          <span className='truncate text-sm font-medium text-foreground'>
            {data.title}
          </span>
          <Badge
            variant='outline'
            className={
              data.node === 'needs_decision'
                ? 'border-transparent bg-primary text-primary-foreground'
                : undefined
            }
          >
            {nodeLabel(data.node)}
          </Badge>
        </div>
      </Header>

      <div className='mx-auto max-w-4xl space-y-4 p-6'>
        <Card className='gap-4 py-5'>
          <CardHeader className='px-5'>
            <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
              {data.title}
            </CardTitle>
            <CardDescription>
              来源 {data.source || '—'} · 优先级 {data.priority || '—'} · 验收人{' '}
              {data.acceptors.length ? data.acceptors.join('、') : '—'}
            </CardDescription>
          </CardHeader>
          <CardContent className='grid gap-4 px-5'>
            {data.description && (
              <div>
                <h3 className='mb-1 text-xs font-medium text-muted-foreground'>
                  描述
                </h3>
                <p className='text-sm leading-relaxed whitespace-pre-wrap'>
                  {data.description}
                </p>
              </div>
            )}
            {data.acceptance_criteria && (
              <div>
                <h3 className='mb-1 text-xs font-medium text-muted-foreground'>
                  验收标准
                </h3>
                <p className='text-sm leading-relaxed whitespace-pre-wrap'>
                  {data.acceptance_criteria}
                </p>
              </div>
            )}
            <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
              <DeepLinks
                issueKey={data.external_issue_key}
                issueUrl={data.external_issue_url}
                prUrl={data.pr_url}
              />
              <p className='text-xs tabular-nums text-muted-foreground'>
                创建 {dateTime(data.created_at)} · 更新 {dateTime(data.updated_at)}
              </p>
            </div>
          </CardContent>
        </Card>

        <Card className='gap-4 py-5'>
          <CardHeader className='px-5'>
            <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
              流程
            </CardTitle>
          </CardHeader>
          <CardContent className='px-5'>
            <NodeTimeline node={data.node} />
          </CardContent>
        </Card>

        <div className='space-y-4'>
          <h2 className='text-sm font-medium'>待处理（{cards.length}）</h2>
          {cards.length ? (
            cards.map((c) => (
              <GateCardView key={c.id} card={c} requirement={data} />
            ))
          ) : (
            <div className='flex items-center gap-3 rounded-lg border p-6 text-sm text-muted-foreground'>
              <Inbox className='size-4' />
              暂无待处理事项——新动态会自动出现
            </div>
          )}
        </div>

        <Card className='gap-4 py-5'>
          <CardHeader className='px-5'>
            <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
              操作记录
            </CardTitle>
            <CardDescription>你在 infera 侧的动作审计（只增不改）</CardDescription>
          </CardHeader>
          <CardContent className='px-5'>
            {(audit ?? []).length === 0 ? (
              <p className='text-sm text-muted-foreground'>暂无记录</p>
            ) : (
              <ul className='divide-y'>
                {(audit ?? []).map((a) => (
                  <li key={a.id} className='flex flex-wrap items-baseline gap-2 py-2.5 text-sm'>
                    <span className='font-medium'>
                      {ACTION_META[a.action] ?? a.action}
                    </span>
                    {a.detail && (
                      <span className='min-w-0 flex-1 truncate text-muted-foreground'>
                        {a.detail}
                      </span>
                    )}
                    <span className='text-xs tabular-nums text-muted-foreground'>
                      {a.actor} · {dateTime(a.at)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  )
}
