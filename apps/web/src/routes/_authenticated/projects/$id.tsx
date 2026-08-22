import { createFileRoute, Outlet } from '@tanstack/react-router'

// 布局路由：仅作为 /tasks 等子路由的父级，页面组件在 $id/index.tsx。
// （此前直接渲染 ProjectDetail 导致子路由无 Outlet 可挂、任务页永不出现。）
export const Route = createFileRoute('/_authenticated/projects/$id')({
  component: Outlet,
})
