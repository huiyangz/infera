package github

import (
	"context"
	"strings"

	gogithub "github.com/google/go-github/v62/github"
)

// PRService 用 go-github 创建 PR / 查合并状态。
type PRService struct {
	client *gogithub.Client
}

func NewPRService(client *gogithub.Client) *PRService { return &PRService{client: client} }

// ownerRepo 从 "owner/repo" 解析出 owner 与 repo。
func ownerRepo(s string) (string, string) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// Create 在 ownerRepo 上创建 head → base 的 PR。
func (s *PRService) Create(ctx context.Context, ownerRepoStr, head, base, title, body string) (*gogithub.PullRequest, error) {
	owner, repo := ownerRepo(ownerRepoStr)
	pr, _, err := s.client.PullRequests.Create(ctx, owner, repo, &gogithub.NewPullRequest{
		Title: gogithub.String(title), Head: gogithub.String(head), Base: gogithub.String(base), Body: gogithub.String(body),
	})
	if err != nil {
		return nil, err
	}
	return pr, nil
}

// IsMerged 查询 PR 是否已合并。
func (s *PRService) IsMerged(ctx context.Context, ownerRepoStr string, prNumber int) (bool, error) {
	owner, repo := ownerRepo(ownerRepoStr)
	merged, _, err := s.client.PullRequests.IsMerged(ctx, owner, repo, prNumber)
	return merged, err
}
