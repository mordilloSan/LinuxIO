package indexer

import (
	"context"
	"io/fs"
	"strings"
	"testing"
)

func TestNormalizeTimerInterval(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "0", want: "0"},
		{raw: "0s", want: "0"},
		{raw: " 30m ", want: "30m0s"},
		{raw: "-1s", wantErr: true},
		{raw: "soon", wantErr: true},
	}
	for _, test := range tests {
		got, err := normalizeTimerInterval(test.raw)
		if test.wantErr {
			if err == nil {
				t.Fatalf("normalizeTimerInterval(%q) returned nil", test.raw)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("normalizeTimerInterval(%q) = %q, %v; want %q", test.raw, got, err, test.want)
		}
	}
}

func TestSetTimerIntervalUpdatesSystemd(t *testing.T) {
	originalWrite := writeTimerDropIn
	originalRemove := removeTimerDropIn
	originalEnable := enableTimerUnit
	originalRestart := restartTimerUnit
	t.Cleanup(func() {
		writeTimerDropIn = originalWrite
		removeTimerDropIn = originalRemove
		enableTimerUnit = originalEnable
		restartTimerUnit = originalRestart
	})

	var dropIn, dropInPath string
	var units []string
	writeTimerDropIn = func(path string, data []byte, _ fs.FileMode, _ ...int) error {
		dropInPath = path
		dropIn = string(data)
		return nil
	}
	removeTimerDropIn = func(path string) error {
		t.Fatalf("unexpected removal of %s", path)
		return nil
	}
	enableTimerUnit = func(_ context.Context, unit string) error {
		units = append(units, "enable "+unit)
		return nil
	}
	restartTimerUnit = func(_ context.Context, unit string) error {
		units = append(units, "restart "+unit)
		return nil
	}

	result, err := SetTimerInterval(context.Background(), "30m")
	if err != nil {
		t.Fatalf("SetTimerInterval: %v", err)
	}
	wantDropIn := "[Timer]\nOnActiveSec=\nOnActiveSec=30m0s\nOnUnitActiveSec=\nOnUnitActiveSec=30m0s\n"
	if dropInPath != indexerTimerDropInPath || dropIn != wantDropIn {
		t.Fatalf("path=%q drop-in=%q", dropInPath, dropIn)
	}
	if strings.Join(units, ",") != "enable linuxio-indexer-index.timer,restart linuxio-indexer-index.timer" {
		t.Fatalf("systemd calls = %v", units)
	}
	if result.Interval != "30m0s" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSetTimerIntervalDisablesAndRemovesDropIn(t *testing.T) {
	originalRemove := removeTimerDropIn
	originalDisable := disableTimerUnit
	originalStop := stopTimerUnit
	t.Cleanup(func() {
		removeTimerDropIn = originalRemove
		disableTimerUnit = originalDisable
		stopTimerUnit = originalStop
	})

	var removed, units []string
	removeTimerDropIn = func(path string) error {
		removed = append(removed, path)
		return nil
	}
	disableTimerUnit = func(_ context.Context, unit string) error {
		units = append(units, "disable "+unit)
		return nil
	}
	stopTimerUnit = func(_ context.Context, unit string) error {
		units = append(units, "stop "+unit)
		return nil
	}

	if _, err := SetTimerInterval(context.Background(), "0"); err != nil {
		t.Fatalf("SetTimerInterval: %v", err)
	}
	if strings.Join(removed, ",") != indexerTimerDropInPath {
		t.Fatalf("removed paths = %v", removed)
	}
	if strings.Join(units, ",") != "disable "+indexerTimerUnitName+",stop "+indexerTimerUnitName {
		t.Fatalf("systemd calls = %v", units)
	}
}
