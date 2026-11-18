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
there and never reads a missing log as a success. For a run the journal no
longer holds, the Logs page shows its scope (`<unit> · invocation <ID> — logs
are limited to the host’s journal retention.`, or `· all runs` for the unit),
"0 shown" and an empty table that reads "No logs match the current filters.",
and the open schedule's "Output of the latest run" reads "No logs available.".
The run's record and the output it saved, under Past runs, are unchanged; the
state beside them is systemd's.

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
- `frontend/src/test/vm/jobs-phase4b.spec.ts` runs in the test VM too (the
  Ubuntu 24.04 one included) and covers three things on a host:
  - A run that passes its timeout (8 s): the record reads `failed` with
    "timed out", the unit's `Result` is `timeout`, the script's child process is
    gone, the output saved before the kill is kept and the alert is raised.
  - Catching up a missed run: two daily schedules, due three minutes ahead, one
    with "Run after a missed activation" and one without; the VM is shut down
    in an orderly way and booted four minutes later (`linuxio-vm.sh off-for`).
    The one that asked runs a few seconds after the boot, long after its time;
    the other runs nothing.
  - A restart during a run: the root job host (`linuxio-jobs@0`), then the
    control plane (`linuxio restart`) and the session's bridge are restarted
    while a script runs; the run stays `running` with the same systemd
    invocation, ends `completed`, and its output has both lines.
- `frontend/src/test/vm/jobs-journal.spec.ts` runs in the test VM and covers a
  run whose journal lines are gone. Real retention takes days; the spec rotates
  the journal and removes every archived file (`journalctl --rotate`, then
  `journalctl --vacuum-time=1s`), and checks that `journalctl` then holds no
  line of the run's invocation or of the unit. An enabled schedule runs once
  and is vacuumed after it ended:
  - The run's record still reads `completed`, and its saved output, under Past
    runs, still holds both lines.
  - Both links to the Logs page, for the unit and for the run, showed the
    lines before the vacuum, and after it show what "Status and logs" above
    says, with no error.

  A second run is vacuumed while it runs: the page's live output, which had
  shown its first line, shows the last one too, and the run ends `completed`.
  Its saved output holds only the line written after the vacuum. The spec
  deletes its schedule and the records of its runs.

## References

- [systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
- [systemd.timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
- [systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)
- [journald.conf](https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html)
