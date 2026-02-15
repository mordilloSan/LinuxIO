package system

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Manager string

const (
	MgrAPT    Manager = "apt"
	MgrDNF    Manager = "dnf"
	MgrYUM    Manager = "yum"
	MgrZypper Manager = "zypper"
	MgrPacman Manager = "pacman"
	MgrAPK    Manager = "apk"
)

type UpdateItem struct {
	Name           string `json:"name"`
	NewVersion     string `json:"newVersion,omitempty"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	Arch           string `json:"arch,omitempty"`
	Repo           string `json:"repo,omitempty"`
}

type UpdatesFastResponse struct {
	Updates []UpdateItem `json:"updates,omitempty"`
}

func GetUpdatesFast() (*UpdatesFastResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	mgr, cmd, args := pickCommand()
	out, _ := runCmd(ctx, cmd, args...)
	resp := &UpdatesFastResponse{}
	switch mgr {
	case MgrAPT:
		resp.Updates = parseAptListUpgradeable(out)
	case MgrDNF, MgrYUM:
		resp.Updates = parseDnfYum(out)
	case MgrZypper:
		resp.Updates = parseZypper(out)
	case MgrPacman:
		resp.Updates = parsePacmanSup(out)
	case MgrAPK:
		resp.Updates = parseAPK(out)
	default:
		resp.Updates = splitAsRawItems(out)
	}
	return resp, nil
}

func seen(bin string) bool {
	if strings.TrimSpace(bin) == "" {
		return false
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

func pickCommand() (Manager, string, []string) {
	switch {
	case seen("apt"):
		return MgrAPT, "apt", []string{"list", "--upgradable"}
	case seen("dnf"):
		return MgrDNF, "dnf", []string{"check-update", "-q", "--refresh"}
	case seen("yum"):
		return MgrYUM, "yum", []string{"check-update", "-q"}
	case seen("zypper"):
		return MgrZypper, "zypper", []string{"-q", "lu", "-s"}
	case seen("pacman"):
		return MgrPacman, "pacman", []string{"-Sup", "--noconfirm"}
	case seen("apk"):
		return MgrAPK, "apk", []string{"list", "-u"}
	default:
		return "", "apt", []string{"list", "--upgradable"}
	}
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if name == "pacman" {
		cmd.Stderr = io.Discard
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var aptLine = regexp.MustCompile(`^([^\s/]+)/([^\s]+)\s+(\S+)\s+(\S+)\s+\[upgradable from:\s*([^\]]+)\]`)

func parseAptListUpgradeable(out string) []UpdateItem {
	var updates []UpdateItem
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, "[upgradable from:") {
			continue
		}
		if m := aptLine.FindStringSubmatch(line); len(m) == 6 {
			updates = append(updates, UpdateItem{Name: m[1], Repo: m[2], NewVersion: m[3], Arch: m[4], CurrentVersion: m[5]})
		}
	}
	return updates
}

var dnfYumLine = regexp.MustCompile(`^(\S+)\.(\S+)\s+(\S+)\s+(\S+)$`)

func parseDnfYum(out string) []UpdateItem {
	var items []UpdateItem
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(strings.ToLower(line), "last metadata") ||
			strings.HasPrefix(strings.ToLower(line), "obsoleting packages") ||
			strings.HasPrefix(strings.ToLower(line), "available packages") ||
			strings.HasPrefix(strings.ToLower(line), "security") ||
			strings.HasPrefix(line, "Loaded plugins") {
			continue
		}
		if m := dnfYumLine.FindStringSubmatch(line); len(m) == 5 {
			items = append(items, UpdateItem{Name: m[1], Arch: m[2], NewVersion: m[3], Repo: m[4]})
		}
	}
	return items
}

func parseZypper(out string) []UpdateItem {
	var items []UpdateItem
	sc := bufio.NewScanner(strings.NewReader(out))
	header := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "S |") {
			header = true
			continue
		}
		if !header || strings.HasPrefix(line, "--") {
			continue
		}
		cols := splitPipeCols(line)
		if len(cols) >= 6 {
			items = append(items, UpdateItem{Name: cols[2], CurrentVersion: cols[3], NewVersion: cols[4], Arch: cols[5], Repo: cols[1]})
		}
	}
	return items
}

func splitPipeCols(line string) []string {
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parsePacmanSup(out string) []UpdateItem {
	var items []UpdateItem
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !strings.Contains(line, ".pkg.tar.") {
			continue
		}
		base := filepath.Base(line)
		base = strings.TrimSuffix(base, ".sig")
		for _, suf := range []string{".pkg.tar.zst", ".pkg.tar.xz", ".pkg.tar.gz", ".pkg.tar"} {
			if before, ok := strings.CutSuffix(base, suf); ok {
				base = before
				break
			}
		}
		parts := strings.Split(base, "-")
		if len(parts) >= 3 {
			name := strings.Join(parts[:len(parts)-2], "-")
			ver := parts[len(parts)-2]
			arch := parts[len(parts)-1]
			items = append(items, UpdateItem{Name: name, NewVersion: ver, Arch: arch})
		}
	}
	return items
}

var apkLine = regexp.MustCompile(`^(\S+)-([^-]+)\s+\S+\s+\{[^}]*\}\s+\(installed:\s*([^)]+)\)`)

func parseAPK(out string) []UpdateItem {
	var items []UpdateItem
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := apkLine.FindStringSubmatch(line); len(m) == 4 {
			items = append(items, UpdateItem{Name: m[1], NewVersion: m[2], CurrentVersion: m[3]})
		}
	}
	return items
}

func splitAsRawItems(out string) []UpdateItem {
	var items []UpdateItem
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		items = append(items, UpdateItem{Name: line})
	}
	return items
}
