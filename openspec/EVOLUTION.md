# 演进规则

`openspec/specs/` 是活文档业务模型：**永远在原有模型上增量演进**。
本文冻结「怎么演进、谁维护、何时更新」。

## 生命周期：delta → 评审 → 并回

```
功能任务立项
   │  1. 提 delta：openspec/changes/<change-id>/change.md
   │     （ADDED / MODIFIED / REMOVED Requirements，格式见 conventions.md）
   ▼
评审（随功能一起）
   │  2. spec 层面的异议在 change.md 上改，达成一致后实现
   ▼
功能交付
   │  3. 交付物必须包含：产品改动 + 对应 delta 提案
   ▼
并回（accept 后，Tech Lead 把关）
   │  4. 按 conventions.md §3 把 delta 应用回 specs/<domain>/spec.md
   │ 5. 提案 git mv 进 changes/archive/<YYYY-MM-DD>-<change-id>/
   ▼
specs/ 恢复为唯一完整事实源，下一轮变更继续在它上面做增量
```

## 谁维护

| 角色 | 职责 |
|---|---|
| 功能任务执行者 | 交付时**必须**带上对应能力域的 delta 提案（`changes/<change-id>/change.md`）；实现期间 spec 层变化同步更新提案，不直接改 `specs/` |
| Tech Lead | 评审 delta 与实现是否一致；**并回时把关**：确认 delta 已应用回 `specs/`、归档到位、无重名/空段落，才算并回完成 |
| 任何后来者 | 只读消费 `specs/`；发现 spec 与现实不符时，提一个新 change 来修（含 MODIFIED/REMOVED），不在原 spec 上顺手涂改 |

## 何时更新

- **功能任务交付前**：对应能力域的 delta 提案是交付物的一部分——没有它，
  该任务对业务模型的影响就丢了；
- **行为变化即 delta**：任何改变外部可观察行为（API 契约、页面行为、状态机、
  门禁规则、同步与 MCP 工具语义）的任务都要提 change；
- **纯内部重构、测试补充、构建脚本**不改变行为的，不强制提 change；
- **并回**发生在交付被接受后（不是提 PR 时），由并回动作一次性完成
  specs 更新 + archive 归档（同一 commit，见 conventions.md §3）。

## 硬性规则

1. **禁止另起炉灶重写既有 spec**：不允许新建一份「v2 spec」绕开存量、
   不允许整域推倒重写、不允许直接编辑 `specs/` 而不经过 change——
   对既有内容的任何改动只能以 MODIFIED / REMOVED delta 表达。
2. **8 个能力域的目录名与结构冻结**（见 README）：新增域本身就是一个
   change（在提案中说明新域的 Purpose 与首批 Requirement），
   并经 Tech Lead 评审后才可建目录。
3. **格式以 conventions.md 为准**：后续任务不得另立格式或改动
   README.md / conventions.md / EVOLUTION.md / templates——要改这些
   结构文件，同样提 change 且由 Tech Lead 评审。
4. **spec 是行为契约不是实现文档**：文件路径、函数名只能作括号备注；
   冲突时以 Scenario 为准、以代码评审为裁判。
