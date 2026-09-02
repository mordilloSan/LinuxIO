# Process & Systemd Architecture

How LinuxIO's processes are wired together by **systemd socket activation**
under one umbrella **`linuxio.target`**. This is the deployment counterpart to
the [Server Yamux Protocol](./server-yamux-protocol.md), which covers what flows
*through* the webserver↔bridge connection once it exists.

## Why Multiple Binaries

LinuxIO uses **privilege separation** (the same shape Cockpit uses): split the system into the smallest pieces that each need a given privilege, so the large, network-facing code never runs as root.

| Concern | Binary | Why it's separate |
|---------|--------|-------------------|
| Serve HTTP/WebSocket to the browser | `linuxio-webserver` | Big attack surface (TLS-less HTTP, untrusted input) → runs **unprivileged**, heavily sandboxed, never root. |
| Authenticate (PAM) and launch a session | `linuxio-auth` | Needs **root** for PAM + privilege drop → kept tiny, written in C, audited, socket-activated per connection. |
| Execute user-facing operations | `linuxio-bridge` | Runs with **exactly the logged-in user's** privileges (or root only when the user is privileged). One process per login. |
| Maintain the filesystem index | `linuxio-indexer` | Needs broad read access but a narrow write boundary → isolated, sandboxed, and socket activated. |
| Operate/inspect the stack | `linuxio` (CLI) | Convenience wrapper over `systemctl`/`journalctl` → runs as the invoking admin. |

## Binaries

| Binary | Lang | Source | Runs as | Lifetime | Role |
|--------|------|--------|---------|----------|------|
| `linuxio` | Go | `backend/` (`main.go`) | invoking user (often via `sudo`) | on-demand | CLI: status, logs, start/stop/restart, verbose, version |
| `linuxio-webserver` | Go | `backend/webserver/` | `DynamicUser` + group `linuxio-bridge-socket` | long-running (socket-activated) | HTTP + WebSocket relay; **yamux client** |
| `linuxio-auth` | C | `backend/auth/linuxio-auth.c` | `root` | per auth connection (supervises its bridge) | PAM authentication, sudoers policy query, fork + supervise bridge |
| `linuxio-bridge` | Go | `backend/bridge/` | logged-in user (root only if privileged) | per login session | **yamux server**; executes operations |
| `linuxio-docker-update` | Go | `backend/docker-update/` | root, sandboxed | per scheduled or durable update operation | Docker update worker |
| `linuxio-indexer` | Go | `backend/indexer/` | root, sandboxed | socket activated; exits when idle | Filesystem scanner, SQLite index, local HTTP/SSE API |

These binaries install to `/usr/local/bin/`. The CLI is a thin management
front-end; the remaining processes are independently constrained by their
responsibilities.

### Binary invocation map

Only `linuxio` is an operator administration CLI. The other binaries expose
metadata for diagnostics and narrowly scoped process modes owned by systemd or
the auth/bridge launch protocol.

| Binary | Supported interactive surface | Managed or private surface |
|---|---|---|
| `linuxio` | `status`, `logs`, `start`, `stop`, `restart [--full]`, `verbose enable\|disable\|status`, `version [--self]`, `help`/`-h`/`--help` | None |
| `linuxio-webserver` | `help`/`-h`/`--help`, `version`/`-v`/`--version` | `run [-port N] [-verbose]`; used by `linuxio-webserver.service` |
| `linuxio-auth` | `version`, `--version` | No-argument accepted-socket process; root and systemd only |
| `linuxio-bridge` | `version`, `--version`, `-v` | No-argument session process with inherited fd 3 and auth bootstrap; `linuxio-auth` only |
| `linuxio-docker-update` | `help`/`-h`/`--help` | `run [--config PATH]` for the managed timer and `run-operation --id ID` for transient durable work |
| `linuxio-indexer` | `--help`, `-h`, `--version` | Managed daemon; private `--trigger-index` timer client and `--index-mode` scanner worker |

The private indexer modes are an implementation protocol between the daemon and
its systemd units, not alternate administration paths. Indexer status,
configuration, and indexing remain available through LinuxIO and its HTTP APIs.

## Systemd Units

Core units under `linuxio.target`:

```
linuxio.target                       umbrella; WantedBy=multi-user.target
├─ linuxio-webserver.socket          TCP :8090 (dual-stack) → activates webserver.service
│  └─ linuxio-webserver.service      runs `linuxio-webserver run` (DynamicUser, sandboxed)
├─ linuxio-auth.socket               unix /run/linuxio/auth.sock (Accept=yes) → per-conn instance
│  └─ linuxio-auth@.service          one instance per connection; root; forks+supervises a bridge
├─ linuxio-indexer.socket            unix /run/linuxio/indexer.sock → activates linuxio-indexer.service
│  └─ linuxio-indexer.service        runs the managed daemon; exits when idle
├─ linuxio-indexer-index.timer      periodic request to linuxio-indexer-index.service
│  └─ linuxio-indexer-index.service asks the daemon for a full index
├─ linuxio-bridge-socket-user.service  oneshot: materializes the linuxio-bridge-socket user/group
└─ linuxio-issue.service             oneshot: updates the login issue/MOTD
```

Key unit facts (see `packaging/systemd/`):

- **`linuxio-webserver.socket`** — `ListenStream=8090`, `BindIPv6Only=both` (one socket answers both the A and AAAA records Avahi publishes). systemd owns the listening fd; the service inherits it.
- **`linuxio-webserver.service`** — `ExecStart=/usr/local/bin/linuxio-webserver run`, `DynamicUser=yes`, `Group=linuxio-bridge-socket`. `StateDirectory=linuxio/webserver` gives the dynamic user private persistent storage for the managed TLS certificate. `RuntimeDirectory=linuxio/webserver` is an indexer activity marker whose lifetime is owned by systemd; `Wants=linuxio-indexer.service` warms the indexer without making it a prerequisite. Extensive hardening: `ProtectSystem=strict`, `PrivateDevices`, `MemoryDenyWriteExecute`, `NoNewPrivileges`, `SystemCallFilter`, `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`, etc.
- **`linuxio-auth.socket`** — `ListenStream=/run/linuxio/auth.sock`, `SocketUser=root`, `SocketGroup=linuxio-bridge-socket`, `SocketMode=0660`, **`Accept=yes`**, `MaxConnections=16`. The group+mode is the access-control boundary: only members of `linuxio-bridge-socket` (i.e. the webserver) may connect.
- **`linuxio-auth@.service`** — template; one instance per accepted connection, with the connected socket as `StandardInput=socket`. `User=root`. `TasksMax=1024` / `MemoryMax=2G` bound the bridge it spawns, because **the bridge runs inside this instance's cgroup** (see below).
- **`linuxio-indexer.socket` / `linuxio-indexer.service`** — the `root:root`,
  mode-`0600` Unix socket activates one database/scanner owner. The daemon exits
  after its fixed idle grace while systemd retains the socket. Indexer-backed
  bridge routes are privileged, so the bridge reaches the socket only when the
  authenticated user is sudo-authorized and the bridge remains root. The
  service writes only its canonical config and `/var/lib/linuxio/indexer`
  state. See the [filesystem indexer guide](./indexer.md).
- **`linuxio-bridge-socket-user.service`** — `Type=oneshot`, `DynamicUser=yes`, `User=linuxio-bridge-socket`, `Before=linuxio-auth.socket`. Its only job is to make the `linuxio-bridge-socket` user/group exist *before* the auth socket is created with that group ownership.

### The `linuxio-bridge-socket` group trick

The webserver runs as a `DynamicUser` — its uid is **ephemeral** and changes across restarts. You can't grant a moving uid stable access to a root-owned socket. The fix: a stable **group**, `linuxio-bridge-socket`.

- `linuxio-bridge-socket-user.service` materializes the group (ordered before the auth socket).
- `linuxio-auth.socket` is created `root:linuxio-bridge-socket` mode `0660`.
- `linuxio-webserver.service` joins that group via `Group=linuxio-bridge-socket`.

So the (uid-less) webserver can `connect()` to the root-owned auth socket purely through stable group membership.

## Boot & Login Flow

### Boot — nothing is running yet

```
linuxio.target → pulls in the webserver, auth, and indexer sockets
  systemd binds  TCP :8090            (webserver.socket)
  systemd binds  /run/linuxio/auth.sock (auth.socket, root:linuxio-bridge-socket 0660)
  systemd binds  /run/linuxio/indexer.sock (linuxio-indexer.socket, root:root 0600)
  No linuxio-webserver / linuxio-auth / linuxio-bridge / linuxio-indexer process exists.
```

### First request — webserver starts via socket activation

```
Browser → :8090
  systemd starts linuxio-webserver.service, passing the listening fd
  via LISTEN_FDS / LISTEN_PID.
  The service weakly starts linuxio-indexer.service and systemd creates
  /run/linuxio/webserver, which pins the indexer's idle grace while the
  webserver is alive. The webserver still cannot open the root-only socket.
  Webserver adopts it through the shared socket-activation helper in
  backend/common/socketactivation, called from backend/webserver/cmd/root.go.
  No bind race, no extra privilege.
```

### Login — auth instance forks and supervises a bridge

```
Webserver  ── connect ──►  /run/linuxio/auth.sock        (allowed via group membership)
                                  │  Accept=yes
                                  ▼
                    linuxio-auth@<conn>.service  (root)   ← connected socket is its stdin
                       1. PAM authenticates once; root queries sudoers with
                          `sudo -n -l -U <user> -u root -- linuxio-bridge`
                       2. fork → drop to user uid/gid (or stay root if privileged)
                       3. dup2 the SAME client socket onto bridge FD 3
                       4. pass bootstrap (session id, uid/gid, flags) via a pipe → bridge stdin
                       5. wait for exec confirmation, then reply OK to the webserver
                       6. waitpid(bridge) — BLOCKS for the whole session  ◄── supervises
                                  │
                                  ▼
                          linuxio-bridge  (user)  ── yamux server on FD 3
```

The auth instance does **not** exit after forking — it holds the PAM session open and blocks in `waitpid` as the bridge's parent for the entire login ([linuxio-auth.c](../backend/auth/linuxio-auth.c)). Consequences:

- The bridge lives in the `linuxio-auth@.service` cgroup → `TasksMax`/`MemoryMax` apply per login.
- PAM session open/close brackets the bridge's lifetime exactly.
- One bridge per login is fully isolated from other logins (`MaxConnections=16`).

### After login — the connection becomes the yamux transport

The webserver keeps its end of the socket it dialed; it is now wired straight to the forked bridge (the auth daemon is out of the data path). The webserver wraps it as a yamux **client** and multiplexes WebSocket streams over it. From here on, see [Server Yamux Protocol](./server-yamux-protocol.md). When the bridge exits, the auth instance reaps it and closes the PAM session; the webserver's yamux session closes → the HTTP session is terminated.

## Privilege Boundaries (summary)

| Boundary | Mechanism | Guarantee |
|----------|-----------|-----------|
| Browser ↔ webserver | HTTP + session cookie, validated before WS upgrade | network-facing process is unprivileged + sandboxed |
| Webserver ↔ auth socket | unix socket, `root:linuxio-bridge-socket 0660` | only the webserver's group may request a login |
| Auth ↔ bridge | PAM authentication + root-side sudoers query + `fork`/privilege drop | bridge stays root only when sudoers permits the exact bridge command; otherwise it starts with the user's uid/gid |
| Webserver ↔ bridge | inherited socket fd + yamux | webserver never gains the bridge's privileges; just relays bytes |
| Bridge ↔ indexer socket | privileged route metadata + `root:root 0600` Unix socket | only a sudo-authorized root bridge reaches the machine-wide index |
| Webserver → bridge launch | embedded SHA-256 pin (`version.BridgeSHA256`), checked by `validateBridgeHash` | a tampered/substituted bridge binary won't be spawned |

## Build & Install

The `Makefile` produces six release artifacts:

| Target | Output | Notes |
|--------|--------|-------|
| `make build-cli` | `linuxio` | Go |
| `make build-backend` | `linuxio-webserver` | Go; embeds version + **bridge SHA-256** |
| `make build-bridge` | `linuxio-bridge` | Go |
| `make build-auth` | `linuxio-auth` | C; hardened flags (RELRO, PIE, FORTIFY, stack-clash, LTO), links `libpam` + `libsystemd` |
| `make build-docker-update` | `linuxio-docker-update` | Go; transient Docker update worker |
| `make build-indexer` | `linuxio-indexer` | Go + SQLite; FTS5 enabled |

`make build` / `make fastbuild` build all six; the internal `_build-binaries` step hashes the freshly built `linuxio-bridge` and passes it as `BRIDGE_SHA256` into the webserver build so the pin always matches. Install via `make localinstall` (`packaging/scripts/localinstall.sh`):

- binaries → `/usr/local/bin/`
- units → `/etc/systemd/system/linuxio*`
- tmpfiles → `/usr/lib/tmpfiles.d/linuxio.conf`
- PAM stack → `/etc/pam.d/linuxio`
- config → `/etc/linuxio/`

## Managing the Stack (`linuxio` CLI)

```
linuxio status              # list all linuxio* units with colored state
linuxio logs [web|bridge|auth|indexer] [N]   # tail journald, filtered per component
linuxio start | stop        # start/stop linuxio.target
linuxio restart [--full]    # restart control plane (bridge-socket-user + auth.socket + webserver);
                            #   --full restarts the whole linuxio.target
linuxio verbose enable|disable|status   # toggle debug logging for webserver and indexer
linuxio version [--self]    # versions of CLI + each installed component
```

`restart` (no args) cycles only the control-plane units — `linuxio-bridge-socket-user.service`, `linuxio-auth.socket`, `linuxio-webserver.service` — and deliberately leaves `linuxio-webserver.socket` alone, so the listening TCP fd on :8090 stays bound and browser connections aren't dropped (`restartTargets` in `backend/cli/main.go`). `--full` restarts the whole `linuxio.target`.

Journald, panic and `SIGQUIT` tracebacks, profiling builds, and core-dump limits
follow the [Production Diagnostic Data Policy](./production-diagnostics.md).

## File Locations

| Component | Path |
|-----------|------|
| Systemd units | `packaging/systemd/*.{target,socket,service}` |
| Managed TLS certificate | `/var/lib/linuxio/webserver/certificates/0-self-signed.{cert,key}` |
| Install script | `packaging/scripts/localinstall.sh` |
| CLI (commands) | `backend/cli/main.go` |
| Shared socket-activation adopt | `backend/common/socketactivation`, `backend/webserver/cmd/root.go` |
| Auth daemon (PAM, fork, supervise) | `backend/auth/linuxio-auth.c` |
| Bridge entry point | `backend/bridge/cmd/lifecycle.go`, `cmd/yamux.go` |
| Indexer daemon and API | `backend/indexer/`, `backend/indexer/api/` |
| Build | `Makefile` (`build-*`, `_build-binaries`) |

## See Also

- [Server Yamux Protocol](./server-yamux-protocol.md) — what flows over the webserver↔bridge connection (byte relay + mux framing).
- [Privilege Pattern](./privilege_pattern.md) — declaring privileged routes inside the bridge.
- [API Contract](./api-contract.md) — Go-owned API contract and generated frontend client.
- [Production Diagnostic Data Policy](./production-diagnostics.md) — credentials and safe correlation data across diagnostic sinks.
