package api

import "strings"

// externalFSTypes are the network filesystems. A local disk reached through
// FUSE (fuseblk for NTFS, fuse.mergerfs, fuse.gocryptfs) is not one of them.
var externalFSTypes = map[string]struct{}{
	"nfs":            {},
	"nfs4":           {},
	"cifs":           {},
	"smbfs":          {},
	"smb2":           {},
	"smb3":           {},
	"ceph":           {},
	"glusterfs":      {},
	"9p":             {},
	"davfs":          {},
	"fuse.cifs":      {},
	"fuse.smb":       {},
	"fuse.smb3":      {},
	"fuse.nfs":       {},
	"fuse.ceph":      {},
	"fuse.iscsi":     {},
	"fuse.sshfs":     {},
	"fuse.rclone":    {},
	"fuse.s3fs":      {},
	"fuse.glusterfs": {},
}

// ExternalFilesystem reports whether a mount of this type from this source,
// both in lower case as /proc/self/mountinfo names them, is a network
// filesystem or an iSCSI one: the index leaves those out unless it is told to
// include network mounts, and whatever looks at every mount on its own
// account leaves them alone, since one that does not answer blocks the caller.
func ExternalFilesystem(fsType, source string) bool {
	if _, ok := externalFSTypes[fsType]; ok {
		return true
	}
	if strings.Contains(fsType, "iscsi") || strings.Contains(source, "iscsi") {
		return true
	}
	// An SMB share, or a WebDAV one, which davfs2 mounts as plain "fuse".
	if strings.HasPrefix(source, "//") || strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return true
	}
	return false
}
