import { FolderKanban, SquareCheck, Workflow } from 'lucide-react'
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
      ],
    },
  ],
}

