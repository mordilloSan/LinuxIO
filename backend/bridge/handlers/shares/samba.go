package shares

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/mordilloSan/go-logger/logger"
)

const smbConfFile = "/etc/samba/smb.conf"

// reservedSections are built-in smb.conf sections, not user file shares
var reservedSections = map[string]bool{
	"global":   true,
	"homes":    true,
	"printers": true,
	"print$":   true,
}

var (
	sectionRegex   = regexp.MustCompile(`^\[([^\]]+)\]$`)
	keyValueRegex  = regexp.MustCompile(`^\s*([^=]+?)\s*=\s*(.*)$`)
	validShareName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _.-]*$`)
)

// ListSambaShares reads smb.conf and returns all user-defined shares
func ListSambaShares() ([]SambaShare, error) {
	sections, err := parseSmbConf()
	if err != nil {
		return nil, err
	}

	var shares []SambaShare
	for name, props := range sections {
		if reservedSections[strings.ToLower(name)] {
			continue
		}
		shares = append(shares, SambaShare{
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
func CreateSambaShare(name string, properties map[string]string) error {
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

	sections, err := parseSmbConf()
	if err != nil {
		return err
	}
	for existing := range sections {
		if strings.EqualFold(existing, name) {
			return fmt.Errorf("share already exists: %s", existing)
		}
	}

	if dirErr := os.MkdirAll(path, 0755); dirErr != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, dirErr)
	}

	block := formatSambaSection(name, properties)

	f, err := os.OpenFile(smbConfFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", smbConfFile, err)
	}
	defer f.Close()

	if _, err := f.WriteString("\n" + block); err != nil {
		return fmt.Errorf("failed to write to %s: %w", smbConfFile, err)
	}

	logger.Infof("Created Samba share: %s", name)
	return reloadSamba()
}

// UpdateSambaShare replaces a share section in smb.conf and reloads samba
func UpdateSambaShare(name string, properties map[string]string) error {
	if !validShareName.MatchString(name) {
		return fmt.Errorf("invalid share name: %s", name)
	}
	path, ok := properties["path"]
	if !ok || path == "" {
		return fmt.Errorf("share must have a 'path' property")
	}
	if !validExportPath.MatchString(path) {
		return fmt.Errorf("invalid share path: %s", path)
	}

	content, err := os.ReadFile(smbConfFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", smbConfFile, err)
	}

	newContent, found := replaceSmbSection(string(content), name, properties)
	if !found {
		return fmt.Errorf("share not found: %s", name)
	}

	if err := os.WriteFile(smbConfFile, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", smbConfFile, err)
	}

	logger.Infof("Updated Samba share: %s", name)
	return reloadSamba()
}

// DeleteSambaShare removes a share section from smb.conf and reloads samba
func DeleteSambaShare(name string) error {
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

	logger.Infof("Deleted Samba share: %s", name)
	return reloadSamba()
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
func replaceSmbSection(content, name string, properties map[string]string) (string, bool) {
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
				// Write the replacement section header + properties
				result = append(result, fmt.Sprintf("[%s]", name))
				if path, ok := properties["path"]; ok {
					result = append(result, fmt.Sprintf("   path = %s", path))
				}
				var keys []string
				for k := range properties {
					if k != "path" {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				for _, k := range keys {
					result = append(result, fmt.Sprintf("   %s = %s", k, properties[k]))
				}
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

// GetSambaClients returns currently connected Samba clients via smbstatus -j
func GetSambaClients() ([]SambaConnectedClient, error) {
	output, err := exec.Command("smbstatus", "-j").CombinedOutput()
	if err != nil {
		logger.Debugf("smbstatus -j failed: %v, output: %s", err, strings.TrimSpace(string(output)))
		return []SambaConnectedClient{}, nil
	}

	var status struct {
		Sessions map[string]struct {
			Username    string `json:"username"`
			RemoteHost string `json:"remote_machine"`
		} `json:"sessions"`
		Shares map[string]struct {
			User    string `json:"user"`
			Machine string `json:"machine"`
			Service string `json:"service"`
		} `json:"tcon"`
	}

	if err := json.Unmarshal(output, &status); err != nil {
		logger.Debugf("smbstatus JSON parse failed: %v", err)
		return []SambaConnectedClient{}, nil
	}

	var clients []SambaConnectedClient
	seen := make(map[string]bool)

	for _, share := range status.Shares {
		if share.Service == "" || reservedSections[strings.ToLower(share.Service)] {
			continue
		}
		key := share.User + "@" + share.Machine + ":" + share.Service
		if seen[key] {
			continue
		}
		seen[key] = true
		clients = append(clients, SambaConnectedClient{
			Username: share.User,
			IP:       share.Machine,
			Share:    share.Service,
		})
	}

	return clients, nil
}

// reloadSamba reloads the Samba configuration using the first method that works.
// Tries smbcontrol first, then falls back to systemctl with common service names.
func reloadSamba() error {
	// smbcontrol is the most portable method
	if out, err := exec.Command("smbcontrol", "all", "reload-config").CombinedOutput(); err == nil {
		logger.Infof("Samba configuration reloaded via smbcontrol")
		return nil
	} else {
		logger.Debugf("smbcontrol failed: %s", strings.TrimSpace(string(out)))
	}

	// Fall back to systemctl with distro-specific service names
	for _, service := range []string{"smbd", "smb", "samba"} {
		if out, err := exec.Command("systemctl", "reload", service).CombinedOutput(); err == nil {
			logger.Infof("Samba reloaded via systemctl reload %s", service)
			return nil
		} else {
			logger.Debugf("systemctl reload %s failed: %s", service, strings.TrimSpace(string(out)))
		}
	}

	return fmt.Errorf("failed to reload Samba: no working reload method found (is Samba installed?)")
}
