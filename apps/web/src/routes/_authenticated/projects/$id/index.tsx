import { createFileRoute } from '@tanstack/react-router'
import { ProjectDetail } from '@/features/projects/project-detail'

// 项目详情总览页：/projects/{id}（「项目任务」入口指向 $id/tasks）。
export const Route = createFileRoute('/_authenticated/projects/$id/')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <ProjectDetail projectId={id} />
}
