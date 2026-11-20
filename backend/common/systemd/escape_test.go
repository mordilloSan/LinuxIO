package systemd

import (
	"context"
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient/testdbus"
)

// The expected names are what systemd-escape --path prints.
func TestEscapePath(t *testing.T) {
	tests := map[string]string{
		"/swap.img":               "swap.img",
		"/":                       "-",
		"/dev/disk/by-uuid/ab-cd": `dev-disk-by\x2duuid-ab\x2dcd`,
		"/.hidden/x":              `\x2ehidden-x`,
		"/a/.b":                   "a-.b",
		"//a//b/":                 "a-b",
		"/a/./b":                  "a-b",
		"/a b/c":                  `a\x20b-c`,
		"/über":                   `\xc3\xbcber`,
		`/a\b`:                    `a\x5cb`,
		"/x:y_z.w":                "x:y_z.w",
		"/home/swapfile":          "home-swapfile",
	}
	for path, want := range tests {
		got, err := EscapePath(path)
		if err != nil || got != want {
			t.Errorf("EscapePath(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

// systemd refuses to name a unit after a path it cannot normalize, and a
// relative path names no unit.
func TestEscapePathRefusesPathsSystemdWouldNot(t *testing.T) {
	for _, path := range []string{"/a/../b", "..", "a/b", ""} {
		if got, err := EscapePath(path); err == nil {
			t.Errorf("EscapePath(%q) = %q, want an error", path, got)
		}
	}
}

// requireUnitName passes escaped names through untouched, so the manager is
// asked for exactly the unit systemd-fstab-generator wrote.
func TestEscapedSwapUnitReachesTheManagerUnchanged(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	unit := `dev-disk-by\x2duuid-ab\x2dcd.swap`
	if err := StopUnit(context.Background(), unit); err != nil {
		t.Fatalf("StopUnit: %v", err)
	}
	calls := manager.Calls()
	if len(calls) != 1 || calls[0].Method != "StopUnit" || !reflect.DeepEqual(calls[0].Args, []any{unit, "replace"}) {
		t.Fatalf("calls = %#v, want StopUnit(%q, replace)", calls, unit)
	}
}
