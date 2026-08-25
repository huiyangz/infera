package store

import "time"

// 本文件是 WorkspaceStats（L202608251850-1-T01）的共用装配逻辑：五类状态
// 归并、执行统计与逐小时分桶在 pg/memory 两个实现间共享同一份代码，聚合
// 口径天然一致（与 agentactivity.go 同款做法）。

// deliveryStatusKeys 需求状态固定键（与 RequirementStats.ByStatus 同款）：
// 原始计数恒含五键，无行为 0。
var deliveryStatusKeys = []string{"active", "queued", "completed", "blocked", "cancelled"}

// normalizeStatusCounts 原始状态计数归一：只保留固定键（缺席补 0），未知
// 状态不进桶（仍计入 Total）。
func normalizeStatusCounts(raw map[string]int) map[string]int {
	out := make(map[string]int, len(deliveryStatusKeys))
	for _, k := range deliveryStatusKeys {
		out[k] = raw[k]
	}
	return out
}

// assembleWorkspaceStats 跨项目统计装配（冻结契约见 WorkspaceStats）：
// 状态分布为全量快照（statusCounts 需为全部 deliveries 的按状态计数）；
// 执行统计与逐小时分桶只统计窗口 [from,to) 内的行（半开区间：started_at
// ==from 计入、==to 剔除；查询层已过滤，此处防御性兜底）。计数不分状态
// （attempt 各计一次）；时长只累计已收尾行（finished_at 非空）的
// finished-started，整段计入起始桶——跨小时收尾不拆分；running 计次不计
// 时长。loc nil → UTC。窗口非正 → ErrInvalid。
func assembleWorkspaceStats(statusCounts map[string]int, runs []StageRun, from, to time.Time, loc *time.Location) (WorkspaceStats, error) {
	if !from.Before(to) {
		return WorkspaceStats{}, ErrInvalid
	}
	if loc == nil {
		loc = time.UTC
	}

	ts := WorkspaceTaskStatus{ByStatus: normalizeStatusCounts(statusCounts)}
	for _, n := range statusCounts {
		ts.Total += n
	}
	// 五类归并（Multica 统计口径）：completed→已完成、active→进行中、
	// queued/blocked→待办（均未被引擎自动推进）、cancelled→已取消。
	ts.Done = ts.ByStatus["completed"]
	ts.InProgress = ts.ByStatus["active"]
	ts.Todo = ts.ByStatus["queued"] + ts.ByStatus["blocked"]
	ts.Cancelled = ts.ByStatus["cancelled"]

	ex := WorkspaceExecution{}
	hourly := make([]WorkspaceHourBucket, 24)
	for i := range hourly {
		hourly[i].Hour = i
	}
	for _, r := range runs {
		if r.StartedAt.Before(from) || !r.StartedAt.Before(to) {
			continue
		}
		ex.RunsTotal++
		switch r.Status {
		case "done":
			ex.Done++
		case "failed":
			ex.Failed++
		case "running":
			ex.Running++
		}
		var dur int64
		if r.FinishedAt != nil {
			dur = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
			ex.DurationMSTotal += dur
		}
		b := &hourly[r.StartedAt.In(loc).Hour()]
		b.Runs++
		b.DurationMS += dur
	}
	return WorkspaceStats{TaskStatus: ts, Execution: ex, Hourly: hourly}, nil
}
