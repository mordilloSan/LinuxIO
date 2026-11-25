package shares

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

const testPool = "uuid-tank"

// fakePool stands in for the pool: it keeps what was made on it and the
// order of the calls, and fails the call named in fail.
type fakePool struct {
	top       string
	calls     []string
	fail      map[string]error
	rules     map[string]btrfs.BtrfsSnapshotRule
	acls      map[string]btrfs.FolderACL
	limits    map[string]uint64
	snapshots map[string][]string
	jobs      map[string]bool
	// onCreate runs once the subvolume is made.
	onCreate func()
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
		top:       filepath.Join(dir, "tank"),
		fail:      map[string]error{},
		rules:     map[string]btrfs.BtrfsSnapshotRule{},
		acls:      map[string]btrfs.FolderACL{},
		limits:    map[string]uint64{},
		snapshots: map[string][]string{},
		jobs:      map[string]bool{},
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
	wantCalls := []string{"listSnapshots Media", "createSubvolume Media", "setLimit Media", "saveRule Media", "setACL Media"}
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
