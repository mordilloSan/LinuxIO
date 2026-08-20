# Docker Management Roadmap

> **Status: Planned.** LinuxIO already has the interaction backbone: concise
> cards and tables, route-backed selection, expanded details, logs, terminal,
> local Compose editing, and native container updates. This roadmap deepens
> those surfaces without replacing their visual model.

The goal is a Docker management experience that is both operationally complete
and recognizably LinuxIO: glanceable by default, detailed on selection, honest
about state, and visually restrained. Work is ordered by data and ownership
dependencies so that richer frontend surfaces do not get built on ambiguous or
duplicated backend state.

## Product decisions

- **Keep cards simple.** A collapsed card remains a fast status summary. It
  must not become a compressed inspect screen.
- **Put depth behind selection.** Monitoring history, configuration, mounts,
  networks, activity, and advanced actions belong in the existing focused
  detail layout.
- **Use one telemetry owner.** `go-monitoring` owns sampling, rate calculation,
  retention, and rollups. Docker inventory calls do not become a second metrics
  collector.
- **Keep files as the local Compose source of truth.** The editor works with
  the real project directory, Compose files, environment files, build context,
  configs, and secrets.
- **Make long operations observable.** Pulls, builds, deployments, updates,
  backups, scans, and cleanup expose progress and a bounded result history.
- **Treat security and remote hosts as architecture, not toggles.** They arrive
  only after environment scoping, credential ownership, capabilities, and
  permission boundaries are explicit.
- **Do not couple this work to table customization.** The current card/table
  toggle and row-selection behavior remain. Configurable table infrastructure
  is a separate TODO described below.

## Scope

This roadmap includes:

- correct live and historical container CPU, memory, network, and block-I/O
  monitoring;
- container inspection, lifecycle controls, processes, and file access;
- local and discovered Compose projects, multi-file workspaces, environment
  provenance, validation, deployment controls, and one-time templates;
- deeper image, volume, and network management;
- Docker activity, events, updates, schedules, and actionable health summaries;
- remote Docker environments as a later phase; and
- image vulnerability scanning, auditability, and environment-scoped RBAC as a
  later phase.

Explicitly excluded:

- Git repository connections, repository synchronization, webhooks, commit
  reconciliation, or any other GitOps workflow;
- Docker Swarm;
- Kubernetes;
- automatic reconciliation against any remote project source;
- per-process or GPU container telemetry;
- Prometheus/OpenTelemetry export and cost or billing analytics; and
- cross-environment fleet-wide mutations in the first multi-host release.

## Current baseline

| Area | What exists | Important gap |
|------|-------------|---------------|
| Containers | Cards and table, Compose grouping, start/stop/restart/remove, update status, logs, terminal, ports, networks, volumes, and current metrics | No typed inspect detail for health/config/resources/security; no pause/kill/redeploy/create flow; metrics have correctness and ownership problems |
| Monitoring | Docker list polling enriches every container; host monitoring has persistent history and reusable charts | Docker history is not exposed per container; current CPU is not an interval rate; current network/block values are cumulative; historical block I/O is absent |
| Compose | Project discovery, configured folders, multiple resolved config files, create/edit, sibling `.env`, validation, normalization, and lifecycle Tasks | No workspace tree, environment provenance, profile UI, resolved preview, contextual safety findings, or deployment history |
| Images | Repository/tag/ID/size/created/usage data and deletion/pruning | No pull/tag/run/build, inspect/config, layer history, registry workflow, export, or vulnerability results |
| Volumes | Create/delete/prune, usage, options, labels, and selected details | No content browser, export/import, clone, backup/restore, or attached-container safety workflow |
| Networks | Create/delete/prune, IPAM details, connected containers, and selected details | No connect/disconnect, richer create options, port-conflict view, or topology |
| Operations | Typed Tasks, progress, durable app/container updates, native update timer, and planned notifications/schedules | No unified Docker activity centre or Docker event history |
| Environments | One process-global Docker client created from the local environment | No environment identity, connection registry, remote agent, health model, or route scoping |
| Security | Application authentication and privileged route declarations | No Docker-resource RBAC, environment scoping, audit view, image scanner, or remote credential store |

### Monitoring audit

There are currently two different container telemetry paths:

1. `backend/bridge/handlers/docker/container.go` lists all containers and then
   requests one one-shot Docker stats response for each container, sequentially,
   on every list refresh. The frontend polls that Call every five seconds.
2. The sibling `go-monitoring` agent already collects and persists per-container
   CPU, memory, and network bandwidth. Its history plugin is enabled by default
   and feeds the same 1-hour, 12-hour, 24-hour, 7-day, and 30-day retention tiers
   used by the hardware charts.

LinuxIO currently consumes the agent's container history only to add aggregate
Docker memory to host memory history. It discards the per-container CPU,
network, identity, and metadata records.

The direct Docker path must not be treated as historical monitoring:

- the response has no capture timestamp or availability state;
- stats failures and decode failures become indistinguishable zero values;
- CPU uses cumulative container/system totals without a previous-sample delta;
- network and block I/O are cumulative byte counters, not rates; and
- stopped containers and failed samples both appear as zeros.

The agent path is the right foundation, but it also needs changes:

- add per-container block read/write throughput;
- preserve stable container ID and useful metadata through every rollup;
- aggregate only across samples in which that container is present;
- represent counter reset, recreation, stopped, missing, and unavailable states;
  and
- expose a capability/schema version so LinuxIO can handle older agents
  honestly.

## Metric contract

The UI and API must use explicit semantics rather than labels such as "disk"
or "network" without units.

| Metric | Current value | Historical value | Rollup behavior |
|--------|---------------|------------------|-----------------|
| CPU | Docker CLI interval CPU percentage; may exceed 100% for multi-core use | Percentage samples | Average plus maximum for the bucket |
| Memory | Working-set bytes, limit bytes, and percent when a limit exists | Working-set bytes and optional limit | Average plus maximum for the bucket |
| Network receive/send | Bytes per second, with optional lifetime totals kept separately | Receive/send bytes per second | Average plus maximum for the bucket |
| Block read/write | Bytes per second, with optional lifetime totals kept separately | Read/write bytes per second | Average plus maximum for the bucket |

The bridge contract uses named receive/send and read/write fields; it must not
leak the agent's positional bandwidth arrays to callers. CPU follows the Docker
CLI convention and may exceed 100% when a container consumes more than one
logical CPU. A separately named host-normalized percentage may be added if a
comparison view needs it.

"Block I/O" means bytes read from or written to block devices by the
container. Writable-layer size and filesystem capacity are different,
potentially expensive measurements and must not be presented as this metric.

Every sample needs:

- `captured_at_ms`;
- environment ID, with `local` as the compatibility default;
- full container ID as the instance identity;
- display name and optional Compose project/service identity;
- metric values and units;
- sample availability/status; and
- an explicit reset or lifecycle boundary when counters restart or a container
  is recreated.

History must never silently stitch two container IDs together. A future
logical service timeline may show predecessor and successor instances, but it
must render the recreation boundary.

## Target ownership

~~~text
Docker Engine
├── inventory / inspect / lifecycle ──> Docker bridge handlers
├── events ───────────────────────────> bounded event/activity projection
└── stats ────────────────────────────> go-monitoring collector
                                           │
                                           ├── current sample cache
                                           ├── metrics.db history
                                           └── bounded rollups/retention
                                                    │
                                                    v
LinuxIO bridge
├── typed Docker inventory and mutation contracts
├── typed current/history metric adapters
├── capability and environment scoping
└── Task/activity projection
         │
         v
Frontend
├── simple cards and current summaries
├── route-backed selected details
├── synchronized history charts and point-in-time views
└── operation progress, results, and actionable exceptions
~~~

| Owner | Responsibility |
|-------|----------------|
| `go-monitoring` | Collect Docker stats once, calculate deltas/rates, persist samples, roll up history, and enforce retention |
| Docker bridge handlers | Inventory, inspect, lifecycle mutations, resource operations, and merging current metric availability into Docker models |
| Monitoring bridge adapter | Decode/version agent payloads and expose typed, bounded history queries without leaking raw agent JSON |
| Task/durable-operation infrastructure | Progress, cancellation, reconnect semantics, result records, and log references for long mutations |
| Frontend TanStack Query owners | Server-state caching, bounded refetching, range queries, invalidation, and selected-resource state |
| Cards/detail components | Presentation only; cards summarize and focused details explain |

## Phase 1 — telemetry correctness and container history

This phase is the foundation for every later health, attention, and automation
feature.

### Backend

1. Extend `go-monitoring` container samples with block read/write counters and
   derive per-second rates using the same reset-safe delta ownership as network
   bandwidth. Align CPU calculation and validation with the documented Docker
   multi-core convention instead of clamping or rejecting values above 100%.
2. Preserve full container ID through raw rows and rollups. Carry name, image,
   and optional Compose identity as metadata, not as the primary key.
3. Fix rollups so a missing/stopped container does not contribute an artificial
   zero to the bucket denominator. Retain average and maximum values needed by
   chart tooltips.
4. Add sample status and schema/capability versioning. An old agent may provide
   CPU/memory/network only; LinuxIO must show block I/O as unavailable rather
   than zero.
5. Add a typed bulk history Call, provisionally
   `docker.get_container_metrics_history`, accepting optional container IDs,
   resolution, `from_ms`, `to_ms`, and a bounded limit. One response carries
   timestamped samples for the requested containers; cards must not issue one
   history request each.
6. Add a point-in-time Call, provisionally
   `docker.get_container_metrics_snapshot`, which returns the nearest captured
   sample at or before the requested timestamp plus its actual capture time and
   tolerance/staleness information.
7. Add optional `captured_at_ms` and metrics availability to the current
   `ContainerInfo` contract for a compatibility window.
8. Add and consume a typed bridge adapter for the agent's current container
   samples. Once the minimum compatible agent is present, remove sequential
   `ContainerStats` calls from `ListContainers`; inventory must remain available
   when monitoring is not.
9. Keep any direct-Docker compatibility fallback bounded and temporary. Never
   run two permanent samplers or merge their incompatible rates.

### Frontend

1. Keep the collapsed container card's present CPU/memory summary. Add only a
   subtle stale/unavailable state when the sample is not current.
2. Add a monitoring-history surface inside the existing selected-container
   layout, reusing `HistoryAreaChart`, synchronized crosshairs, theme tokens,
   and the existing 1h/12h/24h/7d/30d range configuration.
3. Present four clear groups: CPU, memory, network receive/send, and block
   read/write. Tooltips show exact timestamp, value, unit, and aggregation
   resolution.
4. Render stopped intervals, missing samples, counter resets, and recreation
   boundaries as gaps/events, not zero lines.
5. Add an all-container monitoring view after selected history is stable. At a
   chosen timestamp it shows aggregate use and ranks container cards by CPU,
   memory, network, or block I/O; selecting one opens its existing focused
   detail.
6. Keep selected-resource URL state, Escape/close behavior, logs, and responsive
   layout unchanged.

### Exit criteria

- [ ] CPU matches Docker's interval semantics within a documented tolerance.
- [ ] Network and block I/O are visibly rates or totals; the UI never confuses
      the two.
- [ ] CPU, memory, network, and block history can be queried for one or all
      containers with samples at a selected time within retention.
- [ ] A stopped, missing, reset, old-agent, and monitoring-unavailable sample
      has a distinct tested presentation.
- [ ] Recreated containers do not silently inherit another ID's history.
- [ ] Listing containers performs no unbounded or sequential per-container
      stats fan-out after migration.
- [ ] Backend tests cover delta math, counter reset, rollup identity, missing
      samples, bounds, and older-agent compatibility.
- [ ] Frontend tests cover range queries, selection, exact-time tooltips, gaps,
      and card-summary stability.

## Phase 2 — container inspection and lifecycle depth

### Backend

1. Add a lazy typed inspect Call rather than expanding every list payload. Map
   container state and health, platform, command/entrypoint, environment,
   restart policy, log driver, resource constraints, mounts, network aliases,
   labels, capabilities/security settings, and raw IDs needed by the UI.
2. Mask environment values by default. If reveal is supported, make it an
   explicit privileged operation and never put revealed values in logs,
   activity records, or query persistence.
3. Add pause, unpause, kill, and signal operations with state-aware validation.
4. Add redeploy/recreate as a Task that reuses the proven configuration-fidelity
   work from native container updates. Define image pull, name, mounts,
   networks, labels, restart policy, resources, health configuration, and
   rollback/unknown-outcome behavior before exposing the action.
5. Add container creation only after the same typed configuration model can be
   used by create, inspect, and recreate. Avoid separate form-only contracts.
6. Add bounded process inspection through Docker's top API.
7. Add container file operations through Docker archive APIs with explicit
   path validation, size limits, cancellation, and no host-path escape.

### Frontend

1. Expand the selected view with Overview, Monitoring, Configuration, Network,
   Mounts, Logs, Processes, and Files sections. Tabs are optional presentation;
   they must stay inside the existing focused-detail experience.
2. Keep common actions visible and place destructive/advanced actions behind a
   clear secondary menu with state-based disabling and confirmation.
3. Show health-check status and recent probe output separately from generic
   running state.
4. Provide a readable typed inspect view first; raw JSON is a secondary expert
   view, not the default.
5. Reuse the current terminal and log dialogs. Structured JSON/logfmt parsing
   and multiline grouping may enhance logs without replacing raw-text access.
6. For create/redeploy, preview what will change and state whether the image is
   pulled, the container is recreated, and downtime is expected.

### Exit criteria

- [ ] List payloads remain summary-sized; inspect is lazy and cached per
      container ID.
- [ ] Secret-bearing fields are masked and excluded from logs/activity.
- [ ] Every action validates current state and reports acknowledged, failed,
      canceled, or unknown outcomes honestly.
- [ ] Focused details remain usable on narrow screens and preserve route-backed
      selection and Escape/close behavior.
- [ ] Create and recreate share one tested configuration model.

## Phase 3 — local Compose projects and environment clarity

Git-backed sources are not part of this phase or this roadmap. The supported
source types are managed local projects and discovered/adopted local projects.

### Backend

1. Make project source explicit: managed project directory, discovered running
   project, or adopted existing directory. Adoption preserves original paths
   and files.
2. Model the ordered Compose file set, project directory, active profiles,
   project interpolation environment files, service `env_file` references,
   build contexts, configs, secrets, and included/extended files.
3. Add a bounded workspace API for listing, reading, creating, moving, and
   deleting files beneath the project root. Reject traversal and symlink
   escape, distinguish UTF-8 from binary files, and enforce file/total limits.
4. Save related workspace edits atomically where possible. Protect active
   Compose/environment filenames from accidental deletion while still allowing
   deliberate replacement through the editor.
5. Define and expose environment provenance:
   - shell/process environment used for Compose interpolation;
   - the project `.env` file;
   - ordered explicit Compose `--env-file` inputs;
   - service-level `env_file` files; and
   - explicit service `environment` values.
6. Preserve comments and ordering when editing env files. State clearly that a
   filesystem env file is plaintext on disk; masking in the UI is not
   encryption.
7. Extend validation from syntax/interpolation into context-aware findings:
   duplicate published ports, missing referenced files/dependencies, missing
   external networks/volumes, unsupported features for the installed Compose
   version, privileged mode, Docker socket mounts, writable host mounts,
   dangerous capabilities, host networking, unpinned/latest images, and absent
   restart policy. Errors may block; warnings and suggestions remain advisory.
8. Add a resolved-config preview and source locations for environment values.
   Offer automatic fixes only when the edit is deterministic and lossless.
9. Add deployment options for pull policy, build/no-cache, force recreate,
   remove orphans, profiles, and waiting for health/completion where supported.
   Any volume recreation option requires explicit data-loss confirmation.
10. Stream structured Compose progress and retain a bounded deployment summary
    containing project, options, start/finish, result, and safe changed-file
    metadata. Do not persist environment values or file contents in activity.
11. Support one-time local or non-Git remote template import into a new managed
    directory. Import severs source linkage: there is no synchronization or
    reconciliation after creation.

### Frontend

1. Keep stack cards concise: project name, state, running/total services,
   updates, and local source type. Selected details hold services, files,
   environment, monitoring, and activity.
2. Turn the current Compose/`.env` editor into a project workspace with a file
   tree and tabs while preserving raw YAML editing, overwrite protection,
   unsaved-state handling, and joint validation.
3. Add an environment view that separates interpolation variables from values
   passed into containers. Show effective value provenance and precedence;
   mask secret-marked values by default.
4. Show validation findings beside the relevant file/line and provide a
   project-wide findings list with error/warning/suggestion severity.
5. Add resolved configuration and deployment-preview views before destructive
   operations.
6. Stream deploy/build output in the operation surface, then offer logs without
   making closing the viewer stop an already-owned background operation.
7. Add an optional read-only service relationship graph derived from the
   resolved Compose model. It is a visualization, not a second visual editor or
   source of truth.

### Exit criteria

- [ ] Compose, `.env`, additional env files, `env_file`, include/extends, and
      build sidecars resolve from the real project context.
- [ ] The UI can explain where each effective environment value comes from and
      whether it is interpolation-only or injected into a service.
- [ ] Workspace operations cannot leave the project outside its root or expose
      secret values through validation/activity output.
- [ ] Profiles and advanced CLI options are capability-gated against the
      installed Compose version.
- [ ] Deployments have live progress and a bounded, secret-free result history.
- [ ] No Git source, synchronization, webhook, or reconciliation concept is
      introduced.

## Phase 4 — images, volumes, and networks

### Images

Backend work:

- add pull, tag, export, and run-from-image Tasks/Calls;
- expose lazy image inspect, architecture/OS, configuration, digests, usage,
  and layer history;
- add registry credential ownership without returning stored secrets;
- add BuildKit-backed builds with bounded context, progress, cancellation, and
  result metadata; and
- keep prune/delete protections aware of containers and active operations.

Frontend work:

- keep repository/tag/size/created as the card/table summary;
- add selected Configuration, Layers, Usage, Builds, and later Vulnerabilities
  sections;
- provide pull/tag/run/export flows with progress and clear registry identity;
  and
- add a build workspace only after the Compose workspace primitives are shared.

### Volumes

Backend work:

- inspect attached containers and enforce in-use removal protection;
- browse, upload, download, edit, rename, and delete volume content through a
  constrained helper/API rather than exposing host mountpoints directly;
- add streamed archive export/import, clone, backup, and restore Tasks;
- validate archive paths, ownership, permissions, quotas, and free space; and
- record backup source, target, checksum, size, time, and result without
  indexing file contents.

Frontend work:

- add selected Usage, Attachments, Files, Backups, and Configuration sections;
- reuse the existing file-browser interaction model with volume-specific safety
  messaging; and
- distinguish export, backup, restore, clone, prune, and destructive removal.

### Networks

Backend work:

- expose complete inspect/IPAM/driver options and aliases;
- support advanced create plus container connect/disconnect with conflict and
  state validation;
- build a published/exposed port projection with collision detection; and
- expose a topology model of networks and attached containers.

Frontend work:

- add selected Configuration, Containers, Ports, and Topology sections;
- provide a searchable port-conflict view; and
- make topology optional and interactive, never an always-on decorative graph.

### Exit criteria

- [ ] Every long resource operation uses progress/cancellation and has bounded
      output.
- [ ] In-use and data-loss risks are detected before image/volume/network
      deletion.
- [ ] File/archive operations are path-safe and tested with malicious inputs.
- [ ] Image layers, volume files, and network topology live behind selection;
      summary cards remain concise.

## Phase 5 — activity, events, updates, and automation

### Backend

1. Define one bounded Docker activity projection for user actions and system
   operations: operation ID, environment, actor, resource, action, lifecycle,
   timestamps, safe summary, result/error, and optional journal cursor.
2. Subscribe to Docker events with reconnect/cursor behavior and bounded
   retention. Events are observations, not proof that a LinuxIO mutation
   succeeded.
3. Project existing Task and durable-operation state into the activity model
   instead of creating a parallel execution system.
4. Keep the native systemd container-update service/timer as the update owner.
   Add execution summaries, per-container results, and notification hooks around
   it rather than introducing Watchtower.
5. Build cleanup and update schedules on the existing scheduled-execution
   architecture. Store definitions/results in their canonical owners; do not
   make the bridge an in-memory scheduler.
6. Connect actionable failures to the planned persistent notification system
   after activity identities and terminal outcomes are stable.

### Frontend

1. Add an Activity Centre for queued/running/completed/failed/canceled work,
   with live progress and bounded raw output where available.
2. Add a durable event/activity view with filters for environment, resource,
   action, result, and time.
3. Add a compact "Needs attention" dashboard section for unhealthy containers,
   stopped expected-running workloads, available updates, failed operations,
   vulnerable images, and later offline environments.
4. Add schedule list/detail forms showing enabled state, next/last run, timezone,
   retention, manual trigger, and history.
5. Keep notifications actionable: open the affected resource or operation, not
   merely a toast.

### Exit criteria

- [ ] An operation has one identity from start through progress, terminal
      result, activity, and notification.
- [ ] Docker events, LinuxIO actions, and scheduler executions are visually and
      semantically distinct.
- [ ] Reconnect does not duplicate operations or invent success.
- [ ] The attention summary derives from authoritative states and links to the
      exact corrective surface.

## Phase 6 — remote Docker environments

This is deliberately after the single-host data and operation models. Adding a
host selector before every resource and metric carries an environment identity
would create unsafe ambiguity.

### Backend

1. Add an environment registry with an immutable environment ID, display
   metadata, connection mode, capabilities, agent/API version, health, and last
   successful contact.
2. Support the local Docker socket first, authenticated Docker TLS endpoints
   where explicitly configured, and a LinuxIO agent/tunnel for hosts behind
   NAT. Never support unauthenticated remote Docker TCP.
3. Encrypt remote credentials at rest, redact them from contracts/logs, and
   separate connection testing from persistence.
4. Replace the process-global Docker client with environment-scoped client
   ownership, bounded pooling, timeouts, health checks, and deterministic
   teardown.
5. Add environment identity to every Docker resource, metric sample, activity,
   schedule, operation, and cache/query key. Existing requests without an
   environment continue to mean `local` during migration.
6. Negotiate capabilities so older agents and unsupported Docker/Compose
   versions disable features without breaking unrelated pages.
7. Keep first-release actions scoped to one selected environment. Cross-host
   bulk mutations remain out of scope.

### Frontend

1. Add a persistent environment selector with Online, Degraded, Connecting,
   Offline, and Incompatible states.
2. Scope every Docker route, selection URL, query key, mutation, activity entry,
   and notification to the chosen environment.
3. Add environment health cards showing Docker/agent versions, resource counts,
   monitoring freshness, storage pressure, and capability warnings.
4. Preserve the same resource cards and focused details after selection; remote
   state changes context, not the interaction model.
5. Prevent stale data from another environment flashing during a switch and
   require the target environment in destructive confirmation text.

### Exit criteria

- [ ] No resource, cache entry, metric, operation, or permission can collide
      across environments.
- [ ] Losing a remote environment produces honest offline/stale states without
      degrading local Docker management.
- [ ] Credentials never cross into frontend-readable storage or logs.
- [ ] All mutation and recovery tests assert environment ownership.

## Phase 7 — vulnerability scanning, audit, and RBAC

### Backend

1. Define a scanner capability/provider contract, beginning with one supported
   implementation such as Trivy. Scans are Tasks with cancellation, bounded
   logs, version metadata, and explicit unavailable/failed states.
2. Key results by immutable image digest and scanner database/version. Store
   severity, package, installed/fixed versions, references, and scan time with a
   bounded retention/cache policy.
3. Keep scanning advisory initially. Automated update/block policies require a
   separate explicit policy design after result quality is proven.
4. Define Docker permissions by action and environment: view, inspect, logs,
   terminal/exec, create/edit, lifecycle, delete/prune, deploy, schedule,
   environment administration, credential administration, vulnerability scan,
   and audit access.
5. Add built-in roles plus custom roles only after permission checks exist at
   backend route boundaries. Frontend hiding is never authorization.
6. Record a bounded audit event for security-relevant actions with actor,
   environment, action, resource, result, time, and request/session metadata
   appropriate to the application's privacy policy. Never store secrets or
   container environment contents.
7. Separate activity from audit: activity explains operational progress; audit
   answers who attempted what under which authority.

### Frontend

1. Add vulnerability severity summaries to image cards only when results are
   present; full package/fix details belong in selected image details.
2. Add scan-now, rescan, database age, scanner version, filtering, and fix
   availability without implying every finding is exploitable.
3. Add role and environment-scope administration with an effective-permissions
   preview.
4. Gate or disable actions from generated/effective permissions while still
   handling backend authorization rejection.
5. Add a separate audit viewer with resource/user/environment/time filters and
   no mutation controls.

### Exit criteria

- [ ] Scanner absence or failure does not block unrelated image management.
- [ ] Results are tied to immutable digests and scanner metadata.
- [ ] Every protected backend route has permission tests, including cross-user
      and cross-environment denial.
- [ ] Terminal/exec and secret reveal have distinct explicit permissions.
- [ ] Audit records are bounded, secret-free, and cannot be confused with task
      completion records.

## Deferred TODO — configurable table system

Do not implement table customization as part of the phases above. It requires a
shared `AppDataTable`/`AppVirtualDataTable` design rather than route-by-route
preferences.

The later table-system plan must cover:

- column visibility;
- column ordering and resizing;
- richer sortable-column definitions and multi-sort policy;
- persisted width/order/visibility/sort preferences;
- per-table schema/version migration when columns change;
- reset-to-default and density controls;
- responsive behavior and minimum usable columns;
- keyboard and screen-reader behavior;
- virtualized and non-virtualized parity; and
- adoption by Containers, Compose, Images, Volumes, Networks, Activity,
  Vulnerabilities, and Environments without each owning a custom state format.

Until that design lands, new feature phases may add essential fixed columns and
ordinary sorting through existing primitives, but must not invent local column
configuration stores or one-off resize/reorder implementations.

## Cross-cutting implementation rules

- Go-owned API changes update `backend/bridge/apischema`, run `make generate`,
  and validate the combined backend/frontend result with `make test`.
- New read Calls declare retry safety explicitly; mutations do not retry merely
  because the connection disappeared.
- Long-running work uses the existing Task model. It becomes durable only when
  it has an external owner and a proven recovery contract.
- Query keys include resource ID and, once introduced, environment ID. Selected
  route state remains in TanStack Router; transient dialog/editor state remains
  local.
- Secrets are never included in metrics, history, validation messages,
  activity, audit summaries, URLs, or generated test fixtures.
- Payloads, output, history, archives, scans, and event retention are bounded.
- Capability absence disables only the dependent feature.
- Every phase adds unit tests for semantics and focused browser coverage for
  navigation, streaming, responsive selection, and destructive confirmations.
- Measure list latency, metrics collection load, history payload size, chart
  render cost, and large-project/resource behavior before adding concurrency or
  caching complexity.

## Delivery order

| Order | Deliverable | Depends on |
|-------|-------------|------------|
| 1 | Telemetry correctness and selected-container history | Existing `go-monitoring` history and chart primitives |
| 2 | All-container point-in-time monitoring | Typed bulk history and stable identity |
| 3 | Container inspect and lifecycle depth | Correct inventory/current-state ownership |
| 4 | Local Compose workspace and env provenance | Existing editor plus safe workspace API |
| 5 | Images, volumes, and networks | Shared focused-detail and Task patterns |
| 6 | Activity, events, updates, and schedules | Stable operation identities and result semantics |
| 7 | Remote environments | Environment-scoped contracts, metrics, operations, and credentials |
| 8 | Vulnerability scanning, audit, and RBAC | Stable environment/resource identities and permission model |
| TODO | Configurable table system | Separate shared-table design and migration plan |

Each numbered deliverable should be independently releasable. Later phases may
be designed in parallel, but they must not force speculative abstractions into
earlier single-host features.

## Benchmark-informed reference points

The roadmap uses Arcane and Dockhand as feature references, not visual templates
or parity checklists. Their strongest relevant patterns are selected-resource
detail, project workspaces, environment clarity, operation visibility, volume
file access, network topology, image inspection, vulnerability results, and
remote-environment health. LinuxIO's differentiator remains its existing
card-first visual language and cohesive host-management context.

- [Arcane containers](https://getarcane.app/docs/features/containers)
- [Arcane projects](https://getarcane.app/docs/features/projects)
- [Arcane environments](https://getarcane.app/docs/features/environments)
- [Dockhand containers](https://finsys-dockhand.mintlify.app/features/containers)
- [Dockhand stacks](https://finsys-dockhand.mintlify.app/features/stacks)
- [Dockhand manual](https://dockhand.pro/manual/)
