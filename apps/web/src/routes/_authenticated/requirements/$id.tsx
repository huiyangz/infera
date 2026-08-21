import { createFileRoute } from '@tanstack/react-router'
import { RequirementDetailPage } from '@/features/requirements/requirement-detail'

export const Route = createFileRoute('/_authenticated/requirements/$id')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <RequirementDetailPage requirementId={id} />
}
