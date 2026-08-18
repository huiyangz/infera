// Package workspace 管理 delivery 的 workdir 生命周期：
// Acquire（intake 前）→ 全程共享 → Release（终态后延迟清理）。
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tokfinity/infera/internal/git"
)

// cloner Manager 对 git 的最小依赖（便于注入慢/失败 clone 的测试替身）。
type cloner interface {
	Clone(ctx context.Context, rawURL, branch, dir string) error
	Head(ctx context.Context, dir string) (string, error)
}

// *git.Git 天然满足 cloner（生产装配注入的就是它）。
var _ cloner = (*git.Git)(nil)

type Manager struct {
	root      string
	g         cloner
	retention time.Duration
	// mu 只保护 dirs/bases 两个 map——绝不覆盖 clone 等慢操作：
	// 一个 delivery 的网络 clone 不能阻塞所有其它交付的 workspace 获取。
	mu    sync.Mutex
	dirs  map[string]string // deliveryID -> workdir path
	bases map[string]string // deliveryID -> base commit
	// locks per-delivery 互斥（clone/registry 写入串行化；sync.Map 惰性创建）。
	locks sync.Map // deliveryID -> *sync.Mutex
}

// New 建管理器；g 必须由调用方注入（带 GITHUB_TOKEN 的实例——token 不在本包读取，
// main 建一次与 LsRemote/push 共享；测试传 git.New() 或替身）。
func New(root string, g cloner, retention time.Duration) *Manager {
	return &Manager{
		root: root, g: g, retention: retention,
		dirs: map[string]string{}, bases: map[string]string{},
	}
}

// lockDelivery 取 per-delivery 互斥锁（惰性创建），返回解锁函数。
// 锁序约定：先 per-delivery 锁、再 mu；反序路径不存在（mu 持有期间不做慢操作、
// 不取 per-delivery 锁），无死锁环。
func (m *Manager) lockDelivery(deliveryID string) func() {
	v, _ := m.locks.LoadOrStore(deliveryID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Acquire 保证 delivery 的 workdir 就绪并返回 (dir, baseCommit)。
// 有仓库则 clone（幂等——已存在则复用）；绿地项目只建目录。
// clone 失败时清理半成品并返回错误（可重试）。
// 慢操作（网络 clone）只持 per-delivery 锁：并发 delivery 互不阻塞；
// 同一 delivery 并发 Acquire 由 per-delivery 锁 + 双检收敛为一次 clone。
func (m *Manager) Acquire(ctx context.Context, deliveryID, repoURL, branch string) (string, string, error) {
	m.mu.Lock()
	if dir, ok := m.dirs[deliveryID]; ok {
		defer m.mu.Unlock()
		return dir, m.bases[deliveryID], nil
	}
	m.mu.Unlock()

	unlock := m.lockDelivery(deliveryID)
	defer unlock()

	// 双检：等 per-delivery 锁期间别人可能已建好。
	m.mu.Lock()
	if dir, ok := m.dirs[deliveryID]; ok {
		m.mu.Unlock()
		return dir, m.bases[deliveryID], nil
	}
	m.mu.Unlock()

	dir := filepath.Join(m.root, deliveryID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	base := ""
	if repoURL != "" {
		if err := m.g.Clone(ctx, repoURL, branch, dir); err != nil {
			_ = os.RemoveAll(dir)
			return "", "", err
		}
		head, err := m.g.Head(ctx, dir)
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", "", err
		}
		base = head
	}
	m.mu.Lock()
	m.dirs[deliveryID] = dir
	m.bases[deliveryID] = base
	m.mu.Unlock()
	return dir, base, nil
}

// Path 返回 delivery 的 workdir 路径（不保证存在）。
func (m *Manager) Path(deliveryID string) string {
	return filepath.Join(m.root, deliveryID)
}

// Release 按保留期延迟清理；retention<=0 立即清理。
// 取 per-delivery 锁：与在途的 Acquire 串行（清理不与 clone 竞争同一目录）。
// 清理路径由 root+id 现算，不查内存注册表——注册表随进程重启丢失，
// 重启后对已终态交付的 Release 仍必须清掉磁盘目录（否则 workdir 永久泄漏）。
func (m *Manager) Release(deliveryID string) {
	unlock := m.lockDelivery(deliveryID)
	defer unlock()
	m.mu.Lock()
	delete(m.dirs, deliveryID)
	delete(m.bases, deliveryID)
	m.mu.Unlock()
	dir := filepath.Join(m.root, deliveryID)
	if m.retention <= 0 {
		_ = os.RemoveAll(dir)
		return
	}
	go func() {
		time.Sleep(m.retention)
		_ = os.RemoveAll(dir)
	}()
}

// Sweep 清扫孤儿 workdir：注册表只在内存，进程重启即丢——上次进程遗留
// （已终态、延迟清理计时器随进程消失）的目录按「keep 认领 + 注册表在途 +
// 保留期内（按目录 mtime 代理最后活动时间）」之外回收。
// keep 应认领一切仍需保留 workdir 的交付（active/queued/blocked——blocked
// 可能是 persist 失败保留的救援现场，绝不能扫掉）；错误静默（root 不存在 =
// 无孤儿可扫）。
func (m *Manager) Sweep(keep func(deliveryID string) bool) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-m.retention)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		m.mu.Lock()
		_, inflight := m.dirs[id]
		m.mu.Unlock()
		if inflight || (keep != nil && keep(id)) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(m.root, id))
	}
}
