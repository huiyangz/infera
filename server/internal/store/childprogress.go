package store

import "slices"

// 本文件冻结「子任务真实进度聚合」契约（L202608260142-1-T01）：父任务的子
// 任务按阶段（wave）分组的真实状态计数，前端任务详情页进度区以本聚合为唯一
// 数据源，不得另开并行入口。聚合是纯函数（输入子任务平表），pg/memory 两库
// 经既有只读查询 ListChildDeliveries 取数后口径天然一致，无写路径。
//
// 状态维度不发明新值：原始计数 ByStatus 恒含 deliveries 既有五键
// （active/queued/completed/blocked/cancelled，未知状态只计入 Total）。
// 六个展示计数是把这五个真实状态按 Multica 语义归并出的只读视图：
//
//	done        ← completed（终态）
//	in_progress ← active 且 pending_gate 为空（引擎正在推进）
//	in_review   ← active 且 pending_gate 非空（已产出、停在门禁等人工验收）
//	blocked     ← blocked（等待外部输入）
//	todo        ← queued（还没轮到自己的批次，未被引擎推进）
//	cancelled   ← cancelled（终态）
//
// 六者互斥且恰好覆盖五个已知状态（in_progress + in_review 拆完 active），
// 前端可直接相加不做去重。

// ChildProgressCounts 单一维度（总体或单个阶段）的子任务真实状态计数。
type ChildProgressCounts struct {
	Total      int            `json:"total"`
	Done       int            `json:"done"`
	InProgress int            `json:"in_progress"`
	InReview   int            `json:"in_review"`
	Blocked    int            `json:"blocked"`
	Todo       int            `json:"todo"`
	Cancelled  int            `json:"cancelled"`
	ByStatus   map[string]int `json:"by_status"`
}

// ChildStageProgress 一个阶段（批次）的子任务聚合。Stage=wave：拆分子任务
// 为批次 1..N，任务同步镜像子任务为其上游 stage；0 = 无阶段组（排在编号
// 阶段之后，与项目任务分组视图同序）。
type ChildStageProgress struct {
	Stage int `json:"stage"`
	ChildProgressCounts
}

// ChildProgress 父任务子任务真实进度整体。ActiveStage 为当前活跃阶段：
// 最小编号阶段中仍存在非终态（非 completed/cancelled）子任务者；全部完结
// 或只有无阶段子任务 → null（阶段屏障串行推进，前序阶段未完结时后序
// 阶段的 queued 不改写活跃阶段）。Stages 无子任务时为空数组（非 null）。
type ChildProgress struct {
	DeliveryID  string `json:"delivery_id"`
	ActiveStage *int   `json:"active_stage"`
	ChildProgressCounts
	Stages []ChildStageProgress `json:"stages"`
}

// AssembleChildProgress 装配父任务的子任务真实进度聚合（纯函数、只读）。
// children 为该父的全部子任务（通常来自 ListChildDeliveries，已按 wave、
// 创建时间升序）；分组顺序编号阶段升序、无阶段（0）垫底。
func AssembleChildProgress(parentID string, children []Delivery) ChildProgress {
	out := ChildProgress{
		DeliveryID:          parentID,
		ActiveStage:         nil,
		ChildProgressCounts: emptyChildProgressCounts(),
		Stages:              []ChildStageProgress{},
	}

	// 先按阶段分桶（桶内保持入参顺序），再逐桶装配。
	buckets := make(map[int][]Delivery)
	for _, c := range children {
		buckets[c.Wave] = append(buckets[c.Wave], c)
		countChildStatus(&out.ChildProgressCounts, c)
	}
	waves := make([]int, 0, len(buckets))
	for w := range buckets {
		waves = append(waves, w)
	}
	// 编号阶段升序；无阶段（wave 0）垫底——与 buildTaskGroups 同序。
	slices.SortFunc(waves, func(a, b int) int {
		switch {
		case a == 0 && b != 0:
			return 1
		case a != 0 && b == 0:
			return -1
		default:
			return a - b
		}
	})
	for _, w := range waves {
		group := ChildStageProgress{Stage: w, ChildProgressCounts: emptyChildProgressCounts()}
		for _, c := range buckets[w] {
			countChildStatus(&group.ChildProgressCounts, c)
		}
		out.Stages = append(out.Stages, group)
	}

	// 当前活跃阶段：最小编号、仍存在非终态子任务的阶段（wave<=0 不参与，
	// 与引擎批次调度跳过无阶段子任务的口径一致）。
	for _, w := range waves {
		if w <= 0 {
			continue
		}
		for _, c := range buckets[w] {
			if !childTerminal(c) {
				active := w
				out.ActiveStage = &active
				break
			}
		}
		if out.ActiveStage != nil {
			break
		}
	}
	return out
}

// emptyChildProgressCounts 零计数（ByStatus 恒含五键，无行为 0）。
func emptyChildProgressCounts() ChildProgressCounts {
	return ChildProgressCounts{ByStatus: normalizeStatusCounts(nil)}
}

// childTerminal 子任务是否已到终态（completed / cancelled）。
func childTerminal(d Delivery) bool {
	return d.Status == "completed" || d.Status == "cancelled"
}

// countChildStatus 把一条子任务计入某维度的总数、原始状态与六类归并。
// 未知状态只计入 Total，不进 ByStatus 桶、不进任何归并计数——与
// WorkspaceStats 的 normalizeStatusCounts 口径一致。
func countChildStatus(c *ChildProgressCounts, d Delivery) {
	c.Total++
	switch d.Status {
	case "completed":
		c.ByStatus["completed"]++
		c.Done++
	case "cancelled":
		c.ByStatus["cancelled"]++
		c.Cancelled++
	case "blocked":
		c.ByStatus["blocked"]++
		c.Blocked++
	case "queued":
		c.ByStatus["queued"]++
		c.Todo++
	case "active":
		c.ByStatus["active"]++
		if d.PendingGate != "" {
			c.InReview++
		} else {
			c.InProgress++
		}
	}
}
