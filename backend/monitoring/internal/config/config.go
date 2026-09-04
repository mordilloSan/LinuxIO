// Package config loads and validates the linuxio-monitoring YAML config.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/app"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/defaults"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
)

const (
	CurrentVersion = 1
	DefaultPath    = "/etc/linuxio/monitoring/config.yaml"
)

// Duration serialises as a Go duration string in YAML and JSON.
type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		return err
	}
	return d.parse(raw)
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	return d.parse(raw)
}

func (d *Duration) parse(raw string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if parsed < 0 {
		return errors.New("duration must not be negative")
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Version   int        `yaml:"version"`
	Collector Collector  `yaml:"collector"`
	History   History    `yaml:"history"`
	Listeners []Listener `yaml:"listeners"`
}

type Collector struct {
	Interval             Duration `yaml:"interval"`
	SmartRefreshInterval Duration `yaml:"smart_refresh_interval"`
	DiskUsageCache       Duration `yaml:"disk_usage_cache"`
}

type History struct {
	Retention Duration            `yaml:"retention"`
	Plugins   []string            `yaml:"plugins"`
	Intervals map[string]Duration `yaml:"intervals,omitempty"` // per-plugin sampling interval; absent means collector.interval
}

type Listener struct {
	Name    string   `yaml:"name" json:"name"`
	Address string   `yaml:"address" json:"address"`
	Plugins []string `yaml:"plugins,omitempty" json:"plugins,omitempty"`
}

// View is the flat JSON shape served by config.get and accepted by config.set.
type View struct {
	Version              int               `json:"version"`
	CollectorInterval    string            `json:"collector_interval"`
	SmartRefreshInterval string            `json:"smart_refresh_interval"`
	DiskUsageCache       string            `json:"disk_usage_cache"`
	HistoryRetention     string            `json:"history_retention"`
	History              string            `json:"history"`
	HistoryIntervals     map[string]string `json:"history_intervals"`
	Listeners            []Listener        `json:"listeners"`
}

const defaultDiskUsageCache = 0 // re-read usage on every collection; set to keep sleeping disks asleep

func Default() Config {
	return Config{
		Version: CurrentVersion,
		Collector: Collector{
			Interval:             Duration(defaults.CollectorInterval),
			SmartRefreshInterval: Duration(defaults.SmartRefreshInterval),
			DiskUsageCache:       Duration(defaultDiskUsageCache),
		},
		History: History{
			Retention: Duration(store.DefaultHistoryRetention()),
			Plugins:   store.DefaultHistoryPluginNames(),
		},
		Listeners: []Listener{},
	}
}

func (c Config) HistoryString() string { return strings.Join(c.History.Plugins, ",") }

// HistoryIntervalDurations returns history.intervals as plain durations.
func (c Config) HistoryIntervalDurations() map[string]time.Duration {
	out := make(map[string]time.Duration, len(c.History.Intervals))
	for plugin, interval := range c.History.Intervals {
		out[plugin] = interval.Duration()
	}
	return out
}

func (c Config) View() View {
	listeners := c.Listeners
	if listeners == nil {
		listeners = []Listener{}
	}
	intervals := make(map[string]string, len(c.History.Intervals))
	for plugin, interval := range c.History.Intervals {
		intervals[plugin] = interval.Duration().String()
	}
	return View{
		Version:              c.Version,
		CollectorInterval:    c.Collector.Interval.Duration().String(),
		SmartRefreshInterval: c.Collector.SmartRefreshInterval.Duration().String(),
		DiskUsageCache:       c.Collector.DiskUsageCache.Duration().String(),
		HistoryRetention:     c.History.Retention.Duration().String(),
		History:              c.HistoryString(),
		HistoryIntervals:     intervals,
		Listeners:            listeners,
	}
}

// Load reads a strict YAML config. An absent file yields Default() and false.
func Load(path string) (Config, bool, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), false, nil
		}
		return Config{}, false, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg, err := decodeStrict(data)
	if err != nil {
		return Config{}, true, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := Validate(cfg); err != nil {
		return Config{}, true, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, true, nil
}

func decodeStrict(data []byte) (Config, error) {
	probe := yaml.NewDecoder(bytes.NewReader(data))
	var document any
	if err := probe.Decode(&document); err != nil {
		return Config{}, err
	}
	if document == nil {
		return Config{}, errors.New("YAML document is empty")
	}
	var extra any
	if err := probe.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, fmt.Errorf("unexpected trailing YAML: %w", err)
		}
		return Config{}, errors.New("multiple YAML documents are not supported")
	}
	cfg := Default()
	if err := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict()).Decode(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.Listeners == nil {
		cfg.Listeners = []Listener{}
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := utils.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func SaveIfMissing(path string, cfg Config) (bool, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat config: %w", err)
	}
	if err := Save(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

var reservedSocketPaths = map[string]struct{}{
	monitoringapi.APISocketPath:     {},
	monitoringapi.ControlSocketPath: {},
}

func Validate(cfg Config) error {
	if cfg.Version != CurrentVersion {
		return fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.Collector.Interval.Duration() <= 0 {
		return errors.New("collector.interval must be greater than zero")
	}
	if cfg.Collector.SmartRefreshInterval.Duration() <= 0 {
		return errors.New("collector.smart_refresh_interval must be greater than zero")
	}
	if cfg.Collector.DiskUsageCache.Duration() < 0 {
		return errors.New("collector.disk_usage_cache must not be negative")
	}
	if cfg.History.Retention.Duration() <= 0 {
		return errors.New("history.retention must be greater than zero")
	}
	if _, err := store.ParseHistoryPlugins(cfg.HistoryString(), true); err != nil {
		return fmt.Errorf("history.plugins: %w", err)
	}
	if _, err := store.HistoryEvery(cfg.HistoryIntervalDurations(), cfg.Collector.Interval.Duration()); err != nil {
		return fmt.Errorf("history.intervals: %w", err)
	}
	return validateListeners(cfg.Listeners)
}

func validateListeners(listeners []Listener) error {
	seenNames := map[string]bool{}
	seenAddresses := map[string]bool{}
	for i, listener := range listeners {
		if err := validateListenerName(i, listener, seenNames); err != nil {
			return err
		}
		if err := validateListenerAddress(i, listener, seenAddresses); err != nil {
			return err
		}
		if err := validateListenerPlugins(i, listener.Plugins); err != nil {
			return err
		}
	}
	return nil
}

func validateListenerName(i int, listener Listener, seenNames map[string]bool) error {
	name := strings.ToLower(strings.TrimSpace(listener.Name))
	if name == "" {
		return fmt.Errorf("listeners[%d].name cannot be empty", i)
	}
	if name == "api" || name == "control" {
		return fmt.Errorf("listeners[%d].name %q is reserved", i, listener.Name)
	}
	if seenNames[name] {
		return fmt.Errorf("duplicate listener name %q", listener.Name)
	}
	seenNames[name] = true
	return nil
}

func validateListenerAddress(i int, listener Listener, seenAddresses map[string]bool) error {
	address := strings.TrimSpace(listener.Address)
	if address == "" {
		return fmt.Errorf("listeners[%d].address cannot be empty", i)
	}
	if app.IsListenDisabled(address) {
		return fmt.Errorf("listeners[%d].address cannot be disabled", i)
	}
	network, addr := app.SplitListenAddress(app.GetAddress(address))
	if network == "unix" {
		if _, reserved := reservedSocketPaths[addr]; reserved {
			return fmt.Errorf("listeners[%d].address %q is reserved for LinuxIO", i, address)
		}
	}
	key := network + ":" + addr
	if network == "tcp" {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("listeners[%d].address %q: %w", i, listener.Address, err)
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("listeners[%d].address %q: port must be between 1 and 65535", i, listener.Address)
		}
		key = "tcp:" + net.JoinHostPort(host, port)
	}
	if seenAddresses[key] {
		return fmt.Errorf("duplicate listener address %q", listener.Address)
	}
	seenAddresses[key] = true
	return nil
}

func validateListenerPlugins(i int, plugins []string) error {
	seenPlugins := map[string]bool{}
	for _, plugin := range plugins {
		plugin = strings.ToLower(strings.TrimSpace(plugin))
		if !store.IsPluginName(plugin) {
			return fmt.Errorf("listeners[%d].plugins: unknown plugin %q", i, plugin)
		}
		if seenPlugins[plugin] {
			return fmt.Errorf("listeners[%d].plugins: duplicate plugin %q", i, plugin)
		}
		seenPlugins[plugin] = true
	}
	return nil
}
