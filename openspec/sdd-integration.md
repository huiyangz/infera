# SDD 流程接入建议

把「维护对应能力域 spec」纳入每个功能任务的交付要求，让 [EVOLUTION.md](EVOLUTION.md)
定义的「delta → 评审 → 并回」生命周期嵌进 SDD 编排的日常动作里，从此 Spec 不再是
一次性的立项文档，而是随每个功能任务增量演进。

**定位**：本文件是流程建议（INFERA-283，纯文档交付），不改任何产品代码与编排配置；
机制、格式、职责一律以 [EVOLUTION.md](EVOLUTION.md)（谁维护、何时更新）与
[conventions.md](conventions.md)（§1 spec 格式、§2 delta 格式、§3 并回）为准，本文只做
「流程怎么接」，不重复定义格式。可照抄的完整示范见
[changes/example-sdd-spec-maintenance/change.md](changes/example-sdd-spec-maintenance/change.md)。

一句话主张：**功能任务的交付物 = 产品改动 + 对应能力域的 delta 提案，缺后者即交付不完整。**

## 1. 任务模板修改建议：新增「Spec 影响」段

SDD 任务卡（Tech Lead 派发给执行者的任务描述）现行六段结构，建议在 `Scope` 之后
插入 `Spec 影响` 段，并在 `Acceptance` / `Delivery` 各补一条对应条款。

### 修改前（现行模板）

```markdown
## Scope
<做什么、不做什么，In scope / Out of scope 分列>

## Depends on
<前置任务、上游分支、合入顺序>

## Contract
<跨卡契约：接口、数据形态、口径定义>

## Setup
<环境与依赖准备说明>

## Acceptance
<可核验的验收标准列表>

## Delivery
<交付方式：分支名、push 目标、汇报要求>
```

### 修改后（建议模板，新增部分以「★」标注）

```markdown
## Scope
<做什么、不做什么，In scope / Out of scope 分列>

## Spec 影响                                ★
- 受影响能力域：`task-management`、`gates-approvals`（从 README 8 域表中选；  ★
  纯内部改动无对应域时写「无」，并附一句理由）                                ★
- delta 提案路径：`openspec/changes/infera-<任务号>-<动词短语>/change.md`     ★
- 并回责任人：Tech Lead（accept 后按 EVOLUTION.md 并回把关）                 ★

## Depends on
<前置任务、上游分支、合入顺序>

## Contract
<跨卡契约：接口、数据形态、口径定义>

## Setup
<环境与依赖准备说明>

## Acceptance
<可核验的验收标准列表>
- 交付包含「Spec 影响」段列出的全部能力域的 delta 提案，What Changes       ★
  与实际产品改动一一对应                                                     ★

## Delivery
<交付方式：分支名、push 目标、汇报要求>
- delta 提案目录随任务分支一并 push，路径与「Spec 影响」段一致              ★
```

### 「Spec 影响」段字段说明

| 字段 | 要求 |
|---|---|
| 受影响能力域 | 按 [README](README.md) 的「域 ↔ 模块 ↔ 文档映射表」定位：改动落在哪个 `server/internal` 包 / `apps/web/src/features` feature，就归属哪个域。一卡可涉及多域，全部列出；同一域的多条变化写在同一份 change 的同一区块下（conventions.md §2） |
| delta 提案路径 | `openspec/changes/<change-id>/change.md`，`<change-id>` 建议带任务号前缀（如 `infera-300-add-xxx`），命名细则见 conventions.md §2。路径在派发时由 Tech Lead 写死，执行者不自行改名 |
| 并回责任人 | 固定为 Tech Lead（EVOLUTION.md「谁维护」表：并回时把关）。执行者的职责止于「交付包含 delta」，不直接改 `specs/` |

找不到归属域的两种情形：

- 改动只落在横切基础设施（`api`/`store`/`db`/`config`，服务全部 8 域、无独立业务语义）
  → 「Spec 影响」写「无：横切基础设施改动，不改可观察业务行为」，不强制提 change；
- 改动像是第 9 个业务域 → **不要**自行建 `specs/<新域>/`（EVOLUTION.md 硬性规则 2：
  目录名与结构已冻结）：在卡上说明新域的 Purpose 与首批 Requirement，作为提案交
  Tech Lead 评审后再建目录。

## 2. 编排流程修改建议：Tech Lead 的四个 spec 检查点

对应任务生命周期的四个环节，每个环节一个动作、一个卡点：

| 环节 | Tech Lead 动作 | 卡点（不满足时） |
|---|---|---|
| **① 计划（拆卡时）** | 按域↔模块映射表标注「受影响能力域」，写好「Spec 影响」段（域 + delta 路径 + 并回责任人）；判定不了归属域按上文两种情形处理 | 域归属不明 → 先弄清再拆卡，不带「Spec 影响：待定」派发 |
| **② 派发** | 任务卡发出前核对「Spec 影响」段三字段齐全、delta 路径与 change-id 命名合法 | 缺字段 → 补齐后再派发 |
| **③ 执行（执行者侧，Lead 只需知道）** | 执行者随功能变更在任务分支上提交 delta；实现期间行为有变，同步更新提案，不直接改 `specs/`（EVOLUTION.md「谁维护」） | — |
| **④ 验收（含 PR 评审）** | 核对 delta：**存在**（Spec 影响段列的每域都有区块）、**覆盖**（What Changes ↔ 实际改动一一对应）、**格式**（MODIFIED 给全文、名称与 `specs/` 原文逐字一致、无空区块，conventions.md §2） | 缺 delta 且无「无」豁免 → 视为交付不完整，验收退回 |
| **⑤ 并回（PR 合并、交付 accept 后）** | 按 conventions.md §3 把 delta 应用回 `specs/<domain>/spec.md` + `git mv` 归档，同一 commit 完成；并回后核对无重名 Requirement、无空 Scenario、`Purpose` 未动 | 核对不过 → 修完再算并回完成（EVOLUTION.md：并回由 Lead 把关） |

①②④⑤ 是 Lead 的检查点，③ 是执行者的既定职责（EVOLUTION.md 已冻结，无需重议）。
检查点全部是「读卡/读提案」级别的动作，不新增工具、不新增流程节点。

## 3. 验收标准建议

建议 Tech Lead 把以下条款直接抄进功能任务的 `Acceptance` 段（即上文模板中标注 ★ 的那条的展开）：

```markdown
- 交付物包含 `openspec/changes/<change-id>/change.md`，覆盖「Spec 影响」段
  列出的全部能力域；
- change.md 的 What Changes 与实际产品改动一一对应：不多报（列了没做的行为）、
  不漏报（做了没列的行为，含默认行为/回退行为的变化）；
- MODIFIED / REMOVED 段中的 Requirement 名称与 `openspec/specs/<domain>/spec.md`
  现有名称逐字一致（名称是 delta 的定位键，抄错即并回失败）；
- 纯内部任务（重构 / 测试补充 / 构建脚本，不改外部可观察行为）豁免：不强制
  delta，但「Spec 影响」段须写「无」并给理由（EVOLUTION.md「何时更新」）。
```

**「缺 delta 即交付不完整」的边界**：该要求只约束改变外部可观察行为的任务
（API 契约、页面行为、状态机、门禁规则、同步与 MCP 工具语义——EVOLUTION.md
「行为变化即 delta」）；对纯文档卡（如本卡 INFERA-283 本身）同样豁免。

## 4. 判定指引：MODIFIED / REMOVED 还是新加 Requirement

写 delta 前先问三个问题：

1. `specs/<domain>/spec.md` 里是否已有**名称相同**的 Requirement？
2. 是否已有**语义相同**（同一行为契约，名称不同或散落多条）的 Requirement？
3. 本次改动是**收紧/扩展**既有行为，还是**替换/废除**它，还是**全新能力**？

对照表：

| 情形 | 用法 | 要点 |
|---|---|---|
| 全新能力，specs 无对应 Requirement | `ADDED` | 写法与 spec 正文同款，并回时追加到该域 spec 末尾 |
| 既有 Requirement 的行为收窄、扩展或某条 Scenario 的 THEN 变了 | `MODIFIED` | **给出替换后的完整全文**（含全部 Scenario，包括没变的）——评审看到的就是并回后的最终形态 |
| 行为彻底废除且无替代 | `REMOVED` | 需求名 + 一句理由，不写 Scenario；并回时整段删除 |
| 旧行为被新契约取代 | `REMOVED` 旧 + `ADDED` 新 | 同一 change 内两个区块并列，What Changes 里说明取代关系 |
| 需求改名（行为不变） | `REMOVED` 旧名 + `ADDED` 新名 | 名称是域内唯一且稳定的定位键，改名不等同于 MODIFIED（conventions.md §1） |
| 新页面/新端点，但既有 Requirement 一字未动 | `ADDED`，不碰既有条目 | 「相关」不等于「修改」：只有既有 Scenario 的结果真的变了才 MODIFIED |

常见误用（评审时重点盯）：

- `MODIFIED` 只写变化的部分而不是全文 → 并回整段覆盖后会丢掉未抄回的 Scenario；
- 废除的行为不提 `REMOVED`，悄悄不提 → `specs/` 留下与现实不符的死需求；
- 借一次 `MODIFIED` 顺手重写整域 → 违反 EVOLUTION.md 硬性规则 1（禁止另起炉灶）；
- 同一行为拆成多条新 `ADDED`，而实际是拆碎了既有一条 Requirement → 加剧碎片化；
  应优先一条 `MODIFIED` 表达同一契约的演进。

## 5. 可选建议：是否引入 openspec CLI（仅建议，不实施）

现状：本仓是**纯 Markdown 约定**（README：不引入 openspec CLI、不装任何依赖），
格式由 conventions.md 冻结。上游 [Fission-AI/OpenSpec](https://github.com/Fission-AI/OpenSpec)
的 CLI 提供 `validate`（提案结构校验）、spec 一致性检查与归档清单等能力。

引入能补上的短板：

- delta 引用的 Requirement 名称是否存在（防抄错名导致并回失败）；
- 域内重名 Requirement、空 Scenario、空区块等机械错误；
- 并回后 `specs/` 与已归档提案的一致性对账。

代价与风险：

- 与「不装任何依赖」的既定原则直接冲突（README / conventions.md 开头条款）；
- 本仓 spec 为中文变体格式，CLI 的解析假设未必兼容，可能反过来迫使格式向工具让步
  ——格式主权在 conventions.md，不在工具；
- 8 域 89 条 Requirement、并回频率 = 功能任务频率，人工核对成本目前很低。

**建议：暂不引入。** 维持纯 Markdown + 人工把关（本文 §2 检查点 ④⑤ 已把机械核对
变成 checklist）。触发重评估的条件：人工并回核对反复出错、或能力域数量明显增长
（如 >12 域）时，先以 change 的方式提案（评估自研轻量 lint 脚本或引入 CLI），
经 Tech Lead 评审再动——结构文件与工具链的变化同样走 EVOLUTION.md 硬性规则 3。

## 6. 落地清单

把本建议变成编排动作，Tech Lead 只需做三件事：

1. 派发功能任务时用 §1 的「修改后」模板（多花一分钟填「Spec 影响」三字段）；
2. 验收时按 §2 检查点 ④ 的三个核对（存在 / 覆盖 / 格式）过一遍 delta；
3. accept 后按 §2 检查点 ⑤ 并回 + 归档（流程即 conventions.md §3，无需新学习）。

执行者只需记住一句：**行为变了就提 delta，提案路径看卡上的「Spec 影响」段。**
完整照抄范本：[changes/example-sdd-spec-maintenance/change.md](changes/example-sdd-spec-maintenance/change.md)。
