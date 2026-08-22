# Multica 同步使用说明

把 Multica 工作区的项目和 issue 同步进 infera：项目落 infera 项目列表，issue 落
项目下的交付列表（带外部来源标记）。同步是**手动触发的全量镜像**，幂等可重复。

## 数据从哪来（配置）

凭据只走环境变量（三键齐才装配，缺任何一键同步路由返回 503）：

```bash
# .env
MULTICA_SERVER_URL=<Multica 服务地址>
MULTICA_TOKEN=<访问 token>
MULTICA_WORKSPACE_ID=<工作区 id>
```

注意：设置了任一 `MULTICA_*` 键后，需求流转装配也会启动，还需 `GITHUB_TOKEN`、
`MULTICA_PROJECT_ID`、`MULTICA_TECH_LEAD_AGENT_ID`、`MULTICA_WORKSPACE_SLUG`，
否则进程启动即 fatal。

## 怎么触发

登录后调用（前端有同步按钮，同一入口）：

```bash
# 触发一轮全量同步（同步执行，完成即回结果）
curl -b cookies -X POST http://localhost:8080/api/multica/sync
# → {"projects_imported":1,"issues_imported":21,"issues_skipped":77,"skips":[...]}

# 查看最近一轮（running + last）
curl -b cookies http://localhost:8080/api/multica/sync
```

同步范围：工作区**全部**项目与 issue。跳过规则（计入 `skips`，不落库）：

| reason | 含义 |
|---|---|
| `smoke` | 标题含 `[infera-e2e]` 的自动化冒烟单 |
| `no_project` | issue 未挂项目，无处可落 |
| `parent_cycle` | 父子关系成环，排不出导入顺序 |

状态翻译（multica → infera）：`done`/`cancelled` → `completed`；`blocked` →
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
    WHERE multica_issue_key='<INFERA-XX>';
   ```

   重启服务，`ResumeActive` 会点火全部 active 交付：intake（绿地建 workdir，
   无仓库项目只建目录）→ spec → 停在 `spec_approval` 人工门。此后照常走门禁
   审批流（见 README 的阶段图）。
3. **真跑前**把 `AGENT_CMD` 从 `echo` 换回真 agent 命令（如 `claude`）。

## 边界与坑

- **同步结果不持久化**：最近一轮结果只存进程内存，重启即空（`GET` 回 `last:null`）。
- **重复同步会覆写 infera 侧状态**：非终态 issue 再同步会把交付打回 `queued`
  （实测：active 停在门禁的交付再同步后 status 变 queued；引擎字段 stage/gate
  保留）。在 multica 侧先把单转终态，或在驱动期间避免重复同步。
- 同步项目随仓库绑定落 `repo_url`（INFERA-175）：multica 项目资源里
  `github_repo` → 其 URL；`local_directory` → 其 `local_path`（按普通 clone
  语义处理，不引入 worktree/daemon 特殊模式）。择一：两类并存 `github_repo`
  优先，同类型取 `position` 最小。覆写：multica 侧解析出绑定 → 覆写
  `repo_url`；无资源 → 保留 infera 侧现值，不清空。`PATCH /api/projects/{id}`
  仍仅支持改 `pinned`——手工改绑仓库需另行建卡。
- 同步按 multica issue id 幂等 upsert，重复执行不产生重复行；父子关系随同步落库
  （子需求 `wave=1`）。
