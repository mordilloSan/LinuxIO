package btrfs

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

const (
	// progressStep is how many sent bytes pass between progress reports.
	progressStep = 8 << 20
	// stderrTail bounds the stderr kept from each side of a send/receive.
	stderrTail = 64 << 10
)

// BtrfsReplicationResult is the outcome of one replication of a job.
type BtrfsReplicationResult struct {
	Sent     *BtrfsSnapshot `json:"sent,omitempty"`
	Parent   *BtrfsSnapshot `json:"parent,omitempty"`
	Bytes    int64          `json:"bytes"`
	Pruned   int            `json:"pruned"`
	UpToDate bool           `json:"upToDate"`
}

// replicate is swapped by tests.
var replicate = Replicate

// shellQuote single-quotes s for the remote shell ssh hands its command to.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// targetCommand runs btrfs args on the job's target, locally or over ssh. The
// last arg is a path; over ssh it is the only quoted, user-supplied word.
func targetCommand(ctx context.Context, job BtrfsReplicationJob, args ...string) (string, error) {
	if job.Target.Kind != TargetSSH {
		return runBtrfs(ctx, args...)
	}
	remote := append([]string{"btrfs"}, args[:len(args)-1]...)
	remote = append(remote, shellQuote(args[len(args)-1]))
	return runCommand(ctx, "ssh", append(SSHArgs(job), remote...)...)
}

// ListTargetSnapshots lists the child subvolumes of the target subvolume,
// whose subvolume ID is targetID, named like a LinuxIO snapshot leaf,
// including partial receives (no ReceivedUUID); see receivedOnly. Rows whose
// parent is another subvolume are ignored, so only this job's copies count.
func ListTargetSnapshots(ctx context.Context, job BtrfsReplicationJob, targetID int) ([]BtrfsSnapshot, error) {
	// Without -a, -o limits the list to children of the subvolume at the
	// path; -a would list the whole filesystem. -p prints each parent ID.
	out, err := targetCommand(ctx, job, "subvolume", "list", "-o", "-p", "-u", "-q", "-R", job.Target.Path)
	if err != nil {
		return nil, fmt.Errorf("list target snapshots in %s: %w", job.Target.Path, err)
	}
	rows, err := parseSubvolumeList(out)
	if err != nil {
		return nil, err
	}
	snaps := []BtrfsSnapshot{}
	for _, r := range rows {
		if r.ParentID != targetID {
			continue
		}
		base := path.Base(r.Path)
		source, at, kind, ok := ParseSnapshotPath(SnapshotRoot + "/" + EscapeSource(job.Path) + "/" + base)
		if !ok {
			continue
		}
		snaps = append(snaps, BtrfsSnapshot{
			Source: source, Path: filepath.Join(job.Target.Path, base), Kind: kind, CreatedAt: at,
			ID: r.ID, UUID: r.UUID, ReceivedUUID: r.ReceivedUUID,
		})
	}
	return snaps, nil
}

// receivedOnly keeps the completed copies; a receive sets ReceivedUUID last.
func receivedOnly(snaps []BtrfsSnapshot) []BtrfsSnapshot {
	return slices.DeleteFunc(snaps, func(s BtrfsSnapshot) bool { return s.ReceivedUUID == "" })
}

// ensureTarget requires the target path to be a btrfs subvolume, creating it
// when the path does not exist yet, and returns its subvolume ID from the
// "Subvolume ID:" line of `subvolume show`, which every btrfs-progs version
// prints. Its parent directory must exist.
func ensureTarget(ctx context.Context, job BtrfsReplicationJob) (int, error) {
	p := job.Target.Path
	out, err := targetCommand(ctx, job, "subvolume", "show", p)
	if err != nil {
		exists, existsErr := targetExists(ctx, job)
		if existsErr != nil {
			return 0, existsErr
		}
		if exists {
			return 0, fmt.Errorf("target %s is not a btrfs subvolume", p)
		}
		if _, err = targetCommand(ctx, job, "subvolume", "create", p); err != nil {
			return 0, fmt.Errorf("create target subvolume %s: %w", p, err)
		}
		if out, err = targetCommand(ctx, job, "subvolume", "show", p); err != nil {
			return 0, fmt.Errorf("show target subvolume %s: %w", p, err)
		}
	}
	for line := range strings.Lines(out) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Subvolume ID:"); ok {
			if id, convErr := strconv.Atoi(strings.TrimSpace(v)); convErr == nil {
				return id, nil
			}
		}
	}
	return 0, fmt.Errorf("cannot determine the target subvolume id of %s", p)
}

func targetExists(ctx context.Context, job BtrfsReplicationJob) (bool, error) {
	p := job.Target.Path
	if job.Target.Kind != TargetSSH {
		_, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("stat target %s: %w", p, err)
		}
		return true, nil
	}
	_, err := runCommand(ctx, "ssh", append(SSHArgs(job), "test", "-e", shellQuote(p))...)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check target %s: %w", p, err)
	}
	return true, nil
}

// removePartial deletes the target copy of leaf when a failed receive left
// it without a ReceivedUUID, reporting whether it did. It runs even when ctx
// is cancelled, bounded by a minute.
func removePartial(ctx context.Context, job BtrfsReplicationJob, targetID int, leaf string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	remote, err := ListTargetSnapshots(ctx, job, targetID)
	if err != nil {
		return false
	}
	for _, r := range remote {
		if path.Base(r.Path) == leaf && r.ReceivedUUID == "" {
			if _, err := targetCommand(ctx, job, "subvolume", "delete", r.Path); err != nil {
				slog.Warn("failed to remove partial btrfs receive", "path", r.Path, "error", err)
				return false
			}
			return true
		}
	}
	return false
}

// Replicate sends the newest snapshot of job.Path the job accepts to the
// target, incrementally from the newest snapshot the target already holds,
// then prunes the target to KeepOnTarget. Timeline jobs send timeline and
// manual snapshots, manual jobs only manual ones. Only completed copies
// count as on the target. It does not touch the job file; see
// RecordReplicationOutcome.
func Replicate(ctx context.Context, job BtrfsReplicationJob, progress func(bytes int64)) (BtrfsReplicationResult, error) {
	targetID, err := ensureTarget(ctx, job)
	if err != nil {
		return BtrfsReplicationResult{}, err
	}
	local, err := ListSnapshots(ctx, job.UUID, &job.Path)
	if err != nil {
		return BtrfsReplicationResult{}, err
	}
	remote, err := ListTargetSnapshots(ctx, job, targetID)
	if err != nil {
		return BtrfsReplicationResult{}, err
	}
	remote = receivedOnly(remote)
	var snap *BtrfsSnapshot
	for i := range local { // newest first
		if local[i].Kind == KindManual || (local[i].Kind == KindTimeline && job.After == AfterTimeline) {
			snap = &local[i]
			break
		}
	}
	if snap == nil || onTarget(remote, *snap) {
		return BtrfsReplicationResult{UpToDate: true}, nil
	}
	top, err := TopLevel(ctx, job.UUID)
	if err != nil {
		return BtrfsReplicationResult{}, err
	}
	result := BtrfsReplicationResult{Sent: snap, Parent: ChooseParent(local, remote)}
	parent := ""
	if result.Parent != nil {
		parent = filepath.Join(top, result.Parent.Path)
	}
	if result.Bytes, err = sendReceive(ctx, job, filepath.Join(top, snap.Path), parent, progress); err != nil {
		if removePartial(ctx, job, targetID, path.Base(snap.Path)) {
			err = fmt.Errorf("%w (removed partial copy)", err)
		}
		return result, err
	}
	if remote, err = ListTargetSnapshots(ctx, job, targetID); err != nil {
		return result, err
	}
	for _, s := range PruneTarget(receivedOnly(remote), job.KeepOnTarget) {
		if _, err := targetCommand(ctx, job, "subvolume", "delete", s.Path); err != nil {
			return result, fmt.Errorf("prune %s: %w", s.Path, err)
		}
		result.Pruned++
	}
	return result, nil
}

func onTarget(remote []BtrfsSnapshot, snap BtrfsSnapshot) bool {
	base := path.Base(snap.Path)
	for _, r := range remote {
		if path.Base(r.Path) == base {
			return true
		}
	}
	return false
}

// sendReceive pipes `btrfs send [-p parent] snap` into a local or ssh
// `btrfs receive`, copying in this goroutine, and returns the bytes sent.
func sendReceive(ctx context.Context, job BtrfsReplicationJob, snap, parent string, progress func(int64)) (int64, error) {
	btrfsPath, err := findCommand("btrfs")
	if err != nil {
		return 0, fmt.Errorf("btrfs: %w", err)
	}
	sendArgs := []string{"send"}
	if parent != "" {
		sendArgs = append(sendArgs, "-p", parent)
	}
	send := execCommand(ctx, btrfsPath, append(sendArgs, snap)...)
	recvName, recvArgs := btrfsPath, []string{"receive", job.Target.Path}
	if job.Target.Kind == TargetSSH {
		if recvName, err = findCommand("ssh"); err != nil {
			return 0, fmt.Errorf("ssh: %w", err)
		}
		recvArgs = append(SSHArgs(job), "btrfs", "receive", shellQuote(job.Target.Path))
	}
	recv := execCommand(ctx, recvName, recvArgs...)
	var sendErr, recvErr tailBuffer
	send.Env = append(os.Environ(), "LC_ALL=C")
	recv.Env = send.Env
	send.Stderr, recv.Stderr = &sendErr, &recvErr

	stdout, err := send.StdoutPipe()
	if err != nil {
		return 0, fmt.Errorf("btrfs send: %w", err)
	}
	if err = send.Start(); err != nil {
		return 0, fmt.Errorf("btrfs send: %w", err)
	}
	stdin, err := recv.StdinPipe()
	if err == nil {
		err = recv.Start()
	}
	if err != nil {
		// Closing the read end makes send exit on a broken pipe.
		_ = stdout.Close()
		_ = send.Wait()
		return 0, fmt.Errorf("btrfs receive: %w", err)
	}
	counter := &countingReader{r: stdout, progress: progress}
	_, copyErr := io.Copy(stdin, counter)
	// After a failed copy send may still be writing; closing both ends lets
	// each process see EOF or a broken pipe and exit before the waits.
	closeErr := stdin.Close()
	_ = stdout.Close()
	sendWait, recvWait := send.Wait(), recv.Wait()
	if err := cmp.Or(sendWait, recvWait, copyErr, closeErr); err != nil {
		return counter.n, fmt.Errorf("replicate %s to %s: %w (send: %s; receive: %s)", snap, job.Target.Path, err,
			strings.TrimSpace(string(sendErr.b)), strings.TrimSpace(string(recvErr.b)))
	}
	return counter.n, nil
}

// countingReader counts bytes read and reports them every progressStep.
type countingReader struct {
	r           io.Reader
	n, reported int64
	progress    func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.progress != nil && c.n-c.reported >= progressStep {
		c.reported = c.n
		c.progress(c.n)
	}
	return n, err
}

// tailBuffer keeps the last stderrTail bytes written to it.
type tailBuffer struct{ b []byte }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if over := len(t.b) - stderrTail; over > 0 {
		t.b = t.b[over:]
	}
	return len(p), nil
}

// RunReplication replicates each enabled job and records its outcome,
// returning one error per failed job. It stops early only when ctx is done.
func RunReplication(ctx context.Context, jobs []BtrfsReplicationJob) []error {
	var errs []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return append(errs, err)
		}
		if !job.Enabled {
			continue
		}
		_, err := replicate(ctx, job, nil)
		if recErr := RecordReplicationOutcome(ctx, job, err); recErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", job.ID, recErr))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", job.ID, err))
		}
	}
	return errs
}

// RecordReplicationOutcome stores LastRun and LastError on the job with the
// same ID and raises or resolves its alert.
// It records even when ctx is cancelled, so a cancelled run keeps its failure.
func RecordReplicationOutcome(ctx context.Context, job BtrfsReplicationJob, runErr error) error {
	ctx = context.WithoutCancel(ctx)
	now := time.Now().UTC()
	var lastErr *string
	key := "replication-" + job.ID
	if runErr != nil {
		msg := truncate(runErr.Error(), alerts.MaxMessageLen)
		lastErr = &msg
		sum := sha256.Sum256([]byte(msg))
		raiseAlert(ctx, alerts.Observation{
			Source: alertSource, Key: key, Severity: alerts.SeverityError,
			Title:        truncate("Replication failed for "+job.Path, 200),
			Message:      msg,
			Link:         "/storage/btrfs?fs=" + job.UUID,
			OccurrenceID: hex.EncodeToString(sum[:8]),
		})
	} else if err := alerts.Resolve(ctx, alertSource, key); err != nil {
		slog.Warn("failed to resolve btrfs replication alert", "job", job.ID, "error", err)
	}
	_, err := UpdateJobs(ctx, func(jobs *[]BtrfsReplicationJob) error {
		for i := range *jobs {
			if (*jobs)[i].ID == job.ID {
				(*jobs)[i].LastRun, (*jobs)[i].LastError = &now, lastErr
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record replication outcome: %w", err)
	}
	return nil
}
