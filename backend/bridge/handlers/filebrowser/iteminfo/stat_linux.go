// Package iteminfo provides file and resource information structures and utilities
package iteminfo

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

// CollectStatInfo gathers extended Linux-specific file metadata including owner, group, and permissions.
// This function collects detailed stat information for a file or directory.
func CollectStatInfo(realPath string) (*ResourceStatData, error) {
	root, err := fsroot.Open()
	if err != nil {
		return nil, err
	}
	defer root.Close()

	cleanPath := filepath.Clean("/" + strings.TrimPrefix(realPath, "/"))
	info, err := root.Root.Lstat(fsroot.ToRel(cleanPath))
	if err != nil {
		return nil, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return nil, fmt.Errorf("unsupported stat type for path: %s", cleanPath)
	}

	data := &ResourceStatData{
		Mode:     info.Mode().String(),
		Size:     info.Size(),
		Modified: info.ModTime().Format(time.RFC3339),
		RealPath: cleanPath,
		Name:     filepath.Base(cleanPath),
	}

	uid := strconv.FormatUint(uint64(stat.Uid), 10)
	if u, err := user.LookupId(uid); err == nil && u != nil {
		data.Owner = u.Username
	} else {
		data.Owner = uid
	}

	gid := strconv.FormatUint(uint64(stat.Gid), 10)
	if g, err := user.LookupGroupId(gid); err == nil && g != nil {
		data.Group = g.Name
	} else {
		data.Group = gid
	}

	data.Permissions = formatPermissionHuman(info.Mode())
	data.Raw = formatStatLine(data.Mode, data.Owner, data.Group, data.Size, info.ModTime(), cleanPath)

	return data, nil
}

func formatPermissionHuman(mode os.FileMode) string {
	segments := []struct {
		label string
		read  os.FileMode
		write os.FileMode
		exec  os.FileMode
	}{
		{"owner", 0o400, 0o200, 0o100},
		{"group", 0o040, 0o020, 0o010},
		{"others", 0o004, 0o002, 0o001},
	}

	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		abilities := []string{}
		if mode.Perm()&segment.read != 0 {
			abilities = append(abilities, "read")
		}
		if mode.Perm()&segment.write != 0 {
			abilities = append(abilities, "write")
		}
		if mode.Perm()&segment.exec != 0 {
			abilities = append(abilities, "execute")
		}
		if len(abilities) == 0 {
			abilities = append(abilities, "none")
		}
		parts = append(parts, fmt.Sprintf("%s: %s", segment.label, strings.Join(abilities, ", ")))
	}
	return strings.Join(parts, " | ")
}

func formatStatLine(mode, owner, group string, size int64, modTime time.Time, path string) string {
	timestamp := modTime.Format("2006-01-02 15:04:05.000000000 -0700")
	components := []string{
		strings.TrimSpace(mode),
		strings.TrimSpace(owner),
		strings.TrimSpace(group),
		fmt.Sprintf("%d", size),
		timestamp,
		path,
	}
	return strings.Join(components, " ")
}
