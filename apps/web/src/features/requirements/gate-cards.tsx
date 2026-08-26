import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, ExternalLink, MessageSquareOff, Undo2 } from 'lucide-react'
import { toast } from 'sonner'
import { MarkdownEditor } from '@/components/markdown/markdown-editor'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Textarea } from '@/components/ui/textarea'
import {
  approveCard,
  decideCard,
  getRequirementPRReview,
  mergeCard,
  rejectCard,
  reworkCard,
} from './api'
import type { GateCard, PRReview, Requirement } from './types'

/** 卡片动作成功后失效需求详情与列表（轮询外的一致性兜底） */
function useInvalidateCards(requirementId: string) {
  const qc = useQueryClient()
  return () => {
    qc.invalidateQueries({ queryKey: ['requirement', requirementId] })
    qc.invalidateQueries({ queryKey: ['requirements'] })
  }
}

/** 深链逃生口（FR-8）：默认不打扰——普通链接、新窗口打开，不自动跳转 */
function DeepLink({ url, label }: { url: string; label?: string }) {
  if (!url) return null
  return (
    <a
      href={url}
      target='_blank'
      rel='noreferrer'
      className='inline-flex items-center gap-1 text-xs text-muted-foreground underline underline-offset-4 hover:text-foreground'
    >
      <ExternalLink className='size-3' />
      {label ?? '查看完整时间线'}
    </a>
  )
}

/** 卡片共享骨架：标题 + 描述 + 触发评论正文 + 底部（深链 + 动作区） */
function GateCardShell({
  title,
  description,
  payload,
  footer,
  children,
}: {
  title: string
  description: string
  payload: string
  footer?: React.ReactNode
  children?: React.ReactNode
}) {
  return (
    <Card data-gap-card className='gap-4 py-5'>
      <CardHeader className='px-5'>
        <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
          {title}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className='grid gap-4 px-5'>
        {/* 触发评论正文是 agent 产出的 Markdown：只读渲染（INFERA-296，
            不再以 <pre> 原样平铺源码）；无正文时保留占位文案 */}
        {payload ? (
          <MarkdownEditor value={payload} editable={false} />
        ) : (
          <p className='text-sm text-muted-foreground'>（无正文）</p>
        )}
        {children}
      </CardContent>
      {footer && <CardFooter className='px-5'>{footer}</CardFooter>}
    </Card>
  )
}

/** 审批卡（FR-4）：计划正文 + 批准 / 驳回并反馈（反馈必填）。 */
export function ApprovalCard({
  card,
  requirement,
}: {
  card: GateCard
  requirement: Requirement
}) {
  const invalidate = useInvalidateCards(requirement.id)
  const [feedback, setFeedback] = useState('')

  const approve = useMutation({
    mutationFn: () => approveCard(requirement.id, card.id),
    onSuccess: () => {
      invalidate()
      toast.success('已批准')
    },
    onError: (e: Error) => toast.error(e.message),
  })
  const reject = useMutation({
    mutationFn: () => rejectCard(requirement.id, card.id, feedback),
    onSuccess: () => {
      invalidate()
      toast.success('已驳回，反馈已代发')
    },
    onError: (e: Error) => toast.error(e.message),
  })

  return (
    <GateCardShell
      title='审批'
      description='Agent 提交了执行计划，批准后继续；驳回时反馈将原样代发'
      payload={card.payload}
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-3'>
          <DeepLink url={requirement.external_issue_url} />
          <div className='flex items-center gap-2'>
            <Button
              size='lg'
              disabled={approve.isPending}
              onClick={() => approve.mutate()}
            >
              <Check /> 批准
            </Button>
            <Button
              size='lg'
              variant='destructive'
              disabled={!feedback.trim() || reject.isPending}
              onClick={() => reject.mutate()}
            >
              <Undo2 /> 驳回并反馈
            </Button>
          </div>
        </div>
      }
    >
      <Textarea
        aria-label='驳回反馈'
        placeholder='驳回反馈：说明需要调整的地方…'
        value={feedback}
        onChange={(e) => setFeedback(e.target.value)}
      />
    </GateCardShell>
  )
}

/** 决策卡（FR-4）：异常升级内容 + 重试 / 跳过 / 中止 / 自定义回复。 */
export function DecisionCard({
  card,
  requirement,
}: {
  card: GateCard
  requirement: Requirement
}) {
  const invalidate = useInvalidateCards(requirement.id)
  const [custom, setCustom] = useState('')

  const decide = useMutation({
    mutationFn: (input: { choice: string; text: string }) =>
      decideCard(requirement.id, card.id, input.choice, input.text),
    onSuccess: (_v, input) => {
      invalidate()
      toast.success(
        input.choice === 'custom' ? '自定义回复已代发' : `已决策：${input.choice}`
      )
    },
    onError: (e: Error) => toast.error(e.message),
  })
  const pending = decide.isPending

  return (
    <GateCardShell
      title='需决策'
      description='执行遇到异常，等待你的决策；选择将代发到执行侧'
      payload={card.payload}
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-3'>
          <DeepLink url={requirement.external_issue_url} />
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              size='lg'
              disabled={pending}
              onClick={() => decide.mutate({ choice: 'retry', text: '' })}
            >
              重试
            </Button>
            <Button
              size='lg'
              variant='outline'
              disabled={pending}
              onClick={() => decide.mutate({ choice: 'skip', text: '' })}
            >
              跳过
            </Button>
            <Button
              size='lg'
              variant='destructive'
              disabled={pending}
              onClick={() => decide.mutate({ choice: 'abort', text: '' })}
            >
              中止
            </Button>
            <Button
              size='lg'
              variant='outline'
              disabled={!custom.trim() || pending}
              onClick={() => decide.mutate({ choice: 'custom', text: custom })}
            >
              <MessageSquareOff /> 发送自定义回复
            </Button>
          </div>
        </div>
      }
    >
      <Textarea
        aria-label='自定义回复'
        placeholder='自定义回复内容…'
        value={custom}
        onChange={(e) => setCustom(e.target.value)}
      />
    </GateCardShell>
  )
}

/** verdict 结论词提取（与 flow.ExtractVerdict 同语义：全词、首个命中） */
const VERDICT_RE = /\b(PASS|FAIL)\b/

/**
 * PR 评审区（FR-4/FR-7）：行级评审评论（path:line / side / author / body）
 * 与 diff 概要（文件数与 +/- 行数），数据来自只读端点 pr-review——用户
 * 不访问 GitHub 页面。删除行评论 line=0，行号取 original_line。
 */
function PRReviewSection({ review }: { review: PRReview }) {
  return (
    <div data-pr-review className='grid gap-2'>
      <div className='text-xs tabular-nums text-muted-foreground'>
        <span className='font-medium'>diff 概要</span>
        <span className='ml-2'>
          {review.diff.files} 个文件 · +{review.diff.additions} / -
          {review.diff.deletions}
        </span>
      </div>
      {review.comments.length === 0 ? (
        <p className='text-xs text-muted-foreground'>无行级评审评论</p>
      ) : (
        <ul className='divide-y rounded-lg border'>
          {review.comments.map((c) => (
            <li key={c.id} className='grid gap-1 px-3 py-2.5'>
              <div className='flex flex-wrap items-baseline gap-x-2 gap-y-1 text-xs text-muted-foreground'>
                <span className='font-mono'>
                  {c.path}:{c.line || c.original_line}
                </span>
                <span>{c.side === 'LEFT' ? '删除行' : '新增行'}</span>
                <span>{c.author}</span>
              </div>
              {/* 评论正文按 Markdown 渲染（INFERA-296）；行级评论为只读 */}
              <MarkdownEditor value={c.body} editable={false} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** 合并卡（FR-5）：verdict 结论 + 行级评审评论与 diff 概要 + 合并 / 拒绝并返工。 */
export function MergeCard({
  card,
  requirement,
}: {
  card: GateCard
  requirement: Requirement
}) {
  const invalidate = useInvalidateCards(requirement.id)
  const [feedback, setFeedback] = useState('')
  const verdict = card.payload.match(VERDICT_RE)?.[1]
  // 评审面只读拉取：未关联 PR 时不请求；失败不拖垮卡片（verdict 照常呈现）。
  const { data: review } = useQuery({
    queryKey: ['requirement-pr-review', requirement.id],
    queryFn: () => getRequirementPRReview(requirement.id),
    enabled: requirement.pr_url !== '',
    retry: false,
  })

  const merge = useMutation({
    mutationFn: () => mergeCard(requirement.id, card.id),
    onSuccess: (res) => {
      invalidate()
      toast.success(res.merged ? '已合并' : `未合并：${res.message || 'PR 状态不允许'}`)
    },
    onError: (e: Error) => toast.error(e.message),
  })
  const rework = useMutation({
    mutationFn: () => reworkCard(requirement.id, card.id, feedback),
    onSuccess: () => {
      invalidate()
      toast.success('已拒绝合并，返工反馈已代发')
    },
    onError: (e: Error) => toast.error(e.message),
  })

  return (
    <GateCardShell
      title='合并'
      description='Reviewer 已给出结论，确认后合并 PR；拒绝时代工反馈原样代发'
      payload={card.payload}
      footer={
        <div className='flex w-full flex-wrap items-center justify-between gap-3'>
          <div className='flex items-center gap-3'>
            <DeepLink url={requirement.external_issue_url} />
            <DeepLink url={requirement.pr_url} label='查看 PR' />
          </div>
          <div className='flex items-center gap-2'>
            <Button
              size='lg'
              disabled={merge.isPending}
              onClick={() => merge.mutate()}
            >
              <Check /> 合并
            </Button>
            <Button
              size='lg'
              variant='destructive'
              disabled={!feedback.trim() || rework.isPending}
              onClick={() => rework.mutate()}
            >
              <Undo2 /> 拒绝并返工
            </Button>
          </div>
        </div>
      }
    >
      <div className='flex flex-wrap items-center gap-2'>
        {verdict && (
          <Badge
            variant='outline'
            className={
              verdict === 'PASS'
                ? 'border-transparent bg-primary text-primary-foreground'
                : 'text-foreground'
            }
          >
            {verdict}
          </Badge>
        )}
        <span className='text-xs text-muted-foreground'>
          {verdict === 'PASS'
            ? '结论：通过'
            : verdict === 'FAIL'
              ? '结论：未通过'
              : '结论：未识别（按正文人工裁定）'}
        </span>
      </div>
      {review && <PRReviewSection review={review} />}
      <Textarea
        aria-label='返工反馈'
        placeholder='返工反馈：说明拒绝合并的原因…'
        value={feedback}
        onChange={(e) => setFeedback(e.target.value)}
      />
    </GateCardShell>
  )
}

/** 兜底「有新动态」卡（FR-4 规则一）：中性呈现，无动作，深链查看原文。 */
export function UpdateCard({
  card,
  requirement,
}: {
  card: GateCard
  requirement: Requirement
}) {
  return (
    <GateCardShell
      title='有新动态'
      description='执行侧有新进展，无需处理；如需细节可打开完整时间线'
      payload={card.payload}
      footer={
        <div className='flex w-full items-center justify-between gap-3'>
          <DeepLink url={requirement.external_issue_url} />
          <DeepLink url={requirement.pr_url} label='查看 PR' />
        </div>
      }
    />
  )
}

/** kind → 卡片组件分发（详情页用；未知类型按兜底卡渲染，不崩） */
export function GateCardView({
  card,
  requirement,
}: {
  card: GateCard
  requirement: Requirement
}) {
  switch (card.kind) {
    case 'approval':
      return <ApprovalCard card={card} requirement={requirement} />
    case 'decision':
      return <DecisionCard card={card} requirement={requirement} />
    case 'merge':
      return <MergeCard card={card} requirement={requirement} />
    default:
      return <UpdateCard card={card} requirement={requirement} />
  }
}
