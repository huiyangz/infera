package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerBackend 用本地 Docker daemon 起容器跑 Claude Code。
type DockerBackend struct {
	cli    *client.Client
	image  string // 如 "infera-agent"
	apiKey string // ANTHROPIC_API_KEY
}

func NewDockerBackend(image string) (*DockerBackend, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	return &DockerBackend{cli: cli, image: image, apiKey: apiKey}, nil
}

func (b *DockerBackend) Execute(ctx context.Context, in ExecInput) (ExecResult, error) {
	// 用 prompt 作为 claude -p 的参数；system 指令已在 prompt 前缀里（由 ExecuteService 拼好）
	cmd := []string{"-p", in.Prompt, "--output-format", "text"}

	createResp, err := b.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      b.image,
			Cmd:        cmd,
			Env:        []string{"ANTHROPIC_API_KEY=" + b.apiKey},
			WorkingDir: "/work",
		},
		nil, nil, nil, "",
	)
	if err != nil {
		return ExecResult{}, fmt.Errorf("container create: %w", err)
	}
	containerID := createResp.ID
	defer func() { _ = b.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}) }()

	if err := b.cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return ExecResult{}, fmt.Errorf("container start: %w", err)
	}

	statusCh, errCh := b.cli.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return ExecResult{}, fmt.Errorf("container wait: %w", err)
		}
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	case <-statusCh:
	}

	// 非 TTY 容器的日志是多路复用的（8 字节头），必须用 stdcopy 解复用，
	// 否则 io.Copy 会把帧头字节混进输出。
	logs, err := b.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true, ShowStderr: true,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("container logs: %w", err)
	}
	defer logs.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logs); err != nil {
		return ExecResult{}, fmt.Errorf("read logs: %w", err)
	}
	out := stdout.String()
	if out == "" {
		out = stderr.String() // 兜底：某些错误只进 stderr
	}
	return ExecResult{SessionID: containerID, Output: out}, nil
}

// 确保 io 包被引用（保留给未来扩展，如流式读取）。
var _ io.Reader = (*bytes.Reader)(nil)
