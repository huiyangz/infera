package agent

import (
	"bytes"
	"context"
	"sync/atomic"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// dockerCli 进程级共享 Docker 客户端：FromEnv + 版本协商只做一次，
// RunInContainer 各调用复用，不再 per-call 建连/关闭。
var dockerCli atomic.Value // *client.Client

// dockerClient 取共享客户端；未初始化时创建（首次失败重试一次）后缓存。
// 并发首次调用可能各建一次、Store 后写者胜——被覆盖的客户端只是多余对象，无副作用。
func dockerClient() (*client.Client, error) {
	if cli, ok := dockerCli.Load().(*client.Client); ok {
		return cli, nil
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, err
		}
	}
	dockerCli.Store(cli)
	return cli, nil
}

// RunInContainer 在一次性容器里执行命令，workdir 绑定挂载到 /work，返回合并输出。
// agent runner 与 testrunner 共用。
func RunInContainer(ctx context.Context, image string, cmd []string, workdir string) (string, error) {
	cli, err := dockerClient()
	if err != nil {
		return "", err
	}
	// cli 进程生命周期内共享，这里不 Close。

	cfg := &container.Config{Image: image, Cmd: cmd, WorkingDir: "/work"}
	hc := &container.HostConfig{
		Mounts: []mount.Mount{{Type: mount.TypeBind, Source: workdir, Target: "/work"}},
	}
	c, err := cli.ContainerCreate(ctx, cfg, hc, nil, nil, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = cli.ContainerRemove(context.Background(), c.ID, container.RemoveOptions{Force: true}) }()
	if err := cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
		return "", err
	}
	waitCh, errCh := cli.ContainerWait(ctx, c.ID, container.WaitConditionNotRunning)
	select {
	case <-waitCh:
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
	logs, err := cli.ContainerLogs(ctx, c.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", err
	}
	defer logs.Close()
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, logs); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// DockerRunner 容器化 agent：命令可配置（claude / pi / ...），prompt 走最后一个参数。
type DockerRunner struct {
	image string
	cmd   []string
}

func NewDocker(image string, cmd []string) *DockerRunner {
	return &DockerRunner{image: image, cmd: cmd}
}

func (d *DockerRunner) Run(ctx context.Context, req Request) (Result, error) {
	out, err := RunInContainer(ctx, d.image, append(append([]string{}, d.cmd...), req.Prompt), req.Workdir)
	return Result{Output: out}, err
}

var _ Runner = (*DockerRunner)(nil)
var _ Runner = (*LocalRunner)(nil)
