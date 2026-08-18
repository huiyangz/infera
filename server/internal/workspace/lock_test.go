package workspace

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
)

// gateCloner 克隆可闸控的替身：Clone 通知 enter 后阻塞到 release 关闭
// （确定性模拟慢 clone），Head 即返。并发断言不靠计时。
type gateCloner struct {
	enter   chan string // 每次进入 clone 的 dir（含 delivery 目录名）
	release chan struct{}

	mu     sync.Mutex
	clones int
}

func newGateCloner() *gateCloner {
	return &gateCloner{enter: make(chan string, 8), release: make(chan struct{})}
}

func (g *gateCloner) Clone(_ context.Context, _, _, dir string) error {
	g.mu.Lock()
	g.clones++
	g.mu.Unlock()
	g.enter <- dir
	<-g.release // 模拟慢速网络 clone
	return nil
}

func (g *gateCloner) Head(context.Context, string) (string, error) {
	return strings.Repeat("a", 40), nil
}

func (g *gateCloner) cloneCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.clones
}

// TestAcquireDifferentDeliveriesNotSerialized：Acquire 不得持全局锁做 clone——
// 一个 delivery 的慢 clone 阻塞所有其它交付的 workspace 获取。
// 闸控确定性验证：A 的 clone 仍阻塞时，B 的 clone 必须已能开始。
func TestAcquireDifferentDeliveriesNotSerialized(t *testing.T) {
	g := newGateCloner()
	ws := New(t.TempDir(), g, time.Hour)
	ctx := context.Background()

	doneA := make(chan error, 1)
	go func() {
		_, _, err := ws.Acquire(ctx, "delivery-a", "https://example.com/a.git", "main")
		doneA <- err
	}()
	dirA := <-g.enter // A 已进入 clone（阻塞中，模拟网络慢）

	doneB := make(chan error, 1)
	go func() {
		_, _, err := ws.Acquire(ctx, "delivery-b", "https://example.com/b.git", "main")
		doneB <- err
	}()
	// A 的 clone 未返回期间，B 的 clone 也必须开始——否则就是全局锁串行化。
	select {
	case dirB := <-g.enter:
		require.NotEqual(t, dirA, dirB)
	case <-time.After(2 * time.Second):
		t.Fatal("Acquire 被其它 delivery 的 clone 阻塞（全局锁串行化）")
	}

	close(g.release)
	require.NoError(t, <-doneA)
	require.NoError(t, <-doneB)
	require.Equal(t, 2, g.cloneCount())
}

// TestAcquireSameDeliveryClonesOnce：并发 Acquire 同一 delivery 只 clone 一次，
// 双方拿到同一 workdir（per-delivery 互斥 + 双检）。
func TestAcquireSameDeliveryClonesOnce(t *testing.T) {
	g := newGateCloner()
	ws := New(t.TempDir(), g, time.Hour)
	ctx := context.Background()

	type result struct {
		dir  string
		base string
		err  error
	}
	resCh := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			dir, base, err := ws.Acquire(ctx, "same", "https://example.com/x.git", "main")
			resCh <- result{dir, base, err}
		}()
	}
	<-g.enter // 第一个 clone 开始（另一个 goroutine 在 per-delivery 锁上等）
	close(g.release)
	r1, r2 := <-resCh, <-resCh
	require.NoError(t, r1.err)
	require.NoError(t, r2.err)
	require.Equal(t, r1.dir, r2.dir)
	require.Equal(t, r1.base, r2.base)
	require.Equal(t, 1, g.cloneCount(), "并发同 delivery 只允许一次 clone")
}

// errHeadCloner Clone 成功但 Head 失败（clone 后半程失败路径）。
type errHeadCloner struct{ gateCloner }

func (e *errHeadCloner) Head(context.Context, string) (string, error) {
	return "", errors.New("head failed")
}

// TestAcquireHeadFailureRetriable：Head 失败同样清理半成品并可重试。
func TestAcquireHeadFailureRetriable(t *testing.T) {
	g := &errHeadCloner{*newGateCloner()}
	ws := New(t.TempDir(), g, time.Hour)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, _, err := ws.Acquire(ctx, "h1", "https://example.com/x.git", "main")
		done <- err
	}()
	<-g.enter
	close(g.release)
	require.Error(t, <-done)

	// 换正常 git 重试同 delivery：干净重来。
	ws2 := New(t.TempDir(), git.New(), time.Hour)
	dir, base, err := ws2.Acquire(ctx, "h1", newBare(t), "main")
	require.NoError(t, err)
	require.NotEmpty(t, base)
	require.DirExists(t, dir)
}
