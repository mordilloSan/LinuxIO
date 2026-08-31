# Configuration and storage layout

This is the canonical description of LinuxIO's indexer state and bridge-owned
per-user configuration. Files with different owners or lifecycles remain
separate. LinuxIO does not install a host-wide frontend configuration file.

## Managed indexer files

| Path | Owner and lifecycle | Purpose |
|---|---|---|
| `/etc/linuxio/indexer/config.yaml` | `root:root`; atomically updated by the indexer | Two persisted scan choices: operator exclusions and network-mount policy. |
| `/var/lib/linuxio/indexer/indexer.db` | `root:root`; persistent rebuildable cache | SQLite index data; `-wal` and `-shm` sidecars may appear beside it. |
| `/run/linuxio/indexer.sock` | `root:root`, mode `0600`; recreated by systemd | Root-only Unix HTTP/SSE API socket. |

The config API writes only the canonical YAML atomically. The service's only
writable `/etc` path is `/etc/linuxio/indexer`; index data is written only
under `/var/lib/linuxio/indexer`. The managed index root is `/`, the database
path and SQLite options are fixed, hidden entries are included, FTS is enabled,
and one completed generation is retained.

The effective exclusion set merges operator `exclude_paths` with the mandatory
`/proc`, `/dev`, `/sys`, and `/var/lib/linuxio/indexer` exclusions. The
mandatory paths cannot be removed through the config API.

## Per-user bridge files

For an authenticated user, the bridge tries the home directory first and then
`/var/lib/linuxio/users/<uid>`:

| Home file | Fallback file | Purpose |
|---|---|---|
| `$HOME/.linuxio-config.yaml` | `/var/lib/linuxio/users/<uid>/.linuxio-config.yaml` | Functional bridge settings. |
| `$HOME/.linuxio-ui.yaml` | `/var/lib/linuxio/users/<uid>/.linuxio-ui.yaml` | UI preferences. |
| `$HOME/.linuxio-config.yaml.lock` | `/var/lib/linuxio/users/<uid>/.linuxio-config.yaml.lock` | Config sidecar lock. |
| `$HOME/.linuxio-ui.yaml.lock` | `/var/lib/linuxio/users/<uid>/.linuxio-ui.yaml.lock` | UI sidecar lock. |

An invalid core document is retained as
`.linuxio-config.yaml.broken-<UTC timestamp>` before defaults are written. If
neither persistent tier is safe, the bridge uses memory and creates no files.
See [Bridge Configuration Storage](config-storage.md) for ownership, fallback,
validation, and recovery behavior.

## Systemd lifecycle

`linuxio.target` wants `linuxio-webserver.socket`, `linuxio-auth.socket`, and
`linuxio-indexer.socket`. The enabled `linuxio-indexer-index.timer` attaches to
the target through its `WantedBy=` symlink. The indexer socket is bound to the
webserver socket, so it follows the application entrypoint, while indexer
failure remains non-fatal to the webserver and other LinuxIO services.

The Unix socket activates `linuxio-indexer.service`. The timer and manual index
actions ask that daemon to perform work; they do not start a second scanner or
database owner. While `linuxio-webserver.service` runs, its systemd-owned
`/run/linuxio/webserver` directory pins the indexer without granting socket
access. After that marker and active work are gone for the fixed 90-second
idle grace, the daemon exits while systemd retains the socket for the next request. Indexer-backed bridge routes
use standard privileged-route metadata, so only a sudo-authorized bridge
running as root can open the socket.
