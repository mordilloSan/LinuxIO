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

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

const maxIndexerConfigPayloadBytes = 1 << 20

func FetchConfig(ctx context.Context) (indexerapi.IndexerConfig, error) {
	resp, err := sendConfigRequest(ctx, http.MethodGet, nil)
	if err != nil {
		return indexerapi.IndexerConfig{}, err
	}
	cfg, err := decodeConfigResponse(resp)
	if err != nil {
		return indexerapi.IndexerConfig{}, fmt.Errorf("fetch indexer config: %w", err)
	}
	return cfg, nil
}

func UpdateConfig(ctx context.Context, payload []byte) (indexerapi.IndexerConfig, error) {
	body, err := normalizeConfigPatchPayload(payload)
	if err != nil {
		return indexerapi.IndexerConfig{}, err
	}
	resp, err := sendConfigRequest(ctx, http.MethodPut, bytes.NewReader(body))
	if err != nil {
		return indexerapi.IndexerConfig{}, err
	}
	cfg, err := decodeConfigResponse(resp)
	if err != nil {
		return indexerapi.IndexerConfig{}, fmt.Errorf("update indexer config: %w", err)
	}
	return cfg, nil
}

func sendConfigRequest(ctx context.Context, method string, body io.Reader) (*http.Response, error) {
	resp, err := daemonRequest(ctx, method, indexerapi.RouteConfig, nil, body)
	if err != nil {
		return nil, fmt.Errorf("indexer config request: %w", err)
	}
	return resp, nil
}

func decodeConfigResponse(resp *http.Response) (indexerapi.IndexerConfig, error) {
	defer resp.Body.Close()

	payload, err := readBoundedBody(resp.Body)
	if err != nil {
		return indexerapi.IndexerConfig{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(payload))
		if message == "" {
			message = resp.Status
		}
		responseErr := &ResponseError{Route: indexerapi.RouteConfig, StatusCode: resp.StatusCode, Message: message}
		if resp.StatusCode >= http.StatusInternalServerError {
			return indexerapi.IndexerConfig{}, fmt.Errorf("%w: %w", ErrUnavailable, responseErr)
		}
		return indexerapi.IndexerConfig{}, responseErr
	}

	var cfg indexerapi.IndexerConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return indexerapi.IndexerConfig{}, fmt.Errorf("decode response: %w", err)
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

	var patch indexerapi.IndexerConfigPatch
	if err := jsonv2.Unmarshal(payload, &patch, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("invalid indexer config JSON: %w", err)
	}

	body, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("encode indexer config patch: %w", err)
	}
	return body, nil
}
