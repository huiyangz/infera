import { createFileRoute } from '@tanstack/react-router'
import { ProjectTasks } from '@/features/projects/project-tasks'

// 项目任务列表页（L202608221241-2-T04）：从项目详情进入，父子结构只读。
export const Route = createFileRoute('/_authenticated/projects/$id/tasks')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <ProjectTasks projectId={id} />
}
