# Memory overflow: swap, zswap and zram from the dashboard

## Status

The Memory overflow dialog on the dashboard, the three mutations and the
`zram` capability are built; what they do is in
[Memory overflow](../memory.md). What is not built is below: the overflow
alert, and the Fedora/RHEL path for zram. Open limitations of what is built
are in the [backlog](./backlog.md).

## Overflow alert

An alert "overflow nearly full" when swap, or the zram device, is more than 80
percent used. Nothing raises it today.

Open decision: who raises it. The monitoring daemon already samples swap every
few seconds, but alerts are raised today from the btrfs timer. Decide before
building it.

- [ ] Raise and resolve the alert from the chosen component, and describe it in
  [Alerts](../alerts.md).

## Fedora and RHEL

The capability declares only the Debian package `zram-tools`, so Install on
Fedora and RHEL fails and the zram row cannot be chosen there. The Fedora
package is `zram-generator`, which has its own configuration file and no
service to enable: systemd generates its units, so the `EnableService` path
used for `zram-tools` does not apply as it is.

Open decision: how the mode applies a `zram-generator` configuration
(regenerate and start the generated unit, or ask for a restart), and how the
capability detects it, since it has no `zramswap` command.

- [ ] Declare the RHEL package, detect `zram-generator`, write its
  configuration with the same size, algorithm and priority as
  `/etc/default/zramswap`, and start and stop the generated unit through
  `common/systemd`.
- [ ] Give the stop and the room check the same safeguards as the Debian path,
  and run the root integration test on a Fedora VM.
