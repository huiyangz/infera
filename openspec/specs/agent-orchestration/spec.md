# agent-orchestration（Agent 编排与执行）

## Purpose

管理交付的执行引擎与 agent 编排：阶段图（intake → spec → test_gen → code_gen → unit_test → DONE，含回环与 blocked 语义）、agent 可绑定节点与项目级绑定、agent 进程/容器执行、workdir 生命周期、产物固化（本地 commit / push + PR）与 unit_test 命令节点。

（正文由第 2 层任务补齐）
