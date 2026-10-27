package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobsclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobslauncher"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/logging"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const (
	jobsStateRoot   = "/var/lib/linuxio/jobs"
	jobsRuntimeRoot = "/run/linuxio/jobs"
)

// resumer is the root job host as the boot reconcile uses it.
type resumer interface {
	Resume(ctx context.Context, id string) error
	Close() error
}

type hostResumer struct{ *jobsclient.Client }

func (h hostResumer) Resume(ctx context.Context, id string) error {
	return jobsclient.Resume(ctx, h.Client, id)
}

type reconcileDeps struct {
	root    string
	ready   func()
	connect func(ctx context.Context) (resumer, error)
	raise   func(ctx context.Context, obs alerts.Observation) error
}

// runJobsReconcile runs linuxio-jobs-reconcile.service (Type=notify) once per
// boot. It exits 1 only when the jobs directory cannot be read; a job it
// cannot resume or alert is logged and skipped.
func runJobsReconcile(stderr io.Writer) int {
	if err := logging.Configure("linuxio-jobs", false); err != nil {
		fmt.Fprintf(stderr, "linuxio-bridge jobs reconcile: journald unavailable, logging to stderr: %v\n", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	// After READY systemd no longer times the unit; bound the resumes here.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	err := reconcileJobs(ctx, reconcileDeps{
		root: jobsStateRoot,
		ready: func() {
			if err := systemd.Notify("READY=1"); err != nil {
				slog.Warn("job reconcile readiness not sent", "error", err)
			}
		},
		connect: func(ctx context.Context) (resumer, error) {
			hello := bridgeipc.HelloSession{Username: "root", UID: 0, Privileged: true, SessionRef: "reconcile"}
			c, err := jobsclient.Connect(ctx, 0, hello, jobslauncher.LauncherSocketPath, jobslauncher.HostSocketPath(0))
			if err != nil {
				return nil, err
			}
			return hostResumer{c}, nil
		},
		raise: alerts.Raise,
	})
	if err != nil {
		logging.ReportFatal(err)
		return 1
	}
	return 0
}

// reconcileJobs marks every uid's running jobs interrupted by a reboot,
// signals readiness, then resumes every interrupted resumable job of the root
// host, whatever interrupted it, and alerts on the non-resumable root jobs it
// marked itself (the host alerted those it marked at shutdown). Other uids'
// jobs are only marked: their users read them from tasks.list, and a record a
// user wrote must not reach the admin's alerts.
func reconcileJobs(ctx context.Context, deps reconcileDeps) error {
	work, err := markInterrupted(ctx, deps.root)
	// Job hosts are ordered after this unit, so readiness comes once no store
	// holds a running job of the last boot and before the root host is needed.
	deps.ready()
	if err != nil {
		return err
	}
	for _, record := range work.alerts {
		alertInterrupted(ctx, deps.raise, record)
	}
	if len(work.resumes) == 0 {
		return nil
	}
	host, err := deps.connect(ctx)
	if err != nil {
		for _, id := range work.resumes {
			slog.Warn("interrupted job not resumed", "job_id", id, "error", err)
		}
		return nil
	}
	defer func() { _ = host.Close() }()
	for _, id := range work.resumes {
		if err := host.Resume(ctx, id); err != nil {
			slog.Warn("interrupted job not resumed", "job_id", id, "error", err)
		}
	}
	return nil
}

type reconcileWork struct {
	alerts  []jobs.Record
	resumes []string
}

func markInterrupted(ctx context.Context, root string) (reconcileWork, error) {
	var work reconcileWork
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return work, nil
	}
	if err != nil {
		return work, fmt.Errorf("read jobs directory: %w", err)
	}
	for _, entry := range entries {
		uid, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err == nil && entry.IsDir() {
			work.markUID(ctx, root, entry.Name(), uid)
		}
	}
	return work, nil
}

func (w *reconcileWork) markUID(ctx context.Context, root, name string, uid uint64) {
	stateDir := filepath.Join(root, name)
	store := jobs.NewStore(stateDir, filepath.Join(jobsRuntimeRoot, name))
	// Every host of the last boot is gone, whatever pid now matches.
	marked, err := store.ReconcileCrashed(ctx, func(jobs.Executor) bool { return false }, jobs.ReasonReboot)
	if err != nil {
		slog.Warn("job store not reconciled", "uid", uid, "error", err)
	}
	if err = returnStoreToOwner(stateDir, handBackCandidates(stateDir, marked)); err != nil {
		slog.Warn("job store ownership not restored", "uid", uid, "error", err)
	}
	for _, record := range marked {
		slog.Info("job marked interrupted by reboot", "job_id", record.ID, "route", record.Route, "uid", uid)
		if uid == 0 && !record.Resumable {
			w.alerts = append(w.alerts, record)
		}
	}
	if uid != 0 {
		return
	}
	records, err := store.List(ctx)
	if err != nil {
		slog.Warn("interrupted jobs not listed", "uid", uid, "error", err)
		return
	}
	for _, record := range records {
		if record.State == jobs.StateInterrupted && record.Resumable {
			w.resumes = append(w.resumes, record.ID)
		}
	}
}

func alertInterrupted(ctx context.Context, raise func(context.Context, alerts.Observation) error, record jobs.Record) {
	obs := alerts.Observation{Source: "jobs", Key: record.ID, Severity: alerts.SeverityError, Title: record.Route + " was interrupted by a reboot", Link: "/"}
	if record.Error != nil {
		obs.Message = record.Error.Message
	}
	if err := raise(ctx, obs); err != nil {
		slog.Warn("job alert failed", "job_id", record.ID, "error", err)
	}
}

// handBackCandidates names the files this root process created in a store:
// the lock and the records ReconcileCrashed rewrote. Only these may be handed
// back; any other root-owned file there was put by someone else (a user can
// rename(2) a root file into their own store) and must stay root's.
func handBackCandidates(stateDir string, marked []jobs.Record) []string {
	paths := []string{filepath.Join(stateDir, ".lock")}
	for _, record := range marked {
		paths = append(paths, filepath.Join(stateDir, record.ID+".json"))
	}
	return paths
}

// returnStoreToOwner gives a user's store back the given files this root
// process created in it, which that user's host could not open otherwise.
// The user controls the directory, so each file is pinned with an O_PATH
// descriptor that does not follow links and changed only while it is a
// single-link regular file owned by root: a planted symlink or hard link
// cannot redirect the chown.
func returnStoreToOwner(stateDir string, paths []string) error {
	var dir unix.Stat_t
	if err := unix.Lstat(stateDir, &dir); err != nil {
		return fmt.Errorf("stat job store: %w", err)
	}
	if dir.Uid == 0 {
		return nil
	}
	var errs []error
	for _, path := range paths {
		errs = append(errs, chownRootFile(path, int(dir.Uid), int(dir.Gid)))
	}
	return errors.Join(errs...)
}

func chownRootFile(path string, uid, gid int) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Uid != 0 {
		return nil
	}
	if err := unix.Fchownat(fd, "", uid, gid, unix.AT_EMPTY_PATH); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}
