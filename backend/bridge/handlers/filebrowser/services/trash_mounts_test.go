package services

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// What the Trash takes on disk is asked for from every folder the file
// browser shows, so the look for trashes stays off a mount that may not
// answer (a network filesystem) and off an automount point nothing is
// mounted on, which looking would mount.
func TestLocalMountPointsLeaveOutNetworkMountsAndIdleAutomounts(t *testing.T) {
	mountinfo := strings.Join([]string{
		`22 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw`,
		`40 22 0:35 / /mnt/nas rw,relatime shared:20 - nfs4 nas:/export rw`,
		`41 22 0:36 / /mnt/share rw,relatime shared:21 - cifs //nas/share rw`,
		`42 22 0:37 / /mnt/idle rw,relatime shared:22 - autofs systemd-1 rw`,
		// An automount point with its disk mounted on it: the disk decides.
		`43 22 0:38 / /mnt/disk rw,relatime shared:23 - autofs systemd-1 rw`,
		`44 43 8:17 / /mnt/disk rw,relatime shared:24 - ext4 /dev/sdb1 rw`,
		// And one with a network share mounted on it.
		`45 22 0:39 / /mnt/net rw,relatime shared:25 - autofs /etc/auto.net rw`,
		`46 45 0:40 / /mnt/net rw,relatime shared:26 - nfs nas:/net rw`,
		`47 22 0:41 / /mnt/with\040space rw,relatime - btrfs /dev/sdc rw`,
		`not a mountinfo line`,
	}, "\n")

	require.Equal(t, []string{"/", "/mnt/disk", "/mnt/with space"}, localMountPoints(mountinfo))
}
