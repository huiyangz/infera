package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDiffArtifactCapped：超大 diff 落库前按字节上限截断并尾缀标记——
// 防单条 artifact 撑爆存储/时间线；未超限的原样保存。
func TestDiffArtifactCapped(t *testing.T) {
	e, st, _, _ := newEnv(t, passTR{})
	fp := &fakePersister{diff: strings.Repeat("x", maxDiffBytes*2)}
	e.WithPersister(fp)
	d := driveToCodeReview(t, e, st)

	a := artifactByKind(t, st, d.ID, "diff")
	require.LessOrEqual(t, len(a.Content), maxDiffBytes+256, "超限 diff 必须截断（上限 + 截断标记）")
	require.Contains(t, a.Content, "截断", "截断必须带标记")

	// 未超限：原样保存（无标记）。
	e2, st2, _, _ := newEnv(t, passTR{})
	e2.WithPersister(&fakePersister{})
	d2 := driveToCodeReview(t, e2, st2)
	require.Equal(t, "diff --git a/hello.txt b/hello.txt", artifactByKind(t, st2, d2.ID, "diff").Content)
}
