package verbosemode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/logging"
)

func TestVerbosemodeEnabledRequiresEveryDropin(t *testing.T) {
	prev := dropinRoot
	dropinRoot = t.TempDir()
	t.Cleanup(func() { dropinRoot = prev })

	if Enabled() || Partial() {
		t.Fatal("missing drop-ins reported enabled")
	}
	paths := Paths()
	for i, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		wantEnabled := i == len(paths)-1
		if Enabled() != wantEnabled || !Partial() {
			t.Fatalf("after %d drop-ins: enabled=%v partial=%v", i+1, Enabled(), Partial())
		}
	}
}

func TestReloadOnSIGHUPAppliesPersistedState(t *testing.T) {
	prev := dropinRoot
	dropinRoot = t.TempDir()
	t.Cleanup(func() { dropinRoot = prev; logging.SetDebug(false) })
	for _, path := range Paths() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ReloadOnSIGHUP(ctx)
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !logging.DebugEnabled() {
		if time.Now().After(deadline) {
			t.Fatal("SIGHUP did not enable debug logging")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReloadOnSIGHUPWithoutDropinsStaysInfo(t *testing.T) {
	prev := dropinRoot
	dropinRoot = t.TempDir()
	t.Cleanup(func() { dropinRoot = prev; logging.SetDebug(false) })
	logging.SetDebug(true)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ReloadOnSIGHUP(ctx)
	for range 2 {
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for logging.DebugEnabled() {
		if time.Now().After(deadline) {
			t.Fatal("SIGHUP without drop-ins left debug logging enabled")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Let a coalesced second signal drain, then confirm the level held.
	time.Sleep(50 * time.Millisecond)
	if logging.DebugEnabled() {
		t.Fatal("debug logging re-enabled after second SIGHUP")
	}
}

func TestReloadDaemonsSignalsEveryUnitWebserverLast(t *testing.T) {
	prev := reloadUnit
	t.Cleanup(func() { reloadUnit = prev })
	var order []string
	failure := errors.New("reload failed")
	reloadUnit = func(_ context.Context, unit string) error {
		order = append(order, unit)
		if unit == "linuxio-indexer.service" {
			return failure
		}
		return nil
	}

	err := ReloadDaemons(t.Context())
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want it to wrap the indexer failure", err)
	}
	want := []string{"linuxio-monitoring.service", "linuxio-indexer.service", "linuxio-webserver.service"}
	if !slices.Equal(order, want) {
		t.Fatalf("reload order = %v, want %v", order, want)
	}
}
