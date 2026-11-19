package packages

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"

	pkgkit "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/packages/internal/packagekit"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// savedCheckpoint is a checkpoint the job saved, with how many packages it had
// asked PackageKit to update by then.
type savedCheckpoint struct {
	checkpoint  packageCheckpoint
	callsBefore int
}

// captureCheckpoints records what the job saves in its record instead of a
// job host's store.
func captureCheckpoints(t *testing.T, service *fakePackageKit) func() []savedCheckpoint {
	t.Helper()
	var mu sync.Mutex
	var saved []savedCheckpoint
	previous := saveCheckpoint
	saveCheckpoint = func(_ context.Context, value any) error {
		mu.Lock()
		defer mu.Unlock()
		checkpoint, ok := value.(packageCheckpoint)
		if !ok {
			t.Errorf("saved %T, want a packageCheckpoint", value)
		}
		saved = append(saved, savedCheckpoint{checkpoint: checkpoint, callsBefore: len(service.updatePackageCalls())})
		return nil
	}
	t.Cleanup(func() { saveCheckpoint = previous })
	return func() []savedCheckpoint {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(saved)
	}
}

func checkpointOf(t *testing.T, cp packageCheckpoint) jsontext.Value {
	t.Helper()
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("encode checkpoint: %v", err)
	}
	return raw
}

func ignoreProgress(*PkgUpdateProgress) error { return nil }

const pickedTransaction = godbus.ObjectPath("/7_picked")

var threePackages = []string{"first;1.0;x86_64;repo", "second;1.0;x86_64;repo", "third;1.0;x86_64;repo"}

// The job saves which package it is at and the transaction updating it before
// it asks PackageKit to update that package, with how the packages before it
// went, so a pick-up never updates a package a second time.
func TestPackageUpdateSavesWhereItStandsBeforeEachUpdate(t *testing.T) {
	service := setupFakePackageKit(t, false)
	saved := captureCheckpoints(t, service)
	service.setUpdateError(threePackages[1], "not available yet")

	if err := updatePackagesWithProgress(context.Background(), threePackages, ignoreProgress); err == nil {
		t.Fatal("updatePackagesWithProgress succeeded, want the second package's failure")
	}
	got := saved()
	if len(got) != 3 {
		t.Fatalf("saved %d checkpoints, want one per package: %+v", len(got), got)
	}
	for index, s := range got {
		if s.callsBefore != index {
			t.Fatalf("checkpoint %d saved after %d updates, want before the update of its own package", index, s.callsBefore)
		}
		if s.checkpoint.Index != index || !godbus.ObjectPath(s.checkpoint.Transaction).IsValid() {
			t.Fatalf("checkpoint %d = %+v, want its index and a transaction", index, s.checkpoint)
		}
	}
	if got[0].checkpoint.Transaction == got[1].checkpoint.Transaction {
		t.Fatalf("checkpoints name one transaction for two packages: %+v", got)
	}
	last := got[2].checkpoint
	if last.Updated != 1 || len(last.Failures) != 1 || !strings.Contains(last.Failures[0], "not available yet") {
		t.Fatalf("last checkpoint = %+v, want one updated and the second package's failure", last)
	}
}

// Picked up while PackageKit still runs its transaction, the job follows that
// transaction to its end, never updates its package again, and updates the
// packages it had not reached.
func TestPickUpFollowsTheTransactionPackageKitStillRuns(t *testing.T) {
	service := setupFakePackageKit(t, false)
	saved := captureCheckpoints(t, service)
	tx := service.runTransaction(pickedTransaction, pkgkit.RoleUpdatePackages)
	checkpoint := checkpointOf(t, packageCheckpoint{Index: 1, Transaction: string(pickedTransaction), Updated: 1})

	done := make(chan error, 1)
	go func() {
		done <- pickUpPackageUpdate(context.Background(), threePackages, checkpoint, ignoreProgress)
	}()
	waitListed(t, service)
	select {
	case err := <-done:
		t.Fatalf("the pick-up ended (%v) while PackageKit ran its transaction", err)
	case <-time.After(50 * time.Millisecond):
	}
	tx.finish("")

	if err := waitDone(t, done); err != nil {
		t.Fatalf("pick-up: %v", err)
	}
	if got := service.updatePackageCalls(); !slices.EqualFunc(got, [][]string{{threePackages[2]}}, slices.Equal) {
		t.Fatalf("UpdatePackages calls = %v, want only the package the job had not reached", got)
	}
	if got := saved(); len(got) != 1 || got[0].checkpoint.Index != 2 || got[0].checkpoint.Updated != 2 {
		t.Fatalf("saved %+v, want the third package's checkpoint counting two updated", got)
	}
}

// waitListed waits until the pick-up, already watching the transaction, asks
// PackageKit which transactions run.
func waitListed(t *testing.T, service *fakePackageKit) {
	t.Helper()
	select {
	case <-service.listed:
	case <-time.After(5 * time.Second):
		t.Fatal("the pick-up did not ask PackageKit which transactions run")
	}
}

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the pick-up did not end")
		return nil
	}
}

// Picked up after PackageKit ended its transaction, the job reads how it went
// from PackageKit's history and goes on from there.
func TestPickUpReadsHowAnEndedTransactionWent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		packages  []string
		index     int
		succeeded bool
		wantErr   string
		wantCalls [][]string
	}{
		{name: "succeeded", packages: threePackages, index: 1, succeeded: true, wantCalls: [][]string{{threePackages[2]}}},
		{name: "succeeded last", packages: threePackages, index: 2, succeeded: true},
		{name: "failed", packages: threePackages, index: 1, wantErr: "updated 2 of 3 packages; 1 failed: " + threePackages[1] + ": PackageKit recorded transaction /7_picked as failed", wantCalls: [][]string{{threePackages[2]}}},
		{name: "failed alone", packages: threePackages[:1], index: 0, wantErr: "PackageKit recorded transaction /7_picked as failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := setupFakePackageKit(t, false)
			captureCheckpoints(t, service)
			service.setHistory(
				fakeOldTransaction{path: "/9_later", succeeded: true, role: pkgkit.RoleUpdatePackages, duration: 900},
				fakeOldTransaction{path: pickedTransaction, succeeded: tc.succeeded, role: pkgkit.RoleUpdatePackages, duration: 1500},
			)
			checkpoint := checkpointOf(t, packageCheckpoint{Index: tc.index, Transaction: string(pickedTransaction), Updated: tc.index})

			err := pickUpPackageUpdate(context.Background(), tc.packages, checkpoint, ignoreProgress)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr) {
				t.Fatalf("pick-up = %v, want %q", err, tc.wantErr)
			}
			if errors.Is(err, bridgeipc.ErrNotResumed) {
				t.Fatalf("pick-up = %v, a known outcome is not an interruption", err)
			}
			if got := service.updatePackageCalls(); !slices.EqualFunc(got, tc.wantCalls, slices.Equal) {
				t.Fatalf("UpdatePackages calls = %v, want %v", got, tc.wantCalls)
			}
		})
	}
}

// When PackageKit cannot account for the transaction the job was in, or it is
// not the job's, the job stays interrupted and updates nothing.
func TestPickUpLeavesInterruptedWhatPackageKitCannotAccountFor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		checkpoint *packageCheckpoint
		running    uint32 // the role of a transaction PackageKit runs at its path; 0 for none
		history    []fakeOldTransaction
	}{
		{name: "no transaction yet"},
		{name: "checkpoint of another batch", checkpoint: &packageCheckpoint{Index: 5, Transaction: string(pickedTransaction)}},
		{name: "unknown to PackageKit", checkpoint: &packageCheckpoint{Index: 1, Transaction: string(pickedTransaction)}},
		{
			name:       "only another transaction in the history",
			checkpoint: &packageCheckpoint{Index: 1, Transaction: string(pickedTransaction)},
			history:    []fakeOldTransaction{{path: "/9_other", succeeded: true, role: pkgkit.RoleUpdatePackages, duration: 900}},
		},
		{
			name:       "never ended",
			checkpoint: &packageCheckpoint{Index: 1, Transaction: string(pickedTransaction)},
			history:    []fakeOldTransaction{{path: pickedTransaction, role: pkgkit.RoleUpdatePackages}},
		},
		{
			name:       "another role in the history",
			checkpoint: &packageCheckpoint{Index: 1, Transaction: string(pickedTransaction)},
			history:    []fakeOldTransaction{{path: pickedTransaction, succeeded: true, role: 13, duration: 900}},
		},
		{
			name:       "another role running",
			checkpoint: &packageCheckpoint{Index: 1, Transaction: string(pickedTransaction)},
			running:    13,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := setupFakePackageKit(t, false)
			captureCheckpoints(t, service)
			service.setHistory(tc.history...)
			if tc.running != 0 {
				service.runTransaction(pickedTransaction, tc.running)
			}
			var checkpoint jsontext.Value
			if tc.checkpoint != nil {
				checkpoint = checkpointOf(t, *tc.checkpoint)
			}

			err := pickUpPackageUpdate(context.Background(), threePackages, checkpoint, ignoreProgress)
			if !errors.Is(err, bridgeipc.ErrNotResumed) {
				t.Fatalf("pick-up = %v, want the job left interrupted", err)
			}
			if got := service.updatePackageCalls(); len(got) != 0 {
				t.Fatalf("UpdatePackages calls = %v, want none", got)
			}
		})
	}
}

// Cancel after a pick-up asks PackageKit to cancel the transaction the job
// follows, and updates no further package.
func TestCancelAfterPickUpCancelsTheTransaction(t *testing.T) {
	service := setupFakePackageKit(t, false)
	captureCheckpoints(t, service)
	tx := service.runTransaction(pickedTransaction, pkgkit.RoleUpdatePackages)
	checkpoint := checkpointOf(t, packageCheckpoint{Index: 0, Transaction: string(pickedTransaction)})
	following := make(chan struct{}, 1)
	report := func(p *PkgUpdateProgress) error {
		if p.Type == "item_progress" {
			select {
			case following <- struct{}{}:
			default:
			}
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pickUpPackageUpdate(ctx, threePackages, checkpoint, report) }()
	waitListed(t, service)
	// Its progress reaching the job shows the job follows the transaction.
	tx.emit("ItemProgress", threePackages[0], uint32(10), uint32(50))
	select {
	case <-following:
	case <-time.After(5 * time.Second):
		t.Fatal("the pick-up did not follow the transaction")
	}
	cancel()

	if err := waitDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("pick-up = %v, want canceled", err)
	}
	if got := service.canceled(); !slices.Equal(got, []godbus.ObjectPath{pickedTransaction}) {
		t.Fatalf("PackageKit Cancel on %v, want on %s", got, pickedTransaction)
	}
	if got := service.updatePackageCalls(); len(got) != 0 {
		t.Fatalf("UpdatePackages calls = %v, want none after the cancel", got)
	}
}

// A job its host stops is not canceled in PackageKit: the transaction runs on
// and the next host picks the job up.
func TestHostStopLeavesTheTransactionRunning(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.setUpdateHang(threePackages[0])
	task, _, err := bridgeipc.NewTaskService().ClaimForOwnerWithIdentity("packages.update", nil, bridgeipc.TaskOwner{}, bridgeipc.TaskLifetimeSession, bridgeipc.TaskIdentity{})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	done := make(chan error, 1)
	task.Start(func(ctx context.Context, _ *bridgeipc.Task, _ any) (any, error) {
		err := updatePackagesWithProgress(ctx, threePackages, ignoreProgress)
		done <- err
		return nil, err
	})
	select {
	case <-service.updateStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the update did not start")
	}
	task.BeginInterrupt()
	task.Interrupt()

	if err := waitDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("update = %v, want stopped", err)
	}
	if got := service.canceled(); len(got) != 0 {
		t.Fatalf("PackageKit Cancel on %v, want none for a host stop", got)
	}
}

// A transaction PackageKit lists as running but drops before the job asks
// its role, as it does a while after one ends, is read from the history.
func TestPickUpReadsTheHistoryOfATransactionDroppedWhileAsked(t *testing.T) {
	service := setupFakePackageKit(t, false)
	captureCheckpoints(t, service)
	service.listGone(pickedTransaction)
	service.setHistory(fakeOldTransaction{path: pickedTransaction, succeeded: true, role: pkgkit.RoleUpdatePackages, duration: 1500})
	checkpoint := checkpointOf(t, packageCheckpoint{Index: 1, Transaction: string(pickedTransaction), Updated: 1})

	if err := pickUpPackageUpdate(context.Background(), threePackages, checkpoint, ignoreProgress); err != nil {
		t.Fatalf("pick-up = %v, want the history's success", err)
	}
	if got := service.updatePackageCalls(); !slices.EqualFunc(got, [][]string{{threePackages[2]}}, slices.Equal) {
		t.Fatalf("UpdatePackages calls = %v, want only the package the job had not reached", got)
	}
}

// A transaction PackageKit ends with an exit other than success and no
// ErrorCode (a key or EULA required, a skipped transaction) changed nothing:
// a failed package, and a failed install.
func TestFinishedWithoutSuccessIsAFailure(t *testing.T) {
	service := setupFakePackageKit(t, false)
	const packageID = "keyed;1.0;x86_64;repo"
	service.setExit(packageID, 4) // PK_EXIT_ENUM_KEY_REQUIRED

	if err := updatePackagesWithProgress(context.Background(), []string{packageID}, ignoreProgress); err == nil || !strings.Contains(err.Error(), "exit 4") {
		t.Fatalf("update = %v, want the exit as a failure", err)
	}
	if err := installPackageWithProgress(context.Background(), packageID, nil); err == nil || !strings.Contains(err.Error(), "exit 4") {
		t.Fatalf("install = %v, want the exit as a failure", err)
	}
}

// PackageKit refuses a Cancel while dpkg runs: the job still ends canceled
// and updates no further package.
func TestCancelPackageKitRefusesStopsTheBatch(t *testing.T) {
	service := setupFakePackageKit(t, false)
	service.refuseCancel = true
	service.setUpdateHang(threePackages[0])
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- updatePackagesWithProgress(ctx, threePackages, ignoreProgress) }()
	select {
	case <-service.updateStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the update did not start")
	}
	cancel()

	if err := waitDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("update = %v, want canceled", err)
	}
	if got := service.canceled(); len(got) != 1 {
		t.Fatalf("PackageKit Cancel calls = %v, want one", got)
	}
	if got := service.updatePackageCalls(); len(got) != 1 {
		t.Fatalf("UpdatePackages calls = %v, want none after the cancel", got)
	}
}

// A job left interrupted says how far its batch got, which its checkpoint
// alone knew.
func TestPickUpLeftInterruptedSaysHowFarTheBatchGot(t *testing.T) {
	service := setupFakePackageKit(t, false)
	captureCheckpoints(t, service)
	checkpoint := checkpointOf(t, packageCheckpoint{Index: 1, Transaction: string(pickedTransaction), Updated: 1})

	err := pickUpPackageUpdate(context.Background(), threePackages, checkpoint, ignoreProgress)
	want := "it was updating " + threePackages[1] + ", 1 of the 1 packages before it were updated and 1 were not reached"
	if !errors.Is(err, bridgeipc.ErrNotResumed) || !strings.Contains(err.Error(), want) {
		t.Fatalf("pick-up = %v, want it left interrupted saying %q", err, want)
	}
}
