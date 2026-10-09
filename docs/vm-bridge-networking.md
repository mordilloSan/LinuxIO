# VM Bridge Networking

LinuxIO gives a VM a physical-LAN presence by attaching it to a Linux bridge.
The host and VM can then communicate normally, unlike macvtap. Libvirt's
`default` NAT network remains the safe default.

Bridge setup runs from the LinuxIO UI through the host's existing network
manager, without manual configuration or additional networking packages, on
Netplan and NetworkManager hosts. Phase 1 attaches VMs to any existing bridge;
Phase 2a creates a bridge on a spare NIC; Phase 2b moves the host's management
IP onto a new bridge inside the manager's native rollback transaction. ifupdown
hosts are out of scope by design and get a manual recipe. See
[Deliberately not done](#deliberately-not-done) for the gaps that were closed
as not worth their cost.

Implementation coverage:

| Host networking owner | Phase 2a: spare NIC | Phase 2b: move host IP |
| --- | --- | --- |
| NetworkManager | Implemented; refuses NICs that already have a profile | Implemented; dynamic IPv6 identity is re-derived on the bridge |
| Netplan | Implemented with renderer/configuration restrictions | Implemented with configuration restrictions |
| Native systemd-networkd | Implemented | Missing |
| ifupdown / interfaces-based Debian | Out of scope; manual recipe shown | Out of scope; manual recipe shown |
| Legacy ifcfg / network service | Missing | Missing |

An ifcfg profile managed by NetworkManager belongs to the NetworkManager row.
The legacy ifcfg row refers to hosts using the network service and ifup/ifdown.
LinuxIO's ordinary network settings recognize these configuration backends;
that support does not include bridge creation for all of them.

Retain one ownership rule across all phases:

- Phase 1 consumes a bridge that already exists.
- Phase 2a asks the current network owner to create a bridge without moving
  the host IP.
- Phase 2b moves the host IP through the current network owner, using its native
  rollback transaction. Owners without one (networkd, ifupdown) are not
  automated.

## Phase 1 — attach VMs to an existing bridge

Implemented behavior:

1. `virt.networks` returns libvirt's supported `default` NAT network and host
   Linux bridges. All libvirt network XML is still inspected so its backing
   bridges are not offered twice.
2. VM creation validates the selected source before storage mutation and emits
   either `<interface type="network">` or `<interface type="bridge">`.
3. The create dialog defaults to NAT. Home Assistant OS prefers the sole active
   host bridge only when it has a live physical Ethernet uplink. Other bridges
   remain available for explicit selection. When a bridge-preferring image
   stays on NAT, the dialog warns that the VM is reachable only from the host
   and cannot discover LAN devices, and points at **Create LAN bridge**.
   **Create LAN bridge** beside the network selector opens the existing bridge
   setup. It can create a bridge on a spare wired NIC or guide the host-IP move
   below. Successful setup selects the new bridge and retains the VM form;
   canceling or reverting leaves its network selection unchanged. A page refresh
   recovers a pending handoff, but does not retain the unsaved VM form.
4. New domains include the QEMU guest-agent channel, and cloud images install
   `qemu-guest-agent`.
5. Bridged guest addresses are discovered best-effort through libvirt ARP,
   followed by the guest agent for unresolved interfaces. Discovery has a
   three-second budget that interrupts blocked transport reads; missing address
   data does not prevent VM list or detail results.

Phase 1 does not configure the host network manager. It can use an existing
bridge regardless of who created it.

## Phase 2a — create a bridge on a spare NIC

LinuxIO offers only wired physical interfaces that are not wireless, loopback,
already enslaved, or carrying non-link-local addresses/default routes.

Docker loads `br_netfilter` and sets the iptables `FORWARD` policy to `DROP`,
which drops VM↔LAN frames on any Linux bridge. The installers ship
`packaging/libvirt/hooks/qemu.d/linuxio-bridge-netfilter` into
`/etc/libvirt/hooks/qemu.d/`; on every domain start it inserts
`FORWARD -i <bridge> -o <bridge> -j ACCEPT` (IPv4 and IPv6) for each host bridge
the domain attaches to, skipping libvirt's own `virbr*`. Bridge options only
warn about the `DROP` policy when that hook is not installed.

The current request performs one inventory and ownership scan. The selected
backend then rechecks its mutation boundary and owns persistence:

- **NetworkManager:** creates persistent bridge and port profiles inside a
  checkpoint. Rollback uses checkpoint flags `0x02|0x04`, so NetworkManager
  deletes the new profiles and disconnects new devices itself.
- **Netplan:** writes a D-Bus configuration transaction, calls `Try`, verifies
  the link, and calls `Apply`. Failure calls `Cancel`.
- **Native systemd-networkd:** writes `.netdev`/`.network` files atomically,
  reloads/reconfigures networkd, and restores those files synchronously on
  failure. This is safe here because the spare NIC does not carry management
  L3 state.

## Phase 2b — move a management NIC onto a bridge

This flow requires explicit console/out-of-band acknowledgement. It preserves
the member's L3 configuration, pins the bridge MAC to the live member MAC, and
verifies that the member, addresses, and default route moved before offering
confirmation.

The current runtime ownership check resolves NetworkManager and networkd per
interface and refuses mixed or unknown ownership. An interface configured in
`/etc/network/interfaces` is reported as ifupdown with the manual recipe below.

### NetworkManager

1. Read the member's active profile with `GetSettings` and refuse non-Ethernet,
   already-enslaved, and 802.1X profiles. LinuxIO cannot safely copy 802.1X
   secrets.
2. Create a 90-second checkpoint with flags `0x02|0x04`.
3. Add a persistent bridge profile containing copies of the active `ipv4` and
   `ipv6` maps, connection policy including the firewall zone, and the pinned MAC.
   Dynamic IPv6 (`auto`/`dhcp`) is pinned to the cloned MAC on the bridge:
   `addr-gen-mode=eui64`, `ip6-privacy=0`, `dhcp-duid=ll`, `dhcp-iaid=mac`.
   Stock profiles use stable-privacy addresses and interface-name DHCPv6
   identities that cannot cross the rename, so the host may get a different
   global IPv6 address once; verification therefore requires the original IPv4
   addresses and default route on the bridge, not the original IPv6 addresses.
4. Add a persistent Ethernet port profile, retaining physical Ethernet
   settings, and activate both profiles.
5. Confirmation calls `CheckpointDestroy`; explicit revert calls
   `CheckpointRollback`; no confirmation lets NetworkManager roll back by
   itself. Explicit rollback checks every device result returned by
   NetworkManager before reporting success.

### Netplan

1. Create a Netplan D-Bus configuration object.
2. `Set` deltas remove L3 keys from the member and place them on the bridge,
   preserving the member's effective renderer.
3. Call `Try(90)` and leave the transaction pending.
4. Confirmation calls `Apply`; explicit revert calls `Cancel`; no confirmation
   lets Netplan reject the change automatically.

Netplan remains the persistence owner even when it renders NetworkManager.

### Native systemd-networkd

Guided handoff is refused because networkd has no native timed rollback
primitive; the message tells the operator to define the bridge in networkd
configuration, after which Phase 1 attaches VMs to it.

### ifupdown / interfaces-based Debian

Out of scope by design: ifupdown has no transactional rollback and
`systemctl restart networking` drops every interface, so a guided handoff
cannot be made safe without replacing the network manager. Both bridge flows
report the interface as ifupdown-managed and give the manual recipe: add a
`br0` stanza (`iface br0 inet dhcp`, `bridge_ports <nic>`) carrying the NIC's
address settings, set the NIC to `inet manual`, restart networking, then pick
`br0` as the VM network. Phase 1 attaches VMs to that bridge like any other.

## Durable operation record

The existing durable-task store keeps only the data needed after reconnect:

- operation ID and initiating numeric UID;
- backend, bridge, member, and confirmation deadline; and
- the NetworkManager checkpoint path or Netplan configuration path.

The route is host-exclusive, so two administrators cannot start overlapping
handoffs. Confirm/revert first claim the durable decision state and then call
the stored native D-Bus object. The client never supplies an arbitrary object
path.

The network and VM pages store the operation UUID in validated `handoffOperationId`
URL search before starting the mutation. Refreshing that URL resumes status
polling for the same UID-bound operation. A new start reconciles expired records
under the store's exclusive lock, including records owned by another UID,
without exposing their contents. Recovery allows for bounded native start and
decision calls before releasing exclusivity.

There is no LinuxIO reverter service or startup recovery hook. NetworkManager
or Netplan owns the timeout outside the authenticated session.

## Safety limits and invariants

- Rebooting during the confirmation window is unsupported: native D-Bus
  transaction objects are not assumed to survive daemon or host restart. The
  new profiles stay on disk, so a working change becomes permanent and a broken
  one needs a console fix.
- A process failure exactly while committing a confirmation can leave the
  durable result unknown. LinuxIO reports that state and asks the operator to
  inspect the console instead of claiming either outcome.
- A recorded apply or rollback failure also remains unknown after expiry;
  elapsed time does not prove that native recovery succeeded.
- A timeout is intentionally treated as a revert. Redoing a successful change
  is preferable to locking out the host.
- Complex or secret-bearing layouts are refused rather than partially copied.
- Dynamic IPv6 needs a portable address and DHCP identity. NetworkManager
  handoff re-derives it from the cloned MAC on the bridge and accepts that the
  host's global IPv6 address may change once. Netplan DHCPv6 and
  NetworkManager-rendered dynamic IPv6 are refused. Netplan with networkd
  supports static IPv6 and EUI64 SLAAC without privacy extensions. LinuxIO
  never converts a dynamic address into a static one to make verification pass.

## Deliberately not done

Closed on 2026-10-09 as not worth their cost for the targeted hosts (Debian
stable, Ubuntu LTS, current and previous Fedora):

- **Session-independent recovery.** It would only change the outcome when a
  broken bridge configuration coincides with a reboot inside the 90-second
  window. NetworkManager and Netplan already revert on timeout; the reverter
  service, boot ordering and state directory are not justified by that double
  failure.
- **Native systemd-networkd host-IP handoff.** Hosts running networkd without
  Netplan are outside the target list, and the feature needs the recovery
  mechanism above. Those operators get a clear message and write the bridge
  themselves.
- **Debian ifupdown automation.** No transactional rollback, and restarting
  `networking.service` drops every interface. The manual recipe is the product.
- **Legacy ifcfg/network-service hosts, bond/VLAN uplinks, spare-NIC bridges on
  NICs that already carry a NetworkManager auto-profile.** Revisit only when a
  real host needs them.

## Verification

Automated coverage includes:

- network enumeration, domain XML, and ARP/guest-agent fallback;
- one-scan Phase 2a preflight and all three persistence backends;
- NetworkManager setting copies, 802.1X refusal, checkpoint handle lifecycle;
- Netplan `Set`/`Try` without early `Apply`, then confirm/revert calls; and
- UID-bound durable status, confirmation, explicit revert, timeout, and
  exclusive claims after abandoned operations;
- transport interruption during blocked address discovery; and
- real browser navigation and refresh recovery without a second Start;
- bridge setup from VM creation, preserving its form and selecting only a
  successfully created or confirmed bridge;
- NetworkManager dynamic-IPv6 pinning, IPv4-only handoff verification, the
  ifupdown recipe in both flows, and hook-aware firewall warnings.

Runtime verification still pending on representative hosts (unit tests,
browser fixtures and WSL do not establish host-IP migration or guest LAN
connectivity):

- [ ] Ubuntu Server (Netplan/networkd) with Docker: handoff from the Create VM
  dialog, Home Assistant reachable from the LAN and from a container, and still
  reachable after a host reboot (exercises the libvirt hook).
- [ ] Stock Fedora/NetworkManager host with `ipv6.method=auto` defaults: handoff
  succeeds and the host keeps its IPv4 address.
- [ ] ifupdown Debian host: both bridge flows show the recipe; a hand-made
  `br0` is attached and reachable.

Use console access during host-level tests.
