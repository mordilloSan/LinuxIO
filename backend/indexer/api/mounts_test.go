package api

import "testing"

func TestNetworkFilesystemTypes(t *testing.T) {
	network := []struct{ fsType, source string }{
		{"nfs4", "server:/export"},
		{"cifs", "//server/share"},
		{"fuse.sshfs", "user@host:/srv"},
		{"fuse.rclone", "remote:bucket"},
		{"fuse.s3fs", "bucket"},
		{"ceph", "10.0.0.1:6789:/"},
		{"glusterfs", "server:/volume"},
		{"fuse.glusterfs", "server:/volume"},
		{"9p", "hostshare"},
		{"davfs", "https://dav.example/files"},
		{"fuse", "https://dav.example/files"},
	}
	for _, mount := range network {
		if !ExternalFilesystem(mount.fsType, mount.source) {
			t.Errorf("%s from %s is not treated as a network mount", mount.fsType, mount.source)
		}
	}
	// Local disks reached through FUSE stay indexed.
	local := []struct{ fsType, source string }{
		{"ext4", "/dev/sda1"},
		{"btrfs", "/dev/nvme0n1p2"},
		{"fuseblk", "/dev/sdb1"},
		{"fuse.mergerfs", "/mnt/disk1:/mnt/disk2"},
		{"fuse.gocryptfs", "/home/user/.encrypted"},
	}
	for _, mount := range local {
		if ExternalFilesystem(mount.fsType, mount.source) {
			t.Errorf("%s from %s is treated as a network mount", mount.fsType, mount.source)
		}
	}
}
