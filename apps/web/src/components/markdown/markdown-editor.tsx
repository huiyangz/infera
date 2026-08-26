import { useState, type KeyboardEvent } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import './markdown.css'

/**
 * Markdown 展示 / 编辑基础组件（INFERA-295）。
 *
 * 受控组件：`value` 由调用方持有，编辑经 `onChange` 上抛，保存经 `onSave`
 * 上抛——组件自身不发请求、不落库，接入方（任务详情页）决定怎么存。
 *
 * 选型：react-markdown + remark-gfm（理由见交付说明）。安全策略是结构性的：
 * 不启用 rehype-raw，源码里的内嵌 HTML 一律按字面文本渲染（经 React 转义），
 * 从根上没有注入面；链接 URL 再经 react-markdown 默认 urlTransform 过滤，
 * 仅放行 http/https/mailto 等安全协议，`javascript:` 会被剥掉。
 */

/** 展示模式：preview 渲染结果，source 可编辑的原始文本 */
export type MarkdownMode = 'preview' | 'source'

/** 模式切换分段控件的取值，顺序即展示顺序 */
const MODES: Array<{ key: MarkdownMode; label: string }> = [
  { key: 'preview', label: '预览' },
  { key: 'source', label: '源码' },
]

export type MarkdownEditorProps = {
  /** Markdown 源文本（受控） */
  value: string
  /** 源文本变化（源码模式下编辑触发） */
  onChange?: (next: string) => void
  /** 保存回调：保存按钮与源码模式 Cmd/Ctrl+S 触发，携带当前 value */
  onSave?: (next: string) => void
  /** 初始展示模式，默认 preview */
  defaultMode?: MarkdownMode
  /** 是否可编辑。false 时纯展示（无切换、无保存），供只读区域复用 */
  editable?: boolean
  /** 源码模式空值占位文案 */
  placeholder?: string
  className?: string
}

/** 源码模式 Cmd/Ctrl+S：保存并吞掉浏览器默认「另存页面」 */
function isSaveShortcut(event: KeyboardEvent<HTMLTextAreaElement>): boolean {
  return (
    (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's'
  )
}

export function MarkdownEditor({
  value,
  onChange,
  onSave,
  defaultMode = 'preview',
  editable = true,
  placeholder = '支持 Markdown 语法',
  className,
}: MarkdownEditorProps) {
  const [mode, setMode] = useState<MarkdownMode>(defaultMode)
  // 只读形态不提供切换入口，固定落在 preview（defaultMode 与 editable=false
  // 同时传入时以只读语义为准，不出现一个进不去源码模式的假开关）
  const activeMode: MarkdownMode = editable ? mode : 'preview'

  return (
    <div
      data-slot='markdown-editor'
      className={cn('flex flex-col gap-3', className)}
    >
      {editable && (
        <div className='flex items-center justify-between gap-2'>
          <div
            role='group'
            aria-label='Markdown 显示模式'
            className='inline-flex items-center gap-0.5 rounded-full border border-border p-0.5'
          >
            {MODES.map(({ key, label }) => (
              <button
                key={key}
                type='button'
                aria-pressed={activeMode === key}
                onClick={() => setMode(key)}
                className={cn(
                  'h-7 rounded-full px-3 text-xs font-medium transition-colors',
                  activeMode === key
                    ? 'bg-primary text-primary-foreground'
                    : 'text-muted-foreground hover:text-foreground'
                )}
              >
                {label}
              </button>
            ))}
          </div>
          {onSave && (
            <Button size='sm' onClick={() => onSave(value)}>
              保存
            </Button>
          )}
        </div>
      )}

      {activeMode === 'source' ? (
        <Textarea
          aria-label='Markdown 源码'
          spellCheck={false}
          value={value}
          placeholder={placeholder}
          onChange={(event) => onChange?.(event.target.value)}
          onKeyDown={(event) => {
            if (isSaveShortcut(event)) {
              event.preventDefault()
              onSave?.(value)
            }
          }}
          className='min-h-48 resize-y font-mono text-[13px] leading-relaxed'
        />
      ) : (
        <div className='md-render'>
          <Markdown
            remarkPlugins={[remarkGfm]}
            components={{
              // 外链统一新开页并断开 opener，避免 in-app 文档被反向操控。
              // node 是 react-markdown 传给自定义组件的内部属性，必须摘掉，
              // 否则会经 {...props} 落到 DOM 上（node="[object Object]"）
              a: ({ node: _node, ...props }) => (
                <a {...props} target='_blank' rel='noopener noreferrer' />
              ),
            }}
          >
            {value}
          </Markdown>
        </div>
      )}
    </div>
  )
}
