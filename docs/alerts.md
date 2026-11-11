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
host's own mail. This design sits between the two: a store, but a file. The
settings follow Unraid's: per kind of work what is reported, and a grid of
level by channel.

## Storage

| Path | Owner | Purpose |
|---|---|---|
| `/var/lib/linuxio/alerts.json` | `root:root` `0600` | All alert records |
| `/var/lib/linuxio/alerts.json.lock` | `root:root` `0600` | `filelock` sidecar |
| `/var/lib/linuxio/notifications.json` | `root:root` `0600` | The [notification settings](#notification-settings), including the SMTP password |
| `/var/lib/linuxio/notifications.json.lock` | `root:root` `0600` | `filelock` sidecar of the settings |
| `/var/lib/linuxio/notifications.json.unreadable` | `root:root` `0600` | A copy of a settings file that did not parse, kept when a save from the settings page replaced it ([below](#notification-settings)) |

Both files are written with the same pattern as `docker-update-status.json`:
take the lock, read, modify, write atomically (a temporary file and a rename,
so a reader without the lock only ever sees a whole file). The shared package
is `backend/common/alerts`. The Docker update worker, the `linuxio` CLI (also
as the scheduled units' end hook), the btrfs timer program, the root job host
and privileged bridges all use it. Nothing else opens the files.

Only root can read the files, so only privileged sessions see alerts. That is
the audience rule. Unprivileged sessions get an empty list; a user's own job
host raises no alert and reads no settings.

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

The page and the settings call the three severities **levels**: `info` is a
*notice*, `warning` a *warning*, `error` an *alert*.

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
- **Eviction.** The store keeps at most 200 records. A raise into a full store
  evicts the oldest dismissed record; when none is dismissed, the oldest
  record of the lowest level present (notices before warnings, warnings
  before alerts). Only stored records are weighed, not the one being raised:
  a notice raised into a store of 200 undismissed alerts still evicts the
  oldest alert.

A source only resolves after confirming the condition is gone. Failed or
incomplete checks leave the record alone.

## Notification settings

`notifications.json` holds what each kind of work reports besides its
failures, where each level goes, each schedule's own choice, and the delivery
channels.

```text
report    kind -> what it reports besides failures
levels    level (info | warning | error) -> { browser, email, webhook }
scripts   schedule ID -> failures | every_run, overriding report.scheduled_scripts
minSeverity, dockerUpdates      the older members, still written (below)
dockerUpdatesAvailable          also report each container with a newer image
webhookUrl, smtpHost, smtpPort, smtpUsername, smtpPassword, emailFrom, emailTo
```

**Failures are always reported.** No setting stops a failure from being
raised, and an alert (`error`) always reaches the browser: a stored
`levels.error.browser: false` is read and saved as true. What a result is: a
failure is an alert; a run cut short by an orderly shutdown, a reboot or a
power loss is a warning; a job host that crashed under a job, a job that could
not be resumed after a reboot, and a script whose end hook never reported are
alerts, since their outcome is unknown; a run that completed, or changed
something, is a notice. A cancelled job or run reports nothing. No report
setting decides whether a failure or an interruption is raised; a warning then
follows the levels grid like any other warning.

| Kind (`report` key) | Offered values | Default |
|---|---|---|
| Docker updates (`docker_updates`) | `failures`, `changes`, `every_run` | `failures`, or `changes` where an older file says `dockerUpdates: on_change` |
| Scheduled scripts (`scheduled_scripts`) | `failures`, `every_run` | `failures` |
| Snapshots and replication (`snapshots`) | `failures`, `changes`, `every_run` | `failures` |
| Pool resync (`resync`) | `failures`, `changes` | `changes`: a pool that is whole again keeps being reported |
| Jobs started in the browser (`browser_jobs`) | `failures`, `long_jobs` | `failures` |

System updates are not in this file: their choice is the provider's drop-in
([below](#automatic-updates-notification-policy)). Storage health has no
setting and is always raised. A job is long when it ran five minutes or more
(`alerts.LongJob`). A schedule's own choice (`scripts`) is `failures` or
`every_run`; it is kept here, keyed by the schedule's ID, and never in the
schedule's definition file, whose text decides the script path and the unit:
a new member there would change every schedule and make an older build refuse
to list them.

**Defaults** (no file): every level reaches the browser, alerts also reach
email and webhook once those are set up, and every kind reports as in the
table.

**An older file loads into the behaviour it had.** When the file names no
`levels`, each level reaches the browser and goes by email and webhook when it
is at or above `minSeverity` (none when `minSeverity` is empty). When `report`
does not name a kind, it takes its default; for Docker updates that is
`changes` where `dockerUpdates` is `on_change` and `failures` otherwise
(`never` included). Stored `levels` and `report` entries win over the older
members. A stored report or script choice that its kind does not offer (a hand
edit, or a newer build's value after a downgrade) is dropped on load and takes
its default instead of failing every later save. Loading never rewrites the
file.

The levels keep what was sent before, with one exception: an interruption by
a shutdown or a reboot was an alert in the builds before these settings and is
a warning now. On a host whose older file said "Errors only" (`minSeverity:
error`), such a run is therefore no longer emailed or posted after the
upgrade unless *Warnings* is ticked for that channel. The migration does not
tick it, since that would send every other warning too.

**An older build keeps reading the file.** New members are only added, none
changes meaning, and unknown members are ignored. Every save writes
`minSeverity` as the least severe level ticked for email or webhook (empty
when none is) and `dockerUpdates` as `on_change` for a Docker report of
`changes` or `every_run`, otherwise `on_failure`. So after a save, an older
build delivers by its least severity and reports Docker updates by its policy,
and a stored Docker `never` becomes `on_failure`.

**Writers.** Every write is one locked read, change and atomic write under
`notifications.json.lock`, so the settings page and a schedule's dialog
cannot lose each other's change. The settings page (`alerts.set_notifications`)
sets every member it shows and keeps the stored `scripts`; a schedule's
create, update and delete (`schedules.*`) change only that schedule's entry
in `scripts`. Readers do not take the lock.

**A file that does not parse.** Its parse error names the path and a byte
offset and never quotes the file, which holds the SMTP password. While it does
not parse:

- the settings page shows the defaults with the warning *The stored
  notification settings could not be read. Saving replaces them.*, and its
  `Send test` asks for a save first;
- a save from the page starts from the defaults and validates what the page
  sent; only when the new file is about to be written is the old one copied
  to `notifications.json.unreadable` beside it (root `0600`, replacing an
  older one), since it may hold the only copy of the SMTP password, the
  webhook URL and every schedule's choice, and then the new file replaces it.
  A copy that fails fails the save; a save that is refused (400) leaves the
  file where it is, keeps no copy and logs no warning about the file. A save
  that goes through logs one warning, `notification settings unreadable, starting from the
  defaults`, with `path`, `kept_as` and `error`. What only the old file held
  is not in the new one: every schedule's own choice and the SMTP password;
- every other writer refuses it and leaves it as it is: a schedule's create or
  update that names a choice answers 409 (*the notification settings could not
  be read; saving them on the Notifications settings page repairs them*), an
  edit that leaves the choice alone still saves, and the Schedules page shows
  no choices. A stored file that parses but that validation refuses (a hand
  edit such as an invalid `dockerUpdates`) answers a schedule's choice 409 as
  well, with *the stored notification settings are not valid; saving them on
  the Notifications settings page repairs them*;
- the bell lists every level; `alerts.list`, `schedules.list` and
  `schedules.get` log that at debug level, and `alerts.get_notifications`
  logs the warning `notification settings unreadable, showing the defaults`;
- producers decide with the defaults, logging one warning each time they read
  the settings, and nothing is emailed or posted: delivery logs
  `alert delivery settings unreadable` and sends nothing.

## Who writes, with no session

Each row says what the source raises, at which level, under which key, and
which setting governs it. Every raise is then delivered as the
[grid](#delivery) says for its level.

| Source | Writer | Raises | Setting | Resolve |
|---|---|---|---|---|
| Automatic updates | `ExecStopPost=` drop-in on the provider's upgrade service, running `linuxio alert auto-update` | `auto-update/run`: *Automatic updates failed* (alert) on a failed run; on a successful run that the policy reports, *Automatic updates installed N packages*, *Automatic updates found nothing to install* or, where the provider cannot tell what it installed, *Automatic updates ran* (notice). A notice becomes a warning with "Reboot required." when `/run/reboot-required` exists | The provider's policy ([below](#automatic-updates-notification-policy)); failures under every policy that installs the drop-in | Never; each run is a new occurrence of the one record |
| Pending system updates | `ExecStopPost=` drop-in on `apt-daily.service`, running `linuxio alert pending-updates` after each package-list refresh (APT only) | `auto-update/pending` (notice): packages an upgrade would install. The occurrence is the package set, so an unchanged set is not re-delivered | *Tell me when system updates are waiting* (the automatic-update options) | The next refresh that finds nothing pending |
| Docker update outcome | `linuxio-docker-update` worker, already root; reads the settings once per pass | `docker-update/<name>` (alert): a scheduled update fails or the replacement container is not healthy. `docker-update/updated-<name>` (notice), *Docker container updated: `<name>`*: a scheduled update replaced the container. `docker-update/linuxio-update-pass` (notice), one record for every pass, each pass a new occurrence, for a pass that replaced nothing (in either mode): *Docker updates: nothing to update* when nothing waits, *Docker updates: N waiting* when updates wait while the "waiting" alerts are off | Failures always, whatever is stored (also a stored `never`). `updated-` under `changes` and `every_run`. The pass notice under `every_run` only, and not for a pass that failed, replaced a container (it raises only the per-container notices), could not check one of its containers, or whose waiting updates the "waiting" alerts report | Failure: the next successful update of that container. Updated and pass notice: dismissed by the user |
| Docker update available | `linuxio-docker-update` worker, after the check every scheduled pass makes (the Check only mode is enough) | `docker-update/available-<name>` (notice), *Docker update available: `<name>`*. The occurrence is the remote digest | `dockerUpdatesAvailable` (*Tell me when Docker updates are waiting*) | The next check that finds the container current or cannot check it, or runs with the setting off, or the update that replaces it |
| Jobs started in the browser | The root job host (`linuxio-jobs@0`); a user's job host raises none | `jobs/<job id>`, titled by the job's name: *`<name>` failed* (alert) with the error; *`<name>` was interrupted*, a warning when a shutdown or a reboot stopped it ("The job host stopped before the job finished (shutdown)." or "(reboot)") and an alert when its job host crashed ("… (crash).") or it could not be resumed after the reboot ("It could not be resumed after the reboot: …"); *`<name>` finished* (notice) with "It ran for 12 min." for a completed job that ran five minutes or more. The name is the job's label; a file-browser job is *`<Action>: <label>`* (*Copy: 12 items failed*; *Move to Trash*, *Restore from Trash*, *Delete from Trash* and *Change permissions* for the routes whose words do not read as an action); a job without a label is named by its route. Link `/history?job=<id>` | Failures and interruptions always. The notice under `browser_jobs: long_jobs` | A resumed job that completes clears its interrupted alert (and may then raise its notice); otherwise dismissed by the user |
| Scheduled scripts | The end hook of `linuxio-schedule-<id>.service` (`linuxio scheduled-run end`, run as root by the unit's `ExecStopPost=` drop-in); the root job host for a run it finds ended without that hook | `jobs/<run id>`, one per run: *`<name>` failed* (alert) with the reason, such as "exited with status 3"; *`<name>` was interrupted*: a warning, "The system stopped before the run finished.", when the hook saw the shutdown or the root host found a run of an earlier boot; an alert, "The run ended without reporting its result.", when the root host found the run's unit gone in the same boot without its end hook having reported. `jobs/finished-<schedule id>` (notice), *`<name>` finished* with how long it ran: one record per schedule, each completed run a new occurrence, so the bell keeps one line per schedule that each run makes unseen again. Link `/services/schedules?schedule=<id>`. A stopped run ends cancelled and raises none. Timer runs of snapshots, replication, resyncs and Docker updates raise no jobs alert: the rows of their own sources report them | Failures and interruptions always. The notice when the schedule's own choice is `every_run`, or when it has none and `report.scheduled_scripts` is `every_run` | Failure and interruption: dismissed by the user; each run has its own record. Notice: dismissed by the user, or resolved when the schedule is deleted |
| Btrfs health | `linuxio-btrfs-timer health run` every ten minutes, and the `storage.btrfs_list` bridge route | `btrfs/health-<uuid>`, `btrfs/scrub-<uuid>`, `btrfs/space-<uuid>`, `btrfs/offline-<uuid>`, `btrfs/resync-<uuid>`; see [Btrfs management](./btrfs.md#filesystem-health) | None: storage health is always raised | The next check that shows the condition cleared, or the pool gone |
| Btrfs missing disks | `linuxio-btrfs-timer degraded run` at boot and `linuxio-btrfs-timer resync`; see [Missing disks](./btrfs.md#missing-disks) | At once rather than at the next health run: `btrfs/health-<uuid>` for a pool mounted without a disk, `btrfs/offline-<uuid>` with the reason a pool was left unmounted, `btrfs/resync-<uuid>` when a resync gives up, and the notice `btrfs/resynced-<uuid>`, *Pool `<name>` is whole again*, when it finished | None for the conditions and a failed resync. `resynced-` under `report.resync: changes`, the default | As Btrfs health; `resynced` stays until dismissed, or until the next resync starts |
| Btrfs snapshots | `linuxio-btrfs-timer snapshot` (hourly timer, pre-update hook), and the page's Run now (`storage.btrfs_snapshot_run_now`, a Mutation, so a job in the root job host) | `btrfs/snapshot-<uuid8>-<path>` (alert): a failed snapshot or an unmounted filesystem. `btrfs/snapshot-run` (notice), one record, each timer pass a new occurrence: *Snapshots: N taken, M sent* or, under `every_run`, *Snapshots: nothing to do*, link `/storage/subvolumes?view=snapshots`. Only the timer's own pass (`snapshot run`) raises it: not the pre-update pass, not the page's Run now (which runs the rules itself, not the timer), not a stopped pass, and not a pass that failed and took and sent nothing | Failures always. The notice under `report.snapshots`: `changes` for a pass that took or sent a snapshot, `every_run` for every pass | Failure: the next successful run for that rule. Notice: dismissed by the user |
| Btrfs replication | `linuxio-btrfs-timer snapshot run` and Run now | `btrfs/replication-<job id>` (alert): a failed replication run. What it sent counts in the snapshot pass's notice | None for the failure | The next successful run of that job |

Later sources follow the same shape: a root process that already knows the
outcome calls `alerts.Raise` or `alerts.Resolve`, or a unit drop-in runs
`linuxio alert raise`. Storage checks (capacity, SMART, degraded pools) would
be a oneshot timer; they are out of this slice.

### Limits

What the sources cannot tell, or do not report, as built:

- **DNF and Mint** report only their service result: they cannot tell what a
  run installed, so they offer no "when something changed", and their "every
  run" notice reads *Automatic updates ran*. APT whose log cannot be read
  reads the same way.
- **A stored `never` for system updates** means no drop-in is installed, so a
  failed automatic update raises nothing until another policy is saved.
  Nothing rewrites it on its own; the settings page says so and offers one
  click ([below](#automatic-updates-notification-policy)).
- **A stored Docker `never` no longer silences failures**: a failed scheduled
  Docker update is always raised. An admin who chose *Never* for Docker gets
  those alerts from this build on.
- **A pass that fails as a whole** without a failure of one of its items
  (Docker not reachable, a rules or jobs file that cannot be read, a scheduled
  container that is gone) is recorded as failed in the history and raises no
  alert. The timer's sources raise alerts per rule, job or container only.
  This predates the settings.
- **A container whose update check fails** (an unreachable or throttling
  registry) raises no alert: the error shows in `docker-update-status.json`
  and as a *Scan failed* chip in the containers table, and such a container's
  "update available" alert is cleared until a check succeeds. This predates
  the settings.
- **A long job's last frame waits for its notice**: the notice is raised, and
  delivered by email or webhook (up to 15 s), before the job's terminal frame
  reaches the browser, as a failure's alert is.
- **A resumed job's run time includes the downtime**: it keeps its original
  start, so its notice counts the time the machine was down.
- **Only the root job host raises alerts**, so a long job of a user without
  sudo raises nothing; that user hears of their own jobs only through the
  [notice at sign-in](#navbar).
- **An interruption by a shutdown or a reboot is a warning**, so the grid can
  keep it from the bell, email and webhook; alerts, crashes among them, always
  reach the browser. On a host upgraded from "Errors only" such a run is no
  longer emailed unless *Warnings* is ticked for email.

## Automatic updates notification policy

The automatic-updates settings carry the policy, with the vocabulary
unattended-upgrades uses for its own mail reports:

```text
notify: never | on_failure | on_change | always
```

`on_change` means a run that installed something. `always` includes runs that
found nothing. Every policy reports a failed run. The on-disk drop-in is
authoritative; no drop-in means `never`.

What each provider offers, as `support.notify_policies`:

| Provider | Upgrade service | Offered |
|---|---|---|
| APT (Ubuntu, Debian) | `apt-daily-upgrade.service` | `on_failure`, `on_change`, `always` |
| DNF4 | `dnf-automatic.service` | `on_failure`, `always` |
| DNF5 | `dnf5-automatic.service` | `on_failure`, `always` |
| Linux Mint | `mintupdate-automation-upgrade.service` | `on_failure`, `always` |

`never` is offered by none, since no choice may silence a failure, but it is
still read (a host with no drop-in has it) and still accepted by
`updates.set_auto_updates`, where it writes no drop-in. While the stored
policy is `never`, `AutoUpdateState.failures_unreported` is true; where the
policy can be configured and automatic updates are enabled, the settings page
then shows *System update
failures are not reported on this machine.* with a *Report failures* button,
which saves `on_failure`, and its *System updates* select reads *Not
reported*. A DNF or Mint drop-in an older build wrote with `on_change` reads
as `on_failure`, which is what it runs as there; the next save writes
`on_failure`.

The drop-in is `/etc/systemd/system/<upgrade-service>.d/linuxio-alert.conf`:

```ini
[Service]
ExecStartPre=-/usr/local/bin/linuxio-btrfs-timer snapshot pre-update
ExecStopPost=/usr/local/bin/linuxio alert auto-update --provider apt --policy on_change
```

`ExecStopPost=` runs on success and failure and receives `$SERVICE_RESULT`
and `$INVOCATION_ID`. The command reads the provider's run evidence
(unattended-upgrades' log file for APT; service result only for the others),
applies the policy, checks `/run/reboot-required`, and raises or stays quiet.
The pre-update snapshot exists only while the drop-in does.

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
| `alerts.test_notifications` | none | `SuccessResponse`, or the delivery error |

`AlertList` is `{ alerts, unseen }`. `alerts` excludes dismissed records and
the levels not ticked for the browser; `unseen` counts the same alerts.
`seen` on each alert is for the calling UID. `alerts.mark_all_seen` marks only
what the list shows, so an alert kept from the browser stays unseen and is
counted once its level is shown again; `alerts.mark_seen` with explicit IDs
marks those IDs.

`NotificationSettings` and `NotificationSettingsView` carry `levels` and
`report`, never `scripts` (a schedule's choice travels with the schedule:
`notify` on `schedules.create`, `schedules.update` and `ScheduleStatus`). A
sent `levels` or `report` decides alone: a level or kind it leaves out takes
its default, and the request's `minSeverity` (with `levels`) or
`dockerUpdates` (with `report`) is then neither used nor validated; the stored
one is derived from the map. A request with neither, as an older page sends,
sets the levels from its `minSeverity` and the Docker report from its
`dockerUpdates`, and keeps the other stored kinds. A request that validation refuses (an unknown
kind or level, a value its kind does not offer, a webhook URL that is not
http or https, a bad SMTP setting or address) answers 400. The view names every
level and kind, and `settingsUnreadable: true` when the stored file does not
parse (the rest is then the defaults). The request and answer shapes are in
the [API Contract](./api-contract.md#notification-settings).

There is no watch Channel. The navbar query refetches on window focus and on
an interval (a minute); mutations replace the cache from their result, and
saving the notification settings or deleting a schedule refetches it, so the
bell follows a grid change or a resolved "finished" notice at once. Add
`alerts.watch` only if that latency turns out to matter.

## Delivery

**Settings → Notifications** sends alerts beyond the bell, with no extra
binary: Go's HTTP client and SMTP client do the sending. The page has three
cards and one *Save* under them:

- **What to hear about**: one select per kind (*System updates*, *Docker
  updates*, *Scheduled scripts*, *Snapshots and replication*, *Pool resync*,
  *Jobs started in the browser*), each offering only its own values (*Failures
  only*, *Also when something changed*, *Every run*; *Also when a pool is
  whole again*; *Also long jobs that finish*), and the two *Tell me when …
  updates are waiting* boxes. The card says that failures are always reported
  and a run cut short by a shutdown or a reboot is a warning, and that storage
  health is always raised (missing disks and errors as alerts, low space and
  an unmounted pool as warnings). The system update choices save as they
  change; everything else saves with *Save*.
- **Where each level goes**: a table of *Notices*, *Warnings* and *Alerts* by
  *Browser*, *Email* and *Webhook*. The *Alerts* / *Browser* box is ticked and
  cannot be unticked.
- **Delivery**: the webhook URL, the email account and *Send test*.

The grid decides at delivery and in the bell:

- **Webhook**: an alert is posted only when its level is ticked for webhook
  and a URL is set. One JSON `POST` per alert with `host`, `source`,
  `severity`, `title`, `message`, `link` and `time`, plus `text` and
  `content` holding a one-line summary so Slack- and Discord-style webhooks
  accept it as is. A status of 300 or above is a failure.
- **Email**: an alert is emailed only when its level is ticked for email and
  an SMTP server and recipients are set. SMTP with implicit TLS on port 465,
  otherwise STARTTLS when the server offers it. A password is only sent over
  TLS (Go's `PlainAuth` refuses otherwise). Header fields are folded to one
  line, so alert text cannot add headers. Addresses are validated on save.
- **Browser**: a level not ticked for the browser is neither listed in the
  bell nor counted in its badge; alerts always are.

An alert is delivered when `Raise` creates it or records a new occurrence,
after the store is written; re-observing the same occurrence sends nothing.
Delivery is synchronous in the raising process. Each delivery is bounded to
15 seconds and its failure is logged (`alert delivery failed`), never
returned, since the alert is already recorded. A webhook error names only the
URL's host (`hooks.example answered 403 Forbidden`, `post to hooks.example:
…`, or `invalid webhook URL` for a stored URL that does not parse), so a token
in its path or query never reaches the journal or the page. The password is never sent
back to the browser: the API reports `smtpPasswordSet`, and a save with an
empty password keeps the stored one unless the username was cleared.
**Send test** delivers a test alert to every configured channel whatever the
grid says; it waits for a save while the page has unsaved changes or the
stored file cannot be read.

The settings file sits beside `alerts.json`, so tests that point the store
at a temporary directory never read a host's real delivery settings.

## Navbar

The bell shows persisted alerts and live transfers. The localStorage toast
history is removed; toasts still pop, they are not kept. Opening the panel
marks everything it lists seen. Each alert has a dismiss action. The badge is
the unseen count.

**While you were away.** Right after a sign-in (not after a reload), the shell
says once what happened while the user was away:

- an admin, who reads alerts, hears how many are unseen: *While you were
  away: 3 new notifications*. *Show* marks them seen and opens the bell's
  tray, with keyboard focus on the bell's button beside it;
- another user, who cannot read alerts, hears how many of their own jobs
  failed or were interrupted since they last opened the history: *While you
  were away: 2 of your jobs did not finish*, with *Show* going to `/history`.
  The time is the per-user `dismissals.historySeenAt`, stored when the history
  page has shown a loaded history with at least one entry; the bridge stores it
  in UTC and never later than its own clock. A user with no stored time hears
  nothing at that sign-in, which stores the baseline.

## Not in this slice

- Per-route settings for jobs started in the browser (they are one line), and
  a line for the file indexer.
- Matchers, digests, push services other than webhooks.
- `alerts.restore`, `alerts.mark_unseen`, resolved history.
- Storage, unit-failure, and release-check sources; alerts for a timer pass
  that fails as a whole and for a container whose update check fails
  ([Limits](#limits)).
- Alerts raised by unprivileged sessions or a user's job host.
