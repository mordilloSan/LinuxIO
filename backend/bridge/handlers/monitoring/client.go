// Package monitoring provides shared utilities for communicating with the
// linuxio-monitoring daemon over its two unix sockets.
package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

const maxCommandPayloadBytes = 1 << 20

var (
	commandRetryInterval = 150 * time.Millisecond
	commandRetryTimeout  = 5 * time.Second
)

// ErrUnavailable marks a daemon that cannot be reached.
var ErrUnavailable = errors.New("linuxio-monitoring unavailable")

func unixClient(socketPath string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

// controlClient reaches control.sock: commands and privileged history reads.
// apiClient reaches api.sock: live reads any session may perform.
var (
	controlClient = unixClient(monitoringapi.ControlSocketPath, 0)
	apiClient     = unixClient(monitoringapi.APISocketPath, 15*time.Second)
)

type commandRequest struct {
	Command string          `json:"command"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type commandError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type commandResponse struct {
	OK              bool            `json:"ok"`
	Command         string          `json:"command"`
	RestartRequired bool            `json:"restart_required,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
	Error           *commandError   `json:"error,omitempty"`
}

// runCommand posts one command to the agent's command API and returns the
// decoded envelope. Agent-reported failures (ok=false) surface as errors with
// the agent's message.
func runCommand(ctx context.Context, command string, params any) (commandResponse, error) {
	body := commandRequest{Command: command}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return commandResponse{}, fmt.Errorf("encode %s params: %w", command, err)
		}
		body.Params = raw
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return commandResponse{}, fmt.Errorf("encode %s request: %w", command, err)
	}

	resp, err := doCommandRequest(ctx, command, payload)
	if err != nil {
		return commandResponse{}, err
	}
	defer resp.Body.Close()

	var decoded commandResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxCommandPayloadBytes))
	if err := decoder.Decode(&decoded); err != nil {
		return commandResponse{}, fmt.Errorf("decode %s response (%s): %w", command, resp.Status, err)
	}
	if !decoded.OK {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return commandResponse{}, fmt.Errorf("%s", decoded.Error.Message)
		}
		return commandResponse{}, fmt.Errorf("%s failed: %s", command, resp.Status)
	}
	return decoded, nil
}

func doCommandRequest(ctx context.Context, command string, payload []byte) (*http.Response, error) {
	deadline := time.Now().Add(commandRetryTimeout)
	var lastErr error

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/api/v1/command", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create %s request: %w", command, err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := controlClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if !isTransientCommandDialError(err) || time.Now().After(deadline) {
			return nil, fmt.Errorf("monitoring command request: %w", err)
		}

		timer := time.NewTimer(commandRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, fmt.Errorf("monitoring command request: %w", errors.Join(ctx.Err(), lastErr))
		case <-timer.C:
		}
	}
}

func isTransientCommandDialError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET)
}
