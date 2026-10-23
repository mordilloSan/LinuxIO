package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/client"
)

// CheckDockerAvailability verifies that Docker is installed and accessible.
// It uses a short-lived throwaway client so that repeated capability polls
// (which the frontend runs every ~minute) do not reset the shared client's
// idle timer and prevent it from ever being released.
func CheckDockerAvailability(ctx context.Context) (bool, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return false, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		return false, fmt.Errorf("docker daemon not accessible: %w", err)
	}
	return true, nil
}
