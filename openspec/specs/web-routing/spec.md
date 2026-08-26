# web-routing（路由与页面结构）

## Purpose

管理 Web 前端的路由树与页面骨架：登录/鉴权路由分组、认证后页面布局、错误页（401/403/404/500/503）与页面导航结构。本域只约定「URL → 页面 → 布局」的结构、守卫与导航信息架构，以及 UI 设计系统约束（`apps/web/DESIGN.md`、`apps/web/AGENTS.md`）；各页面承载的具体业务行为（任务卡、审批、统计等）归各自能力域，登录/会话机制本身归 `auth` 域。

## Requirement: 文件驱动的路由树

路由树 SHALL 由 `apps/web/src/routes/` 的文件与目录结构生成（产物 `src/routeTree.gen.ts`），URL 层级与目录层级一致；括号目录（`(auth)`、`(errors)`）与无路径布局目录（`_authenticated`）SHALL NOT 产生 URL 段；`routeTree.gen.ts` 为生成物，人工改动 SHALL NOT 作为交付内容。

#### Scenario: 目录层级即 URL

- **WHEN** 访问 `/projects/<id>/tasks`
- **THEN** 命中 `routes/_authenticated/projects/$id/tasks.tsx` 对应的路由并渲染其页面

#### Scenario: 分组目录不出现在 URL

- **WHEN** 路由文件位于 `(auth)`、`(errors)` 或 `_authenticated` 目录下
- **THEN** URL 不含这些目录名（如 `/(auth)/sign-in.tsx` → `/sign-in`，`/_authenticated/stats.tsx` → `/stats`）

## Requirement: 公开区与认证区的划分

路由 SHALL 划分为公开区与认证区：公开区仅含登录页 `/sign-in` 与错误页 `/401`、`/403`、`/404`、`/500`、`/503`（挂根路由下）；认证区为 `_authenticated` 布局下的其余全部路由；业务页面 SHALL NOT 挂在公开区。

#### Scenario: 公开页无需会话直达

- **WHEN** 未登录用户直接访问 `/sign-in` 或任一错误页
- **THEN** 页面正常渲染，不触发登录跳转

#### Scenario: 认证区路由清单

- **WHEN** 枚举 `_authenticated` 下的路由
- **THEN** 覆盖：项目列表 `/`、项目详情 `/projects/$id`（含 `/tasks`、`/agent-activity` 子路由）、交付 `/deliveries/$id`（含 `/gate` 子路由）、需求 `/requirements` 与 `/requirements/$id`、`/decisions`、`/discovery`、`/stats`、`/errors/$error`

## Requirement: 认证区路由守卫

进入认证区任意路由前，系统 SHALL 探测登录态（`GET /api/me`）：未登录 SHALL 在路由加载前重定向到 `/sign-in`；探测请求本身失败（网络异常/5xx）SHALL NOT 被当作未登录处理，而 SHALL 交由路由错误边界如实展示。

#### Scenario: 未登录跳登录

- **WHEN** 未登录用户访问 `/projects/<id>`
- **THEN** 守卫在页面加载前重定向到 `/sign-in`

#### Scenario: 已登录直达

- **WHEN** 已登录用户访问同一路由
- **THEN** 直接渲染页面，不再经过登录跳转

#### Scenario: 探测故障不误判

- **WHEN** `/api/me` 因后端故障抛错（非 401）
- **THEN** 认证区路由以错误边界呈现故障，不得静默当作未登录跳转登录页

## Requirement: 认证区布局与全局导航

认证区 SHALL 使用统一布局（侧边栏 + 内容区，含跳到主内容的快捷链接）；全局导航 SHALL 提供且仅提供四个顶层入口：项目 `/`、需要决策 `/decisions`、需求发现 `/discovery`、统计 `/stats`（命令面板与侧边栏同源数据）；导航 SHALL NOT 含 `/agents` 或 `/agent-activity` 顶层入口——Agent 执行时序视图作为项目详情页签到达。

#### Scenario: 顶层入口清单

- **WHEN** 渲染全局导航或命令面板
- **THEN** 顶层入口为上述四项，不含任何指向 `/agents`、`/agent-activity` 的项

#### Scenario: 时序视图走项目页签

- **WHEN** 用户需要查看某项目的 Agent 执行时序
- **THEN** 经 `/projects/<id>/agent-activity` 页签到达，而非独立顶层页面

#### Scenario: 侧边栏状态持久化

- **WHEN** 折叠或展开侧边栏后刷新页面
- **THEN** 开合状态保持（经 `sidebar_state` cookie 记忆）

## Requirement: 路由与页面的挂载关系

每条叶子路由 SHALL 挂载 `apps/web/src/features/` 下对应能力域的页面组件；本项目详情与交付详情为布局路由（仅提供子路由挂载点，页面在子路由）；路径中 `$` 前缀段 SHALL 解析为路由参数。本域只约定「URL → 页面」的结构映射，页面内业务行为归各自域的 spec。

#### Scenario: 首页挂载项目列表

- **WHEN** 访问 `/`
- **THEN** 渲染项目列表页（`features/projects`）

#### Scenario: 详情布局路由

- **WHEN** 访问 `/projects/<id>` 或 `/deliveries/<id>`
- **THEN** 布局路由仅作挂载点，页面由 `index`（详情）、`tasks`、`agent-activity`、`gate` 等子路由渲染

#### Scenario: 动态参数

- **WHEN** 路由路径含 `$id`、`$error` 段
- **THEN** 该段解析为路由参数供页面使用（如 `/deliveries/<deliveryID>` 取得交付 ID）

## Requirement: 错误页体系

根路由 SHALL 定义全局 404（notFound 组件）与通用错误（error 组件）渲染；系统 SHALL 提供公开可达的 `/401`、`/403`、`/404`、`/500`、`/503` 独立错误页，以及认证区内按 slug 映射的 `/errors/$error` 错误展示页（`unauthorized`、`forbidden`、`not-found`、`internal-server-error`、`maintenance-error`）；未知 slug SHALL 回落到 404 组件。

#### Scenario: 未匹配路由

- **WHEN** 访问的路径不匹配路由树任何路由
- **THEN** 渲染 404 页，页面提供返回与回首页操作

#### Scenario: 错误页直达

- **WHEN** 直接访问 `/503`
- **THEN** 渲染维护页，无需登录

#### Scenario: 未知 slug 回落

- **WHEN** 访问 `/errors/<未知 slug>`
- **THEN** 渲染 404 组件

## Requirement: UI 设计系统约束

全部页面（公开区与认证区）SHALL 遵循 `apps/web/DESIGN.md` 定义的设计语言与 `apps/web/AGENTS.md` 的前端工程约定（Vite + React 19 + TanStack Router + shadcn/ui，pnpm 管理），并支持深浅双模主题（CSS 变量 + `.dark`）。

#### Scenario: 主题切换持久化

- **WHEN** 用户切换日间/夜间/跟随系统主题
- **THEN** 主题全局生效并持久化（cookie），刷新后保持

#### Scenario: 双模可读

- **WHEN** 系统处于深色或浅色模式
- **THEN** 页面以对应变量渲染，登录页与认证区页面在两种模式下均可读
