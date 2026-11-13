# Scheduled scripts

An administrator writes a script in LinuxIO and picks when it runs and as which
account. systemd runs it. LinuxIO keeps the script and its settings, writes the
systemd units, and shows what systemd and the journal report.

## Who owns what

| Concern | Owner |
| --- | --- |
| When a script runs, and catching up a missed run | the systemd `.timer` |
| The account, the process, the timeout, and not running twice at once | the systemd `.service` |
| The script's output | journald |
| The script text and its settings | LinuxIO, under `/etc/linuxio/schedules` |
| A record of each run and its alert | the run hooks ([Jobs](./jobs.md#scheduled-runs)) |

LinuxIO has no timing loop, no process supervisor and no log store of its own
for scripts. A script runs and logs with no browser and no bridge connected.

## What is stored

Everything is under `/etc/linuxio/schedules` (`backend/bridge/handlers/schedules/store.go`):

- `definitions/<id>.json`, owned by root, mode `0600`: the name, the account,
  the arguments, the calendar expression, the timezone, whether a missed run is
  caught up, the timeout, the optional working folder, and whether the schedule
  is enabled.
- `scripts/<id>-<revision>.sh`, owned by root and the account's group, mode
  `0440`. The revision is in the name, so a changed script is a new file. The
  script text is never put in a unit file.
- `.lock`, which every change takes.

The folders are set to `0755` after they are made, because the bridge's umask
would otherwise keep the account from reading its script.

The units are `linuxio-schedule-<id>.service` and `linuxio-schedule-<id>.timer`
in `/etc/systemd/system`.

Limits: 100 schedules; a name of at most 128 characters; a script of 1 to
65536 bytes with no NUL; a timeout of 1 second to 7 days; a working folder that
is an absolute path. systemd checks the calendar expression
(`systemd-analyze`), and the timezone must be one Go can load.

A new schedule is disabled until it is enabled.

## The units

The service (`renderService` in `backend/bridge/handlers/schedules/native.go`):

```ini
[Unit]
StartLimitIntervalSec=0

[Service]
Type=oneshot
User=<account>
Group=<account's group>
TimeoutStartSec=<timeout>s
TimeoutStopSec=15s
KillMode=control-group
NoNewPrivileges=yes
UMask=0077
ExecStart=:/usr/bin/bash <script path> <arguments>
StandardOutput=journal
StandardError=journal
SyslogIdentifier=linuxio-schedule
WorkingDirectory=<folder, when set>
```

The timer has `OnCalendar=<expression> <timezone>`, `AccuracySec=1ms` and
`Persistent=` set from the schedule.

What follows from that:

- One service per schedule means a timer that fires while the script still runs
  starts no second copy.
- `StartLimitIntervalSec=0` lets a schedule run as often as every second.
  `AccuracySec=1ms` stops systemd from grouping runs into its default
  one-minute window. Neither is a real-time promise.
- systemd ends a run that passes its timeout, with every process it started.
- The arguments are a fixed list, each written as one unit word. The script is
  the administrator's program; nothing from a request is pasted into a command
  line.

## Changing, running and stopping

Nine routes, all `Privileged()` (`backend/bridge/handlers/schedules/handlers.go`):
`schedules.list`, `get`, `create`, `update`, `delete`, `enable`, `disable`,
`run_now` and `stop`. Picking the account, root included, is part of that
admin-only edit. The changes run as jobs.

- **Disable** stops future runs. It does not stop a script that is running.
- **Edit and delete** are refused while the script runs or is queued; stop it
  or wait.
- **Run now** means systemd accepted the start. It does not mean the script
  finished, or that it succeeded.
- **Stop** names one run by its systemd invocation ID. LinuxIO asks systemd for
  the unit of that invocation and stops only that. A stop for a run that
  already ended fails; it never falls back to stopping the unit by name, which
  could hit the next run.

## Status and logs

The page reads the timer's next and last activation and the service's state,
result and invocation ID from systemd. That is the current or latest state. The
list and the open schedule refetch every 5 seconds, and after each action.

Logs are the journal. The page links to the Logs page for the unit, and for the
latest run by its invocation ID, and follows a running script's output live.
How far back the journal goes depends on the host: a reboot without persistent
journal storage, rotation or a vacuum removes old lines. LinuxIO shows what is
there and never reads a missing log as a success.

Each run also gets a record, a saved copy of its output and, when it fails or
is interrupted, an alert. That is done by two hooks on the unit, not by this
package: see [Jobs](./jobs.md#scheduled-runs) for the records and
[Process & Systemd Architecture](./process-systemd-architecture.md#scheduled-runs)
for the hooks. What a schedule reports (`notify`: failures or every run) is in
[Alerts](./alerts.md#notification-settings).

## The page

Services, Schedules tab (`frontend/src/routes/_authenticated/services/-components/SchedulesTab.tsx`).
The editor offers daily and weekly times and hourly, minute and second presets,
and keeps a custom calendar expression when one is edited. A new schedule
starts as daily at 03:00 UTC.

## Testing

- Unit tests cover the stored files and their modes, unit rendering (checked
  with `systemd-analyze verify` where it is installed), the admin-only routes,
  stopping one invocation, and the page.
- `frontend/src/test/vm/jobs-phase4.spec.ts` runs in the test VM and covers the
  run records, the saved output, the alerts, stop, an orderly reboot and a
  power cut.
- Not covered by any test: a run that passes its timeout, catching up a missed
  run, and journal retention over several days. They are in the
  [backlog](./TODO/backlog.md#needs-a-real-machine-or-a-measurement).

## References

- [systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
- [systemd.timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
- [systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)
- [journald.conf](https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html)
