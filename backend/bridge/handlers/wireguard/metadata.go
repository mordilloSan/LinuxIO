package wireguard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func interfaceDNSPath(interfaceName string) string {
	return filepath.Join(wgConfigDir, interfaceName+".dns")
}

func SaveInterfaceDNS(interfaceName string, dns []string) error {
	dns = cleanDNSList(dns)
	if len(dns) == 0 {
		return RemoveInterfaceDNS(interfaceName)
	}

	data, err := json.Marshal(dns)
	if err != nil {
		return fmt.Errorf("marshal interface DNS: %w", err)
	}

	path := interfaceDNSPath(interfaceName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write interface DNS to %s: %w", path, err)
	}
	slog.Debug("saved interface DNS", "component", "wireguard", "subsystem", "metadata", "interface", interfaceName, "path", path)
	return nil
}

func LoadInterfaceDNS(interfaceName string) ([]string, error) {
	path := interfaceDNSPath(interfaceName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read interface DNS from %s: %w", path, err)
	}

	var dns []string
	if err := json.Unmarshal(data, &dns); err != nil {
		return nil, fmt.Errorf("unmarshal interface DNS: %w", err)
	}
	slog.Debug("loaded interface DNS", "component", "wireguard", "subsystem", "metadata", "interface", interfaceName, "path", path)
	return cleanDNSList(dns), nil
}

func RemoveInterfaceDNS(interfaceName string) error {
	path := interfaceDNSPath(interfaceName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove interface DNS %s: %w", path, err)
	}
	slog.Debug("removed interface DNS", "component", "wireguard", "subsystem", "metadata", "interface", interfaceName, "path", path)
	return nil
}

func cleanDNSList(dns []string) []string {
	if len(dns) == 0 {
		return nil
	}

	cleaned := make([]string, 0, len(dns))
	for _, entry := range dns {
		entry = strings.TrimSpace(entry)
		if entry != "" {
			cleaned = append(cleaned, entry)
		}
	}
	return cleaned
}
