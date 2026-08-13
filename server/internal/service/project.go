package service

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/pkg/db/generated"
)

// RepoCloner 抽象“克隆仓库到本地”（生产用 github.RepoCloner，测试用 fake）。
type RepoCloner interface {
	Clone(ctx context.Context, repoURL, dest string) error
}

type ProjectService struct {
	q      *generated.Queries
	cloner RepoCloner
}

func NewProject(pool *pgxpool.Pool, cloner RepoCloner) *ProjectService {
	return &ProjectService{q: generated.New(pool), cloner: cloner}
}

type CreateProjectInput struct {
	Name          string
	RepoURL       string
	DefaultBranch string
}

// Create 建项目；repo_url 非空时先试 clone 校验可达 + 有权限。
func (s *ProjectService) Create(ctx context.Context, in CreateProjectInput) (generated.Project, error) {
	if in.Name == "" {
		return generated.Project{}, fmt.Errorf("name is required")
	}
	branch := in.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	if in.RepoURL != "" {
		tmp, err := os.MkdirTemp("", "infera-clone-*")
		if err != nil {
			return generated.Project{}, fmt.Errorf("mktemp: %w", err)
		}
		defer os.RemoveAll(tmp)
		if err := s.cloner.Clone(ctx, in.RepoURL, tmp); err != nil {
			return generated.Project{}, fmt.Errorf("repo not accessible: %w", err)
		}
	}
	return s.q.CreateProject(ctx, generated.CreateProjectParams{
		Name: in.Name, RepoUrl: in.RepoURL, DefaultBranch: branch,
	})
}

func (s *ProjectService) List(ctx context.Context) ([]generated.Project, error) {
	return s.q.ListProjects(ctx)
}

func (s *ProjectService) Get(ctx context.Context, id pgtype.UUID) (generated.Project, error) {
	return s.q.GetProject(ctx, id)
}

func (s *ProjectService) Update(ctx context.Context, id pgtype.UUID, in CreateProjectInput) (generated.Project, error) {
	branch := in.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	return s.q.UpdateProject(ctx, generated.UpdateProjectParams{
		ID: id, Name: in.Name, RepoUrl: in.RepoURL, DefaultBranch: branch,
	})
}

func (s *ProjectService) Delete(ctx context.Context, id pgtype.UUID) error {
	return s.q.DeleteProject(ctx, id)
}
