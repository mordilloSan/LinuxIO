# Watched conditions: checks that raise alerts on a schedule

## Status

Not built. This is the agreed plan. It takes over the "Overflow alert" that
was planned under Memory overflow, and the "Storage, unit-failure, and
release-check sources" that [Alerts](../alerts.md#not-in-this-slice) lists as
not built.

What exists today: about 18 kinds of alert report the outcome of something
that ran (an update, a job, a snapshot), and five watch a condition, all of
them btrfs (`health`, `scrub`, `space`, `offline`, `resync`), from
`linuxio-worker health` every ten minutes. Nothing watches swap, a full
filesystem, a failing disk, temperature, a failed service or an md array.

## Problem

A host can run out of swap, fill a filesystem, lose a disk of an md array or
have a service fail while nobody is signed in, and LinuxIO says nothing.
[Alerts](../alerts.md) exist for exactly that case, but only work that
LinuxIO started reports into them.

The first of these to be planned was the overflow alert: "overflow nearly
full" when swap, or the zram device, is more than 80 percent used. Its plan
left one decision open, who raises it, and described the choice wrongly
("alerts are raised today from the btrfs timer": several programs raise
alerts, and the btrfs health run is only the one that watches a condition).

## Decisions

**Who raises them: `linuxio-worker check`, on one timer.** The reasons, in the
order they were weighed:

- Not the bridge. It exists only while someone is signed in, and a session
  without sudo cannot write `/var/lib/linuxio/alerts.json`. Swap that fills
  at night would raise nothing. The root job host does not fill the gap: it
  exits after 60 seconds without work.
- Not `linuxio-monitoring`. It has the swap numbers, but it would become a
  new writer of the alert store, with its sandbox opened for it, and every
  check would stop when it is down.
- Not a command of `linuxio`. `linuxio` holds admin commands only; what a
  timer runs as root is a word of `linuxio-worker`
  ([Process and systemd architecture](../process-systemd-architecture.md)).
- `linuxio-worker` is the root program timers already start to check things
  and raise alerts. It reads swap, filesystems, md arrays and systemd
  directly, so those checks need no other LinuxIO process.

**One runner, many checks.** TrueNAS does this with "alert sources": small
checks, each on its own schedule, in one place that raises, clears and
delivers. `linuxio-worker check` is that place. Adding a check later is one
file in the runner, not a new unit or a new word.

**Limits are fixed.** They are constants in the code and listed below, as in
TrueNAS, where pool capacity alerts at 85, 90 and 95 percent and nothing on
the settings screen changes that. There are no number fields to validate or
explain.

**A value at the line does not flip.** A check raises at its limit and clears
only below a lower one. Between the two it leaves the alert as it is. Whether
an alert is raised now is read from the alert store, so the rule needs no
state of its own.

**What can be set: each check on or off.** A check that is switched off
clears its alerts and is not run. Where a level goes (bell, email, webhook)
is the levels grid that exists. Btrfs health is not in the list and stays
always raised, as today.

**A check that cannot read its data changes nothing.** It neither raises nor
clears, as [Alerts](../alerts.md) asks of every source: only a check that saw
the condition gone resolves it.

## How it runs

```text
linuxio-check.timer   OnBootSec=3min, OnUnitActiveSec=5min, WantedBy=linuxio.target
linuxio-check.service Type=oneshot, ExecStart=/usr/local/bin/linuxio-worker check
```

One run goes through the checks that are due. A check has its own interval
(five minutes for swap and filesystems, longer for the rest). When each last
ran, and what a check must remember between runs (a disk's error counts), is
in `/var/lib/linuxio/checks.json`, `root:root` `0600`, written through
`atomicfile` like the other state files.

Alerts use the source `host` and one key per thing watched, so two full
filesystems are two alerts and each clears by itself. Each links to the page
that shows the condition.

The settings are one more member of `notifications.json`:

```text
checks    check ID -> { "off": true }                 absent: the check is on
          "units"  -> { "ignore": ["foo.service"] }   units the failed-service check skips
```

The settings page gets a section "Watched conditions": one row per check with
its switch, what raises it in one line, and for failed services the list of
ignored units.

## The checks

Levels are LinuxIO's three: notice (`info`), warning, alert (`error`).

| Check (ID) | Raised | Cleared | Level | Reads | Every |
|---|---|---|---|---|---|
| Swap nearly full (`swap`) | All swap together, or one zram device, is 80% used or more. *Overflow nearly full* | Below 70% | warning | `/proc/swaps`, `/proc/meminfo`, `/sys/block/zram*/mm_stat` | 5 min |
| Filesystem nearly full (`filesystem`) | Space or inodes of a mounted filesystem at 85%, 90% or 95% | One point under the level it is at lowers it; under 84% clears it | notice at 85%, warning at 90%, alert at 95% | `/proc/self/mountinfo` and `statfs`. Only filesystems on a block device; not btrfs (its own `btrfs/space`), not read-only images, not tmpfs or overlays | 5 min |
| md array degraded (`md`) | An array runs without one of its disks | The array is whole again, or gone | alert | `/sys/block/md*/md/degraded` | 5 min |
| Service failed (`units`) | A system unit is in the `failed` state and not on the ignore list | The unit is no longer failed, or gone | warning; alert for LinuxIO's own units | systemd over D-Bus (`backend/common/systemd`) | 5 min |
| Disk failing (`smart`) | The drive's overall health is failed, an NVMe critical warning is set, the last self-test failed, or reallocated, pending or uncorrectable sectors grew since the last check | Health passed and no count grew for a day | alert for failed health and the NVMe flag; warning for the rest | The SMART data `linuxio-monitoring` already collected, over `api.sock` | 30 min |
| Too hot (`temperature`) | A sensor is at its own critical limit, or a disk at the limit its drive reports | 3 °C under the limit | alert for a sensor's critical limit; warning for a disk | Sensors and disk temperatures from `linuxio-monitoring`, over `api.sock` | 5 min |
| Clock not synchronized (`ntp`) | Time synchronization is on and has not synchronized for an hour | Synchronized | warning | `org.freedesktop.timedate1` over D-Bus | 30 min |
| Certificate expiring (`certificate`) | LinuxIO's HTTPS certificate expires within 14 days. It renews itself 30 days ahead when the web server starts, so this means it has not restarted since | The certificate on disk is valid for longer | warning | The certificate file | daily |
| SSH logins failing (`ssh-logins`) | 50 or more failed SSH logins in the last 24 hours | Fewer than 25 | warning | `sshd`'s lines in the journal, through `journalctl` as `backend/common/scheduledrun/output.go` does | 30 min |
| LinuxIO release available (`release`) | PackageKit lists an update to the `linuxio` package | PackageKit lists none | notice | `FindUpdateByName` in `backend/bridge/handlers/packages`, the lookup behind `control.version` | daily |

Notes on single checks:

- **Swap.** A host with no swap raises nothing. The reader is the one the
  Memory overflow dialog uses, moved from `backend/bridge/handlers/memory`
  to `backend/common`, because a worker may import only `common`.
- **Service failed.** Units that already report through LinuxIO are left
  out, so nothing is reported twice: `linuxio-schedule-*`, and the units the
  run-record hook covers (Docker update, snapshots, replication, resync, the
  distribution's automatic update).
- **Disk failing and too hot.** `linuxio-monitoring` polls the disks with
  `smartctl` and leaves a sleeping disk asleep; a second poller would wake
  them. These two therefore depend on it. When it does not answer they
  change nothing, and the service check reports `linuxio-monitoring.service`.
- **LinuxIO release available.** This is the "release-check source" of
  [Alerts](../alerts.md#not-in-this-slice). It reads PackageKit's cache and
  refreshes nothing, so it sees a release only after the package lists are
  refreshed; a failed lookup changes nothing.

## Steps

Each step works by itself and ends with its docs updated and its row here
ticked.

### Step 1: the runner, the settings, and four checks that need nothing else

- [ ] A new package `hostcheck` under `backend/common`: the check interface (ID, interval,
  `Run` returning what to raise and what it saw clear), the runner (which
  checks are due, on or off, raise and resolve through
  `backend/common/alerts`), the rule between two limits, and `checks.json`.
- [ ] Move the swap and zram reader out of
  `backend/bridge/handlers/memory/read.go` into `backend/common`; the bridge
  keeps using it from there.
- [ ] The checks `swap`, `filesystem`, `md` and `units`, each with tests
  against a fake `/proc` and `/sys` root.
- [ ] `linuxio-worker check` (a new `check.go` in `backend/worker`), in the usage text
  and in the invocation map of
  [Process and systemd architecture](../process-systemd-architecture.md).
- [ ] `packaging/systemd/linuxio-check.service` and `.timer`, the installer's
  unit list and enabling, `make check-systemd`.
- [ ] `checks` in `NotificationSettings` (`backend/common/alerts/settings.go`)
  with validation and defaults, through the alerts routes
  (`backend/bridge/handlers/alerts`), `make generate`.
- [ ] The "Watched conditions" section in
  `frontend/src/routes/_authenticated/settings/-components/NotificationSettingsSection.tsx`.
- [ ] Docs: a row per check and the `checks` setting in
  [Alerts](../alerts.md), the alert in [Memory overflow](../memory.md)
  (and its "not built" line removed), this file's rows ticked.
- [ ] A VM spec: fill a small filesystem past 95%, fail a unit, switch a
  check off, and see each alert raised and cleared.

Size: about 18 files across the worker, `common`, packaging, the generated
API, the settings page and docs.

### Step 2: disk failing and too hot

- [ ] A reader of `api.sock` `/api/v1/live` in `backend/common`, taken from
  `backend/bridge/handlers/monitoring/client.go`; the bridge uses it from
  there.
- [ ] The checks `smart` and `temperature`, with the counts a disk had at the
  last check kept in `checks.json`.
- [ ] Docs: the two rows in [Alerts](../alerts.md), and in
  [Monitoring](../monitoring.md) that the worker reads the metrics socket.

Size: about 6 files.

### Step 3: clock, certificate, SSH logins and the release check

- [ ] The checks `ntp`, `certificate`, `ssh-logins` and `release`.
- [ ] Docs: their rows in [Alerts](../alerts.md); "Storage, unit-failure,
  and release-check sources" removed from its "Not in this slice".

Size: about 6 files.

## To settle while building

- Whether data space on a btrfs pool joins the `filesystem` check.
  `btrfs/space` watches metadata only; `statfs` on btrfs is approximate.
- Whether an administrator can install a certificate of their own. If so,
  `certificate` must also cover it, and "restart to renew" does not apply.
- Where each alert's link leads when the condition has no page of its own
  (a failed unit has the Services page; a full filesystem has Storage).

## Not planned

- Editable limits and a level per check. TrueNAS lets the level of each
  alert be changed; LinuxIO's levels grid already decides where each level
  goes, and a check with three levels has no single one to change.
- Moving btrfs health into the runner. It works, runs on its own timer, and
  its alerts are described with the filesystem.
- A network link that went down and an unhealthy container. Both raise
  often on a healthy host; they can be added as checks if wanted.
