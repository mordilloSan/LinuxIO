package shares

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/storage"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// A shared folder is a subvolume at the top of a pool that LinuxIO made to
// be shared (docs/shared-folders.md): it has a size limit and a snapshot
// schedule of its own, an access table, and a record in sharedFoldersFile
// from which its smb.conf section, its export and its ACL are written. Any
// other path is a plain share, which the routes of samba.go and nfs.go edit
// and which has no access table.

// folderStorage is the storage operations a shared folder is made of.
type folderStorage struct {
	mounts          func(ctx context.Context) ([]btrfs.Mount, error)
	createSubvolume func(ctx context.Context, uuid, path string) error
	deleteSubvolume func(ctx context.Context, uuid, path string) error
	// setLimit turns quotas on for the pool and limits the folder's size.
	setLimit   func(ctx context.Context, uuid, path string, max uint64) error
	saveRule   func(ctx context.Context, rule btrfs.BtrfsSnapshotRule) error
	deleteRule func(ctx context.Context, uuid, path string) error
	// ruleExists says a snapshot rule names the path.
	ruleExists func(uuid, path string) (bool, error)
	// replicated says a replication job copies the path.
	replicated func(uuid, path string) (bool, error)
	setACL     func(ctx context.Context, uuid, path string, acl btrfs.FolderACL) error
	// listSnapshots returns the paths of the folder's snapshots.
	listSnapshots  func(ctx context.Context, uuid, path string) ([]string, error)
	deleteSnapshot func(ctx context.Context, uuid, snapshotPath string) error
	// dropReplication removes the jobs that copy the folder elsewhere; the
	// copies stay where they are.
	dropReplication func(ctx context.Context, uuid, path string) error
}

// folderOps is a var so tests can stand in for the pool.
var folderOps = folderStorage{
	mounts: btrfs.ListMounts,
	createSubvolume: func(ctx context.Context, uuid, path string) error {
		_, err := storage.CreateBtrfsSubvolume(ctx, apischema.BtrfsSubvolumeRequest{UUID: uuid, Path: path})
		return err
	},
	deleteSubvolume: func(ctx context.Context, uuid, path string) error {
		return storage.DeleteBtrfsSubvolume(ctx, apischema.BtrfsSubvolumeRequest{UUID: uuid, Path: path})
	},
	setLimit: func(ctx context.Context, uuid, path string, max uint64) error {
		if _, err := storage.SetBtrfsQuotas(ctx, apischema.BtrfsQuotaRequest{UUID: uuid, Enabled: true}); err != nil {
			return err
		}
		_, err := storage.LimitBtrfsQgroup(ctx, apischema.BtrfsQgroupLimitRequest{UUID: uuid, Path: path, MaxReferencedBytes: &max})
		return err
	},
	saveRule: func(ctx context.Context, rule btrfs.BtrfsSnapshotRule) error {
		_, err := storage.SaveBtrfsSnapshotRule(ctx, rule)
		return err
	},
	deleteRule: func(ctx context.Context, uuid, path string) error {
		_, err := storage.DeleteBtrfsSnapshotRule(ctx, uuid, path)
		return err
	},
	replicated: func(uuid, path string) (bool, error) {
		jobs, err := btrfs.LoadJobs()
		return slices.ContainsFunc(jobs, func(j btrfs.BtrfsReplicationJob) bool {
			return j.UUID == uuid && j.Path == path
		}), err
	},
	ruleExists: func(uuid, path string) (bool, error) {
		rules, err := btrfs.LoadRules()
		return slices.ContainsFunc(rules, func(r btrfs.BtrfsSnapshotRule) bool {
			return r.UUID == uuid && r.Path == path
		}), err
	},
	setACL: btrfs.SetFolderACL,
	listSnapshots: func(ctx context.Context, uuid, path string) ([]string, error) {
		snapshots, err := btrfs.ListSnapshots(ctx, uuid, &path)
		if err != nil {
			return nil, err
		}
		paths := make([]string, len(snapshots))
		for i, snapshot := range snapshots {
			paths[i] = snapshot.Path
		}
		return paths, nil
	},
	deleteSnapshot: func(ctx context.Context, uuid, snapshotPath string) error {
		return storage.DeleteBtrfsSnapshot(ctx, apischema.BtrfsSnapshotRequest{UUID: uuid, SnapshotPath: snapshotPath})
	},
	dropReplication: func(ctx context.Context, uuid, path string) error {
		jobs, err := btrfs.LoadJobs()
		if err != nil {
			return err
		}
		for _, job := range jobs {
			if job.UUID != uuid || job.Path != path {
				continue
			}
			if _, err := storage.DeleteBtrfsReplicationJob(ctx, job.ID); err != nil {
				return err
			}
		}
		return nil
	},
}

// validFolderName is a name that is one folder of a pool and a share name
// at once.
var validFolderName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// maxShareNameLength is the longest share name a client connects to.
const maxShareNameLength = 80

// errNotMounted is a pool with no permanent mount of its top level.
var errNotMounted = errors.New("no permanent mount of its top level")

// mountedTop returns where the top level of the pool uuid is mounted for
// good, which is where its folders are on the host. The mount LinuxIO makes
// for its own work under btrfs.TopLevelDir goes away, so a share cannot
// point into it.
func mountedTop(ctx context.Context, uuid string) (string, error) {
	mounts, err := folderOps.mounts(ctx)
	if err != nil {
		return "", fmt.Errorf("list btrfs mounts: %w", err)
	}
	for _, m := range mounts {
		if m.UUID == uuid && m.SubvolID == 5 && m.SubPath == "" && !strings.HasPrefix(m.Mountpoint, btrfs.TopLevelDir+"/") {
			return m.Mountpoint, nil
		}
	}
	return "", fmt.Errorf("%w: pool %s has %w", bridgeipc.ErrConflict, uuid, errNotMounted)
}

// permanentTop is mountedTop for an operation that changes a shared folder:
// the top folder must be root's alone. A user who may write it could put
// another folder of the pool under a shared folder's name, which root would
// then give to itself, open to others or delete in its place.
func permanentTop(ctx context.Context, uuid string) (string, error) {
	top, err := mountedTop(ctx, uuid)
	if err != nil {
		return "", err
	}
	if err = topIsRootOnly(top); err != nil {
		return "", err
	}
	return top, nil
}

// topIsRootOnly fails unless root owns the folder at top and neither its
// group nor everyone may write it. A var so unit tests, whose folders are
// the test user's, can pass.
var topIsRootOnly = func(top string) error {
	var st unix.Stat_t
	if err := unix.Stat(top, &st); err != nil {
		return fmt.Errorf("stat %s: %w", top, err)
	}
	if st.Uid != 0 || st.Mode&0o022 != 0 {
		return fmt.Errorf("%w: other users may write %s, the top folder of the pool, so LinuxIO does not keep shared folders there; make it root's with mode 755", bridgeipc.ErrConflict, top)
	}
	return nil
}

func aclPerm(level string) btrfs.ACLPerm {
	switch level {
	case levelRW:
		return btrfs.ACLReadWrite
	case levelRO:
		return btrfs.ACLRead
	}
	return btrfs.ACLNone
}

// folderACL is the ACL of a folder's access table.
func folderACL(f apischema.SharedFolder, accounts localAccounts) btrfs.FolderACL {
	plan := planAccess(f.Access, f.Everyone, accounts)
	acl := btrfs.FolderACL{
		Users:  make(map[int]btrfs.ACLPerm, len(plan.users)),
		Groups: make(map[int]btrfs.ACLPerm, len(plan.groups)),
		Other:  aclPerm(plan.everyone),
	}
	for uid, level := range plan.users {
		acl.Users[uid] = aclPerm(level)
	}
	for gid, level := range plan.groups {
		acl.Groups[gid] = aclPerm(level)
	}
	return acl
}

// folderSambaProperties is a shared folder's smb.conf section. Samba makes a
// file with the mode "create mask" allows, and the group bits of that mode
// become the mask of the ACL the file inherits: with Samba's own defaults
// (0744, 0755) the people the folder gives read and write could not change
// what another of them made. "inherit acls" has Samba start from the
// folder's default ACL.
func folderSambaProperties(f apischema.SharedFolder, folderPath string) map[string]string {
	props := sambaAccessProperties(f.Access, f.Everyone)
	props["path"] = folderPath
	props["browseable"] = "yes"
	props["inherit acls"] = "yes"
	props["create mask"] = "0666"
	props["directory mask"] = "0777"
	return props
}

// sectionNamed returns the name smb.conf has a section under, compared as
// Samba compares them, or "".
func sectionNamed(name string) (string, error) {
	return folderSection(name, "")
}

// folderSection returns the section of a shared folder: the one named name,
// or else the one whose path is folderPath (when given), which is where a
// rename that failed half-way left it. "" when there is none.
func folderSection(name, folderPath string) (string, error) {
	sections, err := parseSmbConf()
	if err != nil {
		return "", err
	}
	for existing := range sections {
		if name != "" && strings.EqualFold(existing, name) {
			return existing, nil
		}
	}
	if folderPath == "" {
		return "", nil
	}
	for existing, props := range sections {
		if !reservedSections[strings.ToLower(existing)] && props["path"] != "" && filepath.Clean(props["path"]) == folderPath {
			return existing, nil
		}
	}
	return "", nil
}

func exportExists(path string) (bool, error) {
	exports, err := parseExportsFile()
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(exports, func(e apischema.NFSExport) bool {
		return filepath.Clean(e.Path) == path
	}), nil
}

// publishFolder makes smb.conf and /etc/exports say what f says: its section
// written, renamed from oldName or removed, and its export written or
// removed. It goes by what the files hold, not by what the record held, so
// running it again after a failure finishes the job.
func publishFolder(ctx context.Context, oldName string, f apischema.SharedFolder, folderPath string) error {
	current, err := folderSection(oldName, folderPath)
	if err != nil {
		return err
	}
	switch {
	case f.SMB && current != "":
		err = UpdateSambaShare(ctx, current, f.Name, folderSambaProperties(f, folderPath))
	case f.SMB:
		err = CreateSambaShare(ctx, f.Name, folderSambaProperties(f, folderPath))
	case current != "":
		err = DeleteSambaShare(ctx, current)
	}
	if err != nil {
		return err
	}
	exported, err := exportExists(folderPath)
	if err != nil {
		return err
	}
	switch {
	case len(f.NFS) > 0 && exported:
		return UpdateNFSShare(ctx, folderPath, f.NFS)
	case len(f.NFS) > 0:
		return CreateNFSShare(ctx, folderPath, f.NFS)
	case exported:
		return DeleteNFSShare(ctx, folderPath)
	}
	return nil
}

// unpublishFolder takes a folder off the network: its section and its
// export, each tried whatever the other does.
func unpublishFolder(ctx context.Context, name, folderPath string) error {
	var errs []error
	if current, err := folderSection(name, folderPath); err != nil {
		errs = append(errs, err)
	} else if current != "" {
		errs = append(errs, DeleteSambaShare(ctx, current))
	}
	if exported, err := exportExists(folderPath); err != nil {
		errs = append(errs, err)
	} else if exported {
		errs = append(errs, DeleteNFSShare(ctx, folderPath))
	}
	return errors.Join(errs...)
}

// checkShareName checks the share name of f against Samba's rules, the other
// shared folders and the sections of smb.conf. oldName is the name f has
// now, "" for a new folder.
func checkShareName(f apischema.SharedFolder, oldName string, folders []apischema.SharedFolder) error {
	if !validShareName.MatchString(f.Name) || len(f.Name) > maxShareNameLength || reservedSections[strings.ToLower(f.Name)] {
		return fmt.Errorf("%w: share name %q", bridgeipc.ErrInvalidArgs, f.Name)
	}
	if other, taken := folderNamed(folders, f.Name); taken && (other.UUID != f.UUID || other.Path != f.Path) {
		return fmt.Errorf("%w: a shared folder is already named %s", bridgeipc.ErrConflict, other.Name)
	}
	if strings.EqualFold(f.Name, oldName) {
		return nil
	}
	section, err := sectionNamed(f.Name)
	if err != nil {
		return err
	}
	if section != "" {
		return fmt.Errorf("%w: a share is already named %s", bridgeipc.ErrConflict, section)
	}
	return nil
}

// checkServers checks that f, at folderPath, can be shared the ways it asks
// to be: the server is installed, and what it would be told is valid.
func checkServers(f apischema.SharedFolder, folderPath string) error {
	if (f.SMB || len(f.NFS) > 0) && !validExportPath.MatchString(folderPath) {
		return fmt.Errorf("%w: %s has characters a share path may not have", bridgeipc.ErrConflict, folderPath)
	}
	if f.SMB {
		if err := requireSambaServerAvailability(); err != nil {
			return err
		}
		if grantsNobody(f.Access, f.Everyone) {
			return fmt.Errorf("%w: sharing over SMB needs someone with access", bridgeipc.ErrInvalidArgs)
		}
	}
	if len(f.NFS) > 0 {
		if err := requireNFSServerAvailability(); err != nil {
			return err
		}
		if _, err := validateNFSExport(folderPath, f.NFS); err != nil {
			return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
		}
	}
	return nil
}

// checkSharing checks what a create or an update asks of the folder f, at
// folderPath, against the other folders, the local accounts and the servers
// installed, and returns the accounts its access was checked against.
func checkSharing(f apischema.SharedFolder, oldName, folderPath string, folders []apischema.SharedFolder) (localAccounts, error) {
	if err := checkShareName(f, oldName, folders); err != nil {
		return localAccounts{}, err
	}
	accounts, err := readLocalAccounts()
	if err != nil {
		return localAccounts{}, err
	}
	if err := validateAccess(f.Access, f.Everyone, accounts); err != nil {
		return localAccounts{}, err
	}
	return accounts, checkServers(f, folderPath)
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// ListSharedFolders returns the shared folders with where each is on the
// host.
func ListSharedFolders(ctx context.Context) ([]apischema.SharedFolder, error) {
	folders, err := loadFolders()
	if err != nil {
		return nil, err
	}
	for i := range folders {
		if top, err := mountedTop(ctx, folders[i].UUID); err == nil {
			folders[i].Folder = filepath.Join(top, folders[i].Path)
		} else if !errors.Is(err, bridgeipc.ErrConflict) {
			return nil, err
		}
	}
	return folders, nil
}

// CreateSharedFolder makes a shared folder: the subvolume, its size limit,
// its snapshot rule, its access, its record, and its SMB section and NFS
// export. A step that fails undoes the ones before it, newest first; the
// folder is new and empty, so nothing of anyone's is deleted. Quotas turned
// on for the pool stay on.
func CreateSharedFolder(ctx context.Context, req apischema.SharedFolderCreateRequest) (apischema.SharedFolder, error) {
	var created apischema.SharedFolder
	err := withFoldersLock(ctx, func() error {
		f, rule, accounts, err := newSharedFolder(ctx, req)
		if err != nil {
			return err
		}
		created = f
		return makeSharedFolder(ctx, f, req.MaxReferencedBytes, rule, accounts)
	})
	if err != nil {
		return apischema.SharedFolder{}, fmt.Errorf("create shared folder %s: %w", req.Name, err)
	}
	slog.Info("shared folder created", "uuid", created.UUID, "path", created.Path, "smb", created.SMB, "nfs", len(created.NFS) > 0)
	return created, nil
}

// newSharedFolder checks a create request through, touching nothing, and
// returns the folder it asks for, its snapshot rule (nil for none) and the
// accounts its access was checked against.
func newSharedFolder(ctx context.Context, req apischema.SharedFolderCreateRequest) (apischema.SharedFolder, *btrfs.BtrfsSnapshotRule, localAccounts, error) {
	f := apischema.SharedFolder{
		UUID: req.UUID, Path: req.Name, Name: req.Name,
		Access: nonNil(req.Access), Everyone: req.Everyone,
		SMB: req.SMB, NFS: nonNil(req.NFS),
	}
	refuse := func(err error) (apischema.SharedFolder, *btrfs.BtrfsSnapshotRule, localAccounts, error) {
		return apischema.SharedFolder{}, nil, localAccounts{}, err
	}
	if !validFolderName.MatchString(req.Name) || req.Name == btrfs.SnapshotRoot {
		return refuse(fmt.Errorf("%w: folder name %q", bridgeipc.ErrInvalidArgs, req.Name))
	}
	if req.MaxReferencedBytes != nil && *req.MaxReferencedBytes == 0 {
		return refuse(fmt.Errorf("%w: a size limit of zero", bridgeipc.ErrInvalidArgs))
	}
	var rule *btrfs.BtrfsSnapshotRule
	if req.SnapshotRule != nil {
		named := *req.SnapshotRule
		named.UUID, named.Path = f.UUID, f.Path
		if err := btrfs.ValidateRule(named); err != nil {
			return refuse(fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err))
		}
		rule = &named
	}
	top, err := permanentTop(ctx, f.UUID)
	if err != nil {
		return refuse(err)
	}
	f.Folder = filepath.Join(top, f.Path)
	folders, err := loadFolders()
	if err != nil {
		return refuse(err)
	}
	accounts, err := checkSharing(f, "", f.Folder, folders)
	if err != nil {
		return refuse(err)
	}
	f.Access = withAccountIDs(f.Access, accounts)
	if folderIndex(folders, f.UUID, f.Path) >= 0 {
		return refuse(fmt.Errorf("%w: %s is already a shared folder", bridgeipc.ErrConflict, f.Path))
	}
	// One name directly below root's own mount point, not followed if it is
	// a link.
	if _, statErr := os.Lstat(f.Folder); !errors.Is(statErr, fs.ErrNotExist) {
		return refuse(fmt.Errorf("%w: %s is already on the pool", bridgeipc.ErrConflict, f.Folder))
	}
	if err = refuseLeftovers(ctx, f); err != nil {
		return refuse(err)
	}
	return f, rule, accounts, nil
}

// refuseLeftovers refuses a new folder where an earlier one of the same name
// left something behind: its snapshots would be listed, pruned and deleted
// as the new folder's, its rule would snapshot it, its job would send it to
// the old target, and an undo would remove the section or the export that
// was there before.
func refuseLeftovers(ctx context.Context, f apischema.SharedFolder) error {
	left := func(what string) error {
		return fmt.Errorf("%w: %s of an earlier %s is still there; remove it or choose another name", bridgeipc.ErrConflict, what, f.Path)
	}
	if section, err := folderSection("", f.Folder); err != nil {
		return err
	} else if section != "" {
		return left("the SMB share " + section)
	}
	if exported, err := exportExists(f.Folder); err != nil {
		return err
	} else if exported {
		return left("an NFS export")
	}
	if ruled, err := folderOps.ruleExists(f.UUID, f.Path); err != nil {
		return err
	} else if ruled {
		return left("the snapshot schedule")
	}
	if copied, err := folderOps.replicated(f.UUID, f.Path); err != nil {
		return err
	} else if copied {
		return left("a replication job")
	}
	snapshots, err := folderOps.listSnapshots(ctx, f.UUID, f.Path)
	if err != nil {
		return err
	}
	if len(snapshots) > 0 {
		return left(fmt.Sprintf("%d snapshots", len(snapshots)))
	}
	return nil
}

// folderStep is one step of making a shared folder, with how to take it
// back; undo is nil for a step that leaves nothing of its own.
type folderStep struct {
	do   func(ctx context.Context) error
	undo func(ctx context.Context) error
}

// makeSharedFolder makes the checked folder f, step by step, and undoes the
// steps done when one fails.
func makeSharedFolder(ctx context.Context, f apischema.SharedFolder, limit *uint64, rule *btrfs.BtrfsSnapshotRule, accounts localAccounts) error {
	steps := []folderStep{{
		do:   func(ctx context.Context) error { return folderOps.createSubvolume(ctx, f.UUID, f.Path) },
		undo: func(ctx context.Context) error { return folderOps.deleteSubvolume(ctx, f.UUID, f.Path) },
	}}
	if limit != nil {
		// The limit goes with the subvolume; quotas stay on for the pool.
		steps = append(steps, folderStep{
			do: func(ctx context.Context) error { return folderOps.setLimit(ctx, f.UUID, f.Path, *limit) },
		})
	}
	if rule != nil {
		steps = append(steps, folderStep{
			do:   func(ctx context.Context) error { return folderOps.saveRule(ctx, *rule) },
			undo: func(ctx context.Context) error { return folderOps.deleteRule(ctx, f.UUID, f.Path) },
		})
	}
	steps = append(steps,
		folderStep{
			do: func(ctx context.Context) error { return folderOps.setACL(ctx, f.UUID, f.Path, folderACL(f, accounts)) },
		},
		folderStep{
			do:   func(context.Context) error { return putFolder(f) },
			undo: func(context.Context) error { return removeFolder(f.UUID, f.Path) },
		},
		// Publishing writes two files, and a failure after the first
		// leaves it written: this step is undone even when it fails.
		folderStep{
			do:   func(ctx context.Context) error { return publishFolder(ctx, "", f, f.Folder) },
			undo: func(ctx context.Context) error { return unpublishFolder(ctx, f.Name, f.Folder) },
		},
	)
	for i, step := range steps {
		err := step.do(ctx)
		if err == nil {
			continue
		}
		done := steps[:i]
		if i == len(steps)-1 {
			done = steps
		}
		// The request may be why the step failed (cancelled, timed out);
		// the undo runs all the same.
		undoCtx := context.WithoutCancel(ctx)
		for _, made := range slices.Backward(done) {
			if made.undo == nil {
				continue
			}
			if undoErr := made.undo(undoCtx); undoErr != nil {
				slog.Warn("failed to undo a step of a shared folder that could not be made", "uuid", f.UUID, "path", f.Path, "error", undoErr)
			}
		}
		return err
	}
	return nil
}

// UpdateSharedFolder changes a shared folder's share name, access and
// sharing. The folder keeps its path: a new name is a new share name only,
// so its snapshots, its copies and whatever mounts it are untouched. SMB off
// and no NFS client stop sharing it; the folder, its access and its record
// stay. Access is tightened on the folder before the record and the network
// follow.
func UpdateSharedFolder(ctx context.Context, req apischema.SharedFolderUpdateRequest) (apischema.SharedFolder, error) {
	var updated apischema.SharedFolder
	err := withFoldersLock(ctx, func() error {
		folders, err := loadFolders()
		if err != nil {
			return err
		}
		i := folderIndex(folders, req.UUID, req.Path)
		if i < 0 {
			return fmt.Errorf("%w: no shared folder %s on %s", bridgeipc.ErrNotFound, req.Path, req.UUID)
		}
		old := folders[i]
		f := old
		f.Name, f.Access, f.Everyone = req.Name, nonNil(req.Access), req.Everyone
		f.SMB, f.NFS = req.SMB, nonNil(req.NFS)
		top, err := permanentTop(ctx, f.UUID)
		if err != nil {
			return err
		}
		f.Folder = filepath.Join(top, f.Path)
		accounts, err := checkSharing(f, old.Name, f.Folder, folders)
		if err != nil {
			return err
		}
		f.Access = withAccountIDs(f.Access, accounts)
		if err = folderOps.setACL(ctx, f.UUID, f.Path, folderACL(f, accounts)); err != nil {
			return err
		}
		if err = putFolder(f); err != nil {
			return err
		}
		updated = f
		return publishFolder(ctx, old.Name, f, f.Folder)
	})
	if err != nil {
		return apischema.SharedFolder{}, fmt.Errorf("update shared folder %s: %w", req.Path, err)
	}
	slog.Info("shared folder updated", "uuid", updated.UUID, "path", updated.Path, "name", updated.Name, "smb", updated.SMB, "nfs", len(updated.NFS) > 0)
	return updated, nil
}

// DeleteSharedFolder deletes a shared folder with everything in it and its
// snapshots, once ConfirmName is its share name. It comes off the network
// first; its snapshot rule and the jobs that copy it go, and the copies
// already made elsewhere stay. The record goes last, so a delete that fails
// part-way is still listed and can be run again.
//
// A folder whose pool is not mounted, or is gone, cannot be reached: only
// what LinuxIO holds of it is removed (forgetFolder), so it does not stay
// listed for ever.
func DeleteSharedFolder(ctx context.Context, req apischema.SharedFolderDeleteRequest) error {
	withData := true
	err := withFoldersLock(ctx, func() error {
		folders, err := loadFolders()
		if err != nil {
			return err
		}
		i := folderIndex(folders, req.UUID, req.Path)
		if i < 0 {
			return fmt.Errorf("%w: no shared folder %s on %s", bridgeipc.ErrNotFound, req.Path, req.UUID)
		}
		f := folders[i]
		if req.ConfirmName != f.Name {
			return fmt.Errorf("%w: the name typed is not the folder's, %s", bridgeipc.ErrInvalidArgs, f.Name)
		}
		top, err := permanentTop(ctx, f.UUID)
		if errors.Is(err, errNotMounted) {
			withData = false
			return forgetFolder(ctx, f)
		}
		if err != nil {
			return err
		}
		if err = unpublishFolder(ctx, f.Name, filepath.Join(top, f.Path)); err != nil {
			return err
		}
		f.SMB, f.NFS = false, []apischema.NFSClient{}
		if err = putFolder(f); err != nil {
			return err
		}
		if err = deleteFolderData(ctx, f); err != nil {
			return err
		}
		return removeFolder(f.UUID, f.Path)
	})
	if err != nil {
		return fmt.Errorf("delete shared folder %s: %w", req.Path, err)
	}
	if !withData {
		slog.Info("shared folder removed, its pool not being mounted", "uuid", req.UUID, "path", req.Path)
		return nil
	}
	slog.Info("shared folder deleted with its data", "uuid", req.UUID, "path", req.Path)
	return nil
}

// forgetFolder removes what LinuxIO holds of a shared folder whose pool is
// not mounted: its section, its snapshot rule, the jobs that copy it and its
// record. What is on the pool stays, a folder that is no longer shared when
// the pool comes back. Its export names a place the record does not hold, so
// it stays in /etc/exports, where it shows as a plain share.
func forgetFolder(ctx context.Context, f apischema.SharedFolder) error {
	section, err := sectionNamed(f.Name)
	if err != nil {
		return err
	}
	if section != "" {
		if err = DeleteSambaShare(ctx, section); err != nil {
			return err
		}
	}
	if err = folderOps.deleteRule(ctx, f.UUID, f.Path); err != nil {
		return err
	}
	if err = folderOps.dropReplication(ctx, f.UUID, f.Path); err != nil {
		return err
	}
	return removeFolder(f.UUID, f.Path)
}

// deleteFolderData deletes what a shared folder holds on its pool: the
// subvolume first, then its snapshot rule, the jobs that copy it and its
// snapshots. A folder that cannot be deleted (mounted, or holding a
// subvolume someone made inside) so keeps its snapshots and its schedule;
// once it is gone, the rest is deleted whatever is left of a run before.
func deleteFolderData(ctx context.Context, f apischema.SharedFolder) error {
	if err := folderOps.deleteSubvolume(ctx, f.UUID, f.Path); err != nil {
		return err
	}
	if err := folderOps.deleteRule(ctx, f.UUID, f.Path); err != nil {
		return err
	}
	if err := folderOps.dropReplication(ctx, f.UUID, f.Path); err != nil {
		return err
	}
	snapshots, err := folderOps.listSnapshots(ctx, f.UUID, f.Path)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if err := folderOps.deleteSnapshot(ctx, f.UUID, snapshot); err != nil {
			return err
		}
	}
	return nil
}

// ReapplyAccess writes the access of every shared folder again, after the
// local users or groups changed: a row whose account is gone is dropped, so
// an account made later under its name or ID does not inherit it, and the
// members of a group with no access are worked out anew (planAccess). Each
// folder is tried; the errors come back together.
func ReapplyAccess(ctx context.Context) error {
	// A host with no shared folder has nothing to apply, and no lock to
	// take for it.
	if folders, err := loadFolders(); err != nil || len(folders) == 0 {
		return err
	}
	return withFoldersLock(ctx, func() error {
		folders, err := loadFolders()
		if err != nil {
			return err
		}
		accounts, err := readLocalAccounts()
		if err != nil {
			return err
		}
		var errs []error
		dropped := false
		for i := range folders {
			kept, err := reapplyFolder(ctx, folders[i], accounts)
			if err != nil {
				errs = append(errs, fmt.Errorf("shared folder %s: %w", folders[i].Name, err))
				continue
			}
			if len(kept) != len(folders[i].Access) {
				folders[i].Access = kept
				dropped = true
			}
		}
		if dropped {
			errs = append(errs, saveFolders(folders))
		}
		return errors.Join(errs...)
	})
}

// reapplyFolder writes f's ACL again without the rows whose account is
// gone, or is another account under the same name, and its section when a
// row went (Samba reads a group's members itself, so only a dropped row
// changes it), and returns the rows kept. On
// an error the caller keeps the record as it was, so the next run drops the
// rows again and finishes the job.
func reapplyFolder(ctx context.Context, f apischema.SharedFolder, accounts localAccounts) ([]apischema.SharedFolderAccess, error) {
	top, err := permanentTop(ctx, f.UUID)
	if err != nil {
		return nil, err
	}
	next := f
	next.Access = slices.DeleteFunc(slices.Clone(f.Access), func(row apischema.SharedFolderAccess) bool {
		return validateAccess([]apischema.SharedFolderAccess{row}, f.Everyone, accounts) != nil
	})
	if err = folderOps.setACL(ctx, f.UUID, f.Path, folderACL(next, accounts)); err != nil {
		return nil, err
	}
	if len(next.Access) == len(f.Access) {
		return next.Access, nil
	}
	// Published whether or not it is shared: a stop sharing that failed
	// half-way left a section naming the account, which this removes.
	if err = publishFolder(ctx, f.Name, next, filepath.Join(top, f.Path)); err != nil {
		return nil, err
	}
	slog.Info("shared folder access dropped for accounts that are gone", "uuid", f.UUID, "path", f.Path)
	return next.Access, nil
}

// refuseSharedFolderSection refuses a plain share route the section name of
// a shared folder: changed there, the section would no longer say what the
// folder's record says. A folder that is not shared keeps its name too, so
// sharing it again finds it free.
func refuseSharedFolderSection(name string) error {
	folders, err := loadFolders()
	if err != nil {
		return err
	}
	if f, ok := folderNamed(folders, name); ok {
		return fmt.Errorf("%w: %s is a shared folder; change it as one", bridgeipc.ErrConflict, f.Name)
	}
	return nil
}

// refuseSharedFolderPath is refuseSharedFolderSection for a path: a shared
// folder has one section and one export, its own. With inside, a path in the
// folder is refused too: a new plain share makes the folders missing on its
// path, by path, as root, in a place the folder's users may write.
func refuseSharedFolderPath(ctx context.Context, path string, inside bool) error {
	folders, err := loadFolders()
	if err != nil || len(folders) == 0 || path == "" {
		return err
	}
	mounts, err := folderOps.mounts(ctx)
	if err != nil {
		return fmt.Errorf("list btrfs mounts: %w", err)
	}
	path = filepath.Clean(path)
	for _, f := range folders {
		for _, m := range mounts {
			if m.UUID != f.UUID || m.SubvolID != 5 || m.SubPath != "" {
				continue
			}
			folder := filepath.Join(m.Mountpoint, f.Path)
			if path == folder || inside && strings.HasPrefix(path, folder+"/") {
				return fmt.Errorf("%w: %s is in the shared folder %s; change it as one", bridgeipc.ErrConflict, path, f.Name)
			}
		}
	}
	return nil
}

// plainShare runs a plain share route's change unless it is on the section
// or the folder of a shared folder, under the lock the shared folder
// operations take, so neither sees the other half-done.
func plainShare(ctx context.Context, sections []string, path string, inside bool, change func() error) error {
	return withFoldersLock(ctx, func() error {
		for _, section := range sections {
			if err := refuseSharedFolderSection(section); err != nil {
				return err
			}
		}
		if err := refuseSharedFolderPath(ctx, path, inside); err != nil {
			return err
		}
		return change()
	})
}
