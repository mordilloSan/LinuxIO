package hostbridge

import (
	"context"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/ifconfig"
)

// Environment extends the interface-configuration environment with the
// runtime seams the bridge engine needs.
type Environment struct {
	ifconfig.Environment
	RemoveFile          func(path string) error
	ReadFile            func(path string) ([]byte, error)
	InterfaceProbes     func(ctx context.Context) ([]InterfaceProbe, error)
	ManagerForInterface func(ctx context.Context, iface string) (string, error)
	VerifyBridge        func(ctx context.Context, bridge, member string) (bool, error)
	RemoveBridge        func(name string) error
	// VerifyBridgeHandoff is an optional runtime verification seam. The
	// default verifier checks the bridge/member links, MAC, and addresses.
	VerifyBridgeHandoff func(ctx context.Context, state *BridgeHandoffState) (bool, error)
}

func DefaultEnvironment() Environment {
	return Environment{
		Environment: ifconfig.DefaultEnvironment(),
		RemoveFile:  os.Remove,
	}
}

// InterfaceProbe is the read-only host state needed before creating a bridge.
// It is deliberately small so bridge preflights cannot accidentally become a
// second network inventory implementation.
type InterfaceProbe struct {
	Name         string
	MAC          string
	Ethernet     bool
	Loopback     bool
	Wireless     bool
	Bridge       bool
	Master       string
	Addresses    []string
	DefaultRoute bool
	// VirtualChildren are macvlan/macvtap/ipvlan links stacked on this
	// interface, in any network namespace. They hold the kernel rx handler a
	// bridge port would need, so enslaving fails with EBUSY while any exist.
	VirtualChildren []string
}

// BridgeHandoffPlan is the explicit, risky operation that moves a host's
// management L3 configuration from Member to Name. ConsoleAcknowledged is
// deliberately part of the plan so callers cannot accidentally omit the
// out-of-band recovery acknowledgement.
type BridgeHandoffPlan struct {
	Name                string `json:"name"`
	Member              string `json:"member"`
	ConsoleAcknowledged bool   `json:"consoleAcknowledged"`
}

// BridgeHandoffState maps a user operation to the native rollback transaction
// owned by NetworkManager or Netplan.
type BridgeHandoffState struct {
	Plan                 BridgeHandoffPlan `json:"plan"`
	Backend              string            `json:"backend"`
	Handle               string            `json:"handle,omitempty"`
	MemberMAC            string            `json:"memberMac"`
	OriginalAddresses    []string          `json:"originalAddresses,omitempty"`
	OriginalDefaultRoute bool              `json:"originalDefaultRoute"`
}
