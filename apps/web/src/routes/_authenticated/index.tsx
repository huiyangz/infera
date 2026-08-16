import { createFileRoute } from '@tanstack/react-router'
import { ProjectsList } from '@/features/projects/projects-list'

export const Route = createFileRoute('/_authenticated/')({
  component: ProjectsList,
})
