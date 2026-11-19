package dockerupdate

import (
	"bufio"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
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

// composeLineLimit is how much of one line of `docker compose` output is
// kept. Docker's progress events fit; the rest of a longer line, such as
// one a build step prints, is dropped rather than failing the command.
const composeLineLimit = 1024 * 1024

// readComposeLines calls emit with each line of r, without its line ending,
// cut to its first limit bytes.
func readComposeLines(r io.Reader, limit int, emit func(line string)) error {
	reader := bufio.NewReaderSize(r, 64*1024)
	var line []byte
	for {
		fragment, more, err := reader.ReadLine()
		if room := limit - len(line); room > 0 {
			line = append(line, fragment[:min(len(fragment), room)]...)
		}
		if err != nil {
			if len(line) > 0 {
				emit(string(line))
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !more {
			emit(string(line))
			line = line[:0]
		}
	}
}

// composeStopDelay is how long a cancelled `docker compose` has to stop what
// it started before it is killed.
const composeStopDelay = 10 * time.Second

// composeCommand is the `docker` command with args (composeCommandArgs), run
// in target's working folder and environment. A cancelled ctx sends it
// SIGTERM, which Compose answers by stopping cleanly, as it did when
// systemd's stop of the pass reached it directly (the unit now signals the
// program alone, KillMode=mixed), and kills it after composeStopDelay.
func composeCommand(ctx context.Context, target ComposeTarget, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = composeStopDelay
	if target.IsolateEnvironment {
		cmd.Env = composeCommandEnvironment()
	}
	if target.WorkingDir != "" {
		cmd.Dir = target.WorkingDir
	}
	return cmd
}

func composeCommandArgs(target ComposeTarget, args ...string) ([]string, error) {
	return composeCommandArgsWithProgress(target, "json", args...)
}

// composeCommandArgsWithProgress is composeCommandArgs with Compose's
// --progress mode.
func composeCommandArgsWithProgress(target ComposeTarget, progress string, args ...string) ([]string, error) {
	if strings.TrimSpace(target.Name) == "" {
		return nil, fmt.Errorf("compose project name is empty")
	}
	if len(target.ConfigFiles) == 0 {
		return nil, fmt.Errorf("compose project %q has no config files", target.Name)
	}

	baseArgs := []string{"compose", "--progress=" + progress, "--project-name", target.Name}
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
	return runComposeCommand(ctx, target, emitter, baseArgs)
}

// runComposeCommand runs `docker` with args (composeCommandArgs) for target,
// streaming each non-empty output line to emitter. env is added to the
// command's environment.
func runComposeCommand(ctx context.Context, target ComposeTarget, emitter ComposeLineEmitter, args []string, env ...string) error {
	cmd := composeCommand(ctx, target, args)
	if len(env) > 0 {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, env...)
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

	readErr := readComposeLines(pipe, composeLineLimit, func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || emitter == nil {
			return
		}
		// Try to decode a structured --progress=json event first.
		if evt, ok := parseComposeProgress(line); ok {
			emitter("progress", humanizeComposeProgress(evt), evt)
			return
		}
		// Fall back to plain text (warnings, errors, build logs).
		msgType := "stdout"
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "error") || strings.Contains(lower, "failed") {
			msgType = "stderr"
		}
		emitter(msgType, line, nil)
	})
	if readErr != nil {
		// Drain so the child cannot block on a full pipe, then reap it.
		_, _ = io.Copy(io.Discard, pipe)
		return errors.Join(fmt.Errorf("read docker compose output: %w", readErr), cmd.Wait())
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
	if err := composePullServices(ctx, target, normalized, emitter); err != nil {
		return fmt.Errorf("pull compose services %q: %w", normalized, err)
	}
	// The image is the one just pulled: an update never builds, also for a
	// service with pull_policy: build.
	upArgs := append([]string{"up", "-d", "--no-deps", "--no-build"}, normalized...)
	if err := RunComposeProject(ctx, target, emitter, upArgs...); err != nil {
		return fmt.Errorf("reconcile compose services %q: %w", normalized, err)
	}
	return nil
}

// composePullServices retries the pull with the registry backoff while its
// output shows throttling or a transient network fault. Compose only exits 1,
// so the output text is the only signal.
func composePullServices(ctx context.Context, target ComposeTarget, services []string, emitter ComposeLineEmitter) error {
	pullArgs := append([]string{"pull"}, services...)
	for retry := 0; ; retry++ {
		output := &ComposeMessageCollector{}
		err := RunComposeProject(ctx, target, func(msgType, message string, progress *ComposeProgress) {
			output.Emit(msgType, message, progress)
			if emitter != nil {
				emitter(msgType, message, progress)
			}
		}, pullArgs...)
		if err == nil || ctx.Err() != nil || retry == registryRetries || !retryableRegistryMessage(output.String()) {
			return err
		}
		if emitter != nil {
			emitter("stderr", fmt.Sprintf("registry pull failed temporarily, retrying in %s", 5*time.Second<<retry), nil)
		}
		if err := waitRegistryBackoff(ctx, retry); err != nil {
			return err
		}
	}
}

// composeUpFromLocalImages brings services up on the images the host holds
// and pulls or builds none, whatever their pull_policy says: a rollback must
// not fetch or build again the image it undoes, and a rebuild brings a
// service up on the image it has just built.
func composeUpFromLocalImages(ctx context.Context, target ComposeTarget, services []string, emitter ComposeLineEmitter) error {
	return RunComposeProject(ctx, target, emitter, append([]string{"up", "-d", "--no-deps", "--pull", "never", "--no-build"}, services...)...)
}

// composeBuildAttestations is the environment of a rebuild's build. With the
// attestations BuildKit adds by default, two builds that come entirely from
// the cache give two image IDs for the same layers, configuration and
// creation time (seen with Docker 29 and the containerd image store); without
// them the second gives the first one's ID, so a rebuild that changed
// nothing is seen as such.
var composeBuildAttestations = []string{"BUILDX_NO_DEFAULT_ATTESTATIONS=1"}

// composeBuildService builds the image of service again, pulling its base
// images first (`build --pull`), without default attestations
// (composeBuildAttestations). The build runs with --progress=plain, which
// writes the log of the build's steps as lines of text, and emitter gets each
// line; with --progress=json Compose passes BuildKit's raw status objects
// through instead.
func composeBuildService(ctx context.Context, target ComposeTarget, service string, emitter ComposeLineEmitter) error {
	args, err := composeCommandArgsWithProgress(target, "plain", "build", "--pull", service)
	if err != nil {
		return err
	}
	return runComposeCommand(ctx, target, emitter, args, composeBuildAttestations...)
}

// composeServiceSettingsOf reads services of target as Compose resolves them
// from the project's files, environment files and overrides
// (`docker compose config --format json <services>`). The services are named,
// since Compose leaves out a service of a profile that is not active unless it
// is named. Compose writes its warnings to stderr, so only stdout is read as
// the configuration.
func composeServiceSettingsOf(ctx context.Context, target ComposeTarget, services []string) (map[string]composeServiceSettings, error) {
	args, err := composeCommandArgs(target, append([]string{"config", "--format", "json"}, services...)...)
	if err != nil {
		return nil, err
	}
	cmd := composeCommand(ctx, target, args)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read Compose project %q configuration: %w: %s", target.Name, err, strings.TrimSpace(stderr.String()))
	}
	var config struct {
		Services map[string]struct {
			Image      string         `json:"image"`
			PullPolicy string         `json:"pull_policy"`
			Build      jsontext.Value `json:"build"`
		} `json:"services"`
	}
	if err := json.Unmarshal(out, &config); err != nil {
		return nil, fmt.Errorf("decode Compose project %q configuration: %w", target.Name, err)
	}
	settings := make(map[string]composeServiceSettings, len(config.Services))
	for name, service := range config.Services {
		settings[name] = composeServiceSettings{
			Image:      service.Image,
			PullPolicy: service.PullPolicy,
			Build:      len(service.Build) > 0 && string(service.Build) != "null",
		}
	}
	return settings, nil
}

func validateComposeUpdateInputs(ctx context.Context, target ComposeTarget, emitter ComposeLineEmitter) error {
	if err := RunComposeProject(ctx, target, emitter, "config", "--quiet"); err != nil {
		return fmt.Errorf("validate Compose project %q configuration: %w", target.Name, err)
	}
	return nil
}
