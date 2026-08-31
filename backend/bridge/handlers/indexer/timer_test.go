package indexer

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
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

func TestSetTimerIntervalUpdatesConfigAndSystemd(t *testing.T) {
	originalUpdate := updateTimerConfig
	originalWrite := writeTimerDropIn
	originalRemove := removeTimerDropIn
	originalEnable := enableTimerUnit
	originalRestart := restartTimerUnit
	t.Cleanup(func() {
		updateTimerConfig = originalUpdate
		writeTimerDropIn = originalWrite
		removeTimerDropIn = originalRemove
		enableTimerUnit = originalEnable
		restartTimerUnit = originalRestart
	})

	var patch, dropIn, dropInPath string
	var removed []string
	var units []string
	updateTimerConfig = func(_ context.Context, payload []byte) (indexerapi.IndexerConfig, bool, error) {
		patch = string(payload)
		return indexerapi.IndexerConfig{Interval: "30m0s"}, false, nil
	}
	writeTimerDropIn = func(path string, data []byte, _ fs.FileMode) error {
		dropInPath = path
		dropIn = string(data)
		return nil
	}
	removeTimerDropIn = func(path string) error {
		removed = append(removed, path)
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
	wantDropIn := "[Timer]\nOnActiveSec=\nOnUnitActiveSec=\nOnActiveSec=30m0s\nOnUnitActiveSec=30m0s\n"
	if patch != `{"interval":"30m0s"}` || dropInPath != indexerTimerDropInPath || dropIn != wantDropIn {
		t.Fatalf("patch=%s path=%q drop-in=%q", patch, dropInPath, dropIn)
	}
	if strings.Join(removed, ",") != indexerLegacyTimerDropInPath {
		t.Fatalf("removed paths = %v", removed)
	}
	if strings.Join(units, ",") != "enable linuxio-indexer-index.timer,restart linuxio-indexer-index.timer" {
		t.Fatalf("systemd calls = %v", units)
	}
	if result.Interval != "30m0s" || result.TimerUnit != indexerTimerUnitName {
		t.Fatalf("result = %#v", result)
	}
}

func TestSetTimerIntervalDisablesAndRemovesBothDropIns(t *testing.T) {
	originalUpdate := updateTimerConfig
	originalRemove := removeTimerDropIn
	originalDisable := disableTimerUnit
	originalStop := stopTimerUnit
	t.Cleanup(func() {
		updateTimerConfig = originalUpdate
		removeTimerDropIn = originalRemove
		disableTimerUnit = originalDisable
		stopTimerUnit = originalStop
	})

	var removed, units []string
	updateTimerConfig = func(_ context.Context, _ []byte) (indexerapi.IndexerConfig, bool, error) {
		return indexerapi.IndexerConfig{Interval: "0"}, false, nil
	}
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
	if strings.Join(removed, ",") != indexerTimerDropInPath+","+indexerLegacyTimerDropInPath {
		t.Fatalf("removed paths = %v", removed)
	}
	if strings.Join(units, ",") != "disable "+indexerTimerUnitName+",stop "+indexerTimerUnitName {
		t.Fatalf("systemd calls = %v", units)
	}
}
