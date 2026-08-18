// R10 双道审查（code_review 门禁前置）的测试：解析容错、两道齐全挂门、
// 缺绑定 / 失败 blocked+事件写明缺失、local 占位跳过、任务清单逐项核验注入。
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// findingsRunner 按角色回放预置输出（审查角色输出含 infera-findings 块），
// 其余角色走 fakeRunner 同款默认输出；failAt 角色报错。
type findingsRunner struct {
	calls   []agent.Request
	outputs map[string]string
	failAt  string
}

func (f *findingsRunner) Run(_ context.Context, req agent.Request) (agent.Result, error) {
	f.calls = append(f.calls, req)
	if req.Role == f.failAt {
		return agent.Result{}, errors.New("review agent crashed")
	}
	if out, ok := f.outputs[req.Role]; ok {
		return agent.Result{Output: out}, nil
	}
	switch req.Role {
	case "spec":
		return agent.Result{Output: "# 规格正文"}, nil
	case "design":
		return agent.Result{Output: "# 设计正文"}, nil
	case "tasks":
		return agent.Result{Output: "```infera-tasks\n[{\"title\":\"任务A\",\"detail\":\"做 A\"},{\"title\":\"任务B\",\"detail\":\"做 B\"}]\n```"}, nil
	case "test_gen":
		return agent.Result{Output: "tests: a_test.go"}, nil
	case "code_gen":
		return agent.Result{Output: "改了 2 个文件"}, nil
	case "code_review":
		return agent.Result{Output: "review ok"}, nil
	}
	return agent.Result{Output: "ok: " + req.Role}, nil
}

func (f *findingsRunner) roles() []string {
	out := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		out = append(out, c.Role)
	}
	return out
}

// lastCallFor 某角色最近一次调用的 prompt。
func (f *findingsRunner) lastCallFor(role string) agent.Request {
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Role == role {
			return f.calls[i]
		}
	}
	return agent.Request{}
}

const scFindingsOutput = "逐项核验结论：\n```infera-findings\n[{\"task_index\":1,\"severity\":\"major\",\"message\":\"任务1缺少 Y\",\"evidence\":\"a.go:9\"},{\"task_index\":0,\"severity\":\"info\",\"message\":\"整体结构清晰\",\"evidence\":\"\"}]\n```"
const cqFindingsOutput = "质量核验结论：\n```infera-findings\n[{\"task_index\":0,\"severity\":\"minor\",\"message\":\"缺少空指针防护\",\"evidence\":\"b.go:17\"}]\n```"

// TestParseFindingsBlock：解析容错——无块/坏 JSON/非数组 → nil；
// 空消息过滤、未知 severity 归一 info、负 task_index 归一 0、空块数组 → 空清单。
func TestParseFindingsBlock(t *testing.T) {
	require.Nil(t, ParseFindingsBlock("没有任何块"))
	require.Nil(t, ParseFindingsBlock("```infera-findings\n{not json}\n```"))
	require.Nil(t, ParseFindingsBlock("```infera-findings\n{\"task_index\":1}\n```")) // 非数组

	got := ParseFindingsBlock("```infera-findings\n[{\"task_index\":2,\"severity\":\"blocker\",\"message\":\"  \",\"evidence\":\"x\"},{\"task_index\":-1,\"severity\":\"weird\",\"message\":\"意见\",\"evidence\":\" c.go:1 \"}]\n```")
	require.Equal(t, []store.Finding{
		{TaskIndex: 0, Severity: "info", Message: "意见", Evidence: "c.go:1"}, // 空消息条目被过滤，其余归一
	}, got)

	require.Equal(t, []store.Finding{}, ParseFindingsBlock("```infera-findings\n[]\n```")) // 空数组=无意见（非 nil）
}

// TestDualReviewsAtCodeReviewGate：两道齐全 → 各落 findings artifact（解析后的契约 JSON）、
// 发 review_findings 事件、门禁挂起；既有 code_review 预审 agent_output 保留。
func TestDualReviewsAtCodeReviewGate(t *testing.T) {
	st := store_Memory(t)
	ar := &findingsRunner{outputs: map[string]string{
		"spec_conformance": scFindingsOutput,
		"code_quality":     cqFindingsOutput,
	}}
	e := New(st, ar, &FakeWS{}, passTR{})
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.NoError(t, e.Continue(ctx, d.ID))

	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, StatusActive, got.Status)

	// 角色序：预审在先，两道审查随后。
	require.Equal(t, []string{"spec", "test_gen", "code_gen", "code_review", "spec_conformance", "code_quality"}, ar.roles())
	// 既有预审产出不受影响。
	require.Equal(t, "review ok", artifactByKind(t, st, d.ID, "agent_output").Content)

	var sc store.FindingsReport
	require.NoError(t, json.Unmarshal([]byte(artifactByKind(t, st, d.ID, store.KindSpecConformanceFindings).Content), &sc))
	require.Equal(t, "spec_conformance", sc.Review)
	require.False(t, sc.TaskBased, "small 路径无任务清单 → 按规格整体核验")
	require.Len(t, sc.Findings, 2)
	require.Equal(t, store.Finding{TaskIndex: 1, Severity: "major", Message: "任务1缺少 Y", Evidence: "a.go:9"}, sc.Findings[0])
	require.Contains(t, sc.Raw, "逐项核验结论")

	var cq store.FindingsReport
	require.NoError(t, json.Unmarshal([]byte(artifactByKind(t, st, d.ID, store.KindCodeQualityFindings).Content), &cq))
	require.Equal(t, "code_quality", cq.Review)
	require.Len(t, cq.Findings, 1)

	types := eventTypes(t, st, d.ID)
	require.Equal(t, 2, countEvents(t, st, d.ID, "review_findings"))
	require.Contains(t, types, "gate_pending")

	// 无任务清单时 spec_conformance prompt 不含清单注入段，但携带规格。
	scPrompt := ar.lastCallFor("spec_conformance").Prompt
	require.Contains(t, scPrompt, "# 规格正文")
	require.NotContains(t, scPrompt, "任务清单（逐项核验")
}

// TestSpecConformanceTaskBased：large 路径有 tasks artifact → spec_conformance
// prompt 注入编号任务清单，report.task_based=true。
func TestSpecConformanceTaskBased(t *testing.T) {
	st := store_Memory(t)
	ar := &findingsRunner{outputs: map[string]string{
		"spec_conformance": scFindingsOutput,
		"code_quality":     cqFindingsOutput,
	}}
	e := New(st, ar, &FakeWS{}, passTR{})
	ctx := context.Background()
	d := driveToTasksApproval(t, e, st)

	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → test_gen
	require.NoError(t, e.Continue(ctx, d.ID))                      // → code_review 门
	require.Equal(t, "code_review", get(t, st, d.ID).PendingGate)

	scPrompt := ar.lastCallFor("spec_conformance").Prompt
	require.Contains(t, scPrompt, "任务清单（逐项核验")
	require.Contains(t, scPrompt, "1. 任务A——做 A")
	require.Contains(t, scPrompt, "2. 任务B——做 B")

	var sc store.FindingsReport
	require.NoError(t, json.Unmarshal([]byte(artifactByKind(t, st, d.ID, store.KindSpecConformanceFindings).Content), &sc))
	require.True(t, sc.TaskBased)
}

// TestDualReviewMissingBindingBlocks：审查道缺绑定 → stage_failed 写明缺哪道 + blocked，不挂门。
func TestDualReviewMissingBindingBlocks(t *testing.T) {
	st := store_Memory(t)
	ar := &findingsRunner{}
	e := New(st, ar, &FakeWS{}, passTR{})
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if node == "spec_conformance" || node == "code_quality" {
			return nil, &orchestration.ErrIncompleteBindings{Missing: []string{"spec_conformance", "code_quality"}}
		}
		return nil, nil
	}
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	err := e.Continue(ctx, d.ID)
	require.Error(t, err)

	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.Empty(t, got.PendingGate, "缺审查道不应挂人工门")
	require.Contains(t, stageFailedPayload(t, st, d.ID), "spec_conformance")
	require.Contains(t, stageFailedPayload(t, st, d.ID), "code_quality")
}

// TestDualReviewAgentFailureBlocks：某道 agent 失败 → stage_failed 写明哪道 + blocked。
func TestDualReviewAgentFailureBlocks(t *testing.T) {
	st := store_Memory(t)
	ar := &findingsRunner{failAt: "code_quality"}
	e := New(st, ar, &FakeWS{}, passTR{})
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.Error(t, e.Continue(ctx, d.ID))

	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.Empty(t, got.PendingGate)
	require.Contains(t, stageFailedPayload(t, st, d.ID), "code_quality")
	// spec_conformance 已产出的报告保留（append-only），但门禁未挂起。
	require.NotNil(t, artifactByKind(t, st, d.ID, store.KindSpecConformanceFindings))
}

// TestDualReviewLocalRunnerSkips：某道绑定 local runner → 跳过该道（无 findings artifact、
// local_stage_pending 事件），另一道照常产出，门禁挂起。
func TestDualReviewLocalRunnerSkips(t *testing.T) {
	st := store_Memory(t)
	ar := &findingsRunner{outputs: map[string]string{"code_quality": cqFindingsOutput}}
	e := New(st, ar, &FakeWS{}, passTR{})
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if node == "spec_conformance" {
			return nil, orchestration.ErrLocalRunner
		}
		return nil, nil
	}
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{}))
	require.NoError(t, e.Continue(ctx, d.ID))

	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.NotNil(t, artifactByKind(t, st, d.ID, store.KindCodeQualityFindings))
	arts, err := st.ListArtifacts(ctx, d.ID)
	require.NoError(t, err)
	for _, a := range arts {
		require.NotEqual(t, store.KindSpecConformanceFindings, a.Kind, "local 占位道不应产出 findings")
	}
	types := eventTypes(t, st, d.ID)
	require.Contains(t, types, "local_stage_pending")
}

// stageFailedPayload 拼出全部 stage_failed 事件 payload（含 error 字段）。
func stageFailedPayload(t *testing.T, st *store.Memory, deliveryID string) string {
	t.Helper()
	evs, err := st.ListEvents(context.Background(), deliveryID)
	require.NoError(t, err)
	out := ""
	for _, ev := range evs {
		if ev.EventType == "stage_failed" {
			out += string(ev.Payload)
		}
	}
	return out
}
