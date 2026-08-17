import { createFileRoute } from '@tanstack/react-router'
import { ProjectDetail } from '@/features/projects/project-detail'

export const Route = createFileRoute('/_authenticated/projects/$id')({
  validateSearch: (search: Record<string, unknown>) => ({
    d: typeof search.d === 'string' ? search.d : undefined,
  }),
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  const { d } = Route.useSearch()
  return <ProjectDetail projectId={id} selectedDeliveryId={d} />
}
