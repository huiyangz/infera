package store

import (
	"math"
	"slices"
	"time"
)

// 本文件是 ProjectStageRuns（INFERA-234 T01）的共用装配逻辑：耗时计算与
// 分 stage 聚合在 pg/memory 两个实现间共享同一份代码，语义天然一致。

// stageRunDurationMS 已收尾运行的耗时（毫秒）；未收尾（finished_at nil）→ nil。
func stageRunDurationMS(startedAt time.Time, finishedAt *time.Time) *int64 {
	if finishedAt == nil {
		return nil
	}
	ms := finishedAt.Sub(startedAt).Milliseconds()
	return &ms
}

// aggregateByStage 分 stage 聚合明细窗口（冻结契约见 StageRunStageStats）：
// 计数含全部行，耗时只统计已收尾行；行按 stage 字典序升序输出。
func aggregateByStage(runs []StageRunDetail) []StageRunStageStats {
	type acc struct {
		stats     StageRunStageStats
		durations []float64
	}
	byStage := map[string]*acc{}
	for _, r := range runs {
		a, ok := byStage[r.Stage]
		if !ok {
			a = &acc{stats: StageRunStageStats{Stage: r.Stage}}
			byStage[r.Stage] = a
		}
		a.stats.Total++
		switch r.Status {
		case "done":
			a.stats.Done++
		case "failed":
			a.stats.Failed++
		case "running":
			a.stats.Running++
		}
		if r.DurationMS != nil {
			a.durations = append(a.durations, float64(*r.DurationMS))
		}
	}
	stages := make([]string, 0, len(byStage))
	for s := range byStage {
		stages = append(stages, s)
	}
	slices.Sort(stages)
	out := make([]StageRunStageStats, 0, len(stages))
	for _, s := range stages {
		a := byStage[s]
		a.stats.AvgMS = avgMS(a.durations)
		a.stats.P95MS = p95MS(a.durations)
		out = append(out, a.stats)
	}
	return out
}

// avgMS 均值（空集 0）。
func avgMS(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// p95MS 最近邻位法（nearest-rank）：升序取第 ceil(0.95n) 个（1-based）；
// 空集 0。
func p95MS(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Clone(xs)
	slices.Sort(sorted)
	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}
