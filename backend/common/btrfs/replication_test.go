package btrfs

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func replSnap(uuid, received string, hour int) BtrfsSnapshot {
	return BtrfsSnapshot{
		Path: "@/" + uuid, UUID: uuid, ReceivedUUID: received,
		CreatedAt: time.Date(2026, 10, 19, hour, 0, 0, 0, time.UTC),
	}
}

func TestChooseParentNoCommon(t *testing.T) {
	local := []BtrfsSnapshot{replSnap("l1", "", 1), replSnap("l2", "", 2)}
	remote := []BtrfsSnapshot{replSnap("r1", "other", 1)}
	if got := ChooseParent(local, remote); got != nil {
		t.Fatalf("ChooseParent = %+v, want nil", got)
	}
	if got := ChooseParent(local, nil); got != nil {
		t.Fatalf("ChooseParent(no remote) = %+v, want nil", got)
	}
	// An empty local UUID never matches an empty remote received UUID.
	if got := ChooseParent([]BtrfsSnapshot{replSnap("", "", 1)}, []BtrfsSnapshot{replSnap("r1", "", 1)}); got != nil {
		t.Fatalf("ChooseParent(empty uuids) = %+v, want nil", got)
	}
}

func TestChooseParentNewestCommon(t *testing.T) {
	local := []BtrfsSnapshot{replSnap("l1", "", 1), replSnap("l3", "", 3), replSnap("l2", "", 2), replSnap("l4", "", 4)}
	remote := []BtrfsSnapshot{replSnap("r1", "l1", 1), replSnap("r3", "l3", 3), replSnap("r2", "l2", 2)}
	got := ChooseParent(local, remote)
	if got == nil || got.UUID != "l3" {
		t.Fatalf("ChooseParent = %+v, want l3", got)
	}
	if local[1].UUID != "l3" {
		t.Fatalf("ChooseParent reordered its input: %+v", local)
	}
}

func TestPruneTargetKeepsNewest(t *testing.T) {
	remote := []BtrfsSnapshot{replSnap("r3", "", 3), replSnap("r1", "", 1), replSnap("r4", "", 4), replSnap("r2", "", 2)}
	var got []string
	for _, s := range PruneTarget(remote, 2) {
		got = append(got, s.UUID)
	}
	if want := []string{"r1", "r2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PruneTarget(2) = %v, want %v", got, want)
	}
	if got := PruneTarget(remote, 0); len(got) != 0 {
		t.Fatalf("PruneTarget(0) = %v, want nothing (unlimited)", got)
	}
	if got := PruneTarget(remote, 10); len(got) != 0 {
		t.Fatalf("PruneTarget(10) = %v, want nothing", got)
	}
	if remote[0].UUID != "r3" {
		t.Fatalf("PruneTarget reordered its input: %+v", remote)
	}
}

func validSSHJob() BtrfsReplicationJob {
	return BtrfsReplicationJob{
		UUID: rootUUID, Path: "@home", After: "timeline", KeepOnTarget: 5,
		Target: BtrfsReplicationTarget{Kind: "ssh", Host: "backup.lan", User: "root", Path: "/backups/home"},
	}
}

func TestValidateJobSSHRequiresAbsolutePath(t *testing.T) {
	ctx := context.Background()
	stubMounts(t, []Mount{
		{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256},
		{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5},
	})

	ok := validSSHJob()
	if err := ValidateJob(ctx, &ok); err != nil {
		t.Fatalf("valid ssh job rejected: %v", err)
	}
	if !jobIDPattern.MatchString(ok.ID) {
		t.Fatalf("generated ID %q is not a lowercase UUID", ok.ID)
	}
	if ok.Target.Port != 22 {
		t.Fatalf("port = %d, want default 22", ok.Target.Port)
	}
	kept := ok.ID
	if err := ValidateJob(ctx, &ok); err != nil || ok.ID != kept {
		t.Fatalf("revalidate: id %q -> %q, err %v", kept, ok.ID, err)
	}

	local := BtrfsReplicationJob{UUID: rootUUID, Path: "@home", After: "manual", Target: BtrfsReplicationTarget{Kind: "local", Path: "/data/backups"}}
	if err := ValidateJob(ctx, &local); err != nil {
		t.Fatalf("valid local job rejected: %v", err)
	}

	bad := map[string]func(j *BtrfsReplicationJob){
		"relative ssh path": func(j *BtrfsReplicationJob) { j.Target.Path = "backups/home" },
		"unclean ssh path":  func(j *BtrfsReplicationJob) { j.Target.Path = "/backups/../etc" },
		"empty ssh path":    func(j *BtrfsReplicationJob) { j.Target.Path = "" },
		"host with user":    func(j *BtrfsReplicationJob) { j.Target.Host = "root@backup" },
		"host with port":    func(j *BtrfsReplicationJob) { j.Target.Host = "backup:22" },
		"host with space":   func(j *BtrfsReplicationJob) { j.Target.Host = "backup lan" },
		"host option":       func(j *BtrfsReplicationJob) { j.Target.Host = "-oProxyCommand=x" },
		"empty host":        func(j *BtrfsReplicationJob) { j.Target.Host = "" },
		"user option":       func(j *BtrfsReplicationJob) { j.Target.User = "-oProxyCommand=x" },
		"empty user":        func(j *BtrfsReplicationJob) { j.Target.User = "" },
		"port too high":     func(j *BtrfsReplicationJob) { j.Target.Port = 65536 },
		"negative port":     func(j *BtrfsReplicationJob) { j.Target.Port = -1 },
		"bad kind":          func(j *BtrfsReplicationJob) { j.Target.Kind = "ftp" },
		"bad after":         func(j *BtrfsReplicationJob) { j.After = "daily" },
		"negative keep":     func(j *BtrfsReplicationJob) { j.KeepOnTarget = -1 },
		"missing uuid":      func(j *BtrfsReplicationJob) { j.UUID = "" },
		"absolute source":   func(j *BtrfsReplicationJob) { j.Path = "/home" },
		"bad id":            func(j *BtrfsReplicationJob) { j.ID = "../../etc" },
		"uppercase id":      func(j *BtrfsReplicationJob) { j.ID = "3F1C0A52-7D1E-4B8A-9C11-0E5A6B7D8F90" },
		"local same fs": func(j *BtrfsReplicationJob) {
			j.Target = BtrfsReplicationTarget{Kind: "local", Path: "/srv/backups"}
		},
		"local not mounted": func(j *BtrfsReplicationJob) {
			j.UUID = dataUUID
			j.Target = BtrfsReplicationTarget{Kind: "local", Path: "/datax/backups"}
			stubMounts(t, []Mount{{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5}})
		},
		"local relative": func(j *BtrfsReplicationJob) {
			j.Target = BtrfsReplicationTarget{Kind: "local", Path: "data/backups"}
		},
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			stubMounts(t, []Mount{
				{Mountpoint: "/", UUID: rootUUID, Subvol: "@", SubvolID: 256},
				{Mountpoint: "/data", UUID: dataUUID, SubvolID: 5},
			})
			j := validSSHJob()
			mutate(&j)
			err := ValidateJob(ctx, &j)
			if err == nil {
				t.Fatalf("%s accepted: %+v", name, j)
			}
			if name == "local same fs" && !strings.Contains(err.Error(), "target must be a different btrfs filesystem") {
				t.Fatalf("same fs error = %v", err)
			}
		})
	}
}

func TestSSHArgsStrict(t *testing.T) {
	old := ReplicationDir
	ReplicationDir = "/etc/linuxio/btrfs-replication"
	t.Cleanup(func() { ReplicationDir = old })

	job := validSSHJob()
	job.Target.Port = 2222
	want := []string{
		"-i", "/etc/linuxio/btrfs-replication/id_ed25519",
		"-o", "UserKnownHostsFile=/etc/linuxio/btrfs-replication/known_hosts",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=30",
		"-p", "2222",
		"root@backup.lan",
	}
	if got := SSHArgs(job); !reflect.DeepEqual(got, want) {
		t.Fatalf("SSHArgs =\n%q\nwant\n%q", got, want)
	}
	job.Target.Port = 0
	if got := SSHArgs(job); got[13] != "22" {
		t.Fatalf("default port = %q, want 22", got[13])
	}
}

func TestJobsRoundTrip(t *testing.T) {
	old := ReplicationPath
	ReplicationPath = filepath.Join(t.TempDir(), "btrfs-replication.json")
	t.Cleanup(func() { ReplicationPath = old })

	got, err := LoadJobs()
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("missing file: got %v, %v; want empty", got, err)
	}

	ran := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	msg := "boom"
	a := validSSHJob()
	a.ID, a.LastRun, a.LastError = "3f1c0a52-7d1e-4b8a-9c11-0e5a6b7d8f90", &ran, &msg
	b := BtrfsReplicationJob{ID: "9a2b4c6d-1e3f-4a5b-8c7d-9e0f1a2b3c4d", UUID: rootUUID, Path: "@", After: "manual", Target: BtrfsReplicationTarget{Kind: "local", Path: "/data/b"}}
	saveJobs(t, UpsertJob(UpsertJob(nil, a), b))
	got, err = LoadJobs()
	if err != nil || !reflect.DeepEqual(got, []BtrfsReplicationJob{a, b}) {
		t.Fatalf("LoadJobs = %+v, %v", got, err)
	}

	a.Enabled = true
	written, err := UpdateJobs(t.Context(), func(jobs *[]BtrfsReplicationJob) error {
		*jobs = DeleteJob(UpsertJob(*jobs, a), b.ID)
		return nil
	})
	if err != nil || len(written) != 1 || !written[0].Enabled {
		t.Fatalf("UpdateJobs = %+v, %v", written, err)
	}
	got, err = LoadJobs()
	if err != nil || !reflect.DeepEqual(got, written) {
		t.Fatalf("after update LoadJobs = %+v, %v; want %+v", got, err, written)
	}
}

func TestCheckTargetUnique(t *testing.T) {
	job := func(id string, enabled bool, target BtrfsReplicationTarget) BtrfsReplicationJob {
		return BtrfsReplicationJob{ID: id, Enabled: enabled, Target: target}
	}
	nas := BtrfsReplicationTarget{Kind: TargetSSH, Host: "nas", Port: 22, User: "root", Path: "/backups/home"}
	otherUser := nas
	otherUser.User = "backup"
	otherPath := nas
	otherPath.Path = "/backups/data"
	jobs := []BtrfsReplicationJob{job("a", true, nas), job("b", false, otherPath)}

	for name, tc := range map[string]struct {
		job     BtrfsReplicationJob
		wantErr bool
	}{
		"same target, other user": {job("c", true, otherUser), true},
		"same job updated":        {job("a", true, nas), false},
		"disabled duplicate":      {job("c", false, nas), true},
		"target of disabled job":  {job("c", true, otherPath), true},
		"local same path":         {job("c", true, BtrfsReplicationTarget{Kind: TargetLocal, Path: "/backups/home"}), false},
	} {
		err := CheckTargetUnique(jobs, tc.job)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", name, err, tc.wantErr)
		}
		if err != nil && err.Error() != "another job already replicates to this target" {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// saveJobs replaces the job file with jobs.
func saveJobs(t *testing.T, jobs []BtrfsReplicationJob) {
	t.Helper()
	if _, err := UpdateJobs(t.Context(), func(j *[]BtrfsReplicationJob) error { *j = jobs; return nil }); err != nil {
		t.Fatal(err)
	}
}
