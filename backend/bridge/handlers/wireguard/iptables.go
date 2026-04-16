package wireguard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/coreos/go-iptables/iptables"
)

type natBackend interface {
	Name() string
	Setup(interfaceName, egressNic, subnet string) error
	Cleanup(interfaceName, egressNic, subnet string) error
}

type natCommandRunner interface {
	LookPath(name string) (string, error)
	Run(name string, args ...string) ([]byte, error)
	RunInput(name, input string, args ...string) ([]byte, error)
}

type execNATCommandRunner struct{}

type iptablesBackend struct{}
type firewalldBackend struct{}
type nftBackend struct{}

var (
	wireguardNATRunner natCommandRunner = execNATCommandRunner{}
	newIPTablesClient                   = iptables.New
)

func (execNATCommandRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (execNATCommandRunner) Run(name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.Command(path, args...).CombinedOutput()
}

func (execNATCommandRunner) RunInput(name, input string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(input)
	return cmd.CombinedOutput()
}

func SetupNAT(interfaceName, egressNic, subnet string) (string, error) {
	slog.Info("configuring NAT", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "service", egressNic, "subnet", subnet)

	if err := validateNATArgs(egressNic, subnet); err != nil {
		return "", err
	}
	if err := enableIPForwarding(); err != nil {
		slog.Error("failed to enable IP forwarding", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "service", egressNic, "subnet", subnet, "error", err)
		return "", fmt.Errorf("enable IP forwarding: %w", err)
	}

	backendName, err := detectPreferredNATBackend()
	if err != nil {
		return "", err
	}
	backend, err := openNATBackend(backendName)
	if err != nil {
		return "", err
	}

	if err := backend.Setup(interfaceName, egressNic, subnet); err != nil {
		return "", err
	}
	slog.Info("configured NAT", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "service", egressNic, "subnet", subnet, "mode", backendName)
	return backendName, nil
}

func CleanupNAT(interfaceName, egressNic, subnet, backendName string) error {
	slog.Info("removing NAT rules", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "service", egressNic, "subnet", subnet, "mode", backendName)

	backends, err := cleanupBackends(backendName)
	if err != nil {
		return err
	}

	var cleanupErrors []error
	for _, backend := range backends {
		if err := backend.Cleanup(interfaceName, egressNic, subnet); err != nil {
			slog.Warn("NAT backend cleanup failed", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "service", egressNic, "subnet", subnet, "mode", backend.Name(), "error", err)
			cleanupErrors = append(cleanupErrors, err)
			continue
		}
		slog.Info("removed NAT rules", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "service", egressNic, "subnet", subnet, "mode", backend.Name())
		return nil
	}

	if len(cleanupErrors) == 0 {
		return nil
	}
	return fmt.Errorf("cleanup had %d errors (first: %v)", len(cleanupErrors), cleanupErrors[0])
}

func (iptablesBackend) Name() string {
	return "iptables"
}

func (iptablesBackend) Setup(interfaceName, egressNic, subnet string) error {
	ipt, err := newIPTablesClient()
	if err != nil {
		slog.Error("failed to initialize iptables", "component", "wireguard", "subsystem", "nat", "error", err)
		return fmt.Errorf("initialize iptables: %w", err)
	}

	if err := insertRuleIfMissing(ipt, "filter", "FORWARD", 1,
		"-i", interfaceName,
		"-o", egressNic,
		"-j", "ACCEPT"); err != nil {
		return fmt.Errorf("add forward rule (wg -> egress): %w", err)
	}

	if err := insertRuleIfMissing(ipt, "filter", "FORWARD", 1,
		"-o", interfaceName,
		"-i", egressNic,
		"-m", "state",
		"--state", "RELATED,ESTABLISHED",
		"-j", "ACCEPT"); err != nil {
		return fmt.Errorf("add forward rule (egress -> wg): %w", err)
	}

	if err := appendRuleIfMissing(ipt, "nat", "POSTROUTING",
		"-o", egressNic,
		"-s", subnet,
		"-j", "MASQUERADE"); err != nil {
		return fmt.Errorf("add MASQUERADE rule: %w", err)
	}

	return nil
}

func (iptablesBackend) Cleanup(interfaceName, egressNic, subnet string) error {
	ipt, err := newIPTablesClient()
	if err != nil {
		return fmt.Errorf("initialize iptables: %w", err)
	}

	var cleanupErrors []error
	if err := removeRuleIfExists(ipt, "filter", "FORWARD",
		"-i", interfaceName,
		"-o", egressNic,
		"-j", "ACCEPT"); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	if err := removeRuleIfExists(ipt, "filter", "FORWARD",
		"-o", interfaceName,
		"-i", egressNic,
		"-m", "state",
		"--state", "RELATED,ESTABLISHED",
		"-j", "ACCEPT"); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	if err := removeRuleIfExists(ipt, "nat", "POSTROUTING",
		"-o", egressNic,
		"-s", subnet,
		"-j", "MASQUERADE"); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("iptables cleanup failed (first: %v)", cleanupErrors[0])
	}
	return nil
}

func (firewalldBackend) Name() string {
	return "firewalld"
}

func (firewalldBackend) Setup(interfaceName, egressNic, subnet string) error {
	rules, err := firewalldRules()
	if err != nil {
		return err
	}
	for _, rule := range firewalldDirectRules(interfaceName, egressNic, subnet) {
		if _, ok := rules[strings.Join(rule, " ")]; ok {
			continue
		}
		args := append([]string{"--direct", "--add-rule"}, rule...)
		output, cmdErr := wireguardNATRunner.Run("firewall-cmd", args...)
		if cmdErr != nil {
			return commandOutputError("firewall-cmd", args, output, cmdErr)
		}
	}
	return nil
}

func (firewalldBackend) Cleanup(interfaceName, egressNic, subnet string) error {
	rules, err := firewalldRules()
	if err != nil {
		return err
	}
	for _, rule := range firewalldDirectRules(interfaceName, egressNic, subnet) {
		if _, ok := rules[strings.Join(rule, " ")]; !ok {
			continue
		}
		args := append([]string{"--direct", "--remove-rule"}, rule...)
		output, cmdErr := wireguardNATRunner.Run("firewall-cmd", args...)
		if cmdErr != nil {
			return commandOutputError("firewall-cmd", args, output, cmdErr)
		}
	}
	return nil
}

func (nftBackend) Name() string {
	return "nft"
}

func (nftBackend) Setup(interfaceName, egressNic, subnet string) error {
	table := nftTableName(interfaceName)
	_, _ = wireguardNATRunner.Run("nft", "delete", "table", "ip", table)
	script := buildNFTSetupScript(table, interfaceName, egressNic, subnet)
	output, err := wireguardNATRunner.RunInput("nft", script, "-f", "-")
	if err != nil {
		return commandOutputError("nft", []string{"-f", "-"}, output, err)
	}
	return nil
}

func (nftBackend) Cleanup(interfaceName, _, _ string) error {
	table := nftTableName(interfaceName)
	output, err := wireguardNATRunner.Run("nft", "delete", "table", "ip", table)
	if err != nil && !nftMissingTable(output) {
		return commandOutputError("nft", []string{"delete", "table", "ip", table}, output, err)
	}
	return nil
}

func detectPreferredNATBackend() (string, error) {
	return preferredNATBackendName(firewalldRunning(), nftAvailable(), iptablesAvailable())
}

func preferredNATBackendName(hasFirewalld, hasNft, hasIPTables bool) (string, error) {
	switch {
	case hasFirewalld:
		return "firewalld", nil
	case hasNft:
		return "nft", nil
	case hasIPTables:
		return "iptables", nil
	default:
		return "", fmt.Errorf("no supported firewall backend found for NAT (tried firewalld, nft, iptables)")
	}
}

func cleanupBackends(preferred string) ([]natBackend, error) {
	if strings.TrimSpace(preferred) != "" {
		backend, err := openNATBackend(preferred)
		if err != nil {
			return nil, err
		}
		return []natBackend{backend}, nil
	}

	backends := make([]natBackend, 0, 3)
	if firewalldRunning() {
		backends = append(backends, firewalldBackend{})
	}
	if nftAvailable() {
		backends = append(backends, nftBackend{})
	}
	if iptablesAvailable() {
		backends = append(backends, iptablesBackend{})
	}
	if len(backends) == 0 {
		return nil, fmt.Errorf("no supported firewall backend found for NAT cleanup")
	}
	return backends, nil
}

func openNATBackend(name string) (natBackend, error) {
	switch name {
	case "firewalld":
		return firewalldBackend{}, nil
	case "nft":
		return nftBackend{}, nil
	case "iptables":
		return iptablesBackend{}, nil
	default:
		return nil, fmt.Errorf("unsupported NAT backend %q", name)
	}
}

func validateNATArgs(egressNic, subnet string) error {
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
	return nil
}

func firewalldRunning() bool {
	if _, err := wireguardNATRunner.LookPath("firewall-cmd"); err != nil {
		return false
	}
	output, err := wireguardNATRunner.Run("firewall-cmd", "--state")
	return err == nil && strings.TrimSpace(string(output)) == "running"
}

func nftAvailable() bool {
	_, err := wireguardNATRunner.LookPath("nft")
	return err == nil
}

func iptablesAvailable() bool {
	_, err := newIPTablesClient()
	return err == nil
}

func firewalldRules() (map[string]struct{}, error) {
	output, err := wireguardNATRunner.Run("firewall-cmd", "--direct", "--get-all-rules")
	if err != nil {
		return nil, commandOutputError("firewall-cmd", []string{"--direct", "--get-all-rules"}, output, err)
	}
	rules := make(map[string]struct{})
	for line := range strings.SplitSeq(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rules[line] = struct{}{}
	}
	return rules, nil
}

func firewalldDirectRules(interfaceName, egressNic, subnet string) [][]string {
	return [][]string{
		{"ipv4", "filter", "FORWARD", "0", "-i", interfaceName, "-o", egressNic, "-j", "ACCEPT"},
		{"ipv4", "filter", "FORWARD", "0", "-o", interfaceName, "-i", egressNic, "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
		{"ipv4", "nat", "POSTROUTING", "0", "-o", egressNic, "-s", subnet, "-j", "MASQUERADE"},
	}
}

func buildNFTSetupScript(table, interfaceName, egressNic, subnet string) string {
	lines := []string{
		fmt.Sprintf("add table ip %s", table),
		fmt.Sprintf("add chain ip %s forward { type filter hook forward priority 0; policy accept; }", table),
		fmt.Sprintf("add chain ip %s postrouting { type nat hook postrouting priority srcnat; policy accept; }", table),
		fmt.Sprintf("add rule ip %s forward iifname %q oifname %q accept", table, interfaceName, egressNic),
		fmt.Sprintf("add rule ip %s forward iifname %q oifname %q ct state related,established accept", table, egressNic, interfaceName),
		fmt.Sprintf("add rule ip %s postrouting oifname %q ip saddr %s masquerade", table, egressNic, subnet),
	}
	return strings.Join(lines, "\n") + "\n"
}

func nftTableName(interfaceName string) string {
	var builder strings.Builder
	builder.WriteString("linuxio_wg_")
	for _, r := range interfaceName {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return builder.String()
}

func nftMissingTable(output []byte) bool {
	lower := strings.ToLower(string(output))
	return strings.Contains(lower, "no such file") || strings.Contains(lower, "does not exist")
}

func commandOutputError(name string, args []string, output []byte, err error) error {
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
}

func removeRuleIfExists(ipt *iptables.IPTables, table, chain string, rulespec ...string) error {
	exists, err := ipt.Exists(table, chain, rulespec...)
	if err != nil {
		return fmt.Errorf("check rule existence: %w", err)
	}
	if !exists {
		return nil
	}
	if err := ipt.Delete(table, chain, rulespec...); err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	return nil
}

func insertRuleIfMissing(ipt *iptables.IPTables, table, chain string, position int, rulespec ...string) error {
	exists, err := ipt.Exists(table, chain, rulespec...)
	if err != nil {
		return fmt.Errorf("check rule existence: %w", err)
	}
	if exists {
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
		return nil
	}
	if err := ipt.Append(table, chain, rulespec...); err != nil {
		return fmt.Errorf("append rule: %w", err)
	}
	return nil
}

func enableIPForwarding() error {
	const path = "/proc/sys/net/ipv4/ip_forward"
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	slog.Debug("enableIPForwarding: enabled IPv4 forwarding")
	return nil
}

func natConfigPath(interfaceName string) string {
	return filepath.Join(wgConfigDir, interfaceName+".nat")
}

func SaveNATConfig(interfaceName, egressNic, subnet, backend string) error {
	cfg := NATConfig{
		EgressNic: egressNic,
		Subnet:    subnet,
		Backend:   backend,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal NAT config: %w", err)
	}

	path := natConfigPath(interfaceName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write NAT config to %s: %w", path, err)
	}
	slog.Debug("saved NAT config", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "path", path)
	return nil
}

func LoadNATConfig(interfaceName string) (*NATConfig, error) {
	path := natConfigPath(interfaceName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read NAT config from %s: %w", path, err)
	}

	var cfg NATConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal NAT config: %w", err)
	}
	slog.Debug("loaded NAT config", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "path", path)
	return &cfg, nil
}

func RemoveNATConfig(interfaceName string) error {
	path := natConfigPath(interfaceName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove NAT config %s: %w", path, err)
	}
	slog.Debug("removed NAT config", "component", "wireguard", "subsystem", "nat", "interface", interfaceName, "path", path)
	return nil
}
