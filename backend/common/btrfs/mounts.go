package btrfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// TopLevelDir is where TopLevel mounts filesystems that have no top-level mount.
var TopLevelDir = "/run/linuxio/btrfs"

// topLevelSubvolID is the id of the btrfs top-level subvolume.
const topLevelSubvolID = 5

// Mount is one mounted btrfs subvolume.
type Mount struct {
	Mountpoint string   `json:"mountpoint"`
	Device     string   `json:"device"`
	UUID       string   `json:"uuid"`
	Subvol     string   `json:"subvol"` // path without leading slash; "" for the top level
	SubvolID   int      `json:"subvolId"`
	Options    []string `json:"options"`
	// SubPath is the filesystem path the mount shows, from findmnt's source
	// brackets without the leading slash: Subvol for a subvolume mount, a
	// path inside it (@/www) for a bind mount, "" when none is printed.
	SubPath string `json:"subPath,omitempty"`
}

// listMounts is a variable so tests can stub mount discovery.
var listMounts = ListMounts

// ListMounts returns the mounted btrfs filesystems reported by findmnt.
func ListMounts(ctx context.Context) ([]Mount, error) {
	out, err := runCommand(ctx, "findmnt", "--json", "--list", "-t", "btrfs", "-o", "TARGET,SOURCE,UUID,OPTIONS")
	if err != nil {
		// findmnt exits 1 with no output when nothing matches.
		var exitErr *exec.ExitError
		if out == "" && errors.As(err, &exitErr) {
			return nil, nil
		}
		return nil, err
	}
	return parseFindmnt([]byte(out))
}

type findmntEntry struct {
	Target   string         `json:"target"`
	Source   string         `json:"source"`
	UUID     string         `json:"uuid"`
	Options  string         `json:"options"`
	Children []findmntEntry `json:"children"`
}

// parseFindmnt accepts flat or tree findmnt JSON; tree children are flattened.
func parseFindmnt(data []byte) ([]Mount, error) {
	var doc struct {
		Filesystems []findmntEntry `json:"filesystems"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse findmnt output: %w", err)
	}
	return appendMounts(make([]Mount, 0, len(doc.Filesystems)), doc.Filesystems), nil
}

func appendMounts(mounts []Mount, entries []findmntEntry) []Mount {
	for _, fs := range entries {
		device, bracket, _ := strings.Cut(fs.Source, "[")
		m := Mount{Mountpoint: fs.Target, Device: device, UUID: fs.UUID, Options: strings.Split(fs.Options, ","),
			SubPath: strings.TrimPrefix(strings.TrimSuffix(bracket, "]"), "/")}
		for _, opt := range m.Options {
			switch key, val, _ := strings.Cut(opt, "="); key {
			case "subvol":
				m.Subvol = strings.TrimPrefix(val, "/")
			case "subvolid":
				m.SubvolID, _ = strconv.Atoi(val)
			}
		}
		mounts = append(mounts, m)
		mounts = appendMounts(mounts, fs.Children)
	}
	return mounts
}

// TopLevel returns a mountpoint of the filesystem's top-level subvolume,
// mounting it under TopLevelDir when no such mount exists.
func TopLevel(ctx context.Context, uuid string) (string, error) {
	if uuid == "" || !filepath.IsLocal(uuid) || strings.ContainsRune(uuid, filepath.Separator) {
		return "", fmt.Errorf("invalid filesystem uuid %q", uuid)
	}
	mounts, err := listMounts(ctx)
	if err != nil {
		return "", err
	}
	for _, m := range mounts {
		if m.UUID == uuid && m.SubvolID == topLevelSubvolID {
			return m.Mountpoint, nil
		}
	}
	dir := filepath.Join(TopLevelDir, uuid)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create top-level mount dir: %w", err)
	}
	if _, err := runCommand(ctx, "mount", "-t", "btrfs", "-o", "subvolid=5,noatime", "UUID="+uuid, dir); err != nil {
		return "", err
	}
	return dir, nil
}
