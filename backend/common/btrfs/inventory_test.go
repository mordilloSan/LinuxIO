package btrfs

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func checkUsage(t *testing.T, name string) {
	t.Helper()
	total, unalloc, used, alloc, err := parseFilesystemUsage(fixture(t, name))
	if err != nil {
		t.Fatalf("parseFilesystemUsage: %v", err)
	}
	if total != 214748364800 || unalloc != 105193144320 || used != 86973104128 {
		t.Fatalf("total/unalloc/used = %d/%d/%d", total, unalloc, used)
	}
	want := []BtrfsAllocation{
		{Kind: "data", Profile: "raid1", TotalBytes: 53687091200, UsedBytes: 42949672960},
		{Kind: "metadata", Profile: "dup", TotalBytes: 1073741824, UsedBytes: 536870912},
		{Kind: "system", Profile: "single", TotalBytes: 33554432, UsedBytes: 16384},
	}
	if !reflect.DeepEqual(alloc, want) {
		t.Fatalf("alloc = %+v, want %+v", alloc, want)
	}
}

func TestParseFilesystemUsage516(t *testing.T) { checkUsage(t, "fi_usage_5.16.txt") }
func TestParseFilesystemUsage66(t *testing.T)  { checkUsage(t, "fi_usage_6.6.txt") }

func TestParseFilesystemUsageInvalid(t *testing.T) {
	if _, _, _, _, err := parseFilesystemUsage("garbage"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFilesystemShowMissingDevice(t *testing.T) {
	label, devices, missing, err := parseFilesystemShow(fixture(t, "fi_show.txt"))
	if err != nil {
		t.Fatalf("parseFilesystemShow: %v", err)
	}
	if label != "data" || !missing {
		t.Fatalf("label=%q missing=%v", label, missing)
	}
	want := []BtrfsDevice{
		{ID: 1, Path: "/dev/sda1", SizeBytes: 107374182400, UsedBytes: 55834574848},
		{ID: 2, Path: "/dev/sdb", Missing: true},
	}
	if !reflect.DeepEqual(devices, want) {
		t.Fatalf("devices = %+v, want %+v", devices, want)
	}
}

func TestParseFilesystemShowMissingEmptyPath(t *testing.T) {
	_, devices, missing, err := parseFilesystemShow("Label: none  uuid: " + dataUUID + "\n\tdevid    1 size 10 used 5 path /dev/sdb1\n\tdevid    2 size 0 used 0 path  MISSING\n")
	if err != nil || !missing || len(devices) != 2 || devices[1].Path != "" || !devices[1].Missing || devices[0].Missing {
		t.Fatalf("devices=%+v missing=%v err=%v", devices, missing, err)
	}
}

func TestParseFilesystemShowOmittedMissingDevice(t *testing.T) {
	_, devices, missing, err := parseFilesystemShow("Label: none  uuid: " + dataUUID + "\n\tdevid    1 size 10 used 5 path /dev/sdb1\n\n*** Some devices missing\n")
	if err != nil || !missing || len(devices) != 1 || devices[0].Missing {
		t.Fatalf("devices=%+v missing=%v err=%v", devices, missing, err)
	}
}

func TestAttachDeviceStatsMatchesCanonicalPathAndDevid(t *testing.T) {
	old := evalSymlinks
	evalSymlinks = func(p string) (string, error) {
		if p == "/dev/mapper/cryptdata" {
			return "/dev/dm-0", nil
		}
		return p, nil
	}
	t.Cleanup(func() { evalSymlinks = old })

	devices := []BtrfsDevice{
		{ID: 1, Path: "/dev/mapper/cryptdata"},
		{ID: 2, Path: "/dev/sdb1"},
		{ID: 3, Path: ""},
	}
	devices = attachDeviceStats(devices, map[string]BtrfsDeviceErrors{
		"/dev/dm-0": {Corruption: 4},
		"devid:3":   {Read: 1},
	})
	if devices[0].Errors.Corruption != 4 || devices[1].Errors != (BtrfsDeviceErrors{}) || devices[2].Errors.Read != 1 {
		t.Fatalf("devices = %+v", devices)
	}
}

func TestParseFilesystemShowNoLabel(t *testing.T) {
	label, _, missing, err := parseFilesystemShow("Label: none  uuid: " + dataUUID + "\n\tdevid    1 size 10 used 5 path /dev/sdb1\n")
	if err != nil || label != "" || missing {
		t.Fatalf("label=%q missing=%v err=%v", label, missing, err)
	}
}

func TestParseDeviceStats(t *testing.T) {
	got, err := parseDeviceStats(fixture(t, "device_stats.txt"))
	if err != nil {
		t.Fatalf("parseDeviceStats: %v", err)
	}
	want := map[string]BtrfsDeviceErrors{
		"/dev/sda1": {Read: 2, Corruption: 3},
		"devid:2":   {Write: 5, Generation: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestParseScrubStatusFinished(t *testing.T) {
	got, err := parseScrubStatus(fixture(t, "scrub_status.txt"))
	if err != nil {
		t.Fatalf("parseScrubStatus: %v", err)
	}
	start := time.Date(2026, time.October, 18, 3, 0, 1, 0, time.Local)
	end := start.Add(42*time.Minute + 13*time.Second)
	want := BtrfsScrubStatus{Status: "finished", StartedAt: &start, FinishedAt: &end, ErrorCount: 3, BytesScrubbed: 42949672960, Percent: 100}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scrub = %+v, want %+v", got, want)
	}
}

func TestParseScrubStatusRunning(t *testing.T) {
	got, err := parseScrubStatus(fixture(t, "scrub_status_running.txt"))
	if err != nil {
		t.Fatalf("parseScrubStatus: %v", err)
	}
	if got.Status != "running" || got.FinishedAt != nil || got.StartedAt == nil ||
		got.Percent != 13.97 || got.BytesScrubbed != 7503290368 || got.ErrorCount != 0 {
		t.Fatalf("scrub = %+v", got)
	}
}

func TestParseScrubStatusNone(t *testing.T) {
	got, err := parseScrubStatus(fixture(t, "scrub_status_none.txt"))
	if err != nil || got.Status != "none" || got.StartedAt != nil {
		t.Fatalf("scrub = %+v err=%v", got, err)
	}
}

func TestListFilesystemsToleratesCommandFailure(t *testing.T) {
	stubMounts(t, []Mount{
		{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5},
		{Mountpoint: "/data/sub", UUID: dataUUID, SubvolID: 256},
		{Mountpoint: "/", UUID: rootUUID, SubvolID: 256},
	})
	old := runBtrfs
	t.Cleanup(func() { runBtrfs = old })
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		if args[len(args)-1] != "/data" {
			return "", errors.New("boom")
		}
		switch strings.Join(args[:2], " ") {
		case "filesystem usage":
			return fixture(t, "fi_usage_6.6.txt"), nil
		case "filesystem show":
			return fixture(t, "fi_show.txt"), nil
		case "device stats":
			return fixture(t, "device_stats.txt"), nil
		}
		return "", errors.New("scrub failed")
	}

	got, err := ListFilesystems(context.Background())
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}
	if len(got) != 2 || got[0].UUID != dataUUID || got[1].UUID != rootUUID {
		t.Fatalf("filesystems = %+v", got)
	}
	data := got[0]
	if len(data.Mountpoints) != 2 || data.Label != "data" || !data.Degraded || data.TotalBytes != 214748364800 ||
		data.Scrub.Status != "none" || len(data.Devices) != 2 ||
		data.Devices[0].Errors.Corruption != 3 || data.Devices[1].Errors.Write != 5 {
		t.Fatalf("data filesystem = %+v", data)
	}
	if !data.HealthKnown || got[1].HealthKnown {
		t.Fatalf("HealthKnown = %v/%v, want true/false", data.HealthKnown, got[1].HealthKnown)
	}
	if root := got[1]; root.TotalBytes != 0 || root.Scrub.Status != "none" || len(root.Mountpoints) != 1 {
		t.Fatalf("root filesystem = %+v", root)
	}
}

// A mounted filesystem's `filesystem show` omits a missing device and only
// prints "*** Some devices missing"; `device stats` still names it by devid,
// so it is listed as a missing device that remove/replace can target.
func TestListFilesystemsAddsMissingDeviceFromStats(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	old := runBtrfs
	t.Cleanup(func() { runBtrfs = old })
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "filesystem show":
			return fixture(t, "fi_show_mounted_missing.txt"), nil
		case "device stats":
			return fixture(t, "device_stats.txt"), nil
		}
		return "", errors.New("unused")
	}
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	want := []BtrfsDevice{
		{ID: 1, Path: "/dev/sda1", SizeBytes: 107374182400, UsedBytes: 55834574848, Errors: BtrfsDeviceErrors{Read: 2, Corruption: 3}},
		{ID: 2, Path: "", Missing: true, Errors: BtrfsDeviceErrors{Write: 5, Generation: 1}},
	}
	if fs := got[0]; !fs.Degraded || !reflect.DeepEqual(fs.Devices, want) {
		t.Fatalf("degraded=%v devices=%+v, want %+v", fs.Degraded, fs.Devices, want)
	}
}

func TestListFilesystemsHealthUnknownWhenStatsFail(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	old := runBtrfs
	t.Cleanup(func() { runBtrfs = old })
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "filesystem show":
			return fixture(t, "fi_show.txt"), nil
		case "device stats":
			return "", errors.New("boom")
		}
		return "", errors.New("unused")
	}
	got, err := ListFilesystems(context.Background())
	if err != nil || len(got) != 1 || got[0].HealthKnown {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
