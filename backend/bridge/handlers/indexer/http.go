package indexer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

const maxIndexerResponseBytes = 1 << 20

const indexerResponseHeaderTimeout = 10 * time.Second

var ErrUnavailable = errors.New("indexer unavailable")

// Client is shared by all daemon requests. It intentionally has no
// client timeout: SSE callers need to outlive a full indexing run.
var Client = &http.Client{Transport: &http.Transport{
	ResponseHeaderTimeout: indexerResponseHeaderTimeout,
	DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "unix", "/run/linuxio/indexer.sock")
	},
}}

type ResponseError struct {
	Route      string
	StatusCode int
	Message    string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("indexer %s: %s", e.Route, e.Message)
}

func daemonRequest(ctx context.Context, method, route string, query url.Values, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+route, body)
	if err != nil {
		return nil, fmt.Errorf("build indexer request: %w", err)
	}
	req.URL.RawQuery = query.Encode()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := Client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return resp, nil
}

func daemonJSON(ctx context.Context, method, route string, query url.Values, body io.Reader, dst any) error {
	resp, err := daemonRequest(ctx, method, route, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := readBoundedBody(resp.Body)
	if err != nil {
		return fmt.Errorf("read indexer %s response: %w", route, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(payload))
		if message == "" {
			message = resp.Status
		}
		responseErr := &ResponseError{Route: route, StatusCode: resp.StatusCode, Message: message}
		if resp.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("%w: %w", ErrUnavailable, responseErr)
		}
		return responseErr
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(payload, dst); err != nil {
		return fmt.Errorf("decode indexer %s response: %w", route, err)
	}
	return nil
}

func readBoundedBody(r io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(r, maxIndexerResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxIndexerResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxIndexerResponseBytes)
	}
	return payload, nil
}

func Add(ctx context.Context, entry indexerapi.EntryRequest) error {
	body, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal indexer add: %w", err)
	}
	return daemonJSON(ctx, http.MethodPost, indexerapi.RouteAdd, nil, bytes.NewReader(body), nil)
}

func Delete(ctx context.Context, path string) error {
	q := url.Values{"path": {utils.NormalizeIndexerPath(path)}}
	return daemonJSON(ctx, http.MethodDelete, indexerapi.RouteDelete, q, nil, nil)
}

func Reindex(ctx context.Context, path string) error {
	q := url.Values{"path": {utils.NormalizeIndexerPath(path)}}
	return daemonJSON(ctx, http.MethodPost, indexerapi.RouteReindex, q, nil, nil)
}

func DirSize(ctx context.Context, path string) (indexerapi.DirSizeResponse, error) {
	var out indexerapi.DirSizeResponse
	err := daemonJSON(ctx, http.MethodGet, indexerapi.RouteDirSize, url.Values{"path": {utils.NormalizeIndexerPath(path)}}, nil, &out)
	return out, err
}

func fetchDaemonStatus(ctx context.Context) (indexerapi.StatusResponse, error) {
	var out indexerapi.StatusResponse
	err := daemonJSON(ctx, http.MethodGet, indexerapi.RouteStatus, nil, nil, &out)
	return out, err
}

func Subfolders(ctx context.Context, path string) ([]indexerapi.SubfolderResult, error) {
	var out []indexerapi.SubfolderResult
	err := daemonJSON(ctx, http.MethodGet, indexerapi.RouteSubfolders, url.Values{"path": {utils.NormalizeIndexerPath(path)}}, nil, &out)
	return out, err
}

func Search(ctx context.Context, query, limit, basePath string) ([]indexerapi.EntryResult, error) {
	q := url.Values{"q": {query}, "limit": {limit}}
	if basePath != "" && basePath != "/" {
		q.Set("base", utils.NormalizeIndexerPath(basePath))
	}
	var out []indexerapi.EntryResult
	err := daemonJSON(ctx, http.MethodGet, indexerapi.RouteSearch, q, nil, &out)
	return out, err
}
