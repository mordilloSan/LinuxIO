package terminal

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/moby/moby/client"
)

// ListContainerShells returns a list of available shells in a container.
func ListContainerShells(ctx context.Context, containerID string) ([]string, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	shells := []string{"bash", "sh", "zsh", "ash", "dash"}
	available := []string{}
	for _, sh := range shells {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		execResp, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
			AttachStdout: true,
			AttachStderr: true,
			Cmd:          []string{"which", sh},
		})
		if err != nil {
			continue
		}

		attachResp, err := cli.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
		if err != nil {
			continue
		}

		out, _ := io.ReadAll(attachResp.Reader)
		attachResp.Close()

		inspect, err := cli.ExecInspect(ctx, execResp.ID, client.ExecInspectOptions{})
		if err == nil && inspect.ExitCode == 0 && strings.TrimSpace(string(out)) != "" {
			available = append(available, sh)
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no known shell found")
	}
	return available, nil
}
