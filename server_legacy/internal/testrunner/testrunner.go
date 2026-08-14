package testrunner

import "context"

// Result 是一次测试运行的判定。
type Result struct {
	Pass   bool
	Detail string // 失败时的摘要（给 Coder Agent 当修复线索）
}

// Runner 抽象"跑测试并判定通过与否"。
// P3 用 FakeRunner；P4 用 RealRunner（在容器里跑 go test / jest）。
type Runner interface {
	Run(ctx context.Context, workdir string) (Result, error)
}
