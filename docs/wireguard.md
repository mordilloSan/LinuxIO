# WireGuard Interfaces

How LinuxIO brings a WireGuard interface up and down, starts it at boot and
removes it. Code: `backend/bridge/handlers/wireguard`
(`interface_power.go`, `interface_boot.go`, `remove_interface.go`). An
interface's config is `/etc/wireguard/<name>.conf` and its exported peer files
are in `/etc/wireguard/<name>/`; both are wg-quick's own layout, so `wg-quick`
and `systemctl` work on an interface LinuxIO made, and LinuxIO lists one made
by hand.

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
create, down, up, a unit left active by a hand-run down, a failed start's
output, an interface outside its unit, an enabled interface across a reboot,
and removal. `make test-vm-browser VMS=1 PLAYWRIGHT_ARGS=wireguard.spec.ts`.

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
- The NAT hook `sysctl -w net.ipv4.ip_forward=1` is refused by Ubuntu's
  profile for `wg-quick`; see the [backlog](./TODO/backlog.md).
