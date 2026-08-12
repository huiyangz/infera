package github

import (
	"context"

	gogithub "github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// NewClient 用 PAT 构造 go-github client。
func NewClient(token string) *gogithub.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return gogithub.NewClient(oauth2.NewClient(context.Background(), ts))
}
