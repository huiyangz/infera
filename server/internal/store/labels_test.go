package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// checkLabels 验证标签存储面（INFERA-218 T01 冻结的契约，内存/pg 共用断言）：
// 标签库创建/幂等 upsert（重复导入不产生重复行、名称颜色一致）+ 交付挂/摘标。
func checkLabels(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	proj := &Project{Name: "demo", DefaultBranch: "main"}
	require.NoError(t, s.CreateProject(ctx, proj))
	d := &Delivery{ProjectID: proj.ID, Title: "需求A", Status: "active"}
	require.NoError(t, s.CreateDelivery(ctx, d))

	// --- 创建：本地标签（无外部 ID）回填 ID 与时间戳；name+color roundtrip。 ---
	local := &Label{Name: "情报", Color: "#3b82f6"}
	require.NoError(t, s.CreateLabel(ctx, local))
	require.NotEmpty(t, local.ID)
	require.False(t, local.CreatedAt.IsZero())

	// --- 幂等 upsert：首次 = 插入并回填内部 ID。 ---
	l1 := &Label{Name: "auto", Color: "#22c55e", ExternalLabelID: "m-label-1"}
	require.NoError(t, s.UpsertLabelByExternalID(ctx, l1))
	require.NotEmpty(t, l1.ID, "插入后回填内部 ID")

	// --- 幂等 upsert：同外部 ID 重放 → 行数不变、ID 稳定、名称颜色一致。 ---
	l2 := &Label{Name: "auto", Color: "#22c55e", ExternalLabelID: "m-label-1"}
	require.NoError(t, s.UpsertLabelByExternalID(ctx, l2))
	require.Equal(t, l1.ID, l2.ID, "同外部标签 ID 命中同一行")
	labels, err := s.ListLabels(ctx)
	require.NoError(t, err)
	require.Len(t, labels, 2, "重复导入不产生重复标签")

	// --- 幂等 upsert：上游改名/换色 → 覆盖为最新值（不另起新行）。 ---
	l3 := &Label{Name: "自动化", Color: "#a855f7", ExternalLabelID: "m-label-1"}
	require.NoError(t, s.UpsertLabelByExternalID(ctx, l3))
	require.Equal(t, l1.ID, l3.ID)
	labels, err = s.ListLabels(ctx)
	require.NoError(t, err)
	require.Len(t, labels, 2)
	for _, l := range labels {
		if l.ID == l1.ID {
			require.Equal(t, "自动化", l.Name)
			require.Equal(t, "#a855f7", l.Color, "颜色存上游 hex 原值")
		}
	}

	// --- upsert 入参校验：空外部 ID → ErrInvalid。 ---
	require.ErrorIs(t, s.UpsertLabelByExternalID(ctx, &Label{Name: "x"}), ErrInvalid)

	// --- 创建冲突：外部 ID 已被占用 → ErrConflict（不静默产生第二行）。 ---
	require.ErrorIs(t, s.CreateLabel(ctx, &Label{Name: "dup", ExternalLabelID: "m-label-1"}), ErrConflict)

	// --- 列表按 name 升序。 ---
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		names = append(names, l.Name)
	}
	require.Equal(t, []string{"情报", "自动化"}, names)

	// --- 挂标：挂上后交付可见；重复挂 = 幂等（不产生重复行）。 ---
	require.NoError(t, s.AttachLabel(ctx, d.ID, local.ID))
	require.NoError(t, s.AttachLabel(ctx, d.ID, l1.ID))
	require.NoError(t, s.AttachLabel(ctx, d.ID, l1.ID), "重复挂同一标签幂等")
	got, err := s.ListDeliveryLabels(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, got, 2, "重复挂标不产生重复关联")
	require.Equal(t, "情报", got[0].Name)
	require.Equal(t, "#3b82f6", got[0].Color)
	require.Equal(t, "自动化", got[1].Name)

	// --- 批量取：任务列表一次装配多个交付的标签（免 N+1）。 ---
	d2 := &Delivery{ProjectID: proj.ID, Title: "需求B", Status: "active"}
	require.NoError(t, s.CreateDelivery(ctx, d2))
	require.NoError(t, s.AttachLabel(ctx, d2.ID, local.ID))
	byID, err := s.LabelsByDeliveryID(ctx, []string{d.ID, d2.ID})
	require.NoError(t, err)
	require.Len(t, byID[d.ID], 2)
	require.Len(t, byID[d2.ID], 1)

	// --- 摘标：摘掉后不再返回；再摘 → ErrNotFound。 ---
	require.NoError(t, s.DetachLabel(ctx, d.ID, l1.ID))
	got, err = s.ListDeliveryLabels(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "情报", got[0].Name)
	require.ErrorIs(t, s.DetachLabel(ctx, d.ID, l1.ID), ErrNotFound, "摘不存在的关联报 not found")

	// --- 挂标引用校验：交付/标签不存在 → ErrNotFound。 ---
	require.ErrorIs(t, s.AttachLabel(ctx, "00000000-0000-0000-0000-000000000000", local.ID), ErrNotFound)
	require.ErrorIs(t, s.AttachLabel(ctx, d.ID, "00000000-0000-0000-0000-000000000000"), ErrNotFound)

	// --- 交付侧挂在多个交付上互不影响。 ---
	got2, err := s.ListDeliveryLabels(ctx, d2.ID)
	require.NoError(t, err)
	require.Len(t, got2, 1, "摘另一交付的标签不影响本交付")
}

func TestMemoryLabels(t *testing.T) { checkLabels(t, NewMemory()) }

func TestPgLabels(t *testing.T) { checkLabels(t, testPool(t)) }
