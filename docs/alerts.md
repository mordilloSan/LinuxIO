# Alerts

LinuxIO's persistent, user-facing alert list: conditions that need an
administrator's attention and outcomes that happened while nobody was logged
in. It is deliberately small. There is no daemon, no database, no delivery
layer, and no new binary. Alerts are records in one root-owned JSON file;
existing root processes write them, root bridges read them.

Alerts are separate from live Tasks, journald logs, and transient toasts.
A failed operation may raise an alert, but the alert does not copy the Task,
run, or journal. Ordinary log lines never become alerts.

## Reference behaviour

Unraid stores notifications as files in two directories and has done so for a
decade. Proxmox has no alert store at all and hands `package-updates` to the
host's own mail. This design sits between the two: a store, but a file.

## Storage

| Path | Owner | Purpose |
|---|---|---|
| `/var/lib/linuxio/alerts.json` | `root:root` `0600` | All alert records |
| `/var/lib/linuxio/alerts.json.lock` | `root:root` `0600` | `filelock` sidecar |
| `/var/lib/linuxio/notifications.json` | `root:root` `0600` | Delivery settings, including the SMTP password; see [Delivery](#delivery) |

The file is written with the same pattern as `docker-update-status.json`:
take the lock, read, modify, write atomically. The shared package is
`backend/common/alerts`. The Docker update worker, the `linuxio` CLI, the
root job host, the boot job reconcile, and privileged bridges all use it.
Nothing else opens the file.

Only root can read the file, so only privileged sessions see alerts. That is
the audience rule. Unprivileged sessions get an empty list.

## Record

```text
id                 "<source>/<key>", stable identity
source, key        lower-case [a-z0-9._-]
severity           info | warning | error
title, message     bounded text; message optional
link               optional in-app path starting with "/"
occurrence_id      source-defined identity of the latest material occurrence
occurrence_count   how many material occurrences
first_occurrence, last_occurrence, last_observed_at
dismissed_at       nullable
seen               map of uid -> occurrence_count that uid has seen
```

The store normalises what it is given instead of rejecting it: the key is
lower-cased, characters outside `[a-z0-9._-]` become `-`, a leading `.`, `_`
or `-` gets an `x` prefix, and it is cut to 64 characters; title and message
are truncated to their limits with an ellipsis. Callers do not normalise.
A blank title, a bad severity, source or link are still errors.

Lifecycle:

- **Raise** with a new `(source, key)` creates an active record.
- **Raise** with an existing key and the same `occurrence_id` only updates
  `last_observed_at`. Repeated observation of an unchanged condition is
  silent: no count change, no seen reset, no undismiss.
- **Raise** with a different `occurrence_id` is a material change: the count
  increments, `last_occurrence` moves, title/message/severity are replaced,
  the alert becomes unseen for everyone, and a dismissal is cleared.
- **Resolve** removes the record. A later raise starts a fresh record with
  count 1 and no seen state, which is correct: the condition recurred.
  Resolved history lives in logs and run status, not here.
- **Dismiss** hides an active record until the next material change.
- **Seen** is per UID. An alert is unseen for a user when their recorded count
  is lower than `occurrence_count`.

A source only resolves after confirming the condition is gone. Failed or
incomplete checks leave the record alone.

## Who writes, with no session

| Source | Writer | Raise | Resolve |
|---|---|---|---|
| Automatic updates | `ExecStopPost=` drop-in on the provider's upgrade service, running `linuxio alert auto-update` | A run that matches the configured policy: failed, installed packages, or always | Never; each run is a new occurrence of the one `auto-update/run` record |
| Docker update outcome | `linuxio-docker-update` worker, already root | A scheduled update fails or the replacement container is not healthy. One record per container name | The next successful update of that container |
| Jobs | The root job host (`linuxio-jobs@0`) and `linuxio-bridge jobs reconcile` at boot; a user's job host raises none | `jobs/<job id>`: a root job failed, or was interrupted by a shutdown, a host crash or a reboot (not when the boot reconcile resumes it) | Never; dismissed by the user |
| Btrfs health | `linuxio-btrfs-timer health run` every ten minutes, and the `storage.btrfs_list` bridge route | `btrfs/health-<uuid>`, `btrfs/scrub-<uuid>`, `btrfs/space-<uuid>`, `btrfs/offline-<uuid>`, `btrfs/resync-<uuid>`; see [Btrfs management](./btrfs.md#filesystem-health) | The next check that shows the condition cleared, or the pool gone |
| Btrfs missing disks | `linuxio-btrfs-timer degraded run` at boot and `linuxio-btrfs-timer resync`; see [Missing disks](./btrfs.md#missing-disks) | At once rather than at the next health run: `btrfs/health-<uuid>` for a pool mounted without a disk, `btrfs/offline-<uuid>` with the reason a pool was left unmounted, `btrfs/resync-<uuid>` when a resync gives up, and the info `btrfs/resynced-<uuid>` when it finished | As Btrfs health; `resynced` stays until dismissed, or until the next resync starts |
| Btrfs snapshots | `linuxio-btrfs-timer snapshot` (hourly timer, pre-update hook, Run now) | `btrfs/snapshot-<uuid8>-<path>`: a failed snapshot or an unmounted filesystem | The next successful run for that rule |
| Btrfs replication | `linuxio-btrfs-timer snapshot run` and Run now | `btrfs/replication-<job id>`: a failed replication run | The next successful run of that job |

Later sources follow the same shape: a root process that already knows the
outcome calls `alerts.Raise` or `alerts.Resolve`, or a unit drop-in runs
`linuxio alert raise`. Storage checks (capacity, SMART, degraded pools) would
be a oneshot timer; they are out of this slice.

## Automatic updates notification policy

The automatic-updates settings gain one option, with the vocabulary
unattended-upgrades uses for its own mail reports:

```text
notify: never | on_failure | on_change | always
```

`on_change` means a run that installed something. `always` includes runs that
found nothing. The on-disk drop-in is authoritative; no drop-in means `never`.

The drop-in is `/etc/systemd/system/<upgrade-service>.d/linuxio-alert.conf`:

```ini
[Service]
ExecStopPost=/usr/local/bin/linuxio alert auto-update --provider apt --policy on_change
```

`ExecStopPost=` runs on success and failure and receives `$SERVICE_RESULT`
and `$INVOCATION_ID`. The command reads the provider's run evidence
(unattended-upgrades' log file for APT; service result only for the others),
applies the policy, checks `/run/reboot-required`, and raises or stays quiet.

| Provider | Upgrade service |
|---|---|
| APT (Ubuntu, Debian) | `apt-daily-upgrade.service` |
| DNF4 | `dnf-automatic.service` |
| DNF5 | `dnf5-automatic.service` |
| Linux Mint | `mintupdate-automation-upgrade.service` |

Email stays with the providers. unattended-upgrades has `Mail` and
`MailReport`, dnf-automatic has an `email` emitter. Exposing those is a later
settings addition, not a delivery layer.

## CLI

```text
linuxio alert raise --source S --key K --title T [--severity info|warning|error]
                    [--message M | --message-stdin] [--link /path] [--occurrence ID]
linuxio alert resolve --source S --key K
linuxio alert auto-update --provider apt|dnf|mint --policy on_failure|on_change|always
```

All three need root. A permission error prints "This command requires sudo"
like `linuxio verbose` does.

## Bridge API

All routes are `Privileged`. Mutations return the fresh list so the frontend
can replace its cache without a second round trip.

| Route | Request | Result |
|---|---|---|
| `alerts.list` | none | `AlertList` |
| `alerts.mark_seen` | `{ ids }` | `AlertList` |
| `alerts.mark_all_seen` | none | `AlertList` |
| `alerts.dismiss` | `{ id }` | `AlertList` |
| `alerts.get_notifications` | none | `NotificationSettingsView` (no password) |
| `alerts.set_notifications` | `NotificationSettings` | `NotificationSettingsView` |
| `alerts.test_notifications` | none | success, or the delivery error |

`AlertList` is `{ alerts, unseen }`. `alerts` excludes dismissed records.
`seen` on each alert is for the calling UID.

There is no watch Channel. The navbar query refetches on window focus and on
an interval; mutations replace the cache from their result. Add `alerts.watch`
only if that latency turns out to matter.

## Delivery

**Settings → Notifications** sends alerts beyond the bell, with no extra
binary: Go's HTTP client and SMTP client do the sending.

- **Send alerts** picks the least severe alert delivered: errors only,
  warnings and errors, or everything. Off delivers nothing.
- **Webhook**: one JSON `POST` per alert with `host`, `source`, `severity`,
  `title`, `message`, `link` and `time`, plus `text` and `content` holding a
  one-line summary so Slack- and Discord-style webhooks accept it as is. A
  status of 300 or above is a failure.
- **Email**: SMTP with implicit TLS on port 465, otherwise STARTTLS when the
  server offers it. A password is only sent over TLS (Go's `PlainAuth`
  refuses otherwise). Header fields are folded to one line, so alert text
  cannot add headers. Addresses are validated on save.

An alert is delivered when `Raise` creates it or records a new occurrence,
after the store is written; re-observing the same occurrence sends nothing.
Each delivery is bounded to 15 seconds and its failure is logged, never
returned, since the alert is already recorded. The password is never sent
back to the browser: the API reports `smtpPasswordSet`, and a save with an
empty password keeps the stored one unless the username was cleared.
**Send test** delivers a test alert through the saved channels whatever the
minimum severity.

The settings file sits beside `alerts.json`, so tests that point the store
at a temporary directory never read a host's real delivery settings.

## Navbar

The bell shows persisted alerts and live transfers. The localStorage toast
history is removed; toasts still pop, they are not kept. Opening the panel
marks everything seen. Each alert has a dismiss action. The badge is the
unseen count.

## Not in this slice

- Per-source minimum severity. The auto-update policy lives in its own
  settings.
- Matchers, digests, push services other than webhooks.
- `alerts.restore`, `alerts.mark_unseen`, resolved history.
- Storage, scheduled-task, unit-failure, and release-check sources.
- Alerts raised by unprivileged sessions.
