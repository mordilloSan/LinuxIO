# LinuxIO Docs

Start with [API Contract](./api-contract.md) for bridge/frontend API work.

Docs in this folder describe implemented behavior. Plans and roadmaps that have
not fully shipped live in [TODO](./TODO); a doc moves up into this folder once
its plan lands.

The Code column names the directories and files each doc covers, so the doc for
a piece of code can be found from its path. `scripts/docs-for.sh <path>...`
lists the docs covering a path, and `.claude/hooks/docs-pointer.sh` tells a
Claude Code agent the first time it opens a covered file. A path that contains
the code of two or more other docs, such as `backend/bridge/handlers`, is
general: its doc sets rules for that code without describing it, so
`scripts/docs-for.sh --changed` reports changes covered only by general docs
as needing a look, like changes no doc covers. `make check-docs`
fails when a doc in this folder has no row, when a row names no code (`-`
marks a doc that covers none), or when a named path no longer exists.

## Development

| Doc | Code | Use |
|-----|------|-----|
| [Development targets and overrides](./development.md) | `Makefile`, `mk`, `scripts`, `packaging/vm` | Repository setup, checks, builds, the test VM, and configurable Make inputs. |

## API And Bridge

| Doc | Code | Use |
|-----|------|-----|
| [API Contract](./api-contract.md) | `backend/bridge/apischema`, `backend/bridge/internal/apigen`, `frontend/src/api`, `frontend/src/api/generated`, `frontend/src/api/operation-query-invalidations.ts` | Canonical implemented contract for Go-owned routes, generated frontend types, JSON request envelopes, Calls, Mutations and jobs, Channels, Tasks, and adding endpoints. |
| [Bridge Configuration Storage](./config-storage.md) | `backend/bridge/internal/userconfig`, `backend/bridge/handlers/config` | Per-user core/UI files, home and `/var/lib/linuxio` fallback, memory-only degradation, corrupt-document recovery, ownership, and logging. |
| [Configuration and Storage Layout](./configuration-storage-layout.md) | `backend/bridge/internal/userconfig`, `backend/indexer` | Indexer paths and bridge per-user configuration files, with ownership and lifecycle. |
| [Jobs](./jobs.md) | `backend/common/jobs`, `backend/bridge/internal/jobshost`, `backend/bridge/internal/jobsclient`, `backend/bridge/cmd/jobs_host.go`, `backend/bridge/cmd/jobs_relay.go`, `backend/cli/scheduled_run.go`, `frontend/src/routes/_authenticated/history`, `frontend/src/components/tasks`, `frontend/src/routes/_authenticated/-components/app-update/JobsUpdateBanner.tsx`, `frontend/src/routes/_authenticated/-components/app-update/useJobHostStatus.ts`, `frontend/src/routes/_authenticated/-components/app-update/useRefreshHistoryAfterUpdate.ts` | Mutations that outlive the login: the rules a route that runs as a job follows, the bridge-to-host relay, the per-uid job host and who starts it, the store and its records, the job lifecycle with cancel, shutdown, crash, reboot and resume, what each event does, the update wait, the records of timer runs, the history page, saved output and update banner, logs, how it is tested, and what is deliberately not built. |
| [Handler Patterns](./bridge_handler_patterns.md) | `backend/bridge/handlers` | Handler package style: `handlers.go` layout, context propagation, logging, naming, validation. |
| [Privilege Pattern](./privilege_pattern.md) | `backend/bridge/handlers`, `backend/bridge/apischema` | How to decide and declare privileged routes. |
| [Capabilities](./capabilities.md) | `backend/bridge/handlers/capabilities`, `frontend/src/api/capabilities.ts` | Detecting optional host tooling, gating features/routes on it, and the UI install flow. |
| [AI Assistant](./AI-assistant.md) | `backend/bridge/handlers/assistant`, `backend/bridge/internal/acpagents`, `frontend/src/api/acp`, `frontend/src/routes/_authenticated/assistant` | The ACP agent relay behind the Assistant page: launch table, login flow, the plugin manager, and what LinuxIO deliberately does not store. |
| [Process & Systemd Architecture](./process-systemd-architecture.md) | `packaging/systemd`, `backend/auth`, `backend/webserver`, `backend/bridge/cmd`, `backend/common/socketactivation` | LinuxIO processes and helpers, systemd socket activation, `linuxio.target`, the job hosts and who starts them, and privilege separation. |
| [Filesystem Indexer](./indexer.md) | `backend/indexer`, `backend/bridge/handlers/indexer` | Architecture, operation, bridge and daemon APIs, configuration, recovery, and administration. |
| [Monitoring Daemon](./monitoring.md) | `backend/monitoring`, `backend/bridge/handlers/monitoring` | Trust boundary and sockets, installed files, sampling semantics, HTTP API and plugin allowlists, configuration, capability, and troubleshooting. |
| [Third-Party Notices](./THIRD_PARTY_NOTICES.md) | - | Attribution and license notices for incorporated indexer and monitoring code and dependencies. |
| [Production Diagnostic Data Policy](./production-diagnostics.md) | `backend/common/logging`, `backend/common/debugserver` | Credentials and permitted correlation identifiers in journald, tracebacks, pprof, support artifacts, and core-dump controls. |
| [Server Yamux Protocol](./server-yamux-protocol.md) | `backend/webserver/ws`, `backend/webserver/bridge`, `backend/common/ipc`, `frontend/src/api/StreamMultiplexer.ts` | Lower-level WebSocket/yamux byte relay and mux framing. |

## Host Integration

| Doc | Code | Use |
|-----|------|-----|
| [Alerts](./alerts.md) | `backend/common/alerts`, `backend/bridge/handlers/alerts`, `backend/cli/alert.go`, `frontend/src/routes/_authenticated/settings/-components/NotificationSettingsSection.tsx`, `frontend/src/routes/_authenticated/-components/footer/NotificationsBell.tsx`, `frontend/src/routes/_authenticated/-components/useAwayNotice.ts` | The file-backed alert store, who raises alerts at which level and which setting governs each, the notification settings (what each kind reports, the grid of level by channel, each schedule's own choice) and their delivery by email and webhook, the `linuxio alert` CLI, bridge routes, the navbar bell and the notice at sign-in, and what is deliberately not built. |
| [Automatic Updates](./automatic-updates.md) | `backend/bridge/handlers/packages/internal/autoupdate` | Per-distribution automatic-update providers, the systemd timers LinuxIO manages, and how the backend selects a provider from `/etc/os-release`. |
| [Btrfs](./btrfs.md) | `backend/common/btrfs`, `backend/btrfs-timer`, `backend/bridge/handlers/storage`, `frontend/src/routes/_authenticated/storage` | The Storage → Subvolumes tab and pool details: top-level access, snapshot layout, rollback steps and leftovers, the hourly snapshot timer and retention, pre-update snapshots, device operations, replication over SSH, quotas and compression, files written on the host, and what is refused. |
| [Rsync Backups](./rsync-backups.md) | `backend/bridge/handlers/shares`, `frontend/src/routes/_authenticated/storage/rsync.tsx` | Read-only rsync module (plain or over SSH) that TerraMaster TOS pulls from, and what the backup does and does not cover. |
| [Scheduled Scripts](./TODO/scheduled-execution.md) | `backend/bridge/handlers/schedules`, `backend/common/scheduledrun` | Script scheduling through systemd, execution accounts, task controls, and retained journal logs. |
| [Storage Disks](./storage-disks.md) | `backend/bridge/handlers/storage`, `frontend/src/routes/_authenticated/storage` | The Storage → Disks overview: how drives, pools, arrays, partitions and mounts are drawn, which partitions count as unused, the block-device inventory, and live disk activity. |
| [VM Bridge Networking](./vm-bridge-networking.md) | `backend/bridge/handlers/virt`, `backend/bridge/handlers/network`, `packaging/libvirt` | Attaching VMs to host bridges, spare-NIC and host-IP bridge setup through Netplan/NetworkManager, the Docker libvirt hook, and what is deliberately not automated. |
| [VM Templates](./vm-templates.md) | `backend/bridge/handlers/virt`, `frontend/src/routes/_authenticated/vm` | The VMs → Templates manager: preset images, on-disk template layout, and version metadata. |

## Frontend

| Doc | Code | Use |
|-----|------|-----|
| [Frontend Task State](./frontend-task-state-unification.md) | `frontend/src/tasks`, `frontend/src/api/tasks.ts`, `frontend/src/api/task-state.ts` | Task cache ownership, subscriptions, recovery, lifecycle cleanup, and validation. |
| [Dialog Styling](./dialog-styling.md) | `frontend/src/components/ui/AppDialog.tsx`, `frontend/src/components/ui/app-dialog.css`, `frontend/src/components/dialog` | Shared dialog typography, surfaces, form spacing, actions and visual checks. |
| [TanStack Router](./tanstack-router.md) | `frontend/src/routes`, `frontend/src/router` | Canonical routing guide: file conventions, where loaders and guards go, search validation, child-route tabs, error/pending defaults, and adding a route. |
| [Table Row Gestures](./table-row-gestures.md) | `frontend/src/components/tables` | The one row-interaction contract for every data table: click, long press, double click, Escape; which props opt a table in, and why column defs must be stable. |
| [Docker Icons](./docker-icons.md) | `backend/bridge/handlers/docker/icons.go`, `frontend/src/components/docker/DockerIcon.tsx` | Docker icon resolution and labels. |
