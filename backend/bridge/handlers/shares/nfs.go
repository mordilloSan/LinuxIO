package shares

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mordilloSan/go-logger/logger"
)

const (
	exportsFile    = "/etc/exports"
	nfsdClientsDir = "/proc/fs/nfsd/clients"
)

var (
	validExportPath = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
	exportLineRegex = regexp.MustCompile(`^(\S+)\s+(.+)$`)
	clientRegex     = regexp.MustCompile(`(\S+?)(\([^)]*\))?\s*`)
)

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

	logger.Infof("Added NFS export: %s", line)
	return applyNFSExports()
}

// UpdateNFSShare modifies an existing export's clients in /etc/exports
func UpdateNFSShare(path string, clients []NFSClient) error {
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

	logger.Infof("Updated NFS export: %s", newLine)
	return applyNFSExports()
}

// DeleteNFSShare removes an export from /etc/exports and applies changes
func DeleteNFSShare(path string) error {
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

	logger.Infof("Removed NFS export for path: %s", path)
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
			logger.Warnf("Skipping malformed exports line: %s", line)
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

	output, err := exec.Command("exportfs", "-v").CombinedOutput()
	if err != nil {
		logger.Debugf("exportfs -v failed: %v, output: %s", err, strings.TrimSpace(string(output)))
		return active
	}
	logger.Debugf("exportfs -v output: %s", strings.TrimSpace(string(output)))

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

// GetNFSClients returns server-level NFS clients.
// It prefers the kernel NFSv4 client table and augments it with live port 2049
// TCP sessions so older clients still appear when lease metadata is unavailable.
func GetNFSClients() ([]NFSConnectedClient, error) {
	merged := make(map[string]NFSConnectedClient)

	kernelClients, err := getKernelNFSClients()
	if err != nil {
		logger.Debugf("kernel NFS client scan failed: %v", err)
	} else {
		mergeNFSClients(merged, kernelClients)
	}

	socketClients, err := getSocketNFSClients()
	if err != nil {
		logger.Debugf("ss failed: %v", err)
	} else {
		mergeNFSClients(merged, socketClients)
	}

	if len(merged) == 0 {
		return []NFSConnectedClient{}, nil
	}

	clients := make([]NFSConnectedClient, 0, len(merged))
	for _, client := range merged {
		clients = append(clients, client)
	}
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].IP != clients[j].IP {
			return clients[i].IP < clients[j].IP
		}
		return clients[i].Name < clients[j].Name
	})

	return clients, nil
}

func getKernelNFSClients() ([]NFSConnectedClient, error) {
	entries, err := os.ReadDir(nfsdClientsDir)
	if err != nil {
		return nil, err
	}

	clients := make([]NFSConnectedClient, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		infoPath := filepath.Join(nfsdClientsDir, entry.Name(), "info")
		data, err := os.ReadFile(infoPath)
		if err != nil {
			logger.Debugf("skipping nfsd client %s: %v", entry.Name(), err)
			continue
		}

		client, err := parseKernelNFSClientInfo(string(data))
		if err != nil {
			logger.Debugf("skipping nfsd client %s: %v", entry.Name(), err)
			continue
		}

		clients = append(clients, client)
	}

	return clients, nil
}

func parseKernelNFSClientInfo(raw string) (NFSConnectedClient, error) {
	client := NFSConnectedClient{}

	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "address":
			client.IP = normalizeNFSClientAddress(value)
		case "name":
			client.Name = trimQuotedValue(value)
		case "status":
			client.Status = trimQuotedValue(value)
		case "seconds from last renew":
			if seconds, err := strconv.Atoi(value); err == nil {
				client.SecondsFromLastRenew = seconds
			}
		case "minor version":
			if minorVersion, err := strconv.Atoi(value); err == nil {
				client.MinorVersion = minorVersion
			}
		}
	}

	if client.IP == "" {
		return NFSConnectedClient{}, fmt.Errorf("missing client address")
	}
	if client.Status == "" {
		client.Status = "lease-active"
	}

	return client, nil
}

func getSocketNFSClients() ([]NFSConnectedClient, error) {
	output, err := exec.Command("ss", "-tn", "state", "established", "( sport = :2049 )").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w, output: %s", err, strings.TrimSpace(string(output)))
	}

	var clients []NFSConnectedClient
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Recv-Q") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		ip := normalizeNFSClientAddress(fields[3])
		if ip != "" && !seen[ip] {
			seen[ip] = true
			clients = append(clients, NFSConnectedClient{
				IP:     ip,
				Status: "established",
			})
		}
	}

	return clients, nil
}

func mergeNFSClients(dst map[string]NFSConnectedClient, clients []NFSConnectedClient) {
	for _, client := range clients {
		if client.IP == "" {
			continue
		}

		existing, ok := dst[client.IP]
		if !ok {
			dst[client.IP] = client
			continue
		}

		if existing.Name == "" && client.Name != "" {
			existing.Name = client.Name
		}
		if client.Status != "" && (existing.Status == "" || existing.Status == "established") {
			existing.Status = client.Status
		}
		if client.SecondsFromLastRenew > 0 && (existing.SecondsFromLastRenew == 0 || client.SecondsFromLastRenew < existing.SecondsFromLastRenew) {
			existing.SecondsFromLastRenew = client.SecondsFromLastRenew
		}
		if client.MinorVersion > existing.MinorVersion {
			existing.MinorVersion = client.MinorVersion
		}

		dst[client.IP] = existing
	}
}

func normalizeNFSClientAddress(value string) string {
	address := trimQuotedValue(value)
	if address == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		return strings.Trim(host, "[]")
	}

	return strings.Trim(address, "[]")
}

func trimQuotedValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

// applyNFSExports runs exportfs -ra to apply changes
func applyNFSExports() error {
	out, err := exec.Command("exportfs", "-ra").CombinedOutput()
	if err != nil {
		return fmt.Errorf("exportfs -ra failed: %s", strings.TrimSpace(string(out)))
	}
	logger.Infof("NFS exports applied successfully")
	return nil
}
