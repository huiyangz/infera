# SDD 阶段扩展设计（设计层 + 任务层）

日期：2026-08-17
状态：用户确认（"每层文档都要人签发"；小需求合并门、大需求全门、拆分迁设计门、实现按任务逐条跑）

## 新阶段图（11 阶段）

```
intake → spec → spec_approval → [small] ────────────────────→ test_gen → code_gen → unit_test → code_review
                          └→ [large] → design → design_approval → tasks → tasks_approval → test_gen → …
                                           └─ 拆分在此（按设计边界）：
                                              父跳过 tasks/tasks_approval/test_gen，停 code_gen（合并，机制同现状）
                                              子需求各自完整流水线（复杂度各自判定）
```

- **spec_approval 决定复杂度**：approve body 可带 `{"complexity":"small"|"large"}`；
  spec agent 在规格末尾附 fenced block ```infera-complexity\nlarge\n``` 给建议，gate 响应带 `complexity_suggestion`；人可改判。
- **小需求**：设计+任务揉进规格文档（prompt 已含），spec_approval 一道门全审，直跳 test_gen。
- **大需求**：design agent 产设计文档（架构/模块边界/接口/取舍，可能拆分的附 ```infera-split``` 块，同现有约定）；
  design_approval 门显示设计文档 + 拆分编辑器（从 spec 门迁来）；不拆 → tasks；拆 → 现有拆分机制（父 split_mode，
  跳过 tasks/tasks_approval/test_gen 停 code_gen 合并）。
- **tasks agent**：吃规格(+设计)，输出 fenced block ```infera-tasks\n[{"title":"","detail":""}]```；
  引擎解析存 artifact kind="tasks"（JSON）。tasks_approval 门显示可编辑清单（approve body 可带
  `{"tasks":[...]}` 覆盖，同 split 编辑器模式）；批 = 放行实现（superpowers 的 "go"）。
- **实现按任务逐条跑**：code_gen 对有 tasks artifact 的交付循环执行——每个任务一次 agent 调用
  （prompt = code_gen 基底 + "当前任务 i/N：title/detail" + 失败反馈），成功存 artifact kind="task_done"
  (content=任务序号) + 事件 `task_done`；全部完成存 kind="summary" 后前进。unit_test 失败回环只重跑
  **剩余**任务（task_done 持久，进度不丢）。无 tasks artifact 的交付（老数据）单次整体实现，行为不变。
- **打回**：tasks_approval 打回 → 回 tasks；design_approval 打回 → 回 design（拆分与不拆同路径）；
  spec_approval 打回 → 回 spec（不变）。
- **子需求上下文注入**：子需求（parent_id != ""）跑 spec 时，prompt 注入父的 spec artifact +
  design artifact 作为背景约束段（"以下是父需求规格/设计，作为约束参考，不要重写"）。
- **兼容**：complexity ''（老数据）按 small 走；旧拆分父停在 code_gen 机制不变。

## 数据模型（migration 0004）

deliveries ADD `complexity TEXT NOT NULL DEFAULT ''`（''|small|large）。
任务进度不加列——由 artifacts（kind=task_done）推导，与 merge ledger 同哲学。

## API 变更

- approve body 扩展：`{complexity}`（spec_approval）/ `{split}`（design_approval，逻辑同现有但作用于新门）/
  `{tasks}`（tasks_approval 覆盖清单）
- gate 响应扩展：spec_approval + `complexity_suggestion`；design_approval + `design` 文档 + `split_plan`
  （从 design artifact 解析）；tasks_approval + `tasks` 清单（从 tasks artifact）
- 新事件：`task_done`、`tasks_overridden`、`complexity_set`
- EngineAPI：ApproveWithSplit 扩为 `Approve(ctx, id, opts ApproveOpts)`（复杂度/split/tasks 三选一按门分发）
  ——具体签名实现时定，原则：一个入口、按当前门校验对应选项

## 前端

- 阶段条**按交付模式派生显示**：small 显示 7 阶段（同现状）；large 全 11；拆分父 11 但
  tasks/tasks_approval/test_gen 显示跳过（虚线圈灰勾）。STAGE_META 增 4 阶段文案。
- 规格审批页：复杂度选择（segmented：小/大，AI 建议预选）
- 设计审批页：设计文档 + 拆分编辑器（迁移）
- 任务审批页：清单编辑器（增删改、拖序可后）+ 「批准，开始实现」
- 详情页：任务进度卡（任务 x/N + 每任务状态点）；EVENT_LABEL 增 task_done 等

## 测试

- 引擎：复杂度分岔、设计门拆分（含跳过三阶段）、任务逐条执行/失败反馈/回环只跑剩余、
  子上下文注入、老数据（complexity ''）走 small
- E2E：大需求全链路（fake agent：spec 带 complexity 块 → 大 → design 带 split 块 → 拆 → 子 small 全流程 →
  父合并完成）；小需求任务链路（tasks 块 → 门 → 逐任务实现 → 完成）

## 非目标

- 任务拖拽排序 UI、任务级人工逐项确认（门只批清单）、双道 agent 审查（下一轮）
