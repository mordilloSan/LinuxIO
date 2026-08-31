# Filesystem Indexer

LinuxIO ships `linuxio-indexer` as a first-party, root-owned service. It keeps
the SQLite cache used for filesystem search, recursive directory sizes and
counts, and direct-subfolder sizes. The filesystem remains authoritative.
Filesystem validation and mutations belong to `linuxio-bridge`; a stale or
missing index never authorizes or blocks a filesystem mutation.

The implementation follows up on FileBrowser Quantum's
[Apache-2.0-licensed indexer](https://github.com/gtsteffaniak/filebrowser/blob/main/LICENSE).
Attribution and dependency licenses are recorded in
[Third-Party Notices](THIRD_PARTY_NOTICES.md).

## Architecture and trust boundary

```text
Browser
  | authenticated WebSocket
linuxio-webserver (DynamicUser, relays request bytes)
  | per-session yamux connection
linuxio-bridge (enforces session and privileged-route policy)
  | HTTP over root-only Unix socket
linuxio-indexer (root-owned scanner and SQLite cache)
```

The webserver does not parse bridge request payloads. It relays bytes between
the browser WebSocket and the authenticated session's yamux connection. The
bridge router checks the route's privileged metadata and, for an authorized
root session, calls `/run/linuxio/indexer.sock`.

The webserver should not connect to the indexer directly. Its systemd service
uses `DynamicUser=yes`, while the indexer socket is `root:root` mode `0600` and
the daemon also checks that the Unix peer UID is `0`. Giving the webserver
direct access would widen its privileges and move or duplicate bridge route
authorization. Keeping the indexer client in the bridge preserves the existing
webserver, bridge, and authentication boundaries.

| Boundary | Owner |
|----------|-------|
| Scanner, generations, SQLite storage, HTTP/SSE daemon | `linuxio-indexer` |
| Daemon wire routes and types | `backend/indexer/api` |
| Unix-socket client and LinuxIO API adapters | `backend/bridge/handlers/indexer` |
| Public generated bridge contract | `backend/bridge/apischema` |
| Filesystem validation and mutation policy | `linuxio-bridge` file-browser handlers |
| Authentication and privileged-session creation | `linuxio-auth` and the bridge bootstrap path |
| Browser transport relay | `linuxio-webserver` |

One daemon owns lifecycle and operation admission. Full scans run in a private,
bounded worker subprocess of the same binary; manual requests, the timer, and
scoped reindexing never start an independent daemon.

## Installed files and units

| Path or unit | Purpose |
|--------------|---------|
| `/usr/local/bin/linuxio-indexer` | Daemon and private full-scan worker |
| `/etc/linuxio/indexer/config.yaml` | Strict two-field YAML configuration |
| `/var/lib/linuxio/indexer/indexer.db` | Persistent, rebuildable SQLite cache |
| `/run/linuxio/indexer.sock` | Root-only Unix API socket, `root:root`, mode `0600` |
| `linuxio-indexer.socket` | Owns the socket and activates the daemon |
| `linuxio-indexer.service` | Sandboxed scanner and database owner |
| `linuxio-indexer-index.timer` | Periodic full-index schedule |
| `linuxio-indexer-index.service` | Requests a full index from the daemon |

The daemon is socket activated. The timer makes its first request after five
minutes and then hourly by default. `linuxio-webserver.service` weakly starts
the daemon and owns `/run/linuxio/webserver`, an activity marker that keeps the
indexer warm while the webserver is running. The marker does not grant socket
access.

When the marker is absent and no request, event stream, or scan is active, the
daemon exits after a 90-second idle grace. The socket remains ready to activate
it again.

The service can write only its configuration and state directories. It is
limited to Unix sockets and receives `CAP_DAC_READ_SEARCH` for scanning the
host filesystem. Shutdown cancels background work, closes HTTP listeners,
waits for the work to exit, and then closes SQLite.

## Fixed policy and configuration

The installed database location is fixed:

```text
/var/lib/linuxio/indexer/indexer.db
```

There is no supported database-path setting or command-line flag. `db_path` is
an unknown YAML/JSON field and is rejected. The daemon and its private worker
both use the compiled path. Tests may pass temporary paths directly to internal
storage functions, but that is not an operator interface.

The persisted configuration contains only operator scan choices:

```yaml
exclude_paths: []
include_network_mounts: false
```

`exclude_paths` adds paths to the mandatory exclusions. The scanner always
excludes `/proc`, `/dev`, `/sys`, and `/var/lib/linuxio/indexer`, so virtual
filesystems and the database cannot index themselves. Mandatory exclusions
cannot be removed through the file or API. Unknown fields and invalid values
fail strict configuration decoding.

Network mounts are skipped unless `include_network_mounts` is true. Docker
overlay `merged` trees are always skipped to avoid indexing duplicate layer
contents. Directory symlinks are recorded as entries but are not traversed.

| Setting | Fixed value |
|---------|-------------|
| Index root | `/` |
| Database | `/var/lib/linuxio/indexer/indexer.db` |
| SQLite options | `storage.DefaultOpenOptions()` |
| Full-text search | enabled |
| Hidden entries | included |
| Search result cap | `100` |
| Search query cap | `256` bytes |
| Idle grace | `90s` |
| Idle check interval | `15s` |
| Completed generations retained | `1` |
| Full scan publication | atomic generation |

Both configuration fields apply to the next scan without a daemon restart. The
recurring interval is a systemd timer setting, not a YAML field. LinuxIO writes
it through `indexer.set_timer_interval`.

## Operation

### Full index

A full index is requested by the dashboard, `POST /index`, or the systemd
timer. The daemon admits one operation and starts its private `--index-mode`
worker. The worker:

1. opens the canonical SQLite database;
2. creates an unpublished generation;
3. walks `/`, applying mandatory and operator exclusions;
4. streams entries into SQLite in batches and reports progress to the daemon;
5. publishes the generation only after the scan succeeds;
6. checkpoints SQLite and removes older completed generations.

Queries continue to use the previous completed generation during the scan. A
failed or cancelled scan does not replace it. Ordinary per-entry filesystem
errors, including inaccessible files or subtrees, are logged and counted, then
the walk continues. Context cancellation, a deadline, or failure to write the
worker progress stream aborts the scan.

Until one generation completes, status is `uninitialized`. Search, directory
details, and subfolder queries return `503 Service Unavailable` in that state.

### Scoped reindex

`POST /reindex?path=...` reconciles one path against the latest completed
generation inside a transaction. It scans the subtree, removes cached entries
that no longer exist below it, updates ancestor aggregates by the size delta,
and commits atomically. If the path no longer exists, its stale cached subtree
is deleted. A request for a regular file is rejected; reindex its parent or use
`POST /add` for the one entry.

### Mutation reconciliation

After a successful bridge filesystem mutation, the bridge sends a best-effort
notification:

| Change | Daemon action |
|--------|---------------|
| Create or replace one entry | `POST /add` |
| Remove a path | `DELETE /delete` |
| Copy or extract a directory tree | scoped reindex |
| Move or rename | delete the old path and add or reindex the new path |

These notifications update the cache; they are not part of the mutation's
authorization or durability. A failed notification is logged and a later
scoped or full index repairs the cache. There is no durable notification queue.

### Concurrency and recovery

Full index, scoped reindex, add, and delete share one daemon operation lock.
Conflicting requests return `409 Conflict`. Active requests, streams, and scans
pin the daemon so idle shutdown cannot interrupt them.

SQLite schema compatibility is checked at open through `PRAGMA user_version`.
When SQLite confirms corruption, the daemon moves the database and any WAL/SHM
sidecars to `indexer.db.corrupt-<UTC timestamp>*`. An incompatible schema uses
`indexer.db.incompatible-<UTC timestamp>*`. It then creates an empty compatible
database. Busy, timeout, permission, and ordinary I/O errors preserve the
existing files and fail startup instead of replacing data.

The worker never performs quarantine. This prevents it from renaming a database
behind the daemon's open handle.

## Public LinuxIO bridge API

The frontend uses the generated bridge contract, not the daemon's Unix HTTP
API. Every route below is privileged. Unprivileged sessions are rejected by the
bridge before an indexer request is made.

| Route | Kind | Request | Result |
|-------|------|---------|--------|
| `indexer.get_config` | Call | none | `exclude_paths`, `include_network_mounts`, `interval` |
| `indexer.get_status` | Call | none | daemon state, counts, sizes, active operation, warning |
| `indexer.set_config` | Call | partial `exclude_paths` and/or `include_network_mounts` | saved combined config |
| `indexer.set_timer_interval` | Call | `interval` | saved `interval` |
| `filebrowser.index` | session Task, system singleton | optional `path`; absent or empty means `/` | progress plus final index/reindex totals |
| `filebrowser.search` | Call | `query`, optional `basePath`, optional compatibility `limit` | matching filesystem entries |
| `filebrowser.dir_size` | Call | `path` | `size`, `fileCount`, `folderCount` |
| `filebrowser.subfolders` | Call | `path`; empty means `/` | direct subfolder paths and sizes |

The daemon always caps search at 100 results. The public `limit` field remains
in the bridge request shape for compatibility but cannot raise or lower that
daemon cap.

`filebrowser.index` maps daemon SSE events into normal LinuxIO Task progress and
results. If it finds an already-running matching daemon operation, it attaches
instead of starting another scan. Task observation and recovery use the common
`tasks.*` routes.

The status result contains:

| Field | Meaning |
|-------|---------|
| `status` | `uninitialized`, `running`, or `idle` |
| `running` | whether an index/reindex operation is active |
| `num_dirs`, `num_files`, `total_size` | latest completed generation totals |
| `last_indexed` | completion time in RFC 3339 form, when initialized |
| `database_size` | current SQLite file size in bytes |
| `active_operation`, `active_operation_id`, `active_path` | active work identity, when running |
| `warning` | partial status-read warning, when available |

The daemon's internal `/status` response has the same snake-case fields except
for `running`, which the bridge derives from `status`. Absent string values are
empty or omitted on the internal wire and become nullable fields in the public
bridge result.

## Internal daemon HTTP/SSE API

This is an in-repository service contract defined in `backend/indexer/api`. It
is not a network or third-party API. The server accepts only its systemd Unix
socket and checks `SO_PEERCRED`; every endpoint requires peer UID `0`.

Successful non-streaming responses use JSON. Errors use an HTTP status and a
plain-text message.

| Method and path | Input | Success response | Notes |
|-----------------|-------|------------------|-------|
| `POST /index` | none | `202` operation response | Starts a full atomic-generation scan |
| `POST /reindex?path=<path>` | absolute path | `202` operation response | Reconciles one directory subtree |
| `GET /status` | none | status object | Returns current state and latest completed totals |
| `GET /status?stream=1&operation=<op>&operation_id=<id>&path=<path>` | exact active-operation tuple | `text/event-stream` | Attaches to active work; a mismatch falls back to JSON status |
| `GET /search?q=<query>&base=<path>` | query and optional base, default `/` | array of entry results | Query max 256 bytes; result cap 100; `limit` is ignored |
| `GET /dirsize?path=<path>` | optional path, default `/` | path, recursive size, file count, directory count | Reads cached aggregates |
| `GET /subfolders?path=<path>` | optional path, default `/` | array of direct-subfolder results | Each result includes path, name, size, and modification time |
| `POST /add` | strict JSON `{"path":"/absolute/path"}` | `{"status":"ok"}` | Stats and upserts one current entry; excluded paths are a successful no-op |
| `DELETE /delete?path=<path>` | non-root path | `{"status":"ok"}` | Deletes the cached path recursively and adjusts ancestors |
| `GET /config` | none | persisted two-field config | Timer interval is not part of this response |
| `PUT /config` | strict partial config JSON | full saved config | Maximum body 1 MiB; atomic config-file write |

Paths are normalized absolute paths rooted at `/`. Traversal outside that root
is rejected.

An accepted index or reindex returns:

```json
{
  "status": "running",
  "path": "/",
  "operation_id": "<opaque operation ID>"
}
```

Use all three returned identity values to attach to its event stream. SSE event
names and payloads are:

| Event | Payload |
|-------|---------|
| `started` | `status`, `operation`, `operation_id`, `path` |
| `progress` | operation identity plus optional `phase`, `message`, `files_indexed`, `dirs_indexed`, `current_path`, `bytes_indexed` |
| `complete` | operation identity, `duration_ms`, totals, and optional `deleted_entries` |
| `error` | operation identity and `message` |

Search entry results contain `path`, `name`, `type`, `size`, `mod_time`,
`inode`, and directory aggregate fields when applicable. Direct-subfolder
results contain `path`, `name`, `size`, and `mod_time`.

Common error statuses are:

| Status | Meaning |
|--------|---------|
| `400 Bad Request` | invalid path/body/query, missing generation for a mutation, or invalid config |
| `403 Forbidden` | non-Unix transport or peer UID other than root |
| `404 Not Found` | `/add` target disappeared before it could be read |
| `405 Method Not Allowed` | wrong method on a mutation or config endpoint |
| `409 Conflict` | another operation is active |
| `500 Internal Server Error` | storage, config write, or unexpected daemon failure |
| `503 Service Unavailable` | cache query before the first completed generation |

## Administration

Start a full scan and inspect status:

```bash
sudo curl --unix-socket /run/linuxio/indexer.sock \
  -X POST http://localhost/index
sudo curl --unix-socket /run/linuxio/indexer.sock \
  http://localhost/status
```

Inspect or change daemon configuration:

```bash
sudo curl --unix-socket /run/linuxio/indexer.sock \
  http://localhost/config
sudo curl --unix-socket /run/linuxio/indexer.sock \
  -H 'Content-Type: application/json' \
  -X PUT \
  -d '{"include_network_mounts":true}' \
  http://localhost/config
```

Useful diagnostics:

```bash
linuxio logs indexer
linuxio version
systemctl status \
  linuxio-indexer.socket \
  linuxio-indexer.service \
  linuxio-indexer-index.timer
journalctl \
  -u linuxio-indexer.service \
  -u linuxio-indexer-index.service \
  --since today
```

`sudo linuxio verbose enable` enables debug logging for the webserver and
indexer service drop-ins. `sudo linuxio verbose disable` removes both.

Upgrades preserve compatible caches. If manual recovery is needed, stop
LinuxIO, move `/var/lib/linuxio/indexer/indexer.db*` to a separate directory,
start LinuxIO, and run a full index. Move rather than delete the files so the
old cache remains available for inspection.

## Disposable-host smoke test

The repository includes an opt-in systemd smoke test. It requires built local
binaries, root, systemd as PID 1, and a clean disposable host:

```bash
make build
LINUXIO_SYSTEMD_SMOKE=1 \
LINUXIO_SYSTEMD_SMOKE_CONFIRM=disposable-linuxio-host \
  make test-indexer-systemd-integration
```

Without `LINUXIO_SYSTEMD_SMOKE=1`, the script exits without changing the host.
