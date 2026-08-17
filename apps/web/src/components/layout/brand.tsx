import { Link } from '@tanstack/react-router'
import { Workflow } from 'lucide-react'
import { useSidebar } from '@/components/ui/sidebar'
import { cn } from '@/lib/utils'

/**
 * 侧边栏品牌区（DESIGN.md 编辑风）：黑色方块图标 + 小写字标。
 * 替换模板的 TeamSwitcher——单租户没有"切换团队"，这里就是产品标识。
 */
export function Brand({ className }: { className?: string }) {
  const { setOpenMobile } = useSidebar()
  return (
    <Link
      to='/'
      onClick={() => setOpenMobile(false)}
      className={cn(
        'flex items-center gap-2.5 px-2 py-1.5',
        className,
      )}
    >
      <span className='flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground'>
        <Workflow className='size-4.5' strokeWidth={2} />
      </span>
      <span className='grid leading-tight'>
        <span className='text-lg font-semibold tracking-[-0.4px] lowercase'>
          infera
        </span>
        <span className='truncate text-[11px] text-muted-foreground'>
          Agent 交付流水线
        </span>
      </span>
    </Link>
  )
}
