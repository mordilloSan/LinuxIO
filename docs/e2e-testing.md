# Three-Tier Test Plan

This plan defines how LinuxIO should expand automated coverage without making the
test suite depend on dangerous host mutations. The goal is to get the most value
from fast tests first, add real browser coverage where it is meaningful, and keep
privileged host testing isolated in a disposable VM.

## Principles

- Treat every generated `query` route as safe and read-only.
- Treat generated `job` and `duplex` routes as mutation-capable unless they are
  explicitly included in a small E2E allowlist.
- Keep destructive operations manual for now. This includes reboot, power,
  system time changes, destructive networking, destructive storage, broad
  systemd changes, app update flows, and anything that can break the runner.
- Do not commit VM disk images to the repo. Commit the VM recipe and cache built
  images outside git.
- Prefer real login, real routing, and real WebSocket behavior in Playwright.
- Prefer Vitest and Go tests for edge cases that can be covered without a real
  host.

## Tier 1: Full Vitest And Backend Coverage

Tier 1 is the default place for all deterministic logic, error paths, generated
runtime behavior, and UI state transitions that do not need a real host.

### Frontend Scope

- Auth/session behavior:
  - real `AuthContext` state transitions with mocked `/auth/login` and
    `/auth/logout`
  - WebSocket close code `1008` mapped to session-expired UI state
  - redirect to `/sign-in`
  - protected route guards after session expiry
  - logout state cleanup
- Remaining higher-risk hooks:
  - upload/download/archive/copy/move background-job recovery paths
  - `useXtermStreamTerminal`
  - `useFileQueries`
  - `useFileMutations`
  - update edge cases: verification timeout, version mismatch, and stream close
    before any output
- UI behavior where components own logic:
  - confirm/cancel dialog flows
  - file editor dirty-state close confirmation
  - Docker compose progress dialog interaction
  - settings and dev-tools panels that mutate local or backend state
- Generated API/runtime regression tests:
  - route-mode errors
  - query and mutation option shaping for key generated endpoints
  - retry and no-retry behavior across API layers
  - generated query route smoke tests using mocked bridge responses

### Backend Scope

- Session/auth integration:
  - expired or invalid session cookie rejects protected HTTP routes
  - expired or invalid session cookie rejects bridge/WebSocket upgrade
  - WebSocket close code `1008` and close path map to session expiry
  - `/auth/logout` clears the browser session as expected
  - config and bridge access fail correctly once the session is expired
- Backend tests should use short session lifetimes and in-memory fixtures rather
  than sleeping against production timeout values.

### Tier 1 Done Criteria

- The high-risk hooks above have focused Vitest coverage.
- Session expiry behavior is covered at both frontend runtime and backend
  middleware boundaries.
- Generated route/runtime tests catch route-mode and retry regressions.
- No test in this tier requires sudo, Docker daemon access, systemd mutation, or
  a real LinuxIO install.

## Tier 2: Local Playwright For Non-Privileged Queries

Tier 2 proves that the real browser, real login, real WebSocket bridge, and real
generated query routes work together against a normal local LinuxIO test user.

This tier should run outside the VM. It should not create or delete host users by
default. Instead, it should accept credentials from environment variables:

```sh
E2E_BASE_URL=http://127.0.0.1:8080
E2E_USERNAME=teste
E2E_PASSWORD=teste
```

The `teste/teste` account can remain a convenient local default when already
configured, but the suite should document that the user is expected to be
non-privileged.

### Tier 2 Scope

- Real login and logout through the browser UI.
- Redirect to `/sign-in` for unauthenticated access.
- Protected routes redirect correctly after backend session expiry.
- Backend/WebSocket session expiry closes the bridge with code `1008` and the
  frontend shows the session-expired path.
- Query-only smoke tests for generated routes that do not require elevated host
  privileges.
- Config read through the real UI when it is query-only.
- Read-only file browser flows against a fixture directory.
- Read-only Docker/log/terminal views only when they work without elevated
  privileges on the local test host.

### Tier 2 Exclusions

- No file upload, copy, move, delete, archive, or download mutations.
- No config write tests.
- No Docker compose mutations.
- No sudo or privileged bridge paths.
- No app update tests.
- No destructive route coverage.

### Harness Shape

- Add Playwright under the frontend workspace.
- Add tags such as `@local-query` and `@session`.
- Provide a setup check that fails clearly when `E2E_BASE_URL`,
  `E2E_USERNAME`, or `E2E_PASSWORD` is missing.
- Prefer a short backend session timeout in the test environment for expiry
  tests.
- Keep fixtures under the non-privileged user's home directory.

### Tier 2 Done Criteria

- A developer with an already configured LinuxIO test user can run local
  Playwright without a VM.
- Browser tests cover real auth, route protection, WebSocket session expiry, and
  non-privileged generated query routes.
- The suite never performs host mutation beyond logging in and reading safe
  state.

## Tier 3: VM Playwright For Privileged Queries And Safe Mutations

Tier 3 runs against a disposable Ubuntu 24.04 VM. It is the place for privileged
queries and safe mutations/jobs that need a real Linux host, Docker, systemd, or
filesystem effects.

### VM Baseline

The committed repo should contain a VM recipe, not a VM image. The recipe should:

- install Ubuntu 24.04
- create a sudo-capable test user
- enable password or key-based login for the test harness
- install and enable Docker
- prepare a deterministic fixture directory
- expose LinuxIO over a known host-only address or forwarded port
- allow the current LinuxIO build to be installed at test time

Prepared base images can be cached outside git. The cache key should include the
VM recipe hash, the Ubuntu base image digest, and any major dependency versions.
Future distro coverage can add additional recipes or profiles using the same
test contract.

### Tier 3 Scope

- Privileged generated query routes.
- Config write/read through the real UI.
- File browser upload, copy, move, delete, archive, and download flows within an
  isolated fixture tree.
- Docker compose up, progress, logs, and cleanup using a fixture compose file.
- Terminal stream flows with bounded commands and predictable output.
- Background job recovery paths for safe jobs:
  - upload
  - download
  - archive
  - copy
  - move
- Session expiry behavior while privileged bridge activity is open.

### Tier 3 Mutation Allowlist

Tier 3 should maintain a small allowlist for safe `job` and `duplex` routes that
are permitted in VM Playwright. The allowlist is not a full route safety
manifest. It exists only to keep VM browser tests from accidentally exercising a
dangerous generated mutation.

Initial allowlist candidates:

- config set routes that write only LinuxIO configuration
- file operations restricted to the E2E fixture tree
- archive/download jobs restricted to the E2E fixture tree
- Docker compose operations restricted to a disposable fixture project
- terminal commands restricted to bounded, non-destructive commands

### Tier 3 Exclusions

These remain manual until there is a stronger isolation story:

- reboot and power operations
- app update flows
- system time changes
- destructive networking changes
- destructive storage changes
- broad systemd unit mutation
- any mutation that can affect the host outside the VM fixture boundary

### Harness Shape

- Add tags such as `@vm`, `@privileged`, and `@safe-mutation`.
- Run Playwright from the host against the VM URL.
- Reset the VM to a clean snapshot or cached base image before each run.
- Install the current LinuxIO build during test setup so the cached base image
  does not go stale with application code.
- Store logs, screenshots, traces, and VM console output as test artifacts.
- Keep Tier 3 out of the default fast test path. Run it manually, nightly, or in
  dedicated CI.

### Tier 3 Done Criteria

- A clean Ubuntu 24.04 VM can be prepared from committed scripts.
- The prepared VM can be cached outside the repo and reused by test runs.
- Privileged queries and safe mutations are covered through real browser flows.
- Unsafe routes are not automated.

## Suggested Implementation Order

1. Add Tier 1 missing tests around auth/session, high-risk hooks, UI dialog
   behavior, update edge cases, and generated runtime behavior.
2. Add the Playwright harness with local credentials, tags, traces, and a small
   real login/logout suite.
3. Expand Tier 2 to non-privileged generated query smoke tests and session expiry
   flows.
4. Add the VM recipe for Ubuntu 24.04 with Docker and a sudo test user.
5. Add Tier 3 fixtures and the safe mutation allowlist.
6. Add VM Playwright specs for privileged queries, config write/read,
   filebrowser mutations, Docker compose flows, and terminal streaming.

## Non-Goals For Now

- No full generated route safety manifest.
- No failure for every unclassified generated route.
- No checked-in VM images.
- No local Playwright suite that creates or deletes host users.
- No automated destructive route tests.
