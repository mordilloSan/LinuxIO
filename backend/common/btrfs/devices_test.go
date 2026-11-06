package btrfs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

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

// A paused balance holds the pool, so the kernel refuses the removal with a
// positive code, which must not read as success.
func TestRemoveDeviceReportsKernelRefusal(t *testing.T) {
	k := threeDiskKernel()
	k.fail = map[uintptr]error{iocRmDevV2: kernelCode(8)}
	stubKernel(t, k)
	err := RemoveDevice(context.Background(), dataUUID, 2)
	var code kernelCode
	if !errors.As(err, &code) || code != 8 {
		t.Fatalf("err = %v, want one wrapping kernelCode(8)", err)
	}
}

// A cancel sent before the removal claimed the pool is lost with ENOTCONN, so
// a later tick must send it again until it lands.
func TestRemoveDeviceCancelsWithContext(t *testing.T) {
	k := threeDiskKernel()
	k.remove = func(uint64) error {
		select {
		case <-k.cancelled:
			return unix.ECANCELED
		case <-time.After(10 * time.Second):
			return errors.New("no resent cancel landed")
		}
	}
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	fake, lost := fsIoctl, make(chan struct{})
	var once sync.Once
	fsIoctl = func(mnt string, req uintptr, arg unsafe.Pointer) error {
		if req == iocRmDevV2 && (*volArgsV2)(arg).Flags != 0 {
			cancel()
			select { // the removal claims the pool only after a cancel was lost
			case <-lost:
			case <-time.After(10 * time.Second):
				return errors.New("no cancel was sent")
			}
		}
		err := fake(mnt, req, arg)
		if req == iocRmDevV2 && errors.Is(err, unix.ENOTCONN) {
			once.Do(func() { close(lost) })
		}
		return err
	}
	if err := RemoveDevice(ctx, dataUUID, 3); err == nil || err.Error() != "removal of device 3 canceled" {
		t.Fatalf("err = %v", err)
	}
	// Ticks may lose more cancels before the removal claims the pool.
	if calls, want := slices.Compact(k.Calls()), []string{"RM_DEV cancel", "RM_DEV 3 flags=8", "RM_DEV cancel"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestResumeRemoveSkipsAGoneDevice(t *testing.T) {
	k := raid1Kernel()
	stubKernel(t, k)
	if err := ResumeRemove(context.Background(), dataUUID, 3); err != nil || len(k.Calls()) != 0 {
		t.Fatalf("gone device: err = %v, calls = %v", err, k.Calls())
	}

	k = threeDiskKernel()
	stubKernel(t, k)
	if err := ResumeRemove(context.Background(), dataUUID, 3); err != nil {
		t.Fatal(err)
	}
	if want := []string{"RM_DEV 3 flags=8"}; !reflect.DeepEqual(k.Calls(), want) {
		t.Fatalf("calls = %v, want %v", k.Calls(), want)
	}

	k = threeDiskKernel()
	k.devices[2].path = ""
	stubKernel(t, k)
	if err := ResumeRemove(context.Background(), dataUUID, 3); err == nil || err.Error() != "pool "+dataUUID+" is degraded; removal not resumed" {
		t.Fatalf("degraded pool: err = %v", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("degraded pool: calls = %v", calls)
	}

	// An unread device list says nothing about device 3: fail closed.
	for _, req := range []uintptr{iocFSInfo, iocDevInfo} {
		k = threeDiskKernel()
		k.fail = map[uintptr]error{req: unix.EIO}
		stubKernel(t, k)
		if err := ResumeRemove(context.Background(), dataUUID, 3); err == nil || err.Error() != "could not read the devices of pool "+dataUUID {
			t.Fatalf("failed read %#x: err = %v", req, err)
		}
		if calls := k.Calls(); len(calls) != 0 {
			t.Fatalf("failed read %#x: calls = %v", req, calls)
		}
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

	// The kernel copies Result back only on success: a running scrub comes
	// back there, a running replace and a busy pool as the return code.
	for _, c := range []struct {
		start func(*devReplaceArgs) error
		want  string
	}{
		{func(a *devReplaceArgs) error { a.Result = 3; return nil }, "replace device 1: a scrub is running"},
		{func(*devReplaceArgs) error { return kernelCode(2) }, "replace device 1: a replace is already running"},
		{func(*devReplaceArgs) error { return kernelCode(8) }, "replace device 1: add/delete/balance/replace/resize operation in progress"},
	} {
		k.replaceStart = c.start
		if err := Replace(context.Background(), dataUUID, 1, "/dev/sdd", func(float64) {}); err == nil || err.Error() != c.want {
			t.Fatalf("refused start: err = %v, want %s", err, c.want)
		}
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
	if _, _, err := balanceProgressOf("/data"); !errors.Is(err, unix.ENOTCONN) {
		t.Fatalf("no balance: err = %v", err)
	}
	k.balanceProgress = func() (balanceArgs, error) {
		return balanceArgs{State: balanceStateRunning | balanceStatePauseReq, Stat: balanceProgress{Expected: 4, Completed: 1}}, nil
	}
	if state, pct, err := balanceProgressOf("/data"); err != nil || state != balanceStateRunning|balanceStatePauseReq || pct != 25 {
		t.Fatalf("pausing: state=%d pct=%v err=%v", state, pct, err)
	}
	k.fail = map[uintptr]error{iocBalanceCtl: unix.ENOTCONN}
	if err := BalanceCancel(context.Background(), dataUUID); err == nil || err.Error() != "no balance is running" {
		t.Fatalf("cancel: err = %v", err)
	}
}

// The host pauses the balance at shutdown and then cancels the job's
// context. A cancel then would delete the balance, paused or being paused,
// so none is sent.
func TestBalancePauseIsNotCanceledByTheJobContext(t *testing.T) {
	for _, c := range []struct {
		name  string
		state uint64 // BALANCE_PROGRESS once the pause was asked for
	}{
		{"paused", 0},
		{"pausing", balanceStateRunning | balanceStatePauseReq},
	} {
		k := raid1Kernel()
		looked := make(chan struct{}, 1)
		k.balanceProgress = func() (balanceArgs, error) {
			select {
			case <-k.cancelled:
				looked <- struct{}{}
				return balanceArgs{State: c.state}, nil
			default:
				return balanceArgs{State: balanceStateRunning}, nil
			}
		}
		started := make(chan struct{})
		k.balance = func(a *balanceArgs) error {
			close(started)
			<-k.cancelled // the pause
			// The canceled context looks before the paused ioctl returns.
			select {
			case <-looked:
			case <-time.After(10 * time.Second):
				return errors.New("the canceled context did not read the balance state")
			}
			a.State = balanceStatePauseReq
			return unix.ECANCELED
		}
		stubKernel(t, k)
		progressInterval = time.Hour // only the context's cancel reads progress
		ctx, cancel := context.WithCancel(context.Background())
		ended := make(chan error, 1)
		go func() { ended <- Balance(ctx, dataUUID, BalanceOptions{}, func(float64) {}) }()
		<-started
		if paused, err := PauseBalance(context.Background(), dataUUID); err != nil || !paused {
			t.Fatalf("%s: pause: paused=%v err=%v", c.name, paused, err)
		}
		cancel()
		if err := <-ended; !errors.Is(err, errBalancePaused) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, errBalancePaused)
		}
		if calls, want := k.Calls(), []string{"BALANCE flags=7", "BALANCE_CTL 1"}; !reflect.DeepEqual(calls, want) {
			t.Fatalf("%s: calls = %v, want %v", c.name, calls, want)
		}
	}
}

// Suspend checkpoints a pause only when there was a balance to pause.
func TestPauseBalanceReportsWhetherItPaused(t *testing.T) {
	for _, c := range []struct {
		name       string
		ctl        error
		wantPaused bool
		wantErr    bool
	}{
		{"running", nil, true, false},
		{"none", unix.ENOTCONN, false, false},
		{"failed", unix.EPERM, false, true},
	} {
		k := raid1Kernel()
		if c.ctl != nil {
			k.fail = map[uintptr]error{iocBalanceCtl: c.ctl}
		}
		stubKernel(t, k)
		paused, err := PauseBalance(context.Background(), dataUUID)
		if paused != c.wantPaused || (err != nil) != c.wantErr {
			t.Fatalf("%s: paused=%v err=%v", c.name, paused, err)
		}
	}
}

// A balance that ends between the state read and the cancel is no failure
// to cancel, so only a real failure is logged.
func TestCancelRunningBalanceWarnsOnlyOnARealFailure(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	for _, c := range []struct {
		ctl  error
		warn bool
	}{{unix.ENOTCONN, false}, {unix.EIO, true}} {
		logs.Reset()
		k := raid1Kernel()
		k.balanceProgress = func() (balanceArgs, error) { return balanceArgs{State: balanceStateRunning}, nil }
		k.fail = map[uintptr]error{iocBalanceCtl: c.ctl}
		stubKernel(t, k)
		cancelRunningBalance("/data")
		if warned := strings.Contains(logs.String(), "btrfs balance not canceled"); warned != c.warn {
			t.Fatalf("cancel failing with %v: warned=%v, logs %q", c.ctl, warned, logs.String())
		}
	}
}

// The cancel route cancels the balance in the kernel while the job's context
// is live: the job ends canceled, not failed.
func TestBalanceCanceledElsewhereEndsCanceled(t *testing.T) {
	k := raid1Kernel()
	k.balance = func(a *balanceArgs) error {
		a.State = balanceStateCancelReq
		return unix.ECANCELED
	}
	stubKernel(t, k)
	if err := Balance(context.Background(), dataUUID, BalanceOptions{}, func(float64) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
}

func TestResumeBalanceResumesAPausedBalance(t *testing.T) {
	k := raid1Kernel()
	k.balanceProgress = func() (balanceArgs, error) { return balanceArgs{}, nil } // paused
	var got balanceArgs
	k.balance = func(a *balanceArgs) error {
		got = *a
		return nil
	}
	stubKernel(t, k)
	if err := ResumeBalance(context.Background(), dataUUID, BalanceOptions{DataProfile: new("single")}, true, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if got != (balanceArgs{Flags: balanceResume}) {
		t.Fatalf("args = %+v, want only the resume flag", got)
	}
	if calls, want := k.Calls(), []string{"BALANCE flags=16"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}

	// The kernel resumed it first (EINPROGRESS): follow it to its end.
	k = raid1Kernel()
	reads := 0
	k.balanceProgress = func() (balanceArgs, error) {
		if reads++; reads > 1 {
			return balanceArgs{}, unix.ENOTCONN
		}
		return balanceArgs{}, nil
	}
	k.balance = func(*balanceArgs) error { return unix.EINPROGRESS }
	stubKernel(t, k)
	if err := ResumeBalance(context.Background(), dataUUID, BalanceOptions{}, true, func(float64) {}); err != nil {
		t.Fatalf("resumed by the kernel: %v", err)
	}
}

func TestResumeBalanceObservesAKernelRun(t *testing.T) {
	for _, c := range []struct {
		name string
		end  error // what BALANCE_PROGRESS gives after two running reads
		want error
	}{
		{"finished", unix.ENOTCONN, nil},
		{"paused", nil, errBalancePaused},
	} {
		k := raid1Kernel()
		reads := 0
		k.balanceProgress = func() (balanceArgs, error) {
			if reads++; reads > 2 {
				return balanceArgs{}, c.end
			}
			return balanceArgs{State: balanceStateRunning, Stat: balanceProgress{Expected: 4, Completed: 1}}, nil
		}
		stubKernel(t, k)
		var reported []float64
		err := ResumeBalance(context.Background(), dataUUID, BalanceOptions{}, true, func(p float64) { reported = append(reported, p) })
		if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if calls := k.Calls(); len(calls) != 0 {
			t.Fatalf("%s: calls = %v, want none", c.name, calls)
		}
		if !reflect.DeepEqual(reported, []float64{25}) {
			t.Fatalf("%s: reported = %v", c.name, reported)
		}
	}
}

func TestResumeBalanceRerunsOnlyWhenNotPaused(t *testing.T) {
	k := raid1Kernel() // no balance: BALANCE_PROGRESS gives ENOTCONN
	var got balanceArgs
	k.balance = func(a *balanceArgs) error {
		got = *a
		return nil
	}
	stubKernel(t, k)
	opts := BalanceOptions{DataProfile: new("single")}
	if err := ResumeBalance(context.Background(), dataUUID, opts, false, func(float64) {}); err != nil {
		t.Fatal(err)
	}
	if got.Flags != balanceData || got.Data.Flags != balanceArgsConvert|balanceArgsSoft || got.Data.Target != bgSingle {
		t.Fatalf("args = %+v, want a soft convert of data", got)
	}
	if calls := k.Calls(); len(calls) != 1 {
		t.Fatalf("calls = %v, want one balance", calls)
	}

	// This job paused it, so a balance gone has ended: the kernel resumed it
	// at mount and finished it before the host started.
	k = raid1Kernel()
	stubKernel(t, k)
	if err := ResumeBalance(context.Background(), dataUUID, opts, true, func(float64) {}); err != nil {
		t.Fatalf("paused then gone: err = %v, want nil", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("paused then gone: calls = %v, want none", calls)
	}
}

func TestResumeBalanceRefusesADegradedPool(t *testing.T) {
	k := threeDiskKernel()
	k.devices[2].path = ""
	k.balanceProgress = func() (balanceArgs, error) { return balanceArgs{}, nil } // paused
	stubKernel(t, k)
	if err := ResumeBalance(context.Background(), dataUUID, BalanceOptions{}, true, func(float64) {}); err == nil || err.Error() != "pool "+dataUUID+" is degraded; balance not resumed" {
		t.Fatalf("err = %v", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
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

// replaceStates makes the fake kernel's STATUS give states in turn, the last
// one from then on, at 25% each.
func replaceStates(k *fakeKernel, states ...uint64) {
	var mu sync.Mutex
	k.replaceStatus = func() (uint64, uint64) {
		mu.Lock()
		defer mu.Unlock()
		state := states[0]
		if len(states) > 1 {
			states = states[1:]
		}
		return state, 250
	}
}

func TestResumeReplaceFollowsAKernelRun(t *testing.T) {
	for _, c := range []struct {
		name    string
		missing bool // device 1 is gone once the replace finished
		wantErr string
	}{
		{"installed", false, ""},
		{"missing", true, "the replace of device 1 finished, but the device is missing from pool " + dataUUID},
	} {
		k := raid1Kernel()
		if c.missing {
			k.devices[1].path = ""
		}
		replaceStates(k, replaceStateStarted, replaceStateStarted, replaceStateFinished)
		stubKernel(t, k)
		var reported []float64
		err := ResumeReplace(context.Background(), dataUUID, 1, time.Time{}, func(p float64) { reported = append(reported, p) })
		if (c.wantErr == "" && err != nil) || (c.wantErr != "" && (err == nil || err.Error() != c.wantErr)) {
			t.Fatalf("%s: err = %v, want %q", c.name, err, c.wantErr)
		}
		if calls := k.Calls(); len(calls) != 0 {
			t.Fatalf("%s: calls = %v, want none: a replace is followed, never started", c.name, calls)
		}
		if !reflect.DeepEqual(reported, []float64{25, 25}) {
			t.Fatalf("%s: reported = %v", c.name, reported)
		}
	}
}

// STATUS keeps the last replace's end, so a FINISHED read at once is this
// job's only when that replace started no earlier than the job.
func TestResumeReplaceRefusesAStaleFinished(t *testing.T) {
	since := time.Unix(1_800_000_000, 0)
	for _, c := range []struct {
		name    string
		state   uint64
		started uint64
		since   time.Time
	}{
		{"an earlier replace finished", replaceStateFinished, 1_799_999_999, since},
		{"no job start to compare", replaceStateFinished, 1_800_000_000, time.Time{}},
		{"canceled", replaceStateCanceled, 1_800_000_000, since},
		{"never started", 0, 0, since},
	} {
		k := raid1Kernel()
		replaceStates(k, c.state)
		k.replaceStarted = c.started
		stubKernel(t, k)
		err := ResumeReplace(context.Background(), dataUUID, 2, c.since, func(float64) {})
		if want := "the replace of device 2 did not survive the interruption; start it again"; err == nil || err.Error() != want {
			t.Fatalf("%s: err = %v, want %s", c.name, err, want)
		}
		if calls := k.Calls(); len(calls) != 0 {
			t.Fatalf("%s: calls = %v, want none", c.name, calls)
		}
	}
}

// After a power loss the kernel may finish the replace at mount before the
// host starts: a FINISHED that started in the job's second or later is it.
func TestResumeReplaceTrustsAFinishedReplaceItStarted(t *testing.T) {
	since := time.Unix(1_800_000_000, 600_000_000) // the kernel keeps whole seconds
	for _, started := range []uint64{1_800_000_000, 1_800_000_042} {
		k := raid1Kernel()
		replaceStates(k, replaceStateFinished)
		k.replaceStarted = started
		stubKernel(t, k)
		if err := ResumeReplace(context.Background(), dataUUID, 2, since, func(float64) {}); err != nil {
			t.Fatalf("started %d: %v", started, err)
		}
		if calls := k.Calls(); len(calls) != 0 {
			t.Fatalf("started %d: calls = %v, want none", started, calls)
		}
	}
	// It still needs the device in the pool.
	k := raid1Kernel()
	k.devices[2].path = ""
	replaceStates(k, replaceStateFinished)
	k.replaceStarted = 1_800_000_000
	stubKernel(t, k)
	if err := ResumeReplace(context.Background(), dataUUID, 2, since, func(float64) {}); err == nil || !strings.Contains(err.Error(), "the device is missing") {
		t.Fatalf("missing device: err = %v", err)
	}
}

// The context ending stops the following but leaves the replace to the
// caller, which cancels it only when the user asked.
func TestResumeReplaceDoesNotCancelOnContextEnd(t *testing.T) {
	k := raid1Kernel()
	replaceStates(k, replaceStateStarted)
	stubKernel(t, k)
	ctx, cancel := context.WithCancel(context.Background())
	err := ResumeReplace(ctx, dataUUID, 2, time.Time{}, func(float64) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
	}
	if err = CancelReplace(context.Background(), dataUUID); err != nil {
		t.Fatal(err)
	}
	if calls := k.Calls(); !reflect.DeepEqual(calls, []string{"REPLACE_CANCEL"}) {
		t.Fatalf("calls after CancelReplace = %v", calls)
	}
}

func TestResumeReplaceReportsSuspended(t *testing.T) {
	k := raid1Kernel()
	replaceStates(k, replaceStateSuspended)
	stubKernel(t, k)
	err := ResumeReplace(context.Background(), dataUUID, 2, time.Time{}, func(float64) {})
	if want := "the kernel suspended the replace of device 2; remount the pool to continue it"; err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %s", err, want)
	}
	if calls := k.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %v, want none", calls)
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
