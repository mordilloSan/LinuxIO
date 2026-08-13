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

### Backend writers

| Backend | Bridge representation |
|---|---|
| netplan | `bridges:` stanza with `interfaces:` and `macaddress` |
| systemd-networkd | new `.netdev` writer (`Kind=bridge`) + bridge `.network` + member `Bridge=` rewrite |
| NetworkManager | bridge connection + member slave connection keyfiles, `nmcli connection load`/`up` |
| ifupdown | `iface br0` stanza with `bridge_ports`; requires `bridge-utils` |
| ifcfg | `TYPE=Bridge` file + `BRIDGE=` on the member |

Backend support may ship incrementally (Debian/Ubuntu backends first);
unsupported or ambiguous layouts refuse with the existing structured errors.
The `bridge-utils` requirement on ifupdown hosts joins the capabilities
system with its install flow.

### Apply safety: snapshot, revert timer, check-in

The hand-off is a durable operation using the existing operation-record and
transient-unit machinery:

1. Snapshot every config file the change touches.
2. Arm a revert as a transient systemd timer/service (survives bridge and
   session death) that restores the snapshot and re-runs the backend apply
   after ~90 seconds.
3. Apply the new configuration. The connection may drop; the operation
   record already models this as an expected ambiguous outcome.
4. The frontend reconnects (same IP, per the MAC pinning above) and issues a
   confirmation Call that cancels the revert unit and finalizes the record.
5. No confirmation → the timer restores the previous configuration, and the
   operation record reports the revert honestly.

Bridge-creation mutations follow the existing rule: not `RetrySafe`. The
confirmation and explicit-revert Calls are idempotent. netplan's `netplan
try` and NetworkManager's D-Bus checkpoints offer native equivalents, but
the snapshot-plus-timer design is backend-agnostic and is the primary
mechanism.

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
