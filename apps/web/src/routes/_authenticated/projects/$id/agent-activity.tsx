import { createFileRoute } from '@tanstack/react-router'
import { ProjectAgentActivity } from '@/features/projects/project-agent-activity'

// 项目详情「Agent 执行时序」tab（INFERA-259）：原独立路由 /agent-activity
// 的可视化主体迁入项目域，成为项目详情页内一级导航的第三个页签。
export const Route = createFileRoute(
  '/_authenticated/projects/$id/agent-activity'
)({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <ProjectAgentActivity projectId={id} />
}
