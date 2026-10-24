package dockerupdate

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// ComposeProgress is a single structured progress event parsed from
// `docker compose --progress=json`. The JSON tags mirror Docker's own event
// schema so the same struct is used to both decode Docker's output and to
// encode the payload sent to the frontend.
type ComposeProgress struct {
	ID       string `json:"id"` // layer id (e.g. "fbcfea79c1c4") or group (e.g. "Image alpine:3.17")
	ParentID string `json:"parent_id,omitempty"`
	Text     string `json:"text"`              // "Pulling", "Downloading", "Extracting", "Pull complete", "Creating", "Started"…
	Status   string `json:"status"`            // "Working" | "Done" | "Error"
	Details  string `json:"details,omitempty"` // Docker's humanized current (e.g. "2.097MB")
	Current  int64  `json:"current,omitzero"`
	Total    int64  `json:"total,omitzero"`
	Percent  int    `json:"percent,omitzero"`
}

// ComposeLineEmitter receives each line of compose output. For structured
// `--progress=json` events, progress is non-nil and message holds a humanized
// rendering of the same event; for plain text lines progress is nil.
type ComposeLineEmitter func(msgType, message string, progress *ComposeProgress)

// ComposeTarget identifies a Compose project by its config files, environment
// files and working directory. IsolateEnvironment restricts the docker compose
// process to a fixed set of environment variables.
type ComposeTarget struct {
	Name               string
	ConfigFiles        []string
	EnvironmentFiles   []string
	IsolateEnvironment bool
	WorkingDir         string
}

// ComposeMessageCollector accumulates compose output so a failure can report it.
type ComposeMessageCollector struct {
	mu    sync.Mutex
	lines []string
}

func composeCommandEnvironment() []string {
	return filterComposeCommandEnvironment(os.Environ())
}

func filterComposeCommandEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !composeEnvironmentVariableAllowed(name) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func composeEnvironmentVariableAllowed(name string) bool {
	if strings.HasPrefix(name, "DOCKER_") || strings.HasPrefix(name, "LC_") {
		return true
	}
	switch name {
	case "HOME", "HTTP_PROXY", "HTTPS_PROXY", "LANG", "LANGUAGE", "LOGNAME", "NO_PROXY", "PATH", "SSL_CERT_DIR", "SSL_CERT_FILE", "TMPDIR", "USER", "XDG_CONFIG_HOME":
		return true
	default:
		return false
	}
}

// Emit is a ComposeLineEmitter that records each non-empty line.
func (c *ComposeMessageCollector) Emit(_ string, message string, _ *ComposeProgress) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	c.mu.Lock()
	c.lines = append(c.lines, message)
	c.mu.Unlock()
}

// String returns the collected lines joined by newlines.
func (c *ComposeMessageCollector) String() string {
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
	if err := json.Unmarshal([]byte(line), &evt, json.MatchCaseInsensitiveNames(true)); err != nil {
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

func composeCommandArgs(target ComposeTarget, args ...string) ([]string, error) {
	if strings.TrimSpace(target.Name) == "" {
		return nil, fmt.Errorf("compose project name is empty")
	}
	if len(target.ConfigFiles) == 0 {
		return nil, fmt.Errorf("compose project %q has no config files", target.Name)
	}

	baseArgs := []string{"compose", "--progress=json", "--project-name", target.Name}
	if target.WorkingDir != "" {
		baseArgs = append(baseArgs, "--project-directory", target.WorkingDir)
	}
	for _, environmentFile := range target.EnvironmentFiles {
		if strings.TrimSpace(environmentFile) == "" {
			return nil, fmt.Errorf("compose project %q has an empty environment file path", target.Name)
		}
		baseArgs = append(baseArgs, "--env-file", environmentFile)
	}
	for _, configFile := range target.ConfigFiles {
		if strings.TrimSpace(configFile) == "" {
			return nil, fmt.Errorf("compose project %q has an empty config file path", target.Name)
		}
		baseArgs = append(baseArgs, "--file", configFile)
	}
	return append(baseArgs, args...), nil
}

// RunComposeProject executes a docker compose command, streaming output lines
// to the emitter. Every config file is passed in label order so projects that
// use override files are reconciled with the same effective configuration.
func RunComposeProject(ctx context.Context, target ComposeTarget, emitter ComposeLineEmitter, args ...string) error {
	// --progress=json makes Docker emit one machine-readable JSON event per
	// progress update (per-layer current/total/percent) instead of the
	// humanized, TTY-less text dump. We parse those below into structured
	// progress and a clean humanized line.
	baseArgs, err := composeCommandArgs(target, args...)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "docker", baseArgs...)
	if target.IsolateEnvironment {
		cmd.Env = composeCommandEnvironment()
	}
	if target.WorkingDir != "" {
		cmd.Dir = target.WorkingDir
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

	if scanErr := scanner.Err(); scanErr != nil {
		// Drain so the child cannot block on a full pipe, then reap it.
		_, _ = io.Copy(io.Discard, pipe)
		return errors.Join(fmt.Errorf("scanner error: %w", scanErr), cmd.Wait())
	}

	return cmd.Wait()
}

func composePullAndUpServices(ctx context.Context, target ComposeTarget, services []string, emitter ComposeLineEmitter) error {
	if err := validateComposeUpdateInputs(ctx, target, emitter); err != nil {
		return err
	}
	return composePullAndUpServicesValidated(ctx, target, services, emitter)
}

func composePullAndUpServicesValidated(ctx context.Context, target ComposeTarget, services []string, emitter ComposeLineEmitter) error {
	seen := make(map[string]struct{}, len(services))
	normalized := make([]string, 0, len(services))
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" {
			return fmt.Errorf("compose service name is empty")
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		normalized = append(normalized, service)
	}
	if len(normalized) == 0 {
		return fmt.Errorf("no compose services selected")
	}
	pullArgs := append([]string{"pull"}, normalized...)
	if err := RunComposeProject(ctx, target, emitter, pullArgs...); err != nil {
		return fmt.Errorf("pull compose services %q: %w", normalized, err)
	}
	upArgs := append([]string{"up", "-d", "--no-deps"}, normalized...)
	if err := RunComposeProject(ctx, target, emitter, upArgs...); err != nil {
		return fmt.Errorf("reconcile compose services %q: %w", normalized, err)
	}
	return nil
}

func validateComposeUpdateInputs(ctx context.Context, target ComposeTarget, emitter ComposeLineEmitter) error {
	if err := RunComposeProject(ctx, target, emitter, "config", "--quiet"); err != nil {
		return fmt.Errorf("validate Compose project %q configuration: %w", target.Name, err)
	}
	return nil
}
