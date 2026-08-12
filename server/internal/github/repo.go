package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// RepoCloner 用 git CLI 把仓库 clone 到本地目录。
type RepoCloner struct{ token string }

func NewRepoCloner(token string) RepoCloner { return RepoCloner{token: token} }

// Clone 把 repoURL clone 到 dest（浅克隆）。token 注入到 URL 用于私有仓库鉴权。
func (c RepoCloner) Clone(ctx context.Context, repoURL, dest string) error {
	authed := c.withToken(repoURL)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", authed, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %w: %s", err, string(out))
	}
	return nil
}

func (c RepoCloner) withToken(url string) string {
	if c.token == "" || !strings.HasPrefix(url, "https://") {
		return url
	}
	return strings.Replace(url, "https://", "https://"+c.token+"@", 1)
}
