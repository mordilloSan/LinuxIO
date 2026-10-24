// Package monitoring provides shared utilities for communicating with the
// linuxio-monitoring daemon over its two unix sockets.
package monitoring

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
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

// reply is one daemon response with its body read (and bounded).
type reply struct {
	Status     string
	StatusCode int
	Body       []byte
}

// statusError is a daemon reply other than 200 OK on a read route.
type statusError struct{ reply }

func (e *statusError) Error() string { return "unexpected status " + e.Status }

// exchange sends one request to a daemon socket and reads at most limit bytes
// of the reply. A nil payload sends no body. Transport failures come back
// unwrapped so callers can tell a dial retry from a bad reply.
func exchange(ctx context.Context, client *http.Client, method, path string, payload []byte, limit int64) (reply, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, body)
	if err != nil {
		return reply{}, fmt.Errorf("create %s request: %w", path, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return reply{}, fmt.Errorf("read %s response: %w", path, err)
	}
	return reply{Status: resp.Status, StatusCode: resp.StatusCode, Body: data}, nil
}

// getJSON reads one JSON document from a daemon socket. Transport failures
// wrap ErrUnavailable, an expired ctx returns its own error, and any status
// but 200 returns a *statusError carrying the reply.
func getJSON(ctx context.Context, client *http.Client, path string, limit int64, out any) error {
	rep, err := exchange(ctx, client, http.MethodGet, path, nil, limit)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if rep.StatusCode != http.StatusOK {
		return &statusError{rep}
	}
	if err := json.Unmarshal(rep.Body, out); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("decode %s response: %w", path, err)
	}
	return ctx.Err()
}

// getAPI reads a world-readable route on api.sock. Every failure to get a
// good reply, a non-200 status included, counts as the daemon being unavailable.
func getAPI(ctx context.Context, path string, limit int64, out any) error {
	err := getJSON(ctx, apiClient, path, limit, out)
	if status, ok := errors.AsType[*statusError](err); ok {
		return fmt.Errorf("%w: %s returned %s", ErrUnavailable, path, status.Status)
	}
	return err
}

// runCommand posts one command to the daemon's command API and returns the
// decoded envelope. Daemon-reported failures (ok=false) surface as errors with
// the daemon's message.
func runCommand(ctx context.Context, command string, params any) (monitoringapi.CommandResponse[jsontext.Value], error) {
	body := monitoringapi.CommandRequest{Command: command}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return monitoringapi.CommandResponse[jsontext.Value]{}, fmt.Errorf("encode %s params: %w", command, err)
		}
		body.Params = raw
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return monitoringapi.CommandResponse[jsontext.Value]{}, fmt.Errorf("encode %s request: %w", command, err)
	}

	rep, err := postCommand(ctx, payload)
	if err != nil {
		return monitoringapi.CommandResponse[jsontext.Value]{}, err
	}

	var decoded monitoringapi.CommandResponse[jsontext.Value]
	if err := json.Unmarshal(rep.Body, &decoded); err != nil {
		return decoded, fmt.Errorf("decode %s response (%s): %w", command, rep.Status, err)
	}
	if !decoded.OK {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return decoded, fmt.Errorf("%s", decoded.Error.Message)
		}
		return decoded, fmt.Errorf("%s failed: %s", command, rep.Status)
	}
	return decoded, nil
}

// postCommand retries while the control socket is still coming up.
func postCommand(ctx context.Context, payload []byte) (reply, error) {
	deadline := time.Now().Add(commandRetryTimeout)
	var lastErr error

	for {
		rep, err := exchange(ctx, controlClient, http.MethodPost, monitoringapi.RouteCommand, payload, maxCommandPayloadBytes)
		if err == nil {
			return rep, nil
		}
		lastErr = err

		if !isTransientCommandDialError(err) || time.Now().After(deadline) {
			return reply{}, fmt.Errorf("monitoring command request: %w", err)
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
			return reply{}, fmt.Errorf("monitoring command request: %w", errors.Join(ctx.Err(), lastErr))
		case <-timer.C:
		}
	}
}

// healthTimeout bounds the capability probe; the daemon either answers at
// once or is not running.
const healthTimeout = 5 * time.Second

// CheckHealth asks the daemon's world-readable API socket whether it is
// healthy. The daemon ships with LinuxIO, so detection only needs to know that
// it is running.
func CheckHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	rep, err := exchange(ctx, apiClient, http.MethodGet, monitoringapi.RouteHealth, nil, 4096)
	if err != nil {
		return fmt.Errorf("linuxio-monitoring is not running: %w", err)
	}
	if rep.StatusCode != http.StatusOK {
		return fmt.Errorf("linuxio-monitoring health %s: %s", rep.Status, strings.TrimSpace(string(rep.Body)))
	}
	return nil
}

func isTransientCommandDialError(err error) bool {
	return errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET)
}
