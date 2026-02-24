package terminal

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ListContainerShells returns a list of available shells in a container.
func ListContainerShells(containerID string) ([]string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	ctx := context.Background()
	shells := []string{"bash", "sh", "zsh", "ash", "dash"}
	available := []string{}
	for _, sh := range shells {
		execResp, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
			AttachStdout: true,
			AttachStderr: true,
			Cmd:          []string{"which", sh},
		})
		if err != nil {
			continue
		}

		attachResp, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
		if err != nil {
			continue
		}

		out, _ := io.ReadAll(attachResp.Reader)
		attachResp.Close()

		inspect, err := cli.ContainerExecInspect(ctx, execResp.ID)
		if err == nil && inspect.ExitCode == 0 && strings.TrimSpace(string(out)) != "" {
			available = append(available, sh)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no known shell found")
	}
	return available, nil
}
