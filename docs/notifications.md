# Notifications

> **Status: Planned.** This is the target design; no part of the server-side
> notification store or `notifications.watch` Channel is implemented yet.

This plan describes durable, per-user notifications on the current Call,
Channel, and Task architecture. The initial slice is deliberately small: a
bounded JSON snapshot on disk and durable notifications for terminal Task
outcomes. Broader event sources and user preferences are later phases.

See the [API reliability roadmap](./api-reliability-roadmap.md) for the related
transport and recovery work.

## MVP scope

- One logical `NotificationStore` per user, with the persisted file as the
  authority across that user's bridge processes. There is no database
  dependency in the first implementation.
- A bounded (approximately 200 item) JSON snapshot, written with the existing
  file lock and atomic writer that fsyncs the temporary file and parent
  directory.
- Meaningful, durable events only. Progress updates, transient UI toasts, and
  every low-level source event are not persisted.
- A terminal Task notification is requested after the Task's terminal commit.
  The stable Task ID is the dedupe key, so retries or reconnects cannot create
  duplicates. The server never relies on a frontend recovered event to create
  the notification.

The initial release does not include notification preferences, a full-page
notification experience, or global disk, Docker, and system sources. Those are
follow-up phases once the store and delivery semantics are proven.

## Storage and data model

`NotificationStore` owns one user's persisted snapshot. Each bridge may keep a
small in-memory view and subscriber set, but the file revision—not a process
map—is authoritative across concurrent sessions. A notification
contains only safe, presentation-ready metadata:

```text
id, user_id, created_at, read_at (nullable)
severity (info | warn | error)
source (task | ...future sources...)
title, message
metadata (allow-listed labels and an internal route, when applicable)
```

The store enforces ownership, validates fields, and keeps the newest bounded
set (about 200 records). Inserts and read-state changes take the per-user file
lock, update the snapshot, and atomically replace the file. A failed write does
not publish a delta. Startup treats a missing file as empty; malformed or
unreadable data is reported and kept from being served as a partial snapshot.

Retention is count-bounded in the MVP. Future time-based retention or explicit
expiry must preserve the same atomic-write and per-user isolation guarantees.

## Calls

Bounded operations are direct Calls, not Tasks or Channels:

- `notifications.list` — return the authoritative newest snapshot (with unread
  count and revision).
- `notifications.mark_read` — mark selected IDs read.
- `notifications.mark_unread` — mark selected IDs unread.
- `notifications.mark_all_read` — mark all currently stored items read.
- `notifications.clear` — remove the user's stored items.

Each mutating Call updates the store before returning. The result includes the
new persisted revision so a client can reconcile its cache with a subsequent
watch.

## `notifications.watch` Channel

`notifications.watch` is a server-producing Channel. Opening it reads and sends
one authoritative `snapshot` containing items, unread count, and persisted
revision. Whenever that revision changes, the Channel sends a replacement
snapshot; the frontend replaces the corresponding TanStack Query data.

Local writes publish a coalesced refresh signal through a bounded one-slot
subscriber channel, so a slow client cannot block a writer or accumulate an
unbounded delta queue. Because another bridge process cannot signal that memory
channel, an open watcher also checks the persisted revision at a bounded
interval (initially no more than once per second) and reloads only when it
changes. File locking makes every reload coherent. This gives cross-session
eventual live delivery without adding a notification daemon, filesystem-watch
dependency, or replay log.

A reconnect simply receives the latest snapshot; it does not depend on replaying
a cursor. Closing the Channel removes the subscriber and timer, and no goroutine
or file descriptor remains owned by a disconnected client.

## Task integration

After a Task commits success, failure, or cancellation, the server upserts one
notification for that stable Task ID in the owning user's store. Repeated
handling is idempotent. Task truth does not depend on notification I/O: a
notification write failure is logged and retried while that terminal Task is
available. Durable-operation reconciliation also repairs a missing notification
by stable operation ID after restart. The two files are intentionally eventually
consistent rather than pretending to form one transaction.

An in-memory session Task can still lose its notification if the bridge dies in
the narrow interval after terminal commit and before a successful store write;
the Task itself is not durable either. A connected watcher receives the next
authoritative snapshot after persistence, while a later connection obtains it
on open.

## Frontend behavior

TanStack Query owns the server notification cache. `notifications.list` seeds
the query and `notifications.watch` replaces it with newer snapshots through the
same query update path. Sonner remains a presentation layer for newly received
items; it does not become a second source of truth. At cutover, remove the
current localStorage toast-history implementation rather than migrating it.

## Security, privacy, and failure behavior

- Every Call and Channel is scoped to the authenticated user; IDs from another
  user are ignored or rejected without revealing their existence.
- Metadata is allow-listed. Do not persist raw requests, credentials, command
  output, arbitrary HTML, or arbitrary external links. Internal routes must be
  validated against the frontend route policy.
- File permissions and lock ownership follow the existing per-user state-file
  conventions. A crash during replacement leaves either the prior complete
  snapshot or the new complete snapshot, never a partially written document.
- Disk-full, permission, lock-timeout, and decode failures from notification
  Calls are surfaced to the caller and logs with context. Background source
  failures are logged and remain eligible for idempotent reconciliation. They do
  not change the source Task outcome or publish an incomplete snapshot.
- Clearing and retention are bounded operations; no unbounded history or
  cross-user aggregation is introduced by this design.

## Later phases

After the MVP has operational metrics and recovery tests, add preferences and
toast policy, a full notifications page, and carefully selected disk, Docker,
and system sources. Each source must emit meaningful, deduplicated events into
the same per-user store; source-specific progress remains ephemeral.
