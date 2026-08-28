# LinuxIO Agent Instructions

## Simple, idiomatic code

- Prefer the smallest coherent change that fixes the root cause and follows the surrounding package or component. Favor the standard library, existing dependencies, and existing project boundaries over unrelated refactors or speculative extensibility. Add a helper, interface, wrapper, or configuration option only when it creates real reuse, a needed ownership or test boundary, or materially simpler call sites.
- In Go, prefer synchronous, explicit control flow. For request-scoped, I/O, or blocking work, take the caller's `context.Context` first and propagate it. Handle errors with useful operation context, preserve identity with `%w` when callers may use `errors.Is` or `errors.As`, and do not panic for ordinary runtime or input failures. Every goroutine needs a clear owner, cancellation path, and exit condition.
- In React, keep rendering pure and derive values instead of mirroring props or query data in state. Use Effects only to synchronize with external systems, with complete dependencies and cleanup; handle user actions in event handlers. Keep server state in TanStack Query, shareable URL state in validated  TanStack Router search or path parameters, and transient UI state locally. Reuse generated query options and route helpers, preserving their invalidation and cancellation paths. Treat memoization as an optimization, not a default. Don't add `useCallback`/`useMemo`/`memo` outside `*Virtual*` files or props consumed by them; the compiler handles it. Existing ones may be removed when touching the file, confirming with `make compiler-coverage`.
- In the frontend, style through the shared `components/ui` components and the `--app-*` CSS variables `AppThemeProvider` writes to `:root` (`theme/variables.css`, `theme/index.ts`). Outside `components/ui` and `theme/`, code does not call `useAppTheme()`, compute colours with `alpha()`/`darken()`/`lighten()`, branch on `palette.mode`, or set hex colours or `fontSize` inline; `theme/styling-boundary.test.ts` lists the few files that must hand a browser API a resolved colour (canvas charts, xterm, ace, colour editors) and fails on any new one.
- The backend targets Linux; do not add portability layers without a concrete requirement. Prefer Go APIs and existing D-Bus or service abstractions over shell commands. When a subprocess is necessary, use the existing context-aware command path, pass arguments separately, honor cancellation and timeouts, and return useful stderr with the error. Treat optional host  facilities as capabilities so their absence does not break unrelated features, keep privileged mutations narrow and validated, and distinguish unit tests from tests requiring systemd, Docker, libvirt, root, or a real host.

## Task execution and architectural authority

- Treat an implementation specification supplied with the task as the approved
  product and architectural direction for that task.

- Before editing, inspect the relevant current implementation and validate the
  specification against the actual working tree.

- Do not repeat high-level product or architectural planning that has already
  been established in the supplied specification.

- If the specification differs from the current implementation only in minor
  details, adapt the implementation using existing repository patterns.

- If the current repository fundamentally conflicts with the specification in
  a way that would make the requested implementation incorrect, unsafe, or
  architecturally invalid, report the conflict rather than silently redesigning
  the feature.

- Keep low-level implementation decisions in the coding agent: exact functions,
  types, file edits, test structure, error propagation, and other details that
  are already determined by existing repository patterns.

- Preserve the security and responsibility boundaries between
  `linuxio-webserver`, `linuxio-bridge`, and `linuxio-auth`. Do not move
  privileged or authentication-sensitive behavior across these boundaries
  merely for implementation convenience.

- Subagents may be used when they provide a clear benefit for bounded,
  independent work, but they are not required for non-trivial tasks and should
  not be spawned merely to repeat planning already provided with the task.

- When subagents are used, parallel agents must be read-only or own clearly
  disjoint files. Never allow overlapping edits in the same worktree.

- The main coding agent owns integration, conflict resolution, final diff
  review, and the final handoff.

- Never run Make verification concurrently with implementation or with another
  Make invocation when repository tooling may mutate the shared worktree.

- Wait for every spawned agent before handoff and review the complete resulting
  diff after all implementation and verification work is complete.

## Required quality workflow

- Every LinuxIO code change must be linted and tested before handoff.
- Always use repository Make targets. Never invoke underlying tools directly, including `npm`, `eslint`, `tsc`, `go test`, `gofmt`, `golangci-lint`, `govulncheck`, `vitest`, `oxlint`, or `oxfmt`.
- If the required focused target does not exist, add or improve a Make target instead of bypassing Make.
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