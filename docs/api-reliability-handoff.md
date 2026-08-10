# API Reliability Handoff

This is a portable handoff for the session that continues the LinuxIO API
reliability work on another machine. The canonical design remains in the linked
documents; this file records the exact implementation checkpoint, evidence, and
safe next slice.

## Starting Point

- Branch at handoff: `dev/v0.18.0`
- Completed transport commit:
  `5ad6d8bb refactor(api): finish typed Task transport migration`
- The worktree was clean after that commit and before this handoff file was
  added.
- Current generated inventory: 203 Calls, 18 Tasks, and 9 Channel/Duplex
  routes.

On the new machine, begin with:

```bash
git status --short
git log -1 --oneline
```

Do not assume the branch or worktree still matches this checkpoint. Preserve
any newer or uncommitted work before continuing.

## Completed Architecture

The transport migration is finished. Do not reopen it or add a compatibility
runtime unless a verified regression requires it.

- TanStack Query owns frontend server-state caching and mutation presentation;
  it has no Task lifecycle dependency.
- Every bounded backend operation is a Call. Query versus mutation is only a
  frontend policy choice.
- Long-lived sessions and streams use payload-specific Channels.
- All 18 Tasks use one typed `TaskRunner[Request, Result]` form. The compiler
  checks each terminal result before the bridge registry erases it.
- Generated Task contracts contain their actual terminal-result and progress
  types. Starting a Task returns `TaskSnapshot`; Task hooks then resolve the
  declared terminal result.
- `filebrowser.resource_patch` and `virt.create` were converted from raw
  emitters to typed Task runners.
- `HandleEvents`, the universal `Events` interface, `taskEmitter`, handler-form
  Tasks, and the Router's `tasks.*` dispatch switch were removed.
- `TaskService` registers and owns the implementations of
  `tasks.get/list/cancel/watch/data/events`. The Router still owns common route
  dispatch, privilege checks, decoding, and Task admission; do not move
  scheduling merely to satisfy an abstraction.
- Direct log Channels and Task watch/events interrupt blocked writes when the
  client closes, aborts, or disconnects. Task-watch close detaches; abort
  cancels the Task.

The following are intentional final surfaces, not compatibility wrappers:

- `createTaskEndpoint` and `TaskEndpoint` types;
- generated route metadata;
- backend `Mode` and `Kind`;
- Task `RequestShape`; and
- payload-specific Channel helpers.

They enforce real type, lifecycle, or protocol behavior. Remove one only when a
measured replacement is a clear net deletion without weakening safety.

## Verification at the Checkpoint

The final verification completed successfully:

- `make generate`
- `make test`
  - Go formatting, modernization, and golangci-lint: passed with 0 issues
  - backend race tests: passed
  - frontend lint and TypeScript: passed
  - Vitest: 143 files / 684 tests passed
  - C authentication helper: 16 checks passed
  - hermetic PAM integration: 22 checks passed
  - cross-language authentication: passed
  - dead-code scan: no dead code

One early full run saw a terminal-close timeout while other migration failures
were present. It did not recur in two later full runs, and the exact test passed
20/20 sequential executions under `-race`.

No performance gain was claimed. The migration removed concepts and obsolete
paths, but latency or throughput claims require a reproducible benchmark.

## Canonical Documents

Read these before changing the next cross-cutting behavior:

- [API Reliability Roadmap](./api-reliability-roadmap.md): canonical phase
  ordering and decisions.
- [API Contract](./api-contract.md): implemented Call/Channel/Task behavior.
- [Handler Patterns](./bridge_handler_patterns.md): current handler style.
- [Transport Simplification Plan](./api-transport-simplification-plan.md):
  completed migration record and retained-boundary rationale.
- [Durable Operations and Transient Units](./transient-units-plan.md): future
  durable-operation pilot.
- [Notifications](./notifications.md): future persistent notification design.
- [`ToDo`](../ToDo): short index only; do not duplicate detailed plans there.

## Next Slice: Strict Input and Explicit Call Policy

Continue with Phase 2 of the
[API Reliability Roadmap](./api-reliability-roadmap.md). Keep it a small,
vertical reliability change rather than another framework migration.

### 1. Strict standard-library request decoding

The current request path uses permissive `json.Unmarshal` in the typed
`apischema` decoder and the Task-service primitive decoder. Replace it with one
small shared standard-library policy that:

- uses `json.Decoder`;
- calls `DisallowUnknownFields()`;
- accepts exactly one JSON value;
- rejects trailing input; and
- preserves ordinary `encoding/json` scalar type errors.

Preserve the existing empty/null-to-`{}` behavior unless reviewing real route
contracts proves that it should change. Required-field meaning stays in domain
validation; do not infer presence from zero values. Do not generate custom
backend decoders without profiling evidence or a concrete presence-tracking
requirement.

Add table tests covering a valid object, unknown fields, trailing JSON, scalar
type mismatch, empty input, and null input for both normal routes and reserved
Task-service routes.

### 2. Explicit retry safety and honest connection loss

`frontend/src/api/calls.ts` currently infers retry safety from command names.
Replace that heuristic with explicit Go-owned route policy, for example an
`apischema.RetrySafe()` option emitted into one compact generated frontend map.
The default must be no retry.

Only explicitly safe reads may retry a connection-close failure, within the
original deadline. Never blindly retry a mutation.

Distinguish these outcomes through the transport and frontend error type:

| Outcome | Meaning |
|---------|---------|
| `connection_unavailable` | No request stream opened; the request was not sent. |
| backend result/error | The bridge confirmed the outcome. |
| `outcome_unknown` | The stream opened and closed before a result; a mutation may have been accepted. |

Preserve route names, JSON envelopes, query keys, invalidations, privilege
checks, deadlines, and AbortSignal behavior. Add deterministic fault tests for
loss before opening a stream and loss after opening it. Feature code must not
branch on error message text.

Because this slice changes Go contracts, generation, and frontend transport,
finish it with:

```bash
make generate
make test
```

Use a fresh test-only worker for the final Make run and inspect the complete
post-test worktree before handoff.

## Later Phases

After strict input and explicit Call policy, follow the roadmap in order:

1. Define exact Task lifetime/owner scope and session activity semantics.
2. Prove one durable `control.app_update` operation with a persistent record
   and an external systemd execution owner.
3. Implement the bounded persistent per-user notification core.
4. Extend durability, notification sources, or decoder sophistication only from
   measured need.

Do not couple notifications to the in-memory Task registry, describe ordinary
Tasks as bridge-survivable, or add a universal recovery layer.

## Working Principles

- Prefer the smallest coherent root-cause solution.
- Use idiomatic Go and React; keep domain handlers simple.
- Preserve caller contexts, error identity, owner checks, and cancellation.
- Keep buffers, replay, retention, and persistent records bounded.
- Make performance claims only from measurements.
- Fail closed, report unknown outcomes honestly, and never retry mutations
  speculatively.
- Do not add compatibility wrappers, parallel runtimes, or speculative
  abstractions.
- Preserve unrelated worktree changes.
- Verify current source and behavior rather than relying only on this handoff.
