import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { toast } from 'sonner'
import { listProjects } from '@/lib/infera-api'
import { Button } from '@/components/ui/button'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { createProjectRequirement } from './api'

/** 状态两档（契约：backlog 不触发 run / todo 指派即唤醒） */
const STATUS_OPTIONS = [
  { value: 'backlog', label: '待规划' },
  { value: 'todo', label: '待办' },
] as const

/** 优先级词表（契约：上游 urgent/high/medium/low/none 透传） */
const PRIORITY_OPTIONS = [
  { value: 'none', label: '无' },
  { value: 'urgent', label: '紧急' },
  { value: 'high', label: '高' },
  { value: 'medium', label: '中' },
  { value: 'low', label: '低' },
] as const

/**
 * 新建需求对话框（INFERA-178）：项目详情页与项目任务列表页共享的创建入口，
 * Multica 风格——提交走 Layer 1 冻结契约 POST /api/projects/{id}/requirements
 * （上游建卡 + 同步回流），成功后失效 task-groups 让列表出新卡。
 * 智能体默认 Tech Lead（agent_id 空串由服务端解析，见 syncsvc.Creator）；
 * 无 agent 列表端点，「自定义…」档以 agent id 显式指派。
 */
export function CreateRequirementDialog({ projectId }: { projectId: string }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [status, setStatus] = useState<'backlog' | 'todo'>('backlog')
  const [priority, setPriority] = useState<string>('none')
  const [autoMerge, setAutoMerge] = useState(false)
  const [agentChoice, setAgentChoice] = useState<'tech-lead' | 'custom'>(
    'tech-lead',
  )
  const [customAgentId, setCustomAgentId] = useState('')
  const [targetProjectId, setTargetProjectId] = useState(projectId)

  const { data: projects } = useQuery({
    queryKey: ['projects'],
    queryFn: () => listProjects(false),
    enabled: open,
  })

  const reset = () => {
    setTitle('')
    setDescription('')
    setStatus('backlog')
    setPriority('none')
    setAutoMerge(false)
    setAgentChoice('tech-lead')
    setCustomAgentId('')
    setTargetProjectId(projectId)
  }

  const create = useMutation({
    mutationFn: () =>
      createProjectRequirement(targetProjectId, {
        title,
        description,
        status,
        priority,
        auto_merge: autoMerge,
        agent_id: agentChoice === 'custom' ? customAgentId : '',
      }),
    onSuccess: () => {
      toast.success('需求已创建')
      setOpen(false)
      reset()
      qc.invalidateQueries({ queryKey: ['project-task-groups'] })
    },
    onError: () =>
      // 后端错误文案可能含上游/同步细节（如「项目未绑定上游映射」），
      // 一律给中性提示不透传（INFERA-194）；对话框保持打开可修改重试
      toast.error('创建失败，请稍后重试'),
  })

  const canSubmit =
    !!title.trim() &&
    (agentChoice !== 'custom' || !!customAgentId.trim()) &&
    !create.isPending

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <Plus /> 新建需求
        </Button>
      </DialogTrigger>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>新建需求</DialogTitle>
          <DialogDescription>
            状态选「待办」会立即唤醒智能体
          </DialogDescription>
        </DialogHeader>
        <form
          className='grid gap-4'
          onSubmit={(e) => {
            e.preventDefault()
            if (canSubmit) create.mutate()
          }}
        >
          <div className='grid gap-2'>
            <Label htmlFor='req-title'>标题</Label>
            <Input
              id='req-title'
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder='一句话说清要交付什么'
            />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='req-description'>描述</Label>
            <Textarea
              id='req-description'
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder='背景、验收标准、约束…（可留空）'
            />
          </div>
          <div className='grid gap-2'>
            <Label>状态</Label>
            <Select value={status} onValueChange={(v) => setStatus(v as 'backlog' | 'todo')}>
              <SelectTrigger aria-label='状态'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STATUS_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label>优先级</Label>
            <Select value={priority} onValueChange={setPriority}>
              <SelectTrigger aria-label='优先级'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PRIORITY_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label>智能体</Label>
            <Select
              value={agentChoice}
              onValueChange={(v) => setAgentChoice(v as 'tech-lead' | 'custom')}
            >
              <SelectTrigger aria-label='智能体'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='tech-lead'>Tech Lead（默认）</SelectItem>
                <SelectItem value='custom'>自定义…</SelectItem>
              </SelectContent>
            </Select>
            {agentChoice === 'custom' && (
              <Input
                aria-label='智能体 ID'
                className='font-mono text-xs'
                value={customAgentId}
                onChange={(e) => setCustomAgentId(e.target.value)}
                placeholder='agent UUID'
              />
            )}
          </div>
          <div className='grid gap-2'>
            <Label>项目</Label>
            <Select value={targetProjectId} onValueChange={setTargetProjectId}>
              <SelectTrigger aria-label='项目'>
                <SelectValue placeholder='选择项目…' />
              </SelectTrigger>
              <SelectContent>
                {(projects ?? []).map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='flex items-center justify-between'>
            <div>
              <Label htmlFor='req-automerge'>自动合并</Label>
              <p className='text-xs text-muted-foreground'>
                开启后自动打 auto 标签
              </p>
            </div>
            <Switch
              id='req-automerge'
              checked={autoMerge}
              onCheckedChange={setAutoMerge}
            />
          </div>
          <DialogFooter>
            <Button type='submit' disabled={!canSubmit}>
              {create.isPending ? '创建中…' : '创建需求'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
