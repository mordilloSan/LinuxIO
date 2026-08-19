# VM Bridge Networking Plan

> **Status: Planned.** VMs currently attach only to libvirt's `default` NAT
> network, and the host network subsystem has no link-creation primitives.

This plan gives VMs Unraid-style bridged networking: a created VM can appear
on the physical LAN with its own MAC address and DHCP lease while still
reaching the host, by attaching to a Linux bridge that carries the host's IP
configuration. The work splits into two independent deliverables: VM-side
network selection (attach to a bridge that already exists) and host-side
guided bridge creation (make one safely). The second deliverable carries all
the risk, because it moves the management IP of a remote machine.

## Why a real bridge, and not macvtap

| Mode | VM→LAN / LAN→VM | Host↔VM | Host changes needed |
|---|---|---|---|
| `default` NAT (today) | NAT out / no inbound | works | none |
| Linux bridge | yes / yes | works | move host IP onto bridge |
| macvtap | yes / yes | blocked by the kernel | none |

The `default` NAT network already provides host↔VM traffic in both directions
(the host owns `192.168.122.1`). What it lacks is LAN presence: inbound
reachability, a router-issued DHCP lease, and L2 broadcast/mDNS discovery
(which the Home Assistant preset needs). macvtap provides LAN presence with
zero host reconfiguration, but the macvlan driver switches guest frames below
the host's IP stack, so host and guest cannot talk.

Both prior-art systems confirm the choice. TrueNAS SCALE defaults VM NICs to
macvtap, and "the VM cannot mount the NAS's own shares" is its most common
virtualization complaint; the documented fix is a manually created bridge.
Unraid's non-bridged mode works around the macvtap limitation by copying the
host's LAN IP onto a shim macvtap interface — but that shim is created by the
Docker service, so host↔VM connectivity silently depends on an unrelated
daemon. A bridge carrying the host IP is the only mode where LAN presence and
host communication both work without shims. macvtap is out of scope.

## Current baseline

- `buildDomain` hardcodes one virtio NIC on `<source network="default">`
  against the `defaultNetworkName` constant, and request validation rejects
  any other value (`backend/bridge/handlers/virt/domain_xml.go`,
  `connection.go`). The create dialog has no network field and posts the
  literal `"default"`.
- No route enumerates libvirt networks or host bridges; the `libvirtConn`
  interface exposes only lookup/create/autostart/DHCP-lease methods for the
  default network.
- VM IP addresses come exclusively from `NetworkGetDhcpLeases`, which only
  covers libvirt-managed dnsmasq networks.
- The host network subsystem
  (`backend/bridge/handlers/network/internal/network/`) edits per-interface
  L3 configuration through five autodetected file backends (netplan,
  NetworkManager keyfile, systemd-networkd, ifupdown, ifcfg). It cannot
  create bridges, bonds, or VLANs (no `.netdev` writer, no link-add path),
  and it has no rollback: applying a bad change and losing the connection is
  a documented accepted outcome.

## Principles

- **Renderer ownership.** Bridges are created by writing the detected
  backend's declarative config and invoking its apply command, exactly like
  the existing L3 mutations. Never create links imperatively at runtime with
  netlink/`ip link`: state that only exists in the kernel does not survive
  reboot and diverges from the renderer's view. (TrueNAS enforces the same
  rule: hand-rolled bridges get clobbered on the next config sync.)
- **The VM XML records what the user chose.** No background rewriting of
  domain XML when host networking changes (Unraid seds every domain XML at
  libvirt startup when its bridging toggle flips; that is the failure mode to
  avoid, not the pattern to copy).
- **Honest safety.** The IP hand-off gets a real revert mechanism with an
  explicit confirmation step, not a hope that the connection survives.
  Refuse-rather-than-guess stays in force for ambiguous or complex existing
  layouts.
- **NAT stays the default.** Bridged attachment is an explicit choice at VM
  creation; nothing migrates automatically.

## Phase 1 — VM-side network selection

Serve every user who already has a bridge (or can attach to a second NIC)
before touching host network configuration.

This phase is independent of the host's network manager. LinuxIO enumerates
existing bridges from `/sys/class/net/*/bridge`, verifies that the selected
bridge exists and is up, and emits libvirt `<interface type="bridge">` XML.
It does not need NetworkManager, systemd-networkd, or a persistent network
configuration adapter.

Consequently, hosts managed by an otherwise unsupported stack such as Wicked
or ifupdown2 can still expose existing bridges, attach VMs to them, and use ARP
or the guest agent for VM IP discovery. They do not get LinuxIO-guided host
bridge creation.

1. **Enumeration.** A new read-only Call (e.g. `virt.networks`) returning
   libvirt networks (name, active, forward mode) and host Linux bridges
   (`/sys/class/net/*/bridge`), deduplicating bridges that back libvirt
   networks (`virbr*`). Requires adding network-list methods to the
   `libvirtConn` interface.
2. **Domain XML.** A `type='bridge'` branch in `buildDomain` when the
   requested network names a host bridge; `type='network'` otherwise.
   Validation accepts `default` or an existing bridge and rejects everything
   else with a structured error. Preflight verifies the selected source
   exists and is up.
3. **Frontend.** A network select in the create dialog, defaulting to
   "NAT (default)" and listing available bridges. Image presets gain a
   bridged-preferred flag (Home Assistant preselects a bridge when exactly
   one exists; server presets keep NAT).
4. **IP visibility.** Bridged VMs never appear in libvirt DHCP leases.
   Extend NIC enrichment with `DomainInterfaceAddresses` using the ARP
   source, then the guest-agent source when available, and add
   `qemu-guest-agent` to the cloud-init preset package set.
5. **Networks tab.** Show the attachment type (NAT/bridge) alongside the
   existing VM/state/network/IP/MAC columns.

## Phase 2 — guided host bridge creation

An explicit "create bridge" flow on the network page, delivered in two
stages so the dangerous step lands on proven machinery.

### Stage 2a — bridge on a spare NIC

Create a bridge over a NIC that carries no host IP configuration. Exercises
every backend writer with zero management-path risk, and on multi-NIC hosts
it is the complete feature.

### Stage 2b — single-NIC IP hand-off

Move the host's L3 configuration onto the new bridge: strip addresses/DHCP
from the member NIC, enslave it, and apply the same IP mode on the bridge.
Pin the bridge MAC explicitly to the member's MAC — kernel, udev
`MACAddressPolicy`, and renderer defaults differ, and lease continuity is
what makes the host come back on the same address after a few seconds.

The hand-off preserves these invariants regardless of manager:

- the member NIC loses its L3 configuration;
- the bridge receives the member's complete L3 configuration;
- the bridge MAC is pinned to the member's MAC;
- the bridge and member are managed by the same runtime owner;
- the new configuration is persistent before the operation succeeds; and
- a failed check-in restores the previous persistent and runtime state.

### Runtime ownership and persistent configuration

Runtime ownership is resolved per interface, not once per host.
NetworkManager and systemd-networkd can coexist on the same machine, and
Netplan can render different interfaces to different managers. A bridge and
its member must have one confirmed runtime owner; mixed or unknown ownership
is refused.

Full host-network mutation support initially targets NetworkManager and
systemd-networkd. Other managers remain eligible for Phase 1 existing-bridge
attachment but not guided bridge creation.

#### NetworkManager

The NetworkManager path is almost entirely D-Bus-native:

1. Identify the member's active connection profile.
2. Create a checkpoint covering every affected device.
3. Create a persistent bridge profile.
4. Create or update the Ethernet port profile.
5. Move the member's complete IP settings map to the bridge.
6. Activate the profiles and observe their state through D-Bus.
7. Confirm the checkpoint or roll it back.

NetworkManager's profile API owns persistence through its active settings
plugin, including native keyfiles, NM-owned ifcfg profiles, and Ubuntu's
NetworkManager-Netplan integration. LinuxIO should not edit those formats
directly.

A NetworkManager checkpoint supplies timed runtime rollback, but it augments
rather than replaces the durable operation marker. LinuxIO must also record
the previous persistent profile settings and delete or restore changed
profiles if the operation is abandoned or the host reboots. See the
[NetworkManager checkpoint API](https://www.networkmanager.dev/docs/api/latest/gdbus-org.freedesktop.NetworkManager.html).

#### systemd-networkd

The systemd-networkd path separates runtime control from persistence:

1. D-Bus confirms that networkd manages the member and reports its selected
   configuration source.
2. When Netplan owns persistence, the Netplan D-Bus API writes a `bridges:`
   transaction. Otherwise, the native writer creates the bridge `.netdev`,
   bridge `.network`, and member `Bridge=` configuration.
3. networkd D-Bus reloads the configuration and reconfigures the affected
   links.
4. The snapshot, detached reverter, and check-in mechanism owns rollback;
   networkd has no NetworkManager-style checkpoint.

#### Controller boundary

Runtime observation/application and persistent configuration remain separate
capabilities:

```go
type NetworkRuntime interface {
	Manager(ctx context.Context, iface string) (RuntimeManager, error)
	Inspect(ctx context.Context, iface string) (RuntimeState, error)
	Apply(ctx context.Context, change ChangeSet) error
}

type NetworkConfigStore interface {
	Snapshot(ctx context.Context, ifaces []string) (Snapshot, error)
	WriteBridge(ctx context.Context, plan BridgePlan) error
	Restore(ctx context.Context, snapshot Snapshot) error
}
```

The initial compositions are:

| Runtime controller | Persistent configuration |
|---|---|
| `NetworkManagerController` | NetworkManager D-Bus supplies runtime and persistence |
| `NetworkdRuntime` | `NetplanStore` for Netplan-rendered networkd |
| `NetworkdRuntime` | `NativeNetworkdStore` for direct `.netdev`/`.network` configuration |

The bridge operation orchestrates snapshot → write → apply → observe →
confirm/revert. The libvirt handler only consumes the resulting bridge name
and never owns host network configuration. Members owned by ifupdown2,
Wicked, an appliance control plane, or an unknown/mixed manager refuse guided
creation with a structured capability error.

### Apply safety: snapshot, detached reverter, check-in

Unraid performs this same hand-off live: applying network settings re-runs
`rc.inet1` — the same script that builds the bridge at boot — and the webGUI
reconnects seconds later on the same DHCP lease, with no revert mechanism
behind it at all. The live apply is proven practice; the revert below is the
part Unraid doesn't have.

The hand-off is a durable operation using the existing operation-record
machinery. The revert involves no systemd units, transient or installed. It
cannot live in the webserver either — that process runs sandboxed under
`DynamicUser` with no privileged path outside a session bridge — and the
applying bridge dies with its session. The revert is therefore owned by a
detached reverter process: the bridge re-execs its own binary as a revert
subcommand, double-forked so it reparents to PID 1 and survives bridge
death, session logout, and the connection drop.

1. Write the marker: a root-owned on-disk record holding the snapshot of
   every touched config file, the list of links the operation creates, the
   backend apply command, and the confirmation deadline (~90 s).
2. Spawn the detached reverter, which watches the marker.
3. Apply the new configuration. The connection may drop; the operation
   record already models this as an expected ambiguous outcome.
4. The frontend reconnects (same IP, per the MAC pinning above) and issues a
   confirmation Call that claims the marker and finalizes the record; the
   reverter sees the marker go and exits. Confirmation and revert both claim
   the marker by atomic rename, so exactly one outcome ever wins.
5. No confirmation by the deadline → the reverter claims the marker,
   restores the previous configuration, and the operation record reports the
   revert honestly. A confirmation that loses the race reports the reverted
   outcome instead of pretending success.

The revert path has three hard requirements beyond restoring file contents:

- **Tear down created links explicitly.** Re-running the renderer apply does
  not remove an existing kernel bridge on every backend (netplan never
  deletes devices it no longer renders); the revert deletes the links this
  operation created before re-applying the snapshot.
- **Own the reboot-inside-window case.** The reverter dies with the boot
  while the unconfirmed config files persist on disk; the marker persists
  too. The next privileged bridge to start finds the expired marker and
  runs the revert before allowing new network mutations. If the host
  reboots into a config that never comes up, nobody can log in to trigger
  that — this is the one residual gap of keeping systemd out of the revert
  path (closing it needs a boot-time unit, exactly the dependency being
  traded away). The window is ~90 s, MAC pinning makes a boot with the new
  config likely to come up anyway, and the wizard states the console
  requirement up front.
- **Prefer false reverts.** If only the client's network path broke, a
  working change reverts because no check-in arrived. That is the correct
  failure direction: the cost is redoing the change, never a lockout.

Bridge-creation mutations follow the existing rule: not `RetrySafe`. The
confirmation and explicit-revert Calls are idempotent. Netplan's D-Bus
`Try`/`Apply` transaction and NetworkManager's D-Bus checkpoints add native
runtime rollback where available, but neither replaces restoration of the
persistent source or ownership of the reboot-inside-window case. The
snapshot-plus-marker design remains the common primary mechanism. The
transient-unit machinery already in the tree (app and Docker updates) was
considered and rejected here: a transient timer dies at reboot just like the
detached process, so it buys only systemd supervision while putting
`systemd-run` in the lockout-recovery path.

### Preflights

Refuse or warn before writing anything:

- member NIC is already a bridge member or bond slave → refuse;
- wireless uplink → refuse and point at NAT (single-MAC constraint; even
  Unraid only manages a one-VM ipvtap shim there);
- running VMs attached to the affected path → require them off first;
- Docker interaction: hosts running Docker set
  `net.bridge.bridge-nf-call-iptables=1` with an iptables `FORWARD` drop
  policy, which silently discards bridged VM traffic. Preflight must detect
  the condition and report it; whether creation installs an ACCEPT rule for
  the bridge or surfaces a documented manual step is decided at
  implementation per firewall stack (iptables/nftables/firewalld).

The hand-off wizard states the residual risk before applying: if the revert
itself fails, or the host reboots mid-window into a configuration that does
not come up, recovery requires console or out-of-band access, so the user
should not start the hand-off without it available.

### After creation

The new bridge appears in `virt.networks` and the create dialog. Domain XML
keeps referencing the bridge the user picked; if mode migration is ever
needed, the escape hatch is a libvirt network wrapping the bridge
(`<forward mode='bridge'/>`) so domains reference a stable logical name —
reassess from measured need.

## Out of scope

- macvtap attachment and any host-IP shim scheme (rejected above);
- VLAN sub-bridges, VLAN-aware bridges, and bond creation;
- multi-NIC VMs and editing NICs on existing VMs (no VM-edit surface
  exists);
- bridging wireless uplinks;
- a managed NAT network with LinuxIO-owned dnsmasq (TrueNAS's Incus detour
  collided with host DNS on port 53 and was reverted).
