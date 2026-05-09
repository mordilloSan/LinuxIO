package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxIndexerStatusPayloadBytes = 1 << 20

// Status is the daemon status shape exposed by the indexer /status endpoint.
type Status struct {
	Running      bool   `json:"running"`
	Status       string `json:"status"`
	NumDirs      int64  `json:"num_dirs"`
	NumFiles     int64  `json:"num_files"`
	TotalSize    int64  `json:"total_size"`
	LastIndexed  string `json:"last_indexed,omitempty"`
	TotalIndexes int64  `json:"total_indexes"`
	TotalEntries int64  `json:"total_entries"`
	DatabaseSize int64  `json:"database_size"`
	WALSize      int64  `json:"wal_size"`
	SHMSize      int64  `json:"shm_size"`
	TotalOnDisk  int64  `json:"total_on_disk"`
	Warning      string `json:"warning,omitempty"`
}

func FetchStatus(ctx context.Context) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/status", nil)
	if err != nil {
		return Status{}, fmt.Errorf("create indexer status request: %w", err)
	}

	resp, err := indexerClient.Do(req)
	if err != nil {
		return Status{}, fmt.Errorf("indexer status request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxIndexerStatusPayloadBytes))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return Status{}, fmt.Errorf("%s", message)
	}

	var status Status
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxIndexerStatusPayloadBytes))
	if err := decoder.Decode(&status); err != nil {
		return Status{}, fmt.Errorf("decode indexer status: %w", err)
	}

	status.Status = strings.ToLower(strings.TrimSpace(status.Status))
	status.Running = status.Status == "running" || status.Status == "indexing"
	return status, nil
}
