package btrfs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func stubAlerts(t *testing.T) {
	t.Helper()
	old := alerts.Path
	alerts.Path = filepath.Join(t.TempDir(), "alerts.json")
	t.Cleanup(func() { alerts.Path = old })
}

func alertKeys(t *testing.T) map[string]string {
	t.Helper()
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	for _, a := range doc.Alerts {
		keys[a.Key] = a.Severity
	}
	return keys
}

func writeProgsStatus(text string) error {
	return os.WriteFile(filepath.Join(progsScrubDir, "scrub.status."+dataUUID), []byte(text), 0o644)
}

func TestErrorOccurrence(t *testing.T) {
	healthy := BtrfsFilesystem{Devices: []BtrfsDevice{{ID: 1, Path: "/dev/sda1"}}}
	if got := errorOccurrence(healthy); got != "" {
		t.Fatalf("healthy occurrence = %q", got)
	}
	failing := BtrfsFilesystem{
		Degraded: true,
		Devices: []BtrfsDevice{
			{ID: 1, Path: "/dev/sda1", Errors: BtrfsDeviceErrors{Read: 2, Corruption: 3}},
			{ID: 2, Missing: true, Errors: BtrfsDeviceErrors{Write: 5, Generation: 1}},
		},
	}
	if got, want := errorOccurrence(failing), "1:r2,c3;2:w5,g1;missing"; got != want {
		t.Fatalf("occurrence = %q, want %q", got, want)
	}
	many := BtrfsFilesystem{HealthKnown: true}
	for i := 1; i <= 16; i++ {
		many.Devices = append(many.Devices, BtrfsDevice{ID: i, Errors: BtrfsDeviceErrors{Write: 1000000, Read: 1000000, Corruption: 1000000}})
	}
	if got := errorOccurrence(many); len(got) == 0 || len(got) > maxOccurrenceLen {
		t.Fatalf("occurrence length = %d", len(got))
	}
}

func TestRaiseAlertsRaisesAndResolves(t *testing.T) {
	stubAlerts(t)
	ctx := context.Background()
	end := time.Now()
	sick := BtrfsFilesystem{
		UUID: dataUUID, Label: "data", HealthKnown: true, Degraded: true, LowMetadataSpace: true,
		Devices: []BtrfsDevice{{ID: 1, Errors: BtrfsDeviceErrors{Write: 1}}},
		Scrub:   BtrfsScrubStatus{Status: "finished", UncorrectableErrors: 2, FinishedAt: &end},
	}
	RaiseOfflineAlert(ctx, BtrfsOfflinePool{UUID: dataUUID, Label: "data", NumDevices: 2, Devices: []string{"/dev/sda1"}}, "")
	if got := alertKeys(t); got["offline-"+dataUUID] != alerts.SeverityError {
		t.Fatalf("offline alert = %v", got)
	}

	RaiseAlerts(ctx, sick)
	want := map[string]string{"health-" + dataUUID: alerts.SeverityError, "scrub-" + dataUUID: alerts.SeverityError, "space-" + dataUUID: alerts.SeverityWarning}
	if got := alertKeys(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("alerts = %v, want %v", got, want)
	}

	// Unknown health keeps the device alert; a running scrub keeps the scrub alert.
	RaiseAlerts(ctx, BtrfsFilesystem{UUID: dataUUID, Scrub: BtrfsScrubStatus{Status: "running"}})
	if got := alertKeys(t); got["health-"+dataUUID] == "" || got["scrub-"+dataUUID] == "" || got["space-"+dataUUID] != "" {
		t.Fatalf("after unknown health: %v", got)
	}

	RaiseAlerts(ctx, BtrfsFilesystem{UUID: dataUUID, HealthKnown: true, Scrub: BtrfsScrubStatus{Status: "finished"}})
	if got := alertKeys(t); len(got) != 0 {
		t.Fatalf("healthy filesystem left alerts: %v", got)
	}
}

func TestCheckHealthFlagsOfflinePoolsAndDropsStaleAlerts(t *testing.T) {
	stubAlerts(t)
	stubKernel(t, raid1Kernel())
	dir := t.TempDir()
	for name, size := range map[string]string{"sdc": "1000", "sdd": "1000", "ram0": "10", "sde": "0"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "size"), []byte(size+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldDir, oldRead := sysBlockDir, readSuperblock
	sysBlockDir = dir
	readSuperblock = func(device string) (Superblock, error) {
		if device == "/dev/sdc" {
			return Superblock{UUID: "cold-uuid", Label: "cold", NumDevices: 2}, nil
		}
		return Superblock{}, errNotBtrfs
	}
	t.Cleanup(func() { sysBlockDir, readSuperblock = oldDir, oldRead })
	ctx := context.Background()
	if err := alerts.Raise(ctx, alerts.Observation{Source: AlertSource, Key: "health-gone-uuid", Severity: alerts.SeverityError, Title: "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := alerts.Raise(ctx, alerts.Observation{Source: AlertSource, Key: "snapshot-gone-uuid", Severity: alerts.SeverityError, Title: "not ours to drop"}); err != nil {
		t.Fatal(err)
	}

	if resync, err := CheckHealth(ctx); err != nil || len(resync) != 0 {
		t.Fatalf("CheckHealth = %v, %v", resync, err)
	}
	got := alertKeys(t)
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"offline-cold-uuid", "snapshot-gone-uuid"}; !reflect.DeepEqual(keys, want) || got["offline-cold-uuid"] != alerts.SeverityError {
		t.Fatalf("alerts = %v, want %v", got, want)
	}
}

func TestCheckHealthStartsResyncOnceEveryDiskIsBack(t *testing.T) {
	k := raid1Kernel()
	k.generation = 42
	stubKernel(t, k)
	stubDisks(t, nil)
	ctx := context.Background()
	if err := updatePoolStates(ctx, func(s map[string]poolState) { s[dataUUID] = poolState{Generation: 40, Degraded: true} }); err != nil {
		t.Fatal(err)
	}

	resync, err := CheckHealth(ctx)
	if err != nil || !reflect.DeepEqual(resync, []string{dataUUID}) {
		t.Fatalf("CheckHealth = %v, %v; want a resync of %s", resync, err, dataUUID)
	}
	if got := alertKeys(t); got["resync-"+dataUUID] != alerts.SeverityWarning {
		t.Fatalf("alerts = %v, want the resync warning", got)
	}
	states, err := loadPoolStates()
	if err != nil || !reflect.DeepEqual(states[dataUUID], poolState{Generation: 42, Degraded: true}) {
		t.Fatalf("state = %+v, %v; want the generation recorded", states, err)
	}

	// After maxResyncAttempts failures a person has to look.
	if err = updatePoolStates(ctx, func(s map[string]poolState) {
		s[dataUUID] = poolState{Generation: 42, Degraded: true, ResyncFailures: maxResyncAttempts, ResyncError: "balance: no space left on device"}
	}); err != nil {
		t.Fatal(err)
	}
	if resync, err = CheckHealth(ctx); err != nil || len(resync) != 0 {
		t.Fatalf("CheckHealth after failed resyncs = %v, %v", resync, err)
	}
	if got := alertKeys(t); got["resync-"+dataUUID] != alerts.SeverityError {
		t.Fatalf("alerts = %v, want the resync error", got)
	}

	// While a disk is still missing there is nothing to resync yet.
	k.devices[2].path = ""
	if resync, err = CheckHealth(ctx); err != nil || len(resync) != 0 {
		t.Fatalf("degraded CheckHealth = %v, %v", resync, err)
	}
	if got := alertKeys(t); got["resync-"+dataUUID] != "" {
		t.Fatalf("alerts = %v, want no resync warning while degraded", got)
	}
}

func TestOfflineAlertSaysWhyThePoolWasNotMountedDegraded(t *testing.T) {
	stubAlerts(t)
	pool := BtrfsOfflinePool{UUID: dataUUID, Label: "data", NumDevices: 2, Devices: []string{"/dev/sda1"}}
	RaiseOfflineAlert(context.Background(), pool, "/dev/sda1 missed writes the other present disks have")
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 || !strings.Contains(doc.Alerts[0].Message, "did not mount the pool degraded because /dev/sda1 missed writes") {
		t.Fatalf("alerts = %+v, %v", doc.Alerts, err)
	}
}
