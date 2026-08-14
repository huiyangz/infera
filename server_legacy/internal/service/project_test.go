package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/dbtest"
)

type fakeCloner struct{ fail bool }

func (f fakeCloner) Clone(ctx context.Context, repoURL, dest string) error {
	if f.fail {
		return errFakeClone
	}
	return nil
}

var errFakeClone = assert.AnError

func TestCreateProjectOK(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "deliveries", "projects")

	svc := NewProject(pool, fakeCloner{})
	p, err := svc.Create(context.Background(), CreateProjectInput{Name: "web", RepoURL: "https://github.com/x/y.git", DefaultBranch: "main"})
	assert.NoError(t, err)
	assert.Equal(t, "web", p.Name)
	assert.Equal(t, "main", p.DefaultBranch)
}

func TestCreateProjectRejectsBadRepo(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "deliveries", "projects")

	svc := NewProject(pool, fakeCloner{fail: true})
	_, err := svc.Create(context.Background(), CreateProjectInput{Name: "web", RepoURL: "https://github.com/x/y.git"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repo")
}

func TestListAndGetProject(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "deliveries", "projects")

	svc := NewProject(pool, fakeCloner{})
	p, _ := svc.Create(context.Background(), CreateProjectInput{Name: "a"})

	got, err := svc.Get(context.Background(), p.ID)
	assert.NoError(t, err)
	assert.Equal(t, "a", got.Name)

	list, err := svc.List(context.Background())
	assert.NoError(t, err)
	assert.True(t, len(list) >= 1)
}
