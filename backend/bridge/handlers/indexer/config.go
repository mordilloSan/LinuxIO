package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const maxIndexerConfigPayloadBytes = 1 << 20

func FetchConfig(ctx context.Context) (apischema.IndexerConfig, error) {
	resp, err := sendConfigRequest(ctx, http.MethodGet, nil)
	if err != nil {
		return apischema.IndexerConfig{}, err
	}
	cfg, err := decodeConfigResponse(resp)
	if err != nil {
		return apischema.IndexerConfig{}, fmt.Errorf("fetch indexer config: %w", err)
	}
	return cfg, nil
}

func UpdateConfig(ctx context.Context, payload []byte) (apischema.IndexerConfig, bool, error) {
	body, err := normalizeConfigPatchPayload(payload)
	if err != nil {
		return apischema.IndexerConfig{}, false, err
	}
	resp, err := sendConfigRequest(ctx, http.MethodPut, bytes.NewReader(body))
	if err != nil {
		return apischema.IndexerConfig{}, false, err
	}
	restartRequired := strings.EqualFold(
		resp.Header.Get("X-Indexer-Restart-Required"),
		"true",
	)
	cfg, err := decodeConfigResponse(resp)
	if err != nil {
		return apischema.IndexerConfig{}, false, fmt.Errorf("update indexer config: %w", err)
	}
	return cfg, restartRequired, nil
}

func sendConfigRequest(ctx context.Context, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://unix/config", body)
	if err != nil {
		return nil, fmt.Errorf("create indexer config request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := indexerClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("indexer config request: %w", err)
	}
	return resp, nil
}

func decodeConfigResponse(resp *http.Response) (apischema.IndexerConfig, error) {
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxIndexerConfigPayloadBytes))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return apischema.IndexerConfig{}, fmt.Errorf("%s", message)
	}

	var cfg apischema.IndexerConfig
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxIndexerConfigPayloadBytes))
	if err := decoder.Decode(&cfg); err != nil {
		return apischema.IndexerConfig{}, fmt.Errorf("decode response: %w", err)
	}
	return cfg, nil
}

func normalizeConfigPatchPayload(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(strings.TrimSpace(string(payload))) == 0 {
		return nil, bridgeipc.ErrInvalidArgs
	}
	if len(payload) > maxIndexerConfigPayloadBytes {
		return nil, fmt.Errorf("indexer config payload is too large")
	}

	var patch apischema.IndexerConfigPatch
	if err := jsonv2.Unmarshal(payload, &patch, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("invalid indexer config JSON: %w", err)
	}

	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("encode indexer config patch: %w", err)
	}
	return body, nil
}
