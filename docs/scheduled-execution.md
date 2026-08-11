# Scheduled execution and run history

> **Status: Planned.** LinuxIO does not yet expose general scheduled scripts or
> a persistent scheduled-run history.

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
| Stable historical run summary | LinuxIO metadata database |
| User-facing failure condition | LinuxIO alert service |
| Live manual invocation | Task only when interactive progress/cancellation is useful |

The bridge uses the existing systemd D-Bus boundary to create, enable, disable,
start, stop, inspect, and remove units. Do not shell out to `systemctl` or build
an in-process timing loop.

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

A small service-owned SQLite database is appropriate once stable run history
ships. It can share the metadata database used by alerts while keeping schema
and package ownership separate. Retention removes only terminal rows and never
an active or unreconciled run.

## Capturing executions while the bridge is absent

Scheduling must continue with no logged-in user and no bridge process. The
implementation therefore needs a system-owned reconciliation path that can
associate each service activation with a run ID and commit its terminal
summary. Acceptable designs include a narrow LinuxIO runner executable or a
system service that observes only LinuxIO-managed units. The choice is deferred
until implementation, but it must satisfy:

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

Scheduled execution emits meaningful source transitions to the alert service:

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
- No raw logs are stored in the metadata database.
- Host restart produces proven state or `unknown`, never an invented success or
  replacement execution.
