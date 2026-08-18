// R10 双道审查（code_review 门禁前置）：规格符合性 + 代码质量两道独立 agent 审查，
// 各产出结构化 findings（```infera-findings fenced block → 解析 → JSON artifact），
// 结构化意见呈现给人工——人从「审代码」升级为「审审查意见」。
//
// 契约（与 store.Finding/FindingsReport 一并冻结）：
//   - 产出物存 artifact（kind=<道名>_findings，见 store.Kind*Findings 常量；stage=code_review），
//     不建独立 store 表——LatestArtifact(kind) 天然给出「最新一次审查生效」（打回重跑后再审
//     自动覆盖展示），append-only 保留全部历史，与 tasks artifact（R8）同一存储先例；
//     独立表需扩 Store 接口 + pg 迁移，无对应收益。
//   - 两道都产出才挂人工门：缺绑定 / agent 失败 → stage_failed（写明哪道）+ blocked；
//     绑定 local runner 的道跳过（本机交互 = 人工即审查员），门禁照常挂起。
//   - 意见只呈现不拦截：findings 不参与流转判定，批准/打回由人工决定。
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// findingsKinds 道名 → findings artifact kind（LatestArtifact 按 kind 取最新，两道各自独立）。
var findingsKinds = map[string]string{
	"spec_conformance": store.KindSpecConformanceFindings,
	"code_quality":     store.KindCodeQualityFindings,
}

// findingsRe 从审查输出里提取 ```inferna-findings fenced block（JSON 数组）。
var findingsRe = regexp.MustCompile("```infera-findings\\n([\\s\\S]*?)\\n```")

// ParseFindingsBlock 解析审查输出里的意见 fenced block；容错约定同 tasks：
// 无块 / 坏 JSON / 非数组 → nil（按无结构化意见处理，原始输出仍留档 report.Raw）。
// 空消息条目被过滤；未知 severity 归一为 info；负 task_index 归一为 0。
func ParseFindingsBlock(output string) []store.Finding {
	m := findingsRe.FindStringSubmatch(output)
	if m == nil {
		return nil
	}
	var raw []store.Finding
	if err := json.Unmarshal([]byte(m[1]), &raw); err != nil {
		return nil
	}
	out := make([]store.Finding, 0, len(raw))
	for _, f := range raw {
		if strings.TrimSpace(f.Message) == "" {
			continue
		}
		if f.TaskIndex < 0 {
			f.TaskIndex = 0
		}
		switch f.Severity {
		case "critical", "major", "minor", "info":
		default:
			f.Severity = "info"
		}
		f.Message = strings.TrimSpace(f.Message)
		f.Evidence = strings.TrimSpace(f.Evidence)
		out = append(out, f)
	}
	return out
}

// stepFindingsReviews 逐道跑门禁前置的结构化审查（node.FindingsReviews）。
// 任一道缺绑定 / 失败 → blocked（stage_failed 写明哪道）；全部产出（或 local 跳过）后返回，
// 门禁才挂起。
func (e *Engine) stepFindingsReviews(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun) error {
	for _, review := range node.FindingsReviews {
		if err := e.stepFindingsReview(ctx, d, node, run, review); err != nil {
			return err
		}
	}
	return nil
}

// stepFindingsReview 跑一道审查：解析执行器（local 占位 → 跳过该道）、组 prompt
// （规格符合性带任务清单逐项核验）、落 findings artifact、发 review_findings 事件。
// 缺绑定 / agent 失败同 agent 失败约定：stage_failed（写明哪道）+ blocked。
func (e *Engine) stepFindingsReview(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun, review string) error {
	ar, err := e.runnerFor(ctx, d, review)
	if err != nil {
		if !errors.Is(err, orchestration.ErrLocalRunner) {
			e.finishStageRun(ctx, run.ID, "failed")
			e.emit(ctx, d, node.Stage, "stage_failed", map[string]string{
				"error": fmt.Sprintf("门禁前置审查 %s 无法执行: %v", review, err),
			})
			return e.block(ctx, d, fmt.Errorf("stage %s review %s: %w", node.Stage, review, err))
		}
		// local 占位：该道跳过（本机交互 = 人工即审查员），门禁照常挂起。
		e.emitLocalPending(ctx, d, node.Stage, review)
		return nil
	}
	spec, err := e.latestSpec(ctx, d.ID)
	if err != nil {
		return err
	}
	prompt, taskBased, err := e.reviewPrompt(ctx, d, review, spec)
	if err != nil {
		return err
	}
	res, err := runAgent(ctx, ar, agent.Request{Role: review, Prompt: prompt, Workdir: e.ws.Path(d.ID)})
	if err != nil {
		e.finishStageRun(ctx, run.ID, "failed")
		e.emit(ctx, d, node.Stage, "stage_failed", map[string]string{
			"error": fmt.Sprintf("门禁前置审查 %s 失败: %v", review, err),
		})
		return e.block(ctx, d, fmt.Errorf("stage %s review %s: %w", node.Stage, review, err))
	}
	findings := ParseFindingsBlock(res.Output)
	if findings == nil {
		findings = []store.Finding{} // 畸形块容错为空意见，原始输出留档 Raw
	}
	report := store.FindingsReport{Review: review, TaskBased: taskBased, Findings: findings, Raw: res.Output}
	content, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      node.Stage,
		Kind:       findingsKinds[review],
		Content:    string(content),
	}); err != nil {
		return err
	}
	e.emit(ctx, d, node.Stage, "review_findings", map[string]any{
		"review": review, "count": len(findings), "task_based": taskBased,
	})
	return nil
}

// reviewPrompt 组装某道审查的 prompt：角色模板 + 规格符合性的任务清单注入段。
// 返回 (prompt, 是否按任务清单逐项核验)；无任务清单（small 路径 / 老数据 / 空清单）
// → 按规格整体核验，不注入清单段。
func (e *Engine) reviewPrompt(ctx context.Context, d *store.Delivery, review, spec string) (string, bool, error) {
	prompt := agent.BuildPrompt(review, d.Description, spec, "")
	if review != "spec_conformance" {
		return prompt, false, nil
	}
	tasks, err := e.deliveryTasks(ctx, d.ID)
	if err != nil {
		return "", false, err
	}
	if len(tasks) == 0 {
		return prompt, false, nil
	}
	var b strings.Builder
	b.WriteString(prompt)
	fmt.Fprintf(&b, "\n\n任务清单（逐项核验，task_index 为 1-based 序号，共 %d 项）：", len(tasks))
	for i, t := range tasks {
		fmt.Fprintf(&b, "\n%d. %s——%s", i+1, t.Title, t.Detail)
	}
	return b.String(), true, nil
}
