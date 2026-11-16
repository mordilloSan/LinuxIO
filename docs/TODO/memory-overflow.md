# Memory overflow: swap, zswap and zram from the dashboard

## Status

The Memory overflow dialog on the dashboard, the three mutations and the
`zram` capability are built; what they do is in
[Memory overflow](../memory.md), including zram through `zram-generator` on
Fedora and RHEL. What is not built is below: the overflow alert. Open
limitations of what is built are in the [backlog](./backlog.md).

## Overflow alert

An alert "overflow nearly full" when swap, or the zram device, is more than 80
percent used. Nothing raises it today.

Open decision: who raises it. The monitoring daemon already samples swap every
few seconds, but alerts are raised today from the btrfs timer. Decide before
building it.

- [ ] Raise and resolve the alert from the chosen component, and describe it in
  [Alerts](../alerts.md).
