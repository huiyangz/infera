import type { CSSProperties } from 'react'
import type { DeliveryLabel } from '@/lib/infera-types'
import { cn } from '@/lib/utils'

/**
 * 标签 chip（INFERA-220）：Multica 标签的 hex 原值直接做底色（不做色彩换算，
 * 保证与 Multica 视觉一致），文字颜色按底色的 WCAG 相对亮度取墨/白，
 * 保证浅色底也能读。色值是运行时数据，Tailwind 表达不了，走 inline style。
 *
 * 交付列表（项目任务页父子行）与任务详情两处共用，不各写一份。
 */

/** DESIGN.md 的 ink / 纸白：chip 上的两级文字色 */
const INK = '#030303'
const PAPER = '#ffffff'

/** 亮度阈值：底色亮于它配墨字，暗于它配白字 */
const LUMA_THRESHOLD = 0.5

/** 解析 #rgb / #rrggbb；非法（空串、非 hex、位数不对）返回 null */
function parseHex(color: string): [number, number, number] | null {
  const hex = color.trim().replace(/^#/, '')
  const full =
    hex.length === 3
      ? hex
          .split('')
          .map((c) => c + c)
          .join('')
      : hex
  if (!/^[0-9a-fA-F]{6}$/.test(full)) return null
  return [
    Number.parseInt(full.slice(0, 2), 16),
    Number.parseInt(full.slice(2, 4), 16),
    Number.parseInt(full.slice(4, 6), 16),
  ]
}

/** WCAG 相对亮度（sRGB 分量线性化后加权） */
function relativeLuminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => {
    const s = c / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

/** 有效 hex → 原值底色 + 对比文字色；无效 → null（调用方降级中性样式） */
function chipStyle(color: string): CSSProperties | null {
  const rgb = parseHex(color)
  if (!rgb) return null
  return {
    backgroundColor: color,
    color: relativeLuminance(rgb) > LUMA_THRESHOLD ? INK : PAPER,
  }
}

export function LabelChip({
  label,
  className,
}: {
  label: DeliveryLabel
  className?: string
}) {
  const style = chipStyle(label.color)
  return (
    <span
      data-slot='label-chip'
      title={label.name}
      style={style ?? undefined}
      className={cn(
        'inline-flex max-w-40 shrink-0 items-center truncate rounded-full px-2 py-0.5 text-[11px] font-medium leading-4',
        // 颜色非法时的降级：中性 hairline 描边 chip，不吞标签名也不上错色
        style ? undefined : 'border text-foreground',
        className
      )}
    >
      {label.name}
    </span>
  )
}

/**
 * 标签行：多枚 chip 横排，默认放不下换行；nowrap=true 保持单行（单行式
 * 子任务行用，超长由 chip 自身截断 + 容器裁切兜底）。空/无标签渲染
 * null——不占位、不留空壳 UI。
 */
export function LabelChipRow({
  labels,
  className,
  nowrap,
}: {
  labels?: DeliveryLabel[]
  className?: string
  nowrap?: boolean
}) {
  const rows = (labels ?? []).filter((l) => l?.name)
  if (rows.length === 0) return null
  return (
    <span
      data-slot='label-chip-row'
      className={cn(
        'flex flex-wrap items-center gap-1',
        nowrap && 'min-w-0 flex-nowrap overflow-hidden',
        className
      )}
    >
      {/* key 分隔符用 \u0000（转义写法）：任何可见分隔符都可能撞上标签名里的
          同字符，NUL 是安全分隔；但源码必须写转义形式——写裸控制字节会把
          整个文件判成 git 二进制，diff/grep/评审全部失效 */}
      {rows.map((l) => (
        <LabelChip key={`${l.name}\u0000${l.color}`} label={l} />
      ))}
    </span>
  )
}
