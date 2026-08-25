# spec / delta 约定

写 `openspec/` 下任何文件前先读本文。格式由本文件冻结，后续任务（T02–T09
及所有功能任务）严格跟随，不得另立格式或改动既有结构文件
（README.md / conventions.md / EVOLUTION.md / templates）。

通用规则：

- 全部是纯 Markdown，不依赖 openspec CLI 或任何工具；
- 语言用中文，域目录名、Requirement/Scenario 标题语法（英文关键字）保持原样；
- Requirement 行为语句用 **SHALL/SHALL NOT**（可验证的表述，不写实现细节）；
- 一个 Scenario 聚焦一个可测行为，用 `WHEN/THEN` 两段式（必要时加 `WHILE`、`AND`）。

## 1. spec 格式：`openspec/specs/<domain>/spec.md`

每个能力域一个目录，目录名即域 ID（见 README 的 8 域表）。域内只有一个
`spec.md`，结构固定为：

```markdown
# <domain>

## Purpose

<一段话：这个域管什么、不管什么、边界在哪。>

## Requirement: <需求名（祈使句短名）>

<一条可验证的行为陈述，主体 SHALL ……。>

#### Scenario: <场景名>

- **WHEN** <条件/动作>
- **THEN** <可观察、可测试的结果>

#### Scenario: <另一个场景名>

- **WHEN** ……
- **THEN** ……

## Requirement: <下一条需求名>
……
```

要点：

- `## Purpose` 每域必有，且只有一段；
- 每条需求是一个 `## Requirement: <名称>` 段，可含多条 Scenario；
- Requirement 名称在**域内唯一**且**稳定**——delta 靠名称定位要改的需求，
  改名等同于 REMOVE 旧名 + ADD 新名；
- spec 写「系统应满足什么」，不写「代码怎么写的」；引用实现位置可用
  括号备注（如 `（REST 面：api/projects.go）`），但行为以 Scenario 为准。

## 2. delta 提案格式：`openspec/changes/<change-id>/change.md`

`<change-id>` 用 kebab-case 动词短语（如 `add-gate-comment-timestamp`），
必要时带任务号前缀（如 `infera-281-add-xxx`）。提案目录内至少一个
`change.md`（可另加 `design.md` 等辅助文件），结构固定为：

```markdown
# <change-id>: <一句话标题>

## Why

<为什么改：动机、关联任务卡/issue、要解决的问题。>

## What Changes

- <域>: <一句话变化>
- <域>: <一句话变化>

## ADDED Requirements

### Requirement: <新需求名>

<完整的行为陈述（与 spec 中同款写法）。>

#### Scenario: <场景名>

- **WHEN** ……
- **THEN** ……

## MODIFIED Requirements

### Requirement: <既有需求名（原文照抄名称）>

<**替换后的完整全文**——不是 diff，并回时整段覆盖同名 Requirement。>

#### Scenario: <场景名>

- **WHEN** ……
- **THEN** ……

## REMOVED Requirements

### Requirement: <既有需求名>

<一句话说明为什么移除/被谁取代。>
```

三种 delta 区块的语义：

| 区块 | 语义 | 内容要求 |
|---|---|---|
| `## ADDED Requirements` | 该域新增需求 | 完整需求 + Scenario，写法与 spec 同款 |
| `## MODIFIED Requirements` | 改既有需求 | **完整替换后的全文**（含全部 Scenario），并回时按名称整段覆盖 |
| `## REMOVED Requirements` | 删既有需求 | 只需需求名 + 移除理由，不写 Scenario |

规则：

- 一个 change 可含多个域的区块；同一域的多条变化写在同一区块下；
- 用不到的区块整个省略（不要留空区块）；
- `MODIFIED` 必须给出替换后**全文**——评审看到的就是并回后的最终形态；
- 禁止在不提 change 的情况下直接改 `specs/`（唯一例外见 EVOLUTION.md 的并回动作）。

假想完整示例（演示 ADDED 与 MODIFIED 两种写法）见
[changes/templates/example.md](changes/templates/example.md)。

## 3. 并回（archive）流程

提案合入（功能交付被接受）后，把 delta 应用回活文档：

1. **应用 delta**：对 change.md 里每个域的每个区块，在
   `openspec/specs/<domain>/spec.md` 上执行——
   - ADDED：把 `### Requirement:` 段整体追加到该域 spec 末尾
     （标题降一级：`### Requirement:` → `## Requirement:`，Scenario 层级不变）；
   - MODIFIED：用新全文**整段覆盖**同名 `## Requirement:` 段；
   - REMOVED：删除同名 `## Requirement:` 段。
2. **核对**：应用后该域 spec 无重名 Requirement、无空 Scenario、
   `## Purpose` 未被动过（Purpose 变化必须来自显式的 MODIFIED 说明）。
3. **归档**：`git mv openspec/changes/<change-id> openspec/changes/archive/<YYYY-MM-DD>-<change-id>`
   （日期用并回当日）。
4. **提交**：并回与归档在同一个 commit 里完成，commit message 引用任务号，
   例如 `openspec: archive infera-281-add-gate-comment-timestamp`。

并回由 Tech Lead 执行或在 Tech Lead 把关下执行（职责划分见 EVOLUTION.md）。
