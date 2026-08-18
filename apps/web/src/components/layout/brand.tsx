import { Link } from '@tanstack/react-router'
import { Workflow } from 'lucide-react'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'

/**
 * 侧边栏品牌区（DESIGN.md 编辑风）：黑色方块图标 + 小写字标。
 * 用 sidebar 原语构建，折叠态自动收成图标轨。
 */
export function Brand() {
  const { setOpenMobile } = useSidebar()
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton size='lg' asChild>
          <Link to='/' onClick={() => setOpenMobile(false)}>
            <span className='flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground'>
              <Workflow className='size-4' strokeWidth={2} />
            </span>
            <span className='grid flex-1 text-start leading-tight'>
              <span className='text-base font-semibold tracking-[-0.4px] lowercase'>
                infera
              </span>
              <span className='truncate text-[11px] text-muted-foreground'>
                Agent 交付流水线
              </span>
            </span>
          </Link>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
