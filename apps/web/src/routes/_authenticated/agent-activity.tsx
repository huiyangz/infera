import { createFileRoute } from '@tanstack/react-router'
import { AgentActivityPage } from '@/features/agent-activity/agent-activity-page'

export const Route = createFileRoute('/_authenticated/agent-activity')({
  component: AgentActivityPage,
})
