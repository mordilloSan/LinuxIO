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
  - generated Call route smoke tests using mocked bridge responses

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

## Isolated Router Browser Regressions

The frontend workspace includes a safe Playwright harness for routing behavior
that does not require credentials, a backend, or host mutation. It builds a
small browser fixture with the production `RoutedTabContainer` and TanStack
Router, then runs Chromium coverage for:

- direct child-route links and refresh
- back/forward URL, active-tab, and content synchronization
- route pending and error UI inside the parent layout
- first-navigation loading of a lazy child component
- all 21 real page-level child-route components remaining distinct dynamic
  entries in the production Vite manifest

Install the pinned Playwright Chromium build once, then run the suite:

```sh
make setup-frontend-browser
make test-frontend-browser
```

Failed runs retain Playwright traces and screenshots under
`.cache/playwright/test-results/`. This harness protects router mechanics and
production chunk boundaries; it does not claim to cover real authentication,
the WebSocket bridge, or backend data. Those remain Tier 2 concerns below.

## Tier 2: Local Playwright For Non-Privileged Queries

Tier 2 proves that the real browser, real login, real WebSocket bridge, and real
generated Call routes work together against a normal local LinuxIO test user.

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

### Remaining Harness Shape

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
  non-privileged generated Call routes.
- The suite never performs host mutation beyond logging in and reading safe
  state.

## Tier 3: VM Playwright For Privileged Queries And Safe Mutations

Tier 3 runs against a disposable Ubuntu 26.04 VM. It is the place for privileged
queries and safe mutations/jobs that need a real Linux host, Docker, systemd, or
filesystem effects.

### What Exists Now

The rig is in place and drives the jobs checks; the other specs are still few.
See `docs/development.md`, "Test VM", for usage.

- `packaging/vm/linuxio-vm.sh` and `mk/vm.mk` drive one libvirt domain,
  `linuxio-e2e` on `qemu:///system`: 4 vCPU, 4 GiB RAM, a 25 GiB system disk
  over the checksummed Ubuntu 26.04 cloud image, and four 8 GiB data disks
  (`vdb`–`vde`) for storage tests. All disks are qcow2.
- `make vm-create` boots it once with `packaging/vm/user-data.yaml.in`
  (cloud-init over HTTP: test user with passwordless sudo, Docker, btrfs-progs,
  `install-dependencies.sh`) and snapshots it as `clean`. The image cache, SSH
  key and login live in `~/.cache/linuxio-vm`, outside git.
- `make vm-install` installs the current build; `make vm-reset` reverts every
  disk to `clean`; `make vm-destroy` removes the domain and its volumes.
- `make test-vm-browser-rerun` runs `frontend/src/test/vm/*.spec.ts` with
  `frontend/config/playwright.vm.config.ts`; `login.spec.ts` signs in, sees the
  dashboard and signs out.
- `jobs-phase2.spec.ts` and `jobs-phase3.spec.ts` start real jobs in the browser
  (package update, Compose, file copy, scrub, balance, device replace, pool
  create, Docker storage switch) and cut the host: `systemctl stop`, `kill -9`,
  reboot and power loss (`virsh destroy`). `linuxio-vm.sh throttle` caps a data
  disk's speed so a job outlasts the event. What each spec file covers is in
  [Jobs](../jobs.md#testing).
- `make vm-test-btrfs-integration` runs the root btrfs and partition kernel
  tests (`test-btrfs-integration`) as root in the VM; the kernel-level claims
  of the btrfs resume work rest on that run.
- `capability-install.spec.ts` installs zram from Capabilities, on Ubuntu,
  and on Fedora once sign-in works there (see [Distributions](#distributions)).
- `DISTRO=fedora` on the same targets drives a Fedora 45 VM,
  `linuxio-e2e-fedora`, beside the Ubuntu ones (see
  [Distributions](#distributions)).

Not yet: tags, the mutation allowlist, fixtures, and artifact collection beyond
Playwright's own traces and screenshots.

### VM Baseline

The committed repo should contain a VM recipe, not a VM image. The recipe should:

- install Ubuntu 26.04
- create a sudo-capable test user
- enable password or key-based login for the test harness
- install and enable Docker
- prepare a deterministic fixture directory
- expose LinuxIO over a known host-only address or forwarded port
- allow the current LinuxIO build to be installed at test time

Prepared base images can be cached outside git. The cache key should include the
VM recipe hash, the Ubuntu base image digest, and any major dependency versions.

### Distributions

Decided 2026-11-17: the rig gets a VM for every distribution the installer
accepts (`is_debian` and `is_fedora` in
`packaging/scripts/install-dependencies.sh`), not Ubuntu alone:

| Family | Distributions |
|---|---|
| Debian | Ubuntu (exists), Debian, Linux Mint, Pop!_OS |
| RHEL | Fedora, RHEL, CentOS Stream, Rocky Linux, AlmaLinux |

RHEL 8 and its rebuilds are not supported and get no VM.

Built (2026-11-18):

- `packaging/vm/linuxio-vm.sh` takes the distribution as a parameter,
  `DISTRO=fedora` on the Make targets (`LINUXIO_VM_DISTRO` to the script),
  Ubuntu when it is not given. A profile at the top of the script holds what
  differs: the image URL, where its checksum is published, the first-boot
  seed, the package manager the suites install with and the packages they
  install. The domain, its volumes and its cache are named per distribution
  (`linuxio-e2e-fedora`, `linuxio-e2e-fedora-*`, `~/.cache/linuxio-vm-fedora`),
  so two exist side by side; Ubuntu keeps the names it had.
  `docs/development.md`, "Test VM", says how.
- A Fedora 45 profile (Cloud Base image, `user-data-fedora.yaml.in`, `dnf`,
  SELinux enforcing), and these on it, on 2026-11-18 (kernel
  7.3.4-300.fc45):

| Target, with `DISTRO=fedora` | Result |
|---|---|
| `make vm-create` | Passes; the first boot takes about 2½ minutes |
| `make vm-install` | Passes: `linuxio.target` active, the dashboard answers on 8090, no SELinux denial |
| `make test-vm-backend SUITES=relay` | Passes |
| `make test-vm-backend SUITES='filebrowser userconfig' VMS=1` | Passes |
| `make vm-test-memory-integration` | Passes with zram-generator: `TestMemoryIntegration`, and the drain with 2 GiB (the cancel left 1806 of 2063 MiB parked, the rest drained in 13 s) and capped (280 MiB in 2 min 13 s) |
| `make vm-test-memory-integration`, with the host checks (same day) | Passes after `make vm-reset DISTRO=fedora`, with the swap file held off at boot by a drop-in in place of the mask, which had kept `swap.target`, and with it zram, from starting at boot: after each reboot `swap.target` is active and zram the only swap. Also passing: Fedora's default zram alone and beside the swap file, `TestMemoryIntegration` with the generator's units for drop-ins in four folders and the zswap compressor refused with `ENOENT`, the status without sudo under TuneD 2.28, both drains (1806 MiB left after the cancel, drained in 12 s; capped, 279 MiB in 2 min 14 s), the drain through a `UUID=` alias, and `TestMemoryRebootIntegration` with fstab naming the swap file by its path and through a link |
| `make test-vm-backend SUITES=packagekit VMS=1` (same day) | Passes: with PackageKit's cache made cold by a dnf transaction outside it, Resolve returns an empty result, and the install refreshes once and installs `hello` |
| `make test-vm-browser VMS=1 PLAYWRIGHT_ARGS=capability-install.spec.ts` (same day, before `linuxio.rhel`) | Fails at sign-in, and `login.spec.ts` the same way: PAM cannot open the Debian `common-*` files the PAM file included (`PAM _pam_load_conf_file: unable to open config for common-auth`) |
| `make test-vm-browser-rerun VMS=1 PLAYWRIGHT_ARGS=capability-install.spec.ts`, with those includes changed to `password-auth` by hand and SELinux permissive in the VM, on the first sign-in after the VM booted (same day, before `linuxio.rhel`) | Passes: the install showed "Refreshing the package lists", then installed `zram-generator`, 3 s in all; no zram started, and the Memory overflow dialog named zram-generator as the provider |
| `make test-vm-browser VMS=1 PLAYWRIGHT_ARGS=login.spec.ts`, with `packaging/etc/pam.d/linuxio.rhel` installed (same day) | Fails one step later: PAM authenticates and opens the session, and SELinux denies the bridge's exec the `transition` to the user's context ([Process & Systemd Architecture](../process-systemd-architecture.md#selinux)). A wrong password and a user in `/etc/linuxio/disallowed-users` are refused by PAM (`pam_unix`, `pam_listfile`) |
| `make test-vm-browser-rerun VMS=1 PLAYWRIGHT_ARGS=login.spec.ts`, then `PLAYWRIGHT_ARGS=capability-install.spec.ts`, with SELinux enforcing and, made in the VM, first `pam_selinux.so open` removed from the PAM file and then a `SELinuxContext=` drop-in on `linuxio-auth@.service` with its socket already listening (same day) | Both pass under each change with no denial: `zram-generator` installed in 2 s and in 3 s, both without a refresh, later in a boot that had seen other sign-ins; no zram started, and the dialog named zram-generator as the provider |
| `make test-vm-browser VMS=1 VM_KEEP=1 PLAYWRIGHT_ARGS='login.spec.ts capability-install.spec.ts'`, with `SELinuxContext=-unconfined_u:unconfined_r:unconfined_t:s0-s0:c0.c1023` in `linuxio-auth@.service` as installed (same day) | Fails before any sign-in: systemd creates `linuxio-auth.socket`'s listening socket in that context, SELinux denies `init_t` the `create`, the socket fails with Permission denied and `linuxio-webserver.socket` with it, so port 8090 refuses connections. The VM run above added the drop-in while the socket was already listening (under Rejected in the [backlog](./backlog.md#rejected)) |
| `make test-vm-browser-rerun VMS=1 VM_KEEP=1 PLAYWRIGHT_ARGS='login.spec.ts capability-install.spec.ts'`, with an SELinux module that gives `linuxio-auth` a domain of its own loaded in the VM, after a reboot (same day) | Both pass, three times: with the module compiled from a `.te`, with the CIL generated from it on a VM reverted to clean, and with the hand-written CIL now in `packaging/selinux/linuxio.cil`. No denial of LinuxIO's; `linuxio-auth` and its listening socket run as `linuxio_auth_t`, the bridges as `unconfined_t`; a `user_u` and a `staff_u` user are refused |
| `make test-vm-browser VMS=1 VM_KEEP=1 PLAYWRIGHT_ARGS='login.spec.ts capability-install.spec.ts'`, with the installer loading `packaging/selinux/linuxio.cil`, then a reboot and `make test-vm-browser-rerun` with the same specs (same day) | Both pass as installed and again after the reboot. `ausearch -ts boot` shows no denial; the contexts and the refusals (a wrong password, a user in `/etc/linuxio/disallowed-users`, a `user_u` and a `staff_u` user) are those of [Process & Systemd Architecture](../process-systemd-architecture.md#selinux), and the SSH banner shows at an interactive login. On the same VM with the module removed and `linuxio-auth` relabelled `bin_t` (sign-in refused), `make vm-install` made the next sign-in work without a reboot; the installer run in a transient unit with the in-app updater's sandbox (`ProtectSystem=full` and its `ReadWritePaths`) found the module up to date and relabelled the binary, and with another version of the module in the store did not try the load (`/etc/selinux` read-only), named the command that loads it, relabelled the binary and went on, the older module and its policy staying loaded; with the banner's optional block naming a type the policy lacks, the module loaded, sign-in worked after a reboot and only the banner kept `var_run_t`; `uninstall.sh` left no module and no file of its types |

Not run on Fedora yet: the `btrfs`, `indexer`, `docker`, `updater` and
`network` suites, and the browser specs other than `login.spec.ts` and
`capability-install.spec.ts`. The root suites needed three things of the
rig and none of LinuxIO: `cloud-init status` as root (Fedora's cloud-init
keeps its state from other users), its default `zram0` switched off the way
Fedora documents before the memory suite, and a swap file made by btrfs and
labelled `swapfile_t` (`docs/development.md`, "Test VM"). Signing in needed
two things of LinuxIO: a PAM file of the RHEL family's own
(`packaging/etc/pam.d/linuxio.rhel`), since the Debian one includes
`common-*` files the RHEL family does not have, and an SELinux domain for
`linuxio-auth`, which the installer loads
([Process & Systemd Architecture](../process-systemd-architecture.md#selinux)).

Not built yet:

1. The other profiles. The first of the RHEL family also runs the SELinux
   checks of [Process & Systemd Architecture](../process-systemd-architecture.md#selinux)
   (what runs where, who gets in): that its policy has every name the module
   uses is expected, not seen.
2. The browser specs on the RHEL family. `login.spec.ts` needs nothing of
   the distribution, and `capability-install.spec.ts` is written for both
   families (it takes the zram tool out and puts it back with the
   distribution's package manager); both pass on Fedora as installed. The
   others assume Ubuntu: `jobs-phase2.spec.ts` builds `apt`
   packages (`frontend/src/test/vm/fixtures/many-updates.sh`,
   `slow-package.sh`), and `memory.spec.ts`, `ui-actions.spec.ts` and the
   `jobs-phase4`, `-phase5a`, `-phase5b`, `-needrestart` and `-update` specs
   call `apt-get`, `dpkg` or `stopAptRuns`. Each needs an `rpm` twin of what
   it does with packages, or stays on the Debian family; the other specs have
   not been tried on Fedora.
   Then the table above grows a column per distribution. The root Go suites
   are expected to run everywhere.

Open, to settle with the other profiles: Linux Mint and Pop!_OS publish installer images
and no cloud image, and a RHEL image needs a Red Hat account, so these three
need another way in (an unattended install, or an image the owner supplies)
or are covered by their family: Ubuntu for the first two, Rocky Linux or
AlmaLinux for RHEL.

### Tier 3 Scope

- Privileged generated Call routes.
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

- A clean Ubuntu 26.04 VM can be prepared from committed scripts.
- The prepared VM can be cached outside the repo and reused by test runs.
- Privileged queries and safe mutations are covered through real browser flows.
- Unsafe routes are not automated.

## Suggested Implementation Order

1. Add Tier 1 missing tests around auth/session, high-risk hooks, UI dialog
   behavior, update edge cases, and generated runtime behavior.
2. Extend the existing isolated Playwright tooling with local credentials,
   tags, and a small real login/logout suite.
3. Expand Tier 2 to non-privileged generated query smoke tests and session
   expiry flows.
4. Add the VM recipe for Ubuntu 26.04 with Docker and a sudo test user.
5. Add Tier 3 fixtures and the safe mutation allowlist.
6. Add VM Playwright specs for privileged queries, config write/read,
   filebrowser mutations, Docker compose flows, and terminal streaming.

## Non-Goals For Now

- No full generated route safety manifest.
- No failure for every unclassified generated route.
- No checked-in VM images.
- No local Playwright suite that creates or deletes host users.
- No automated destructive route tests.
