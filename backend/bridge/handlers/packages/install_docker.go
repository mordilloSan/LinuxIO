package packages

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// dockerComponent is the InstallSpec.OptionalComponent value that installs
// Docker Engine with Docker's convenience script.
const dockerComponent = "docker"

const (
	dockerScriptFetchTimeout = 30 * time.Second
	dockerScriptMaxBytes     = 1 << 20
)

var dockerInstallScriptURL = "https://get.docker.com"

func installCapabilityComponent(ctx context.Context, task *bridgetask.Task, component string) error {
	switch component {
	case "":
		return nil
	case dockerComponent:
		return installDockerEngine(ctx, task)
	default:
		return fmt.Errorf("unknown optional component %q", component)
	}
}

// installDockerEngine downloads Docker's convenience script and runs it,
// streaming its output to the task. An existing docker binary skips the
// script, which would otherwise warn and pause before reinstalling; the
// caller's service steps then start the installed engine.
func installDockerEngine(ctx context.Context, task *bridgetask.Task) error {
	if path, err := capabilityCommandLookPath("docker"); err == nil {
		reportProgress(task, stageInstallPackage, fmt.Sprintf("Docker is already installed at %s; skipping the install script", path), pctInstallEnd)
		return nil
	}

	reportProgress(task, stageResolve, fmt.Sprintf("Downloading Docker install script from %s", dockerInstallScriptURL), pctResolve)
	script, err := fetchDockerInstallScript(ctx)
	if err != nil {
		return fmt.Errorf("download docker install script: %w", err)
	}
	reportProgress(task, stageResolve, fmt.Sprintf("Downloaded Docker install script (%d bytes, sha256 %x)", len(script), sha256.Sum256(script)), pctResolve)

	file, err := os.CreateTemp("", "linuxio-get-docker-*.sh")
	if err != nil {
		return fmt.Errorf("create docker install script: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(script); err != nil {
		_ = file.Close()
		return fmt.Errorf("write docker install script: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write docker install script: %w", err)
	}

	const message = "Running Docker install script"
	reportProgress(task, stageInstallPackage, message, pctInstallStart)
	slog.Info("Running Docker install script.", "url", dockerInstallScriptURL)
	if err := runCapabilityCommand(ctx, "sh", []string{file.Name()}, func(output InstallCapabilityOutput) {
		reportOutput(task, stageInstallPackage, message, pctInstallStart, output)
	}); err != nil {
		return fmt.Errorf("run docker install script: %w", err)
	}
	reportProgress(task, stageInstallPackage, "Docker install script finished", pctInstallEnd)
	return nil
}

func fetchDockerInstallScript(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, dockerScriptFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dockerInstallScriptURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "LinuxIO")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", dockerInstallScriptURL, resp.Status)
	}
	script, err := io.ReadAll(io.LimitReader(resp.Body, dockerScriptMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(script) > dockerScriptMaxBytes {
		return nil, fmt.Errorf("script exceeds %d bytes", dockerScriptMaxBytes)
	}
	if len(script) == 0 {
		return nil, fmt.Errorf("script is empty")
	}
	return script, nil
}
