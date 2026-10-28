# LinuxIO Agent Instructions

## Simple, idiomatic code

- Prefer the smallest coherent change that fixes the root cause and follows the surrounding package or component. Favor the standard library, existing dependencies, and existing project boundaries over unrelated refactors or speculative extensibility. Add a helper, interface, wrapper, or configuration option only when it creates real reuse, a needed ownership or test boundary, or materially simpler call sites.
- In Go, prefer synchronous, explicit control flow. For request-scoped, I/O, or blocking work, take the caller's `context.Context` first and propagate it. Handle errors with useful operation context, preserve identity with `%w` when callers may use `errors.Is` or `errors.As`, and do not panic for ordinary runtime or input failures. Every goroutine needs a clear owner, cancellation path, and exit condition.
- Logging (`log/slog`, journald sink; guarded by `backend/common/logging/style_test.go`):
  - Levels: Error (3) a request, task, or daemon failed and someone must look (router, task runner, HTTP middleware, fatal exit); Warn (4) degraded but continuing, or a 4xx; Info (6) one line per completed host mutation or daemon lifecycle event; Debug (7) reads, progress, request traces.
  - Log or return, never both: code that returns an error does not log it (the bridge router and task runner log every failed route and task); code that swallows an error logs it.
  - The reason is in MESSAGE: the journald handler appends the `error` attribute once, so never concatenate errors or values into the message.
  - Messages are lowercase constant text (a leading acronym or product name from the `logguard` allow-list is fine), no trailing punctuation, no `[Tag]` or `Prefix: `; context goes in attributes and the error key is `error`, never `err`.
  - Client errors wrap a status sentinel with `%w`: `ErrInvalidArgs` 400, `ErrForbidden` 403, `ErrNotFound` 404, `ErrConflict` 409; `bridgeipc.NewError` only where the code itself is the point.
  - Success is one Info line per completed host mutation, past tense, identifiers as attributes; progress and reads are Debug.
- In React, keep rendering pure and derive values instead of mirroring props or query data in state. Use Effects only to synchronize with external systems, with complete dependencies and cleanup; handle user actions in event handlers. Keep server state in TanStack Query, shareable URL state in validated TanStack Router search or path parameters, and transient UI state locally. Reuse generated query options and route helpers, preserving their invalidation and cancellation paths. Treat memoization as an optimization, not a default. Don't add `useCallback`/`useMemo`/`memo` outside `*Virtual*` files or props consumed by them; the compiler handles it. Existing ones may be removed when touching the file, confirming with `make compiler-coverage`.
- In the frontend, style through the shared `components/ui` components and the `--app-*` CSS variables `AppThemeProvider` writes to `:root` (`theme/variables.css`, `theme/cssVariables.ts`). Outside `components/ui` and `theme/`, code does not call `useAppTheme()`, compute colours with `alpha()`/`darken()`/`lighten()`, branch on `palette.mode`, or set hex colours or `fontSize` inline; `test/guards/styling-boundary.test.ts` lists the few files that must hand a browser API a resolved colour (canvas charts, xterm, ace, colour editors) and fails on any new one.
- The backend targets Linux; do not add portability layers without a concrete requirement. Prefer Go APIs and existing D-Bus or service abstractions over shell commands. When a subprocess is necessary, use the existing context-aware command path, pass arguments separately, honor cancellation and timeouts, and return useful stderr with the error. Treat optional host facilities as capabilities so their absence does not break unrelated features, keep privileged mutations narrow and validated, and distinguish unit tests from tests requiring systemd, Docker, libvirt, root, or a real host.
- Every route whose handler needs root (writes under `/etc` or `/var/lib`, mounts, block-device or filesystem ioctls, systemd, NetworkManager, Docker, libvirt, PackageKit, user and group changes, power) must declare `apischema.Privileged()` in its binding, so the dispatcher refuses it with 403 for a non-sudo session instead of the handler failing with a permission error part-way through. Reads stay unprivileged unless they expose sensitive data. See `docs/privilege_pattern.md`.

## Task execution and architectural authority

- An implementation specification supplied with the task is the approved product and architecture direction: validate it against the working tree, adapt minor differences to existing patterns, and do not re-plan it. If the repository fundamentally conflicts with it (the result would be incorrect, unsafe, or architecturally invalid), report the conflict instead of silently redesigning.
- Preserve the security and responsibility boundaries between `linuxio-webserver`, `linuxio-bridge`, and `linuxio-auth`; never move privileged or authentication-sensitive behavior across them for convenience.
- Use subagents only for bounded, independent work, preferring faster, lower-cost models; keep integration, conflict resolution, final diff review, and final decisions in the main agent. Parallel agents must be read-only or own disjoint files.
- Never run Make concurrently with implementation or with another Make invocation in the same worktree.

## Required quality workflow

- Every LinuxIO code change must be linted and tested before handoff.
- Use repository Make targets for Go, frontend, C, generation, build, and test workflows. Never invoke their underlying tools directly, including `npm`, `tsc`, `go test`, `gofmt`, `golangci-lint`, `govulncheck`, `vitest`, `oxlint`, or `oxfmt`.
- If one of those project workflows lacks a required focused target, add or improve a Make target.
- Run lightweight file-level tools required by higher-level instructions—such as `shellcheck`, `shfmt`, `actionlint`, `jq`, and `rg`—directly. Their use alone does not justify adding a Make target.
- Prefer the narrowest `*-quiet` target that fully covers the current work.
- Frontend-only changes: iterate with focused targets and finish with `make check-frontend-quiet`.
- Backend-only changes: iterate with focused targets and finish with `make check-backend-quiet`.
- Changes spanning frontend and backend, generated API contracts, shared build tooling, or unclear ownership boundaries: run `make test-quiet`.
- For claims that depend on real browser navigation, chunk loading, or browser behavior, additionally run `make test-frontend-browser-quiet`. Run `make setup-frontend-browser-quiet` first only when its browser dependency is not already installed.
- Report the exact Make target or targets run and their results. Call out any target that could not run, distinguish introduced failures from pre-existing or environmental failures, and do not claim browser/runtime coverage from source inspection or unit tests alone.

- Quiet targets retain complete logs in `.cache/test-logs/`; inspect those logs after a failure before rerunning with normal output.

## Generated files and contract changes

- Do not hand-edit `frontend/src/api/generated/*` or `frontend/src/routeTree.gen.ts`.
- When Go-owned API contracts or generator inputs change, use `make generate` and then validate the combined result with `make test-quiet`.
- Trace contract, router, and query-ownership changes through their callers, invalidation paths, cancellation paths, tests, source guards, and canonical documentation rather than changing one layer in isolation.

## Reviews, cleanup, and documentation

- Add or update tests for behavior changes. Reconcile related documentation, audit resolution text, and checkboxes when the implementation lands.
- Before removing code that looks like a compatibility wrapper or legacy API, inspect callers, generated references, history, and production reachability; do not delete it based on naming alone.
- Keep conclusions scoped to the evidence gathered. Clearly separate source-verified findings, automated-test results, and runtime-observed behavior.

## Git and handoff

- Never create, amend, or push a Git commit. Leave commit creation to the user.
- When coding work is complete, always present a concise suggested commit message for the resulting changes, even though no commit is created automatically. Do this only when actual code has been changed and not in exploratory reviews/comments.
