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

	bridgejobs "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	indexerInstallScriptURL      = "https://github.com/mordilloSan/indexer/releases/latest/download/indexer-install.sh"
	indexerInstallScriptMaxBytes = 4 << 20
	indexerInstallTimeout        = 10 * time.Minute
	indexerInstallErrorMaxBytes  = 4 << 10
)

var (
	indexerInstallHTTPClient = &http.Client{Timeout: 30 * time.Second}
	indexerInstallRunner     = runIndexerInstallScript
)

func installIndexer(ctx context.Context, job *bridgejobs.Job) error {
	ctx, cancel := context.WithTimeout(ctx, indexerInstallTimeout)
	defer cancel()

	reportProgress(job, stageResolve, "Downloading Indexer installer", pctResolve)
	script, err := downloadIndexerInstallScript(ctx, indexerInstallHTTPClient)
	if err != nil {
		return fmt.Errorf("download Indexer installer: %w", err)
	}

	reportProgress(job, stageInstallAsset, "Running Indexer installer", pctInstallStart)
	output, err := indexerInstallRunner(ctx, script)
	if err != nil {
		return fmt.Errorf("run Indexer installer: %w", indexerInstallCommandError(err, output))
	}

	reportProgress(job, stageInstallAsset, "Installed Indexer", pctInstallEnd)
	return nil
}

func downloadIndexerInstallScript(ctx context.Context, client *http.Client) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexerInstallScriptURL, nil)
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
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, readWatchtowerErrorBody(resp.Body))
	}
	if resp.ContentLength > indexerInstallScriptMaxBytes {
		return nil, fmt.Errorf("installer exceeds %d bytes", indexerInstallScriptMaxBytes)
	}

	return utils.ReadAllLimited(resp.Body, indexerInstallScriptMaxBytes)
}

func runIndexerInstallScript(ctx context.Context, script []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bash", "-s")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdin = bytes.NewReader(script)
	return cmd.CombinedOutput()
}

func indexerInstallCommandError(err error, output []byte) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	if len(message) > indexerInstallErrorMaxBytes {
		message = message[len(message)-indexerInstallErrorMaxBytes:]
		message = "..." + message
	}
	return fmt.Errorf("%w: %s", err, message)
}
