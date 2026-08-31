# Configuration and storage layout

This is the canonical description of LinuxIO's indexer state and bridge-owned
per-user configuration. Files with different owners or lifecycles remain
separate. LinuxIO does not install a host-wide frontend configuration file; the
frontend router policy remains source-controlled.

## Managed indexer files

| Path | Owner and lifecycle | Purpose |
|---|---|---|
| `/etc/linuxio/indexer/config.yaml` | `root:root`; atomically updated by the indexer | Strict YAML indexer configuration. |
| `/etc/systemd/system/linuxio-indexer-tcp.socket` | `root:root`; created only while `listen_addr` is enabled | Optional read-only TCP socket activation unit. |
| `/var/lib/linuxio/indexer/indexer.db` | `root:root`; persistent indexer state | SQLite index data; `-wal` and `-shm` sidecars may appear beside it. |
| `/run/linuxio/indexer.sock` | `root:root`, mode `0600`; recreated by systemd | Root-only indexer HTTP API socket for privileged bridges and the timer. |

The indexer settings API replaces its configuration atomically. The service's
only writable `/etc` path is `/etc/linuxio/indexer`; index data is written only
under `/var/lib/linuxio/indexer`.

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

`linuxio.target` wants `linuxio-webserver.socket`, `linuxio-auth.socket`,
`linuxio-indexer.socket`, and `linuxio-indexer-index.timer`. The webserver
socket weakly wants the indexer socket, and the webserver service weakly wants
the indexer service without service-to-service ordering. The indexer socket is
bound to the webserver socket, so it follows the application entrypoint.

When `listen_addr` is enabled, its privileged settings action creates and
enables `linuxio-indexer-tcp.socket` for both `linuxio.target` and
`linuxio-webserver.socket`, with the same lifecycle binding.

Unix and read-only TCP requests socket-activate `linuxio-indexer.service`. The
timer and manual index actions ask that daemon to perform work; they do not
start a second scanner or database owner. After the configured idle timeout,
the daemon exits while both enabled sockets remain available for the next
request. Indexer failure is non-fatal to the webserver and other LinuxIO
services. Indexer-backed bridge routes use LinuxIO's standard privileged-route
metadata, so only a sudo-authorized bridge running as root can open the
root-only socket.
