# R11 真实 agent 接入冒烟报告

日期：2026-08-19 ｜ 分支：cv/9c2900f2c350-l202608190011-7（基于 feature/infera-closure @ b01b022）

## 结论

**真产品全链路冒烟未执行——三产品接入信息（地址/命令/镜像/凭证）未获得。**
本机无 infCode/testAgent/codeReviewAgent 二进制，编排者未提供任何接入环境变量或文档（全仓库仅
编排配置设计稿把 infCode 用作示例显示名）。按任务书约定走 fallback：**配置占位、阻塞上报、不伪造结果**。

占位之下，以下内容是**真实执行并验证过的**（专用环境，生产二进制，无 fake 结果混入）：

| # | 验证项 | 结果 |
|---|--------|------|
| V1 | 三产品按真实接口形态注册 + 按角色绑定 6 个可绑定节点（脚本幂等可复现） | ✅ |
| V2 | http 形态线上契约：三个审查角色真实分派、请求/响应形状、R10 findings 解析 | ✅ |
| V3 | 7 阶段链路（生成段=infera echo 桩 + 审查段=codeReviewAgent http 形态）走到 completed | ✅（部分桩） |
| V4 | 占位接入显式失败：交付 blocked 于首阶段、零产物，不可能被误读为产品产出 | ✅ |
| V5 | design/tasks 绑定缺口：11 阶段路径在生产装配下必 blocked（平台 bug，见问题 P1） | ❌ 平台阻塞 |

场景 a（小需求 7 阶段全链）：**配置面与审查链已就绪并验证**；生成段因无产品接入用 infera 内置
echo 桩代替，真跑待接入信息到位。
场景 b（大需求 11 阶段全链）：**被平台 bug P1 阻塞**——与产品接入无关，修掉 P1 之前真跑也过不了
design 阶段。

## 配置清单（可复现）

本目录三件套：

- `r11-agents.env.example` — 接入信息模板（编排者填写，即任务书的「占位环境变量」通道）
- `register-real-agents.sh` — 幂等注册 + 角色绑定脚本（env 驱动；未给真值自动落显式失败占位）
- `http-contract-stub.py` — http 形态契约验证 stub（infera 侧工具，记录 infera 实际发出的请求）

角色绑定映射（脚本内固化）：

| 节点 | 产品 | 说明 |
|------|------|------|
| spec、code_gen | infCode | 生成/任务类 |
| test_gen | testAgent | 测试生成 |
| code_review（门禁预审）、spec_conformance、code_quality | codeReviewAgent | 审查类，含 R10 双道 |
| design、tasks | （应属 infCode） | **平台暂不可绑定**，见问题 P1 |

三种真实接口形态的契约（验证过的行为，产品方按此适配）：

- **cli**：服务器侧 `sh -c <command>`；进程 cwd=workdir；环境变量 `INFERA_ROLE` / `INFERA_WORKDIR` /
  `INFERA_PROMPT`，prompt 同时写 stdin；stdout+stderr 合并即产物；非零退出=阶段失败。
- **http**：`POST $url`，请求体 `{"role","prompt","workdir"}`（Content-Type: application/json，
  **无任何鉴权头**，见问题 P2），期待 `200 {"output":"..."}`；output 含 `infera-findings`
  fenced block 时被 R10 解析为结构化审查意见。
- **docker**：一次性容器；workdir 绑定挂载到 `/work`；prompt 追加为命令最后一个参数；
  **不透传任何环境变量**（role/凭证均不可用，见问题 P3）。

## 冒烟环境（专用，无共享资源污染）

- 数据库：`infera-postgres` 容器（:5433）内**新建** `infera_smoke_r11` 库——未触碰主 checkout 使用的
  `infera_v2`，也未用共享测试库 `infera-postgres-test`。
- 服务：生产二进制 `go build ./cmd/infera` 于 **:18087**（避开主 checkout 的 8080/5173）；
  `AGENT_CMD=echo`、`TEST_CMD=true`、无 GITHUB_TOKEN（仓库用本地 bare repo，persist=本地 commit）。
- workdir / 日志 / bare repos：`/tmp/infera-smoke-r11/`（证据原文留存：`logs/`）。

复现（环境同上启动后）：

```sh
set -a; source docs/smoke/r11-agents.env   # 编排者填好的接入信息
set +a; docs/smoke/register-real-agents.sh  # 注册+绑定；SMOKE_PROJECT_ID 可改为项目级绑定
```

## 链路时间线与证据

### V4 占位显式失败（delivery d37a200f，全局绑定=三产品占位）

```
03:37:11 spec stage_started → stage_failed → delivery_blocked
stage_failed: exit status 70（占位命令拒绝执行，报缺 INFCODE_COMMAND）
artifacts: []   ← 零产物，不存在被误读为产品结果的任何内容
```

### V5 design/tasks 不可绑定（API 证据 + delivery 635a5739 运行时证据）

```
PUT /api/pipeline（全量6节点+design）→ 400 "不可绑定的节点: design"
PUT /api/pipeline（全量6节点+tasks）→ 400 "不可绑定的节点: tasks"
大需求路径（complexity=large 批准后）：
03:37:28 design stage_started → stage_failed → delivery_blocked
blocked 原因: "stage design: orchestration: 节点缺少有效 agent 绑定: design"
```

### V2+V3 http 契约与 7 阶段链（delivery bfd62b80，项目级绑定：
spec/test_gen/code_gen→default-cli(echo 桩)，code_review/双道→codeReviewAgent(http→stub)）

```
03:37:44 spec→gate 03:37:48 批准(small) → test_gen → code_gen → unit_test
03:37:48 code_review: persist_done → review_findings ×2 → gate_pending
03:38:10 code_review 批准 → delivery_completed
artifacts: spec,tests,summary,test_output,diff,agent_output,
           spec_conformance_findings,code_quality_findings
```

stub 收到的 3 个生产请求（完整请求日志 `logs/stub-requests.jsonl`）：

| role | prompt 长度 | workdir |
|------|------------|---------|
| code_review | 352 | …/workdirs/bfd62b80-… |
| spec_conformance | 808 | 同上（带规格全文） |
| code_quality | 749 | 同上 |

请求头全集：`Accept-Encoding, Content-Length, Content-Type, Host, User-Agent`——**无鉴权通道**（P2 证据）。
响应 output 被 R10 正确解析：两道 findings 均落为结构化
`{"review","task_based":false,"findings":[],"raw":…}` artifact。

**PR 链接证据说明**：本环境无 GITHUB_TOKEN、仓库为本地 bare repo，persist 只做本地 commit
（diff artifact 为空——echo 桩未改文件）。PR 链接证据需真跑时在绑 GitHub 库的项目上产生。

## 阻塞点（上报编排者）

**B1｜三产品接入信息缺失**——唯一挡住真跑的外部前提。每产品需要：形态（cli/http/docker）+
对应参数（命令 / URL / 镜像+命令）+ 凭证。落进 `r11-agents.env` 后执行上面复现命令即完成接入，
随后在绑库项目跑两条链路（场景 b 还需先修 P1）。

## 问题清单（infera 侧，建议编排者开修复任务；本任务未改产品代码）

| # | 级别 | 问题 | 证据 | 建议 |
|---|------|------|------|------|
| P1 | **阻塞场景 b** | design/tasks 是 agent 阶段但不在 `orchestration.BindableNodes`：生产装配（main.go 设 ResolveRunner）下大需求路径到 design 必 blocked；11 阶段 E2E 未覆盖此路径（测试未设 ResolveRunner，走兜底单 runner） | V5 双证据 | BindableNodes 纳入 design/tasks（注意既有约定：旧默认绑定需重 PUT pipeline） |
| P2 | 集成缺口 | http runner 无鉴权通道：config 只有 url，请求不带任何可配 header——任务前提里的「凭证」对 http 形态无处安放（仅能进 URL 查询串） | V2 请求头全集 | config 增 headers 或 auth_bearer |
| P3 | 集成缺口 | docker runner 不透传 env：容器内拿不到 role 与凭证；prompt 仅作末参，产品适配面受限 | docker.go 契约 | config 增 env 数组 + 注入 INFERA_ROLE |
| P4 | 小 | cli runner 非零退出时 output 被丢弃，stage_failed 只剩 "exit status 70"——占位/产品排障信息丢失（本报告占位语义靠退出码+服务端约定传达） | V4 | err 非 nil 时也落 output（artifact 或事件） |
| P5 | 小 | http 超时固定 10min，大任务可能不够 | http.go | config 可配 timeout |

## 冒烟环境遗留状态

- `infera_smoke_r11` 库保留：3 个产品 agent + 绑定 + 3 条 delivery 证据可随时查（重跑脚本幂等）。
- :18087/:18090 进程已停。库中 codeReviewAgent 当前为 http→stub 形态（V2/V3 所需），重跑注册脚本即复位。
