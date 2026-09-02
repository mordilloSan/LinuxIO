# Scheduled execution and run history

> **Status: Planned.** LinuxIO does not yet expose general scheduled scripts or
> a persistent scheduled-run history.

This is a future generic scheduling surface for user-defined scripts. It is
distinct from the existing Docker auto-update timer, the durable manual
`docker.update_container` operation, and the read-only systemd timer inventory.
Those surfaces keep their existing owners and are not schedule definitions
described here.

This plan defines future timed script execution without turning the bridge into
a cron daemon, process supervisor, or log database. Native systemd services and
timers own execution. LinuxIO owns declarative configuration, a bounded API
projection, and the user-facing history that systemd and journald do not model.

## Ownership model

| Concern | Owner |
|---|---|
| Calendar activation and missed-run policy | systemd `.timer` |
| Process identity, environment, timeout, resource limits, overlap, and exit | systemd `.service` |
| Standard output and error | journald |
| Schedule definition and safe script reference | LinuxIO configuration |
| Stable historical run summary | root-owned LinuxIO run directory |
| User-facing failure condition | LinuxIO alert daemon |
| Live manual invocation | Task only when interactive progress/cancellation is useful |

The bridge renders unit files under `/etc/systemd/system` with the atomic write
and D-Bus daemon-reload path the Docker auto-update timer already uses, and uses
the existing systemd D-Bus boundary to enable, disable, start, stop, inspect,
and remove them. Do not shell out to `systemctl` or build an in-process timing
loop.

## Schedule definition

A definition contains only validated declarative data:

```text
id, owner uid, name, enabled
script reference and fixed argument list
OnCalendar expression, timezone, persistent missed-run policy
timeout, overlap policy, execution policy
created_at, updated_at
```

The script reference must resolve through a LinuxIO-owned allow-list or managed
script directory. Do not accept an arbitrary shell command line. Arguments are
kept separate, environment keys are allow-listed, secrets use a separate
protected mechanism, and the execution user/group comes from policy rather than
untrusted unit properties.

Unit names are deterministic from the schedule ID, for example
`linuxio-schedule-<id>.timer` and `.service`. Unit names locate native state;
they are not authorization secrets or historical run IDs.

## Native unit policy

The service should use the narrowest practical sandbox and resource policy for
the selected script class. At minimum define:

- `Type=oneshot`;
- an explicit `User=` and `Group=`;
- `WorkingDirectory=` only when required;
- `TimeoutStartSec=`;
- journald stdout/stderr;
- deterministic overlap behavior; and
- collection/retention behavior that does not erase the run summary.

The timer reports next and last activation directly from systemd. Calendar
syntax is validated through systemd rather than approximated in Go. Whether a
missed timer fires after boot is an explicit `Persistent=` product choice.

## Run identity and summary

One activation creates one stable LinuxIO run ID correlated to the exact
systemd unit invocation. A bounded summary contains:

```text
id, schedule_id, unit_name, invocation_id
trigger (scheduled | manual), scheduled_at
started_at, finished_at
state (queued | running | succeeded | failed | canceled | unknown)
exit_code/exit_status, concise result or structured error
```

The run summary is not the log. The log viewer opens journald using the unit and
invocation identity, reusing the existing journal Channel and cursor behavior.
Journal rotation may remove old diagnostic output while the bounded summary
continues to state honestly that the run occurred.

Run summaries are one bounded JSON file per invocation in a root-owned run
directory, keyed by schedule ID and systemd invocation ID, the same shape as
the durable-task file store in `backend/common/durabletask`. Concurrent
schedules write different files, so there is no shared writer, no schema, no
migration, and no database in the scheduler. Listing a schedule's recent runs is
a directory read; nothing in the UI queries runs across schedules. The alert
daemon's database is not used for runs. Retention deletes terminal files beyond
a per-schedule bound and never an active or unreconciled run.

## Capturing executions while the bridge is absent

Scheduling must continue with no logged-in user and no bridge process. A
short-lived root worker binary, the `linuxio-docker-update` precedent, records
each activation. The generated service runs the allow-listed script natively
and brackets it with two worker calls:

```ini
[Service]
Type=oneshot
User=<policy user>
Group=<policy group>
TimeoutStartSec=<timeout>
ExecStartPre=+/usr/local/bin/<worker> begin --schedule <id>
ExecStart=<allow-listed script> <fixed arguments>
ExecStopPost=+/usr/local/bin/<worker> finish --schedule <id>
```

`begin` writes the run file in the `running` state using `$INVOCATION_ID`.
`finish` completes it from `$SERVICE_RESULT`, `$EXIT_CODE`, and `$EXIT_STATUS`,
which systemd passes to `ExecStopPost=` even after a timeout kill, so the finish
record does not depend on the script surviving. The `+` prefix runs both calls
as root so they can write the root-owned directory while the script itself
keeps the policy user and sandbox. No daemon observes units. If a run file has
a `begin` and no `finish`, the bridge reconciles on read by asking systemd for
the unit's invocation state and marks the run `unknown` when neither systemd
nor the file proves an outcome. This design satisfies:

- one run ID per accepted activation;
- no duplicate execution during reconciliation;
- typed start/finish state independent of parsing human log text;
- conservative `unknown` when neither systemd nor a typed result proves the
  outcome; and
- bounded behavior across host and bridge restart.

Do not infer a successful result merely from the absence of an active unit.

## API and UI

Calls manage bounded state:

- `schedules.list/get/create/update/delete`;
- `schedules.enable/disable`;
- `schedules.run_now` for an explicitly authorized manual activation;
- `scheduled_runs.list/get`; and
- `scheduled_runs.cancel` only while systemd confirms a cancellable active
  invocation.

A live status Channel is optional. Querying definitions and recent summaries,
plus D-Bus unit-change invalidation, may be sufficient. Add a Channel only when
it removes polling or provides behavior that cannot be expressed as cache
invalidation.

The UI shows schedule, next/last activation, active state, recent outcomes, and
a link to invocation-filtered logs. Editing timing never edits a run record.

## Alerts

Scheduled execution is an alert source. The worker's `finish` step posts
transitions to the alert daemon's socket, and the bridge posts when it
reconciles a run to `unknown`:

- failure or unknown outcome raises or updates a stable alert keyed by schedule;
- a later successful run resolves that condition when policy says the schedule
  has recovered; and
- successful routine runs do not create durable notifications by default.

Delivery frequency and targets are configured by the notification router, not
by the timer or runner.

## Completion criteria

- Scheduling and execution continue while the bridge and browser are absent.
- Deterministic unit changes converge safely through D-Bus.
- Overlap, missed-run, timeout, privilege, cancellation, and deletion semantics
  have focused tests.
- Each activation has one bounded summary and one exact journald correlation.
- No raw logs are stored in run files.
- Host restart produces proven state or `unknown`, never an invented success or
  replacement execution.
