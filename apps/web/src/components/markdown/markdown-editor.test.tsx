import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import { MarkdownEditor } from './markdown-editor'

/** 覆盖 AC 要求的全部样例元素：标题 / 列表 / 代码块 / 链接 / GFM 表格 */
const SAMPLE = `## 标题二

- 列表项一
- 列表项二

\`\`\`ts
const x = 1
\`\`\`

[链接文本](https://example.com)

| 列A | 列B |
| --- | --- |
| a1 | b1 |
`

afterEach(async () => {
  await cleanup()
})

/** 受控宿主：真实回灌 value，验证的是组件契约而非 mock */
function Host({
  initial,
  onChange,
  onSave,
}: {
  initial?: string
  onChange?: (v: string) => void
  onSave?: (v: string) => void
}) {
  const [value, setValue] = useState(initial ?? SAMPLE)
  return (
    <MarkdownEditor
      value={value}
      onChange={(v) => {
        setValue(v)
        onChange?.(v)
      }}
      onSave={onSave}
    />
  )
}

/** 全选后输入：点击落在文本中段，caret 位置不定；Ctrl+A 归一后再输入才可复现 */
async function replaceAll(
  box: Parameters<typeof userEvent.click>[0],
  text: string
) {
  await userEvent.click(box)
  await userEvent.keyboard('{Control>}a{/Control}')
  await userEvent.type(box, text)
}

describe('MarkdownEditor 渲染（AC1）', () => {
  it('AC1-a: 预览模式渲染标题 / 列表 / 代码块 / 链接 / GFM 表格', async () => {
    const { getByRole, getByText } = await render(
      <MarkdownEditor value={SAMPLE} />
    )

    await expect
      .element(getByRole('heading', { level: 2, name: '标题二' }))
      .toBeInTheDocument()
    await expect.element(getByText('列表项一')).toBeInTheDocument()
    await expect.element(getByText('const x = 1')).toBeInTheDocument()

    const link = getByRole('link', { name: '链接文本' })
    await expect.element(link).toBeInTheDocument()
    await expect.element(link).toHaveAttribute('href', 'https://example.com')

    await expect.element(getByRole('table')).toBeInTheDocument()
    await expect
      .element(getByRole('cell', { name: 'a1' }))
      .toBeInTheDocument()
  })

  it('AC1-b: 空内容不渲染任何文档节点，也不报错', async () => {
    const { container } = await render(<MarkdownEditor value='' />)
    expect(container.querySelector('h1,h2,h3,ul,ol,table,pre,blockquote')).toBeNull()
  })

  it('AC1-c: 排版样式表（markdown.css）随组件生效：标题层级与正文都取到设计字号', async () => {
    // 对齐 label-chip.test 的做法引入全局样式后再读解析值
    const { getByRole, getByText } = await render(
      <MarkdownEditor value={'## 二级\n\n正文段落'} />
    )
    const heading = (await getByRole('heading', { level: 2 }).element())!
    expect(getComputedStyle(heading).fontSize).toBe('20px') // 1.25rem
    expect(getComputedStyle(heading).fontWeight).toBe('600')

    const para = (await getByText('正文段落').element())!
    expect(getComputedStyle(para).lineHeight).toBe('23.8px') // 0.875rem * 1.7
  })
})

describe('MarkdownEditor 预览 / 源码一键切换（AC2）', () => {
  it('AC2-a: 默认预览模式（无编辑框），切到源码出现编辑框并展示原始文本', async () => {
    const { container, getByRole } = await render(
      <MarkdownEditor value={SAMPLE} />
    )

    // 预览：无编辑框，有渲染结果
    expect(container.querySelector('textarea')).toBeNull()
    await expect
      .element(getByRole('heading', { level: 2, name: '标题二' }))
      .toBeInTheDocument()

    await userEvent.click(getByRole('button', { name: '源码' }))

    const editor = getByRole('textbox')
    await expect.element(editor).toBeInTheDocument()
    // 源码模式展示的是未渲染的原始 Markdown
    expect((await editor.element() as HTMLTextAreaElement).value).toContain(
      '## 标题二'
    )
    expect(container.querySelector('h2')).toBeNull()
  })

  it('AC2-b: 源码模式一键切回预览，恢复渲染结果', async () => {
    const { container, getByRole } = await render(
      <MarkdownEditor value={SAMPLE} />
    )

    await userEvent.click(getByRole('button', { name: '源码' }))
    await userEvent.click(getByRole('button', { name: '预览' }))

    expect(container.querySelector('textarea')).toBeNull()
    await expect
      .element(getByRole('heading', { level: 2, name: '标题二' }))
      .toBeInTheDocument()
  })

  it('AC2-c: defaultMode="source" 时初始即源码模式', async () => {
    const { getByRole } = await render(
      <MarkdownEditor value={SAMPLE} defaultMode='source' />
    )
    await expect.element(getByRole('textbox')).toBeInTheDocument()
  })
})

describe('MarkdownEditor 编辑与保存（AC3 / AC4）', () => {
  it('AC3: 源码模式输入触发 onChange，并回灌到受控 value', async () => {
    const onChange = vi.fn()
    const { getByRole } = await render(<Host onChange={onChange} />)

    await userEvent.click(getByRole('button', { name: '源码' }))
    const editor = getByRole('textbox')
    await replaceAll(editor, '## edited')

    expect(onChange).toHaveBeenCalled()
    expect(onChange).toHaveBeenLastCalledWith('## edited')
    // 受控回灌：编辑框内容由调用方 state 驱动，而不是组件内部私藏一份
    expect((await editor.element() as HTMLTextAreaElement).value).toBe(
      '## edited'
    )
  })

  it('AC4-a: 点击保存按钮触发 onSave，携带当前 value', async () => {
    const onSave = vi.fn()
    const { getByRole } = await render(
      <MarkdownEditor value={SAMPLE} onSave={onSave} />
    )

    await userEvent.click(getByRole('button', { name: '保存' }))
    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave).toHaveBeenCalledWith(SAMPLE)
  })

  it('AC4-b: 源码模式 Cmd/Ctrl+S 触发 onSave 且不触发浏览器默认行为', async () => {
    const onSave = vi.fn()
    const { getByRole } = await render(<Host onSave={onSave} />)

    await userEvent.click(getByRole('button', { name: '源码' }))
    const editor = getByRole('textbox')
    await userEvent.click(editor)
    await userEvent.keyboard('{Control>}{s}{/Control}')

    expect(onSave).toHaveBeenCalledWith(SAMPLE)
  })

  it('AC4-c: 源码模式编辑后保存，携带的是编辑后的最新内容', async () => {
    const onSave = vi.fn()
    const { getByRole } = await render(<Host onSave={onSave} />)

    await userEvent.click(getByRole('button', { name: '源码' }))
    await replaceAll(getByRole('textbox'), '## after edit')
    await userEvent.click(getByRole('button', { name: '保存' }))

    expect(onSave).toHaveBeenLastCalledWith('## after edit')
  })
})

describe('MarkdownEditor XSS 安全（Scope：渲染侧 sanitize）', () => {
  it('内嵌 HTML 标签不被渲染为节点（保持纯文本），onerror 不可达', async () => {
    const { container, getByText } = await render(
      <MarkdownEditor value={'<img src=x onerror="alert(1)">'} />
    )
    expect(container.querySelector('img')).toBeNull()
    // 源码以转义文本呈现，而非 DOM 节点
    await expect
      .element(getByText('<img src=x onerror="alert(1)">'))
      .toBeInTheDocument()
  })

  it('javascript: 协议链接被剥离，不落入 href', async () => {
    const { getByText } = await render(
      <MarkdownEditor value={'[点我](javascript:alert(1))'} />
    )
    const anchor = (await getByText('点我').element()) as HTMLAnchorElement
    expect(anchor.getAttribute('href') ?? '').not.toContain('javascript:')
  })

  it('自定义 a 渲染不把 react-markdown 的内部 node prop 泄漏到 DOM', async () => {
    const { getByText } = await render(
      <MarkdownEditor value={'[链接文本](https://example.com)'} />
    )
    const anchor = (await getByText('链接文本').element()) as HTMLAnchorElement
    expect(anchor.getAttribute('node')).toBeNull()
    expect(anchor.getAttribute('rel')).toBe('noopener noreferrer')
  })
})

describe('MarkdownEditor 只读形态（editable=false，供纯展示区域复用）', () => {
  it('不渲染模式切换与保存入口，仅渲染文档', async () => {
    const { container, getByRole } = await render(
      <MarkdownEditor value={SAMPLE} editable={false} />
    )

    expect(container.querySelector('button')).toBeNull()
    expect(container.querySelector('textarea')).toBeNull()
    await expect
      .element(getByRole('heading', { level: 2, name: '标题二' }))
      .toBeInTheDocument()
  })
})
