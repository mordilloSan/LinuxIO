# Scheduled scripts with systemd

> **Status: Implemented.** Administrators manage scripts under **Services → Scheduled tasks**.
> Completion is recorded in [Completed or closed TODOs](./TODO/completed.md).
> The initial implementation uses systemd and journald directly; it does not
> require a new worker binary, scheduler daemon, or LinuxIO run-history store.

LinuxIO supplies the script editor, validated configuration, and controls for
native systemd services and timers. Reuse existing Linux functionality before
adding execution, logging, or recovery code. The existing Docker auto-update
worker and read-only systemd timer inventory retain their current owners.

## Ownership

| Concern | Owner |
|---|---|
| Calendar and missed-run policy | systemd `.timer` |
| Execution user/group, process lifetime, timeout, and overlap | systemd `.service` |
| Script stdout/stderr and retained execution logs | journald |
| Script content, approved execution policy, and schedule configuration | LinuxIO configuration |
| Configuration and controls | Existing privileged bridge and UI |
| Future notifications | Separate notification integration; not a prerequisite for scheduling |

Use the existing atomic unit-file writes and systemd D-Bus boundary, as the
Docker auto-update timer does. Do not add a timing loop, process supervisor,
custom privilege-dropping launcher, or another log database. Native execution
and logging continue with no browser or bridge connected.

## Configuration and privilege boundary

A managed task contains its ID, display name, script, fixed argument list,
execution account, calendar expression, timezone, missed-run policy, timeout,
optional working directory, and enabled state. Save scripts in a protected
LinuxIO-managed directory and keep script contents out of unit properties.
Definitions are stored as root-owned `0600` JSON under
`/etc/linuxio/schedules/definitions/`. Versioned scripts are root-owned `0440`
files readable by the execution group under `/etc/linuxio/schedules/scripts/`.
Managed directory modes are applied explicitly after creation so the bridge’s
restrictive umask cannot prevent the selected account from reading its script.
New tasks are disabled until explicitly enabled. Up to 100 tasks are supported.
Use deterministic unit names `linuxio-schedule-<id>.service` and
`linuxio-schedule-<id>.timer`.

Task management is administrator-only: selecting another account,
including root, is an explicit privileged operation. The existing privileged
bridge validates settings and manages units. Systemd launches the script with
`User=` and `Group=`; a LinuxIO process need not switch users or remain running.
Preserve the webserver, auth, and bridge privilege boundaries.

Validate calendars through systemd, resolve execution accounts through the
host, and escape unit syntax and argument boundaries correctly. Accept script
content as the administrator's program, not as an interpolated unit command
line. An unprivileged user must not be able to replace a script scheduled for
root execution. Configuration changes must not replace a script during a run.

## Native execution policy

A generated service uses the following shape (placeholders are illustrative):

```ini
[Service]
Type=oneshot
User=<approved account>
Group=<approved group>
TimeoutStartSec=<configured timeout>
ExecStart=:/usr/bin/bash <managed script path> <fixed arguments>
StandardOutput=journal
StandardError=journal
```

No LinuxIO `begin` or `finish` hook is needed for this scope. Apply sandboxing
compatible with the selected account's intended script operations.

- A stable service unit prevents overlapping activations from starting another
  instance; a timer firing while that service is active leaves it running.
- `Persistent=` on the timer makes missed calendar activation catch-up explicit.
- Service start-rate limiting is disabled (`StartLimitIntervalSec=0`); the
  calendar controls activation frequency, including every-second schedules.
- `AccuracySec=1ms` supports second-level calendars without systemd's default
  one-minute coalescing window. This is not a real-time execution guarantee.
- Systemd owns timeout and process termination, including child-process policy.
- Disabling a timer prevents future timer activations; it does not cancel a
  running script. Cancellation is a separate authorized action.
- Editing or deleting a running task is rejected until it finishes or is stopped.
- Native start acknowledgements mean a request was accepted, not that the
  script completed successfully or that a distinct additional run was created.

## Status, logs, and retention

Read timer next/last activation and service state/result from systemd. Those
properties are current or latest native state, not a permanent execution ledger.
Use systemd invocation identity when available to distinguish executions.

Reuse LinuxIO's existing journal viewer and bounded cursor-based log queries.
Open the selected unit's logs, and scope to an invocation where supported.
Script output and system-manager lifecycle messages may use different journal
fields; validate both paths instead of assuming one filter captures everything.
Only show historical outcomes backed by native structured evidence. Do not
parse localized message prose into an authoritative run state or interpret a
missing unit/log entry as success.

History is limited to retained native evidence. Journal persistence across
reboots depends on host configuration, and rotation or vacuuming can remove
older entries. Show unavailable history honestly. Do not introduce a second
store merely to make a complete-history promise that this feature does not need.

The existing viewer downloads its loaded matching entries; paginate older
entries before exporting them. Unit logs default to all retained time, and the
latest invocation has a separate log link.
If ordinary files are required later, systemd supports `LogsDirectory=` and
`StandardOutput=append:/path/to/output.log`, with `StandardError=inherit`.
That writes successive executions to one file and replaces journal capture of
that output. File rotation, duplication to both destinations, and one-file-per-run
naming are separate requirements, not automatic features of this setting.

## API and UI

The task editor offers daily time and weekly day/time controls, plus hourly,
minute, and second presets. A new task defaults to daily at 03:00 UTC. Advanced
calendar expressions remain available and existing custom expressions are
preserved when editing. Working directories use the shared folder picker from
Docker settings, with optional direct path entry.

Expose schedule list/get/create/update/delete, enable/disable, and run-now
through existing Go-owned Call contracts. Reuse the native service controls for
stopping a current run. If a request targets a specific invocation, ensure it
cannot accidentally stop a subsequent invocation; reject it when native
identity cannot be verified safely.

Show the execution account, schedule, enabled state, next/last timer activation,
current/latest service result when available, and a link to logs. Refresh native
state through query invalidation after controls and five-second polling. Do not add a separate
`scheduled_runs` persistence API or an execution Task solely to duplicate
systemd state.

## Notifications are a separate follow-up

Scheduling and logging work without an alert service. When scheduled-script
notifications are implemented, start with native failure triggers such as
`OnFailure=` and connect them to the chosen notification mechanism. Systemd
provides the trigger; LinuxIO-specific alert policy or delivery may still need
an adapter. Do not add a scheduler worker just to anticipate that integration.

Unattended retries, deduplication, recovery, and retention gaps must be resolved
by that notification design. A missing historical log alone is not a failed
script. Durable alert state, if required, is distinct from storing every run.

## Verification boundary

Automated checks cover configuration lifecycle and failure recovery, protected
files, argument/unit rendering (including `systemd-analyze verify`), admin-only
API routes, exact-invocation cancellation through an isolated D-Bus, native
state snapshots, log filters, and UI interactions. These checks do not install
a live timer or prove selected-user execution, timeout, catch-up, reboot survival,
or multi-day retention on a real host. User-provided development-host logs
confirmed successive every-second executions as the selected account and
successful journal capture. Timeout, catch-up, reboot survival, and multi-day
retention remain deployment checks against the host’s systemd and journald
configuration.

## Completion criteria

- Execution and journal capture continue without a browser or bridge.
- Configuration converges safely to deterministic native units through D-Bus.
- Selected-user execution, script-path protection, argument escaping, timeout,
  overlap, missed-run, cancellation, edit, and delete policies have focused tests.
- Status and logs use native state and invocation correlation where available.
- Restart and journal-retention gaps show unavailable or unknown information;
  they do not create invented outcomes or replacement executions.
- No new execution binary, resident scheduler, run-summary store, or custom
  run-reconciliation service is required for the initial feature.

## Native references

- [systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
- [systemd.timer](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
- [systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)
- [journald.conf](https://www.freedesktop.org/software/systemd/man/latest/journald.conf.html)
