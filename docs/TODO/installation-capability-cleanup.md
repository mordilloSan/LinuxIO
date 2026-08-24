# Installation and Capability Cleanup Plan

## Status

Implemented and verified. Focused packaging, backend, and frontend validation
passes, and the integrated `make test-quiet` target passed on 2026-08-24.

This plan removes obsolete development infrastructure, narrows the public
dependency bootstrap, improves capability installation feedback, and reconciles
the production and local installation paths without removing the recovery and
development workflows they intentionally support.

## Decisions and Constraints

The following decisions are settled for this work:

- Remove `packaging/scripts/dev-test-update.sh` and the port-9999 development
  script server. The frontend caller disappeared when application updates moved
  into the backend; the script and server are now orphaned.
- Reduce `packaging/scripts/install-dependencies.sh` to the dependencies needed
  to bootstrap LinuxIO: PAM, Polkit, and PackageKit. Docker uses its separate
  upstream convenience installer and is not an option in this script.
- PackageKit is a hard prerequisite. LinuxIO uses it to install optional distro
  packages from the Capability Manager, so a missing or failed PackageKit
  installation must fail clearly instead of continuing with a notice.
- Optional capabilities are installed from LinuxIO's Capability Manager, not
  from the bootstrap script.
- Docker remains installed through its separate convenience script. This plan
  does not add Docker to the Capability Manager or fold Docker installation
  into the dependency bootstrap.
- Installing `lm-sensors` from the Capability Manager must also run
  `sensors-detect --auto` and expose that command's output.
- Installing Avahi from the Capability Manager must also install
  `libnss-mdns` on Debian-family systems. On RHEL-family systems, `nss-mdns`
  is best-effort because it may require EPEL; failure must warn without
  preventing the Avahi responder from being enabled, started, or re-detected.
- Every capability installation opens a progress dialog with a raw-output view,
  following the interaction used by Docker Compose operations and filesystem
  indexing.
- Keep `packaging/scripts/localinstall.sh`. Its purpose is to deploy temporary
  development builds into a real production-like host for testing.
- Fix local installation's hard-coded port behavior, but do not remove the
  production installer's available-port selection.
- Preserve the production installer's use of current `main` packaging assets.
  This is a deliberate recovery mechanism for fixing packaging or systemd bugs
  in immutable historical releases.
- Do not move substantive script bodies into the Makefile merely to reduce the
  visible script count.

## Current Problems

### Orphaned development updater

`make dev` still starts a Python HTTP server for `packaging/scripts/` on port
9999 and tracks it with `.script-server.pid`. The frontend no longer fetches
`dev-test-update.sh`, so this adds an unused process, configuration surface, PID
cleanup, documentation, and an obsolete simulated updater.

### Bootstrap and Capability Manager duplicate optional setup

The dependency bootstrap owns an interactive catalogue for lm-sensors,
smartmontools, NFS, TuneD, Indexer, Avahi, and other optional facilities. The
Capability Manager already detects and installs these facilities through the
backend capability registry. Keeping two catalogues creates drift in package
names, service activation, progress reporting, and post-install behavior.

Docker is separate: its supported upstream convenience installer remains
available without being wrapped by the dependency bootstrap.

### Capability installation has no durable user-facing operation dialog

The Capability Manager currently renders installation state inside one row.
Users cannot open a dedicated operation view, inspect ordered installer output,
or retain clear success/failure context comparable to Docker Compose and
filesystem indexing operations.

### Capability post-install steps are incomplete

Installing the lm-sensors package makes the `sensors` binary available but does
not perform the host probe currently done by the bootstrap script. Avahi
installation also omits the NSS integration package installed by the bootstrap
script.

### Local installation reports and assumes port 8090

The production installer searches for an available port, while local install
copies the default socket and reports port 8090. This can overwrite or
misreport an existing non-default LinuxIO socket configuration, which is
especially misleading when localinstall is used against a real host.

### The mutable packaging-asset policy is implicit

Versioned binaries intentionally use current `main` packaging assets so an
installer/systemd/configuration bug can be repaired for already-published,
immutable releases. The behavior is valuable, but its reason and compatibility
contract are not explicit enough and can look like accidental version skew.

## Target Installation Model

~~~text
Public bootstrap script
├── PAM runtime
├── Polkit
└── PackageKit (required)

Separate Docker setup
└── Docker's upstream convenience installer

LinuxIO Capability Manager
├── distro package installation through PackageKit
├── component-specific installers (Indexer, monitoring, ...)
├── capability-specific post-install steps
│   ├── lm-sensors -> sensors-detect --auto
│   └── Avahi -> mDNS NSS integration package
└── progress dialog
    ├── phase and percentage
    ├── success/failure result
    └── ordered raw output

LinuxIO installation paths
├── released binaries + current-main recovery assets
└── local development binaries + repository assets
    └── shared port-selection/preservation policy
~~~

## Phase 1: Remove the Orphaned Development Updater

1. Delete `packaging/scripts/dev-test-update.sh`.
2. Remove `SCRIPT_SERVER_PORT` and `SCRIPT_SERVER_PID` from the Makefile.
3. Remove their validation and cleanup paths.
4. Stop launching `python3 -m http.server` from `make dev`.
5. Update the `make dev` status and stop instructions so they mention only the
   processes that still exist.
6. Remove `.script-server.pid` from `.gitignore`.
7. Remove the script-server variables from `docs/development.md`.
8. Add a source guard or focused test proving the obsolete script server and
   `dev-test-update.sh` references do not return.

### Phase 1 exit criteria

- [x] No current implementation or workflow references `dev-test-update.sh`,
  `SCRIPT_SERVER_PORT`, `SCRIPT_SERVER_PID`, `.script-server.pid`, or the
  port-9999 packaging server; this historical roadmap is the only exception.
- [x] `make dev` starts and cleans up only the current development processes.
- [x] No application-update production or integration path changes.

## Phase 2: Narrow the Dependency Bootstrap

1. Keep distro detection and package-manager helpers needed for Debian and
   RHEL-family hosts.
2. Keep mandatory installation for PAM, Polkit, and PackageKit.
3. Change mandatory dependency handling so PackageKit failure is fatal and
   explains that in-app capability installation cannot work without it.
4. Remove Docker installation and the `--all` option from this script. Keep
   Docker's upstream convenience installer as a separate documented command.
5. Remove the optional dependency arrays, terminal checklist, and installers
   now owned by the Capability Manager.
6. Reject removed or unknown options, including `--all`, instead of silently
   accepting them.
7. Update `README.md`:
   - bootstrap PAM, Polkit, and PackageKit;
   - install Docker separately through its convenience script when needed;
   - install LinuxIO binaries through the existing installer;
   - install other optional capabilities from the Capability Manager after
     signing in.
8. Update the script help text so it exactly matches the reduced behavior.
9. Add fixture coverage for supported distro mappings, mandatory failures,
   non-interactive execution, removal of `--all`, and help output. Expose this
   coverage through a repository Make target.

### Phase 2 exit criteria

- [x] The bootstrap has one package catalogue for mandatory dependencies and
  no second catalogue for capabilities owned by LinuxIO.
- [x] PackageKit installation failure stops the bootstrap with a useful error.
- [x] Docker has a separate supported convenience-script installation path,
  with no Docker option in the dependency bootstrap.
- [x] README commands and script help agree with actual behavior.

## Phase 3: Complete Capability-Specific Installation

### lm-sensors

1. Install the existing distro package through PackageKit.
2. After package installation, resolve `sensors-detect` explicitly.
3. Run `sensors-detect --auto` through the existing context-aware subprocess
   boundary.
4. Stream stdout and stderr while preserving their order as closely as the
   process API allows.
5. Propagate cancellation and return useful command/stderr context on failure.
6. Re-detect the capability only after the post-install command completes.
7. Unit-test success, non-zero exit, missing command, cancellation, and output
   forwarding without probing real host hardware.

### Avahi and NSS integration

1. Extend the Avahi package specification to install the responder and NSS
   integration:
   - Debian family: require `avahi-daemon` and `libnss-mdns`;
   - RHEL family: require `avahi`, then install `nss-mdns` best-effort and warn
     that EPEL may be needed if the package is unavailable.
2. Continue enabling and starting `avahi-daemon.service` through the existing
   systemd abstraction.
3. Continue service activation and re-detection after a non-fatal RHEL
   `nss-mdns` failure.
4. Re-detect Avahi after package and service steps complete.
5. Add registry and install-runner tests for both distro families and the
   optional-package failure path.

### Phase 3 exit criteria

- [x] Installing lm-sensors from LinuxIO runs `sensors-detect --auto` once.
- [x] The command is cancellable and its output reaches the task stream.
- [x] Installing Avahi requires mDNS NSS integration on Debian and attempts it
  with a visible non-fatal warning on RHEL-family systems.
- [x] Capability detection runs after every required package, command, and
  service step.

## Phase 4: Add the Capability Installation Dialog

1. Open the dialog before starting `system.install_capability`, so users see
   immediate feedback even before the first progress event.
2. Show the capability label, current phase, global percentage, and terminal
   success or failure state.
3. Add a collapsible raw-output panel consistent with Docker Compose operation
   output:
   - preserve line order and whitespace;
   - distinguish stderr where the backend provides that information;
   - auto-scroll while expanded;
   - keep output bounded to prevent an unbounded browser allocation.
4. Extend `InstallCapabilityProgress` only as much as necessary to carry
   ordered output records. Keep common Task percentage/phase/message fields
   intact.
5. For PackageKit operations, show its transaction messages and percentages.
   Do not label synthesized PackageKit progress as literal subprocess output.
6. For component installers and post-install commands, relay their actual
   stdout/stderr records.
7. Allow the dialog to close without cancelling the system-owned installation,
   matching existing background Task behavior. Completion feedback and query
   invalidation must still occur through the global Task handler.
8. Define reopen/recovery behavior explicitly:
   - an active capability Task can reopen its progress dialog;
   - dismissing a running dialog remains respected across stream reconnects;
     the row's explicit View action reopens it;
   - bounded output available from Task replay is restored;
   - if historical raw output is unavailable, state that honestly rather than
     fabricating a complete log.
9. Add component tests for open/start ordering, progress, raw output,
   auto-scroll disclosure, close-while-running, success, failure, and blocked
   PackageKit prerequisites.
10. Regenerate the Go-owned frontend API contract with `make generate` if the
    progress detail shape changes. Never edit generated frontend files by hand.

### Phase 4 exit criteria

- [x] Every Capability Manager Install action opens a dedicated dialog.
- [x] Live installer output is visible in a raw-output panel.
- [x] Closing the dialog does not cancel the installation.
- [x] Completion feedback and capability refresh still work after the settings
  panel or dialog closes.
- [x] Recovered tasks never present an incomplete log as complete raw output.

## Phase 5: Reconcile Local and Production Port Handling

Keep localinstall as a first-class developer workflow. Do not replace it with
the release installer or remove its repository-source behavior.

1. Define one port-selection policy used by both paths:
   - preserve a valid port from an existing LinuxIO socket configuration when
     reinstalling;
   - otherwise choose an available port from the supported range;
   - fall back deliberately and report failure when no supported port is
     available.
2. Apply the selected port to the socket file actually installed by
   `localinstall.sh`.
3. Report the selected port in the local-install completion URLs instead of a
   constant 8090.
4. Cover port-only and address-qualified `ListenStream` forms.
5. Add fixture tests that use temporary socket files and injected port probes;
   tests must not mutate the host's systemd configuration.
6. Keep temporary development binaries, repository packaging assets, service
   reload, and restart behavior unchanged except where required by the selected
   port.

### Phase 5 exit criteria

- [x] Local install preserves an existing valid LinuxIO port.
- [x] A fresh local install follows the production available-port policy.
- [x] The installed socket and displayed URLs use the same port.
- [x] `make localinstall` remains suitable for testing temporary development
  binaries on a real host.

## Phase 6: Document and Protect the Recovery-Asset Policy

Do not change the production installer to fetch all packaging assets from an
immutable release tag as part of this plan.

1. Document why released binaries intentionally fetch current `main` systemd,
   PAM, configuration, MOTD, and helper assets.
2. Name the policy explicitly in the installer instead of leaving it as an
   unexplained `RAW_BASE` detail.
3. Define the compatibility obligation for current-main packaging assets:
   they must remain capable of installing and repairing supported historical
   binaries, or fail with a clear minimum-version message.
4. Add a release/integration fixture that verifies the expected asset base and
   prevents an accidental conversion to tag-only assets.
5. Consider an explicit packaging-ref override only if it preserves the
   current-main recovery default and has a demonstrated rollback/debug use.
6. Update installation and updater documentation with this exception to normal
   immutable-release expectations.

### Phase 6 exit criteria

- [x] The current-main recovery behavior remains the default.
- [x] Maintainers can explain which assets are mutable and why.
- [x] Compatibility expectations for old binaries are documented, and a
  fixture protects the current-main asset-base policy.

## Out of Scope

- Removing or replacing `localinstall.sh`.
- Moving LinuxIO to `.deb` or `.rpm` packages.
- Adding Docker installation to the Capability Manager.
- Replacing Docker's convenience installer with manual package-by-package
  installation instructions.
- Forcing production packaging assets to use the binary release tag.
- Moving installer or frontend operation logic into the Makefile solely to
  reduce the number of files.
- Removing the release-note, icon-generation, bundle-metrics,
  compiler-coverage, browser-fixture, or login-banner helpers.

## Tests and Verification

All implementation must use repository Make targets. Add focused Make targets
when packaging fixtures are not already covered; do not invoke underlying Go,
Node, formatting, lint, or test tools directly.

Required verification sequence:

1. Run focused packaging, backend, and frontend targets while iterating.
2. Run `make generate` after any Go-owned API progress-contract change.
3. Finish the integrated change with `make test-quiet` because it spans
   packaging, backend, frontend, and generated contracts.
4. Run `make test-frontend-browser-quiet` when the final claim depends on real
   dialog navigation, chunk loading, focus behavior, or browser lifecycle.
5. Inspect `.cache/test-logs/` after any quiet-target failure before rerunning.
6. Inspect the complete diff and post-test worktree after verification because
   generation, formatting, and lint targets may modify files.

## Execution and Review Model

When implementation resumes:

- Sol owns orchestration, integration, conflicts, final diff review, and final
  decisions.
- Use Luna workers with xhigh reasoning and fast service for bounded, disjoint
  implementation areas.
- Suggested ownership boundaries:
  - bootstrap/Make/docs cleanup;
  - capability backend and tests;
  - capability dialog and frontend tests;
  - installer/localinstall port and recovery-policy fixtures.
- Workers must not edit overlapping files.
- Do not run Make verification concurrently with implementation or another
  Make invocation.
- After implementation is quiescent, use a fresh Luna xhigh test worker to run
  the required final Make target and report the exact command and result.
- Sol reviews the entire final diff and reconciles all worker reports before
  handoff.

## Final Completion Checklist

- [x] Obsolete updater/server infrastructure is gone.
- [x] Bootstrap owns only mandatory prerequisites; Docker setup is separate.
- [x] PackageKit is enforced as a prerequisite.
- [x] Optional capability installation is owned by LinuxIO.
- [x] lm-sensors runs `sensors-detect --auto` with streamed output.
- [x] Avahi installation includes Debian NSS integration and best-effort RHEL
  NSS integration with a visible warning on failure.
- [x] Capability installation has a progress/raw-output dialog.
- [x] Localinstall preserves its development purpose and handles ports
  consistently.
- [x] Current-main packaging assets remain an explicit recovery mechanism.
- [x] README and focused architecture/development docs match the implementation.
- [x] Generated contracts are current.
- [x] Required Make targets pass and their exact results are reported.
