package docker

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

// runCompose runs a docker compose command for a project with a single config
// file, which is how every project action invokes it.
func runCompose(ctx context.Context, projectName, configFile, workingDir string, emitter dockerupdate.ComposeLineEmitter, args ...string) error {
	return dockerupdate.RunComposeProject(ctx, dockerupdate.ComposeTarget{
		Name:        projectName,
		ConfigFiles: []string{configFile},
		WorkingDir:  workingDir,
	}, emitter, args...)
}

func composeUp(
	ctx context.Context,
	projectName, configFile, workingDir string,
	removeOrphans bool,
	emitter dockerupdate.ComposeLineEmitter,
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
	emitter dockerupdate.ComposeLineEmitter,
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
	emitter dockerupdate.ComposeLineEmitter,
) error {
	if workingDir == "" {
		workingDir = filepath.Dir(configFile)
	}
	return runCompose(ctx, projectName, configFile, workingDir, emitter, "stop")
}

func composeValidateContent(ctx context.Context, content, envContent, workingDir string) error {
	dir, err := os.MkdirTemp("", "linuxio-compose-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	composeFile := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(content), 0o600); err != nil {
		return fmt.Errorf("failed to write temp compose file: %w", err)
	}

	args := []string{"compose"}
	// Anchor interpolation and relative paths to the stack's real directory so
	// its .env and env_file references resolve; without this every ${VAR}
	// defaults to "" and specs like "${DIR}:/data" fail as ":/data".
	if workingDir != "" {
		if info, statErr := os.Stat(workingDir); statErr == nil && info.IsDir() {
			args = append(args, "--project-directory", workingDir)
		}
	}
	// The editor's env buffer takes precedence over the on-disk .env.
	if envContent != "" {
		envFile := filepath.Join(dir, ".env")
		if err := os.WriteFile(envFile, []byte(envContent), 0o600); err != nil {
			return fmt.Errorf("failed to write temp env file: %w", err)
		}
		args = append(args, "--env-file", envFile)
	}
	args = append(args, "-f", composeFile, "config")

	cmd := exec.CommandContext(ctx, "docker", args...)
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
