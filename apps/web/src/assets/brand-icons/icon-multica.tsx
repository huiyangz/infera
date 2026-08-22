import { type SVGProps } from 'react'
import { cn } from '@/lib/utils'

/** Multica 来源标识：双圈节点 + M（与产品「多智能体节点编排」定位对齐的简笔标记） */
export function IconMultica({ className, ...props }: SVGProps<SVGSVGElement>) {
  return (
    <svg
      role='img'
      viewBox='0 0 24 24'
      xmlns='http://www.w3.org/2000/svg'
      width='24'
      height='24'
      className={cn('[&>path]:stroke-current', className)}
      fill='none'
      stroke='currentColor'
      strokeWidth='2'
      strokeLinecap='round'
      strokeLinejoin='round'
      {...props}
    >
      <title>Multica</title>
      <path d='M6 16.5v-9l6 6 6-6v9' />
      <circle cx='6' cy='16.5' r='1.75' strokeWidth='1.5' />
      <circle cx='18' cy='16.5' r='1.75' strokeWidth='1.5' />
    </svg>
  )
}
