# 任务同步使用说明

把外部任务源工作区的项目和 issue 同步进 infera：项目落 infera 项目列表，issue 落
项目下的交付列表（带外部来源标记）。同步是**全量镜像**，幂等可重复。

## 同步机制（自动 + 手动）

- **启动即同步**：server 启动时调度器立即异步执行一轮同步（不阻塞启动）。
- **周期轮询**：按 `TASK_SYNC_INTERVAL` 周期执行（默认 60s；设 `0` 关闭周期轮询，
  启动同步仍执行）。同步失败不 fatal：错误记入状态接口，下一轮继续。
- **手动触发**：登录后调用 `POST /api/task-sync`（前端有同步按钮，同一入口）。

```bash
# 手动触发一轮全量同步（同步执行，完成即回结果）
curl -b cookies -X POST http://localhost:8080/api/task-sync
# → {"started_at":…,"finished_at":…,"projects_imported":1,"issues_imported":21,
#    "issues_skipped":77,"skips":[…],"error":""}
# 进行中再触发 → 409；上游拉取/落库失败 → 502。

# 查看最近一轮（running + last；结果存进程内存，重启即空）
curl -b cookies http://localhost:8080/api/task-sync

# 自动同步状态（字段语义冻结，前端按此对接）
curl -b cookies http://localhost:8080/api/task-sync/status
# → {"lastSyncAt": time|null, "status": "idle|running|success|error", "error": string}
# idle = 从未完成过任何一轮；lastSyncAt 始终描述最近完成的一轮。
```

## 数据从哪来（配置）

凭据只走环境变量（三键齐才装配，缺任何一键同步路由返回 503）：

```bash
# .env
TASK_SYNC_SERVER_URL=<任务源服务地址，如 http://localhost:8088>
TASK_SYNC_TOKEN=<访问 token>
TASK_SYNC_WORKSPACE_ID=<工作区 id>
```

注意：设置了任一 `TASK_SYNC_*` 键后，需求流转装配也会启动，还需 `GITHUB_TOKEN`、
`TASK_SYNC_PROJECT_ID`、`TASK_SYNC_TECH_LEAD_AGENT_ID`、`TASK_SYNC_WORKSPACE_SLUG`，
否则进程启动即 fatal（详见 [requirements-flow.md](requirements-flow.md)）。

同步范围：任务源工作区**全部**项目与 issue。跳过规则（计入 `skips`，不落库）：

| reason | 含义 |
|---|---|
| `smoke` | 标题含 `[infera-e2e]` 的自动化冒烟单 |
| `no_project` | issue 未挂项目，无处可落 |
| `parent_cycle` | 父子关系成环，排不出导入顺序 |

状态翻译（任务源 → infera）：`done`/`cancelled` → `completed`；`blocked` →
`blocked`；**其余（todo/backlog/in_progress/in_review/未知）一律 `queued`**——
同步镜像永不产生 `active`，镜像只排队、不被引擎点火（重启恢复不会替镜像跑管线）。

## 同步进来的需求怎么绑定管线并驱动

同步进来的交付初始为 `queued`，引擎不会自动驱动。把它推入管线的已验证路径
（INFERA-82，dry-run 用 `AGENT_CMD=echo`）：

1. **绑定管线**：项目编排页（或 `PUT /api/projects/{id}/pipeline`）把节点绑到
   agent。全局默认在首次启动时自动种子（`default-cli` + `local-console` 全节点）；
   项目级覆盖优先于默认。绑定不要求项目已绑仓库。
2. **入队**：当前版本没有"启动排队交付"的 API——镜像入队需直接置库后借重启
   恢复点火：

   ```sql
   UPDATE deliveries SET status='active', current_stage='intake'
    WHERE external_issue_key='<INFERA-XX>';
   ```

   重启服务，`ResumeActive` 会点火全部 active 交付：intake（绿地建 workdir，
   无仓库项目只建目录）→ spec → 停在 `spec_approval` 人工门。此后照常走门禁
   审批流（见 README 的阶段图）。
3. **真跑前**把 `AGENT_CMD` 从 `echo` 换回真 agent 命令（如 `claude`）。

## 边界与坑

- **同步结果不持久化**：最近一轮结果只存进程内存，重启即空（`GET` 回
  `last:null`）；状态接口同样只描述本进程周期。
- **重复同步会覆写 infera 侧状态**：非终态 issue 再同步会把交付打回 `queued`
  （实测：active 停在门禁的交付再同步后 status 变 queued；引擎字段 stage/gate
  保留）。在任务源侧先把单转终态，或在驱动期间避免重复同步。
- 同步项目随仓库绑定落 `repo_url`（INFERA-175）：任务源项目资源里
  `github_repo` → 其 URL；`local_directory` → 其 `local_path`（按普通 clone
  语义处理，不引入 worktree/daemon 特殊模式）。择一：两类并存 `github_repo`
  优先，同类型取 `position` 最小。覆写：任务源侧解析出绑定 → 覆写
  `repo_url`；无资源 → 保留 infera 侧现值，不清空。`PATCH /api/projects/{id}`
  仍仅支持改 `pinned`——手工改绑仓库需另行建卡。
- 同步按外部 issue id 幂等 upsert，重复执行不产生重复行；父子关系随同步落库，
  子任务按上游阶段原值落 `wave`（1..N；无阶段子任务与顶层 = 0，引擎批次调度
  跳过 `wave<=0`，显示层归「无阶段」分组）。
