package shares

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// exportsFile is a var so tests can point it at a temporary location;
// production always uses the standard exports path.
var exportsFile = "/etc/exports"

const nfsServerInstallHint = "nfs-kernel-server or nfs-utils"

var (
	validExportPath = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
	exportLineRegex = regexp.MustCompile(`^(\S+)\s+(.+)$`)
	clientRegex     = regexp.MustCompile(`(\S+?)(\([^)]*\))?\s*`)
	validNFSHost    = regexp.MustCompile(`^[A-Za-z0-9_.:*/@\[\]-]+$`)
	validNFSOption  = regexp.MustCompile(`^[a-z_]+(=[0-9A-Za-z_]+)?$`)

	// nfsServerAvailable and applyNFSExports are overridable in tests.
	nfsServerAvailable = CheckNFSServerAvailability
	applyNFSExports    = runExportfsReload
)

func CheckNFSServerAvailability() (bool, error) {
	_, err := hostcmd.LookPath("exportfs")
	if err != nil {
		return false, fmt.Errorf("exportfs not found (install %s)", nfsServerInstallHint)
	}
	return true, nil
}

func requireNFSServerAvailability() error {
	ok, err := nfsServerAvailable()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("NFS server utilities are unavailable")
	}
	return nil
}

// ListNFSShares reads /etc/exports and returns all configured exports
// with their active status from exportfs -v
func ListNFSShares(ctx context.Context) ([]apischema.NFSExport, error) {
	exports, err := parseExportsFile()
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", exportsFile, err)
	}

	activeExports := getActiveExports(ctx)
	for i, export := range exports {
		normalized := strings.TrimRight(export.Path, "/")
		if activeExports[normalized] || activeExports[export.Path] {
			exports[i].Active = true
		}
	}

	return exports, nil
}

// CreateNFSShare adds a new export to /etc/exports and applies it
func CreateNFSShare(ctx context.Context, path string, clients []apischema.NFSClient) error {
	if err := requireNFSServerAvailability(); err != nil {
		return err
	}

	path, err := validateNFSExport(path, clients)
	if err != nil {
		return err
	}

	exports, err := parseExportsFile()
	if err != nil {
		return err
	}
	for _, e := range exports {
		if filepath.Clean(e.Path) == path {
			return fmt.Errorf("export already exists for path: %s", path)
		}
	}

	if dirErr := mkdirShareDir(path); dirErr != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, dirErr)
	}

	if err := appendToConfFile(ctx, exportsFile, formatExportLine(path, clients)+"\n"); err != nil {
		return err
	}
	slog.Info("NFS export added", "path", path, "count", len(clients))
	return applyNFSExports(ctx)
}

// UpdateNFSShare modifies an existing export's clients in /etc/exports
func UpdateNFSShare(ctx context.Context, path string, clients []apischema.NFSClient) error {
	if err := requireNFSServerAvailability(); err != nil {
		return err
	}

	path, err := validateNFSExport(path, clients)
	if err != nil {
		return err
	}

	if err := rewriteExports(ctx, path, func(content string) ([]string, bool) {
		newLine := formatExportLine(path, clients)
		found := false
		var newLines []string
		for line := range strings.SplitSeq(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				newLines = append(newLines, line)
				continue
			}
			fields := strings.Fields(trimmed)
			if len(fields) >= 1 && filepath.Clean(fields[0]) == path {
				newLines = append(newLines, newLine)
				found = true
			} else {
				newLines = append(newLines, line)
			}
		}
		return newLines, found
	}); err != nil {
		return err
	}
	slog.Info("NFS export updated", "path", path, "count", len(clients))
	return applyNFSExports(ctx)
}

// DeleteNFSShare removes an export from /etc/exports and applies changes
func DeleteNFSShare(ctx context.Context, path string) error {
	if err := requireNFSServerAvailability(); err != nil {
		return err
	}

	if err := rewriteExports(ctx, path, func(content string) ([]string, bool) {
		found := false
		var newLines []string
		for line := range strings.SplitSeq(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				fields := strings.Fields(trimmed)
				if len(fields) >= 1 && fields[0] == path {
					found = true
					continue
				}
			}
			newLines = append(newLines, line)
		}
		return newLines, found
	}); err != nil {
		return err
	}
	slog.Info("NFS export removed", "path", path)
	return applyNFSExports(ctx)
}

// parseExportsFile reads and parses /etc/exports
func parseExportsFile() ([]apischema.NFSExport, error) {
	file, err := os.Open(exportsFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []apischema.NFSExport{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var exports []apischema.NFSExport
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		export, err := parseExportLine(line)
		if err != nil {
			slog.Warn("skipping malformed exports line", "path", exportsFile, "line", line)
			continue
		}
		exports = append(exports, export)
	}

	return exports, scanner.Err()
}

// parseExportLine parses a single line from /etc/exports
// Format: /path client1(opts) client2(opts)
func parseExportLine(line string) (apischema.NFSExport, error) {
	matches := exportLineRegex.FindStringSubmatch(line)
	if matches == nil {
		return apischema.NFSExport{}, fmt.Errorf("invalid export line: %s", line)
	}

	export := apischema.NFSExport{Path: matches[1]}

	clientMatches := clientRegex.FindAllStringSubmatch(matches[2], -1)
	for _, cm := range clientMatches {
		host := cm[1]
		if host == "" {
			continue
		}
		client := apischema.NFSClient{Host: host}
		if len(cm) > 2 && cm[2] != "" {
			opts := strings.Trim(cm[2], "()")
			if opts != "" {
				client.Options = strings.Split(opts, ",")
			}
		}
		export.Clients = append(export.Clients, client)
	}

	return export, nil
}

// getActiveExports returns paths currently exported via exportfs -v
func getActiveExports(ctx context.Context) map[string]bool {
	active := make(map[string]bool)

	exportfs, findErr := hostcmd.LookPath("exportfs")
	if findErr != nil {
		slog.Debug("exportfs inspection skipped", "error", findErr)
		return active
	}

	output, err := exec.CommandContext(ctx, exportfs, "-v").CombinedOutput()
	if err != nil {
		slog.Debug("exportfs inspection failed",
			"command", "exportfs -v",
			"error", err,
			"output", strings.TrimSpace(string(output)))
		return active
	}
	slog.Debug("exportfs inspection completed",
		"command", "exportfs -v",
		"output", strings.TrimSpace(string(output)))

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			p := strings.TrimRight(fields[0], "/")
			active[p] = true
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("exportfs output scan error", "error", err)
	}

	return active
}

// validateNFSExport cleans the export path and checks the client list, since
// every value ends up verbatim in /etc/exports. It returns the cleaned path.
func validateNFSExport(path string, clients []apischema.NFSClient) (string, error) {
	if !validExportPath.MatchString(path) {
		return "", fmt.Errorf("invalid export path: %s", path)
	}
	path = filepath.Clean(path)
	if path == "/" {
		return "", fmt.Errorf("invalid export path: %s", path)
	}
	if len(clients) == 0 {
		return "", errors.New("export must have at least one client")
	}
	for _, c := range clients {
		if !validNFSHost.MatchString(c.Host) {
			return "", fmt.Errorf("invalid client host: %q", c.Host)
		}
		for _, o := range c.Options {
			if !validNFSOption.MatchString(o) {
				return "", fmt.Errorf("invalid export option: %q", o)
			}
		}
	}
	return path, nil
}

// mkdirShareDir creates dir and any missing parents with mode 0755 despite the
// bridge's 0o077 umask so non-root clients can enter them. Existing
// directories keep their mode.
func mkdirShareDir(dir string) error {
	var created []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, statErr := os.Stat(d); !errors.Is(statErr, fs.ErrNotExist) {
			break
		}
		created = append(created, d)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, d := range created {
		if err := os.Chmod(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// confLockFile serialises every read-modify-write of smb.conf and
// /etc/exports; a var so tests can use a temp path.
var confLockFile = "/run/lock/linuxio-shares.lock"

func withConfLock(ctx context.Context, fn func() error) error {
	return filelock.RunExclusive(ctx, confLockFile, fn, filelock.WithTimeout(10*time.Second))
}

// rewriteExports applies edit to /etc/exports under the shares lock.
func rewriteExports(ctx context.Context, path string, edit func(content string) ([]string, bool)) error {
	return withConfLock(ctx, func() error {
		content, err := os.ReadFile(exportsFile)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", exportsFile, err)
		}
		newLines, found := edit(string(content))
		if !found {
			return fmt.Errorf("export not found for path: %s", path)
		}
		if writeErr := atomicfile.Write(exportsFile, []byte(strings.Join(newLines, "\n")), 0o644); writeErr != nil {
			return fmt.Errorf("failed to write %s: %w", exportsFile, writeErr)
		}
		return nil
	})
}

// appendToConfFile appends text to path atomically under the shares lock,
// starting on a fresh line when the existing content does not end with one.
func appendToConfFile(ctx context.Context, path, text string) error {
	return withConfLock(ctx, func() error {
		content, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
		if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
			content = append(content, '\n')
		}
		if writeErr := atomicfile.Write(path, append(content, text...), 0o644); writeErr != nil {
			return fmt.Errorf("failed to write %s: %w", path, writeErr)
		}
		return nil
	})
}

// formatExportLine builds a /etc/exports line from path and clients
func formatExportLine(path string, clients []apischema.NFSClient) string {
	var parts []string
	for _, c := range clients {
		if len(c.Options) > 0 {
			parts = append(parts, fmt.Sprintf("%s(%s)", c.Host, strings.Join(c.Options, ",")))
		} else {
			parts = append(parts, c.Host)
		}
	}
	return fmt.Sprintf("%s\t%s", path, strings.Join(parts, " "))
}

// runExportfsReload runs exportfs -ra to apply changes
func runExportfsReload(ctx context.Context) error {
	exportfs, findErr := hostcmd.LookPath("exportfs")
	if findErr != nil {
		return fmt.Errorf("exportfs not found (install %s)", nfsServerInstallHint)
	}

	out, err := exec.CommandContext(ctx, exportfs, "-ra").CombinedOutput()
	if err != nil {
		return fmt.Errorf("exportfs -ra failed: %s", strings.TrimSpace(string(out)))
	}
	slog.Debug("NFS exports applied successfully")
	return nil
}
