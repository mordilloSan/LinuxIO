package storage

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const testBtrfsUUID = "9a2b4c6d-1e3f-4a5b-8c7d-9e0f1a2b3c4d"

func TestBtrfsErrorOccurrence(t *testing.T) {
	healthy := btrfs.BtrfsFilesystem{Devices: []btrfs.BtrfsDevice{{ID: 1, Path: "/dev/sda1"}}}
	if got := btrfsErrorOccurrence(healthy); got != "" {
		t.Fatalf("healthy occurrence = %q", got)
	}
	failing := btrfs.BtrfsFilesystem{
		Degraded: true,
		Devices: []btrfs.BtrfsDevice{
			{ID: 1, Path: "/dev/sda1", Errors: btrfs.BtrfsDeviceErrors{Read: 2, Corruption: 3}},
			{ID: 2, Missing: true, Errors: btrfs.BtrfsDeviceErrors{Write: 5, Generation: 1}},
		},
	}
	if got, want := btrfsErrorOccurrence(failing), "1:r2,c3;2:w5,g1;missing"; got != want {
		t.Fatalf("occurrence = %q, want %q", got, want)
	}
}

func TestBtrfsErrorOccurrenceManyDevicesFitsAlertLimit(t *testing.T) {
	fs := btrfs.BtrfsFilesystem{HealthKnown: true}
	for i := 1; i <= 16; i++ {
		fs.Devices = append(fs.Devices, btrfs.BtrfsDevice{ID: i, Errors: btrfs.BtrfsDeviceErrors{Write: 1000000, Read: 1000000, Corruption: 1000000}})
	}
	if got := btrfsErrorOccurrence(fs); len(got) == 0 || len(got) > maxOccurrenceLen {
		t.Fatalf("occurrence length = %d", len(got))
	}

	old := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = old })
	fs.UUID = testBtrfsUUID
	raiseBtrfsHealthAlerts(context.Background(), fs)
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 {
		t.Fatalf("alerts = %+v err=%v", doc.Alerts, err)
	}
}

func TestRaiseBtrfsHealthAlertsSkipsUnknownHealth(t *testing.T) {
	old := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = old })

	failing := btrfs.BtrfsFilesystem{UUID: testBtrfsUUID, Degraded: true, HealthKnown: true}
	raiseBtrfsHealthAlerts(context.Background(), failing)
	before, err := alerts.Load()
	if err != nil || len(before.Alerts) != 1 {
		t.Fatalf("raise: alerts=%+v err=%v", before.Alerts, err)
	}

	// A failed stats command leaves health unknown: no resolve, no re-raise.
	raiseBtrfsHealthAlerts(context.Background(), btrfs.BtrfsFilesystem{UUID: testBtrfsUUID})
	after, err := alerts.Load()
	if err != nil || len(after.Alerts) != 1 || !reflect.DeepEqual(after.Alerts, before.Alerts) {
		t.Fatalf("unknown health changed alerts: %+v err=%v", after.Alerts, err)
	}

	raiseBtrfsHealthAlerts(context.Background(), btrfs.BtrfsFilesystem{UUID: testBtrfsUUID, HealthKnown: true})
	resolved, _ := alerts.Load()
	if len(resolved.Alerts) != 0 {
		t.Fatalf("healthy filesystem should resolve the alert: %+v", resolved.Alerts)
	}
}
