# example-sdd-spec-maintenance: worked example —— 在真实 spec 上提 MODIFIED 并并回

> 这**不是真实提案**，是教学示范（假想小功能「任务卡详情新增优先级展示」，挂假想
> 任务号 INFERA-900）：以第 2 层已提交的真实 `openspec/specs/task-management/spec.md`
> 为底，完整走一遍 **提 delta → 评审 → 并回 specs → 归档**，供后续功能任务照抄。
> 流程规则见 [../../EVOLUTION.md](../../EVOLUTION.md)，格式细则见
> [../../conventions.md](../../conventions.md) §2/§3，接入建议见
> [../../sdd-integration.md](../../sdd-integration.md)。
> 本目录是示范件，**不参与并回、永不归档**；真实提案用真实 change-id
> （如 `infera-300-add-xxx`）另起目录。

## 第 0 步：动笔前先判定（不写一个字就对了的提案不存在）

- **动哪个域**：改动落在 `apps/web/src/features/deliveries`（任务卡详情页）→ 查
  [../../README.md](../../README.md) 域↔模块映射表 → 归 `task-management` 域。
- **动哪种区块**：按 sdd-integration.md §4 三问——specs 里已有名称相同的
  Requirement「任务卡详情与阶段呈现」？是。它的行为变了吗？是（详情页新增优先级
  徽标，外部可观察行为）。是收紧/扩展还是全新能力？是在既有 Requirement 上扩展
  → **`MODIFIED`**（给替换后全文），不是 `ADDED`，更不是另写一条新 Requirement。
- **change-id**：`infera-900-add-delivery-priority-badge`（任务号前缀 + 动词短语，
  conventions.md §2），目录 `openspec/changes/infera-900-add-delivery-priority-badge/`。
- **任务卡对应动作**：Tech Lead 在任务卡「Spec 影响」段写——受影响能力域
  `task-management`；delta 路径 `openspec/changes/infera-900-add-delivery-priority-badge/change.md`；
  并回责任人 Tech Lead。

---

以下为提案正文（真实提案目录里 `change.md` 的全部内容，从这里照抄）：

# infera-900-add-delivery-priority-badge: 任务卡详情新增优先级展示

## Why

任务卡详情页头部目前只有标题、状态与阶段条；任务分组视图的行内已有优先级
（「项目任务分组视图」Requirement），详情页却看不到，用户点进详情反而丢失了
这个信息（关联任务 INFERA-900）。

## What Changes

- task-management: 任务卡详情头部新增优先级徽标，取自任务行已有的优先级字段；
  词表外的值回退显示原文，页面不崩。

## MODIFIED Requirements

### Requirement: 任务卡详情与阶段呈现

任务卡详情 SHALL 返回任务行 + 时间线事件 + 阶段产物（拆分父另附子任务清单）。
任务行头部 SHALL 展示优先级徽标：优先级为 `none` 或空时 SHALL NOT 渲染徽标，其余值按「项目内新建任务卡」透传的上游词表渲染对应样式；词表外的优先级值 SHALL 回退显示原文，页面不崩。
阶段条 SHALL 按复杂度派生展示：`small` 及老数据（complexity 空）走 7 阶段，`large` 走全 11 阶段（拆分父的 tasks/tasks_approval/test_gen 显示跳过态）；终态渲染规则：`completed` 全部 done、`blocked` 当前阶段 failed（之前 done、之后 pending、无进行中指示）、`cancelled` 停在放弃点不再推进。
`large` 模式的逐任务实现进度 SHALL 由 tasks/task_done 产物推导（无清单产物时回退 task_done 事件拼装，进度持久不丢）；未知阶段/事件词表外的值 SHALL 回退原文显示不崩；镜像任务卡无 current_stage 时 SHALL 以占位符或 issue key 顶替阶段位，不留悬空「阶段」标签。（`GET /api/deliveries/{id}`；web `features/deliveries/delivery-detail.tsx`、`lib/infera-types.ts`）

#### Scenario: large 拆分父的阶段条与子任务清单

- **WHEN** 读取一张 `complexity=large` 的拆分父任务详情
- **THEN** 阶段条展示全 11 阶段且 tasks/tasks_approval/test_gen 为跳过态，`code_gen` 位显示等待子任务进度（已完成/总数），另附子任务清单（各带批次徽标、阶段、状态、标签）

#### Scenario: blocked 卡当前阶段显示失败

- **WHEN** 任务状态为 `blocked`
- **THEN** 阶段条上当前阶段为 failed、之前阶段 done、之后 pending，无进行中指示

#### Scenario: cancelled 卡停在放弃点

- **WHEN** 任务状态为 `cancelled`
- **THEN** 阶段条停在放弃点：之前阶段 done、当前及之后 pending，不再呈现任何推进或等待

#### Scenario: 优先级展示与词表外回退

- **WHEN** 分别读取优先级为词表内高优先级、`none`、词表外自定义值（如 `super-urgent`）的任务卡详情
- **THEN** 三种情况分别渲染对应样式徽标、不渲染徽标、回退显示原文值，页面均不崩

---

提案正文到此为止。以下演示**交付之后的路径**（执行者不用做，Tech Lead 的动作）。

## 评审环节看什么（随功能一起评审，30 秒清单）

- **存在**：任务卡「Spec 影响」列了 `task-management` → change.md 里就有且只有
  `task-management` 的区块，无缺漏；
- **名称逐字一致**：`### Requirement: 任务卡详情与阶段呈现` 与
  `specs/task-management/spec.md` 现有标题逐字相同（抄错即并回失败）；
- **MODIFIED 是全文**：替换后文本包含原有全部 3 条 Scenario（没变的也抄回来了）
  + 新增 1 条，评审看到的就是并回后的最终形态；
- **What Changes ↔ 改动**：列了「徽标 + 回退」两条行为，diff 里恰好对应这两处
  渲染逻辑，不多报、不漏报。

## 并回演示（PR 合并、交付 accept 后，按 conventions.md §3）

1. **应用 delta**：在 `openspec/specs/task-management/spec.md` 中，用提案
   `MODIFIED Requirements` 下的全文**整段覆盖**同名 `## Requirement: 任务卡详情与阶段呈现`
   段（按名称定位，不看 diff）。并回后该段效果（节选，仅演示——本示范件不真的改
   `specs/`）：

   ```markdown
   ## Requirement: 任务卡详情与阶段呈现

   任务卡详情 SHALL 返回任务行 + 时间线事件 + 阶段产物（拆分父另附子任务清单）。
   任务行头部 SHALL 展示优先级徽标：……（提案 MODIFIED 段全文，含 4 条 Scenario）……
   ```

2. **核对**：该域 spec 无重名 Requirement（新全文未引入与其它 Requirement 同名的段）、
   无空 Scenario（4 条都有 WHEN/THEN）、`## Purpose` 未被动过（本例 Purpose 不涉及
   ——若 Purpose 也要改，必须在提案 What Changes 里显式说明并走 MODIFIED）。
3. **归档**：`git mv openspec/changes/infera-900-add-delivery-priority-badge
   openspec/changes/archive/2026-08-25-infera-900-add-delivery-priority-badge`
   （日期用并回当日，内容保持提案定稿原样）。
4. **提交**：并回与归档同一 commit，例如
   `openspec: archive infera-900-add-delivery-priority-badge (INFERA-900)`。

完成后 `specs/task-management/spec.md` 恢复为唯一完整事实源，下一轮变更继续在它
上面做增量——示例闭环到此走完。

## 照抄要点

- 提案正文只保留用到的区块（本例只有 `MODIFIED`，无 `ADDED`/`REMOVED`——用不到
  的整个省略，不留空区块）；
- `MODIFIED` 全文 = 原文照抄（含全部既有 Scenario）+ 本次行为变化 + 必要的新
  Scenario，不是只写差异；
- Scenario 保持 WHEN/THEN 可测表述，不写实现细节；文件路径/组件名只作括号备注；
- 行为变化的判定与 MODIFIED/REMOVED/ADDED 的选择按
  [../../sdd-integration.md](../../sdd-integration.md) §4 的三问对照表。
