package testrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCmd struct {
	out  string
	code int
}

func (f fakeCmd) RunCommand(ctx context.Context, cmd []string, workdir string) (string, int, error) {
	return f.out, f.code, nil
}

func TestRealRunnerPassOnZeroExit(t *testing.T) {
	r := NewRealRunner(fakeCmd{out: "ok\nPASS", code: 0}, "/work")
	res, err := r.Run(context.Background(), "/work")
	assert.NoError(t, err)
	assert.True(t, res.Pass)
}

func TestRealRunnerFailOnNonZeroExit(t *testing.T) {
	r := NewRealRunner(fakeCmd{out: "FAIL: pkg_test.go:12", code: 1}, "/work")
	res, _ := r.Run(context.Background(), "/work")
	assert.False(t, res.Pass)
	assert.Contains(t, res.Detail, "FAIL")
}
