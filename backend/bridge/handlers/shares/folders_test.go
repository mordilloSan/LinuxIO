package shares

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const testPool = "uuid-tank"

// fakePool stands in for the pool: it keeps what was made on it and the
// order of the calls, and fails the call named in fail.
type fakePool struct {
	top   string
	calls []string
	fail  map[string]error
	rules map[string]btrfs.BtrfsSnapshotRule
	acls  map[string]btrfs.FolderACL
	// snapshotACLs is the ACL of each folder's snapshots, absent when
	// they are root's alone.
	snapshotACLs map[string]btrfs.FolderACL
	limits       map[string]uint64
	snapshots    map[string][]string
	jobs         map[string]bool
	// onCreate runs once the subvolume is made.
	onCreate func()
	// timer is what was asked of systemd about the recycle timer.
	timer []string
	// restarts counts the restarts of smbd.
	restarts int
}

func (p *fakePool) call(name, path string) error {
	p.calls = append(p.calls, name+" "+path)
	return p.fail[name]
}

func (p *fakePool) ops() folderStorage {
	return folderStorage{
		mounts: func(context.Context) ([]btrfs.Mount, error) {
			return []btrfs.Mount{
				{UUID: "uuid-other", SubvolID: 5, Mountpoint: "/mnt/other"},
				{UUID: testPool, SubvolID: 256, Subvol: "@", Mountpoint: "/"},
				{UUID: testPool, SubvolID: 5, Mountpoint: p.top},
				{UUID: "uuid-transient", SubvolID: 5, Mountpoint: btrfs.TopLevelDir + "/uuid-transient"},
			}, nil
		},
		createSubvolume: func(_ context.Context, _, path string) error {
			if err := p.call("createSubvolume", path); err != nil {
				return err
			}
			if p.onCreate != nil {
				p.onCreate()
			}
			return os.Mkdir(filepath.Join(p.top, path), 0o755)
		},
		deleteSubvolume: func(_ context.Context, _, path string) error {
			if err := p.call("deleteSubvolume", path); err != nil {
				return err
			}
			return os.Remove(filepath.Join(p.top, path))
		},
		setLimit: func(_ context.Context, _, path string, max uint64) error {
			p.limits[path] = max
			return p.call("setLimit", path)
		},
		saveRule: func(_ context.Context, rule btrfs.BtrfsSnapshotRule) error {
			if err := p.call("saveRule", rule.Path); err != nil {
				return err
			}
			p.rules[rule.Path] = rule
			return nil
		},
		deleteRule: func(_ context.Context, _, path string) error {
			delete(p.rules, path)
			return p.call("deleteRule", path)
		},
		replicated: func(_, path string) (bool, error) { return p.jobs[path], nil },
		ruleExists: func(_, path string) (bool, error) {
			_, ok := p.rules[path]
			return ok, nil
		},
		setACL: func(_ context.Context, _, path string, acl btrfs.FolderACL) error {
			if err := p.call("setACL", path); err != nil {
				return err
			}
			p.acls[path] = acl
			return nil
		},
		setSnapshotsACL: func(_ context.Context, _, path string, acl *btrfs.FolderACL) error {
			if acl == nil {
				delete(p.snapshotACLs, path)
				return p.call("clearSnapshotsACL", path)
			}
			if err := p.call("setSnapshotsACL", path); err != nil {
				return err
			}
			p.snapshotACLs[path] = *acl
			return nil
		},
		makeFolder: func(_ context.Context, _, path string, _ uint32) error {
			if err := p.call("makeFolder", path); err != nil {
				return err
			}
			return os.MkdirAll(filepath.Join(p.top, path), 0o755)
		},
		listSnapshots: func(_ context.Context, _, path string) ([]string, error) {
			return p.snapshots[path], p.call("listSnapshots", path)
		},
		deleteSnapshot: func(_ context.Context, _, snapshotPath string) error {
			return p.call("deleteSnapshot", snapshotPath)
		},
		dropReplication: func(_ context.Context, _, path string) error {
			delete(p.jobs, path)
			return p.call("dropReplication", path)
		},
	}
}

// setupFolders points every file and tool of the shares package at a temp
// dir and a fake pool whose top level is mounted at <dir>/tank.
func setupFolders(t *testing.T) *fakePool {
	t.Helper()
	dir := t.TempDir()
	pool := &fakePool{
		top:          filepath.Join(dir, "tank"),
		fail:         map[string]error{},
		rules:        map[string]btrfs.BtrfsSnapshotRule{},
		acls:         map[string]btrfs.FolderACL{},
		snapshotACLs: map[string]btrfs.FolderACL{},
		limits:       map[string]uint64{},
		snapshots:    map[string][]string{},
		jobs:         map[string]bool{},
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if err := os.Mkdir(pool.top, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := struct {
		smb, exports, folders, lock, foldersLock, passwd, group string
		rootOnly                                                func(string) error
		ops                                                     folderStorage
		samba, nfs                                              func() (bool, error)
		reload                                                  func(context.Context) ([]byte, error)
		exportfs                                                func(context.Context) error
	}{
		smbConfFile, exportsFile, sharedFoldersFile, confLockFile, foldersLockFile, passwdPath, groupPath,
		topIsRootOnly,
		folderOps, sambaServerAvailable, nfsServerAvailable, smbcontrolReload, applyNFSExports,
	}
	t.Cleanup(func() {
		smbConfFile, exportsFile, sharedFoldersFile, confLockFile = orig.smb, orig.exports, orig.folders, orig.lock
		passwdPath, groupPath, folderOps, foldersLockFile = orig.passwd, orig.group, orig.ops, orig.foldersLock
		topIsRootOnly = orig.rootOnly
		sambaServerAvailable, nfsServerAvailable = orig.samba, orig.nfs
		smbcontrolReload, applyNFSExports = orig.reload, orig.exportfs
	})
	smbConfFile = write("smb.conf", "[global]\n   workgroup = WORKGROUP\n\n[plain]\n   path = /srv/plain\n")
	exportsFile = write("exports", "/srv/plain 10.0.0.0/24(rw)\n")
	passwdPath = write("passwd", "root:x:0:0::/root:/bin/bash\nalice:x:1000:1000::/home/alice:/bin/bash\nbob:x:1001:1001::/home/bob:/bin/bash\ncarol:x:1002:2000::/home/carol:/bin/bash\n")
	groupPath = write("group", "root:x:0:\nalice:x:1000:\nbob:x:1001:\nfamily:x:2000:alice,bob\nkids:x:2001:bob\n")
	sharedFoldersFile = filepath.Join(dir, "shared-folders.json")
	confLockFile = filepath.Join(dir, "shares.lock")
	foldersLockFile = filepath.Join(dir, "shared-folders.lock")
	// The temp dir is the test user's, not root's.
	topIsRootOnly = func(string) error { return nil }
	origTimer, origTimerPath, origRestart := recycleTimer, recycleTimerPath, restartSamba
	t.Cleanup(func() { recycleTimer, recycleTimerPath, restartSamba = origTimer, origTimerPath, origRestart })
	restartSamba = func(context.Context) error { pool.restarts++; return nil }
	recycleTimerPath = filepath.Join(dir, "linuxio-shares-recycle.timer")
	say := func(what string) func(context.Context, string) error {
		return func(_ context.Context, unit string) error {
			pool.timer = append(pool.timer, what+" "+unit)
			return nil
		}
	}
	recycleTimer = recycleTimerOps{
		daemonReload: func(context.Context) error { pool.timer = append(pool.timer, "reload"); return nil },
		enableUnit:   say("enable"), startUnit: say("start"), stopUnit: say("stop"), disableUnit: say("disable"),
	}
	folderOps = pool.ops()
	sambaServerAvailable = func() (bool, error) { return true, nil }
	nfsServerAvailable = func() (bool, error) { return true, nil }
	smbcontrolReload = func(context.Context) ([]byte, error) { return nil, nil }
	applyNFSExports = func(context.Context) error { return nil }
	return pool
}

func mediaRequest() apischema.SharedFolderCreateRequest {
	limit := uint64(1 << 30)
	return apischema.SharedFolderCreateRequest{
		UUID:               testPool,
		Name:               "Media",
		MaxReferencedBytes: &limit,
		SnapshotRule: &btrfs.BtrfsSnapshotRule{
			Enabled: true,
			Every:   btrfs.EveryHourly,
			Keep:    btrfs.BtrfsKeep{Hourly: 24},
		},
		Access:   []apischema.SharedFolderAccess{row("user", "alice", "rw"), row("group", "family", "ro")},
		Everyone: "none",
		SMB:      true,
		NFS:      []apischema.NFSClient{{Host: "10.0.0.0/24", Options: []string{"rw"}}},
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

func TestCreateSharedFolder(t *testing.T) {
	pool := setupFolders(t)
	folder, err := CreateSharedFolder(context.Background(), mediaRequest())
	if err != nil {
		t.Fatalf("CreateSharedFolder() = %v", err)
	}
	media := filepath.Join(pool.top, "Media")
	if folder.Folder != media || folder.Path != "Media" || folder.Name != "Media" {
		t.Errorf("folder = %+v", folder)
	}
	wantCalls := []string{"listSnapshots Media", "createSubvolume Media", "setLimit Media", "saveRule Media", "setACL Media", "clearSnapshotsACL Media"}
	if !slices.Equal(pool.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", pool.calls, wantCalls)
	}
	if pool.limits["Media"] != 1<<30 {
		t.Errorf("limit = %d", pool.limits["Media"])
	}
	if rule := pool.rules["Media"]; rule.UUID != testPool || rule.Path != "Media" || rule.Every != btrfs.EveryHourly {
		t.Errorf("rule = %+v", rule)
	}
	// alice by her own row; the family group by its entry; nobody else.
	acl := pool.acls["Media"]
	if acl.Users[1000] != btrfs.ACLReadWrite || acl.Groups[2000] != btrfs.ACLRead || acl.Other != btrfs.ACLNone {
		t.Errorf("acl = %+v", acl)
	}
	smb := readFile(t, smbConfFile)
	for _, line := range []string{
		"[Media]", "path = " + media, "valid users = alice, +family", "write list = alice",
		"read only = yes", "inherit acls = yes", "create mask = 0666", "directory mask = 0777",
	} {
		if !strings.Contains(smb, line) {
			t.Errorf("smb.conf lacks %q:\n%s", line, smb)
		}
	}
	if exports := readFile(t, exportsFile); !strings.Contains(exports, media+"\t10.0.0.0/24(rw)") {
		t.Errorf("exports = %q", exports)
	}
	info, err := os.Stat(sharedFoldersFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record mode = %o, want 600", info.Mode().Perm())
	}
	listed, err := ListSharedFolders(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Folder != media || !listed[0].SMB || len(listed[0].NFS) != 1 {
		t.Errorf("ListSharedFolders() = %+v, %v", listed, err)
	}
	// Each row keeps the ID of the account it was given to.
	wantRows := []apischema.SharedFolderAccess{
		{Kind: "user", Name: "alice", Level: "rw", ID: 1000},
		{Kind: "group", Name: "family", Level: "ro", ID: 2000},
	}
	if !slices.Equal(listed[0].Access, wantRows) {
		t.Errorf("rows = %+v, want %+v", listed[0].Access, wantRows)
	}
}

// assertNothingLeft checks that a create that failed left nothing of the
// folder, and took back only what it made.
func assertNothingLeft(t *testing.T, pool *fakePool, smbBefore, exportsBefore string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(pool.top, "Media")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folder is still there: %v", err)
	}
	if len(pool.rules) != 0 {
		t.Errorf("the snapshot rule is still there: %v", pool.rules)
	}
	if got := readFile(t, smbConfFile); got != smbBefore {
		t.Errorf("smb.conf changed:\n%s", got)
	}
	if got := readFile(t, exportsFile); got != exportsBefore {
		t.Errorf("exports changed:\n%s", got)
	}
	if info, err := os.Stat(sharedFoldersFile); err == nil && info.IsDir() {
		// The record could not be written at all.
	} else if folders, err := loadFolders(); err != nil || len(folders) != 0 {
		t.Errorf("the record is still there: %+v, %v", folders, err)
	}
	// Only what this create made is taken back.
	made := slices.Contains(pool.calls, "createSubvolume Media") && pool.fail["createSubvolume"] == nil
	if undone := slices.Contains(pool.calls, "deleteSubvolume Media"); undone != made {
		t.Errorf("folder made = %t, deleted = %t: %v", made, undone, pool.calls)
	}
}

// A step that fails leaves nothing of the folder behind: what the steps
// before it made is undone, newest first.
func TestCreateSharedFolderUndoesWhatItMade(t *testing.T) {
	boom := errors.New("boom")
	steps := []struct {
		name string
		fail func(*fakePool)
	}{
		{"the folder itself", func(p *fakePool) { p.fail["createSubvolume"] = boom }},
		// A folder where the record goes cannot be read or replaced.
		{"the record", func(p *fakePool) {
			// In the test's temp dir, removed with it.
			p.onCreate = func() { _ = os.Mkdir(sharedFoldersFile, 0o700) }
		}},
		{"the limit", func(p *fakePool) { p.fail["setLimit"] = boom }},
		{"the snapshot rule", func(p *fakePool) { p.fail["saveRule"] = boom }},
		{"the access", func(p *fakePool) { p.fail["setACL"] = boom }},
		{"the SMB section", func(*fakePool) {
			smbcontrolReload = func(context.Context) ([]byte, error) { return nil, boom }
			systemdReloadUnit = func(context.Context, string) error { return boom }
		}},
		{"the NFS export", func(*fakePool) { applyNFSExports = func(context.Context) error { return boom } }},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			origUnit := systemdReloadUnit
			t.Cleanup(func() { systemdReloadUnit = origUnit })
			pool := setupFolders(t)
			smbBefore, exportsBefore := readFile(t, smbConfFile), readFile(t, exportsFile)
			step.fail(pool)
			if _, err := CreateSharedFolder(context.Background(), mediaRequest()); err == nil {
				t.Fatal("CreateSharedFolder() = nil, want the step's error")
			}
			assertNothingLeft(t, pool, smbBefore, exportsBefore)
		})
	}
}

// What an earlier folder of the same name left on the pool is not taken
// over: its snapshots would be listed, pruned and deleted as the new
// folder's, and its export would be removed by an undo.
func TestCreateSharedFolderRefusesLeftoversOfAnEarlierFolder(t *testing.T) {
	t.Run("snapshots", func(t *testing.T) {
		pool := setupFolders(t)
		pool.snapshots["Media"] = []string{"linuxio-snapshots/Media/old"}
		if _, err := CreateSharedFolder(context.Background(), mediaRequest()); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Fatalf("CreateSharedFolder() = %v, want ErrConflict", err)
		}
		if slices.Contains(pool.calls, "createSubvolume Media") {
			t.Errorf("the folder was made: %v", pool.calls)
		}
	})
	t.Run("a snapshot schedule", func(t *testing.T) {
		pool := setupFolders(t)
		pool.rules["Media"] = btrfs.BtrfsSnapshotRule{UUID: testPool, Path: "Media"}
		req := mediaRequest()
		req.SnapshotRule = nil
		if _, err := CreateSharedFolder(context.Background(), req); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Fatalf("CreateSharedFolder() = %v, want ErrConflict", err)
		}
		if _, ok := pool.rules["Media"]; !ok {
			t.Error("the schedule that was there is gone")
		}
	})
	t.Run("a replication job", func(t *testing.T) {
		pool := setupFolders(t)
		pool.jobs["Media"] = true
		if _, err := CreateSharedFolder(context.Background(), mediaRequest()); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Fatalf("CreateSharedFolder() = %v, want ErrConflict", err)
		}
	})
	t.Run("an SMB share", func(t *testing.T) {
		pool := setupFolders(t)
		section := "\n[old]\n   path = " + filepath.Join(pool.top, "Media") + "/\n"
		if err := os.WriteFile(smbConfFile, []byte(readFile(t, smbConfFile)+section), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := CreateSharedFolder(context.Background(), mediaRequest()); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Fatalf("CreateSharedFolder() = %v, want ErrConflict", err)
		}
	})
	t.Run("an export", func(t *testing.T) {
		pool := setupFolders(t)
		line := filepath.Join(pool.top, "Media") + "\t*(ro)\n"
		if err := os.WriteFile(exportsFile, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := CreateSharedFolder(context.Background(), mediaRequest()); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Fatalf("CreateSharedFolder() = %v, want ErrConflict", err)
		}
		if got := readFile(t, exportsFile); got != line {
			t.Errorf("exports = %q", got)
		}
	})
}

// A pool whose top folder a user may write is no place for a shared
// folder: that user could put another folder under its name, to be given to
// root, opened to others or deleted in its place.
func TestSharedFoldersNeedAPoolTopThatIsRootOnly(t *testing.T) {
	pool, folder := createdMedia(t)
	topIsRootOnly = func(top string) error {
		return fmt.Errorf("%w: %s is not root's alone", bridgeipc.ErrConflict, top)
	}
	ctx := context.Background()
	req := mediaRequest()
	req.Name = "Other"
	if _, err := CreateSharedFolder(ctx, req); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("create = %v, want ErrConflict", err)
	}
	if _, err := UpdateSharedFolder(ctx, updateOf(folder)); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("update = %v, want ErrConflict", err)
	}
	err := DeleteSharedFolder(ctx, apischema.SharedFolderDeleteRequest{UUID: folder.UUID, Path: folder.Path, ConfirmName: folder.Name})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("delete = %v, want ErrConflict", err)
	}
	if err := ReapplyAccess(ctx); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("reapply = %v, want ErrConflict", err)
	}
	if len(pool.calls) != 0 {
		t.Errorf("the pool was touched: %v", pool.calls)
	}
}

func TestTopIsRootOnly(t *testing.T) {
	dir := t.TempDir()
	if os.Getuid() != 0 {
		if err := topIsRootOnly(dir); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Errorf("a folder of the test user = %v, want ErrConflict", err)
		}
	}
	// /usr is root's and nobody else writes it; /tmp is everyone's.
	if err := topIsRootOnly("/usr"); err != nil {
		t.Errorf("/usr = %v", err)
	}
	if err := topIsRootOnly("/tmp"); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("/tmp = %v, want ErrConflict", err)
	}
}

func TestCreateSharedFolderRefuses(t *testing.T) {
	tests := []struct {
		name   string
		change func(*apischema.SharedFolderCreateRequest)
		want   error
	}{
		{"a name that is not one folder", func(r *apischema.SharedFolderCreateRequest) { r.Name = "a/b" }, bridgeipc.ErrInvalidArgs},
		{"a name with a space", func(r *apischema.SharedFolderCreateRequest) { r.Name = "my media" }, bridgeipc.ErrInvalidArgs},
		{"the snapshots folder", func(r *apischema.SharedFolderCreateRequest) { r.Name = btrfs.SnapshotRoot }, bridgeipc.ErrInvalidArgs},
		{"a reserved section", func(r *apischema.SharedFolderCreateRequest) { r.Name = "homes" }, bridgeipc.ErrInvalidArgs},
		{"a share name in use", func(r *apischema.SharedFolderCreateRequest) { r.Name = "PLAIN" }, bridgeipc.ErrConflict},
		{"a pool with no mount of its top level", func(r *apischema.SharedFolderCreateRequest) { r.UUID = "uuid-transient" }, bridgeipc.ErrConflict},
		{"an unknown user", func(r *apischema.SharedFolderCreateRequest) {
			r.Access = []apischema.SharedFolderAccess{row("user", "dave", "rw")}
		}, bridgeipc.ErrInvalidArgs},
		{"SMB with nobody let in", func(r *apischema.SharedFolderCreateRequest) { r.Access = nil }, bridgeipc.ErrInvalidArgs},
		{"a bad NFS client", func(r *apischema.SharedFolderCreateRequest) { r.NFS[0].Host = "a b" }, bridgeipc.ErrInvalidArgs},
		{"a bad snapshot rule", func(r *apischema.SharedFolderCreateRequest) { r.SnapshotRule.Every = "yearly" }, bridgeipc.ErrInvalidArgs},
		{"a limit of zero", func(r *apischema.SharedFolderCreateRequest) { zero := uint64(0); r.MaxReferencedBytes = &zero }, bridgeipc.ErrInvalidArgs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := setupFolders(t)
			req := mediaRequest()
			tt.change(&req)
			_, err := CreateSharedFolder(context.Background(), req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("CreateSharedFolder() = %v, want %v", err, tt.want)
			}
			if len(pool.calls) != 0 {
				t.Errorf("the pool was touched before the refusal: %v", pool.calls)
			}
		})
	}
}

func TestCreateSharedFolderRefusesAFolderAlreadyThere(t *testing.T) {
	pool := setupFolders(t)
	if _, err := CreateSharedFolder(context.Background(), mediaRequest()); err != nil {
		t.Fatal(err)
	}
	pool.calls = nil
	req := mediaRequest()
	req.SMB = false
	req.NFS = nil
	_, err := CreateSharedFolder(context.Background(), req)
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("a second folder of the same name = %v, want ErrConflict", err)
	}
	if len(pool.calls) != 0 {
		t.Errorf("the pool was touched: %v", pool.calls)
	}
}

func updateOf(f apischema.SharedFolder) apischema.SharedFolderUpdateRequest {
	return apischema.SharedFolderUpdateRequest{
		UUID: f.UUID, Path: f.Path, Name: f.Name, Access: f.Access, Everyone: f.Everyone, SMB: f.SMB, NFS: f.NFS,
		PreviousVersions: f.PreviousVersions, RecycleBin: f.RecycleBin,
		TimeMachine: f.TimeMachine, TimeMachineMaxBytes: f.TimeMachineMaxBytes,
	}
}

// Earlier versions: Samba lists the folder's snapshots by the time in
// their names, keeps the names it listed to open them (snapprefix and an
// empty delimiter, without which it rebuilds a name without its kind), and
// reads them as the person signed in, so the folder of the snapshots lets
// read who may read the folder.
func TestSharedFolderEarlierVersions(t *testing.T) {
	pool := setupFolders(t)
	req := mediaRequest()
	req.PreviousVersions = true
	folder, err := CreateSharedFolder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateSharedFolder() = %v", err)
	}
	if !slices.Contains(pool.calls, "setSnapshotsACL Media") {
		t.Errorf("calls = %v", pool.calls)
	}
	// alice may write the folder and family read it: both may read its
	// snapshots; nobody else.
	want := btrfs.FolderACL{
		Users:  map[int]btrfs.ACLPerm{1000: btrfs.ACLRead},
		Groups: map[int]btrfs.ACLPerm{2000: btrfs.ACLRead},
		Other:  btrfs.ACLNone,
	}
	if got := pool.snapshotACLs["Media"]; !reflect.DeepEqual(got, want) {
		t.Errorf("snapshots ACL = %+v, want %+v", got, want)
	}
	smb := readFile(t, smbConfFile)
	for _, line := range []string{
		"vfs objects = shadow_copy2",
		"shadow:snapdir = " + filepath.Join(pool.top, btrfs.SnapshotRoot, "Media"),
		"shadow:format = %Y%m%dT%H%M%SZ",
		"shadow:snapprefix = ^$",
		"shadow:delimiter = \n",
		"shadow:localtime = no",
		"shadow:sort = desc",
	} {
		if !strings.Contains(smb, line) {
			t.Errorf("smb.conf lacks %q:\n%s", line, smb)
		}
	}

	// The access changes: so does who may read the snapshots.
	pool.calls = nil
	req2 := updateOf(folder)
	req2.Everyone = "ro"
	if _, err = UpdateSharedFolder(context.Background(), req2); err != nil {
		t.Fatal(err)
	}
	if got := pool.snapshotACLs["Media"].Other; got != btrfs.ACLRead {
		t.Errorf("snapshots ACL for everyone else = %v, want read", got)
	}

	// Not shared with Windows and Macs, nobody is offered them: the
	// snapshots are root's alone until it is shared again.
	req2.SMB = false
	if _, err = UpdateSharedFolder(context.Background(), req2); err != nil {
		t.Fatal(err)
	}
	if _, open := pool.snapshotACLs["Media"]; open {
		t.Errorf("snapshots open while the folder is not shared over SMB")
	}
	req2.SMB = true
	if _, err = UpdateSharedFolder(context.Background(), req2); err != nil {
		t.Fatal(err)
	}
	if _, open := pool.snapshotACLs["Media"]; !open {
		t.Errorf("snapshots closed after sharing again")
	}

	// Off: the snapshots are root's alone again, and the share says nothing
	// of them.
	pool.calls = nil
	req2.PreviousVersions = false
	if _, err = UpdateSharedFolder(context.Background(), req2); err != nil {
		t.Fatal(err)
	}
	if _, open := pool.snapshotACLs["Media"]; open || !slices.Contains(pool.calls, "clearSnapshotsACL Media") {
		t.Errorf("snapshots still open: %v, calls %v", pool.snapshotACLs, pool.calls)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "shadow:") || strings.Contains(smb, "vfs objects") {
		t.Errorf("smb.conf:\n%s", smb)
	}
}

// The recycle bin: Samba moves what is deleted over SMB into #recycle at the
// top of the folder, which LinuxIO makes there once the folder has its
// access, so the bin carries it (in a bin it cannot write, Samba deletes for
// good); a timer has linuxio-worker empty the bins while a folder has one.
func TestSharedFolderRecycleBin(t *testing.T) {
	pool := setupFolders(t)
	req := mediaRequest()
	req.RecycleBin, req.PreviousVersions = true, true
	folder, err := CreateSharedFolder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateSharedFolder() = %v", err)
	}
	acl, bin := slices.Index(pool.calls, "setACL Media"), slices.Index(pool.calls, "makeFolder Media/#recycle")
	if acl < 0 || bin < acl {
		t.Errorf("the bin is made after the folder's access: %v", pool.calls)
	}
	smb := readFile(t, smbConfFile)
	for _, line := range []string{
		"vfs objects = shadow_copy2 recycle",
		"recycle:repository = #recycle",
		"recycle:keeptree = yes",
		"recycle:versions = yes",
		"recycle:exclude = ~$*,*.tmp,*.temp",
		// Samba makes the folders of the bin as the person deleting, 0700
		// by default, which the next person could not move a file into:
		// 0777 leaves it to the folder's ACL.
		"recycle:directory_mode = 0777",
		"recycle:subdir_mode = 0777",
	} {
		if !strings.Contains(smb, line) {
			t.Errorf("smb.conf lacks %q:\n%s", line, smb)
		}
	}
	if want := []string{"reload", "enable linuxio-shares-recycle.timer", "start linuxio-shares-recycle.timer"}; !slices.Equal(pool.timer, want) {
		t.Errorf("timer = %v, want %v", pool.timer, want)
	}
	// The bin is root's and has the folder's own access, whoever made a
	// folder of that name before, and keeps it through a change: someone
	// given access later can use the bin, someone who lost it cannot.
	if !reflect.DeepEqual(pool.acls["Media/#recycle"], pool.acls["Media"]) || len(pool.acls["Media/#recycle"].Users) == 0 {
		t.Errorf("the bin's access = %+v, the folder's %+v", pool.acls["Media/#recycle"], pool.acls["Media"])
	}
	wider := updateOf(folder)
	wider.Access = append(slices.Clone(wider.Access), row("user", "bob", "rw"))
	if folder, err = UpdateSharedFolder(context.Background(), wider); err != nil {
		t.Fatal(err)
	}
	if pool.acls["Media/#recycle"].Users[1001] != btrfs.ACLReadWrite {
		t.Errorf("the bin's access after bob was added = %+v", pool.acls["Media/#recycle"])
	}
	if text := readFile(t, recycleTimerPath); !strings.Contains(text, "OnCalendar=daily") || !strings.Contains(text, "Unit=linuxio-shares-recycle.service") {
		t.Errorf("timer unit:\n%s", text)
	}

	// Off: Samba deletes for good again, the bin and what is in it stay,
	// and with no bin left to empty the timer stops.
	pool.calls, pool.timer = nil, nil
	off := updateOf(folder)
	off.RecycleBin = false
	if _, err = UpdateSharedFolder(context.Background(), off); err != nil {
		t.Fatal(err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "recycle") {
		t.Errorf("smb.conf:\n%s", smb)
	}
	if _, err = os.Stat(filepath.Join(pool.top, "Media", "#recycle")); err != nil {
		t.Errorf("the bin went with the switch: %v", err)
	}
	if want := []string{"stop linuxio-shares-recycle.timer", "disable linuxio-shares-recycle.timer"}; !slices.Equal(pool.timer, want) {
		t.Errorf("timer = %v, want %v", pool.timer, want)
	}
}

// Whoever may write a folder can put a link or a file where its recycle bin
// was. A save goes through all the same, to the record and the section: one
// that stopped at the bin would leave both behind the access already
// written, for as long as that person liked.
func TestSharedFolderSavesPastABinThatIsNotAFolder(t *testing.T) {
	pool := setupFolders(t)
	req := mediaRequest()
	req.RecycleBin = true
	folder, err := CreateSharedFolder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateSharedFolder() = %v", err)
	}
	for name, binErr := range map[string]error{
		"a link": fmt.Errorf("%w: Media/#recycle", btrfs.ErrLinkOnPath),
		"a file": &fs.PathError{Op: "open", Path: "Media/#recycle", Err: unix.ENOTDIR},
	} {
		pool.fail["makeFolder"] = binErr
		pool.calls = nil
		renamed := updateOf(folder)
		renamed.Name = "Films" + strings.ReplaceAll(name, " ", "")
		got, updateErr := UpdateSharedFolder(context.Background(), renamed)
		if updateErr != nil {
			t.Fatalf("%s: UpdateSharedFolder() = %v", name, updateErr)
		}
		if got.Name != renamed.Name || !strings.Contains(readFile(t, smbConfFile), "["+renamed.Name+"]") {
			t.Errorf("%s: saved as %q, smb.conf:\n%s", name, got.Name, readFile(t, smbConfFile))
		}
		if slices.Contains(pool.calls, "setACL Media/#recycle") {
			t.Errorf("%s: an ACL was set through the bin's name: %v", name, pool.calls)
		}
		folder = got
	}
	// Any other failure of the bin still stops the save.
	pool.fail["makeFolder"] = errors.New("input/output error")
	if _, err = UpdateSharedFolder(context.Background(), updateOf(folder)); err == nil {
		t.Error("a bin that could not be made did not fail the save")
	}
}

// Time Machine: the folder is offered to Macs as a backup destination, with
// the size its backups may take. The Mac modules come first and in their
// own order, as Samba asks.
func TestSharedFolderTimeMachine(t *testing.T) {
	setupFolders(t)
	req := mediaRequest()
	size := uint64(500) << 30
	req.TimeMachine, req.TimeMachineMaxBytes = true, &size
	req.PreviousVersions, req.RecycleBin = true, true
	folder, err := CreateSharedFolder(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateSharedFolder() = %v", err)
	}
	smb := readFile(t, smbConfFile)
	for _, line := range []string{
		"vfs objects = catia fruit streams_xattr shadow_copy2 recycle",
		"fruit:time machine = yes",
		"fruit:time machine max size = 512000M",
	} {
		if !strings.Contains(smb, line) {
			t.Errorf("smb.conf lacks %q:\n%s", line, smb)
		}
	}
	// No limit: Samba is told none.
	noLimit := updateOf(folder)
	noLimit.TimeMachineMaxBytes = nil
	if _, err = UpdateSharedFolder(context.Background(), noLimit); err != nil {
		t.Fatal(err)
	}
	if smb := readFile(t, smbConfFile); !strings.Contains(smb, "fruit:time machine = yes") || strings.Contains(smb, "max size") {
		t.Errorf("smb.conf:\n%s", smb)
	}
	zero := uint64(0)
	noLimit.TimeMachineMaxBytes = &zero
	if _, err = UpdateSharedFolder(context.Background(), noLimit); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("a size of zero = %v, want ErrInvalidArgs", err)
	}
	// Off: no Mac module is loaded for the folder.
	noLimit.TimeMachine, noLimit.TimeMachineMaxBytes = false, nil
	if _, err = UpdateSharedFolder(context.Background(), noLimit); err != nil {
		t.Fatal(err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "fruit") || !strings.Contains(smb, "vfs objects = shadow_copy2 recycle") {
		t.Errorf("smb.conf:\n%s", smb)
	}
}

// smbd tells Macs of a Time Machine folder when it starts, and only then: a
// reload of its settings leaves what it announced as it was. It is
// restarted when that changes, and only then, since a restart cuts every
// open connection.
func TestTimeMachineAnnouncementRestartsSamba(t *testing.T) {
	pool := setupFolders(t)
	ctx := context.Background()
	req := mediaRequest()
	folder, err := CreateSharedFolder(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	step := func(what string, want int, change func(*apischema.SharedFolderUpdateRequest)) {
		t.Helper()
		update := updateOf(folder)
		change(&update)
		pool.restarts = 0
		if folder, err = UpdateSharedFolder(ctx, update); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if pool.restarts != want {
			t.Errorf("%s: %d restarts of smbd, want %d", what, pool.restarts, want)
		}
	}
	if pool.restarts != 0 {
		t.Errorf("a folder without Time Machine restarted smbd %d times", pool.restarts)
	}
	step("access changed", 0, func(u *apischema.SharedFolderUpdateRequest) { u.Everyone = "ro" })
	step("Time Machine on", 1, func(u *apischema.SharedFolderUpdateRequest) { u.TimeMachine = true })
	step("its size changed", 0, func(u *apischema.SharedFolderUpdateRequest) { size := uint64(1) << 40; u.TimeMachineMaxBytes = &size })
	step("renamed", 1, func(u *apischema.SharedFolderUpdateRequest) { u.Name = "Backups" })
	step("no longer shared", 1, func(u *apischema.SharedFolderUpdateRequest) { u.SMB = false })
	step("shared again", 1, func(u *apischema.SharedFolderUpdateRequest) { u.SMB = true })
	pool.restarts = 0
	if err = DeleteSharedFolder(ctx, apischema.SharedFolderDeleteRequest{UUID: folder.UUID, Path: folder.Path, ConfirmName: folder.Name}); err != nil {
		t.Fatal(err)
	}
	if pool.restarts != 1 {
		t.Errorf("deleted: %d restarts of smbd, want 1", pool.restarts)
	}
	// A new folder that is one from the start.
	pool.restarts = 0
	req.TimeMachine = true
	if _, err = CreateSharedFolder(ctx, req); err != nil {
		t.Fatal(err)
	}
	if pool.restarts != 1 {
		t.Errorf("created with Time Machine: %d restarts of smbd, want 1", pool.restarts)
	}
}

// A host with no recycle bin is not asked anything about the timer.
func TestSharedFolderWithoutARecycleBinLeavesTheTimerAlone(t *testing.T) {
	pool := setupFolders(t)
	if _, err := CreateSharedFolder(context.Background(), mediaRequest()); err != nil {
		t.Fatal(err)
	}
	if len(pool.timer) != 0 {
		t.Errorf("timer = %v", pool.timer)
	}
	if _, err := os.Stat(recycleTimerPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the timer unit was written: %v", err)
	}
}

// The timer is a file LinuxIO writes for its own work: both packages stop
// and disable it at removal, and delete it.
func TestPackageRemovalDeletesTheRecycleTimer(t *testing.T) {
	read := func(script string) string {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", script))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, script := range []string{"debian/postrm", "rpm/linuxio.spec"} {
		if !slices.Contains(strings.Fields(read(script)), recycleTimerPath) {
			t.Errorf("packaging/%s does not delete %s", script, recycleTimerPath)
		}
	}
	for _, script := range []string{"debian/prerm", "rpm/linuxio.spec"} {
		if !strings.Contains(read(script), recycleTimerName) {
			t.Errorf("packaging/%s does not stop and disable %s", script, recycleTimerName)
		}
	}
}

// A folder that comes off the network to be deleted has its snapshots
// closed at once, before its data is touched: when the delete then fails,
// they are not left open under a record that says it is not shared.
func TestDeleteSharedFolderClosesItsSnapshotsBeforeItsData(t *testing.T) {
	pool := setupFolders(t)
	req := mediaRequest()
	req.PreviousVersions = true
	if _, err := CreateSharedFolder(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	pool.fail["deleteSubvolume"] = errors.New("busy")
	err := DeleteSharedFolder(context.Background(), apischema.SharedFolderDeleteRequest{UUID: testPool, Path: "Media", ConfirmName: "Media"})
	if err == nil {
		t.Fatal("DeleteSharedFolder() = nil, want the subvolume's error")
	}
	if _, open := pool.snapshotACLs["Media"]; open {
		t.Errorf("the snapshots are still open after a delete that failed")
	}
}

// A deleted folder leaves its snapshots' folder root's alone, so the next
// folder of that name does not start with the old table's readers.
func TestDeleteSharedFolderClosesItsSnapshots(t *testing.T) {
	pool := setupFolders(t)
	req := mediaRequest()
	req.PreviousVersions = true
	if _, err := CreateSharedFolder(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	pool.calls = nil
	if err := DeleteSharedFolder(context.Background(), apischema.SharedFolderDeleteRequest{UUID: testPool, Path: "Media", ConfirmName: "Media"}); err != nil {
		t.Fatal(err)
	}
	if _, open := pool.snapshotACLs["Media"]; open {
		t.Errorf("snapshots still open after delete: calls %v", pool.calls)
	}
}

func createdMedia(t *testing.T) (*fakePool, apischema.SharedFolder) {
	t.Helper()
	pool := setupFolders(t)
	folder, err := CreateSharedFolder(context.Background(), mediaRequest())
	if err != nil {
		t.Fatal(err)
	}
	pool.calls = nil
	return pool, folder
}

// Rename changes the share name people connect to; the folder keeps its
// path, and with it its snapshots and its NFS export.
func TestUpdateSharedFolderRenamesTheShareOnly(t *testing.T) {
	pool, folder := createdMedia(t)
	req := updateOf(folder)
	req.Name = "Films"
	req.Access = []apischema.SharedFolderAccess{row("user", "bob", "rw")}
	req.Everyone = "ro"
	updated, err := UpdateSharedFolder(context.Background(), req)
	if err != nil {
		t.Fatalf("UpdateSharedFolder() = %v", err)
	}
	media := filepath.Join(pool.top, "Media")
	if updated.Name != "Films" || updated.Path != "Media" || updated.Folder != media {
		t.Errorf("updated = %+v", updated)
	}
	smb := readFile(t, smbConfFile)
	if strings.Contains(smb, "[Media]") || !strings.Contains(smb, "[Films]") || !strings.Contains(smb, "path = "+media) {
		t.Errorf("smb.conf:\n%s", smb)
	}
	if strings.Contains(smb, "valid users") || !strings.Contains(smb, "write list = bob") {
		t.Errorf("smb.conf keeps the old access:\n%s", smb)
	}
	if acl := pool.acls["Media"]; acl.Users[1001] != btrfs.ACLReadWrite || len(acl.Groups) != 0 || acl.Other != btrfs.ACLRead {
		t.Errorf("acl = %+v", acl)
	}
	if !slices.Equal(pool.calls, []string{"setACL Media"}) {
		t.Errorf("calls = %v", pool.calls)
	}
}

// Stop sharing takes the folder off the network and keeps the folder, its
// access and its record, so it can be shared again.
func TestUpdateSharedFolderStopsSharing(t *testing.T) {
	pool, folder := createdMedia(t)
	req := updateOf(folder)
	req.SMB = false
	req.NFS = nil
	if _, err := UpdateSharedFolder(context.Background(), req); err != nil {
		t.Fatalf("UpdateSharedFolder() = %v", err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "[Media]") || !strings.Contains(smb, "[plain]") {
		t.Errorf("smb.conf:\n%s", smb)
	}
	if exports := readFile(t, exportsFile); strings.Contains(exports, "Media") || !strings.Contains(exports, "/srv/plain") {
		t.Errorf("exports = %q", exports)
	}
	if _, err := os.Stat(filepath.Join(pool.top, "Media")); err != nil {
		t.Errorf("the folder is gone: %v", err)
	}
	folders, err := loadFolders()
	if err != nil || len(folders) != 1 || folders[0].SMB || len(folders[0].NFS) != 0 || len(folders[0].Access) != 2 {
		t.Errorf("record = %+v, %v", folders, err)
	}
	// Sharing it again publishes it under the access it kept.
	req.SMB = true
	if _, err := UpdateSharedFolder(context.Background(), req); err != nil {
		t.Fatalf("share again = %v", err)
	}
	if smb := readFile(t, smbConfFile); !strings.Contains(smb, "[Media]") || !strings.Contains(smb, "valid users = alice, +family") {
		t.Errorf("smb.conf after sharing again:\n%s", smb)
	}
}

func TestUpdateSharedFolderRefuses(t *testing.T) {
	_, folder := createdMedia(t)
	req := updateOf(folder)
	req.Path = "Other"
	if _, err := UpdateSharedFolder(context.Background(), req); !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Errorf("an unknown folder = %v, want ErrNotFound", err)
	}
	req = updateOf(folder)
	req.Name = "plain"
	if _, err := UpdateSharedFolder(context.Background(), req); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("another share's name = %v, want ErrConflict", err)
	}
	req = updateOf(folder)
	req.Access = nil
	if _, err := UpdateSharedFolder(context.Background(), req); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("SMB with nobody let in = %v, want ErrInvalidArgs", err)
	}
}

func TestDeleteSharedFolder(t *testing.T) {
	pool, folder := createdMedia(t)
	pool.snapshots["Media"] = []string{"linuxio-snapshots/Media/a", "linuxio-snapshots/Media/b"}
	pool.jobs["Media"] = true
	req := apischema.SharedFolderDeleteRequest{UUID: folder.UUID, Path: folder.Path, ConfirmName: "media"}
	if err := DeleteSharedFolder(context.Background(), req); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("a name that does not match = %v, want ErrInvalidArgs", err)
	}
	if len(pool.calls) != 0 || !strings.Contains(readFile(t, smbConfFile), "[Media]") {
		t.Fatalf("the refusal touched the folder: %v", pool.calls)
	}

	req.ConfirmName = "Media"
	if err := DeleteSharedFolder(context.Background(), req); err != nil {
		t.Fatalf("DeleteSharedFolder() = %v", err)
	}
	// Off the network first, then the folder, and only then its schedule,
	// its copies' jobs and its snapshots: a folder that cannot be deleted
	// keeps them.
	want := []string{
		"deleteSubvolume Media",
		"deleteRule Media", "dropReplication Media", "listSnapshots Media",
		"deleteSnapshot linuxio-snapshots/Media/a", "deleteSnapshot linuxio-snapshots/Media/b",
	}
	if !slices.Equal(pool.calls, want) {
		t.Errorf("calls = %v, want %v", pool.calls, want)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "[Media]") {
		t.Errorf("smb.conf:\n%s", smb)
	}
	if exports := readFile(t, exportsFile); strings.Contains(exports, "Media") {
		t.Errorf("exports = %q", exports)
	}
	if folders, err := loadFolders(); err != nil || len(folders) != 0 {
		t.Errorf("record = %+v, %v", folders, err)
	}
}

// A delete that fails part-way keeps the record, off the network, so it is
// listed and can be deleted again.
func TestDeleteSharedFolderKeepsTheRecordWhenItFails(t *testing.T) {
	pool, folder := createdMedia(t)
	pool.snapshots["Media"] = []string{"linuxio-snapshots/Media/a"}
	pool.fail["deleteSubvolume"] = errors.New("busy")
	req := apischema.SharedFolderDeleteRequest{UUID: folder.UUID, Path: folder.Path, ConfirmName: "Media"}
	if err := DeleteSharedFolder(context.Background(), req); err == nil {
		t.Fatal("DeleteSharedFolder() = nil, want the subvolume's error")
	}
	// The folder is still there, so its snapshots and its schedule are too.
	if !slices.Equal(pool.calls, []string{"deleteSubvolume Media"}) || len(pool.rules) != 1 {
		t.Fatalf("calls = %v, rules = %v", pool.calls, pool.rules)
	}
	folders, err := loadFolders()
	if err != nil || len(folders) != 1 || folders[0].SMB || len(folders[0].NFS) != 0 {
		t.Fatalf("record = %+v, %v", folders, err)
	}
	delete(pool.fail, "deleteSubvolume")
	if err := DeleteSharedFolder(context.Background(), req); err != nil {
		t.Fatalf("the second delete = %v", err)
	}
}

// The plain share routes leave a shared folder's section and export alone:
// changed there, they would no longer say what its record says.
func TestPlainRoutesRefuseASharedFolder(t *testing.T) {
	pool, _ := createdMedia(t)
	media := filepath.Join(pool.top, "Media")
	ctx := context.Background()
	checks := map[string]error{
		"update the section": refuseSharedFolderSection("media"),
		"update the export":  refuseSharedFolderPath(ctx, media+"/", false),
	}
	for name, err := range checks {
		if !errors.Is(err, bridgeipc.ErrConflict) {
			t.Errorf("%s = %v, want ErrConflict", name, err)
		}
	}
	if err := refuseSharedFolderSection("plain"); err != nil {
		t.Errorf("a plain section = %v", err)
	}
	if err := refuseSharedFolderPath(ctx, "/srv/plain", true); err != nil {
		t.Errorf("a plain path = %v", err)
	}
	inside := media + "/new/inside"
	if err := refuseSharedFolderPath(ctx, inside, true); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("a new share of a folder inside = %v, want ErrConflict", err)
	}
	if err := refuseSharedFolderPath(ctx, media+"2", true); err != nil {
		t.Errorf("a folder beside, whose name starts the same = %v", err)
	}
	if _, err := handleCreateNFSShare(ctx, apischema.ShareNFSRequest{Path: inside, Clients: []apischema.NFSClient{{Host: "*"}}}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("create_nfs_share inside = %v, want ErrConflict", err)
	}
	// An export inside that is already there (made by hand) can still be
	// removed: removing makes no folder.
	if err := os.WriteFile(exportsFile, []byte(readFile(t, exportsFile)+inside+"\t*(ro)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := handleDeleteNFSShare(ctx, apischema.PathRequest{Path: inside}); err != nil {
		t.Errorf("delete_nfs_share inside = %v", err)
	}
	if _, err := handleDeleteSambaShare(ctx, apischema.NameRequest{Name: "Media"}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("delete_samba_share = %v, want ErrConflict", err)
	}
	if _, err := handleDeleteNFSShare(ctx, apischema.PathRequest{Path: media}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("delete_nfs_share = %v, want ErrConflict", err)
	}
	props := map[string]string{"path": media}
	if _, err := handleUpdateSambaShare(ctx, apischema.ShareUpdateSambaRequest{OldName: "plain", NewName: "plain", Properties: props}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("a plain share moved onto the folder = %v, want ErrConflict", err)
	}
	if _, err := handleCreateSambaShare(ctx, apischema.ShareSambaRequest{Name: "second", Properties: props}); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("a second section on the folder = %v, want ErrConflict", err)
	}
}

// A user or group that is gone loses its rows, and what is left is applied
// again; a table that then lets nobody in closes the share.
func TestReapplyAccessDropsAccountsThatAreGone(t *testing.T) {
	pool, _ := createdMedia(t)
	if err := os.WriteFile(passwdPath, []byte("root:x:0:0::/root:/bin/bash\nbob:x:1001:1001::/home/bob:/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(groupPath, []byte("root:x:0:\nbob:x:1001:\nkids:x:2001:bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReapplyAccess(context.Background()); err != nil {
		t.Fatalf("ReapplyAccess() = %v", err)
	}
	folders, err := loadFolders()
	if err != nil || len(folders) != 1 || len(folders[0].Access) != 0 {
		t.Fatalf("record = %+v, %v", folders, err)
	}
	if acl := pool.acls["Media"]; len(acl.Users) != 0 || len(acl.Groups) != 0 || acl.Other != btrfs.ACLNone {
		t.Errorf("acl = %+v", acl)
	}
	smb := readFile(t, smbConfFile)
	if strings.Contains(smb, "alice") || !strings.Contains(smb, "available = no") {
		t.Errorf("smb.conf:\n%s", smb)
	}
}

// A section that could not be rewritten keeps the row in the record, so the
// next run drops it again and finishes the job.
func TestReapplyAccessTriesAgainAfterAFailure(t *testing.T) {
	createdMedia(t)
	if err := os.WriteFile(passwdPath, []byte("root:x:0:0::/root:/bin/bash\nbob:x:1001:1001::/home/bob:/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	origUnit := systemdReloadUnit
	t.Cleanup(func() { systemdReloadUnit = origUnit })
	boom := errors.New("boom")
	smbcontrolReload = func(context.Context) ([]byte, error) { return nil, boom }
	systemdReloadUnit = func(context.Context, string) error { return boom }
	if err := ReapplyAccess(context.Background()); err == nil {
		t.Fatal("ReapplyAccess() = nil, want the reload's error")
	}
	if folders, err := loadFolders(); err != nil || len(folders[0].Access) != 2 {
		t.Fatalf("record after the failure = %+v, %v: alice's row must stay to be dropped again", folders, err)
	}
	smbcontrolReload = func(context.Context) ([]byte, error) { return nil, nil }
	if err := ReapplyAccess(context.Background()); err != nil {
		t.Fatalf("the second ReapplyAccess() = %v", err)
	}
	folders, err := loadFolders()
	if err != nil || len(folders[0].Access) != 1 || folders[0].Access[0].Name != "family" {
		t.Errorf("record = %+v, %v", folders, err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "alice") {
		t.Errorf("smb.conf still names alice:\n%s", smb)
	}
}

// A row is the account it was given to, by its ID: an account made later
// under the same name is another one, and the row is dropped, even when it
// was kept because applying the access failed when the first went.
func TestAccessDoesNotPassToARecreatedAccount(t *testing.T) {
	pool, folder := createdMedia(t)
	// alice is gone and made again, with another UID; family is made again
	// with another GID.
	if err := os.WriteFile(passwdPath, []byte("root:x:0:0::/root:/bin/bash\nalice:x:1005:1005::/home/alice:/bin/bash\nbob:x:1001:1001::/home/bob:/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(groupPath, []byte("root:x:0:\nalice:x:1005:\nbob:x:1001:\nfamily:x:2005:alice,bob\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Saved as it was listed, the rows are refused: they name the old
	// accounts.
	if _, err := UpdateSharedFolder(context.Background(), updateOf(folder)); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Errorf("an update with the old rows = %v, want ErrInvalidArgs", err)
	}
	if err := ReapplyAccess(context.Background()); err != nil {
		t.Fatalf("ReapplyAccess() = %v", err)
	}
	if acl := pool.acls["Media"]; len(acl.Users) != 0 || len(acl.Groups) != 0 {
		t.Errorf("acl = %+v: the new accounts were given the old rows", acl)
	}
	folders, err := loadFolders()
	if err != nil || len(folders[0].Access) != 0 {
		t.Errorf("record = %+v, %v", folders, err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "alice") || strings.Contains(smb, "family") {
		t.Errorf("smb.conf:\n%s", smb)
	}
}

// A stop sharing that failed after the record was written leaves the section
// behind; dropping a row then removes it rather than leaving it naming the
// account that went.
func TestReapplyAccessRemovesASectionLeftBehind(t *testing.T) {
	_, folder := createdMedia(t)
	unshared := folder
	unshared.SMB, unshared.NFS = false, []apischema.NFSClient{}
	if err := putFolder(unshared); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwdPath, []byte("root:x:0:0::/root:/bin/bash\nbob:x:1001:1001::/home/bob:/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReapplyAccess(context.Background()); err != nil {
		t.Fatalf("ReapplyAccess() = %v", err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "[Media]") {
		t.Errorf("smb.conf:\n%s", smb)
	}
}

// A rename whose section could not be written leaves the record under the
// new name and the section under the old: the next save finds the section
// by its folder and renames it, instead of writing a second one.
func TestUpdateSharedFolderFindsItsSectionByItsFolder(t *testing.T) {
	_, folder := createdMedia(t)
	renamed := folder
	renamed.Name = "Films"
	if err := putFolder(renamed); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateSharedFolder(context.Background(), updateOf(renamed)); err != nil {
		t.Fatalf("UpdateSharedFolder() = %v", err)
	}
	smb := readFile(t, smbConfFile)
	if strings.Contains(smb, "[Media]") || strings.Count(smb, "[Films]") != 1 {
		t.Errorf("smb.conf:\n%s", smb)
	}
}

// A group's members change without the table changing: the ACL follows.
func TestReapplyAccessFollowsGroupMembers(t *testing.T) {
	pool := setupFolders(t)
	req := mediaRequest()
	req.Access = []apischema.SharedFolderAccess{row("group", "family", "rw"), row("group", "kids", "none")}
	if _, err := CreateSharedFolder(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if acl := pool.acls["Media"]; acl.Users[1001] != btrfs.ACLNone || len(acl.Users) != 1 {
		t.Fatalf("acl before = %+v", acl)
	}
	if err := os.WriteFile(groupPath, []byte("root:x:0:\nalice:x:1000:\nbob:x:1001:\nfamily:x:2000:alice,bob\nkids:x:2001:alice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ReapplyAccess(context.Background()); err != nil {
		t.Fatalf("ReapplyAccess() = %v", err)
	}
	acl := pool.acls["Media"]
	if _, bobHasEntry := acl.Users[1001]; bobHasEntry || acl.Users[1000] != btrfs.ACLNone {
		t.Errorf("acl after = %+v", acl)
	}
}

// A folder whose pool is gone, or not mounted, cannot be reached to delete:
// its share and its record are removed, so it does not stay listed for
// ever, and nothing on the pool is touched.
func TestDeleteSharedFolderOfAPoolThatIsNotMounted(t *testing.T) {
	pool, folder := createdMedia(t)
	folderOps.mounts = func(context.Context) ([]btrfs.Mount, error) { return nil, nil }
	req := apischema.SharedFolderDeleteRequest{UUID: folder.UUID, Path: folder.Path, ConfirmName: "Media"}
	if err := DeleteSharedFolder(context.Background(), req); err != nil {
		t.Fatalf("DeleteSharedFolder() = %v", err)
	}
	if !slices.Equal(pool.calls, []string{"deleteRule Media", "dropReplication Media"}) {
		t.Errorf("calls = %v: the pool itself is left alone", pool.calls)
	}
	if _, err := os.Stat(filepath.Join(pool.top, "Media")); err != nil {
		t.Errorf("the folder was touched: %v", err)
	}
	if smb := readFile(t, smbConfFile); strings.Contains(smb, "[Media]") {
		t.Errorf("smb.conf:\n%s", smb)
	}
	if folders, err := loadFolders(); err != nil || len(folders) != 0 {
		t.Errorf("record = %+v, %v", folders, err)
	}
}

func TestListSharedFoldersWithoutARecord(t *testing.T) {
	setupFolders(t)
	folders, err := ListSharedFolders(context.Background())
	if err != nil || folders == nil || len(folders) != 0 {
		t.Errorf("ListSharedFolders() = %v, %v, want an empty list", folders, err)
	}
}

func TestListSharedFoldersOfAPoolThatIsNotMounted(t *testing.T) {
	_, folder := createdMedia(t)
	folderOps.mounts = func(context.Context) ([]btrfs.Mount, error) { return nil, nil }
	folders, err := ListSharedFolders(context.Background())
	if err != nil || len(folders) != 1 || folders[0].Folder != "" || folders[0].Name != folder.Name {
		t.Errorf("ListSharedFolders() = %+v, %v", folders, err)
	}
	if _, err := UpdateSharedFolder(context.Background(), updateOf(folder)); !errors.Is(err, bridgeipc.ErrConflict) {
		t.Errorf("an update while the pool is not mounted = %v, want ErrConflict", err)
	}
}

func TestSharedFoldersFileIsInTheConfigFolder(t *testing.T) {
	if sharedFoldersFile != "/etc/linuxio/shared-folders.json" {
		t.Errorf("sharedFoldersFile = %s", sharedFoldersFile)
	}
}
