package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
)

// TestReleaseWorksAfterRestart：Release 不得依赖内存注册表——进程重启后
// 新 Manager 的 dirs 为空，对已存在 workdir 的 Release 仍必须清掉磁盘目录
// （R13：曾只查 map → 重启后 Release 空转，workdir 永久泄漏）。
func TestReleaseWorksAfterRestart(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	// 第一个"进程"：绿地 delivery 拿到 workdir。
	ws1 := New(root, git.New(), time.Hour)
	dir, _, err := ws1.Acquire(ctx, "d-restart", "", "")
	require.NoError(t, err)
	require.DirExists(t, dir)

	// 重启 = 同 root 的新 Manager（注册表丢失）。
	ws2 := New(root, git.New(), 0) // retention<=0 立即清理
	ws2.Release("d-restart")
	require.NoDirExists(t, dir, "重启后 Release 仍须清理磁盘 workdir")
	require.NoDirExists(t, filepath.Join(root, "d-restart"))
}

// TestReleaseDelayedAfterRestart：retention>0 时延迟清理按 root+id 现算路径，
// 注册表丢失（重启）不妨碍计时器最终删掉目录。
func TestReleaseDelayedAfterRestart(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	ws1 := New(root, git.New(), time.Hour)
	dir, _, err := ws1.Acquire(ctx, "d-delayed", "", "")
	require.NoError(t, err)

	ws2 := New(root, git.New(), 30*time.Millisecond)
	ws2.Release("d-delayed")
	require.DirExists(t, dir, "保留期内不清理")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return // 已清理
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("延迟清理到期后 workdir 仍未删除（泄漏）")
}

// TestReleaseReacquireWithinRetention：retention 窗口内同 ID re-Acquire
// （重启恢复/重试路径）会重新注册新目录——睡醒的延迟清理 goroutine 不得
// 不查注册表就 RemoveAll 活 workdir。
func TestReleaseReacquireWithinRetention(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	retention := 50 * time.Millisecond
	ws := New(root, git.New(), retention)

	dir1, _, err := ws.Acquire(ctx, "d-reacquire", "", "")
	require.NoError(t, err)
	ws.Release("d-reacquire")

	// 窗口内同 ID 重新获取：新目录重新注册（磁盘上还是同一路径）。
	dir2, _, err := ws.Acquire(ctx, "d-reacquire", "", "")
	require.NoError(t, err)
	require.Equal(t, dir1, dir2)
	require.DirExists(t, dir2)

	// 越过 retention（旧计时器已醒）并留缓冲：重新注册的活 workdir 必须仍在。
	time.Sleep(retention + 300*time.Millisecond)
	require.DirExists(t, dir2, "窗口内 re-Acquire 的 workdir 不得被旧清理计时器删掉")
}

// TestSweepOrphans：清扫孤儿 workdir——进程重启丢注册表后，上次遗留的
// 超过保留期且无人认领的目录回收；在途（注册表内）、keep 认领、保留期内
// 的目录绝不动。
func TestSweepOrphans(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	retention := time.Hour
	ws := New(root, git.New(), retention)

	// 在途：注册表内。
	inflight, _, err := ws.Acquire(ctx, "d-inflight", "", "")
	require.NoError(t, err)

	// 孤儿（上个进程遗留）：目录在、注册表无、mtime 已超保留期。
	orphanOld := filepath.Join(root, "d-orphan-old")
	require.NoError(t, os.MkdirAll(orphanOld, 0o755))
	old := time.Now().Add(-2 * retention)
	require.NoError(t, os.Chtimes(orphanOld, old, old))

	// 新孤儿：目录在、注册表无、但 mtime 在保留期内（延迟清理窗口内）。
	orphanFresh := filepath.Join(root, "d-orphan-fresh")
	require.NoError(t, os.MkdirAll(orphanFresh, 0o755))

	// keep 认领（如仍 active/blocked 的交付）的过期目录：绝不动。
	kept := filepath.Join(root, "d-kept")
	require.NoError(t, os.MkdirAll(kept, 0o755))
	require.NoError(t, os.Chtimes(kept, old, old))

	ws.Sweep(func(id string) bool { return id == "d-kept" })

	require.DirExists(t, inflight)
	require.DirExists(t, orphanFresh)
	require.DirExists(t, kept)
	require.NoDirExists(t, orphanOld, "无人认领且超保留期的孤儿目录应被回收")
}
