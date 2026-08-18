import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot, Pencil, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import {
  createAgent,
  deleteAgent,
  getPipeline,
  getProjectPipeline,
  listAgents,
  listProjects,
  updateAgent,
} from '@/lib/infera-api'
import { type Agent, type AgentRunner, stageLabel } from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
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
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { Header } from '@/components/layout/header'

// —— runner 元数据（必填项与提示的唯一来源） ——

const RUNNERS: Record<AgentRunner, { label: string; hint: string }> = {
  cli: { label: 'CLI', hint: '服务器子进程执行，command 为 argv 数组' },
  http: { label: 'HTTP', hint: 'POST 到服务地址，返回 {output}' },
  docker: { label: 'Docker', hint: '容器内执行，指定镜像与入口 argv' },
  local: { label: '本机', hint: '停在该阶段等待本机交互，无需配置' },
}

/** 列表里的配置摘要（一屏看懂这个 agent 怎么跑） */
function configSummary(a: Agent): string {
  const cfg = a.config ?? {}
  switch (a.runner) {
    case 'cli':
      return Array.isArray(cfg.command) ? cfg.command.join(' ') : ''
    case 'http':
      return typeof cfg.url === 'string' ? cfg.url : ''
    case 'docker':
      return typeof cfg.image === 'string' ? cfg.image : ''
    default:
      return ''
  }
}

// —— 表单模型：所有 runner 的字段摊平，提交时只取当前类型的 ——

interface AgentForm {
  name: string
  runner: AgentRunner
  /** cli/docker：argv，每行一个参数（可含 $PROMPT/$WORKDIR/$ROLE 占位符） */
  command: string
  /** http */
  url: string
  /** docker */
  image: string
}

const EMPTY_FORM: AgentForm = {
  name: '',
  runner: 'cli',
  command: '',
  url: '',
  image: '',
}

function formFromAgent(a: Agent): AgentForm {
  const cfg = a.config ?? {}
  const argv = Array.isArray(cfg.command)
    ? cfg.command.map(String).join('\n')
    : ''
  return {
    name: a.name,
    runner: a.runner,
    command: argv,
    url: typeof cfg.url === 'string' ? cfg.url : '',
    image: typeof cfg.image === 'string' ? cfg.image : '',
  }
}

/** 每行一个参数 → argv；空行剔除 */
function parseArgv(text: string): string[] {
  return text
    .split('\n')
    .map((s) => s.trim())
    .filter(Boolean)
}

/** 必填校验：返回首个错误文案（空串 = 通过） */
function validate(f: AgentForm): string {
  if (!f.name.trim()) return '名称不能为空'
  switch (f.runner) {
    case 'cli':
      if (!parseArgv(f.command).length) return 'CLI 需要至少一行命令行参数'
      break
    case 'http':
      if (!f.url.trim()) return 'HTTP 需要服务地址'
      if (!/^https?:\/\/.+/.test(f.url.trim()))
        return '服务地址需以 http:// 或 https:// 开头'
      break
    case 'docker':
      if (!f.image.trim()) return 'Docker 需要镜像名'
      break
  }
  return ''
}

/** 提交体：只带当前 runner 用到的配置键 */
function buildPayload(f: AgentForm): {
  name: string
  runner: AgentRunner
  config: Record<string, unknown>
} {
  const config: Record<string, unknown> = {}
  if (f.runner === 'cli' || f.runner === 'docker') {
    const argv = parseArgv(f.command)
    if (argv.length) config.command = argv
  }
  if (f.runner === 'http') config.url = f.url.trim()
  if (f.runner === 'docker') config.image = f.image.trim()
  return { name: f.name.trim(), runner: f.runner, config }
}

/**
 * 注册/编辑对话框：runner 切换时动态渲染对应配置项，
 * 必填项缺失时按钮禁用并给出提示。
 */
function AgentFormDialog({
  editing,
  onClose,
}: {
  /** null = 注册；否则编辑该 agent */
  editing: Agent | null
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [form, setForm] = useState<AgentForm>(
    editing ? formFromAgent(editing) : EMPTY_FORM
  )
  const err = validate(form)

  const save = useMutation({
    mutationFn: () =>
      editing
        ? updateAgent(editing.id, buildPayload(form))
        : createAgent(buildPayload(form)),
    onSuccess: (a) => {
      toast.success(
        editing ? `Agent ${a.name} 已更新` : `Agent ${a.name} 已注册`
      )
      qc.invalidateQueries({ queryKey: ['agents'] })
      // 默认编排对话框内嵌 agents 摘要
      qc.invalidateQueries({ queryKey: ['pipeline'] })
      onClose()
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const set = <K extends keyof AgentForm>(k: K, v: AgentForm[K]) =>
    setForm((f) => ({ ...f, [k]: v }))

  return (
    <Dialog open onOpenChange={(o) => !o && !save.isPending && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>
            {editing ? `编辑 Agent · ${editing.name}` : '注册 Agent'}
          </DialogTitle>
          <DialogDescription>{RUNNERS[form.runner].hint}</DialogDescription>
        </DialogHeader>
        <form
          className='grid gap-4'
          onSubmit={(e) => {
            e.preventDefault()
            if (!err) save.mutate()
          }}
        >
          <div className='grid gap-2'>
            <Label htmlFor='agent-name'>名称</Label>
            <Input
              id='agent-name'
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder='my-agent'
            />
          </div>
          <div className='grid gap-2'>
            <Label>执行方式</Label>
            <Select
              value={form.runner}
              onValueChange={(v) => set('runner', v as AgentRunner)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(RUNNERS) as AgentRunner[]).map((r) => (
                  <SelectItem key={r} value={r}>
                    {RUNNERS[r].label}
                    <span className='ms-1.5 font-mono text-xs text-muted-foreground'>
                      {r}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {(form.runner === 'cli' || form.runner === 'docker') && (
            <div className='grid gap-2'>
              <Label htmlFor='agent-command'>
                命令行参数{form.runner === 'docker' ? '（可选）' : ''}
              </Label>
              <Textarea
                id='agent-command'
                className='min-h-24 font-mono text-xs'
                value={form.command}
                onChange={(e) => set('command', e.target.value)}
                placeholder={'每行一个参数，例如：\nclaude\n-p\n$PROMPT'}
              />
              <p className='text-xs text-muted-foreground'>
                支持 $PROMPT / $WORKDIR / $ROLE 占位符
              </p>
            </div>
          )}
          {form.runner === 'http' && (
            <div className='grid gap-2'>
              <Label htmlFor='agent-url'>服务地址</Label>
              <Input
                id='agent-url'
                className='font-mono text-xs'
                value={form.url}
                onChange={(e) => set('url', e.target.value)}
                placeholder='http://127.0.0.1:9000/run'
              />
            </div>
          )}
          {form.runner === 'docker' && (
            <div className='grid gap-2'>
              <Label htmlFor='agent-image'>镜像</Label>
              <Input
                id='agent-image'
                className='font-mono text-xs'
                value={form.image}
                onChange={(e) => set('image', e.target.value)}
                placeholder='node:22-alpine'
              />
            </div>
          )}
          {form.runner === 'local' && (
            <p className='rounded-md bg-muted/50 px-3 py-2 text-xs text-muted-foreground'>
              本机执行方式无额外配置，交付停在该节点等待人工处理
            </p>
          )}

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              disabled={save.isPending}
              onClick={onClose}
            >
              取消
            </Button>
            <Button type='submit' disabled={!!err || save.isPending}>
              {save.isPending ? '保存中…' : editing ? '保存修改' : '注册'}
            </Button>
          </DialogFooter>
          {err && (
            <p className='text-sm text-destructive' role='alert'>
              {err}
            </p>
          )}
        </form>
      </DialogContent>
    </Dialog>
  )
}

/**
 * 删除确认：打开前先用绑定 API 聚合引用（默认编排 + 各项目覆盖）。
 * 有引用 → 只列清单不给删除（后端同样会 409 拒绝）；无引用 → 可确认删除。
 */
function DeleteAgentDialog({
  agent,
  onClose,
}: {
  agent: Agent
  onClose: () => void
}) {
  const qc = useQueryClient()
  const { data: refs, isLoading } = useQuery({
    queryKey: ['agent-references', agent.id],
    queryFn: () => collectReferences(agent.id),
    enabled: !!agent.id,
  })

  const del = useMutation({
    mutationFn: () => deleteAgent(agent.id),
    onSuccess: () => {
      toast.success(`Agent ${agent.name} 已删除`)
      qc.invalidateQueries({ queryKey: ['agents'] })
      qc.invalidateQueries({ queryKey: ['pipeline'] })
      onClose()
    },
    // 前端聚合与后端校验之间可能有并发改动，409 文案里带引用位置
    onError: (e: Error) => toast.error(e.message),
  })

  const blocked = !!refs?.length

  return (
    <AlertDialog open onOpenChange={(o) => !o && !del.isPending && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {blocked
              ? `无法删除 Agent「${agent.name}」`
              : `删除 Agent「${agent.name}」？`}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {blocked
              ? '以下位置正在绑定该 Agent，请先在对应编排中改绑后再删除：'
              : '没有编排绑定引用该 Agent，删除后不可恢复。'}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {isLoading ? (
          <Skeleton className='h-16 w-full' />
        ) : blocked ? (
          <ul className='max-h-52 overflow-auto rounded-md border'>
            {refs!.map((r) => (
              <li
                key={`${r.scope}/${r.label}`}
                className='flex items-center gap-2 border-b px-3 py-2 text-sm last:border-b-0'
              >
                <Badge variant='outline' className='shrink-0 font-normal'>
                  {r.scope}
                </Badge>
                <span className='font-medium'>{r.label}</span>
              </li>
            ))}
          </ul>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={del.isPending}>关闭</AlertDialogCancel>
          {!blocked && !isLoading && (
            <AlertDialogAction
              disabled={del.isPending}
              onClick={(e) => {
                e.preventDefault() // 等删除成功再关，失败留在对话框看报错
                del.mutate()
              }}
            >
              {del.isPending ? '删除中…' : '删除'}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

interface AgentReference {
  /** 绑定范围：'默认' | 项目名 */
  scope: string
  /** 节点中文名 */
  label: string
}

/**
 * 聚合引用某 agent 的绑定位置：全局默认绑定 + 各项目覆盖
 * （仅直接绑定；继承默认的项目不重复列）。全部走现有编排 API。
 */
async function collectReferences(agentId: string): Promise<AgentReference[]> {
  const out: AgentReference[] = []
  const pipe = await getPipeline()
  for (const [node, id] of Object.entries(pipe.bindings)) {
    if (id === agentId) out.push({ scope: '默认', label: stageLabel(node) })
  }
  const projects = await listProjects()
  const perProject = await Promise.all(
    projects.map(
      (p) =>
        getProjectPipeline(p.id)
          .then((pp) => ({ name: p.name, pp }))
          .catch(() => null) // 单项目读取失败不阻塞其余引用展示
    )
  )
  for (const r of perProject) {
    if (!r) continue
    for (const [node, id] of Object.entries(r.pp.overrides)) {
      if (id === agentId) out.push({ scope: r.name, label: stageLabel(node) })
    }
  }
  return out
}

export function AgentsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(),
  })
  const [formTarget, setFormTarget] = useState<'new' | Agent | null>(null)
  const [deleting, setDeleting] = useState<Agent | null>(null)

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-lg font-semibold tracking-[-0.2px]'>Agent</h1>
            <p className='text-sm text-muted-foreground'>
              注册流水线各节点的执行者，再在默认或项目编排中绑定
            </p>
          </div>
          <Button size='lg' onClick={() => setFormTarget('new')}>
            注册 Agent
          </Button>
        </div>
      </Header>

      <div className='p-6'>
        {isLoading ? (
          <Skeleton className='h-64 rounded-lg' />
        ) : !data?.length ? (
          <div className='flex flex-col items-center gap-3 p-16 text-center'>
            <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
              <Bot className='size-6 text-muted-foreground' />
            </div>
            <div>
              <p className='font-medium'>还没有注册 Agent</p>
              <p className='mt-1 text-sm text-muted-foreground'>
                点右上角「注册 Agent」，流水线节点绑定后即可自动执行
              </p>
            </div>
          </div>
        ) : (
          <div className='overflow-hidden rounded-md border'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='border-b bg-muted/50 text-left'>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    名称
                  </th>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    执行方式
                  </th>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    配置
                  </th>
                  <th className='px-4 py-2 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
                    更新
                  </th>
                  <th className='w-24 px-4 py-2' />
                </tr>
              </thead>
              <tbody>
                {data.map((a) => (
                  <tr key={a.id} className='border-b last:border-b-0'>
                    <td className='px-4 py-2.5 font-medium'>{a.name}</td>
                    <td className='px-4 py-2.5'>
                      <Badge variant='outline' className='gap-1.5 font-normal'>
                        {RUNNERS[a.runner]?.label ?? a.runner}
                        <span className='font-mono text-[10px] text-muted-foreground'>
                          {a.runner}
                        </span>
                      </Badge>
                    </td>
                    <td className='max-w-72 truncate px-4 py-2.5 font-mono text-xs text-muted-foreground'>
                      {configSummary(a) || '—'}
                    </td>
                    <td className='px-4 py-2.5 text-xs text-muted-foreground tabular-nums'>
                      {timeAgo(a.updated_at)}
                    </td>
                    <td className='px-4 py-2.5 text-right'>
                      <Button
                        variant='ghost'
                        size='icon'
                        aria-label={`编辑 ${a.name}`}
                        onClick={() => setFormTarget(a)}
                      >
                        <Pencil />
                      </Button>
                      <Button
                        variant='ghost'
                        size='icon'
                        aria-label={`删除 ${a.name}`}
                        onClick={() => setDeleting(a)}
                      >
                        <Trash2 />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {formTarget && (
        <AgentFormDialog
          editing={formTarget === 'new' ? null : formTarget}
          onClose={() => setFormTarget(null)}
        />
      )}
      {deleting && (
        <DeleteAgentDialog agent={deleting} onClose={() => setDeleting(null)} />
      )}
    </>
  )
}
