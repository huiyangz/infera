# 假想示例：演示 ADDED 与 MODIFIED 写法

> 这**不是真实提案**，只是教学示例（假想任务「给审批卡评论加时间戳显示」），
> 演示一个 change.md 如何同时用 `ADDED` 与 `MODIFIED` 表达增量。
> 真实提案落在 `openspec/changes/<change-id>/change.md`，不放进 templates/。
> 并回（archive）怎么把下面两段应用回 specs/，见 conventions.md §3。

---

以下为示例正文（假设 `specs/gates-approvals/spec.md` 里已有名为
`审批卡评论展示` 的 Requirement）：

# example-gate-comment-timestamp: 审批卡评论显示相对时间与绝对时间戳

## Why

审批卡上的评论目前只显示相对时间（如「3 小时前」），跨时区排障时无法
还原确切时刻（关联假想任务 INFERA-999）。

## What Changes

- gates-approvals: 审批卡评论在相对时间基础上提供绝对时间戳；新增评论
  排序稳定性的要求。

## ADDED Requirements

### Requirement: 评论排序稳定

同一审批卡下的评论 SHALL 按创建时间升序排列，同秒内的多条评论 SHALL
按服务端接收顺序排列，不随刷新改变。

#### Scenario: 同秒多条评论顺序稳定

- **WHEN** 同一秒内先后提交两条评论并刷新页面
- **THEN** 两条评论的相对顺序与提交顺序一致

## MODIFIED Requirements

### Requirement: 审批卡评论展示

审批卡上的每条评论 SHALL 同时展示相对时间与绝对时间戳
（`YYYY-MM-DD HH:mm:ss`，跟随浏览器本地时区），相对时间 SHALL 在
评论创建后 7 天内展示、之后仅展示绝对时间戳。

#### Scenario: 查看新评论的时间

- **WHEN** 评论创建后 1 小时查看审批卡
- **THEN** 该评论同时显示「1 小时前」与本地时区的绝对时间戳

#### Scenario: 查看超过 7 天的评论

- **WHEN** 评论创建 8 天后查看审批卡
- **THEN** 该评论仅显示绝对时间戳

---

示例要点：

- `MODIFIED` 给出的是**替换后的完整全文**（全部 Scenario 重写），
  并回时整段覆盖 `specs/gates-approvals/spec.md` 中同名 Requirement；
- `ADDED` 的新需求写法与 spec 正文同款，并回时追加到该域 spec 末尾；
- Scenario 用可测的 WHEN/THEN，不写实现细节（不提组件名、函数名）。
