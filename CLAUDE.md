# LinuxIO Claude Instructions

## Simple, idiomatic code

- Prefer the smallest coherent change that fixes the root cause and follows the
  surrounding package or component. Favor the standard library, existing
  dependencies, and existing project boundaries over unrelated refactors or
  speculative extensibility. Add a helper, interface, wrapper, or configuration
  option only when it creates real reuse, a needed ownership or test boundary,
  or materially simpler call sites.
- In Go, prefer synchronous, explicit control flow. For request-scoped, I/O, or
  blocking work, take the caller's `context.Context` first and propagate it.
  Handle errors with useful operation context, preserve identity with `%w` when
  callers may use `errors.Is` or `errors.As`, and do not panic for ordinary
  runtime or input failures. Every goroutine needs a clear owner, cancellation
  path, and exit condition.
- In React, keep rendering pure and derive values instead of mirroring props or
  query data in state. Use Effects only to synchronize with external systems,
  with complete dependencies and cleanup; handle user actions in event
  handlers. Keep server state in TanStack Query, shareable URL state in validated
  TanStack Router search or path parameters, and transient UI state locally.
  Reuse generated query options and route helpers, preserving their invalidation
  and cancellation paths. Treat memoization as an optimization, not a default.
  Don't add `useCallback`/`useMemo`/`memo` outside `*Virtual*` files or props
  consumed by them; the compiler handles it. Existing ones may be removed when
  touching the file, confirming with `make compiler-coverage`.
- In the frontend, style through the shared `components/ui` components and the `--app-*` CSS variables `AppThemeProvider` writes to `:root` (`theme/variables.css`, `theme/index.ts`). Outside `components/ui` and `theme/`, code does not call `useAppTheme()`, compute colours with `alpha()`/`darken()`/`lighten()`, branch on `palette.mode`, or set hex colours or `fontSize` inline; `theme/styling-boundary.test.ts` lists the few files that must hand a browser API a resolved colour (canvas charts, xterm, ace, colour editors) and fails on any new one.
- The backend targets Linux; do not add portability layers without a concrete
  requirement. Prefer Go APIs and existing D-Bus or service abstractions over
  shell commands. When a subprocess is necessary, use the existing
  context-aware command path, pass arguments separately, honor cancellation and
  timeouts, and return useful stderr with the error. Treat optional host
  facilities as capabilities so their absence does not break unrelated features,
  keep privileged mutations narrow and validated, and distinguish unit tests
  from tests requiring systemd, Docker, libvirt, root, or a real host.

## Orchestration and Sonnet delegation

- Keep orchestration, integration, conflict resolution, final diff review, and
  final decisions in the main thread (Sol when available).
- For non-trivial work, prefer Claude Sonnet workers configured with xhigh
  reasoning and fast mode enabled for bounded exploration, implementation,
  and test execution when delegation saves main-context time. Tiny changes do
  not require a subagent when spawning one costs more than the work itself.
- Parallel agents should be read-only or own clearly disjoint files. All agents
  share one worktree, so never allow overlapping edits.
- After non-trivial implementation is quiescent, Sol should spawn a fresh
  Sonnet test worker, with xhigh reasoning and fast mode enabled, to run the
  required final Make target and report the exact result. Sonnet supplies test
  evidence; it does not approve the change or own the final diff review.
- Sol must inspect the complete final diff and post-test worktree, reconcile all
  worker reports, and decide whether the work is ready for handoff. Sol need not
  repeat an identical Make target unless the diff changed after it ran or its
  result is incomplete or ambiguous.
- A test worker must not hand-edit fixes. It must report findings, the exact
  commands run, their outcomes, and any files changed automatically by repository
  tooling. Sol decides and implements follow-up fixes.
- Never run Make verification concurrently with implementation or with another
  Make invocation. Setup, lint, formatting, `go mod tidy`, and modernization can
  mutate the shared worktree. Inspect the diff again after verification.
- Wait for every spawned agent before handoff. Agent reports are evidence; the
  main thread reconciles disagreements and owns the final claims.

## Required quality workflow

- Every LinuxIO code change must be linted and tested before handoff.
- Always use repository Make targets. Never invoke underlying tools directly,
  including `npm`, `eslint`, `tsc`, `go test`, `gofmt`, `golangci-lint`,
  `govulncheck`, `vitest`, `oxlint`, or `oxfmt`.
- If the required focused target does not exist, add or improve a Make target
  instead of bypassing Make.
- Prefer the narrowest `*-quiet` target that fully covers the current work.
- Frontend-only changes: iterate with focused targets and finish with
  `make check-frontend-quiet`.
- Backend-only changes: iterate with focused targets and finish with
  `make check-backend-quiet`.
- Changes spanning frontend and backend, generated API contracts, shared build
  tooling, or unclear ownership boundaries: run `make test-quiet`.
- For claims that depend on real browser navigation, chunk loading, or browser
  behavior, additionally run `make test-frontend-browser-quiet`. Run
  `make setup-frontend-browser-quiet` first only when its browser dependency
  is not already installed.
- Report the exact Make target or targets run and their results. Call out any
  target that could not run, distinguish introduced failures from pre-existing
  or environmental failures, and do not claim browser/runtime coverage from
  source inspection or unit tests alone.

- Quiet targets retain complete logs in `.cache/test-logs/`; inspect those logs
  after a failure before rerunning with normal output.

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