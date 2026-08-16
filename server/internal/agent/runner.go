// Package agent 定义可替换的 agent 运行时。
// 契约：workdir 进、role+prompt 进、output 出。claude / pi / fake 皆可实现。
package agent

import (
	"context"
	"strings"
)

// Request 是引擎交给 runner 的最小契约：角色、组装好的 prompt、工作目录。
type Request struct {
	Role    string // spec | test_gen | code_gen | code_review
	Prompt  string
	Workdir string
	Inputs  map[string]string // 上游产物（预留，引擎拼 prompt 用）
}

// Result 是 runner 的输出契约（后续可扩展 diff/status 等字段）。
type Result struct{ Output string }

// Runner 是可替换的 agent 运行时接口。
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// Prompts 每个角色的指令模板。{description} {spec} 为占位符。
var Prompts = map[string]string{
	"spec":        "你是资深工程师。基于仓库现状，为以下需求撰写实现规格（中文，Markdown）：\n需求：{description}\n要求：列出改动文件、接口变化、验收标准。只输出规格正文。",
	"test_gen":    "你是测试工程师。依据以下规格，在仓库中编写测试用例（Go 项目用 _test.go）。\n规格：\n{spec}\n只输出新增/修改的文件清单与说明。",
	"code_gen":    "你是程序员。在当前仓库中实现以下需求，严格遵循规格。\n需求：{description}\n规格：\n{spec}\n实现完成后输出改动摘要。",
	"code_review": "你是代码审查员。审查当前仓库工作区中未提交的改动，对照规格评估正确性与质量，输出审查意见。\n规格：\n{spec}",
}

// BuildPrompt 组装角色 prompt；空值占位替换为（无）。
func BuildPrompt(role, description, spec string) string {
	return replace(replace(Prompts[role], "{description}", description), "{spec}", spec)
}

// replace 把模板占位符替换为实际值；空值渲染为（无）。
func replace(s, old, new string) string {
	if new == "" {
		new = "（无）"
	}
	return strings.ReplaceAll(s, old, new)
}
