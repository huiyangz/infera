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

type Manager struct {
	root      string
	g         *git.Git
	retention time.Duration
	mu        sync.Mutex
	dirs      map[string]string // deliveryID -> workdir path
	bases     map[string]string // deliveryID -> base commit
}

// New 建管理器；g 必须由调用方注入（带 GITHUB_TOKEN 的实例——token 不在本包读取，
// main 建一次与 LsRemote/push 共享；测试传 git.New()）。
func New(root string, g *git.Git, retention time.Duration) *Manager {
	return &Manager{
		root: root, g: g, retention: retention,
		dirs: map[string]string{}, bases: map[string]string{},
	}
}

// Acquire 保证 delivery 的 workdir 就绪并返回 (dir, baseCommit)。
// 有仓库则 clone（幂等——已存在则复用）；绿地项目只建目录。
// clone 失败时清理半成品并返回错误（可重试）。
func (m *Manager) Acquire(ctx context.Context, deliveryID, repoURL, branch string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dir, ok := m.dirs[deliveryID]; ok {
		return dir, m.bases[deliveryID], nil
	}
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
	m.dirs[deliveryID] = dir
	m.bases[deliveryID] = base
	return dir, base, nil
}

// Path 返回 delivery 的 workdir 路径（不保证存在）。
func (m *Manager) Path(deliveryID string) string {
	return filepath.Join(m.root, deliveryID)
}

// Release 按保留期延迟清理；retention<=0 立即清理。
func (m *Manager) Release(deliveryID string) {
	m.mu.Lock()
	dir := m.dirs[deliveryID]
	delete(m.dirs, deliveryID)
	delete(m.bases, deliveryID)
	m.mu.Unlock()
	if dir == "" {
		return
	}
	if m.retention <= 0 {
		_ = os.RemoveAll(dir)
		return
	}
	go func() {
		time.Sleep(m.retention)
		_ = os.RemoveAll(dir)
	}()
}
