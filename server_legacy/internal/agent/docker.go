package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerBackend 用本地 Docker daemon 起容器跑命令（Claude Code 或 go test 等）。
type DockerBackend struct {
	cli      *client.Client
	image    string   // 如 "infera-agent"
	apiKey   string   // ANTHROPIC_API_KEY
	extraEnv []string // 透传给容器：ANTHROPIC_BASE_URL / HTTPS_PROXY 等
}

func NewDockerBackend(image string) (*DockerBackend, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		// 也接受 ANTHROPIC_AUTH_TOKEN（兼容第三方/代理端点）
		if tok := os.Getenv("ANTHROPIC_AUTH_TOKEN"); tok != "" {
			apiKey = tok
		} else {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY (or ANTHROPIC_AUTH_TOKEN) not set")
		}
	}
	// 透传端点/代理（claude CLI 与 go test 都可能需要）
	var extra []string
	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "GOPROXY"} {
		if v := os.Getenv(k); v != "" {
			extra = append(extra, k+"="+v)
		}
	}
	return &DockerBackend{cli: cli, image: image, apiKey: apiKey, extraEnv: extra}, nil
}

// runContainer 起一个容器跑 cmd（完整命令，清空镜像的 claude entrypoint），
// 可选把 workdir 挂载到 /work，返回合并的 stdout（含 stderr 兜底）与退出码。
func (b *DockerBackend) runContainer(ctx context.Context, cmd []string, workdir string) (string, int, error) {
	var binds []string
	if workdir != "" {
		binds = []string{fmt.Sprintf("%s:/work", workdir)}
	}
	createResp, err := b.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      b.image,
			Cmd:        cmd,
			Entrypoint: []string{}, // 清空 claude entrypoint，让 cmd 是完整命令（Execute 跑 claude，RunCommand 跑 sh/go test）
			Env:        append([]string{"ANTHROPIC_API_KEY=" + b.apiKey}, b.extraEnv...),
			WorkingDir: "/work",
		},
		&container.HostConfig{Binds: binds, AutoRemove: false},
		nil, nil, "",
	)
	if err != nil {
		return "", -1, fmt.Errorf("container create: %w", err)
	}
	id := createResp.ID
	defer func() { _ = b.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}) }()

	if err := b.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return "", -1, fmt.Errorf("container start: %w", err)
	}

	statusCh, errCh := b.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", -1, fmt.Errorf("container wait: %w", err)
		}
	case <-ctx.Done():
		return "", -1, ctx.Err()
	case s := <-statusCh:
		// 非 TTY 容器日志是多路复用的，必须用 stdcopy 解复用。
		logs, _ := b.cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true})
		var stdout, stderr bytes.Buffer
		_, _ = stdcopy.StdCopy(&stdout, &stderr, logs)
		out := stdout.String()
		if out == "" {
			out = stderr.String() // 兜底：某些错误只进 stderr
		}
		return out, int(s.StatusCode), nil
	}
	return "", -1, fmt.Errorf("unknown container state")
}

// Execute 跑 Claude Code（headless）产出 Agent 输出。
func (b *DockerBackend) Execute(ctx context.Context, in ExecInput) (ExecResult, error) {
	out, code, err := b.runContainer(ctx, []string{"claude", "-p", in.Prompt, "--output-format", "text"}, in.Workdir)
	if err != nil {
		return ExecResult{}, err
	}
	if code != 0 {
		return ExecResult{Output: out}, fmt.Errorf("claude exited %d: %s", code, out)
	}
	return ExecResult{Output: out}, nil
}

// RunCommand 在 workdir 起容器跑任意命令（供 testrunner 等用）。
// 非零退出码不算 error（由调用方按 exitCode 判定）。
func (b *DockerBackend) RunCommand(ctx context.Context, cmd []string, workdir string) (stdout string, exitCode int, err error) {
	return b.runContainer(ctx, cmd, workdir)
}
