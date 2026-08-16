import { createFileRoute } from '@tanstack/react-router'
import { ProjectDetail } from '@/features/projects/project-detail'

export const Route = createFileRoute('/_authenticated/projects/$id')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <ProjectDetail projectId={id} />
}
