# 子需求拆分与并行合并 设计

日期：2026-08-17（与用户多轮确认）
状态：确认开工

## 模型（用户原话浓缩）

1. 父需求正常走 intake → spec → **规格审批**：在这里决定拆不拆
2. **拆分方案由 AI 给、人工可编辑**：spec agent 在规格末尾附带结构化拆分建议
   （子需求标题+描述+**并行批次**），审批页展示为可编辑清单
3. 拆分执行：父 **跳过 test_gen**，停在「实现」；子需求落库（各自完整流水线到审查与交付）
4. **批次调度**：同批次并行启动；某批次全部完成并合并后，下一批次才启动；
   不能并行的子需求排成单元素批次（串行）
5. **增量合并**：子需求完成一个就合一个进父（不等齐）
6. **冲突**：父暂停合并队列（其它子需求继续跑），给出"本地合并指引"（git 命令）；
   人工在本机解冲突、推 `infera/<父id前8位>` 分支，回页面点「继续」→
   服务器 fetch 该分支 reset 父 workdir → 恢复合并队列
7. 全部子需求完成并合并 → 父 unit_test → 审查与交付（persist/PR 复用现有 code_review 到达逻辑）→ 闭环

## 数据模型（migration 0003）

deliveries 新增：
- `parent_id UUID NULL REFERENCES deliveries(id)` —— 子需求指向父
- `wave INT NOT NULL DEFAULT 0` —— 批次号（1..N；父与普通需求=0）
- `split_mode BOOLEAN NOT NULL DEFAULT FALSE` —— 父在规格审批选择了拆分
- `merge_state TEXT NOT NULL DEFAULT ''` —— 父合并状态：'' | 'conflict'

store：字段进 Delivery/INSERT/UPDATE/ListProjectDeliveries（前端自建树），
新增 `ListChildDeliveries(ctx, parentID)`。

## spec artifact 的拆分建议约定

spec prompt 追加：若需求适合拆分，输出末尾附 fenced block：

    ```infera-split
    [{"title":"登录接口","description":"...","wave":1}]
    ```

无 block = 不建议拆分。handleGate(spec_approval) 解析后随 gate 响应返回 `split_plan`；
解析失败按无建议处理（不报错）。

## 引擎（graph 不变，节点语义按 split_mode 分岔）

- `Approve(ctx, id, split []ChildSpec)`：spec_approval 且 split 非空 →
  父 split_mode=true、CurrentStage 直接跳 code_gen（跳过 test_gen）、落库；
  为每条子建 delivery（parent_id/wave）+ delivery_created 事件；父 emit `split` 事件。
  返回创建的子需求；**启动子需求由 api 层驱动**（engine 注入
  `OnStartDelivery func(id)` 回调，wave1 及后续批次启动都走它）。
- 子完成钩子（advance DONE 且 parent_id != ""）：`maybeDriveParent(parentID)`：
  - 引擎内部 per-parent 互斥（sync.Map）串行化合并与推进（多个子并行完成安全）
  - 若父 merge_state=conflict → 只 emit `merge_queued` 事件（队列暂存）
  - 否则 merge 该子分支进父 workdir：`git fetch <repo> infera/<子前8>` +
    `git merge FETCH_HEAD --no-edit`
    - 冲突 → merge_state=conflict，emit `merge_conflict`（payload 含子标题、
      全部子分支名、给人工的完整 git 命令文本）
    - 成功 → emit `merge_done`；若该子所在批次全部完成 → 启动下一批次子需求
  - 全部子完成且无冲突 → 父写 code_gen artifact（合并摘要）→ advance unit_test → 正常走
- `ResumeMerge(ctx, id)`（冲突恢复，api 新端点调用）：
  校验 split 父且 conflict → fetch `infera/<父前8>` → `reset --hard` 父 workdir →
  merge_state='' → 依次合并仍在排队的已完成子需求 → 若全部完成 advance；
  api 层随后 runDelivery 驱动父。
- git 包新增：`Fetch(ctx, dir, repoURL, ref) error`、`Merge(ctx, dir, msg) error`
  （冲突返回可识别错误——输出含 CONFLICT）、`ResetHard(ctx, dir, ref) error`。

## API 变更

- `POST /api/deliveries/{id}/approve` 接受可选 body `{"split":[{title,description,wave}]}`
  （EngineAPI.Approve 签名扩展；空/无 body = 普通批准）
- `GET /api/deliveries/{id}/gate`：spec_approval 响应附 `split_plan`（解析结果）
- `POST /api/deliveries/{id}/merge/resume`：冲突恢复（调 engine.ResumeMerge 后驱动父）
- `GET /api/deliveries/{id}`：split 父附 `children: [Delivery]`
- 事件新增：`split` / `merge_done` / `merge_conflict` / `merge_queued`

## 前端

- 类型：Delivery + parent_id/wave/split_mode/merge_state；GateInfo + split_plan
- 规格审批页：拆分方案编辑器（行=标题/描述/批次号，可增删改，AI 建议预填）；
  「批准」与「批准并拆分」两个提交
- 项目左栏：父子树（父行折叠箭头+子缩进；父停在实现时显示
  「已合并 x/y · 批次 w/W」；merge_state=conflict 显示冲突标记）
- 父详情：实现阶段状态化为 等待子需求/合并中/冲突；冲突时横幅展示
  git 指引（可复制）+「合并已推送，继续」按钮（POST merge/resume）；
  档案区增子需求清单（各自阶段徽标）

## 测试

- 引擎单测（真 git + 临时 bare，persist 包同款手法）：拆分落库与批次门控、
  增量合并顺序、冲突→队列→恢复、父最终完成
- E2E：本地 bare 仓库 + fake agent（spec 输出带 infera-split 块）：
  拆分→两波子需求各自过门→增量合并→父完成且 bare 上有 infera/<父前8> 合并分支
- 冲突路径 E2E 用两个子需求改同一行制造真冲突；恢复用推送解决分支模拟

## 非目标

- 完整 DAG 依赖（批次模型先行）；agent 自动解冲突（以后叠加）；
  code-server 网页编辑器（事件已带 workdir 与命令，可后续接链接）
