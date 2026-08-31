# Filesystem Indexer

LinuxIO ships its filesystem indexer as the first-party `linuxio-indexer`
binary. It maintains the SQLite data used for file search, recursive directory
sizes, entry counts, and direct subfolder summaries. Index data is an
acceleration layer only: filesystem mutations and authorization remain owned by
`linuxio-bridge`, and stale index data is never authoritative for an operation.

The implementation follows up on FileBrowser Quantum's
[Apache-2.0-licensed indexer](https://github.com/gtsteffaniak/filebrowser/blob/main/LICENSE)
and adapts its indexing code as a dedicated LinuxIO service. LinuxIO's changes
focus on low memory use, an on-demand API, fast(ish) full indexing, and
efficient query responses.
Attribution and dependency licenses are recorded in
[Third-Party Notices](THIRD_PARTY_NOTICES.md).

## Processes and ownership

| Boundary | Owner |
|----------|-------|
| Scanner, SQLite database, HTTP/SSE daemon | `linuxio-indexer` |
| Daemon wire routes and types | `backend/indexer/api` |
| Unix-socket client and LinuxIO API adapters | `backend/bridge/handlers/indexer` |
| Public generated bridge contract | `backend/bridge/apischema` |
| Filesystem validation and mutation policy | `linuxio-bridge` file-browser handlers |

One daemon owns the database. Manual indexing, the timer, and scoped reindexing
all ask that daemon to do work through `/run/linuxio/indexer.sock`; they never start a
second scanner/database owner. The daemon serializes maintenance operations and
returns HTTP 409 when another operation is already active.

The bridge checks the in-repository protocol version returned by `/status`.
LinuxIO supports its matching bundled daemon rather than an external indexer
compatibility matrix.

The release installer currently supports amd64 (`x86_64`) binaries only. Use
`packaging/scripts/localinstall.sh` after a host build on another architecture;
the local installer remains host-built and does not apply the release
architecture guard.

## Installed files and units

| Path or unit | Purpose |
|--------------|---------|
| `/usr/local/bin/linuxio-indexer` | Managed daemon and private systemd worker modes |
| `/etc/linuxio/indexer/config.yaml` | Strict YAML configuration; preserved during upgrades |
| `/var/lib/linuxio/indexer/indexer.db` | Persistent index database |
| `/run/linuxio/indexer.sock` | Root-only local API socket, `root:root`, mode `0600` |
| `/etc/systemd/system/linuxio-indexer-tcp.socket` | Optional generated TCP socket unit; exists only while `listen_addr` is enabled |
| `linuxio-indexer.socket` | Owns the activity socket and activates the daemon |
| `linuxio-indexer.service` | Sandboxed database/scanner owner |
| `linuxio-indexer-index.timer` | Systemd-owned periodic schedule |
| `linuxio-indexer-index.service` | Asks the daemon to start a full index |

Indexer-backed bridge routes use `apischema.Privileged()`, following LinuxIO's
normal [privilege pattern](privilege_pattern.md). The dispatcher rejects an
unprivileged session before its handler runs. A sudo-authorized session keeps
`linuxio-bridge` as root and can open the root-only socket; the C authentication
helper needs no indexer-specific group or socket logic.

The shared capability scan checks `linuxio-indexer.socket` without waking the
daemon so index-backed UI features can degrade cleanly. This is an internal
health gate; the first-party indexer is not listed in the Capability Manager.

The TCP API is disabled while `listen_addr` is empty. A privileged settings
action validates the address and creates, enables, or removes
`linuxio-indexer-tcp.socket`; changing it requires a daemon restart. When
enabled, systemd keeps that listener active with `linuxio-webserver.socket` and
socket-activates the same indexer daemon that serves the Unix socket. TCP serves
only the API's `GET` and `HEAD` routes without authentication; this intentionally
keeps search authentication-free. Indexing, maintenance, entry mutation, and
configuration writes remain restricted to UID 0 over the Unix socket. Bind
`listen_addr` only to networks where exposing indexed paths, sizes, status, and
configuration is acceptable. The bridge reads the effective address from
systemd; the daemon YAML does not duplicate it.

The binary does not expose a separate administration CLI. The managed daemon
requires systemd activation descriptors; systemd alone uses the private index
worker and timer-trigger modes. The daemon owns its YAML configuration, while
the bridge owns LinuxIO's systemd timer and optional TCP socket settings.

`linuxio-indexer.socket` is bound to `linuxio-webserver.socket`. Starting or stopping
the LinuxIO entrypoint therefore controls whether the indexer activity socket
exists, while an indexer failure remains non-fatal to the rest of LinuxIO.

The daemon exits after `idle_timeout` when there are no requests, SSE streams,
or indexing operations. Both enabled sockets remain available, so the next
Unix or TCP request starts a fresh daemon. The default timeout is two minutes.

## Configuration

LinuxIO's settings API reads and writes `/etc/linuxio/indexer/config.yaml` atomically.
The configuration covers scan roots and visibility, SQLite behavior, query
limits, and daemon idle timeout.

- Scan settings such as `exclude_paths`, `include_hidden`, `include_network_mounts`,
  `fresh_index`, `fts_search`, `keep_indexes`, and `integrity_check` take
  effect on the next full index.
- Query limits and `idle_timeout` apply at runtime.
- Database connection settings and `db_path` are persisted but require a daemon
  restart.
- A non-empty `listen_addr` enables the read-only, socket-activated TCP API after
  restart; an empty value disables and removes its generated socket unit. The
  bridge reads this value back from systemd rather than daemon YAML.
- Setting the timer interval to `0` disables the timer. Other Go duration
  values, such as `30m` or `6h`, update the LinuxIO-owned systemd drop-in,
  enable the unit, and restart it through systemd D-Bus. The bridge reads the
  configured interval back from systemd; it is not duplicated in daemon YAML.

The default index root is `/`. `exclude_paths` lists absolute folders that the
scanner skips, including all descendants; the packaged configuration lists
`/proc` and `/dev` explicitly. Network mounts are excluded unless explicitly
enabled. Docker overlay `merged` views are always skipped to avoid indexing
duplicate layer contents. The service may read the host filesystem but its
persistent writes are limited to the canonical config and index state directories.
It also receives an isolated writable temporary directory for SQLite work files.

## Lifecycle and recovery

The normal lifecycle is:

1. `linuxio.target` starts `linuxio-indexer.socket`, the optional enabled
   `linuxio-indexer-tcp.socket`, and the enabled `linuxio-indexer-index.timer`.
2. A Unix API request, read-only TCP request, or timer connection
   socket-activates `linuxio-indexer.service`.
3. A full index publishes a completed generation atomically; failed scans do
   not replace the last completed generation.
4. File mutations running through a privileged bridge send best-effort
   add/delete/scoped-reindex notifications. An unprivileged bridge cannot open
   the root-only socket, so its changes—and any failed privileged notification—
   are reconciled by the next scoped or full index. There is intentionally no
   durable mutation queue.
5. The daemon exits when idle and is reactivated on demand.

Upgrades replace the binary and units while preserving the config and database.
The installers also perform an idempotent migration from LinuxIO's former
standalone indexer: they stop and remove `indexer.target`, `indexer.socket`,
`indexer.service`, `indexer-index.service`, `indexer-index.timer`,
`/usr/local/bin/indexer`, and `/run/indexer.sock`. Legacy `/etc/indexer`,
`/var/lib/indexer`, and database files are left untouched for recovery. The
uninstaller also preserves `/var/lib/linuxio` unless explicitly invoked with
`--remove-data`.

The installer preserves an existing generated TCP socket but does not recreate
runtime settings. Legacy YAML `socket_path` and `listen_addr` fields are accepted
during the supported upgrade window and dropped on the next save; they no longer
change listeners. Enable the privileged TCP setting again if its generated unit
is missing. The daemon never binds sockets directly.

To roll back, reinstall the prior immutable amd64 release tag:

```bash
sudo /path/to/install-linuxio-binaries.sh v0.3.0
```

The versioned release binaries are replaced atomically; the installer does not
overwrite `/etc/linuxio/indexer/config.yaml` or
`/var/lib/linuxio/indexer/indexer.db`. A rollback therefore keeps the current
configuration and index data. If the older binary requires a rebuilt schema,
move the database aside and run a full index so the old database remains
recoverable.

Useful diagnostics:

```bash
systemctl status linuxio-indexer.socket linuxio-indexer-tcp.socket linuxio-indexer.service linuxio-indexer-index.timer
journalctl -u linuxio-indexer.service -u linuxio-indexer-index.service --since today
sudo curl --unix-socket /run/linuxio/indexer.sock http://localhost/status
sudo curl --unix-socket /run/linuxio/indexer.sock -X POST http://localhost/index
```

If the database is suspected to be stale, run a full index. If it is corrupt,
stop LinuxIO, move `/var/lib/linuxio/indexer/indexer.db*` aside, start LinuxIO,
and run a full index. Moving the files keeps recovery reversible; deleting
persistent data is not required for ordinary upgrades or reinstalls.

## Disposable-host smoke test

The repository includes an opt-in systemd smoke test. It requires built local
binaries, root, systemd as PID 1, and a clean disposable host; it installs and
uninstalls LinuxIO and removes the test data:

```bash
make build
LINUXIO_SYSTEMD_SMOKE=1 \
LINUXIO_SYSTEMD_SMOKE_CONFIRM=disposable-linuxio-host \
  make test-indexer-systemd-integration
```

Without `LINUXIO_SYSTEMD_SMOKE=1` the script exits without changing the host.
