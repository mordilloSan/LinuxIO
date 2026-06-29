package shares

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
)

const sambaServerInstallHint = "samba"

// smbConfFile is a var so tests can point it at a temporary location;
// production always uses the standard Samba path.
var smbConfFile = "/etc/samba/smb.conf"

// reservedSections are built-in smb.conf sections, not user file shares
var reservedSections = map[string]bool{
	"global":   true,
	"homes":    true,
	"printers": true,
	"print$":   true,
}

var (
	sectionRegex     = regexp.MustCompile(`^\[([^\]]+)\]$`)
	keyValueRegex    = regexp.MustCompile(`^\s*([^=]+?)\s*=\s*(.*)$`)
	validShareName   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _.-]*$`)
	smbcontrolReload = func(ctx context.Context) ([]byte, error) {
		return exec.CommandContext(ctx, "smbcontrol", "all", "reload-config").CombinedOutput()
	}
	systemdReloadUnit = systemd.ReloadUnit
	// sambaServerAvailable is the availability gate used by the create/update/
	// delete preflights; overridable in tests. The capability registry calls
	// CheckSambaServerAvailability directly.
	sambaServerAvailable = CheckSambaServerAvailability
)

// CheckSambaServerAvailability reports whether the Samba server (smbd) is
// installed. It backs both the share-management preflight and the system
// capability registry entry for Samba.
func CheckSambaServerAvailability() (bool, error) {
	if _, err := findServerCommand("smbd"); err != nil {
		return false, fmt.Errorf("smbd not found (install %s)", sambaServerInstallHint)
	}
	return true, nil
}

func requireSambaServerAvailability() error {
	ok, err := sambaServerAvailable()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("samba server is unavailable")
	}
	return nil
}

func ensureSmbConfDir() error {
	dir := filepath.Dir(smbConfFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	return nil
}

// ListSambaShares reads smb.conf and returns all user-defined shares
func ListSambaShares(ctx context.Context) ([]apischema.SambaShare, error) {
	sections, err := parseSmbConf()
	if err != nil {
		return nil, err
	}

	var shares []apischema.SambaShare
	for name, props := range sections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if reservedSections[strings.ToLower(name)] {
			continue
		}
		shares = append(shares, apischema.SambaShare{
			Name:       name,
			Properties: props,
		})
	}

	sort.Slice(shares, func(i, j int) bool {
		return shares[i].Name < shares[j].Name
	})

	return shares, nil
}

// CreateSambaShare adds a new share section to smb.conf and reloads samba
func CreateSambaShare(ctx context.Context, name string, properties map[string]string) error {
	if !validShareName.MatchString(name) {
		return fmt.Errorf("invalid share name: %s", name)
	}
	if reservedSections[strings.ToLower(name)] {
		return fmt.Errorf("cannot use reserved section name: %s", name)
	}
	path, ok := properties["path"]
	if !ok || path == "" {
		return fmt.Errorf("share must have a 'path' property")
	}
	if !validExportPath.MatchString(path) {
		return fmt.Errorf("invalid share path: %s", path)
	}

	// Preflight before touching the filesystem so an uninstalled Samba returns a
	// clear error and never leaves an orphan share directory behind.
	if err := requireSambaServerAvailability(); err != nil {
		return err
	}

	sections, err := parseSmbConf()
	if err != nil {
		return err
	}
	for existing := range sections {
		if strings.EqualFold(existing, name) {
			return fmt.Errorf("share already exists: %s", existing)
		}
	}

	if dirErr := ensureSmbConfDir(); dirErr != nil {
		return dirErr
	}

	if dirErr := os.MkdirAll(path, 0755); dirErr != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, dirErr)
	}

	block := formatSambaSection(name, properties)

	f, err := os.OpenFile(smbConfFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", smbConfFile, err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + block); err != nil {
		return fmt.Errorf("failed to write to %s: %w", smbConfFile, err)
	}
	slog.Info("Samba share created", "name", name, "path", path)
	return reloadSamba(ctx)
}

// UpdateSambaShare replaces a share section in smb.conf and reloads samba.
// oldName identifies the existing section; newName is the section name to write.
func UpdateSambaShare(ctx context.Context, oldName, newName string, properties map[string]string) error {
	if !validShareName.MatchString(newName) {
		return fmt.Errorf("invalid share name: %s", newName)
	}
	if reservedSections[strings.ToLower(newName)] {
		return fmt.Errorf("cannot use reserved section name: %s", newName)
	}
	path, ok := properties["path"]
	if !ok || path == "" {
		return fmt.Errorf("share must have a 'path' property")
	}
	if !validExportPath.MatchString(path) {
		return fmt.Errorf("invalid share path: %s", path)
	}

	if err := requireSambaServerAvailability(); err != nil {
		return err
	}

	if !strings.EqualFold(oldName, newName) {
		sections, err := parseSmbConf()
		if err != nil {
			return err
		}
		for existing := range sections {
			if strings.EqualFold(existing, newName) {
				return fmt.Errorf("share already exists: %s", existing)
			}
		}
	}

	content, err := os.ReadFile(smbConfFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", smbConfFile, err)
	}

	newContent, found := replaceSmbSection(
		string(content),
		oldName,
		newName,
		properties,
	)
	if !found {
		return fmt.Errorf("share not found: %s", oldName)
	}

	if err := os.WriteFile(smbConfFile, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", smbConfFile, err)
	}
	slog.Info("Samba share updated", "name", oldName, "new_name", newName, "path", path)
	return reloadSamba(ctx)
}

// DeleteSambaShare removes a share section from smb.conf and reloads samba
func DeleteSambaShare(ctx context.Context, name string) error {
	if err := requireSambaServerAvailability(); err != nil {
		return err
	}

	content, err := os.ReadFile(smbConfFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", smbConfFile, err)
	}

	newContent, found := removeSmbSection(string(content), name)
	if !found {
		return fmt.Errorf("share not found: %s", name)
	}

	if err := os.WriteFile(smbConfFile, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", smbConfFile, err)
	}
	slog.Info("Samba share deleted", "name", name)
	return reloadSamba(ctx)
}

// parseSmbConf parses smb.conf into a map of section name -> key/value properties
func parseSmbConf() (map[string]map[string]string, error) {
	file, err := os.Open(smbConfFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to open %s: %w", smbConfFile, err)
	}
	defer file.Close()

	sections := make(map[string]map[string]string)
	var currentSection string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if matches := sectionRegex.FindStringSubmatch(line); matches != nil {
			currentSection = matches[1]
			if _, exists := sections[currentSection]; !exists {
				sections[currentSection] = make(map[string]string)
			}
			continue
		}

		if currentSection != "" {
			if matches := keyValueRegex.FindStringSubmatch(line); matches != nil {
				sections[currentSection][strings.TrimSpace(matches[1])] = strings.TrimSpace(matches[2])
			}
		}
	}

	return sections, scanner.Err()
}

// formatSambaSection builds a smb.conf section block string
func formatSambaSection(name string, properties map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\n", name)

	// Write path first for readability
	if path, ok := properties["path"]; ok {
		fmt.Fprintf(&b, "   path = %s\n", path)
	}

	var keys []string
	for k := range properties {
		if k != "path" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(&b, "   %s = %s\n", k, properties[k])
	}

	return b.String()
}

// replaceSmbSection replaces a named section in smb.conf content with new properties.
// Lines belonging to the old section (between its header and the next [section]) are removed
// and the new definition is written in place.
func replaceSmbSection(content, oldName, newName string, properties map[string]string) (string, bool) {
	lines := strings.Split(content, "\n")
	var result []string
	found := false
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if matches := sectionRegex.FindStringSubmatch(trimmed); matches != nil {
			if skipping {
				skipping = false
			}
			if strings.EqualFold(matches[1], oldName) {
				found = true
				skipping = true
				result = append(result, formatSmbSection(newName, properties)...)
				continue
			}
		}

		if skipping {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n"), found
}

func formatSmbSection(name string, properties map[string]string) []string {
	lines := []string{fmt.Sprintf("[%s]", name)}
	if path, ok := properties["path"]; ok {
		lines = append(lines, fmt.Sprintf("   path = %s", path))
	}
	var keys []string
	for k := range properties {
		if k != "path" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("   %s = %s", k, properties[k]))
	}
	return lines
}

// removeSmbSection removes a named section and all its content from smb.conf
func removeSmbSection(content, name string) (string, bool) {
	lines := strings.Split(content, "\n")
	var result []string
	found := false
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if matches := sectionRegex.FindStringSubmatch(trimmed); matches != nil {
			if skipping {
				skipping = false
			}
			if strings.EqualFold(matches[1], name) {
				found = true
				skipping = true
				continue
			}
		}

		if skipping {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n"), found
}

// reloadSamba reloads the Samba configuration using the first method that works.
// Tries smbcontrol first, then falls back to systemd D-Bus reloads with common service names.
func reloadSamba(ctx context.Context) error {
	// smbcontrol is the most portable method
	out, err := smbcontrolReload(ctx)
	if err == nil {
		slog.Info("Samba configuration reloaded via smbcontrol")
		return nil
	}
	slog.Debug("smbcontrol reload failed",
		"error", err,
		"output", strings.TrimSpace(string(out)))

	// Fall back to distro-specific unit names through the shared systemd D-Bus helper.
	for _, service := range []string{"smbd.service", "smb.service", "samba.service"} {
		err := systemdReloadUnit(ctx, service)
		if err == nil {
			slog.Info("Samba reloaded via systemd D-Bus", "service", service)
			return nil
		}
		slog.Debug("systemd D-Bus reload failed", "service", service, "error", err)
	}

	return fmt.Errorf("failed to reload Samba: no working reload method found (is Samba installed?)")
}
