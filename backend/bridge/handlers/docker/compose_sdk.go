package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// composeLineEmitter receives each line of compose output. For structured
// `--progress=json` events, progress is non-nil and message holds a humanized
// rendering of the same event; for plain text lines progress is nil.
type composeLineEmitter func(msgType, message string, progress *ComposeProgress)

type composeMessageCollector struct {
	mu    sync.Mutex
	lines []string
}

func (c *composeMessageCollector) Emit(_ string, message string, _ *ComposeProgress) {
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

// parseComposeProgress decodes a single `--progress=json` event line. It
// returns ok=false for any line that is not a JSON progress object (plain
// warnings, errors, build logs) so the caller can fall back to text handling.
func parseComposeProgress(line string) (*ComposeProgress, bool) {
	if len(line) == 0 || line[0] != '{' {
		return nil, false
	}
	var evt ComposeProgress
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		return nil, false
	}
	if evt.ID == "" {
		return nil, false
	}
	return &evt, true
}

// humanizeComposeProgress renders a progress event as a single readable line
// for the raw log / synchronous output, replacing Docker's TTY-less dump (no
// more "Pull complete 0B"). e.g. "fbcfea79c1c4 Downloading 2.097MB".
func humanizeComposeProgress(p *ComposeProgress) string {
	line := strings.TrimSpace(p.ID + " " + p.Text)
	if p.Details != "" && p.Details != "0B" {
		line += " " + p.Details
	}
	return line
}

// runCompose executes a docker compose command, streaming output lines to the emitter.
func runCompose(ctx context.Context, projectName, configFile, workingDir string, emitter composeLineEmitter, args ...string) error {
	// --progress=json makes Docker emit one machine-readable JSON event per
	// progress update (per-layer current/total/percent) instead of the
	// humanized, TTY-less text dump. We parse those below into structured
	// progress and a clean humanized line.
	baseArgs := []string{"compose", "--progress=json", "--project-name", projectName, "--file", configFile}
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
	// Docker can emit long lines (large progress events); raise the buffer cap.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if emitter == nil {
			continue
		}
		// Try to decode a structured --progress=json event first.
		if evt, ok := parseComposeProgress(line); ok {
			emitter("progress", humanizeComposeProgress(evt), evt)
			continue
		}
		// Fall back to plain text (warnings, errors, build logs).
		msgType := "stdout"
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "error") || strings.Contains(lower, "failed") {
			msgType = "stderr"
		}
		emitter(msgType, line, nil)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return cmd.Wait()
}

func composeUp(
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

func composeDown(
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

func composeStop(
	ctx context.Context,
	projectName, configFile, workingDir string,
	emitter composeLineEmitter,
) error {
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, "stop")
}

func composeValidateContent(ctx context.Context, content string) error {
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
