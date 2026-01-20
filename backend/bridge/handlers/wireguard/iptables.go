package wireguard

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/coreos/go-iptables/iptables"
	"github.com/mordilloSan/go-logger/logger"
)

// SetupNAT configures iptables rules for WireGuard NAT/masquerading.
// This must be called AFTER the interface is brought up.
func SetupNAT(interfaceName, egressNic, subnet string) error {
	logger.Infof("SetupNAT: configuring NAT for %s -> %s (subnet: %s)", interfaceName, egressNic, subnet)

	if _, err := net.InterfaceByName(egressNic); err != nil {
		return fmt.Errorf("egress interface %q not found: %w", egressNic, err)
	}
	ip, _, err := net.ParseCIDR(subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet %q: %w", subnet, err)
	}
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("subnet %q is not IPv4", subnet)
	}

	ipt, err := iptables.New()
	if err != nil {
		logger.Errorf("SetupNAT: failed to initialize iptables: %v", err)
		return fmt.Errorf("initialize iptables: %w", err)
	}

	if err := enableIPForwarding(); err != nil {
		logger.Errorf("SetupNAT: failed to enable IP forwarding: %v", err)
		return fmt.Errorf("enable IP forwarding: %w", err)
	}

	// Allow forwarding from WireGuard interface to egress interface
	if err := insertRuleIfMissing(ipt, "filter", "FORWARD", 1,
		"-i", interfaceName,
		"-o", egressNic,
		"-j", "ACCEPT"); err != nil {
		logger.Errorf("SetupNAT: failed to add forward rule (wg -> egress): %v", err)
		return fmt.Errorf("add forward rule (wg -> egress): %w", err)
	}
	logger.Debugf("SetupNAT: added FORWARD rule: %s -> %s", interfaceName, egressNic)

	// Allow established/related connections back from egress to WireGuard
	if err := insertRuleIfMissing(ipt, "filter", "FORWARD", 1,
		"-o", interfaceName,
		"-i", egressNic,
		"-m", "state",
		"--state", "RELATED,ESTABLISHED",
		"-j", "ACCEPT"); err != nil {
		logger.Errorf("SetupNAT: failed to add forward rule (egress -> wg): %v", err)
		return fmt.Errorf("add forward rule (egress -> wg): %w", err)
	}
	logger.Debugf("SetupNAT: added FORWARD rule: %s -> %s (ESTABLISHED)", egressNic, interfaceName)

	// NAT masquerading for outbound traffic
	if err := appendRuleIfMissing(ipt, "nat", "POSTROUTING",
		"-o", egressNic,
		"-s", subnet,
		"-j", "MASQUERADE"); err != nil {
		logger.Errorf("SetupNAT: failed to add MASQUERADE rule: %v", err)
		return fmt.Errorf("add MASQUERADE rule: %w", err)
	}
	logger.Debugf("SetupNAT: added MASQUERADE rule for subnet %s", subnet)

	logger.Infof("SetupNAT: successfully configured NAT for %s", interfaceName)
	return nil
}

// CleanupNAT removes iptables rules for WireGuard NAT/masquerading.
// This should be called BEFORE the interface is brought down.
func CleanupNAT(interfaceName, egressNic, subnet string) error {
	logger.Infof("CleanupNAT: removing NAT rules for %s -> %s (subnet: %s)", interfaceName, egressNic, subnet)

	ipt, err := iptables.New()
	if err != nil {
		logger.Errorf("CleanupNAT: failed to initialize iptables: %v", err)
		return fmt.Errorf("initialize iptables: %w", err)
	}

	var cleanupErrors []error

	// Remove FORWARD rule: WireGuard -> egress
	if err := removeRuleIfExists(ipt, "filter", "FORWARD",
		"-i", interfaceName,
		"-o", egressNic,
		"-j", "ACCEPT"); err != nil {
		logger.Warnf("CleanupNAT: failed to remove forward rule (wg -> egress): %v", err)
		cleanupErrors = append(cleanupErrors, err)
	}

	// Remove FORWARD rule: egress -> WireGuard (established)
	if err := removeRuleIfExists(ipt, "filter", "FORWARD",
		"-o", interfaceName,
		"-i", egressNic,
		"-m", "state",
		"--state", "RELATED,ESTABLISHED",
		"-j", "ACCEPT"); err != nil {
		logger.Warnf("CleanupNAT: failed to remove forward rule (egress -> wg): %v", err)
		cleanupErrors = append(cleanupErrors, err)
	}

	// Remove MASQUERADE rule
	if err := removeRuleIfExists(ipt, "nat", "POSTROUTING",
		"-o", egressNic,
		"-s", subnet,
		"-j", "MASQUERADE"); err != nil {
		logger.Warnf("CleanupNAT: failed to remove MASQUERADE rule: %v", err)
		cleanupErrors = append(cleanupErrors, err)
	}

	if len(cleanupErrors) > 0 {
		logger.Warnf("CleanupNAT: completed with %d errors", len(cleanupErrors))
		return fmt.Errorf("cleanup had %d errors (first: %v)", len(cleanupErrors), cleanupErrors[0])
	}

	logger.Infof("CleanupNAT: successfully removed NAT rules for %s", interfaceName)
	return nil
}

// removeRuleIfExists checks if a rule exists before attempting to delete it.
func removeRuleIfExists(ipt *iptables.IPTables, table, chain string, rulespec ...string) error {
	exists, err := ipt.Exists(table, chain, rulespec...)
	if err != nil {
		return fmt.Errorf("check rule existence: %w", err)
	}

	if !exists {
		logger.Debugf("removeRuleIfExists: rule does not exist in %s/%s, skipping", table, chain)
		return nil
	}

	if err := ipt.Delete(table, chain, rulespec...); err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}

	logger.Debugf("removeRuleIfExists: removed rule from %s/%s", table, chain)
	return nil
}

func insertRuleIfMissing(ipt *iptables.IPTables, table, chain string, position int, rulespec ...string) error {
	exists, err := ipt.Exists(table, chain, rulespec...)
	if err != nil {
		return fmt.Errorf("check rule existence: %w", err)
	}
	if exists {
		logger.Debugf("insertRuleIfMissing: rule already exists in %s/%s, skipping", table, chain)
		return nil
	}
	if err := ipt.Insert(table, chain, position, rulespec...); err != nil {
		return fmt.Errorf("insert rule: %w", err)
	}
	return nil
}

func appendRuleIfMissing(ipt *iptables.IPTables, table, chain string, rulespec ...string) error {
	exists, err := ipt.Exists(table, chain, rulespec...)
	if err != nil {
		return fmt.Errorf("check rule existence: %w", err)
	}
	if exists {
		logger.Debugf("appendRuleIfMissing: rule already exists in %s/%s, skipping", table, chain)
		return nil
	}
	if err := ipt.Append(table, chain, rulespec...); err != nil {
		return fmt.Errorf("append rule: %w", err)
	}
	return nil
}

// enableIPForwarding enables IPv4 forwarding via /proc filesystem.
func enableIPForwarding() error {
	const path = "/proc/sys/net/ipv4/ip_forward"
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	logger.Debugf("enableIPForwarding: enabled IPv4 forwarding")
	return nil
}

// natConfigPath returns the path to the NAT config metadata file for an interface.
func natConfigPath(interfaceName string) string {
	return filepath.Join(wgConfigDir, interfaceName+".nat")
}

// SaveNATConfig stores NAT configuration metadata for later cleanup.
func SaveNATConfig(interfaceName, egressNic, subnet string) error {
	cfg := NATConfig{
		EgressNic: egressNic,
		Subnet:    subnet,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal NAT config: %w", err)
	}

	path := natConfigPath(interfaceName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write NAT config to %s: %w", path, err)
	}

	logger.Debugf("SaveNATConfig: saved NAT config for %s to %s", interfaceName, path)
	return nil
}

// LoadNATConfig loads NAT configuration metadata for cleanup.
func LoadNATConfig(interfaceName string) (*NATConfig, error) {
	path := natConfigPath(interfaceName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No NAT config exists
		}
		return nil, fmt.Errorf("read NAT config from %s: %w", path, err)
	}

	var cfg NATConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal NAT config: %w", err)
	}

	logger.Debugf("LoadNATConfig: loaded NAT config for %s from %s", interfaceName, path)
	return &cfg, nil
}

// RemoveNATConfig removes the NAT configuration metadata file.
func RemoveNATConfig(interfaceName string) error {
	path := natConfigPath(interfaceName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove NAT config %s: %w", path, err)
	}
	logger.Debugf("RemoveNATConfig: removed NAT config for %s", interfaceName)
	return nil
}
