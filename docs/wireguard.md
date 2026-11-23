# WireGuard Interfaces

How LinuxIO brings a WireGuard interface up and down, starts it at boot,
checks that it works and removes it. Code: `backend/bridge/handlers/wireguard`
(`interface_power.go`, `interface_boot.go`, `remove_interface.go`,
`forwarding.go`, `check.go`, `check_probe.go`, `check_packet.go`). An
interface's config is `/etc/wireguard/<name>.conf` and its exported peer files
are in `/etc/wireguard/<name>/`; both are wg-quick's own layout, so `wg-quick`
and `systemctl` work on an interface LinuxIO made, and LinuxIO lists one made
by hand.

## Creating an interface

The create dialog (`CreateInterfaceDialog.tsx` in
`frontend/src/routes/_authenticated/network/-components/wireguard`) asks two
things: the interface's name, prefilled with the first free `wgN`, and how
many devices it is for, one by default. Each device is a peer with its own
client configuration (the request's `numPeers`); more can be added later.

Everything else is under **Advanced**, closed by default, with these
defaults:

- **Port**: the first free one from 51820.
- **CIDR**: the first free `10.10.N.0/24`, N from 20 in steps of 10.
- **NIC**, the network card the peers' traffic leaves by: the first ethernet
  or wireless card with an IPv4 address, else the first one listed.
- **DNS**, **Endpoint host** and **MTU**: empty, so the peers' DNS server
  is the NIC's gateway, their endpoint is the detected public IP and the
  interface's MTU is the one wg-quick picks.

A line under the visible fields names what will be used, such as
`Port 51820 · 10.10.20.0/24 · through enp1s0`, with the DNS, endpoint host
and MTU once set, so the choices made for the user are in view while
Advanced is closed. Advanced shows by itself while one of its fields stops
the create (a port or CIDR another interface uses, an empty port or CIDR, an
MTU out of range, no NIC to pick), so the reason is never hidden; a click on
Advanced then keeps it open, so fixing the field does not fold it away.

After the create the new interface becomes the selected one, `?iface=<name>`,
so its devices are listed, and when it has any the QR code of the first one
opens by itself, `?qr=<peer id>`: making a VPN ends with a phone scanning a
code, not with finding the interface, its peer and the QR button. The id is
the first entry of the interface's own peer list (`list_peers`), read once
the create succeeded. When that read fails the interface is still selected,
with no error, and the code is one click away. The QR dialog says how to
use it ("In the WireGuard app, tap + and choose Scan from QR code."), offers
the configuration file for a computer, and is the same whichever way it
opens: the QR button of a peer's card also sets `?qr=`, and closing it
clears it. `qr` is kept only beside `iface`, selecting another interface or
none clears it, and a `qr` that names no peer of the interface opens
nothing.

## One control: the interface's unit

`wireguard-tools` ships `wg-quick@.service`: `wg-quick up <name>` on start and
`wg-quick down <name>` on stop. LinuxIO brings an interface up by starting
`wg-quick@<name>.service` and down by stopping it, over D-Bus, and waits for
the job to end (`systemd.StartUnitAndWait`, `StopUnitAndWait`). Creating an
interface, and editing one in a way that needs a restart (port, MTU, hooks),
use the same two steps. "Start at boot" enables or disables the same unit.

Why not run `wg-quick` from the job host, as LinuxIO did before:

- A process the job host starts inherits what the host runs under. Ubuntu
  confines `wg-quick` with an AppArmor profile that runs `ip` and `wg` under
  profiles of their own, and under the job host's `NoNewPrivileges=yes` the
  kernel refuses that change (a confined program may then only move to a
  profile that is a subset of its own): every `wg-quick up` failed with
  "Operation not permitted". The unit is how the distribution expects
  `wg-quick` to run, and what its profile is written for.
- The flag stays on the root job host. Root itself has nothing to gain from
  an exec, but the host also runs commands as the signed-in admin (npm for a
  capability, `claude plugin`), and the flag is what keeps those away from
  `sudo`. Changing Ubuntu's profile was turned down too: it belongs to
  another package, and loosening it would loosen it for everyone on the host.
- The unit's state follows the interface. An interface LinuxIO brought up
  itself left its unit inactive, so `systemctl status` disagreed with the
  page, and `systemctl stop` did nothing.

Whether an interface is up is still read from the kernel (`wgctrl`), not from
the unit, so one brought up by hand shows as up.

### When the unit and the interface disagree

- **The unit is active and the interface is gone** (taken down with `wg-quick
  down` or `ip link delete` by hand). A start of an active unit does nothing,
  so the unit is stopped first. Its stop command fails, there being no
  interface, which leaves the unit failed; LinuxIO resets that
  (`clearStaleUnit`). Up then starts it; down has nothing more to do. "Gone"
  is read from `/sys/class/net`, not from `wgctrl`: a unit is stopped only for
  an interface that is definitely not there, never because WireGuard could
  not be asked.
- **The interface is up and the unit does not hold it** (brought up with
  `wg-quick up` by hand, or by a LinuxIO from before this). Down runs
  `wg-quick down` from the job host, the only place LinuxIO still does. Where
  `wg-quick` is confined that fails, as above, and the error says to run
  `wg-quick down <name>` as root (see Known limits). Up fails with wg-quick's
  "already exists", as it would by hand.
- **Both are down.** Down does nothing and succeeds.

### Failures and cancel

systemd says only that the unit failed. What `wg-quick` wrote is in the
journal, so a failed start or stop returns, with the error, the last 40 lines
the unit's invocation wrote since the job was asked for (`lastUnitOutput`:
`journalctl --since=@<time> _SYSTEMD_INVOCATION_ID=<id>`; the time leaves out
the lines of the start when a stop fails days later). A failed start leaves
the unit failed, as `systemctl start` does; the next start clears it.

The start belongs to systemd and carries on when the job is cancelled, so a
cancelled up stops the unit. systemd ends a `wg-quick up` that is still
running with SIGTERM, on which `wg-quick` deletes the link (see Known
limits). An up of an interface that is already up starts nothing, so
cancelling it stops nothing.

## Forwarding

The NAT rules below are `iptables` lines, and a host can have `wg-quick`
without `iptables` (a minimal Debian, some RHEL-family installs). Installing
the WireGuard capability from LinuxIO therefore installs `iptables` too
(`iptables-nft` on the RHEL family), and a create on a host without the
command is refused before anything is written, naming the package; it used to
fail at the first hook, with a config left behind that never came up. The
capability's detection still asks only for `wg-quick`, so a host whose
interfaces work without `iptables` keeps its page.

An interface LinuxIO creates gets NAT: PostUp lines that accept forwarded
packets between the interface and the chosen network card and masquerade
what leaves by it (`natPostUpHooks`), and PostDown lines that delete them.
Those rules pass nothing while the kernel's IPv4 forwarding
(`net.ipv4.ip_forward`) is off, as it is on a server with no Docker or
libvirt: peers connect, reach the server and nothing behind it.

LinuxIO turns forwarding on itself (`ensureForwarding`), in the one place
every up goes through (create, up, and the restart after an edit), before it
starts the unit:

- It writes `1` to `/proc/sys/net/ipv4/ip_forward` when the value is not `1`
  already, and reads it back. A write of `ip_forward` resets the
  per-interface settings to their defaults, so a host that forwards already
  is left alone.
- It writes `/etc/sysctl.d/90-linuxio-wireguard.conf`
  (`net.ipv4.ip_forward = 1`), which `systemd-sysctl` applies at boot, before
  an interface enabled at boot starts.
- When either fails, the up fails and says so, and the unit is not started.
  An interface that is up while its peers reach nothing is the failure
  nobody would be told about.

After the unit has started, LinuxIO turns on the interface's own switch
(`/proc/sys/net/ipv4/conf/<name>/forwarding`, `ensureInterfaceForwards`) when
it is off. That switch is what the kernel reads for a packet arriving on the
interface; a new interface takes it from `conf/default`, which a host can
have off while `ip_forward` is on, and the interface then came up forwarding
nothing. When that write fails the interface is taken down again and the up
fails: an up leaves a working interface or none.

"Start at boot" writes the same file before it enables the unit
(`ensureForwardingAtBoot`), and leaves the running value alone: an interface
LinuxIO did not bring up itself, up since before this LinuxIO or started
with `systemctl`, has no file yet.

Both do this for an interface whose PreUp or PostUp lines hold a masquerade
rule that is not `ip6tables`', or `net.ipv4.ip_forward=1`
(`wantsForwarding`), so an interface without NAT leaves the host as it is,
and one made by hand with the usual rules gets the same treatment. A config
LinuxIO cannot read is left to `wg-quick`, which says what is wrong with it
when the unit starts.

Why not a hook: LinuxIO used to write `sysctl -w net.ipv4.ip_forward=1` as
the first PostUp line. Ubuntu's AppArmor profile for `wg-quick` runs `sysctl`
under a profile that may write only `net.ipv4.conf.all.src_valid_mark`; the
write is refused, `sysctl` prints "permission denied on key
"net.ipv4.ip_forward", ignoring" and exits 0, and the unit finishes with
success. Seen in the test VM on Ubuntu 26.04 with forwarding off: the peer
reached the server's tunnel address and lost every packet sent beyond it.
Fedora's SELinux policy lets `wg-quick` run the line. A config from before
keeps its line, which now does nothing on Ubuntu but print that message into
the unit's journal; it can be deleted in the interface's hooks.

Removing an interface, or saving an edit of its hooks, deletes the sysctl.d
file once no config in `/etc/wireguard` needs forwarding
(`dropForwardingAtBoot`). The running value
is never turned off: Docker, libvirt or the administrator may count on it,
and LinuxIO cannot tell. Removing the LinuxIO package leaves the file, since
the interfaces stay and run without LinuxIO.

### When forwarding goes off later

Forwarding can be turned off while an interface is up (a `sysctl` by hand, a
hardening script), or be off after a boot, where a sysctl.d file that sorts
after LinuxIO's sets it to 0. Nothing else shows it: the interface is up and
its peers connect and handshake.

`wireguard.list_interfaces` therefore reports `forwardingOff` for an
interface that is up, whose config needs forwarding, while `ip_forward` or
the interface's own switch is off (`notForwarding`). The card shows "Not
forwarding" in place of "Active", with a warning and a "Turn on forwarding"
button. The button calls `wireguard.restore_forwarding` (`RestoreForwarding`),
which does what an up does for forwarding (the host's value, the sysctl.d
file, the interface's own switch) and neither starts nor restarts the
interface, so its peers stay connected. For an interface whose config needs
no forwarding it answers 409 and changes nothing.

An interface cannot be named `all` or `default`: those are the kernel's own
folders under `/proc/sys/net/ipv4/conf`, and a write for them would be the
host's switch, not an interface's.

## Connection check

An interface can be up, with its peers connecting and handshaking, and still
be of no use to them: forwarding off, a NAT rule missing, a firewall in the
way, a DNS server that does not answer. "Check Connection" on the interface's
card runs `wireguard.check_interface`, which plays a client on the server
itself and sends real traffic through the tunnel. The dialog opens with the
check and shows each step as it ended.

How it builds the client, in Go through `wgctrl` and netlink, with no
command (the job host's `NoNewPrivileges` keeps `wg`, `ip` and `ping` from
their AppArmor profiles on Ubuntu):

- A key pair is made in memory and its public key is added as a peer to the
  running device only, with the probe address as its one allowed IP: the
  first free host address of the interface's subnet, from offset 2 as for
  peers, that no peer of the config, exported peer file or device holds. The
  config file is never written, so nothing persistent changes.
- A WireGuard link (`linuxio.check`, a name LinuxIO refuses for an
  interface) is created in the host's namespace and configured with the
  server as its one peer, at `127.0.0.1:<listen port>`, allowed IPs
  `0.0.0.0/0`. It is then moved into an anonymous network namespace. A
  WireGuard link keeps its UDP socket in the namespace it was created in, so
  its encrypted packets still reach the server through the host's loopback,
  while everything sent inside the namespace has to go through the tunnel.
- The namespace has no name and lives only while the check holds a handle or
  a socket in it. If the job host dies mid-check, the kernel takes the
  namespace and the link with it.
- The check needs the root job host to be able to create a network namespace
  and raw sockets: hardening `linuxio-jobs@.service` with
  `RestrictNamespaces=`, a capability bounding set without `CAP_SYS_ADMIN` or
  `CAP_NET_RAW`, or `RestrictAddressFamilies=` would break it.
- The check holds the lock the config edits hold, so an edit or the sync
  after one cannot remove the temporary peer under it, and one check runs at
  a time. It is a `Mutation`, so it runs in the root job host beside those
  edits, and a lost answer is never sent again as a second check.
  `wireguard.list_peers` lists the exported peer files, so the temporary peer
  never shows on the page.
- Afterwards, whatever ended the check, the temporary peer is removed from
  the device, the link and the namespace are deleted, and a link of that name
  left in the host's namespace is deleted. A cleanup that fails is logged and
  does not turn a finished check into an error.
- Before it adds its peer, a check removes the probe peers an earlier check
  that died left behind: device peers whose key is in no config and whose one
  allowed IP is the probe address. A peer added by hand with `wg set` and not
  saved to the config looks the same; the next edit's sync removes such a
  peer as well.

The steps, always all four and in this order; a step whose prerequisite
failed is skipped:

1. **handshake**: the first packet through the link starts a handshake, and
   the server's device shows one for the temporary peer within 5 s. It proves
   the server answers on its port.
2. **tunnel**: the server's own tunnel address answers a ping through the
   tunnel. It proves traffic passes both ways.
3. **beyond**: the host's default IPv4 gateway answers a ping through the
   tunnel. Only a packet the server forwarded and masqueraded gets there, so
   it proves forwarding, the NAT rule and the firewall's forward rules. The
   step also passes when the DNS step got an answer from a server that is not
   one of this host's own addresses. Some gateways, cloud routers among them,
   never answer a ping, so a gateway that is silent through the tunnel is
   pinged once more from the host itself: only when it answers the host is
   the step failed. When it answers neither, and no outside DNS server
   answered, the step is skipped, saying it cannot be judged, rather than
   telling the admin that peers reach nothing. With no default gateway and no
   such DNS answer it is skipped too. The host's ping runs only after the
   tunnel's failed, so a healthy check takes no longer.
4. **dns**: the DNS servers the exported client configs carry (the
   interface's DNS, else the default gateway: `defaultClientDNS`) answer a
   query for the root's name servers sent through the tunnel; any reply to it
   counts, up to three servers are asked, and the step names the first that
   answered. With no DNS server it is skipped. It runs before "beyond" is
   decided.

Each probe has its own short wait (5 s for the handshake, 2 s for each
ping and each DNS server) and the whole check ends within 20 s. Above the
steps the dialog gives one verdict: the VPN works, how many steps failed, or
how many could not be checked. Under them it says that the check runs on the
server: it cannot see a router in front of it or a firewall outside it.

## Removal

Remove takes the interface down, disables its unit and then deletes the config
and the peer folder.

- An interface that is still up after the down failed keeps its config, and
  the removal fails: without the config nothing can take the interface down or
  run its PostDown hooks.
- The unit is disabled because an enabled one would fail at every boot once
  its config is gone, and a unit a failed start or stop left failed is reset,
  so it does not stay listed as a failure for an interface that no longer
  exists. Neither stops the removal when it fails; it is logged.

## Testing

`frontend/src/test/vm/wireguard.spec.ts` runs the routes against the installed
package in the test VM, on Ubuntu with wg-quick's AppArmor profile loaded:
create, made through the page's dialog with its port, CIDR and NIC set under
Advanced and checked to end on the first device's QR code with `iface` and
`qr` in the URL, down, up, a unit left active by a hand-run down, a failed start's
output, an interface outside its unit, an enabled interface across a reboot,
and removal. It starts with forwarding off, and a peer in a network namespace
of the VM, built from the interface's exported client, pings the VM's gateway
through the tunnel after the create, after an up that found forwarding turned
off again, and after a reboot with Docker disabled, so that nothing but the
sysctl.d file turns forwarding on in that boot. The file is deleted before
"start at boot", which has to write it again. It also turns forwarding off
under a running interface, reads `forwardingOff` from the list, clicks "Turn
on forwarding" on the page and checks that the link was not replaced, and
brings the interface up on a host whose `conf/default` has forwarding off.
It runs the connection check through the route, where all four steps pass,
and with forwarding off, where the handshake and the tunnel pass and
"beyond" fails, then from the card's dialog, and checks after each that the
device's peers, the listed peers, the links and the named namespaces are what
they were.
`make test-vm-browser VMS=1 PLAYWRIGHT_ARGS=wireguard.spec.ts`.

## Known limits

- On a distribution that confines `wg-quick` (Ubuntu 26.04), LinuxIO cannot
  take down an interface that was brought up outside its unit: the job fails
  and names the command to run. An interface LinuxIO brought up, or systemd
  at boot, is not affected.
- A cancelled up does not undo the PostUp lines that had already run: the
  stop ends `wg-quick up` before the unit's stop command would run PostDown.
  Interrupting `wg-quick up` by hand does the same. Bringing the interface up
  and down again runs both.
- `wg-quick@.service` sets `WG_ENDPOINT_RESOLUTION_RETRIES=infinity`, so an
  interface with a peer `Endpoint` whose name does not resolve stays starting
  until the job is cancelled; run by hand, `wg-quick` gives up after 15 tries.
  The configs LinuxIO writes give the server's peers no endpoint.
- Where a sysctl.d file that sorts after `90-linuxio-wireguard.conf` sets
  `net.ipv4.ip_forward` to 0, forwarding is off after every boot: the card
  says so and "Turn on forwarding" lasts until the next one. LinuxIO does not
  override that file.
- The warning looks at the host's switch and the interface's own, not at the
  switch of the network card the replies come in by, and not at the firewall
  rules.
- An interface that was up and set to start at boot before this LinuxIO was
  installed gets the sysctl.d file at its next up or "start at boot" from
  LinuxIO, not at the upgrade.
- NAT is IPv4 only: the rules are `iptables` rules and only
  `net.ipv4.ip_forward` is set.
- The connection check runs on the server: it cannot tell whether a router
  forwards the UDP port to it, or whether a firewall outside it lets the
  port through.
- The connection check is IPv4 only: an interface with no IPv4 address is
  refused, and only IPv4 DNS servers are asked.
- A job host killed during a connection check leaves the temporary peer on
  the running device (never in the config) until the next check, the next
  edit's sync or the interface's restart removes it. The namespace and the
  link go with the process.
