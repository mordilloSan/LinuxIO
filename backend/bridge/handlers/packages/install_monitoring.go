package packages

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	monitoringInstallScriptURL      = "https://mordillosan.github.io/go-monitoring/install.sh"
	monitoringInstallScriptMaxBytes = 4 << 20
	monitoringInstallTimeout        = 10 * time.Minute
	monitoringInstallErrorMaxBytes  = 4 << 10
)

var (
	monitoringInstallHTTPClient = &http.Client{Timeout: 30 * time.Second}
	monitoringInstallRunner     = runMonitoringInstallScript
)

func installMonitoring(ctx context.Context, task *bridgetask.Task) error {
	ctx, cancel := context.WithTimeout(ctx, monitoringInstallTimeout)
	defer cancel()

	reportProgress(task, stageResolve, "Downloading go-monitoring installer", pctResolve)
	script, err := downloadMonitoringInstallScript(ctx, monitoringInstallHTTPClient)
	if err != nil {
		return fmt.Errorf("download go-monitoring installer: %w", err)
	}

	reportProgress(task, stageInstallAsset, "Running go-monitoring installer", pctInstallStart)
	output, err := monitoringInstallRunner(ctx, script)
	if err != nil {
		return fmt.Errorf("run go-monitoring installer: %w", monitoringInstallCommandError(err, output))
	}

	reportProgress(task, stageInstallAsset, "Installed go-monitoring", pctInstallEnd)
	return nil
}

func downloadMonitoringInstallScript(ctx context.Context, client *http.Client) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, monitoringInstallScriptURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "text/x-shellscript, text/plain;q=0.9, */*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, readHTTPErrorBody(resp.Body))
	}
	if resp.ContentLength > monitoringInstallScriptMaxBytes {
		return nil, fmt.Errorf("installer exceeds %d bytes", monitoringInstallScriptMaxBytes)
	}

	return utils.ReadAllLimited(resp.Body, monitoringInstallScriptMaxBytes)
}

func runMonitoringInstallScript(ctx context.Context, script []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdin = bytes.NewReader(script)
	return cmd.CombinedOutput()
}

func monitoringInstallCommandError(err error, output []byte) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	if len(message) > monitoringInstallErrorMaxBytes {
		message = message[len(message)-monitoringInstallErrorMaxBytes:]
		message = "..." + message
	}
	return fmt.Errorf("%w: %s", err, message)
}
