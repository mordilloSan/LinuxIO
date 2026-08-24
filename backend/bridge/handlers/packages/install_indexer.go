package packages

import (
	"context"
	"fmt"
	"net/http"
	"time"

	bridgetask "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	indexerInstallScriptURL      = "https://github.com/mordilloSan/indexer/releases/latest/download/indexer-install.sh"
	indexerInstallScriptMaxBytes = 4 << 20
	indexerInstallTimeout        = 10 * time.Minute
)

var (
	indexerInstallHTTPClient = &http.Client{Timeout: 30 * time.Second}
	indexerInstallRunner     = runIndexerInstallScript
)

func installIndexer(ctx context.Context, task *bridgetask.Task) error {
	ctx, cancel := context.WithTimeout(ctx, indexerInstallTimeout)
	defer cancel()

	reportProgress(task, stageResolve, "Downloading Indexer installer", pctResolve)
	script, err := downloadIndexerInstallScript(ctx, indexerInstallHTTPClient)
	if err != nil {
		return fmt.Errorf("download Indexer installer: %w", err)
	}

	reportProgress(task, stageInstallAsset, "Running Indexer installer", pctInstallStart)
	err = indexerInstallRunner(ctx, script, func(output InstallCapabilityOutput) {
		reportOutput(task, stageInstallAsset, "Running Indexer installer", pctInstallStart, output)
	})
	if err != nil {
		return fmt.Errorf("run Indexer installer: %w", err)
	}

	reportProgress(task, stageInstallAsset, "Installed Indexer", pctInstallEnd)
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
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, readHTTPErrorBody(resp.Body))
	}
	if resp.ContentLength > indexerInstallScriptMaxBytes {
		return nil, fmt.Errorf("installer exceeds %d bytes", indexerInstallScriptMaxBytes)
	}

	return utils.ReadAllLimited(resp.Body, indexerInstallScriptMaxBytes)
}

func runIndexerInstallScript(ctx context.Context, script []byte, report func(InstallCapabilityOutput)) error {
	return runCapabilityScript(ctx, "bash", []string{"-s"}, script, report)
}
