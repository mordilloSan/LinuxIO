// Package config loads and validates the linuxio-monitoring YAML config.
package config

import (
	"bytes"
	"encoding/json/v2"
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

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
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

// Durations holds the duration settings as plain durations, the form the app
// runs with.
type Durations struct {
	CollectorInterval    time.Duration
	SmartRefreshInterval time.Duration
	DiskUsageCache       time.Duration
	HistoryRetention     time.Duration
}

// DurationSetting describes one duration setting to the code that handles
// every duration alike: Default, Validate, View, config.set, Config.Durations
// and Durations.WithDefaults.
type DurationSetting struct {
	// Path names the setting in the config file and in validation errors.
	Path string
	// Field, Runtime, View and Patch return the setting's member in the config
	// file, in Durations, in the config.get result and in the config.set
	// parameters, where nil means the patch leaves it alone.
	Field   func(*Config) *Duration
	Runtime func(*Durations) *time.Duration
	View    func(*monitoringapi.MonitoringConfig) *string
	Patch   func(*monitoringapi.MonitoringConfigPatch) *string
	Default time.Duration
	// AllowZero accepts zero; otherwise the setting must be greater than zero.
	// A setting is never negative.
	AllowZero bool
}

// DurationSettings lists every duration setting in the order validation checks
// them and config.set reports their errors. A new one needs its field in
// Collector or History, a row here, its members in Durations,
// MonitoringConfig and MonitoringConfigPatch, and the step that applies it in
// the app.
var DurationSettings = []DurationSetting{
	{
		Path:    "collector.interval",
		Field:   func(c *Config) *Duration { return &c.Collector.Interval },
		Runtime: func(d *Durations) *time.Duration { return &d.CollectorInterval },
		View:    func(v *monitoringapi.MonitoringConfig) *string { return &v.CollectorInterval },
		Patch:   func(p *monitoringapi.MonitoringConfigPatch) *string { return p.CollectorInterval },
		Default: DefaultCollectorInterval,
	},
	{
		Path:    "collector.smart_refresh_interval",
		Field:   func(c *Config) *Duration { return &c.Collector.SmartRefreshInterval },
		Runtime: func(d *Durations) *time.Duration { return &d.SmartRefreshInterval },
		View:    func(v *monitoringapi.MonitoringConfig) *string { return &v.SmartRefreshInterval },
		Patch:   func(p *monitoringapi.MonitoringConfigPatch) *string { return p.SmartRefreshInterval },
		Default: DefaultSmartRefreshInterval,
	},
	{
		Path:      "collector.disk_usage_cache",
		Field:     func(c *Config) *Duration { return &c.Collector.DiskUsageCache },
		Runtime:   func(d *Durations) *time.Duration { return &d.DiskUsageCache },
		View:      func(v *monitoringapi.MonitoringConfig) *string { return &v.DiskUsageCache },
		Patch:     func(p *monitoringapi.MonitoringConfigPatch) *string { return p.DiskUsageCache },
		Default:   DefaultDiskUsageCache,
		AllowZero: true,
	},
	{
		Path:    "history.retention",
		Field:   func(c *Config) *Duration { return &c.History.Retention },
		Runtime: func(d *Durations) *time.Duration { return &d.HistoryRetention },
		View:    func(v *monitoringapi.MonitoringConfig) *string { return &v.HistoryRetention },
		Patch:   func(p *monitoringapi.MonitoringConfigPatch) *string { return p.HistoryRetention },
		Default: DefaultHistoryRetention,
	},
}

func (s DurationSetting) validate(cfg *Config) error {
	value := s.Field(cfg).Duration()
	switch {
	case s.AllowZero && value < 0:
		return fmt.Errorf("%s must not be negative", s.Path)
	case !s.AllowZero && value <= 0:
		return fmt.Errorf("%s must be greater than zero", s.Path)
	}
	return nil
}

func Default() Config {
	cfg := Config{
		Version:   CurrentVersion,
		History:   History{Plugins: plugins.DefaultHistory()},
		Listeners: []Listener{},
	}
	for _, setting := range DurationSettings {
		*setting.Field(&cfg) = Duration(setting.Default)
	}
	return cfg
}

// Durations returns the duration settings as the app runs with them.
func (c Config) Durations() Durations {
	var d Durations
	for _, setting := range DurationSettings {
		*setting.Runtime(&d) = setting.Field(&c).Duration()
	}
	return d
}

// WithDefaults replaces each setting that must be greater than zero but is not
// with its default.
func (d Durations) WithDefaults() Durations {
	for _, setting := range DurationSettings {
		if value := setting.Runtime(&d); !setting.AllowZero && *value <= 0 {
			*value = setting.Default
		}
	}
	return d
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

// View returns the flat shape served by config.get and returned by config.set.
func (c Config) View() monitoringapi.MonitoringConfig {
	listeners := make([]monitoringapi.MonitoringListener, 0, len(c.Listeners))
	for _, listener := range c.Listeners {
		listeners = append(listeners, monitoringapi.MonitoringListener{Address: listener.Address, Name: listener.Name, Plugins: listener.Plugins})
	}
	intervals := make(map[string]string, len(c.History.Intervals))
	for plugin, interval := range c.History.Intervals {
		intervals[plugin] = interval.Duration().String()
	}
	view := monitoringapi.MonitoringConfig{
		History:          c.HistoryString(),
		HistoryIntervals: intervals,
		Listeners:        listeners,
		Version:          c.Version,
	}
	for _, setting := range DurationSettings {
		*setting.View(&view) = setting.Field(&c).Duration().String()
	}
	return view
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
	if err := atomicfile.Write(path, data, 0o644); err != nil {
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
	for _, setting := range DurationSettings {
		if err := setting.validate(&cfg); err != nil {
			return err
		}
	}
	if _, err := plugins.ParseHistory(cfg.HistoryString()); err != nil {
		return fmt.Errorf("history.plugins: %w", err)
	}
	if _, err := plugins.HistoryEvery(cfg.HistoryIntervalDurations(), cfg.Collector.Interval.Duration()); err != nil {
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
	if IsListenDisabled(address) {
		return fmt.Errorf("listeners[%d].address cannot be disabled", i)
	}
	network, addr := SplitListenAddress(GetAddress(address))
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

func validateListenerPlugins(i int, names []string) error {
	seenPlugins := map[string]bool{}
	for _, plugin := range names {
		plugin = strings.ToLower(strings.TrimSpace(plugin))
		if !plugins.IsName(plugin) {
			return fmt.Errorf("listeners[%d].plugins: unknown plugin %q", i, plugin)
		}
		if seenPlugins[plugin] {
			return fmt.Errorf("listeners[%d].plugins: duplicate plugin %q", i, plugin)
		}
		seenPlugins[plugin] = true
	}
	return nil
}
