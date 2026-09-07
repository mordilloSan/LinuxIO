package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAbsentFileReturnsDefaults(t *testing.T) {
	cfg, loaded, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || loaded {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	if cfg.Collector.Interval != Duration(time.Minute) || cfg.History.Retention != Duration(720*time.Hour) {
		t.Fatalf("defaults = %+v", cfg)
	}
	if len(cfg.Listeners) != 0 {
		t.Fatalf("default listeners must be empty, got %v", cfg.Listeners)
	}
}

func TestLoadParsesNestedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
collector:
  interval: 30s
  smart_refresh_interval: 2h
  disk_usage_cache: 10m
history:
  retention: 48h
  plugins: [cpu, mem]
listeners:
  - name: homepage
    address: 0.0.0.0:45876
    plugins: [cpu, network]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, loaded, err := Load(path)
	if err != nil || !loaded {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	if cfg.Collector.Interval != Duration(30*time.Second) || cfg.Collector.DiskUsageCache != Duration(10*time.Minute) {
		t.Fatalf("collector = %+v", cfg.Collector)
	}
	if cfg.HistoryString() != "cpu,mem" {
		t.Fatalf("history = %q", cfg.HistoryString())
	}
	if len(cfg.Listeners) != 1 || cfg.Listeners[0].Plugins[1] != "network" {
		t.Fatalf("listeners = %+v", cfg.Listeners)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(path, []byte("version: 1\nallow_remote_commands: true\n"), 0o644)
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "allow_remote_commands") {
		t.Fatalf("expected unknown key error, got %v", err)
	}
}

func TestValidateRejectsBadListeners(t *testing.T) {
	cfg := Default()
	cfg.Listeners = []Listener{{Name: "a", Address: "127.0.0.1:1", Plugins: []string{"nope"}}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "unknown plugin") {
		t.Fatalf("plugin validation: %v", err)
	}
	cfg.Listeners = []Listener{{Name: "a", Address: "127.0.0.1:1"}, {Name: "a", Address: "127.0.0.1:2"}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "duplicate listener name") {
		t.Fatalf("name validation: %v", err)
	}
	cfg.Listeners = []Listener{{Name: "a", Address: "unix:/run/linuxio/monitoring/api.sock"}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved socket validation: %v", err)
	}
	for _, address := range []string{"127.0.0.1:99999", "127.0.0.1:0", ":http"} {
		cfg.Listeners = []Listener{{Name: "a", Address: address}}
		if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "port must be between 1 and 65535") {
			t.Fatalf("port validation for %q: %v", address, err)
		}
	}
	cfg.Listeners = []Listener{{Name: "a", Address: "[::1]"}}
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "missing port") {
		t.Fatalf("missing port validation: %v", err)
	}
	cfg.Listeners = []Listener{{Name: "a", Address: "9000"}, {Name: "b", Address: ":9001"}}
	if err := Validate(cfg); err != nil {
		t.Fatalf("bare and host-less ports must stay valid: %v", err)
	}
	cfg.Listeners = nil
	cfg.Collector.Interval = 0
	if err := Validate(cfg); err == nil {
		t.Fatal("zero interval must fail")
	}
}

func TestSaveRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg := Default()
	cfg.Collector.Interval = Duration(15 * time.Second)
	cfg.Listeners = []Listener{{Name: "lan", Address: ":9000", Plugins: []string{"cpu"}}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, loaded, err := Load(path)
	if err != nil || !loaded {
		t.Fatalf("reload: loaded=%v err=%v", loaded, err)
	}
	if again.Collector.Interval != cfg.Collector.Interval || again.Listeners[0].Plugins[0] != "cpu" {
		t.Fatalf("round trip mismatch: %+v", again)
	}
	created, err := SaveIfMissing(path, Default())
	if err != nil || created {
		t.Fatalf("SaveIfMissing on existing: created=%v err=%v", created, err)
	}
}

func TestViewFlattens(t *testing.T) {
	view := Default().View()
	if view.CollectorInterval != "1m0s" || view.History != strings.Join(Default().History.Plugins, ",") || view.Listeners == nil {
		t.Fatalf("view = %+v", view)
	}
}

func TestLoadParsesHistoryIntervals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
collector:
  interval: 1m
history:
  plugins: [cpu, containers]
  intervals:
    containers: 5m
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.History.Intervals["containers"].Duration(); got != 5*time.Minute {
		t.Fatalf("containers interval = %s", got)
	}
	view := cfg.View()
	if view.HistoryIntervals["containers"] != "5m0s" || len(view.HistoryIntervals) != 1 {
		t.Fatalf("view intervals = %+v", view.HistoryIntervals)
	}
	if Default().View().HistoryIntervals == nil {
		t.Fatal("default view intervals must be an empty object, not null")
	}
}

func TestValidateRejectsHistoryIntervalNotMultipleOfTick(t *testing.T) {
	cfg := Default()
	cfg.History.Intervals = map[string]Duration{"mem": Duration(90 * time.Second)}
	err := Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "history.intervals") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveRoundTripsHistoryIntervals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.History.Intervals = map[string]Duration{"containers": Duration(5 * time.Minute), "sensors": Duration(15 * time.Minute)}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.History.Intervals) != 2 || again.History.Intervals["sensors"] != Duration(15*time.Minute) {
		t.Fatalf("round trip intervals = %+v", again.History.Intervals)
	}
	if got := again.HistoryIntervalDurations(); got["containers"] != 5*time.Minute {
		t.Fatalf("durations = %+v", got)
	}
}
