package apischema

type NetworkInterface struct {
	// Carrier is nil when the kernel will not answer: virtual interfaces have
	// no carrier attribute, and reading it on a down link fails. Nil means
	// "unknown", never "no link".
	Carrier *bool `json:"carrier,omitzero"`
	// ConfigBackend names the on-disk configuration source the bridge detected
	// for this interface (netplan, nmconnection, systemd-networkd, ifupdown,
	// ifcfg). Empty when no backend claims it.
	ConfigBackend string   `json:"config_backend,omitempty"`
	DNS           []string `json:"dns"`
	// DNSOptions is nil when the config backend cannot set search domains or
	// keep static DNS servers alongside DHCP.
	DNSOptions *NetworkDNSOptions `json:"dns_options,omitzero"`
	Driver     string             `json:"driver,omitempty"`
	Duplex     string             `json:"duplex"`
	Gateway    string             `json:"gateway"`
	IPv4       []string           `json:"ipv4"`
	IPv4Method *string            `json:"ipv4_method,omitzero"`
	// LogUnit is the installed systemd unit whose journal covers this
	// interface's stack, resolved from ConfigBackend. Empty when none of the
	// candidate units exist, which is the signal to offer no log view.
	LogUnit   string `json:"log_unit,omitempty"`
	MAC       string `json:"mac"`
	MTU       int    `json:"mtu"`
	Name      string `json:"name"`
	OperState string `json:"operstate"`
	// Optional reports whether boot skips waiting for this link to come
	// online. Nil when the config backend cannot express it.
	Optional *bool  `json:"optional,omitzero"`
	Speed    string `json:"speed"`
	State    int    `json:"state"`
	Type     string `json:"type"`
}

type NetworkDNSOptions struct {
	Search []string `json:"search"`
	// IgnoreDHCP is true when static IPv4 DNS servers replace the ones DHCP
	// offers.
	IgnoreDHCP bool `json:"ignore_dhcp"`
}

// NetworkBridgeCandidate is one wired interface that can, or cannot, be used
// for a spare-NIC bridge. Reasons explain an ineligible candidate.
type NetworkBridgeCandidate struct {
	Name            string   `json:"name"`
	MAC             string   `json:"mac"`
	Backend         string   `json:"backend,omitempty"`
	Eligible        bool     `json:"eligible"`
	Reasons         []string `json:"reasons,omitempty"`
	HandoffEligible bool     `json:"handoffEligible"`
	HandoffReasons  []string `json:"handoffReasons,omitempty"`
}

type NetworkBridgeOptions struct {
	Candidates []NetworkBridgeCandidate `json:"candidates"`
	Warnings   []string                 `json:"warnings,omitempty"`
}

type NetworkBridgeCreateResult struct {
	Name    string `json:"name"`
	Member  string `json:"member"`
	Backend string `json:"backend,omitempty"`
}

type NetworkBridgeHandoffState string

const (
	NetworkBridgeHandoffApplying             NetworkBridgeHandoffState = "applying"
	NetworkBridgeHandoffAwaitingConfirmation NetworkBridgeHandoffState = "awaiting_confirmation"
	NetworkBridgeHandoffConfirmed            NetworkBridgeHandoffState = "confirmed"
	NetworkBridgeHandoffReverted             NetworkBridgeHandoffState = "reverted"
	NetworkBridgeHandoffUnknown              NetworkBridgeHandoffState = "unknown"
)

// NetworkBridgeHandoffStatus is state-oriented so reconnecting clients can
// resolve an ambiguous transport result through the durable operation record.
type NetworkBridgeHandoffStatus struct {
	OperationID string                    `json:"operationId"`
	Name        string                    `json:"name"`
	Member      string                    `json:"member"`
	Backend     string                    `json:"backend,omitempty"`
	State       NetworkBridgeHandoffState `json:"state"`
	Deadline    string                    `json:"deadline,omitempty"`
	Message     string                    `json:"message,omitempty"`
	Error       string                    `json:"error,omitempty"`
}

type InterfaceRequest struct {
	Iface string `json:"iface"`
}

type InterfaceMethodRequest struct {
	Iface  string `json:"iface"`
	Method string `json:"method"`
}

type IPv4ManualRequest struct {
	Iface   string `json:"iface"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
	// Search lists DNS search domains, comma or space separated.
	Search string `json:"search,omitempty"`
}

// IPv4DHCPRequest switches an interface to DHCP. A non-empty DNS replaces the
// IPv4 servers DHCP offers.
type IPv4DHCPRequest struct {
	Iface  string `json:"iface"`
	Method string `json:"method"`
	DNS    string `json:"dns,omitempty"`
	Search string `json:"search,omitempty"`
}

type InterfaceMTURequest struct {
	Iface string `json:"iface"`
	MTU   string `json:"mtu"`
}

type InterfaceOptionalRequest struct {
	Iface    string `json:"iface"`
	Optional bool   `json:"optional"`
}

// NetworkBridgeCreateRequest describes a Stage 2a bridge over a spare NIC.
// Stage 2a never migrates host IP configuration to the new bridge.
type NetworkBridgeCreateRequest struct {
	Name   string `json:"name"`
	Member string `json:"member"`
}

// NetworkBridgeHandoffRequest starts the single-NIC host-IP handoff. The
// operation ID is allocated by the client so status can be recovered after
// the apply severs the current connection.
type NetworkBridgeHandoffRequest struct {
	OperationID         string `json:"operationId"`
	Name                string `json:"name"`
	Member              string `json:"member"`
	ConsoleAcknowledged bool   `json:"consoleAcknowledged"`
}

type NetworkBridgeHandoffOperationRequest struct {
	OperationID string `json:"operationId"`
}
