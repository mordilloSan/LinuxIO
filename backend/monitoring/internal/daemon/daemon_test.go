package daemon

import (
	"testing"
	"time"

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
	if api.Address != "unix:"+monitoringapi.APISocketPath || api.Mode != 0o666 || api.RootOnly || api.Plugins != nil || len(api.APIs) != 1 || api.APIs[0] != "metrics" {
		t.Fatalf("api = %+v", api)
	}
	if control.Address != "unix:"+monitoringapi.ControlSocketPath || control.Mode != 0o600 || !control.RootOnly || len(control.APIs) != 2 {
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

func TestRunOptionsMapsEmptyHistoryToNone(t *testing.T) {
	cfg := config.Default()
	cfg.History.Plugins = []string{}
	opts := runOptions(cfg, "loaded")
	if opts.History != "none" {
		t.Fatalf("History = %q, want \"none\"", opts.History)
	}
	if !opts.HistorySet {
		t.Fatal("HistorySet = false, want true")
	}
}

func TestRunOptionsCarriesHistoryIntervals(t *testing.T) {
	cfg := config.Default()
	cfg.History.Intervals = map[string]config.Duration{"containers": config.Duration(5 * time.Minute)}
	opts := runOptions(cfg, "loaded")
	if opts.HistoryIntervals["containers"] != 5*time.Minute || len(opts.HistoryIntervals) != 1 {
		t.Fatalf("HistoryIntervals = %+v", opts.HistoryIntervals)
	}
}
