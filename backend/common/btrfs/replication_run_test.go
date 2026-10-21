package btrfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

const replPayload = "btrfs-send-stream-0123456789"

// replStub serves a source filesystem through runBtrfs and fakes the send,
// receive and ssh commands through execCommand, recording every argv.
type replStub struct {
	top       string
	calls     [][]string // execCommand argv, base name first
	mutations [][]string // runBtrfs calls that change something
	target    []string   // target subvolume list rows
	onReceive string     // row added to target when a receive runs
	showErr   error      // local `subvolume show` result
	show      string     // `subvolume show` output, with the target's "Subvolume ID:" line
	order     []string   // target-side steps in order: create, receive, delete
}

func stubReplication(t *testing.T, local []subvolumeRow, target []string) *replStub {
	t.Helper()
	s := &replStub{top: t.TempDir(), target: target, show: "backup\n\tName: \t\t\tbackup\n\tSubvolume ID: \t\t256\n"}
	stubMounts(t, []Mount{{Mountpoint: s.top, UUID: rootUUID, SubvolID: 5}})
	stubSubvolumes(t, local, 256, &s.mutations)
	bin := t.TempDir()
	for _, name := range []string{"btrfs", "ssh"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldBtrfs, oldExec, oldSearch, oldDir, oldSend := runBtrfs, execCommand, searchDirs, ReplicationDir, sendStream
	searchDirs = append(slices.Clone(oldSearch), bin)
	ReplicationDir = filepath.Join(t.TempDir(), "btrfs-replication")
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "subvolume list -o -p -u -q -R "):
			return strings.Join(s.target, ""), nil
		case strings.HasPrefix(joined, "subvolume show "):
			if s.showErr != nil {
				return "", s.showErr
			}
			return s.show, nil
		case strings.HasPrefix(joined, "subvolume create "):
			s.showErr = nil // the target exists from now on
			s.mutations = append(s.mutations, args)
			s.order = append(s.order, joined)
			return "", nil
		}
		s.mutations = append(s.mutations, args)
		s.order = append(s.order, joined)
		return "", nil
	}
	sendStream = func(snap, parent string, w *os.File) error {
		args := []string{"btrfs", "send", snap}
		if parent != "" {
			args = []string{"btrfs", "send", "-p", parent, snap}
		}
		s.calls = append(s.calls, args)
		_, err := io.WriteString(w, replPayload)
		return err
	}
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		s.calls = append(s.calls, append([]string{filepath.Base(name)}, args...))
		switch {
		case slices.Contains(args, "receive"):
			s.order = append(s.order, "receive")
			if s.onReceive != "" {
				s.target = append(s.target, s.onReceive)
			}
			return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null")
		case strings.Contains(strings.Join(args, " "), "subvolume list"):
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "list", strings.Join(s.target, ""))
		case strings.Contains(strings.Join(args, " "), "subvolume show"):
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "show", s.show)
		}
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() {
		runBtrfs, execCommand, searchDirs, ReplicationDir, sendStream = oldBtrfs, oldExec, oldSearch, oldDir, oldSend
	})
	return s
}

// callsOf returns the recorded argv whose args contain word.
func (s *replStub) callsOf(word string) [][]string {
	var out [][]string
	for _, c := range s.calls {
		if slices.Contains(c[1:], word) {
			out = append(out, c)
		}
	}
	return out
}

func replAt(hour int) time.Time { return time.Date(2026, 10, 19, hour, 0, 0, 0, time.UTC) }

// targetRow is a direct child of the target subvolume (ID 256).
func targetRow(id int, base, received string) string {
	return targetRowAt(id, 256, "backup/"+base, received)
}

func targetRowAt(id, parent int, rel, received string) string {
	return fmt.Sprintf("ID %d gen 1 parent %d top level 5 parent_uuid - received_uuid %s uuid 7c3d2e1f-2222-4b3c-9d4e-%012d path %s\n", 500+id, parent, received, id, rel)
}

func localUUID(id int) string { return fmt.Sprintf("6b2c1f0e-1111-4a2b-8c3d-%012d", id) }

func TestReplicateFullWhenNoParent(t *testing.T) {
	t1 := SnapshotPath("@home", replAt(1), KindTimeline)
	t2 := SnapshotPath("@home", replAt(2), KindManual)
	t3 := SnapshotPath("@home", replAt(3), KindPreUpdate)
	s := stubReplication(t, []subvolumeRow{snapshotSubvolume(1, t1), snapshotSubvolume(2, t2), snapshotSubvolume(3, t3)}, nil)
	job := BtrfsReplicationJob{
		ID: "11111111-2222-4333-8444-555555555555", UUID: rootUUID, Path: "@home", After: AfterTimeline, Enabled: true,
		Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: "/mnt/backup"},
	}

	res, err := Replicate(context.Background(), job, nil)
	if err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	// Timeline jobs also send manual snapshots; pre-update ones never. The
	// receive starts first so the kernel's send has a reader.
	wantCalls := [][]string{
		{"btrfs", "receive", "/mnt/backup"},
		{"btrfs", "send", filepath.Join(s.top, t2)},
	}
	if !reflect.DeepEqual(s.calls, wantCalls) {
		t.Fatalf("calls =\n%q\nwant\n%q", s.calls, wantCalls)
	}
	if res.Sent == nil || res.Sent.Path != t2 || res.Parent != nil || res.UpToDate || res.Pruned != 0 {
		t.Fatalf("result = %+v", res)
	}
	if res.Bytes != int64(len(replPayload)) {
		t.Fatalf("Bytes = %d, want %d", res.Bytes, len(replPayload))
	}
	if len(s.mutations) != 0 {
		t.Fatalf("unexpected btrfs mutations %q", s.mutations)
	}
}

func TestReplicateIncremental(t *testing.T) {
	t1 := SnapshotPath("@home", replAt(1), KindTimeline)
	t2 := SnapshotPath("@home", replAt(2), KindTimeline)
	t3 := SnapshotPath("@home", replAt(3), KindTimeline)
	// t0 is a partial receive (no received_uuid): ignored and never pruned,
	// although as the oldest it would be pruned first if it counted.
	t0 := SnapshotPath("@home", replAt(0), KindTimeline)
	s := stubReplication(t,
		[]subvolumeRow{snapshotSubvolume(1, t1), snapshotSubvolume(2, t2), snapshotSubvolume(3, t3)},
		[]string{targetRow(9, filepath.Base(t0), "-"), targetRow(1, filepath.Base(t1), localUUID(1)), targetRow(2, filepath.Base(t2), localUUID(2))})
	s.onReceive = targetRow(3, filepath.Base(t3), localUUID(3))
	job := BtrfsReplicationJob{
		ID: "11111111-2222-4333-8444-555555555555", UUID: rootUUID, Path: "@home", After: AfterTimeline, Enabled: true,
		KeepOnTarget: 2,
		Target:       BtrfsReplicationTarget{Kind: TargetSSH, Path: "/srv/it's backup", Host: "nas", User: "root", Port: 2222},
	}

	res, err := Replicate(context.Background(), job, nil)
	if err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	quoted := `'/srv/it'\''s backup'`
	send := s.callsOf("send")
	if want := [][]string{{"btrfs", "send", "-p", filepath.Join(s.top, t2), filepath.Join(s.top, t3)}}; !reflect.DeepEqual(send, want) {
		t.Fatalf("send = %q, want %q", send, want)
	}
	recv := s.callsOf("receive")
	if want := [][]string{append(append([]string{"ssh"}, SSHArgs(job)...), "btrfs", "receive", quoted)}; !reflect.DeepEqual(recv, want) {
		t.Fatalf("receive = %q, want %q", recv, want)
	}
	del := s.callsOf("delete")
	wantDel := append(append([]string{"ssh"}, SSHArgs(job)...), "btrfs", "subvolume", "delete", `'/srv/it'\''s backup/`+filepath.Base(t1)+`'`)
	if len(del) != 1 || !reflect.DeepEqual(del[0], wantDel) {
		t.Fatalf("delete = %q, want %q", del, wantDel)
	}
	if res.Sent == nil || res.Sent.Path != t3 || res.Parent == nil || res.Parent.Path != t2 {
		t.Fatalf("result = %+v", res)
	}
	if res.Bytes != int64(len(replPayload)) || res.Pruned != 1 || res.UpToDate {
		t.Fatalf("result = %+v", res)
	}
}

func TestReplicateUpToDate(t *testing.T) {
	t1 := SnapshotPath("@home", replAt(1), KindManual)
	t2 := SnapshotPath("@home", replAt(2), KindTimeline)
	s := stubReplication(t,
		[]subvolumeRow{snapshotSubvolume(1, t1), snapshotSubvolume(2, t2)},
		[]string{targetRow(2, filepath.Base(t2), localUUID(2))})
	job := BtrfsReplicationJob{
		ID: "11111111-2222-4333-8444-555555555555", UUID: rootUUID, Path: "@home", After: AfterTimeline, Enabled: true,
		Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: "/mnt/backup"},
	}
	res, err := Replicate(context.Background(), job, nil)
	if err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	if !res.UpToDate || res.Sent != nil || res.Bytes != 0 {
		t.Fatalf("result = %+v, want up to date", res)
	}
	if len(s.calls) != 0 || len(s.mutations) != 0 {
		t.Fatalf("calls %q, mutations %q", s.calls, s.mutations)
	}

	// A manual job ignores timeline snapshots: only t1 is a candidate.
	s.target = []string{targetRow(1, filepath.Base(t1), localUUID(1))}
	job.After = AfterManual
	if res, err = Replicate(context.Background(), job, nil); err != nil || !res.UpToDate {
		t.Fatalf("manual job = %+v, %v; want up to date", res, err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("manual job ran %q", s.calls)
	}

	// A partial copy (no received_uuid) of the newest is not on the target.
	s.target = []string{targetRow(1, filepath.Base(t1), "-")}
	if res, err = Replicate(context.Background(), job, nil); err != nil || res.UpToDate || res.Sent == nil || res.Sent.Path != t1 {
		t.Fatalf("partial copy = %+v, %v; want t1 sent", res, err)
	}
}

// Only children of the target subvolume count, matched by parent ID so it
// works whatever path form btrfs-progs prints: a copy inside a nested
// subvolume or beside the target belongs to someone else.
func TestListTargetSnapshotsOnlyDirectChildren(t *testing.T) {
	t1 := filepath.Base(SnapshotPath("@home", replAt(1), KindTimeline))
	t2 := filepath.Base(SnapshotPath("@home", replAt(2), KindTimeline))
	t3 := filepath.Base(SnapshotPath("@home", replAt(3), KindTimeline))
	stubReplication(t, nil, []string{
		targetRowAt(1, 256, "backup/"+t1, localUUID(1)),
		targetRowAt(2, 600, "backup/sub/"+t2, localUUID(2)),
		targetRowAt(3, 700, "other/"+t3, localUUID(3)),
	})
	job := BtrfsReplicationJob{UUID: rootUUID, Path: "@home", Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: "/mnt/backup"}}
	got, err := ListTargetSnapshots(context.Background(), job, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/mnt/backup/"+t1 {
		t.Fatalf("snapshots = %+v, want only %s", got, t1)
	}
	// The target is the top level (ID 5).
	stubReplication(t, nil, []string{targetRowAt(1, 5, t1, localUUID(1)), targetRowAt(2, 256, "backup/"+t2, localUUID(2))})
	if got, err = ListTargetSnapshots(context.Background(), job, 5); err != nil || len(got) != 1 || got[0].Path != "/mnt/backup/"+t1 {
		t.Fatalf("top-level snapshots = %+v, %v", got, err)
	}
}

func TestEnsureTargetReadsSubvolumeID(t *testing.T) {
	s := stubReplication(t, nil, nil)
	job := BtrfsReplicationJob{UUID: rootUUID, Path: "@home", Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: "/mnt/backup"}}
	for show, want := range map[string]int{
		"@backup/home\n\tName: \t\t\thome\n\tSubvolume ID: \t\t312\n":  312,
		"/\n\tName: \t\t\t<FS_TREE>\n\tSubvolume ID: \t\t5\n":          5,
		"/mnt/backup\n\tName: \t\t\tbackup\n\tSubvolume ID: \t\t312\n": 312, // older btrfs-progs
	} {
		s.show = show
		if got, err := ensureTarget(context.Background(), job); err != nil || got != want {
			t.Errorf("show %q: ensureTarget = %d, %v; want %d", show, got, err, want)
		}
	}
	s.show = "@backup/home\n\tName: \t\t\thome\n"
	if _, err := ensureTarget(context.Background(), job); err == nil || !strings.Contains(err.Error(), "cannot determine the target subvolume id") {
		t.Errorf("show without an ID: err = %v", err)
	}
}

func TestReplicateCreatesMissingTarget(t *testing.T) {
	t1 := SnapshotPath("@home", replAt(1), KindTimeline)
	s := stubReplication(t, []subvolumeRow{snapshotSubvolume(1, t1)}, nil)
	s.showErr = errors.New("not a subvolume")
	target := filepath.Join(t.TempDir(), "missing")
	job := BtrfsReplicationJob{
		ID: "11111111-2222-4333-8444-555555555555", UUID: rootUUID, Path: "@home", After: AfterTimeline, Enabled: true,
		Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: target},
	}
	if _, err := Replicate(context.Background(), job, nil); err != nil {
		t.Fatalf("Replicate: %v", err)
	}
	if want := []string{"subvolume create " + target, "receive"}; !reflect.DeepEqual(s.order, want) {
		t.Fatalf("order = %q, want %q", s.order, want)
	}
}

func TestReplicateRejectsNonSubvolumeTarget(t *testing.T) {
	t1 := SnapshotPath("@home", replAt(1), KindTimeline)
	s := stubReplication(t, []subvolumeRow{snapshotSubvolume(1, t1)}, nil)
	s.showErr = errors.New("not a subvolume")
	target := t.TempDir() // exists, but `subvolume show` fails
	job := BtrfsReplicationJob{
		ID: "11111111-2222-4333-8444-555555555555", UUID: rootUUID, Path: "@home", After: AfterTimeline, Enabled: true,
		Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: target},
	}
	_, err := Replicate(context.Background(), job, nil)
	if err == nil || err.Error() != "target "+target+" is not a btrfs subvolume" {
		t.Fatalf("err = %v", err)
	}
	if len(s.calls) != 0 || len(s.mutations) != 0 {
		t.Fatalf("calls %q, mutations %q; want none", s.calls, s.mutations)
	}
}

func TestReplicateFailureRemovesPartialCopy(t *testing.T) {
	t1 := SnapshotPath("@home", replAt(1), KindTimeline)
	s := stubReplication(t, []subvolumeRow{snapshotSubvolume(1, t1)}, nil)
	sendStream = func(_, _ string, w *os.File) error {
		_, _ = io.WriteString(w, replPayload)
		return errors.New("send went wrong")
	}
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		s.calls = append(s.calls, append([]string{filepath.Base(name)}, args...))
		// The failed receive leaves a subvolume without a received_uuid.
		s.target = append(s.target, targetRow(1, filepath.Base(t1), "-"))
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null; echo 'no space left' >&2; exit 1")
	}
	job := BtrfsReplicationJob{
		ID: "11111111-2222-4333-8444-555555555555", UUID: rootUUID, Path: "@home", After: AfterTimeline, Enabled: true,
		Target: BtrfsReplicationTarget{Kind: TargetLocal, Path: "/mnt/backup"},
	}
	_, err := Replicate(context.Background(), job, nil)
	if err == nil || !strings.Contains(err.Error(), "send went wrong") || !strings.Contains(err.Error(), "no space left") {
		t.Fatalf("err = %v, want both stderr tails", err)
	}
	if !strings.Contains(err.Error(), "removed partial copy") {
		t.Fatalf("err = %v, want partial copy removal noted", err)
	}
	if want := [][]string{{"subvolume", "delete", "/mnt/backup/" + filepath.Base(t1)}}; !reflect.DeepEqual(s.mutations, want) {
		t.Fatalf("mutations = %q, want %q", s.mutations, want)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"":            "''",
		"/srv/backup": "'/srv/backup'",
		"it's":        `'it'\''s'`,
		"$(rm -rf /)": "'$(rm -rf /)'",
		"a b\nc":      "'a b\nc'",
		"''":          `''\'''\'''`,
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
		if out, err := exec.Command("sh", "-c", "printf '%s' "+shellQuote(in)).Output(); err != nil || string(out) != in {
			t.Errorf("sh round trip of %q = %q, %v", in, out, err)
		}
	}
}

func TestRunReplicationRecordsOutcome(t *testing.T) {
	useTempAlerts(t)
	oldPath, oldReplicate := ReplicationPath, replicate
	ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
	t.Cleanup(func() { ReplicationPath, replicate = oldPath, oldReplicate })
	failing := map[string]bool{"aaaaaaaa-0000-4000-8000-000000000001": true}
	var ran []string
	replicate = func(_ context.Context, job BtrfsReplicationJob, _ func(int64)) (BtrfsReplicationResult, error) {
		ran = append(ran, job.ID)
		if failing[job.ID] {
			return BtrfsReplicationResult{}, errors.New("boom")
		}
		return BtrfsReplicationResult{}, nil
	}
	jobs := []BtrfsReplicationJob{
		{ID: "aaaaaaaa-0000-4000-8000-000000000001", UUID: rootUUID, Path: "@home", Enabled: true},
		{ID: "aaaaaaaa-0000-4000-8000-000000000002", UUID: rootUUID, Path: "@", Enabled: true},
		{ID: "aaaaaaaa-0000-4000-8000-000000000003", UUID: rootUUID, Path: "data", Enabled: false},
	}
	ctx := context.Background()
	saveJobs(t, jobs)

	errs := RunReplication(ctx, jobs)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), jobs[0].ID+": boom") {
		t.Fatalf("errs = %v", errs)
	}
	if want := []string{jobs[0].ID, jobs[1].ID}; !reflect.DeepEqual(ran, want) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	stored, err := LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if stored[0].LastRun == nil || stored[0].LastError == nil || *stored[0].LastError != "boom" {
		t.Fatalf("failed job = %+v", stored[0])
	}
	if stored[1].LastRun == nil || stored[1].LastError != nil {
		t.Fatalf("ok job = %+v", stored[1])
	}
	if stored[2].LastRun != nil {
		t.Fatalf("disabled job ran: %+v", stored[2])
	}
	doc, err := alerts.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Alerts) != 1 {
		t.Fatalf("alerts = %+v, want one", doc.Alerts)
	}
	a := doc.Alerts[0]
	if a.ID != "btrfs/replication-"+jobs[0].ID || a.Severity != alerts.SeverityError ||
		a.Title != "Replication failed for @home" || a.Message != "boom" || a.Link != "/storage/btrfs?fs="+rootUUID {
		t.Fatalf("alert = %+v", a)
	}

	delete(failing, jobs[0].ID)
	if errs := RunReplication(ctx, jobs); len(errs) != 0 {
		t.Fatalf("second run errs = %v", errs)
	}
	if doc, _ = alerts.Load(); len(doc.Alerts) != 0 {
		t.Fatalf("alert not resolved: %+v", doc.Alerts)
	}
	if stored, _ = LoadJobs(); stored[0].LastError != nil {
		t.Fatalf("LastError not cleared: %+v", stored[0])
	}
}

func TestRecordReplicationOutcomeCancelledAndTruncated(t *testing.T) {
	useTempAlerts(t)
	old := ReplicationPath
	ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
	t.Cleanup(func() { ReplicationPath = old })
	job := BtrfsReplicationJob{ID: "aaaaaaaa-0000-4000-8000-000000000001", UUID: rootUUID, Path: "@home", Enabled: true}
	saveJobs(t, []BtrfsReplicationJob{job})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RecordReplicationOutcome(ctx, job, errors.New(strings.Repeat("x", 3*alerts.MaxMessageLen))); err != nil {
		t.Fatalf("RecordReplicationOutcome: %v", err)
	}
	stored, err := LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	if stored[0].LastRun == nil || stored[0].LastError == nil || len(*stored[0].LastError) != alerts.MaxMessageLen {
		t.Fatalf("stored = %+v", stored[0])
	}
	if doc, _ := alerts.Load(); len(doc.Alerts) != 1 {
		t.Fatalf("alerts = %+v, want one despite the cancelled ctx", doc.Alerts)
	}
}
