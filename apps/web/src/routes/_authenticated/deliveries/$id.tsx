import { createFileRoute, Outlet } from '@tanstack/react-router'

// 布局路由：仅作为 /gate 等子路由的父级（深链重定向在 index，避免劫持子路由）。
export const Route = createFileRoute('/_authenticated/deliveries/$id')({
  component: Outlet,
})
