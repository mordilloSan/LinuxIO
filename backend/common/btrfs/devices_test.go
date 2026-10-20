package btrfs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const threeDeviceShow = "Label: 'data'  uuid: " + dataUUID + "\n" +
	"\tdevid    1 size 100 used 50 path /dev/sda1\n" +
	"\tdevid    2 size 100 used 50 path /dev/sdb1\n" +
	"\tdevid    3 size 100 used 50 path /dev/sdc1\n"

// stubDeviceBtrfs mounts dataUUID at /data, serves the inventory reads with
// show as the `filesystem show` output, and records every other invocation.
func stubDeviceBtrfs(t *testing.T, show string) *[][]string {
	t.Helper()
	return stubDeviceBtrfsUsage(t, show, fixture(t, "fi_usage_6.6.txt"))
}

// stubDeviceBtrfsUsage is stubDeviceBtrfs with usage as the `filesystem
// usage` output.
func stubDeviceBtrfsUsage(t *testing.T, show, usage string) *[][]string {
	t.Helper()
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	var calls [][]string
	old := runBtrfs
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		switch strings.Join(args[:2], " ") {
		case "filesystem usage":
			return usage, nil
		case "filesystem show":
			return show, nil
		case "device stats":
			return fixture(t, "device_stats.txt"), nil
		case "scrub status":
			return fixture(t, "scrub_status.txt"), nil
		}
		calls = append(calls, args)
		return "", nil
	}
	t.Cleanup(func() { runBtrfs = old })
	return &calls
}

func TestValidateProfilesRejectsRaid56(t *testing.T) {
	for _, p := range []string{"raid5", "RAID6"} {
		if err := ValidateProfiles(new(p), nil, 8); err == nil || err.Error() != "raid5 and raid6 are not supported" {
			t.Fatalf("data %s: err = %v", p, err)
		}
		if err := ValidateProfiles(nil, new(p), 8); err == nil || err.Error() != "raid5 and raid6 are not supported" {
			t.Fatalf("metadata %s: err = %v", p, err)
		}
	}
	if err := ValidateProfiles(new("mirror"), nil, 8); err == nil || err.Error() != "unsupported profile mirror" {
		t.Fatalf("unknown profile: err = %v", err)
	}
	if err := ValidateProfiles(nil, nil, 1); err != nil {
		t.Fatalf("nil profiles: err = %v", err)
	}
}

func TestValidateProfilesNeedsDevices(t *testing.T) {
	if err := ValidateProfiles(new("raid10"), nil, 2); err == nil || err.Error() != "profile raid10 needs at least 4 devices" {
		t.Fatalf("err = %v", err)
	}
	if err := ValidateProfiles(new("raid1"), new("raid1c3"), 3); err != nil {
		t.Fatalf("raid1/raid1c3 on 3 devices: err = %v", err)
	}
	for p, want := range map[string]int{"single": 1, "dup": 1, "raid0": 2, "raid1": 2, "raid1c3": 3, "raid1c4": 4, "raid10": 4, "raid5": 0} {
		if got := MinDevices(p); got != want {
			t.Errorf("MinDevices(%s) = %d, want %d", p, got, want)
		}
	}
}

func TestRemoveDeviceRefusesBelowMinimum(t *testing.T) {
	calls := stubDeviceBtrfs(t, fixture(t, "fi_show.txt")) // 2 devices, data raid1
	err := RemoveDevice(context.Background(), dataUUID, 1)
	if err == nil || !strings.Contains(err.Error(), "raid1") {
		t.Fatalf("err = %v, want refusal naming raid1", err)
	}
	if err := RemoveDevice(context.Background(), dataUUID, 9); err == nil || !strings.Contains(err.Error(), "device 9 not found") {
		t.Fatalf("missing id: err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("mutating calls after refusal: %v", *calls)
	}
}

// A missing device does not count towards the raid1 minimum: removing one of
// the two present devices of a degraded three-device raid1 is refused.
func TestRemoveDeviceIgnoresMissingDevices(t *testing.T) {
	degraded := "Label: 'data'  uuid: " + dataUUID + "\n" +
		"\tdevid    1 size 100 used 50 path /dev/sda1\n" +
		"\tdevid    2 size 100 used 50 path /dev/sdb1\n" +
		"\tdevid    3 size 0 used 0 path /dev/sdc1 MISSING\n"
	calls := stubDeviceBtrfs(t, degraded)
	if err := RemoveDevice(context.Background(), dataUUID, 1); err == nil || !strings.Contains(err.Error(), "raid1") {
		t.Fatalf("err = %v, want refusal naming raid1", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("mutating calls after refusal: %v", *calls)
	}
}

func TestRemoveDeviceRefusesUnknownProfiles(t *testing.T) {
	raid5Usage := "Overall:\n    Device size:\t\t300\n    Device unallocated:\t\t100\n    Used:\t\t50\n\n" +
		"Data,RAID5: Size:100, Used:50 (50.00%)\n\nMetadata,RAID1: Size:10, Used:5 (50.00%)\n"
	for _, c := range []struct{ name, usage, want string }{
		{"usage failed", "", "cannot determine current profiles of " + dataUUID},
		{"raid5", raid5Usage, "cannot determine minimum devices for profile raid5"},
	} {
		calls := stubDeviceBtrfsUsage(t, threeDeviceShow, c.usage)
		if err := RemoveDevice(context.Background(), dataUUID, 2); err == nil || err.Error() != c.want {
			t.Fatalf("%s: err = %v, want %q", c.name, err, c.want)
		}
		if len(*calls) != 0 {
			t.Fatalf("%s: mutating calls after refusal: %v", c.name, *calls)
		}
	}
}

func TestRemoveDeviceRuns(t *testing.T) {
	calls := stubDeviceBtrfs(t, threeDeviceShow)
	if err := RemoveDevice(context.Background(), dataUUID, 2); err != nil {
		t.Fatalf("RemoveDevice: %v", err)
	}
	if want := [][]string{{"device", "remove", "2", "/data"}}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

func TestDeviceCommandsArgv(t *testing.T) {
	ctx := context.Background()
	steps := []struct {
		name string
		run  func() error
		want []string
	}{
		{"add", func() error { return AddDevice(ctx, dataUUID, "/dev/sdd", false) }, []string{"device", "add", "/dev/sdd", "/data"}},
		{"add force", func() error { return AddDevice(ctx, dataUUID, "/dev/sdd", true) }, []string{"device", "add", "-f", "/dev/sdd", "/data"}},
		{"replace", func() error { return ReplaceStart(ctx, dataUUID, 1, "/dev/sdd") }, []string{"replace", "start", "-f", "1", "/dev/sdd", "/data"}},
		{"balance convert", func() error { return BalanceStart(ctx, dataUUID, new("raid1"), new("raid1")) }, []string{"balance", "start", "--background", "-dconvert=raid1", "-mconvert=raid1", "/data"}},
		{"balance full", func() error { return BalanceStart(ctx, dataUUID, nil, nil) }, []string{"balance", "start", "--background", "--full-balance", "/data"}},
		{"balance cancel", func() error { return BalanceCancel(ctx, dataUUID) }, []string{"balance", "cancel", "/data"}},
		{"scrub start", func() error { return ScrubStart(ctx, dataUUID) }, []string{"scrub", "start", "/data"}},
		{"scrub cancel", func() error { return ScrubCancel(ctx, dataUUID) }, []string{"scrub", "cancel", "/data"}},
	}
	for _, s := range steps {
		calls := stubDeviceBtrfs(t, threeDeviceShow)
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if want := [][]string{s.want}; !reflect.DeepEqual(*calls, want) {
			t.Fatalf("%s: calls = %v, want %v", s.name, *calls, want)
		}
	}
}

func TestDeviceCommandsRefuseBeforeRunning(t *testing.T) {
	ctx := context.Background()
	calls := stubDeviceBtrfs(t, fixture(t, "fi_show.txt")) // 2 devices
	if err := BalanceStart(ctx, dataUUID, new("raid10"), nil); err == nil || err.Error() != "profile raid10 needs at least 4 devices" {
		t.Fatalf("balance: err = %v", err)
	}
	if err := ReplaceStart(ctx, dataUUID, 7, "/dev/sdd"); err == nil {
		t.Fatal("replace of unknown device id succeeded")
	}
	if err := AddDevice(ctx, rootUUID, "/dev/sdd", false); err == nil || err.Error() != "btrfs filesystem "+rootUUID+" not found" {
		t.Fatalf("add to unknown fs: err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("mutating calls after refusal: %v", *calls)
	}
}

func TestParseBalanceStatusRunning(t *testing.T) {
	running, pct, err := parseBalanceStatus(fixture(t, "balance_status.txt"))
	if err != nil || !running || pct != 30 {
		t.Fatalf("running=%v pct=%v err=%v, want true 30", running, pct, err)
	}
	running, pct, err = parseBalanceStatus("No balance found on '/data'")
	if err != nil || running || pct != 0 {
		t.Fatalf("none: running=%v pct=%v err=%v", running, pct, err)
	}
	if _, _, err := parseBalanceStatus("Balance on '/data' is paused\n3 out of about 10 chunks balanced (12 considered),  70% left"); err == nil {
		t.Fatal("paused balance parsed without error")
	}
	if _, _, err := parseBalanceStatus("garbage"); err == nil {
		t.Fatal("garbage parsed without error")
	}
}

// btrfs balance status exits 1 while a balance runs.
func TestBalanceStatusToleratesRunningExitCode(t *testing.T) {
	stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
	old := runBtrfs
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		return fixture(t, "balance_status.txt"), errors.New("btrfs balance status: exit status 1")
	}
	t.Cleanup(func() { runBtrfs = old })
	running, pct, err := BalanceStatus(context.Background(), dataUUID)
	if err != nil || !running || pct != 30 {
		t.Fatalf("running=%v pct=%v err=%v", running, pct, err)
	}
}

func TestParseReplaceStatus(t *testing.T) {
	running, pct, err := parseReplaceStatus("12.3% done, 0 write errs, 0 uncorr. read errs")
	if err != nil || !running || pct != 12.3 {
		t.Fatalf("running: running=%v pct=%v err=%v", running, pct, err)
	}
	running, pct, err = parseReplaceStatus(fixture(t, "replace_status.txt"))
	if err != nil || running || pct != 100 {
		t.Fatalf("finished: running=%v pct=%v err=%v", running, pct, err)
	}
	if _, _, err := parseReplaceStatus("Started on 19.Oct 10:15:00, canceled on 19.Oct 10:20:00 at 12.3%, 0 write errs, 0 uncorr. read errs"); err == nil {
		t.Fatal("canceled replace parsed without error")
	}
}

// The start commands fork before the kernel call, so a refused or canceled
// operation looks finished; the refreshed filesystem must show the outcome.
func TestBalanceResultRequiresConvertedProfiles(t *testing.T) {
	ctx := context.Background()
	calls := stubDeviceBtrfs(t, threeDeviceShow) // data raid1, metadata dup
	if _, err := BalanceResult(ctx, dataUUID, new("single"), nil); err == nil ||
		err.Error() != "balance ended without converting data to single (canceled or failed; see dmesg)" {
		t.Fatalf("stale data: err = %v", err)
	}
	if _, err := BalanceResult(ctx, dataUUID, nil, new("raid1")); err == nil ||
		err.Error() != "balance ended without converting metadata to raid1 (canceled or failed; see dmesg)" {
		t.Fatalf("stale metadata: err = %v", err)
	}
	if fs, err := BalanceResult(ctx, dataUUID, new("raid1"), new("dup")); err != nil || fs.UUID != dataUUID {
		t.Fatalf("converted: fs=%+v err=%v", fs, err)
	}
	if _, err := BalanceResult(ctx, dataUUID, nil, nil); err != nil {
		t.Fatalf("full balance: err = %v", err)
	}
	stubDeviceBtrfsUsage(t, threeDeviceShow, "")
	if _, err := BalanceResult(ctx, dataUUID, new("raid1"), nil); err == nil {
		t.Fatal("unknown allocation reported as converted")
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected calls: %v", *calls)
	}
}

func TestReplaceResultRequiresNewDevice(t *testing.T) {
	ctx := context.Background()
	calls := stubDeviceBtrfs(t, threeDeviceShow)
	if _, err := ReplaceResult(ctx, dataUUID, 1, "/dev/sdd"); err == nil ||
		err.Error() != "replace ended without installing /dev/sdd (canceled or failed; see dmesg)" {
		t.Fatalf("old path: err = %v", err)
	}
	if fs, err := ReplaceResult(ctx, dataUUID, 3, "/dev/sdc1"); err != nil || fs.UUID != dataUUID {
		t.Fatalf("installed: fs=%+v err=%v", fs, err)
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected calls: %v", *calls)
	}
}
