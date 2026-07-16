# Execution Architecture Migration Plan

Status: reviewed against baseline commit `0cbe62cc6542` on 2026-07-16. No
runtime migration stage has started.

## Decision

LinuxIO should stop treating every mutation as a job, but it should not delete
the current job implementation before its consumers have replacements.

The target is:

- Keep the existing WebSocket and yamux path for queries, bounded mutations,
  terminals, live streams, and durable-job control events.
- Add a direct `Action` contract for bounded mutations. An action is
  synchronous at the API boundary: the response means the operation completed
  or was explicitly accepted by its native owner. Its implementation may use
  asynchronous D-Bus calls, file APIs, or spawned-process promises internally.
- Put raw file upload and download bytes on authenticated HTTP proxied over the
  session's existing yamux connection.
- Rewrite jobs as a small durable facility only for work that LinuxIO must own
  after the initiating stream or bridge is gone.
- Prefer native ownership. A systemd, PackageKit, Docker, libvirt, or device
  operation that already has a recoverable native identity is not automatically
  a LinuxIO job.
- Remove the legacy job framework only after generated coverage proves it has
  no routes or data-stream consumers left.

React Query mutations are **not irrelevant**. They remain the correct frontend
primitive for direct actions: pending state, errors, callbacks, optimistic UI,
and cache invalidation. What becomes obsolete is using one React Query mutation
and several attachment mechanisms to model the complete lifetime of a backend
job.

The transport verdict is also settled: WebSocket plus yamux is a good fit for
LinuxIO's control plane. The production proof showed it is substantially faster
than browser HTTP for small RPCs. Its remaining tradeoff is that every stream,
including HTTP file traffic, shares one underlying TCP connection, so transfer
contention must be measured. Protocol-field cleanup is optional optimization,
not an architectural prerequisite.

## Why the previous draft was not safe to execute

The first plan had the right destination, but missed several properties of the
current code:

- The current job policy supplies serialization, queueing, rate limits, and
  timeouts. Removing it changes mutation behavior even when no caller uses job
  progress.
- Browser stream reset currently closes a yamux stream but does not cancel the
  handler context. Direct actions need a real stream-scoped context first.
- One goroutine already owns `yamux.Accept()`. A bridge HTTP server cannot add a
  second accept loop; accepted streams need a trusted, explicit demultiplexer.
- Detecting HTTP by inspecting browser-controlled first bytes would let a
  WebSocket client bypass the authenticated HTTP route. The webserver must write
  the stream kind.
- `filebrowser.archive` combines expensive artifact creation with byte
  delivery. HTTP replaces delivery, while durable execution replaces archive
  creation. Removing `jobs.data` before both exist causes two rewrites.
- Current job snapshots contain decoded requests. Durable storage must never
  persist or return raw requests because several contracts contain passwords
  and credentials.
- Several routes mix bounded and unbounded work. Classification must use the
  worst case, or the route must be split.
- Some nominal queries have host side effects, such as creating a Docker
  network or refreshing package metadata. The inventory must review behavior,
  not just the declared route mode.
- The previous transient-unit plan assumes bridge-local records can be rebuilt.
  That does not preserve terminal results after the bridge and transient unit
  are gone, and it does not resolve unprivileged job submission.

## Audited starting point

The generated route metadata currently contains 228 routes:

| Current mode | Count |
| --- | ---: |
| Query | 94 |
| Job | 128 |
| Duplex | 6 |

The 128 job entries are not 128 durable mutations. They include job control,
connection-bound log streams, file transfer, semantic reads, bounded actions,
mixed routes, and genuinely long operations.

Other relevant facts:

- Current IDs are bridge-local sequential values such as `job-1`.
- A current job is detached from its request with `context.Background()`, but
  bridge shutdown explicitly cancels the session's jobs. It is not durable.
- Job start waits up to 25 ms and returns a snapshot; later state is recovered
  through attach, events, polling, and frontend reconciliation.
- Production `useJobAction` and `useJobStreamAction` consumers are recorded by
  the Stage 0 manifest and its validation rather than by volatile prose counts.
  Their UI behavior must be migrated, not deleted blindly.
- The current download frontend collects chunks into memory and creates a Blob.
  The discarded HTTP benchmark also created a Blob, so it proved transport
  throughput but did not prove bounded browser memory.
- The HTTP/yamux proof and benchmark page are no longer in the repository.

The production measurements supplied during the proof were:

| Operation | Current WebSocket relay p50 | HTTP over yamux p50 | Conclusion |
| --- | ---: | ---: | --- |
| Uptime query | 3.4 ms | 16.5 ms | Keep control RPC on WebSocket/yamux. |
| Empty config mutation | 11.0 ms | 25.6 ms | Do not move normal actions to HTTP. |
| 273,021,385-byte download | 8663.9 ms | 8653.9 ms | HTTP data-plane throughput is viable. |

Three file samples do not establish tail performance. Terminal contention,
memory, cancellation latency, and native browser-to-disk behavior remain to be
measured.

## Execution contracts

| Contract | Meaning | Lifetime identity | Transport | Completion |
| --- | --- | --- | --- | --- |
| `Query` | Read-only request, bounded by its request context. | yamux stream/correlation ID | WebSocket relay over yamux | One result/error. |
| `Action` | Bounded mutation or native-manager handoff. | yamux stream/correlation ID | WebSocket relay over yamux | One result/error or explicit accepted result. |
| `Duplex` | Interactive or live connection-bound stream. | yamux stream | WebSocket relay over yamux | Stream close. |
| `FileTransfer` | Raw request/response body. Not an RPC route. | HTTP request ID; optional transfer token only if resume requires it | Browser HTTP to webserver, private HTTP/1.1 over yamux | HTTP response/closure. |
| `DurableJob` | LinuxIO-owned work that must outlive the initiating stream or bridge. | Stable opaque durable ID | WebSocket/yamux control; native worker execution | Persisted terminal state. |

### Identity rule

Use one identity for one lifetime:

- A connection-bound operation uses its transport stream and safe correlation
  ID. It does not get a second job ID.
- A durable job needs a separate random ID because yamux stream IDs are local to
  one connection and disappear with that connection.
- A resumable HTTP transfer may need an expiring transfer token, but that token
  is not a job and must not grow job-like control or event machinery.

### Action completion and disconnection

Each action declares two orthogonal policies in the route manifest.

Backend ownership:

- `request_owned`: the default. EOF/reset cancels the operation context and no
  work is intentionally detached from the bridge.
- `native_handoff`: the handler validates the request and obtains acceptance
  from systemd or another recoverable native owner before reporting success. No
  arbitrary bridge goroutine or child process is detached after the response.

Client expectation:

- `normal`: transport loss before a result is an error/ambiguous commit.
- `expected_loss`: reboot, power-off, logout, and selected network changes may
  intentionally destroy the connection after native acceptance. The frontend
  verifies state after reconnect where possible.

An expected-loss action must complete its native handoff before the destructive
step. If no native owner can do that, the operation is a DurableJob rather than
a detached Action.

Cancellation is cooperative and best-effort. D-Bus calls, file loops, and child
processes must use the caller context. Spawned commands that create descendants
need process-group or native-manager cancellation. A disconnect after commit but
before the response is inherently ambiguous; mutations are never retried
transparently. Callers refresh state, or use an explicit idempotency key where
the operation supports one.

### Durability levels

Every durable operation must declare the level it actually provides:

- `D1`: survives stream, WebSocket, and per-login bridge death.
- `D2`: D1 plus webserver/job-control restart reconciliation and retained
  terminal result.
- `D3`: D2 plus host-reboot resume or deterministic restart from a checkpoint.

A transient systemd service provides execution ownership for D1. It does not by
itself provide D2 records or D3 resume. Persistent timers and operation-specific
checkpoints are required where D3 is promised.

## Target topology

### Control plane

```text
Browser
  -> authenticated WebSocket
  -> webserver logical-stream relay
  -> one yamux stream per operation
  -> per-login bridge router
       -> Query
       -> Action
       -> Duplex
       -> DurableJob control/events
```

### File data plane

```text
Browser HTTPS/H2
  -> authenticated fixed webserver file route
  -> session-specific reverse proxy / RoundTripper
  -> one private HTTP/1.1 connection on one yamux stream
  -> channel-backed bridge net.Listener
  -> private bridge http.Server
  -> os.Root/file APIs
```

The bridge HTTP server never listens on TCP or a Unix socket. The webserver
remains the network authentication and cross-origin boundary.

### Yamux stream demultiplexing

The bridge keeps exactly one yamux accept loop. The webserver writes a trusted
stream-kind byte before any browser-controlled data:

```text
0x01 = relay/RPC stream
0x02 = private HTTP connection
```

The bridge accept loop reads that byte and either invokes the existing relay
handler or enqueues the connection into the HTTP listener. Unknown kinds close
immediately. It classifies accepted streams in a small bounded worker set, reads
the kind under a short deadline, then clears the deadline before dispatch. A
peer that opens a stream and sends no kind can therefore consume only one
bounded classifier slot and cannot freeze the sole accept loop. All yamux open
sites, including WebSocket relay, capability fetch, and HTTP transport, must use
one helper that writes the kind first.

Do not sniff `GET`, `PUT`, or relay opcodes. Do not run competing accept loops.
This is a coordinated protocol version, not a rolling-compatible change. Stage
5 must update install, update, and rollback flows to stop the webserver and
therefore close every old yamux/per-login bridge before either binary is
replaced. All sessions reconnect on the new version. Both version-skew
directions must fail closed in tests; there is no sniffing fallback.

### Durable execution

```text
Browser control request
  -> bridge typed DurableJob route
  -> authenticated job-control boundary
  -> persistent allowlisted record
  -> systemd transient service or persistent timer
  -> typed LinuxIO worker/native command
       -> journald detailed logs
       -> bounded progress/result record

Bridge reconnect
  -> durable_jobs.sync/get + one durable_jobs.events stream
  -> reconcile record with native executor state
```

The job-control boundary must work for both privileged and unprivileged login
bridges. Direct system-bus D-Bus is preferred if authorization and ownership can
be proven safely. Otherwise use one minimal systemd socket-activated broker that
validates peer credentials and accepts only typed, allowlisted LinuxIO
operations. It must never accept arbitrary argv or a raw serialized RPC request.

## Non-negotiable invariants

1. Queries and actions do not allocate job IDs or job records.
2. A direct action waits for completion or explicit native acceptance.
3. No raw request payload is written to logs, snapshots, events, or durable
   storage.
4. Durable operation specs are typed, allowlisted, size-bounded, and redacted.
   Passwords, cloud-init secrets, share credentials, tokens, and environment
   secrets remain transient. When a worker genuinely needs a secret, use a
   sealed/0600 one-shot credential channel such as systemd credentials or a
   sealed memfd, never argv, ordinary environment, unit description, state JSON,
   or journald.
5. Results, errors, progress, and event history are size-bounded.
6. Mutation retry is disabled in React Query, core transport, and non-idempotent
   D-Bus use. Retry is opt-in only with a documented idempotency contract.
7. Action admission control is request-scoped and ID-free. Preserve required
   serialization, concurrency, queue/reject, rate, and timeout behavior without
   recreating a job registry.
8. Every mutation declares an authoritative audit writer, commit point, and
   outcome phase. Completed request-owned work has an Info-level completion
   record; native handoffs and expected-loss actions record acceptance before
   destructive effects; durable work records both accepted and terminal state
   transitions. Records contain a safe correlation ID, route/operation, actor
   username and UID, outcome, duration where known, and allowlisted operation
   fields. Queries remain Debug-level unless they have an explicit audit
   requirement. The webserver may access-log but is never the authority for
   bridge business completion.
9. Raw session IDs are not logged. Generic middleware never logs decoded
   requests. Domain logs add targets such as unit, path, or resource only after
   redaction rules are defined.
10. The generated frontend API is mode-safe at compile time. Invalid hooks are
    not present on an endpoint.
11. An identity change/logout cancels in-flight queries and actions, tears down
    streams, clears the durable-job store, and clears or rotates the entire
    QueryClient, including QueryCache and MutationCache. Mutation variables and
    errors can contain credentials just as query results can.
12. HTTP file routes are fixed and authenticated; browser-controlled WebSocket
    bytes cannot reach the private HTTP server.
13. Long file data is never buffered as a complete Blob, request, proxy body, or
    bridge byte slice in the normal path.
14. A route has stable execution semantics. It does not silently become a job
    based on guessed input size.
15. Legacy job support remains usable during migration and is removed only
    after machine-checked zero-consumer gates pass.

## Classification rules

Classify actual worst-case behavior, not route names or typical timing:

1. If it is a semantic read, use `Query`, even when computation is expensive.
   It remains request-scoped and cancellable. Transparent bounded caches are
   allowed; changing host configuration is not.
2. If it transfers browser file bytes, use `FileTransfer` for the bytes.
3. If it is interactive or continuously follows output, use `Duplex`.
4. If a native manager accepts and owns continuation, use `Action` with an
   accepted result and query the native state later. Do not create a fake
   LinuxIO job merely to mirror systemd's or another manager's job.
5. If LinuxIO must own execution after the bridge is gone, use `DurableJob` and
   declare D1/D2/D3.
6. Otherwise, use `Action`.

For a Query or Action implementation, prefer native Linux interfaces in this
order:

1. Typed direct D-Bus to the owning service, with mutation retries disabled.
2. Go file/syscall APIs with descriptor-relative resolution and atomic changes.
3. `exec.CommandContext`-style argument arrays when no stable native API exists.
   Avoid a shell, propagate cancellation, wait for exit, and bound output.

A spawned process remains a request-scoped Action only when the request owns and
waits for it. Hand it to systemd/a durable worker if it must continue after the
request or bridge.

Split a mixed route before migration. Known audit targets include:

- `filebrowser.resource_patch`: only a strict same-filesystem atomic rename with
  no recursive prewalk or copy/delete fallback is bounded. Cross-filesystem
  move and copy can recurse and transfer unbounded data.
- `filebrowser.archive`: artifact creation and artifact delivery are different
  contracts.
- Recursive/batch delete and chmod: classify by their unbounded worst case or
  introduce separate strictly bounded operations.
- Account delete/home migration: `userdel -r` and home moves are not small CRUD.
- Network apply, reboot, power-off, and logoff: define acceptance and expected
  disconnect explicitly.
- Docker update checks and other semantic reads currently declared as jobs.
- Log-follow routes currently declared as jobs but operationally streams.

The query audit must also find hidden side effects. Examples to review include
Docker network creation during client acquisition, icon cache writes, and
package metadata refresh. Move host mutations to startup or an explicit action;
document harmless cache writes separately.

## Route manifest

Stage 0 creates a checked-in, machine-validated manifest covering every
generated RPC route and every private HTTP file endpoint. Each entry contains:

```text
route
source declaration
current mode and policy
target contract
semantic read/mutation
worst-case duration/work
native owner, if any
progress/result need
durability level, if any
disconnect policy
client loss expectation
timeout
concurrency/admission scope
idempotency/retry rule
privilege and execution UID/GID
audit event, authoritative writer, commit point, outcome phase, and allowlisted fields
frontend consumers
cache invalidations
accepted-operation convergence query/event and success condition
migration stage
```

The manifest cannot use `unknown` as an accepted final classification. If one
route contains multiple contracts, its entry names the split routes required
before migration. A generator/test fails when a route is missing or stale.

## Backend and generated API target

During coexistence, the Go declarations distinguish:

```text
Query
Action
LegacyJob / LegacyRunner
DurableJob
Duplex
```

`Action` reuses the existing typed `HandlerFunc` and `Events.Result/Error`
adapter; it does not introduce a parallel handler style. Progress/data emission
is invalid for an action.

Generated TypeScript surfaces are mode-specific:

```text
QueryEndpoint
  execute(request, { signal })  // provider-independent typed call
  useQuery / useQueries / useFetcher / useCache
  useCommand         // React Query-backed imperative read

ActionEndpoint
  useAction          // one React Query mutation and one RPC result

DurableJobEndpoint<TRequest, TResult>
  start / useJobStart
  returns JobHandle immediately; TResult belongs to terminal job state

LegacyJobEndpoint               // coexistence only, removed in Stage 15
  useJobAction / useJobStreamAction and existing recovery surface

DuplexEndpoint
  typed stream opener

HTTP file helper
  typed URL/request construction; no React Query body transport
```

Wrong hooks must fail TypeScript compilation instead of throwing at runtime.
Direct actions return their business result. Durable start results and eventual
business results are separate generated types; do not hide a `JobSnapshot`
behind runtime unwrapping.

### React Query ownership

- A completed direct action invalidates immediately. A native-handoff action
  reports `accepted`, then follows its manifest-declared native query/event and
  invalidates/refetches until the accepted target state converges or times out.
- A durable-job start mutation stays pending only until `JobHandle` is returned.
- One durable-jobs store obtains an atomic snapshot plus persisted event cursor
  from `durable_jobs.sync`, consumes `durable_jobs.events(after_cursor)`, and
  reconciles snapshots by ID, per-job revision, and a persisted global event
  sequence. The exposed cursor is opaque and replay is authorization-filtered.
  Cursor retention gaps force another atomic sync.
- Durable terminal invalidation happens once, keyed by the typed job operation.
- File bodies do not enter React Query.
- Query functions propagate React Query's `AbortSignal` into the bridge request.
- Authentication/bootstrap code can use provider-independent `execute` before a
  QueryClient exists; this path propagates cancellation and typed errors too.
- Actions expose an explicit caller-owned signal/cancel API because TanStack
  mutations do not automatically provide mutation cancellation.
- `retry` and `retryDelay` are absent from action configuration escape hatches;
  core mutation transport retry is forced off.

## File HTTP contract

### Proxy boundary

The webserver proxy must:

- Reuse the existing session-cookie and cross-origin protection middleware.
- Select only the authenticated session's yamux instance.
- Expose only fixed file methods and paths.
- Strip browser `Cookie`, `Authorization`, forwarding, proxy, and internal trust
  headers before proxying; inject only server-owned identity metadata if needed.
- Avoid logging raw filesystem query parameters.
- Record whether file targets use an encoded path or an opaque transfer ticket.
  The simple encoded-path contract accepts that target paths can appear in
  browser download metadata and TLS-terminating proxy request logs even though
  LinuxIO redacts them. Use a short-lived opaque ticket only if path
  confidentiality is a product requirement; do not replace native GET/Range
  downloads with POST merely to hide the path.
- Disable compression and HTTP keep-alive on the private hop so one request owns
  one yamux stream.
- Acquire shared global, per-UID, and per-session upload/download admission
  before opening a yamux stream or temporary file. Limits cover both directions
  so multiple sessions cannot exhaust FDs, memory, streams, or disk bandwidth.
- Close the request stream on browser cancellation and close transports when the
  yamux session ends.
- Treat an active transfer as session activity. Explicit logout, revocation, or
  absolute expiry immediately cancels its request; upload cancellation removes
  the partial file. An idle session does not expire solely because a valid
  transfer is actively making progress.

The private bridge server must have bounded headers/backlog, header timeouts,
graceful shutdown, an explicit idle-transfer timeout, and file handlers only.
Avoid forced flush on every proxy copy for fixed-length downloads.

### Regular-file download

- Implement `GET` and `HEAD`, Range/If-Range, conditional requests, stable
  metadata-based ETag, correct length/type/disposition, and `nosniff`.
- Resolve to an anchored descriptor first without triggering a blocking FIFO or
  device read, inspect with `fstat` and `fstatfs`, reject disallowed object and
  pseudo-filesystem types, then obtain/verify the readable descriptor and clear
  nonblocking mode before serving. All steps stay descriptor-relative so path
  replacement cannot redirect the transfer.
- Reject directories, FIFOs, sockets, devices, and pseudo-files. The current
  `os.Root` is rooted at `/`; it provides safer path resolution, not an
  application allowlist. OS UID permissions remain the principal boundary.
- Sanitize `Content-Disposition` filenames.
- Use a normal browser navigation/anchor so the browser download manager owns
  streaming, resume, progress, and cancellation. Do not use `fetch().blob()`.
- After handoff, the normal download has no LinuxIO transfer entry, byte
  progress, in-app cancellation, completion detection, or authoritative success
  toast; the browser download manager owns that lifecycle. LinuxIO may show a
  non-authoritative "download handed to browser" notification. Durable archive
  preparation remains visible until its artifact is ready. A File System Access
  API enhancement is optional and not required for migration.

### Upload

Start without resumable sessions:

- Use one `PUT` per file through XHR for upload progress and cancellation.
- Require and validate expected length; use bounded buffers and per-session
  concurrency.
- Open and retain the destination-directory descriptor. Create a random `0600`
  temporary through that descriptor, stream and sync/close it, finalize with
  descriptor-relative `renameat2` semantics, fsync the parent directory, and
  clean up through the same descriptor.
- When overwrite is false, use an atomic no-clobber primitive rather than a
  stat-then-rename race. When overwrite is true, preserve/replace metadata only
  according to an explicit contract.
- Delete partial files on cancellation/error and clean abandoned temporaries.
- Migrate editor saves as well as selected-file uploads.
- Implement folder upload as bounded-concurrency per-file PUTs plus direct mkdir
  actions. Preserve current partial-success semantics and represent empty
  directories explicitly.

Add resumability later only if a measured product requirement justifies it. A
resumable transfer record contains only owner, target, expected size, committed
offset, temporary path, and expiry. It is not a durable job. Persistence is
needed only if resume must survive bridge replacement.

### Archive and generated artifacts

Archive creation is a durable job that produces an owned, expiring regular-file
artifact. HTTP serves the completed artifact with the same Range contract as a
normal file. Artifact cleanup is explicit and race-safe. Keep the legacy archive
`jobs.data` path until both durable creation and HTTP artifact delivery work.

## Durable-job minimum

The durable record contains only:

```text
random stable ID
owner UID/GID and authorization scope
typed operation name and schema version
redacted/allowlisted execution spec reference
requested durability level
state and monotonic revision
created/accepted/started/finished timestamps
bounded progress snapshot
bounded result summary or safe error
native executor reference
log cursor/reference
idempotency key, when supported
```

The durable event log separately records a persisted global sequence, owner
scope, job ID/revision, event kind, and timestamp. Clients receive an opaque
cursor and only events they are authorized to see.

Minimum control API. The permanent `durable_jobs.*` namespace intentionally
does not collide with the reserved legacy `jobs.*` primitives during
coexistence, so no backend aggregator or ID discrimination layer is required:

```text
typed operation start routes -> JobHandle
durable_jobs.sync         // atomic owned snapshot + event cursor
durable_jobs.get
durable_jobs.list
durable_jobs.cancel       // native-handoff Action returning CancelAccepted
durable_jobs.events       // replay after cursor; ordered and reconnectable
durable_jobs.log_tail     // bounded Query backed by journald, if the UI needs it
durable_jobs.logs_follow  // one generic filtered Duplex route, if the UI needs it
```

Do not recreate `jobs.attach`, `jobs.data`, per-job relay protocols, duplicate
stream cancellation, or one frontend lifecycle owner per job.

Required semantics:

- One global admission boundary across login bridges.
- Atomic, versioned state transitions and monotonic event revisions.
- An atomic snapshot watermark plus persisted global event
  sequence closes the snapshot/subscribe race. Replay starts strictly after the
  supplied opaque cursor and filters by owner; a retention gap returns
  `resync_required` rather than silently dropping events.
- Owner-based authorization using trusted peer/session credentials.
- Reconciliation between persisted state and systemd/native executor state.
- Cancel validates ownership, durably records cancellation intent, hands the
  stop request to the durable/native controller, and promptly returns a typed
  disposition such as `requested`, `already_requested`, or `already_terminal`.
  Repeated requests are idempotent. The control request has a bounded timeout;
  it does not wait for worker death. Only the event stream marks terminal
  `canceled`, after the executor is confirmed stopped. One authoritative writer
  gives completion/cancel races a deterministic winner and reconciliation
  closes crash windows between the persistent intent and native stop request.
- Worker runs as the declared UID/GID with explicit working directory,
  environment allowlist, filesystem access, resource limits, runtime cap, and
  systemd sandbox properties.
- Detailed output goes to journald; job APIs return bounded summaries.
- Scheduled work uses native persistent timers. Jobs that can start without a
  login bridge enter the same record and ownership model.

The existing `docs/transient-units-plan.md` is useful research but conflicts
with this final target in its bridge-local tracking and in-process-job non-goal.
It must be reconciled or marked superseded when the durable architecture record
is accepted.

## Stop-gated implementation stages

No stage begins automatically after the preceding stage. At every `STOP`, hand
over the diff, route-count delta, exact Make target and result, focused test
evidence, benchmark/security observations, known limitations, and rollback
instructions. Wait for explicit approval.

### Stage 0 — Freeze baseline and classify every route

Scope:

- Add the machine-readable route manifest and coverage validation.
- Record all 228 current routes, job policies, frontend call sites, invalidation
  mappings, hidden query side effects, mixed-route splits, and probable durable
  candidates.
- Record baseline production measurements for query, current short mutation,
  file transfer, terminal latency during transfer, cancellation, and browser/
  webserver/bridge RSS.
- Record the current generated artifacts and legacy primitive consumers.
- Decide how authenticated frontend caches are isolated on identity change.

Exit criteria:

- Every route has a reviewed target or an explicit split; none is unclassified.
- A test fails for missing/stale routes.
- Baseline `make test` passes.
- No runtime route behavior changes.

**STOP 0: present the manifest and baseline for approval.**

### Security prerequisite S — Redact legacy job snapshots

Ship this as an independently reviewable security change before Stage 1. It is
not coupled to the Action/code-generation foundation and does not migrate any
route:

- Remove decoded requests from public legacy snapshots and every start, get,
  list, cancel, event, and attach response.
- Replace frontend recovery/display dependencies with default-deny,
  route-declared, typed safe operation metadata. Passwords, tokens, cloud-init
  data, share credentials, and other request secrets must never cross back to
  the browser.
- Keep execution input private only while the legacy job needs it and release it
  on queued cancellation or terminal completion.
- Test every public snapshot path plus generated frontend recovery and labels.

Exit criteria:

- Credential-bearing request fields are absent from all browser-visible job
  state and event history.
- Existing recoverable job UX uses only reviewed safe metadata.
- No production route mode or execution behavior changes.
- `make test` passes.

**STOP S: present the redaction proof before beginning the Action foundation.**

### Stage 1 — Add the direct-action and mode-safe API foundation

Backend:

- Add `ModeAction` and typed `apischema.Action` while retaining legacy jobs.
- Reuse the existing typed handler adapter and reject action progress/data.
- Derive a one-shot Query/Action context that cancels on explicit abort,
  EOF/reset, timeout, or bridge shutdown. Duplex handlers continue to own their
  stream reads.
- Pass a safe correlation ID into router logging.
- Add the smallest ID-free action admission mechanism proven necessary by the
  manifest. Preserve operation-specific serialization and timeout behavior;
  prefer handler atomicity/locking over a generic scheduler where possible.
- Add explicit disconnect/acceptance policy and ambiguous-commit errors.

Frontend/code generation:

- Generate mode-specific endpoint types, including a temporary
  `LegacyJobEndpoint` so all existing job consumers remain compile-time valid
  until their route migrates.
- Add external `AbortSignal` support to the core request.
- Propagate query cancellation and expose explicit action cancellation.
- Force action retry off at React Query and transport layers.
- Generalize invalidation from job-only to typed operation completion.
- Isolate caches by authenticated identity.
- Retain a provider-independent typed `execute` path for authentication and
  capability bootstrap before the QueryClient provider mounts.

Documentation:

- Update the API contract and bridge-handler pattern for Query, Action,
  DurableJob, Duplex, cancellation, logging, and retries.

Exit criteria:

- Compile-time tests reject invalid endpoint hooks.
- Bootstrap capability fetch works without QueryClient, and logout/identity
  change cancels work and clears both QueryCache and MutationCache.
- Integration tests prove handler context cancellation on abort and EOF.
- Retry, timeout, admission, logging redaction, and identity-switch tests pass.
- No production route has migrated yet.
- `make test` passes.

**STOP 1: review the foundation before moving a real mutation.**

### Stage 2 — Migrate only `config.set` as the action canary

- Change `config.set` from legacy job to direct action.
- Preserve safe write serialization/atomicity for rapid patches from one or
  multiple sessions.
- Keep optimistic UI and declarative invalidation through `useAction`.
- Add its specific structured audit record without logging configuration
  contents.
- Compare direct-action latency/allocations with the frozen legacy baseline.

Exit criteria:

- No job is created and one request returns the business result.
- Rapid ordered/concurrent patch tests show no lost update.
- Abort, timeout, ambiguous disconnect, no-retry, error, and success paths pass.
- Production latency/allocations are no worse than the legacy job path; any
  regression requires explanation and approval.
- `make test` passes.

Rollback: change only this route declaration/frontend hook back to legacy job;
the coexistence layer remains intact.

**STOP 2: present canary behavior and measurements.**

### Stage 3 — Split mixed contracts while behavior is still legacy-compatible

- Split routes whose mode depends on an action field or guessed input size.
- Introduce a strict same-filesystem atomic rename route with no size prewalk or
  copy/delete fallback; keep cross-filesystem move/copy on the long-operation
  path.
- Separate account metadata edits from home migration/deletion where required.
- Give accepted native-manager actions and expected-disconnect actions explicit
  result contracts.
- Reclassify log-follow operations as Duplex and semantic-read jobs as Query or
  explicit refresh Action.
- Remove host-configuration side effects from declared Query paths. Move Docker
  network creation and package refresh to explicit startup/actions; document and
  bound transparent cache writes.

Document the archive split in the manifest, but do not create an intermediate
legacy implementation. Perform that split once in Stage 11, when durable
artifact creation and HTTP delivery are both available.

Exit criteria:

- Each resulting route has one stable execution contract.
- Declared Query routes perform zero semantic host mutations.
- Frontend and invalidation mappings use the new typed routes.
- Existing user-visible behavior is retained.
- `make test` passes.

**STOP 3: review the contract changes before bulk migration.**

### Stage 4 — Migrate bounded actions in small batches

The Stage 0 manifest determines batches. Likely groups are:

1. Preferences and small configuration CRUD.
2. Small atomic filesystem metadata operations such as rename, mkdir, and touch.
3. Native systemd/service handoffs whose result means accepted.
4. Small account/share/storage CRUD without recursive data movement.
5. Network and power actions with explicit expected-disconnect behavior.

Do not put recursive delete/chmod, copy/move, package work, image pulls, VM image
preparation, or similar variable/unbounded operations into a short-action batch.

For each batch:

- Replace `useJobAction` with the generated action endpoint.
- Preserve pending/error/success UX and invalidation.
- For native handoffs, show `accepted` separately from `completed` and follow the
  manifest's convergence query/event until the target state or timeout; do not
  refetch once immediately and imply completion.
- Audit admission, D-Bus `NoRetry`, context propagation, and structured logging.
- Update manifest counts and prove the route creates no job.
- Run `make test`.
- **STOP 4.n after every batch.**

### Stage 5 — Add the private HTTP-over-yamux substrate only

- Add the trusted stream-kind protocol to every yamux open site.
- Keep one bridge accept loop and add a bounded channel-backed `net.Listener`.
- Run a private bridge `http.Server` with lifecycle-safe shutdown.
- Add the session-specific webserver `http.Transport`/reverse proxy with one
  HTTP request per yamux stream.
- Add fixed authenticated probe handlers only; do not cut over production file
  operations in this stage.

Security tests must prove:

- WebSocket payloads cannot select the HTTP server.
- Unknown stream kinds fail closed.
- Session A cannot select session B's bridge or files.
- Cross-origin, expired session, stripped-header, method/path allowlist, size,
  cancellation, and shutdown behavior are correct.
- Production session cookies remain explicitly `SameSite=Strict`, `Secure`, and
  `HttpOnly`; regression tests fail if those defaults are relaxed accidentally.
- A stalled stream-kind sender times out without blocking other classifications,
  and classifier concurrency remains bounded.
- Old-webserver/new-bridge and new-webserver/old-bridge combinations fail closed;
  upgrade and rollback deterministically terminate/recreate all bridge sessions.

Performance tests must include query/action/terminal behavior while a synthetic
HTTP stream is active. `make test` must pass.

Rollback: remove the unused HTTP path and stream-kind support together; no file
consumer depends on it yet.

**STOP 5: present protocol, security, lifecycle, and contention results.**

### Stage 6 — Cut over regular-file downloads

- Add production `GET`/`HEAD` handlers and the full regular-file/Range contract.
- Run old and new paths side by side during verification.
- Change normal single-file download to browser-native HTTP navigation.
- Keep archive download on the legacy path.
- Add a browser-realistic cross-site file-GET test. The production Strict
  session cookie must prevent an authenticated cross-site navigation. If the
  route also enforces Fetch Metadata, accept the anchor-download shape
  (`Sec-Fetch-Site: same-origin`, normally `Sec-Fetch-Dest: empty`), define the
  policy for absent headers/non-browser clients, and reject untrusted
  `cross-site` and, unless explicitly trusted, `same-site` initiators.
- Deliberately remove normal downloads from LinuxIO's global transfer indicator
  after browser handoff. Browser UI owns progress, cancellation, and completion;
  LinuxIO may report only that the handoff started. Keep archive preparation in
  the durable-job UI until its artifact is ready.

Exit criteria:

- Byte equality, Range restart, conditionals, cancellation, filename sanitation,
  special-file rejection, ownership, and race tests pass.
- Browser memory remains bounded for a representative large file.
- Throughput, browser/webserver/bridge RSS, terminal p50/p95/p99, and query/action
  latency under transfer are recorded with enough iterations for useful tails.
- Transfer limits and session-expiry behavior work.
- Explicit logout/revocation/absolute expiry cancels the transfer promptly;
  active progress prevents idle-only expiration.
- `make test` passes.

**STOP 6: approve the browser-owned download UX, path-confidentiality decision,
cross-site defense, and real-condition results.**

### Stage 7 — Cut over single-file upload and editor saves

- Implement atomic single-file PUT, XHR progress/cancellation, overwrite/no-
  clobber semantics, partial cleanup, and specific audit logs.
- Migrate editor saves and ordinary selected-file uploads.
- On each successful PUT, update or invalidate the affected filebrowser entries;
  failure/cancellation does not publish a successful cache change.
- Keep batch/folder upload and archive on legacy transport.

Exit criteria:

- Large upload memory is bounded.
- Cancellation, short/long body, disk-full, permission, overwrite race, symlink,
  session expiry, bridge death, and temp cleanup tests pass.
- Throughput and terminal/control contention are recorded.
- `make test` passes.

**STOP 7: review upload correctness and measurements.**

### Stage 8 — Cut over folder/batch upload

- Use bounded-concurrency one-file PUTs plus mkdir actions.
- Preserve partial-success reporting, overwrite decisions, relative paths, and
  empty directories.
- Reconcile/invalidate only successfully committed files/directories after a
  partial batch; failed or canceled paths remain unchanged in client state.
- Do not add resumability unless Stage 0/7 evidence establishes the requirement.

Exit criteria:

- Mixed success, cancellation, reconnect, limits, path conflicts, and cleanup
  are tested.
- No regular upload consumer uses `jobs.data`.
- `make test` passes.

**STOP 8: decide explicitly whether resumable transfer sessions are needed.**

### Stage 9 — Durable architecture decision and native feasibility proof

This is a design/proof stage, not bulk job implementation.

- Reconcile `docs/transient-units-plan.md` with the D1/D2/D3 model.
- Prove `StartTransientUnit`, status, cancel, UID/GID, sandboxing, journald, and
  bridge-death survival for privileged and unprivileged users.
- Prove persistent timer behavior for scheduled work without a login bridge.
- Decide the authenticated control boundary: safe direct D-Bus or a minimal
  socket-activated typed broker.
- Decide the atomic persistent store, single-writer/locking model, recovery
  source of truth, retention, persisted global event sequence/cursor, atomic
  snapshot watermark, replay-gap behavior, and secret-handling model.
- Threat-model arbitrary command injection, peer identity, owner visibility,
  symlink/state-file attacks, unit-name collision, cancel/finish races, and
  worker privilege.

Preferred fallback if direct authorization is unsafe: one small broker/service
that controls systemd and records, while systemd—not the broker—owns worker
execution.

Exit criteria:

- A throwaway allowlisted worker survives bridge/webserver death and reconciles.
- An unprivileged actor cannot choose arbitrary argv, UID, unit properties, or
  another owner's job.
- Terminal state remains available after the unit and bridge are gone.
- The accepted ADR contains rollback and packaging implications.
- Applicable Make checks pass.

**STOP 9: approve the durable security/ownership architecture before building it.**

### Stage 10 — Build the minimal durable core with no production migration

- Implement stable IDs, typed operation registry, records, native executor
  adapter, global admission, state reconciliation, cancel, retention, atomic
  `durable_jobs.sync`, and one cursor-replay event stream.
- Generate typed start/result contracts.
- Replace duplicated frontend job ownership with one reconnecting store, while
  retaining the legacy provider for unmigrated routes.
- Use a test-only worker to exercise D1/D2 and D3 where promised.

Exit criteria:

- Ownership, redaction, size bounds, atomic transitions, ordering, duplicate/
  out-of-order events, the sync/subscribe race window, cursor retention gaps,
  exactly-once terminal invalidation, restart recovery, cancel races,
  idempotency, retention, and multi-session tests pass.
- Starting returns `JobHandle` promptly; the mutation is not pending for the job
  lifetime.
- `make test` passes.

**STOP 10: review the small core before a real long operation uses it.**

### Stage 11 — Use archive creation as the first production durable canary

- Durable archive job creates an owned artifact atomically.
- HTTP serves only a completed artifact with Range support.
- Reconnect, cancellation, disk-full, cleanup, expiry, ownership, and partial
  artifact behavior are verified.
- Frontend start mutation returns a handle; the central job store announces the
  downloadable artifact.
- Bridge/WebSocket/webserver loss does not stop execution, and reconnect restores
  state, progress summary, logs, artifact, and terminal result.
- `make test` passes.

Archive is non-destructive to the source and does not update LinuxIO itself,
making it safer than application update as the first real durable route.

**STOP 11: review archive recovery and delivery.**

### Stage 12 — Migrate LinuxIO application update

`control.app_update` already uses `systemd-run`, journald, and a status file.
Convert systemd from a bridge-blocking wrapper into the true owner only after
the durable core has survived the archive canary.

Verify:

- Bridge/WebSocket/webserver loss does not stop execution.
- Reconnect across old/new executable and record schema versions restores state,
  progress summary, logs, and terminal result.
- Worker/control protocol compatibility, cancel/restart races, update
  authentication/integrity checks, failed-update recovery, and rollback are
  deterministic.
- `make test` and an isolated real-system update/rollback test pass.

**STOP 12: present self-update and version-skew durability evidence.**

### Stage 13 — Remove `jobs.data` after its last consumer

- Machine-check that regular download, archive delivery, editor save, single
  upload, and batch upload have no `jobs.data`/data-attacher references.
- Remove only the legacy data-stream primitive and transfer-specific state now.
- Keep other legacy job APIs for still-unmigrated jobs.
- `make test` and file performance/security suites pass.

**STOP 13: present the zero-consumer proof and deletion diff.**

### Stage 14 — Migrate remaining durable/native operations by domain

The manifest, not this provisional list, controls final classification:

- Large copy/move/delete and extraction/indexing.
- Package installation/update transactions.
- Docker pulls and long compose/update work.
- VM image download/import and creation portions LinuxIO must own.
- Scheduled scripts and persistent timers.

Before wrapping an operation, check whether its native manager already supplies
the needed durable identity and recovery. Use an accepted Action plus native
state queries when that is sufficient.

For every domain, prove its declared D1/D2/D3 level, cancel/reconcile behavior,
owner isolation, native logs, and bounded records; run `make test`; then
**STOP 14.n after every domain**.

### Stage 15 — Remove the remaining legacy job architecture

Deletion gate:

- Generated manifest contains zero legacy Job/Runner routes.
- No frontend uses `useJobAction`, `useJobStreamAction`, snapshot unwrapping,
  attach polling, local-handled markers, or legacy recovery.
- No backend uses in-memory Registry/Job, `jobs.attach`, legacy events, legacy
  policies, or job-backed adapters.
- The legacy reserved `jobs.*` namespace has no consumers and is removed. The
  new service remains explicitly named `durable_jobs.*`; it is not renamed at
  the end of migration.

Then remove the legacy backend and frontend code, regenerate contracts, update
all architecture documents, and run `make test` plus recovery/performance/
security suites.

**STOP 15: present the zero-route proof and final deletion.**

### Stage 16 — Final validation and optional protocol cleanup

Functional and security:

- Route-by-route regression, structured errors, cancellation, no-retry,
  invalidation, audit logs, privilege, multi-user ownership, identity switching,
  cross-origin defense, secret redaction, and filesystem race tests.

Recovery:

- Kill stream, WebSocket, bridge, webserver, executor worker, and host at the
  points promised by each durability level; reconnect from a new browser
  session and verify reconciliation.

Performance:

- Production query/action p50/p95/p99 and allocations.
- Download/upload throughput and browser/webserver/bridge RSS.
- Terminal and control latency during saturated transfer.
- Concurrent-transfer fairness and cancellation latency.
- Durable start/list/event latency and record retention behavior.

Only after the architecture is stable, separately benchmark whether removing a
redundant inner relay stream-ID field is worth a protocol change. Browser stream
IDs and yamux stream IDs serve different multiplexing layers; this cleanup is
not on the critical path and must not be mixed into the job migration.

Run `make test` and the documented production/manual suites.

**STOP 16: final architecture handoff.**

## Quality and handoff rules

- Backend-only code stages: `make check-backend` at minimum.
- Frontend-only code stages: `make check-frontend` at minimum.
- Cross-stack, generated contract, shared transport, packaging, or unclear
  stages: `make test`.
- A narrower successful target does not replace `make test` where the stage
  crosses ownership boundaries.
- Record the exact target and result at every stop; call out anything that could
  not run.
- Focused real-system tests supplement Make targets; they do not replace them.
- Preserve unrelated user changes and keep each stage independently reviewable.

## Rollback strategy

- Legacy and target endpoint kinds coexist until Stage 15.
- Direct-action migration rolls back per route by restoring its declaration and
  generated frontend hook.
- HTTP endpoints are introduced in parallel; keep the old consumer until the
  new path passes its stop gate.
- The yamux stream-kind change rolls back only as a coordinated
  stop/replace/start of webserver and bridge binaries, terminating active
  sessions first. It is never rolled back one process at a time.
- A durable route never rolls back by abandoning already accepted jobs. Keep the
  new reader/reconciler until all existing records are terminal and retained;
  only new submissions may switch back.
- No stage deletes a compatibility path before its machine-checked consumer
  count is zero.

## Architectural precedents

This split follows the useful parts of established management systems without
copying their full frameworks:

- Cockpit uses connection-bound channels plus direct D-Bus, process, and file
  APIs. A channel is the lifetime handle for bounded/interactive work; it is not
  promoted to a persistent job ID by default.
- TrueNAS uses jobs for significant-time or high-I/O work and separate HTTP
  upload/download endpoints.
- Proxmox uses durable task identifiers and logs for long administrative work,
  not for every read or small mutation.
- systemd already supplies execution ownership, cancellation, resource control,
  timers, and journald. LinuxIO should add only typed authorization, ownership,
  durable summaries, and reconnection semantics that systemd does not provide.

Primary references:

- <https://cockpit-project.org/guide/latest/cockpit-channels.html>
- <https://cockpit-project.org/guide/latest/cockpit-dbus.html>
- <https://cockpit-project.org/guide/latest/cockpit-spawn.html>
- <https://cockpit-project.org/guide/latest/cockpit-file.html>
- <https://api.truenas.com/v25.10/jobs.html>
- <https://systemd.io/CONTROL_GROUP_INTERFACE/>
