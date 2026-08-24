package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedDiscovery 铺「需求发现」数据面：两个项目、三个标签（情报/候选/其他），
// 五条交付——d1 情报（挖掘产出待分析）、d2 情报+候选（已晋级候选）、
// d3 候选（另一项目）、d4 挂无关标签、d5 无标签。创建顺序 d1→d5，
// updated_at 随创建时间递增（降序断言依据）。
func seedDiscovery(t *testing.T, st Store) (d1, d2, d3, d4, d5 Delivery, labelIDs map[string]string) {
	t.Helper()
	ctx := context.Background()
	pa := &Project{Name: "项目甲"}
	require.NoError(t, st.CreateProject(ctx, pa))
	pb := &Project{Name: "项目乙"}
	require.NoError(t, st.CreateProject(ctx, pb))

	labelIDs = map[string]string{}
	for _, name := range []string{"情报", "候选", "其他"} {
		l := &Label{Name: name, Color: "#22c55e"}
		require.NoError(t, st.CreateLabel(ctx, l))
		labelIDs[name] = l.ID
	}
	attach := func(d *Delivery, names ...string) {
		for _, n := range names {
			require.NoError(t, st.AttachLabel(ctx, d.ID, labelIDs[n]))
		}
	}

	d1 = Delivery{ProjectID: pa.ID, Title: "情报卡", Status: "queued", ExternalIssueKey: "INFERA-1"}
	require.NoError(t, st.CreateDelivery(ctx, &d1))
	time.Sleep(2 * time.Millisecond) // updated_at 严格递增，排序可判
	d2 = Delivery{ProjectID: pa.ID, Title: "已分析情报", Status: "queued", ExternalIssueKey: "INFERA-2"}
	require.NoError(t, st.CreateDelivery(ctx, &d2))
	time.Sleep(2 * time.Millisecond)
	d3 = Delivery{ProjectID: pb.ID, Title: "候选卡", Status: "backlog", ExternalIssueKey: "INFERA-3"}
	require.NoError(t, st.CreateDelivery(ctx, &d3))
	time.Sleep(2 * time.Millisecond)
	d4 = Delivery{ProjectID: pa.ID, Title: "普通卡", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, &d4))
	time.Sleep(2 * time.Millisecond)
	d5 = Delivery{ProjectID: pa.ID, Title: "裸卡", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, &d5))

	attach(&d1, "情报")
	attach(&d2, "情报", "候选")
	attach(&d3, "候选")
	attach(&d4, "其他")
	return d1, d2, d3, d4, d5, labelIDs
}

func ids(ds []Delivery) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}

// TestMemoryListDeliveriesByLabelNames：任一标签名命中即返回（OR）；
// 同一交付挂多个命中标签只出现一次（去重）；跨项目取回；按 updated_at
// 降序；无命中与空入参都返回空切片（非 nil 语义由 len 断言覆盖）。
func TestMemoryListDeliveriesByLabelNames(t *testing.T) {
	st := NewMemory()
	d1, d2, d3, d4, _, _ := seedDiscovery(t, st)
	ctx := context.Background()

	got, err := st.ListDeliveriesByLabelNames(ctx, []string{"情报"})
	require.NoError(t, err)
	require.Equal(t, []string{d2.ID, d1.ID}, ids(got), "情报命中 d1/d2，updated_at 降序")

	got, err = st.ListDeliveriesByLabelNames(ctx, []string{"候选"})
	require.NoError(t, err)
	require.Equal(t, []string{d3.ID, d2.ID}, ids(got), "候选命中 d2/d3，跨项目")

	got, err = st.ListDeliveriesByLabelNames(ctx, []string{"情报", "候选"})
	require.NoError(t, err)
	require.Equal(t, []string{d3.ID, d2.ID, d1.ID}, ids(got), "双标签并集，d2 只出现一次")

	got, err = st.ListDeliveriesByLabelNames(ctx, []string{"不存在"})
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = st.ListDeliveriesByLabelNames(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, got, "空入参 = 无过滤条件命中，返回空")

	// 返回行是完整 Delivery 模型（复用现有任务查询模型，非平行投影）。
	got, err = st.ListDeliveriesByLabelNames(ctx, []string{"情报"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	row := got[1] // d1：updated_at 降序的末位
	require.Equal(t, d1.Title, row.Title)
	require.Equal(t, d1.ProjectID, row.ProjectID)
	require.Equal(t, d1.ExternalIssueKey, row.ExternalIssueKey)
	require.False(t, row.CreatedAt.IsZero())
	require.False(t, row.UpdatedAt.IsZero())
	_ = d4
}

// TestPgListDeliveriesByLabelNames：Pg 实现与 Memory 同语义（集成测试，
// TEST_DATABASE_URL 未设置时跳过）——并集、去重、跨项目、updated_at 降序。
func TestPgListDeliveriesByLabelNames(t *testing.T) {
	p := testPool(t)
	d1, d2, d3, _, _, _ := seedDiscovery(t, p)
	ctx := context.Background()

	got, err := p.ListDeliveriesByLabelNames(ctx, []string{"情报", "候选"})
	require.NoError(t, err)
	require.Equal(t, []string{d3.ID, d2.ID, d1.ID}, ids(got), "并集去重，updated_at 降序")

	got, err = p.ListDeliveriesByLabelNames(ctx, []string{"候选"})
	require.NoError(t, err)
	require.Equal(t, []string{d3.ID, d2.ID}, ids(got))

	got, err = p.ListDeliveriesByLabelNames(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}
