# infera P7（项目 + 登录 + onboarding）Design

> **状态**：已与用户对齐，待实现。对应 brainstorm 结论。
> **依赖**：P1-P6 已完成（Delivery 闭环：Agent 接力 + loop + gate + GitHub + 实时）。

## 背景

P1-P6 建好了 Delivery 核心闭环，但是**单租户、无登录、无项目实体**的最小骨架：打开即写需求，`repo_url` 是 Delivery 上的一个字段，每条交付重复填。真实产品流程应是「登录 → 建项目（绑仓库）→ 在项目下提需求」。

同时暴露两个既有缺口需要在本次一起修：
1. **deploy 阶段 + 合并等待**语义过重，先砍掉。
2. **workdir gap**：P4 在 `code_gen` 克隆了项目仓库，但没挂进 Agent / testRunner 容器 → Agent 在空 `/work` 跑 → 开的 PR 是空改动。拉了代码却没用上。

## 目标

1. 加 **Project 实体**（1 项目 = 1 仓库），Delivery 挂在项目下，仓库在项目层绑定一次。
2. **单用户登录**（密码门，单租户内部，不做多租户/计费/OAuth）。
3. 前端流程：登录 → 项目列表 → 项目详情（绑仓库 + 该项目交付）→ 交付详情。
4. 顺带修：① 砍 deploy；② 自动推进到 gate；③ workdir gap（真代码喂给 Agent/testRunner）。

## 非目标（继续延后）

多租户 / 计费 / onboarding 引导 / OAuth、真实部署（deploy 已砍）、per-stage 接管、Agent 自定义管理、Pipeline 模板自定义。真实 Claude/GitHub smoke 待用户提供凭证后单独验。

## 数据模型

- 新表 **`projects`**：`id`(uuid PK)、`name`(text)、`repo_url`(text)、`default_branch`(text default `'main'`)、`created_at`、`updated_at`。
- **`deliveries` 加 `project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE`**；**删掉 `repo_url`、`branch` 列**（仓库统一从项目继承）。
- 登录**不建表**：单用户，密码从 `INFERA_PASSWORD` env 读，session = HMAC 签名的 httpOnly cookie。

**迁移策略**：建 `projects` → 插一个 `Default` 项目（`repo_url=''`）→ 回填现有 deliveries 的 `project_id` → 加 `NOT NULL` → 删 `deliveries.repo_url/branch`。

## 登录

- `POST /api/login {password}`：对则种 httpOnly cookie（`SameSite=Lax`，HMAC-SHA256 签名，key=`INFERA_PASSWORD`），`200`；错 `401`。
- `POST /api/logout`（清 cookie）、`GET /api/me`（返回 `{logged_in}`，前端判断登录态）。
- **auth 中间件**挡 `/api/*`（`/api/login`、`/health` 除外）与 `/ws`：校验 cookie，失败 `401`。密码用 `crypto/subtle` 常量时间比较。

## API

- 项目：`GET /api/projects`（列表）、`POST /api/projects`（`name`/`repo_url`/`default_branch`，**建时试 clone 校验**：仓库可达 + token 有写权限，不过则 400）、`GET /api/projects/[id]`（含该项目 deliveries）、`PATCH /api/projects/[id]`、`DELETE`。
- 交付改为项目内：`POST /api/projects/[id]/deliveries`（只传 `title`/`description`，仓库继承）、`GET /api/projects/[id]/deliveries`。
- 既有 `GET /advance /gate /approve /reject` on `/api/deliveries/[id]` 保留；`/advance` 保留作手动兜底。Create 不再接 `repo_url`。

## 流水线改动

1. **砍 deploy**：`stage.order` 变 7 个 —— `intake → spec → spec_approval → test_gen → code_gen → unit_test → code_review`；`code_review` 为终点，`stage.Next(code_review)=!ok → completed`。删 deploy-wait（`IsLatestPRMerged` / `waiting_for_merge`）。
2. **自动推进到 gate**：
   - 新增 `DeliveryService.RunUntilGate(ctx, id)`：循环 `Advance` 直到 `pending_gate != nil` / `status != active` / completed。
   - 创建交付后、gate `Approve` 后，**异步 goroutine** 跑 `RunUntilGate`；HTTP 立即返回；进度通过 timeline 实时可见（P6 WebSocket）。
   - 防 duplicate：用 delivery 状态 + 轻量 in-progress 标记（如内存 map 或 DB 标志）避免重复启动。
3. **仓库从项目取**：`ExecuteService` 在 `code_gen` 开 PR 时，按 `delivery.project_id → project` 取 `repo_url` / `default_branch`，不再读 `delivery.repo_url`（已删）。
4. **修 workdir gap（拉的真代码喂进去）**：
   - `code_gen` 前：按项目仓库浅 clone 到 `/tmp/infera-repos/<deliveryID>/`（已存在则复用、先 `fetch+reset` 到最新默认分支）。
   - 把该目录挂进 **Coder 容器**（`ExecInput.Workdir`）和 **testRunner 容器**（`RealRunner.workdir`）的 `/work`。
   - Coder 改真文件 → `GitService` push 分支 `infera/<deliveryID>` → 开 PR（**真改动**）。
   - `unit_test` 在同一份代码上跑 `go test`。
   - 绿地空仓库：clone 出空目录，Coder 从零写初始代码，机制不变。

## 前端（深浅双模 + 信息密集）

- 顶栏：logo `infera` · 主题切换（日/夜/跟随系统）· 登出。
- `/login`：密码框 → 成功跳 `/`。
- `/`：项目列表（卡片：名字、仓库、交付数、最近活跃；「新建项目」）。
- `/projects/new`：建项目表单（名字、repo_url、默认分支；提交后端试 clone 校验，失败提示）。
- `/projects/[id]`：项目详情（仓库/分支信息 + 「新建交付」只需一句需求 + 该项目 Deliveries 列表）。
- `/deliveries/[id]`：沿用，加返回项目的面包屑；timeline 实时。
- `/deliveries/[id]/gate`：沿用。
- auth gating：未登录跳 `/login`。
- 主题：CSS 变量 + `.dark` class，默认跟随系统，顶栏可手动切。

## 对现有代码 / 测试的影响

- `internal/stage`：`order` 去 `deploy`，`gates` 不变；`stage_test.go` 更新。
- 所有 service/handler 测试里 `svc.Create(...)` 需先建 project（P1-P6 测试全改）；`dbtest` 加 project seed helper。
- `ExecuteService`：repo 改从 project 取；`code_gen` 挂 workdir；删 `IsLatestPRMerged`。
- `DeliveryService`：删 deploy 分支；加 `RunUntilGate`；Approve 后异步续跑。
- 前端 `lib/types.ts`：`Delivery` 去 `repo_url/branch`、加 `project_id`；新增 `Project`。
- `main`：装 auth 中间件、projects handler；auto-advance goroutine 启动点。
- `DockerBackend`：已透传 `BASE_URL`/proxy（上一轮已加）；本阶段不改动。

## Definition of Done

- 登录门工作；未登录 → 401 / 跳 `/login`。
- 项目 CRUD + 建·时·试·clone 校验。
- Delivery 挂项目；创建后自动推进到 gate；gate 批准后继续到下个 gate / completed。
- `code_gen` 产**真 PR（非空）**、`unit_test` 在真代码上跑 `go test`；Fake 下 `go test ./...` 全绿。
- 7 阶段、无 deploy、深浅双模 UI 上线。
- `go test ./...` 通过、`npm run build` 干净。

## 依赖 / 风险

- 真实闭环仍需 `ANTHROPIC` 凭证（或兼容端点）+ `GITHUB_TOKEN`；用户未提供前只验 Fake 单测 + 本地 git/clone 单测。
- 自动推进异步：长任务（多 Agent + loop）走 goroutine，靠 timeline 看进度；需防重复启动与 goroutine 泄漏。
- 砍 deploy 后 `completed` = code_review 批准（PR 在 code_gen 已开，合不合并由人在 GitHub 自行处理，infera 不跟踪）。
