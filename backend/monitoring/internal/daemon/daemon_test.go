package daemon

import (
	"encoding/json/v2"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
)

func TestListenersAlwaysIncludeFixedSockets(t *testing.T) {
	cfg := config.Default()
	cfg.Listeners = []config.Listener{{Name: "homepage", Address: "0.0.0.0:45876", Plugins: []string{"cpu"}}}
	listeners := Listeners(cfg)
	if len(listeners) != 3 {
		t.Fatalf("listeners = %+v", listeners)
	}
	api, control, homepage := listeners[0], listeners[1], listeners[2]
	if api.Address != "unix:"+monitoringapi.APISocketPath || api.Mode != config.APISocketMode || api.RootOnly || api.Plugins != nil || len(api.APIs) != 1 || api.APIs[0] != "metrics" {
		t.Fatalf("api = %+v", api)
	}
	if control.Address != "unix:"+monitoringapi.ControlSocketPath || control.Mode != config.ControlSocketMode || !control.RootOnly || len(control.APIs) != 2 {
		t.Fatalf("control = %+v", control)
	}
	if homepage.Address != "0.0.0.0:45876" || homepage.RootOnly || len(homepage.APIs) != 1 || homepage.APIs[0] != "metrics" || homepage.Plugins[0] != "cpu" {
		t.Fatalf("homepage = %+v", homepage)
	}
	// A configured address that cannot bind must not take the fixed sockets
	// down with it: they are the only way to fix the address.
	if !homepage.BestEffort {
		t.Fatal("configured listener must be best-effort")
	}
	if api.BestEffort || control.BestEffort {
		t.Fatalf("fixed sockets must stay fatal: api=%v control=%v", api.BestEffort, control.BestEffort)
	}
}

func TestListenersEmptyPluginListMeansAllPlugins(t *testing.T) {
	cfg := config.Default()
	cfg.Listeners = []config.Listener{{Name: "homepage", Address: "0.0.0.0:45876", Plugins: []string{}}}
	listeners := Listeners(cfg)
	// The server reads a nil allowlist as "every metrics plugin"; an empty
	// configured list must reach it as nil, not as an empty allowlist.
	if listeners[2].Plugins != nil {
		t.Fatalf("Plugins = %#v, want nil", listeners[2].Plugins)
	}
}

func TestRuntimeSettingsMapsEmptyHistoryToNone(t *testing.T) {
	cfg := config.Default()
	cfg.History.Plugins = []string{}
	opts := runtimeSettings(cfg, "loaded")
	if opts.History != "none" {
		t.Fatalf("History = %q, want \"none\"", opts.History)
	}
}

func TestRuntimeSettingsCarriesHistoryIntervals(t *testing.T) {
	cfg := config.Default()
	cfg.History.Intervals = map[string]config.Duration{"containers": config.Duration(5 * time.Minute)}
	opts := runtimeSettings(cfg, "loaded")
	if opts.HistoryIntervals["containers"] != 5*time.Minute || len(opts.HistoryIntervals) != 1 {
		t.Fatalf("HistoryIntervals = %+v", opts.HistoryIntervals)
	}
}

// durationFields returns the config.Duration fields of v, a config.Config or
// one of its sections, keyed by their YAML path.
func durationFields(v reflect.Value, prefix string) map[string]reflect.Value {
	fields := map[string]reflect.Value{}
	for i := range v.NumField() {
		field := v.Type().Field(i)
		path := prefix + strings.Split(field.Tag.Get("yaml"), ",")[0]
		switch {
		case field.Type == reflect.TypeFor[config.Duration]():
			fields[path] = v.Field(i)
		case field.Type.Kind() == reflect.Struct:
			maps.Copy(fields, durationFields(v.Field(i), path+"."))
		}
	}
	return fields
}

func durations(cfg config.Config) map[string]time.Duration {
	out := map[string]time.Duration{}
	for path, field := range durationFields(reflect.ValueOf(cfg), "") {
		out[path] = time.Duration(field.Int())
	}
	return out
}

// Every duration in the config file must have a row in config.DurationSettings,
// reach config.get, survive a config.set round trip and reach the settings the
// app runs with. Each one gets a distinct non-default value, so a step that
// leaves one out loses it.
func TestEveryDurationSettingIsCarriedThrough(t *testing.T) {
	defaults := durations(config.Default())
	cfg := config.Default()
	fields := durationFields(reflect.ValueOf(&cfg).Elem(), "")
	require.NotEmpty(t, fields)

	listed := make([]string, 0, len(config.DurationSettings))
	for _, setting := range config.DurationSettings {
		listed = append(listed, setting.Path)
		if field, ok := fields[setting.Path]; assert.True(t, ok, "%s is not a duration in the config file", setting.Path) {
			assert.Same(t, field.Addr().Interface(), setting.Field(&cfg), "%s field", setting.Path)
		}
	}
	assert.ElementsMatch(t, slices.Collect(maps.Keys(fields)), listed, "config.DurationSettings")

	want := map[string]time.Duration{}
	for i, path := range slices.Sorted(maps.Keys(fields)) {
		value := time.Duration(i+2) * 17 * time.Minute
		require.NotEqual(t, defaults[path], value, path)
		fields[path].SetInt(int64(value))
		want[path] = value
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, config.Save(configPath, cfg))
	loaded, _, err := config.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, want, durations(loaded), "config file")

	encoded, err := json.Marshal(cfg.View())
	require.NoError(t, err)
	var members map[string]any
	require.NoError(t, json.Unmarshal(encoded, &members))
	for path, value := range want {
		var names []string
		for name, member := range members {
			if member == value.String() {
				names = append(names, name)
			}
		}
		assert.Len(t, names, 1, "%s in config.get", path)
	}

	// The config.get result sent back as config.set parameters.
	var patch monitoringapi.MonitoringConfigPatch
	require.NoError(t, json.Unmarshal(encoded, &patch))
	patched := config.Default()
	_, err = applyConfigSetParams(&patched, patch)
	require.NoError(t, err)
	assert.Equal(t, want, durations(patched), "config.set")

	settings := reflect.ValueOf(runtimeSettings(cfg, "loaded"))
	var carried []time.Duration
	for _, field := range reflect.VisibleFields(settings.Type()) {
		if field.Type == reflect.TypeFor[time.Duration]() {
			carried = append(carried, time.Duration(settings.FieldByIndex(field.Index).Int()))
		}
	}
	assert.ElementsMatch(t, slices.Collect(maps.Values(want)), carried, "app.Settings")
}
