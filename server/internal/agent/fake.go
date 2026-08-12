package agent

import (
	"context"
	"fmt"
	"sync"
)

// FakeBackend 按 role 返回预设的产出，测试用。
type FakeBackend struct {
	mu    sync.Mutex
	stubs map[Role]string
	calls []ExecInput
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{stubs: map[Role]string{}}
}

// Stub 设定某 role 的返回产出。
func (b *FakeBackend) Stub(role Role, output string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stubs[role] = output
}

func (b *FakeBackend) Execute(ctx context.Context, in ExecInput) (ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, in)
	out, ok := b.stubs[in.Role]
	if !ok {
		out = fmt.Sprintf("fake output for role %s", in.Role)
	}
	return ExecResult{SessionID: "fake-session", Output: out}, nil
}

// Calls 返回收到的执行请求（断言用）。
func (b *FakeBackend) Calls() []ExecInput {
	b.mu.Lock()
	defer b.mu.Unlock()
	cp := make([]ExecInput, len(b.calls))
	copy(cp, b.calls)
	return cp
}
