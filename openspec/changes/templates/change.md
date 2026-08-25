# <change-id>: <一句话标题>

> 复制本模板到 `openspec/changes/<change-id>/change.md` 起手。
> 格式细则与各区块语义见 [../../conventions.md](../../conventions.md) §2；
> 假想完整示例见 [example.md](example.md)。用不到的区块整个删掉。

## Why

<为什么改：动机、关联任务卡/issue、要解决的问题。>

## What Changes

- <域>: <一句话变化>

## ADDED Requirements

### Requirement: <新需求名>

<完整的行为陈述：主体 SHALL ……。>

#### Scenario: <场景名>

- **WHEN** <条件/动作>
- **THEN** <可观察、可测试的结果>

## MODIFIED Requirements

### Requirement: <既有需求名（原文照抄名称）>

<**替换后的完整全文**（含全部 Scenario）——并回时按名称整段覆盖，不是 diff。>

#### Scenario: <场景名>

- **WHEN** ……
- **THEN** ……

## REMOVED Requirements

### Requirement: <既有需求名>

<一句话说明为什么移除/被谁取代。>
