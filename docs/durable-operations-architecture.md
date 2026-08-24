# Durable Operations Architecture

This document describes the implemented durable-operation boundary. Most Tasks
remain session-bound; durability is an explicit route policy for work that has
an external owner and a safe recovery contract. The current durable route is
`docker.update_container`.

## Shared durable record and store

The `backend/common/durabletask` package owns one service-owned JSON record per
operation under the durable-operation data directory. Records are written with
the existing exclusive file lock and atomic writer (including file and parent
directory durability). The record, not an in-memory Task map, a unit name, or a
journal entry, is the authorization and recovery source of truth.

Each record contains:

- a canonical, non-zero UUID `id`, route, numeric owner `uid`, and a
  route-defined safe request fingerprint (and target when applicable);
- `created_at`/`updated_at`, optional start/finish/cancellation timestamps,
  bounded progress, and a bounded typed result or structured error;
- executor kind, identity, and handle; and
- one of `queued`, `launching`, `running`, `completed`, `failed`, `canceled`,
  or `unknown`.

The client supplies the stable operation ID before execution. A claim is
idempotent for the same UID, route, and fingerprint, and conflicts when any of
those identities differ. Route admission can also reject a second active
operation. The initiating session is used for authorization while connected;
its `SessionID` and other raw credentials are never persisted. A later session
for the same numeric UID can read, resume, or cancel according to the route
policy; another UID cannot access the record.

The file store keeps active records until they are terminal. Terminal records
are retained for 30 days and at most the newest 200 records per UID. Pruning
only removes proven terminal records while holding the store lock, and removes
their artifacts with them. Result artifacts are bounded, mode `0600`, and
validated against the operation ID and terminal state before they are applied.

## Lifecycle and recovery

1. The route validates the request and UID, computes its safe fingerprint, and
   claims the stable ID as `queued`.
2. Before external launch, it persists `launching` with a deterministic
   executor identity and handle. It records `running` only after observing
   acceptance. These filesystem and D-Bus operations are intentionally not
   presented as one atomic transaction.
3. The bridge observes the executor and reconciles a typed result artifact or
   terminal executor state into the record. Repeated observations and terminal
   completion are idempotent.
4. A bridge restart or websocket loss does not delete durable state. When the
   owning UID reconnects, route recovery lists its active records and
   reattaches Tasks that resume observation. Recovery validates the persisted
   route, target, fingerprint, and executor identity before doing anything.
5. If the executor is missing or its outcome cannot be proven, the route marks
   the record `unknown` (or `failed` when a launch is definitively absent) and
   never blindly starts a replacement. Host reboot is therefore conservative:
   transient units are not assumed to resume.

Cancellation records intent, invokes the executor's typed stop operation, and
only records `canceled` after the stop or a typed cancellation result is
observed. A queued operation can be canceled before launch. A timeout or
ambiguous stop is represented as `unknown`, not as a false success.

## Shared transient-unit adapter

`backend/bridge/internal/transientunit` is the common systemd D-Bus adapter for
subprocess-shaped durable executors. It uses the existing `dbusclient` and
`godbus` paths to start, inspect, stop, and collect transient units with
bounded timeouts. Inspection verifies the exact unit name, expected
description, and `Transient=true`; a handle is metadata, never the operation
identity. Route packages own command policy, properties, runtime limits, and
result validation. Standard output and error go to journald for diagnostics;
journald is not typed operation state. `CollectMode` cleanup never removes the
service-owned record.

## Implemented durable route

### `docker.update_container`

The route validates the container target and claims an exclusive operation with
the same UID/UUID/fingerprint rules. A deterministic systemd transient unit
owns a root worker. The worker entry point validates the persisted route,
target, and executor identity before running the Docker mutation, then writes a
typed `ExecutorResult` containing `DockerContainerUpdateResult`. The bridge
polls and adopts the unit after uncertain launch, applies the result exactly
once, reports bounded progress, and collects the unit after completion. Missing
or malformed results are failed or marked `unknown` according to the observed
systemd state; no duplicate mutation is launched automatically.

## Session-bound work and extensions

The remaining Task routes keep their existing session lifetime and cancellation
semantics. In particular, `control.app_update` uses a bounded
`systemd-run --wait --pipe` process so installer output reaches the initiating
Task directly. The installer receives `--defer-restart`; after success, the
bridge writes a UID-scoped `/run` status projection, finishes the Task, and then
restarts `linuxio.target`. This deliberately favors observable, ordered updates
over recovery after page, session, or bridge loss. `docker.compose` is likewise
session-bound and has no durable record or external recovery owner.

A new durable route requires all of the following before opting in:

- a stable external owner or job API that survives bridge loss and can be
  queried after restart;
- a route-specific idempotency/resume decision, safe request fingerprint, and
  explicit numeric-UID authorization model;
- a typed terminal result and a bounded, atomically written artifact or job
  record; journald alone is insufficient;
- a typed cancellation operation with a conservative `unknown` outcome;
- bounded runtime, progress, retention, and resource policies; and
- recovery, malformed-record, uncertain-launch, duplicate-completion, and
  authorization tests.

Prefer an existing daemon or transaction owner (for example PackageKit) when it
already provides these guarantees. Use a transient unit only when the route's
command policy and security boundary can be fixed and validated as they are for
the two routes above. Durability is not a generic property that should be added
to every Task.
