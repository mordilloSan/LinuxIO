package apischema

import "github.com/mordilloSan/LinuxIO/backend/common/btrfs"

type NFSExport struct {
	Active  bool        `json:"active"`
	Clients []NFSClient `json:"clients"`
	Path    string      `json:"path"`
}

type SambaShare struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type NFSClient struct {
	Host    string   `json:"host"`
	Options []string `json:"options"`
}

type ShareNFSRequest struct {
	Path    string      `json:"path"`
	Clients []NFSClient `json:"clients"`
}

type ShareSambaRequest struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type ShareUpdateSambaRequest struct {
	OldName    string            `json:"oldName"`
	NewName    string            `json:"newName"`
	Properties map[string]string `json:"properties"`
}

// SharedFolderAccess is one row of a shared folder's access table: what a
// local user or group may do in it.
type SharedFolderAccess struct {
	// Kind is "user" or "group".
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Level is "rw" (read and write), "ro" (read only) or "none" (no access).
	Level string `json:"level"`
	// ID is the UID or GID of the account the row was given to, set by the
	// server; 0 in a request for a row just added. A name that now belongs
	// to another ID is another account, which the row does not reach.
	ID int `json:"id,omitzero"`
}

// SharedFolder is a pool subvolume LinuxIO made to be shared, with who may
// use it. UUID and Path name it as the snapshot rules do; Name is the share
// name people connect to.
type SharedFolder struct {
	UUID   string               `json:"uuid"`
	Path   string               `json:"path"`
	Name   string               `json:"name"`
	Access []SharedFolderAccess `json:"access"`
	// Everyone is the level of whoever matches no row of Access.
	Everyone string `json:"everyone"`
	// SMB says whether the folder has a section in smb.conf.
	SMB bool `json:"smb"`
	// NFS lists the clients the folder is exported to; empty is not exported.
	NFS []NFSClient `json:"nfs"`
	// The Network tab (docs/shared-folders.md, "Network"): earlier versions
	// from the folder's snapshots, a recycle bin for what is deleted over SMB,
	// and Time Machine, with the size it may take (nil for no limit).
	PreviousVersions    bool    `json:"previousVersions"`
	RecycleBin          bool    `json:"recycleBin"`
	TimeMachine         bool    `json:"timeMachine"`
	TimeMachineMaxBytes *uint64 `json:"timeMachineMaxBytes,omitzero"`
	// Folder is where the folder is on the host, empty while its pool has
	// no permanent mount of its top level. Filled when listed, not stored.
	Folder string `json:"folder,omitempty"`
}

type SharedFolderCreateRequest struct {
	UUID string `json:"uuid"`
	// Name is the folder's name at the top of the pool and its share name.
	Name string `json:"name"`
	// MaxReferencedBytes is the size limit; nil is none. A limit turns on
	// quotas for the pool.
	MaxReferencedBytes *uint64 `json:"maxReferencedBytes,omitzero"`
	// SnapshotRule is the folder's snapshot schedule; nil is none. Its UUID
	// and Path are set by the server.
	SnapshotRule *btrfs.BtrfsSnapshotRule `json:"snapshotRule,omitzero"`
	Access       []SharedFolderAccess     `json:"access"`
	Everyone     string                   `json:"everyone"`
	SMB          bool                     `json:"smb"`
	NFS          []NFSClient              `json:"nfs"`
	// The Network tab (docs/shared-folders.md, "Network"): earlier versions
	// from the folder's snapshots, a recycle bin for what is deleted over SMB,
	// and Time Machine, with the size it may take (nil for no limit).
	PreviousVersions    bool    `json:"previousVersions"`
	RecycleBin          bool    `json:"recycleBin"`
	TimeMachine         bool    `json:"timeMachine"`
	TimeMachineMaxBytes *uint64 `json:"timeMachineMaxBytes,omitzero"`
}

// SharedFolderUpdateRequest replaces what can change on a shared folder. SMB
// off and no NFS client stop sharing it; the folder and its access stay.
type SharedFolderUpdateRequest struct {
	UUID     string               `json:"uuid"`
	Path     string               `json:"path"`
	Name     string               `json:"name"`
	Access   []SharedFolderAccess `json:"access"`
	Everyone string               `json:"everyone"`
	SMB      bool                 `json:"smb"`
	NFS      []NFSClient          `json:"nfs"`
	// The Network tab (docs/shared-folders.md, "Network"): earlier versions
	// from the folder's snapshots, a recycle bin for what is deleted over SMB,
	// and Time Machine, with the size it may take (nil for no limit).
	PreviousVersions    bool    `json:"previousVersions"`
	RecycleBin          bool    `json:"recycleBin"`
	TimeMachine         bool    `json:"timeMachine"`
	TimeMachineMaxBytes *uint64 `json:"timeMachineMaxBytes,omitzero"`
}

// SharedFolderDeleteRequest deletes a shared folder with its data and its
// snapshots. ConfirmName must be the folder's share name.
type SharedFolderDeleteRequest struct {
	UUID        string `json:"uuid"`
	Path        string `json:"path"`
	ConfirmName string `json:"confirmName"`
}

// SambaUser is a user in Samba's own user list, who can sign in to shared
// folders from Windows and a Mac.
type SambaUser struct {
	Username string `json:"username"`
	// PasswordStale says the system password was changed after the one Samba
	// holds, to the day: it was set outside LinuxIO.
	PasswordStale bool `json:"passwordStale"`
}

// SMBSettings is the settings of the SMB service as a whole.
type SMBSettings struct {
	// MinProtocol is the oldest protocol the server speaks: "SMB2" or
	// "SMB3". Read, it can be a value LinuxIO does not offer (NT1), as
	// smb.conf has it.
	MinProtocol string `json:"minProtocol"`
	// Encryption is "default" (for a client that asks), "desired" (asked of
	// every client), "required" or "off".
	Encryption string `json:"encryption"`
	// MacDiscovery has smbd announce the server to Macs through Avahi.
	MacDiscovery bool `json:"macDiscovery"`
	// WindowsDiscovery has the WS-Discovery service show the server in
	// Windows' Network.
	WindowsDiscovery bool `json:"windowsDiscovery"`
}
