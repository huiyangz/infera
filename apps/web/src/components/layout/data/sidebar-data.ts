import {
  Compass,
  FolderKanban,
  SquareCheck,
  Workflow,
} from 'lucide-react'
import { type SidebarData } from '../types'

export const sidebarData: SidebarData = {
  user: {
    name: 'infera',
    email: '',
    avatar: '/avatars/shadcn.jpg',
  },
  teams: [
    {
      name: 'infera',
      logo: Workflow,
      plan: 'Agent 交付流水线',
    },
  ],
  navGroups: [
    {
      title: '平台',
      items: [
        {
          title: '项目',
          url: '/',
          icon: FolderKanban,
        },
        {
          title: '需要决策',
          url: '/decisions',
          icon: SquareCheck,
        },
        {
          title: '需求发现',
          url: '/discovery',
          icon: Compass,
        },
        // 「Agent 执行时序」不再有独立入口：可视化迁入项目详情页签（INFERA-259）
      ],
    },
  ],
}

