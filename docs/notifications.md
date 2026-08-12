# Alerts and notification delivery

> **Status: Planned.** The server-side alert store, watch Channel, routing, and
> delivery targets are not implemented yet.

This plan defines LinuxIO's durable user-facing alert lifecycle. It deliberately
separates alerts from live Tasks, scheduled-run records, journald logs, transient
toasts, and external delivery. See the
[API reliability roadmap](./api-reliability-roadmap.md) for dependency order and
[Scheduled Execution](./scheduled-execution.md) for timer, run, and log
ownership.

## Domain boundaries

These records answer different questions:

| Record | Question it answers | Authority |
|---|---|---|
| Task | What is this live API operation doing? | `TaskService` or its explicit durable executor |
| Scheduled run | When and how did this timer-triggered execution finish? | systemd plus a bounded LinuxIO run summary |
| Log | What diagnostic output did the executor produce? | journald |
| Alert | What condition currently needs a user's attention? | LinuxIO alert store |
| Delivery attempt | Was an alert transition sent to an external target? | LinuxIO delivery state |

A failed Task or scheduled run may raise an alert, but the alert does not copy
the Task, run, or journal. Ordinary log lines never become alerts merely because
they have warning or error priority.

## Alert lifecycle

An alert has stable source identity and explicit, independent state:

```text
id, source, category, key
severity (info | warning | error | critical)
title, message, allow-listed metadata
first_occurrence, last_occurrence, occurrence_count
active, resolved_at (nullable)
dismissed_at/dismissed_by (nullable)
```

`(source, key)` is the deduplication identity. Re-observing the same active
condition updates `last_occurrence` and the count instead of inserting a new
row. A source explicitly resolves the condition when it is no longer true.
Resolution is source truth; dismissal is a user action and must not masquerade
as recovery.

Recurrence after resolution reactivates the same logical alert and starts a new
active occurrence. Recurrence after dismissal follows an explicit source
policy: materially new occurrences restore the alert by default, while noisy
unchanged polling does not.

Seen state is presentation state and is stored per authenticated numeric UID:

```text
alert_id, uid, seen_at (nullable)
```

This lets two users observe the same system condition independently. Dismissal
is initially a privileged system-wide action; if product requirements later
need per-user dismissal, add it as a separate relation rather than overloading
seen state.

## Persistence

Use a small service-owned SQLite database for alert lifecycle, per-user seen
state, bounded run summaries, and delivery attempts. These requirements involve
deduplication, concurrent bridge sessions, independent state transitions, and
bounded queries; a relational metadata store is simpler and more honest than
rewriting per-user JSON snapshots or replaying journal history.

SQLite is not the scheduler or log store. Never persist raw journal output,
every toast, Task progress frames, arbitrary requests, credentials, arbitrary
HTML, or unvalidated external links. Apply schema migrations transactionally,
use restrictive file permissions, enable foreign-key checks, and keep retention
bounded. Active alerts are never removed by age-based pruning.

The initial database tables are conceptually:

- `alerts` — source identity, lifecycle, severity, safe presentation fields;
- `alert_seen` — per-UID seen timestamp;
- `scheduled_runs` — bounded execution summaries defined in the scheduling
  plan; and
- later, `delivery_attempts` — target, transition, attempt time, outcome, and
  bounded retry state.

Notification target secrets remain in a separately protected configuration
surface; they do not belong in alert rows or delivery history.

## API

Bounded mutations are direct Calls:

- `alerts.list` — filtered authoritative snapshot plus revision and unseen
  count;
- `alerts.mark_seen` and `alerts.mark_unseen` — mutate the caller's seen state;
- `alerts.mark_all_seen` — mark the current result set seen;
- `alerts.dismiss` — privileged dismissal of an active alert;
- `alerts.restore` — restore a dismissed alert without pretending its source
  condition changed; and
- `alerts.resolve` — internal/source-owned operation, not a general UI action.

The public model should use `alert`, not `notification`, for lifecycle APIs.
“Notification” remains the product label for navbar presentation and external
delivery.

## `alerts.watch` Channel

`alerts.watch` is a server-producing Channel. On open it sends an authoritative
snapshot with a monotonically increasing revision, followed by coalesced
revision changes. Reconnect always starts from a current snapshot; correctness
does not depend on replaying every event.

TanStack Query owns the frontend alert cache. The initial list seeds it and the
watch path replaces or invalidates that same cache. Sonner may present newly
visible transitions, but it is never a history owner. Remove the current
localStorage toast history only when the server-backed navbar cuts over.

A slow watcher cannot block alert writes or accumulate an unbounded queue.
Channel closure removes all subscriber resources. Multiple bridge processes
observe database revision changes through a bounded reconciliation mechanism;
an in-process subscriber map alone is not authoritative.

## Sources

The first source should prove the lifecycle with a meaningful, deduplicated
condition. Candidate integrations after the core is tested include:

- terminal failure or cancellation of selected Tasks;
- failed or unknown scheduled runs;
- SMART and storage-health conditions;
- application or container update availability; and
- failed systemd units that LinuxIO explicitly manages.

Task or run completion remains authoritative if alert persistence fails. Source
reconciliation retries an idempotent upsert by stable source key. Frontend Task
recovery never manufactures alerts.

## Routing and delivery

External delivery is a later layer inspired by Proxmox's
event/matcher/target split:

1. An alert transition emits a delivery event containing severity, source,
   type, timestamp, and allow-listed metadata.
2. Matchers select events by severity and metadata. Calendar rules are added
   only when users need quiet hours or time-based routing.
3. Targets deliver through email, webhook, or another explicitly supported
   adapter.

Each target receives one delivery per matched transition even if multiple
matchers select it. Frequency, grouping, and retry policy belong here, not in
the alert lifecycle. Delivery failure may itself be surfaced as a bounded
administrative alert without recursively routing forever.

## Security and failure behavior

- Every read and seen-state mutation is scoped to the authenticated UID.
- Dismiss/restore and target configuration use explicit privilege checks.
- Alert metadata and internal routes are allow-listed and length-bounded.
- Database busy, full, migration, permission, and decode failures surface with
  operation context and never publish an uncommitted revision.
- A source outcome is not rolled back when alert creation or delivery fails.
- Retention removes only terminal run summaries, resolved alerts allowed by
  policy, and old delivery attempts; active alerts are preserved.

## Initial completion criteria

- The database schema and migrations have crash and concurrent-session tests.
- Deduplication, recurrence, resolution, seen, dismissal, and restoration each
  have explicit transition tests.
- Reconnect obtains a complete snapshot before live changes.
- The navbar has one server state owner and no local persistent history owner.
- No logs or progress frames are copied into the alert database.
- Routing and delivery remain a separate later slice unless the local lifecycle
  is already proven.
