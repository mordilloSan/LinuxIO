package shares

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const exportsFile = "/etc/exports"

const nfsServerInstallHint = "nfs-kernel-server or nfs-utils"

var nfsServerCommandFallbackDirs = []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"}

var (
	validExportPath = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
	exportLineRegex = regexp.MustCompile(`^(\S+)\s+(.+)$`)
	clientRegex     = regexp.MustCompile(`(\S+?)(\([^)]*\))?\s*`)
)

func findNFSServerCommand(command string) (string, error) {
	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}
	for _, dir := range nfsServerCommandFallbackDirs {
		path := filepath.Join(dir, command)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func CheckNFSServerAvailability() (bool, error) {
	_, err := findNFSServerCommand("exportfs")
	if err != nil {
		return false, fmt.Errorf("exportfs not found (install %s)", nfsServerInstallHint)
	}
	return true, nil
}

func requireNFSServerAvailability() error {
	ok, err := CheckNFSServerAvailability()
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
func ListNFSShares() ([]NFSExport, error) {
	exports, err := parseExportsFile()
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", exportsFile, err)
	}

	activeExports := getActiveExports()
	for i, export := range exports {
		normalized := strings.TrimRight(export.Path, "/")
		if activeExports[normalized] || activeExports[export.Path] {
			exports[i].Active = true
		}
	}

	return exports, nil
}

// CreateNFSShare adds a new export to /etc/exports and applies it
func CreateNFSShare(path string, clients []NFSClient) error {
	if err := requireNFSServerAvailability(); err != nil {
		return err
	}

	if !validExportPath.MatchString(path) {
		return fmt.Errorf("invalid export path: %s", path)
	}

	exports, err := parseExportsFile()
	if err != nil {
		return err
	}
	for _, e := range exports {
		if e.Path == path {
			return fmt.Errorf("export already exists for path: %s", path)
		}
	}

	if dirErr := os.MkdirAll(path, 0755); dirErr != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, dirErr)
	}

	line := formatExportLine(path, clients)

	f, err := os.OpenFile(exportsFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", exportsFile, err)
	}
	defer f.Close()

	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("failed to write to %s: %w", exportsFile, err)
	}
	slog.Info("NFS export added", "path", path, "count", len(clients))
	return applyNFSExports()
}

// UpdateNFSShare modifies an existing export's clients in /etc/exports
func UpdateNFSShare(path string, clients []NFSClient) error {
	if err := requireNFSServerAvailability(); err != nil {
		return err
	}

	if !validExportPath.MatchString(path) {
		return fmt.Errorf("invalid export path: %s", path)
	}

	content, err := os.ReadFile(exportsFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", exportsFile, err)
	}

	newLine := formatExportLine(path, clients)
	found := false
	var newLines []string

	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			newLines = append(newLines, line)
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 1 && fields[0] == path {
			newLines = append(newLines, newLine)
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	if !found {
		return fmt.Errorf("export not found for path: %s", path)
	}

	if err := os.WriteFile(exportsFile, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", exportsFile, err)
	}
	slog.Info("NFS export updated", "path", path, "count", len(clients))
	return applyNFSExports()
}

// DeleteNFSShare removes an export from /etc/exports and applies changes
func DeleteNFSShare(path string) error {
	if err := requireNFSServerAvailability(); err != nil {
		return err
	}

	content, err := os.ReadFile(exportsFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", exportsFile, err)
	}

	found := false
	var newLines []string

	for line := range strings.SplitSeq(string(content), "\n") {
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

	if !found {
		return fmt.Errorf("export not found for path: %s", path)
	}

	if err := os.WriteFile(exportsFile, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", exportsFile, err)
	}
	slog.Info("NFS export removed", "path", path)
	return applyNFSExports()
}

// parseExportsFile reads and parses /etc/exports
func parseExportsFile() ([]NFSExport, error) {
	file, err := os.Open(exportsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []NFSExport{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var exports []NFSExport
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
func parseExportLine(line string) (NFSExport, error) {
	matches := exportLineRegex.FindStringSubmatch(line)
	if matches == nil {
		return NFSExport{}, fmt.Errorf("invalid export line: %s", line)
	}

	export := NFSExport{Path: matches[1]}

	clientMatches := clientRegex.FindAllStringSubmatch(matches[2], -1)
	for _, cm := range clientMatches {
		host := cm[1]
		if host == "" {
			continue
		}
		client := NFSClient{Host: host}
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
func getActiveExports() map[string]bool {
	active := make(map[string]bool)

	exportfs, findErr := findNFSServerCommand("exportfs")
	if findErr != nil {
		slog.Debug("exportfs inspection skipped", "error", findErr)
		return active
	}

	output, err := exec.Command(exportfs, "-v").CombinedOutput()
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

	return active
}

// formatExportLine builds a /etc/exports line from path and clients
func formatExportLine(path string, clients []NFSClient) string {
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

// applyNFSExports runs exportfs -ra to apply changes
func applyNFSExports() error {
	exportfs, findErr := findNFSServerCommand("exportfs")
	if findErr != nil {
		return fmt.Errorf("exportfs not found (install %s)", nfsServerInstallHint)
	}

	out, err := exec.Command(exportfs, "-ra").CombinedOutput()
	if err != nil {
		return fmt.Errorf("exportfs -ra failed: %s", strings.TrimSpace(string(out)))
	}
	slog.Info("NFS exports applied successfully")
	return nil
}
