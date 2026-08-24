# 标签导入运行手册

把任务源（Multica）工作台的标签镜像进 infera：先落 workspace 级标签库（名称 +
颜色一致），再逐交付对齐挂标。**不需要单独跑导入脚本**——标签镜像内建在任务同步
链路里，触发一轮同步即完成导入。同步机制与配置见 [task-sync.md](task-sync.md)；
本文只讲标签这一维：映射规则、怎么触发、为什么可重复执行、怎么在你自己的实例上
验证。

## 映射规则

**交付 ↔ 任务源 issue：按 `external_issue_id` 对应，不做标题匹配。**

| 键 | 值 | 用途 |
|---|---|---|
| `external_issue_id` | 任务源 issue 的 uuid | 幂等 upsert 的定位键（唯一锚点） |
| `external_issue_key` | 人读编号，如 `INFERA-221` | 展示用（列表/详情页可见），不参与匹配 |

标题改了、编号变了都不影响对应关系；两端以 issue uuid 一一对应。

**标签库：名称 + 颜色照抄任务源，幂等键是 `external_label_id`。**

- 每轮同步先拉任务源侧的 workspace 标签库（任务源的 `GET /api/labels`），按上游
  标签 id 落 infera 标签库，`name` 与 `color` 与上游一致。
- `color` 存**hex 原值**（如 `auto` → `#22c55e`），不做任何色彩换算——前端 chip
  直接拿它做底色，按亮度自动配墨字/白字，视觉与任务源一致。
- 上游改了名称或颜色，下一轮同步覆写 infera 侧同一条（同 id 命中同一行）。
- issue 引用了标签库列表里没有的标签时，按 issue 内嵌的标签对象兜底落库——
  仍是同一个幂等键，不产生平行条目。

**挂标范围：只有真正导入的 issue 才对齐标签。** 被 skips 规则跳过的单
（`smoke` / `no_project` / `parent_cycle`，见 task-sync.md）不落库、不挂标签。

**摘标语义（重要边界）：同步只摘「镜像域」标签。**

- 镜像域 = `external_label_id` 非空的标签（同步来源）。任务源侧摘掉某标签，下一轮
  同步会摘掉 infera 侧对应交付上的同一标签。
- **infera 侧人工挂的本地标签（外部 id 为空）不在同步管辖内，绝不摘除。**
- 上游整单删除时，既有交付与其历史挂标维持上一轮状态——沿用既有镜像语义，
  同步不删交付，自然也不清它的标签。

## 导入触发

三个入口，同一个链路，效果等价：

1. **手动触发（推荐，立即见效）**：登录后调 `POST /api/task-sync`（前端侧栏的
   「刷新数据」按钮走的就是它）。

   ```bash
   # 登录拿 session cookie
   curl -c cookies -X POST http://localhost:8080/api/login \
     -H 'Content-Type: application/json' -d '{"password":"<INFERA_PASSWORD>"}'

   # 触发一轮全量同步（同步执行，完成即回结果）
   curl -b cookies -X POST http://localhost:8080/api/task-sync
   # → {"started_at":…,"finished_at":…,"projects_imported":1,"issues_imported":21,
   #    "issues_skipped":77,"labels_imported":3,"skips":[…],"error":""}
   ```

   `labels_imported` 是本轮镜像进标签库的标签数。进行中再触发 → 409；上游拉取/
   落库失败 → 502；未配 `TASK_SYNC_*` 三键 → 503。

2. **启动即同步**：server 启动时调度器立即异步执行一轮（不阻塞启动）。
3. **周期轮询**：按 `TASK_SYNC_INTERVAL` 周期执行（默认 60s）。

手动触发后想回看，`GET /api/task-sync` 返回最近一轮结果（含 `labels_imported`）；
注意结果只存进程内存，重启即空。

## 幂等保证

重复执行不产生重复标签，两层幂等键各自兜住：

| 层 | 幂等键 | 机制 |
|---|---|---|
| 标签库 | `labels.external_label_id`（部分唯一索引，空串不参与唯一性） | `ON CONFLICT` 命中即只更新 name/color，不开新行 |
| 交付挂标 | `delivery_labels` 复合主键 `(delivery_id, label_id)` | 重复挂 `ON CONFLICT DO NOTHING`，不产生重复关联 |

- 同一份任务源连跑 N 轮，标签库行数不变，`labels_imported` 每轮同值，每枚标签
  名称 + 颜色始终以任务源为准。
- 挂标对齐是**差集**不是累加：每轮算出 desired（上游当前挂的）与 current（infera
  侧镜像域当前挂的），缺的挂上、多的摘掉——两轮之间不积累。
- 因此「导入一次」和「开着周期同步」对标签来说是同一个终态；周期开着时上游的
  标签增删会在下一轮自动跟上。

## 验证清单

在自己的实例上照做（`<id>` 为 infera 内部 uuid，从 task-groups 响应里取）：

1. **触发一轮同步**（见上节），确认 `labels_imported` ≥ 1 且 `error` 为空。

2. **标签库一致**：

   ```bash
   curl -b cookies http://localhost:8080/api/labels
   # → [{"id":"…","name":"auto","color":"#22c55e","external_label_id":"…",…}, …]
   ```

   与任务源侧 `multica label list --output json` 逐条对照：名称、颜色 hex 原值一致，
   条数一致。`external_label_id` 非空即同步来源标签。

3. **抽查 2–3 个交付**：从任务源挑几个已知挂标的单（记住它们的 INFERA 编号），

   ```bash
   curl -b cookies http://localhost:8080/api/projects | jq '.[].id'
   curl -b cookies http://localhost:8080/api/projects/<id>/task-groups | jq '
     [.[] | {key: .external_issue_key, labels: [.labels[].name]}]
     + [.[] | .stages[].tasks[] | {key: .external_issue_key, labels: [.labels[].name]}]'
   ```

   核对每个编号的标签名清单与任务源一致；再看颜色，前端两处可直接目视——
   **项目任务页**（顶层任务卡片与子任务行都带 chip）、**任务详情页**（标题下方
   与子需求列表行）。

4. **幂等**：再 `POST /api/task-sync` 一轮，重复第 2 步——条数不变、无重名行，
   `labels_imported` 与上轮同值。

5. **摘标对齐**（可选）：
   - 在任务源侧摘掉某交付的一个标签，触发一轮同步 → infera 侧该交付上同名标签
     同步消失，标签库条目仍在（库镜像与挂标是两回事）。
   - 反向也能验镜像域管辖：在 infera 侧给某交付人工挂一枚它上游没有的**同步来源**
     标签（`GET /api/labels` 拿 `id`，`POST /api/deliveries/<id>/labels`，body
     `{"label_id":"<uuid>"}`），触发一轮同步 → 该标签被摘掉（上游没挂，镜像域内
     就该摘）。

「infera 侧人工挂的**本地**标签（外部 id 为空）不受同步影响」这一语义由存储层保证，
但当前**没有创建本地标签的 REST 端点**（`POST /api/labels` 未暴露）——想实测只能
直接往库里插一行 `external_label_id` 为空的标签再挂到交付上。
