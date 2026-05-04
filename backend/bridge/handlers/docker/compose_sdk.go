package docker

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type composeLineEmitter func(msgType, message string)

type composeMessageCollector struct {
	mu    sync.Mutex
	lines []string
}

func (c *composeMessageCollector) Emit(_ string, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	c.mu.Lock()
	c.lines = append(c.lines, message)
	c.mu.Unlock()
}

func (c *composeMessageCollector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

// runCompose executes a docker compose command, streaming output lines to the emitter.
func runCompose(ctx context.Context, projectName, configFile, workingDir string, emitter composeLineEmitter, args ...string) error {
	baseArgs := []string{"compose", "--project-name", projectName, "--file", configFile}
	baseArgs = append(baseArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", baseArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Merge stdout and stderr so we capture all output in order.
	cmd.Stdout = nil
	cmd.Stderr = nil
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into the same pipe

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start docker compose: %w", err)
	}

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if emitter != nil {
			msgType := "stdout"
			// docker compose prefixes warnings/errors
			lower := strings.ToLower(line)
			if strings.HasPrefix(lower, "error") || strings.Contains(lower, "failed") {
				msgType = "stderr"
			}
			emitter(msgType, line)
		}
	}

	return cmd.Wait()
}

func composeUpWithSDK(
	ctx context.Context,
	projectName, configFile, workingDir string,
	removeOrphans bool,
	emitter composeLineEmitter,
) error {
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	args := []string{"up", "-d"}
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, args...)
}

func composeDownWithSDK(
	ctx context.Context,
	projectName, configFile, workingDir string,
	removeOrphans bool,
	emitter composeLineEmitter,
) error {
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	args := []string{"down"}
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, args...)
}

func composeStopWithSDK(
	ctx context.Context,
	projectName, configFile, workingDir string,
	emitter composeLineEmitter,
) error {
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, "stop")
}

func composeValidateContentWithSDK(ctx context.Context, content string) error {
	f, err := os.CreateTemp("", "linuxio-compose-*.yml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", f.Name(), "config")
	cmd.Stdout = nil
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return nil
}
