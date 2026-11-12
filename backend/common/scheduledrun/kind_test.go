package scheduledrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testScheduleID = "3f0c2f0e-7a1b-4c9d-8e2f-1a2b3c4d5e6f"
	testPoolUUID   = "0a1b2c3d-1111-2222-3333-444455556666"
)

func writeDefinition(t *testing.T, scheduleDir, id, contents string) {
	t.Helper()
	dir := filepath.Join(scheduleDir, "definitions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func definitionNamed(id, name string) string {
	return `{"version":1,"definition":{"id":"` + id + `","options":{"name":"` + name + `"}}}`
}

func TestKindOf(t *testing.T) {
	tests := []struct {
		unit string
		want Kind
	}{
		{"linuxio-schedule-" + testScheduleID + ".service", Kind{RouteScript, "Scheduled task", testScheduleID, "/services/schedules?schedule=" + testScheduleID, true}},
		{"linuxio-btrfs-snapshot.service", Kind{RouteSnapshot, "Snapshots and replication", "", "/storage/subvolumes?view=snapshots", false}},
		{"linuxio-btrfs-resync@" + testPoolUUID + ".service", Kind{RouteResync, "Pool resync", testPoolUUID, "/storage?pool=" + testPoolUUID, false}},
		{"linuxio-docker-update.service", Kind{RouteDockerUpdate, "Container updates", "", "/docker/containers", false}},
		{"apt-daily-upgrade.service", Kind{RouteSystemUpdate, "System updates", "", "/updates", false}},
		{"dnf-automatic.service", Kind{RouteSystemUpdate, "System updates", "", "/updates", false}},
		{"dnf5-automatic.service", Kind{RouteSystemUpdate, "System updates", "", "/updates", false}},
		{"mintupdate-automation-upgrade.service", Kind{RouteSystemUpdate, "System updates", "", "/updates", false}},
	}
	for _, tt := range tests {
		got, ok := KindOf(tt.unit, t.TempDir())
		if !ok || got != tt.want {
			t.Errorf("KindOf(%q) = %+v, %v; want %+v, true", tt.unit, got, ok, tt.want)
		}
	}
	for _, unit := range []string{
		"linuxio-btrfs-health.service",
		"apt-daily.service",
		"ssh.service",
		"linuxio-schedule-not-a-uuid.service",
		"linuxio-btrfs-resync@.service",
	} {
		if got, ok := KindOf(unit, t.TempDir()); ok {
			t.Errorf("KindOf(%q) = %+v, true; want false", unit, got)
		}
	}
}

func TestScriptLabelIsTheScheduleName(t *testing.T) {
	dir := t.TempDir()
	writeDefinition(t, dir, testScheduleID, definitionNamed(testScheduleID, "Nightly backup"))
	got, ok := KindOf("linuxio-schedule-"+testScheduleID+".service", dir)
	if !ok || got.Label != "Nightly backup" {
		t.Fatalf("KindOf label = %q, %v; want Nightly backup", got.Label, ok)
	}
}

func TestScriptLabelWithoutAScheduleDirReadsNoFile(t *testing.T) {
	dir := t.TempDir()
	writeDefinition(t, dir, testScheduleID, definitionNamed(testScheduleID, "Nightly backup"))
	t.Chdir(dir) // a relative lookup would find this definition
	got, ok := KindOf("linuxio-schedule-"+testScheduleID+".service", "")
	if !ok || got.Label != "Scheduled task" {
		t.Fatalf("KindOf label = %q, %v; want Scheduled task", got.Label, ok)
	}
}

func TestScriptLabelFallsBackWhenTheDefinitionIsUnreadable(t *testing.T) {
	huge := `{"version":1,"definition":{"options":{"name":"Big","script":"` + strings.Repeat("x", 512<<10) + `"}}}`
	tests := map[string]*string{
		"missing":      nil,
		"invalid JSON": new("{not json"),
		"empty name":   new(definitionNamed(testScheduleID, "")),
		"too large":    &huge,
		"a directory":  nil,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if contents != nil {
				writeDefinition(t, dir, testScheduleID, *contents)
			}
			if name == "a directory" {
				if err := os.MkdirAll(filepath.Join(dir, "definitions", testScheduleID+".json"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := KindOf("linuxio-schedule-"+testScheduleID+".service", dir)
			if !ok || got.Label != "Scheduled task" {
				t.Fatalf("KindOf label = %q, %v; want Scheduled task", got.Label, ok)
			}
		})
	}
}
