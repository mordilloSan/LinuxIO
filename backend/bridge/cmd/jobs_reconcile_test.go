package cmd

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
)

type fakeRootHost struct {
	connects int
	resumed  []string
	refuse   string
	closed   int
}

func (h *fakeRootHost) Resume(_ context.Context, id string) error {
	h.resumed = append(h.resumed, id)
	if id == h.refuse {
		return errors.New("resume refused")
	}
	return nil
}

func (h *fakeRootHost) Close() error {
	h.closed++
	return nil
}

func reconcileRecord(n int, route string, state jobs.State, resumable bool) jobs.Record {
	now := time.Now().UTC()
	return jobs.Record{
		ID:        fmt.Sprintf("00000000-0000-4000-8000-%012x", n),
		Route:     route,
		Owner:     jobs.Owner{UID: 0, Username: "root"},
		State:     state,
		CreatedAt: now,
		StartedAt: now,
		Request:   jsontext.Value(`{}`),
		Executor:  jobs.Executor{Kind: "host", PID: 1, PIDStart: 1},
		Resumable: resumable,
		Protocol:  jobs.Protocol,
	}
}

func seedStore(t *testing.T, dir string, records ...jobs.Record) *jobs.Store {
	t.Helper()
	store := jobs.NewStore(dir, t.TempDir())
	for _, r := range records {
		if err := store.Create(t.Context(), r); err != nil {
			t.Fatalf("Create(%s): %v", r.ID, err)
		}
	}
	return store
}

func TestReconcileMarksRebootAndResumesResumable(t *testing.T) {
	root := t.TempDir()
	refused := reconcileRecord(1, "storage.btrfs_scrub_start", jobs.StateRunning, true)
	resumable := reconcileRecord(2, "storage.btrfs_balance", jobs.StateRunning, true)
	plain := reconcileRecord(3, "storage.format", jobs.StateRunning, false)
	plain.Error = &jobs.Error{Message: "disk vanished"}
	done := reconcileRecord(4, "storage.format", jobs.StateCompleted, false)
	// An orderly shutdown already marked these and the host alerted them.
	suspended := reconcileRecord(7, "storage.btrfs_device_remove", jobs.StateInterrupted, true)
	suspended.Reason = jobs.ReasonShutdown
	stopped := reconcileRecord(8, "storage.format", jobs.StateInterrupted, false)
	stopped.Reason = jobs.ReasonShutdown
	userJob := reconcileRecord(5, "filebrowser.copy", jobs.StateRunning, false)
	userResumable := reconcileRecord(6, "storage.btrfs_balance", jobs.StateRunning, true)
	userSuspended := reconcileRecord(9, "storage.btrfs_balance", jobs.StateInterrupted, true)
	userSuspended.Reason = jobs.ReasonShutdown
	rootStore := seedStore(t, filepath.Join(root, "0"), refused, resumable, plain, done, suspended, stopped)
	userStore := seedStore(t, filepath.Join(root, "1000"), userJob, userResumable, userSuspended)
	if err := os.Mkdir(filepath.Join(root, "lost+found"), 0o700); err != nil {
		t.Fatal(err)
	}

	host := &fakeRootHost{refuse: refused.ID}
	var raised []alerts.Observation
	var atReady *jobs.Record
	var actedAtReady int
	err := reconcileJobs(t.Context(), reconcileDeps{
		root: root,
		ready: func() {
			r, _ := userStore.Get(t.Context(), userJob.ID)
			atReady, actedAtReady = &r, host.connects+len(raised)
		},
		connect: func(context.Context) (resumer, error) {
			host.connects++
			return host, nil
		},
		raise: func(_ context.Context, obs alerts.Observation) error {
			raised = append(raised, obs)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcileJobs: %v", err)
	}
	// Hosts start on READY, so every store is marked by then and nothing has
	// needed the root host yet.
	if atReady == nil || atReady.State != jobs.StateInterrupted || actedAtReady != 0 {
		t.Errorf("at READY the user job was %+v with %d connects and alerts; want it interrupted and none", atReady, actedAtReady)
	}

	// Every interrupted resumable root job is resumed, whatever interrupted
	// it; the refused resume does not stop the others, and one connection
	// serves them all.
	slices.Sort(host.resumed)
	if want := []string{refused.ID, resumable.ID, suspended.ID}; !slices.Equal(host.resumed, want) {
		t.Errorf("resumed %v, want %v", host.resumed, want)
	}
	if host.connects != 1 || host.closed != 1 {
		t.Errorf("connects = %d, closes = %d; want one of each", host.connects, host.closed)
	}

	// Alerts cover only the root jobs this run marked that will not resume.
	// User stores are marked but never alert or resume: a user can write any
	// record there.
	want := []alerts.Observation{
		{Source: "jobs", Key: plain.ID, Severity: alerts.SeverityError, Title: "storage.format was interrupted by a reboot", Message: "disk vanished", Link: "/"},
	}
	if !slices.Equal(raised, want) {
		t.Errorf("raised %+v\nwant %+v", raised, want)
	}

	assertJobStates(t, rootStore, jobs.StateInterrupted, jobs.ReasonReboot, refused, resumable, plain)
	assertJobStates(t, userStore, jobs.StateInterrupted, jobs.ReasonReboot, userJob, userResumable)
	assertJobStates(t, rootStore, jobs.StateCompleted, jobs.ReasonNone, done)
	assertJobStates(t, rootStore, jobs.StateInterrupted, jobs.ReasonShutdown, suspended, stopped)
	assertJobStates(t, userStore, jobs.StateInterrupted, jobs.ReasonShutdown, userSuspended)
}

func assertJobStates(t *testing.T, store *jobs.Store, state jobs.State, reason jobs.Reason, records ...jobs.Record) {
	t.Helper()
	for _, record := range records {
		r, err := store.Get(t.Context(), record.ID)
		if err != nil {
			t.Fatalf("Get(%s): %v", record.ID, err)
		}
		if r.State != state || r.Reason != reason {
			t.Errorf("job %s is %s/%s, want %s/%s", record.ID, r.State, r.Reason, state, reason)
		}
	}
}

func TestReconcileAlertsWhenTheRootHostIsUnreachable(t *testing.T) {
	root := t.TempDir()
	first := reconcileRecord(1, "storage.btrfs_balance", jobs.StateRunning, true)
	second := reconcileRecord(2, "storage.btrfs_scrub_start", jobs.StateRunning, true)
	plain := reconcileRecord(3, "storage.format", jobs.StateRunning, false)
	seedStore(t, filepath.Join(root, "0"), first, second, plain)

	connects := 0
	var raised []string
	err := reconcileJobs(t.Context(), reconcileDeps{
		root:  root,
		ready: func() {},
		connect: func(context.Context) (resumer, error) {
			connects++
			return nil, errors.New("job host did not start")
		},
		raise: func(_ context.Context, obs alerts.Observation) error {
			raised = append(raised, obs.Key)
			return errors.New("alert store unwritable")
		},
	})
	if err != nil {
		t.Fatalf("reconcileJobs: %v", err)
	}
	if connects != 1 {
		t.Errorf("connects = %d, want 1: a host that did not start is not retried per job", connects)
	}
	if !slices.Equal(raised, []string{plain.ID}) {
		t.Errorf("raised %v, want only the non-resumable job", raised)
	}
}

func TestReconcileJobsDirectory(t *testing.T) {
	readied := 0
	deps := reconcileDeps{
		ready:   func() { readied++ },
		connect: func(context.Context) (resumer, error) { t.Fatal("connect called"); return nil, nil },
		raise:   func(context.Context, alerts.Observation) error { t.Fatal("raise called"); return nil },
	}
	deps.root = filepath.Join(t.TempDir(), "missing")
	if err := reconcileJobs(t.Context(), deps); err != nil {
		t.Fatalf("missing jobs directory: %v, want nil", err)
	}
	deps.root = filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(deps.root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reconcileJobs(t.Context(), deps); err == nil {
		t.Fatal("unreadable jobs directory: nil error, want one")
	}
	// Hosts wait on this unit's readiness, so even a failed run signals it.
	if readied != 2 {
		t.Errorf("readiness signalled %d times, want 2", readied)
	}
}

func TestHandBackCandidatesSkipsStrayFiles(t *testing.T) {
	dir := t.TempDir()
	rewritten := reconcileRecord(1, "storage.wipe", jobs.StateRunning, false)
	untouched := reconcileRecord(2, "storage.trim", jobs.StateCompleted, false)
	seedStore(t, dir, rewritten, untouched)
	stray := filepath.Join(dir, "shadow")
	if err := os.WriteFile(stray, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := handBackCandidates(dir, []jobs.Record{rewritten})
	want := []string{filepath.Join(dir, ".lock"), filepath.Join(dir, rewritten.ID+".json")}
	if !slices.Equal(got, want) {
		t.Errorf("handBackCandidates = %q, want %q", got, want)
	}
}
