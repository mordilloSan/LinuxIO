package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func withTestMonitoringClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	orig := monitoringClient
	monitoringClient = &http.Client{Transport: fn}
	t.Cleanup(func() { monitoringClient = orig })
}

func withFastCommandRetry(t *testing.T) {
	t.Helper()
	origInterval := commandRetryInterval
	origTimeout := commandRetryTimeout
	commandRetryInterval = time.Millisecond
	commandRetryTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		commandRetryInterval = origInterval
		commandRetryTimeout = origTimeout
	})
}

func decodeCommandRequest(t *testing.T, req *http.Request) commandRequest {
	t.Helper()
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.Method)
	}
	if req.URL.Path != "/api/v1/command" {
		t.Fatalf("path = %s, want /api/v1/command", req.URL.Path)
	}
	var decoded commandRequest
	if err := json.NewDecoder(req.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return decoded
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestFetchConfigSendsConfigGetCommand(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "config.get" {
			t.Fatalf("command = %q, want config.get", cmd.Command)
		}
		if len(cmd.Params) != 0 {
			t.Fatalf("params = %s, want empty", cmd.Params)
		}
		return jsonResponse(http.StatusOK, `{
			"ok": true,
			"command": "config.get",
			"data": {
				"version": 1,
				"collector_interval": "15s",
				"smart_refresh_interval": "1h",
				"history": "cpu,mem",
				"allow_remote_commands": true,
				"history_retention": "720h",
				"listeners": [{"name": "metrics", "address": "127.0.0.1:45876", "apis": ["metrics"]}]
			}
		}`), nil
	})

	cfg, err := FetchConfig(context.Background())
	if err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if cfg.CollectorInterval != "15s" ||
		cfg.SmartRefreshInterval != "1h" ||
		cfg.History != "cpu,mem" ||
		cfg.HistoryRetention != "720h" ||
		!cfg.AllowRemoteCommands {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.HistoryRetention != "720h" {
		t.Fatalf("history_retention = %q", cfg.HistoryRetention)
	}
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Name != "metrics" {
		t.Fatalf("listeners = %#v", cfg.Listeners)
	}
}

func TestRunCommandRetriesTransientSocketErrors(t *testing.T) {
	withFastCommandRetry(t)
	attempts := 0
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, os.ErrNotExist
		}
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "config.get" {
			t.Fatalf("command = %q, want config.get", cmd.Command)
		}
		return jsonResponse(http.StatusOK, `{
			"ok": true,
			"command": "config.get",
			"data": {
				"version": 1,
				"collector_interval": "15s",
				"history": "cpu",
				"history_retention": "720h",
				"listeners": []
			}
		}`), nil
	})

	if _, err := FetchConfig(context.Background()); err != nil {
		t.Fatalf("FetchConfig: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestUpdateConfigSendsPatchAndReadsRestartRequired(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "config.set" {
			t.Fatalf("command = %q, want config.set", cmd.Command)
		}
		params := string(cmd.Params)
		if !strings.Contains(params, `"collector_interval":"30s"`) {
			t.Fatalf("params missing collector_interval: %s", params)
		}
		if strings.Contains(params, `"history"`) {
			t.Fatalf("params included unset fields: %s", params)
		}
		return jsonResponse(http.StatusOK, `{
			"ok": true,
			"command": "config.set",
			"restart_required": true,
			"data": {"version": 1, "collector_interval": "30s", "history": "cpu", "history_retention": "720h", "listeners": []}
		}`), nil
	})

	interval := "30s"
	cfg, restartRequired, err := UpdateConfig(context.Background(), apischema.MonitoringConfigPatch{
		CollectorInterval: &interval,
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !restartRequired {
		t.Fatal("restartRequired = false, want true")
	}
	if cfg.CollectorInterval != "30s" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestUpdateConfigSendsSmartRefreshIntervalPatch(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "config.set" {
			t.Fatalf("command = %q, want config.set", cmd.Command)
		}
		params := string(cmd.Params)
		if !strings.Contains(params, `"smart_refresh_interval":"2h"`) {
			t.Fatalf("params missing smart_refresh_interval: %s", params)
		}
		if strings.Contains(params, `"collector_interval"`) {
			t.Fatalf("params included unset fields: %s", params)
		}
		return jsonResponse(http.StatusOK, `{
			"ok": true,
			"command": "config.set",
			"data": {"version": 1, "collector_interval": "30s", "smart_refresh_interval": "2h", "history": "cpu", "history_retention": "720h", "listeners": []}
		}`), nil
	})

	interval := "2h"
	cfg, _, err := UpdateConfig(context.Background(), apischema.MonitoringConfigPatch{
		SmartRefreshInterval: &interval,
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if cfg.SmartRefreshInterval != "2h" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestUpdateConfigSendsListenersPatch(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "config.set" {
			t.Fatalf("command = %q, want config.set", cmd.Command)
		}
		params := string(cmd.Params)
		if !strings.Contains(params, `"listeners"`) ||
			!strings.Contains(params, `"address":"0.0.0.0:45876"`) ||
			!strings.Contains(params, `"name":"metrics"`) {
			t.Fatalf("params missing listeners patch: %s", params)
		}
		return jsonResponse(http.StatusOK, `{
			"ok": true,
			"command": "config.set",
			"restart_required": true,
			"data": {
				"version": 1,
				"collector_interval": "30s",
				"history": "cpu",
				"history_retention": "720h",
				"listeners": [{"name": "metrics", "address": "0.0.0.0:45876", "apis": ["metrics"]}]
			}
		}`), nil
	})

	cfg, restartRequired, err := UpdateConfig(context.Background(), apischema.MonitoringConfigPatch{
		Listeners: []apischema.MonitoringListener{
			{Name: "metrics", Address: "0.0.0.0:45876", APIs: []string{"metrics"}},
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if !restartRequired {
		t.Fatal("restartRequired = false, want true")
	}
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Address != "0.0.0.0:45876" {
		t.Fatalf("listeners = %#v", cfg.Listeners)
	}
}

func TestUpdateConfigRejectsEmptyPatch(t *testing.T) {
	err := func() error {
		_, _, err := UpdateConfig(context.Background(), apischema.MonitoringConfigPatch{})
		return err
	}()
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("err = %v, want ErrInvalidArgs", err)
	}
}

func TestRunCommandSurfacesAgentError(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		decodeCommandRequest(t, req)
		return jsonResponse(http.StatusBadRequest, `{
			"ok": false,
			"command": "config.set",
			"error": {"code": "invalid_config", "message": "collector_interval must be greater than zero"}
		}`), nil
	})

	interval := "0s"
	_, _, err := UpdateConfig(context.Background(), apischema.MonitoringConfigPatch{
		CollectorInterval: &interval,
	})
	if err == nil || !strings.Contains(err.Error(), "collector_interval must be greater than zero") {
		t.Fatalf("err = %v, want agent message", err)
	}
}

func TestFetchStatusDecodesMeta(t *testing.T) {
	withTestMonitoringClient(t, func(req *http.Request) (*http.Response, error) {
		cmd := decodeCommandRequest(t, req)
		if cmd.Command != "status.get" {
			t.Fatalf("command = %q, want status.get", cmd.Command)
		}
		return jsonResponse(http.StatusOK, `{
			"ok": true,
			"command": "status.get",
			"data": {
				"version": "1.2.3",
				"data_dir": "/var/lib/go-monitoring",
				"db_path": "/var/lib/go-monitoring/metrics.db",
				"collector_interval": "15s",
				"smart_refresh_interval": "12h",
				"listeners": [{"name": "control", "address": "unix:/run/go-monitoring/agent.sock", "effective_address": "unix:/run/go-monitoring/agent.sock", "apis": ["commands"], "active": true}],
				"config": {"path": "/etc/go-monitoring/config.json", "source": "loaded", "version": 1, "collector_interval": "15s", "history_plugins": ["cpu"], "history_retention": "720h"},
				"retention": {"raw": "48h"}
			}
		}`), nil
	})

	status, err := FetchStatus(context.Background())
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if status.Version != "1.2.3" || status.DBPath != "/var/lib/go-monitoring/metrics.db" {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Listeners) != 1 || !status.Listeners[0].Active {
		t.Fatalf("listeners = %#v", status.Listeners)
	}
	if status.Config.Source != "loaded" || status.Retention["raw"] != "48h" {
		t.Fatalf("config meta = %#v retention = %#v", status.Config, status.Retention)
	}
}
