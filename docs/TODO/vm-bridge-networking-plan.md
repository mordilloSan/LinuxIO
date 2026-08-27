# VM Bridge Networking Plan

> **Status:** Phase 1 and Phase 2a are implemented. Phase 2b is implemented
> with native NetworkManager checkpoints and Netplan `Try` transactions.
> Bare systemd-networkd single-NIC handoff is deliberately unsupported.

LinuxIO gives a VM a physical-LAN presence by attaching it to a Linux bridge.
The host and VM can then communicate normally, unlike macvtap. Libvirt's
`default` NAT network remains the safe default.

The implementation has one ownership rule across all phases:

- Phase 1 consumes a bridge that already exists.
- Phase 2a asks the current network owner to create a bridge without moving
  the host IP.
- Phase 2b asks the current network owner to move the host IP using its native
  rollback transaction.

## Phase 1 — attach VMs to an existing bridge

Implemented behavior:

1. `virt.networks` returns libvirt's supported `default` NAT network and host
   Linux bridges. All libvirt network XML is still inspected so its backing
   bridges are not offered twice.
2. VM creation validates the selected source before storage mutation and emits
   either `<interface type="network">` or `<interface type="bridge">`.
3. The create dialog defaults to NAT. Home Assistant OS prefers the sole active
   host bridge when exactly one is available.
4. New domains include the QEMU guest-agent channel, and cloud images install
   `qemu-guest-agent`.
5. Bridged guest addresses are discovered best-effort through libvirt ARP,
   followed by the guest agent for unresolved interfaces.

No host network manager is involved. Hosts using an unsupported manager can
still attach VMs to bridges created outside LinuxIO.

## Phase 2a — create a bridge on a spare NIC

LinuxIO offers only wired physical interfaces that are not wireless, loopback,
already enslaved, or carrying non-link-local addresses/default routes. It also
reports Docker/iptables forwarding warnings without treating them as link
ownership.

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

Runtime ownership is resolved per interface. Mixed or unknown ownership is
refused.

### NetworkManager

1. Read the member's active profile with `GetSettings` and refuse non-Ethernet,
   already-enslaved, and 802.1X profiles. LinuxIO cannot safely copy 802.1X
   secrets.
2. Create a 90-second checkpoint with flags `0x02|0x04`.
3. Add a persistent bridge profile containing copies of the active `ipv4` and
   `ipv6` maps and the pinned MAC.
4. Add a persistent Ethernet port profile, retaining physical Ethernet
   settings, and activate both profiles.
5. Confirmation calls `CheckpointDestroy`; explicit revert calls
   `CheckpointRollback`; no confirmation lets NetworkManager roll back by
   itself.

### Netplan

1. Create a Netplan D-Bus configuration object.
2. `Set` deltas remove L3 keys from the member and place them on the bridge.
3. Call `Try(90)` and leave the transaction pending.
4. Confirmation calls `Apply`; explicit revert calls `Cancel`; no confirmation
   lets Netplan reject the change automatically.

Netplan remains the persistence owner even when it renders NetworkManager.

### Native systemd-networkd

Guided single-NIC handoff is refused with an instruction to define the bridge
in networkd configuration. networkd has no native timed rollback primitive;
LinuxIO does not ship a second rollback daemon to emulate one. Once that bridge
exists, Phase 1 can attach VMs to it.

## Durable operation record

The existing durable-task store keeps only the data needed after reconnect:

- operation ID and initiating numeric UID;
- backend, bridge, member, and confirmation deadline; and
- the NetworkManager checkpoint path or Netplan configuration path.

The route is host-exclusive, so two administrators cannot start overlapping
handoffs. Confirm/revert first claim the durable decision state and then call
the stored native D-Bus object. The client never supplies an arbitrary object
path.

There is no LinuxIO marker format, transient reverter service, startup recovery
hook, or hidden `revert-network` CLI. The root network daemon remains alive
outside the authenticated session and owns the timeout.

## Safety limits

- Rebooting during the confirmation window remains unsupported: native D-Bus
  transaction objects are not assumed to survive daemon or host restart.
- A process failure exactly while committing a confirmation can leave the
  durable result unknown. LinuxIO reports that state and asks the operator to
  inspect the console instead of claiming either outcome.
- A timeout is intentionally treated as a revert. Redoing a successful change
  is preferable to locking out the host.
- Complex or secret-bearing layouts are refused rather than partially copied.

## Verification

Automated coverage must include:

- network enumeration, domain XML, and ARP/guest-agent fallback;
- one-scan Phase 2a preflight and all three persistence backends;
- NetworkManager setting copies, 802.1X refusal, checkpoint handle lifecycle;
- Netplan `Set`/`Try` without early `Apply`, then confirm/revert calls; and
- UID-bound durable status, confirmation, explicit revert, and timeout.

Runtime testing should use console access. WSL can validate bridge XML,
attachment, spare-NIC creation, and link behavior, but its nested virtual
switch may not provide guest DHCP through a bridged WSL NIC.
