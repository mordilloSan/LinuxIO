package btrfs

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

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

// threeDiskKernel adds a third raid1 member to raid1Kernel.
func threeDiskKernel() *fakeKernel {
	k := raid1Kernel()
	k.maxID = 3
	k.devices[3] = &fakeDevice{path: "/dev/sdc1", total: 100 << 30, used: 51 << 30, physical: 100 << 30}
	return k
}

func TestRemoveDeviceRefusesBelowMinimum(t *testing.T) {
	k := raid1Kernel() // 2 devices, raid1
	stubKernel(t, k)
	err := RemoveDevice(context.Background(), dataUUID, 1)
	if err == nil || !strings.Contains(err.Error(), "raid1") {
		t.Fatalf("err = %v, want refusal naming raid1", err)
	}
	if err := RemoveDevice(context.Background(), dataUUID, 9); err == nil || !strings.Contains(err.Error(), "device 9 not found") {
		t.Fatalf("missing id: err = %v", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("mutating calls after refusal: %v", calls)
	}
}

// A missing device does not count towards the raid1 minimum: removing one of
// the two present devices of a degraded three-device raid1 is refused.
func TestRemoveDeviceIgnoresMissingDevices(t *testing.T) {
	k := threeDiskKernel()
	k.devices[3].path = ""
	stubKernel(t, k)
	if err := RemoveDevice(context.Background(), dataUUID, 1); err == nil || !strings.Contains(err.Error(), "raid1") {
		t.Fatalf("err = %v, want refusal naming raid1", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("mutating calls after refusal: %v", calls)
	}
}

func TestRemoveDeviceRefusesUnknownProfiles(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*fakeKernel)
		want string
	}{
		{"space info failed", func(k *fakeKernel) { k.fail = map[uintptr]error{iocSpaceInfo: unix.EIO} }, "cannot determine current profiles of " + dataUUID},
		{"raid5", func(k *fakeKernel) { k.spaces[0].Flags = bgData | bgRaid5 }, "cannot determine minimum devices for profile raid5"},
	} {
		k := threeDiskKernel()
		c.edit(k)
		stubKernel(t, k)
		if err := RemoveDevice(context.Background(), dataUUID, 2); err == nil || err.Error() != c.want {
			t.Fatalf("%s: err = %v, want %q", c.name, err, c.want)
		}
		if calls := k.Calls(); len(calls) != 0 {
			t.Fatalf("%s: mutating calls after refusal: %v", c.name, calls)
		}
	}
}

func TestRemoveDeviceRuns(t *testing.T) {
	k := threeDiskKernel()
	stubKernel(t, k)
	if err := RemoveDevice(context.Background(), dataUUID, 2); err != nil {
		t.Fatalf("RemoveDevice: %v", err)
	}
	if want := []string{"RM_DEV 2 flags=8"}; !reflect.DeepEqual(k.Calls(), want) {
		t.Fatalf("calls = %v, want %v", k.Calls(), want)
	}
}

func TestAddDeviceWipesOnlyWithForce(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	var wiped []string
	old := wipeDeviceEdges
	wipeDeviceEdges = func(device string) error {
		wiped = append(wiped, device)
		return nil
	}
	t.Cleanup(func() { wipeDeviceEdges = old })

	if err := AddDevice(context.Background(), dataUUID, "/dev/sdd", false); err != nil {
		t.Fatal(err)
	}
	if err := AddDevice(context.Background(), dataUUID, "/dev/sde", true); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ADD_DEV /dev/sdd", "ADD_DEV /dev/sde"}; !reflect.DeepEqual(k.Calls(), want) || !reflect.DeepEqual(wiped, []string{"/dev/sde"}) {
		t.Fatalf("calls = %v, wiped = %v", k.Calls(), wiped)
	}
	if err := AddDevice(context.Background(), rootUUID, "/dev/sdd", false); err == nil || err.Error() != "btrfs filesystem "+rootUUID+" not found" {
		t.Fatalf("add to unknown fs: err = %v", err)
	}
}

func TestReplaceReportsProgressAndChecksResult(t *testing.T) {
	k := raid1Kernel()
	k.replaceStatus = func() (uint64, uint64) { return replaceStateStarted, 500 }
	seen := make(chan struct{})
	k.replaceStart = func(a *devReplaceArgs) error {
		<-seen // finish only after progress was reported
		return nil
	}
	stubKernel(t, k)

	var mu sync.Mutex
	var once sync.Once
	var reported []float64
	err := Replace(context.Background(), dataUUID, 1, "/dev/sdd", func(p float64) {
		mu.Lock()
		reported = append(reported, p)
		mu.Unlock()
		once.Do(func() { close(seen) })
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls := k.Calls(); !reflect.DeepEqual(calls, []string{"REPLACE_START 1 /dev/sdd"}) {
		t.Fatalf("calls = %v", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) == 0 || reported[0] != 50 {
		t.Fatalf("reported = %v", reported)
	}

	k.replaceStart = func(a *devReplaceArgs) error {
		a.Result = 2
		return nil
	}
	if err := Replace(context.Background(), dataUUID, 1, "/dev/sdd", func(float64) {}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("refused start: err = %v", err)
	}
	if err := Replace(context.Background(), dataUUID, 7, "/dev/sdd", func(float64) {}); err == nil || !strings.Contains(err.Error(), "device 7 not found") {
		t.Fatalf("unknown device: err = %v", err)
	}
}

func TestReplaceCancelsWithContext(t *testing.T) {
	k := raid1Kernel()
	started := make(chan struct{})
	k.replaceStart = func(*devReplaceArgs) error {
		close(started)
		<-k.cancelled
		return unix.ECANCELED
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	err := Replace(ctx, dataUUID, 1, "/dev/sdd", func(float64) {})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("err = %v", err)
	}
	if calls := k.Calls(); !reflect.DeepEqual(calls, []string{"REPLACE_START 1 /dev/sdd", "REPLACE_CANCEL"}) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestBalanceArgs(t *testing.T) {
	cases := []struct {
		name string
		opts BalanceOptions
		want func(*testing.T, balanceArgs)
	}{
		{"convert both", BalanceOptions{DataProfile: new("raid1"), MetadataProfile: new("raid1c3")}, func(t *testing.T, a balanceArgs) {
			if a.Flags != balanceData|balanceMetadata|balanceSystem || a.Data.Flags != balanceArgsConvert || a.Data.Target != bgRaid1 ||
				a.Meta.Target != bgRaid1c3 || a.Sys != a.Meta {
				t.Errorf("args = %+v", a)
			}
		}},
		{"single uses the extended bit", BalanceOptions{DataProfile: new("single")}, func(t *testing.T, a balanceArgs) {
			if a.Flags != balanceData || a.Data.Target != bgSingle || a.Meta.Flags != 0 {
				t.Errorf("args = %+v", a)
			}
		}},
		{"usage filter", BalanceOptions{DataUsageMax: new(50)}, func(t *testing.T, a balanceArgs) {
			if a.Flags != balanceData || a.Data.Flags != balanceArgsUsage || a.Data.Usage != 50 {
				t.Errorf("args = %+v", a)
			}
		}},
		{"full", BalanceOptions{}, func(t *testing.T, a balanceArgs) {
			if a.Flags != balanceData|balanceMetadata|balanceSystem || a.Data.Flags != 0 || a.Meta.Flags != 0 {
				t.Errorf("args = %+v", a)
			}
		}},
	}
	for _, c := range cases {
		k := threeDiskKernel()
		var got balanceArgs
		k.balance = func(a *balanceArgs) error {
			got = *a
			return nil
		}
		stubKernel(t, k)
		if err := Balance(context.Background(), dataUUID, c.opts, func(float64) {}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		c.want(t, got)
	}
}

func TestBalanceRefusesBeforeRunning(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	if err := Balance(context.Background(), dataUUID, BalanceOptions{DataProfile: new("raid10")}, func(float64) {}); err == nil || err.Error() != "profile raid10 needs at least 4 devices" {
		t.Fatalf("err = %v", err)
	}
	if err := Balance(context.Background(), dataUUID, BalanceOptions{DataUsageMax: new(120)}, func(float64) {}); err == nil || !strings.Contains(err.Error(), "percentage") {
		t.Fatalf("err = %v", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("calls after refusal: %v", calls)
	}
}

func TestBalanceCancelsWithContextAndReportsProgress(t *testing.T) {
	k := raid1Kernel()
	k.balanceProgress = func() (balanceArgs, error) {
		return balanceArgs{State: balanceStateRunning, Stat: balanceProgress{Expected: 200, Completed: 50}}, nil
	}
	k.balance = func(*balanceArgs) error {
		<-k.cancelled
		return unix.ECANCELED
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var reported []float64
	err := Balance(ctx, dataUUID, BalanceOptions{}, func(p float64) {
		mu.Lock()
		reported = append(reported, p)
		mu.Unlock()
		cancel() // once progress has been seen, cancel
	})
	if err == nil || err.Error() != "balance canceled" {
		t.Fatalf("err = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reported) == 0 || reported[0] != 25 {
		t.Fatalf("reported = %v", reported)
	}
	if calls := k.Calls(); !reflect.DeepEqual(calls, []string{"BALANCE flags=7", "BALANCE_CTL 2"}) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestBalanceProgressAndCancel(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	if running, _, err := balanceProgressOf("/data"); err != nil || running {
		t.Fatalf("no balance: running=%v err=%v", running, err)
	}
	k.balanceProgress = func() (balanceArgs, error) {
		return balanceArgs{State: balanceStateRunning | balanceStatePauseReq}, nil
	}
	if _, _, err := balanceProgressOf("/data"); err == nil || err.Error() != "balance is paused" {
		t.Fatalf("paused: err = %v", err)
	}
	k.fail = map[uintptr]error{iocBalanceCtl: unix.ENOTCONN}
	if err := BalanceCancel(context.Background(), dataUUID); err == nil || err.Error() != "no balance is running" {
		t.Fatalf("cancel: err = %v", err)
	}
}

func TestBalanceResultRequiresConvertedProfiles(t *testing.T) {
	k := raid1Kernel() // data raid1, metadata raid1
	stubKernel(t, k)
	if _, err := BalanceResult(context.Background(), dataUUID, new("raid1"), nil); err != nil {
		t.Fatalf("converted: %v", err)
	}
	if _, err := BalanceResult(context.Background(), dataUUID, new("single"), nil); err == nil || !strings.Contains(err.Error(), "without converting data to single") {
		t.Fatalf("stale: err = %v", err)
	}
	if _, err := BalanceResult(context.Background(), dataUUID, nil, new("dup")); err == nil || !strings.Contains(err.Error(), "metadata to dup") {
		t.Fatalf("metadata: err = %v", err)
	}
}

func TestReplaceResultRequiresNewDevice(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	if _, err := ReplaceResult(context.Background(), dataUUID, 2, "/dev/sdb1"); err != nil {
		t.Fatalf("installed: %v", err)
	}
	if _, err := ReplaceResult(context.Background(), dataUUID, 2, "/dev/sdd"); err == nil || !strings.Contains(err.Error(), "without installing /dev/sdd") {
		t.Fatalf("not installed: err = %v", err)
	}
}

func TestGrowDeviceAndSetLabel(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	if err := GrowDevice(context.Background(), dataUUID, 2); err != nil {
		t.Fatal(err)
	}
	if err := GrowDevice(context.Background(), dataUUID, 9); err == nil {
		t.Fatal("grow of unknown device succeeded")
	}
	if err := SetLabel(context.Background(), dataUUID, "tank"); err != nil {
		t.Fatal(err)
	}
	if err := SetLabel(context.Background(), dataUUID, "a/b"); err == nil {
		t.Fatal("bad label accepted")
	}
	if want := []string{"RESIZE 2:max", "SET_FSLABEL tank"}; !reflect.DeepEqual(k.Calls(), want) {
		t.Fatalf("calls = %v, want %v", k.Calls(), want)
	}
}
