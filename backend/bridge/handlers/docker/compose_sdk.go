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

type composeProjectTarget struct {
	Name               string
	ConfigFiles        []string
	EnvironmentFiles   []string
	IsolateEnvironment bool
	WorkingDir         string
}

type composeMessageCollector struct {
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

func composeCommandArgs(target composeProjectTarget, args ...string) ([]string, error) {
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

// runComposeProject executes a docker compose command, streaming output lines
// to the emitter. Every config file is passed in label order so projects that
// use override files are reconciled with the same effective configuration.
func runComposeProject(ctx context.Context, target composeProjectTarget, emitter composeLineEmitter, args ...string) error {
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

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	return cmd.Wait()
}

// runCompose preserves the single-config call surface used by the existing
// project actions while update reconciliation uses runComposeProject directly.
func runCompose(ctx context.Context, projectName, configFile, workingDir string, emitter composeLineEmitter, args ...string) error {
	return runComposeProject(ctx, composeProjectTarget{
		Name:        projectName,
		ConfigFiles: []string{configFile},
		WorkingDir:  workingDir,
	}, emitter, args...)
}

func composePullAndUp(ctx context.Context, target composeProjectTarget, service string, emitter composeLineEmitter) error {
	return composePullAndUpServices(ctx, target, []string{service}, emitter)
}

func composePullAndUpServices(ctx context.Context, target composeProjectTarget, services []string, emitter composeLineEmitter) error {
	if err := validateComposeUpdateInputs(ctx, target, emitter); err != nil {
		return err
	}
	return composePullAndUpServicesValidated(ctx, target, services, emitter)
}

func composePullAndUpServicesValidated(ctx context.Context, target composeProjectTarget, services []string, emitter composeLineEmitter) error {
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
	if err := runComposeProject(ctx, target, emitter, pullArgs...); err != nil {
		return fmt.Errorf("pull compose services %q: %w", normalized, err)
	}
	upArgs := append([]string{"up", "-d", "--no-deps"}, normalized...)
	if err := runComposeProject(ctx, target, emitter, upArgs...); err != nil {
		return fmt.Errorf("reconcile compose services %q: %w", normalized, err)
	}
	return nil
}

func validateComposeUpdateInputs(ctx context.Context, target composeProjectTarget, emitter composeLineEmitter) error {
	if err := runComposeProject(ctx, target, emitter, "config", "--quiet"); err != nil {
		return fmt.Errorf("validate Compose project %q configuration: %w", target.Name, err)
	}
	return nil
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
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}
