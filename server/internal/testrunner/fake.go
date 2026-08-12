package testrunner

import "context"

// FakeRunner 按构造参数返回固定 pass/fail，测试用。
type FakeRunner struct{ pass bool }

func NewFakeRunner(pass bool) *FakeRunner { return &FakeRunner{pass: pass} }

func (f *FakeRunner) Run(ctx context.Context, workdir string) (Result, error) {
	if f.pass {
		return Result{Pass: true, Detail: "fake: all passed"}, nil
	}
	return Result{Pass: false, Detail: "fake: 2 cases failed"}, nil
}
