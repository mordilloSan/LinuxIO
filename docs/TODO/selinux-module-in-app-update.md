# SELinux module in in-app updates

## Status

Not built; nothing below exists in the code. Decided 2026-11-18: kept for later. It is built when a release first needs to change the module, or when the owner asks; an update always runs the installer of the release it updates to, so the fix can ship in that same release. It removes the Known limits line "An in-app update cannot load a module that is new or changed" in [Process & Systemd Architecture](../process-systemd-architecture.md#known-limits).

Source tags:
- **[repo: path]** read from that file at fb74b5b9.
- **[observed]** the Fedora 45 VM facts of 2026-11-18 in the table below.
- **[memory Mn]** neither read nor seen; each one has a VM check, listed under Verification.

## Problem

- An in-app update runs the target release's installer in `linuxio-updater-<n>.service`. That is a transient unit with `ProtectSystem=full`, and its `ReadWritePaths=` come from `updaterWritablePaths` of the bridge installed *before* the update [repo: backend/bridge/handlers/appupdate/appupdate.go].
- Where `/etc/selinux` is not writable, `load_selinux_module` skips a new or changed module, prints a notice and keeps the old one [repo: packaging/scripts/install-linuxio-binaries.sh].
- For a release install, that notice tells the admin to run `curl … -o linuxio.cil && semodule -i linuxio.cil`. That is a root load of a file from `main` with no content check [repo: `selinux_module_load_command`].
- Result [repo: docs/process-systemd-architecture.md, Known limits]:
  - a host updated in-app while SELinux was permissive or disabled gets no module;
  - a module change reaches a host only through a shell install;
  - a binary that needs a rule the old module lacks is refused sign-in until then.

## Facts the design rests on

| # | Fact | Source |
|---|---|---|
| F1 | In the updater's sandbox `semodule -i` fails with "Read-only file system" on `/etc/selinux`; the policy store and the loaded policy stay exactly as they were | observed |
| F2 | `restorecon /usr/local/bin/linuxio-auth` works in the sandbox | observed |
| F3 | With `/etc/selinux` in `ReadWritePaths`, every such unit fails to start with status 226, because the policy refuses `init_t` the bind mount (`mounton` on `selinux_config_t`). Any mount on that folder is ruled out | observed |
| F4 | `semodule -i` takes about 5 s. `semodule -E linuxio` returns the stored CIL byte for byte. The installer loads only a file that passes `selinux_module_valid` and differs from the store's copy | observed; repo |
| F5 | A module removed while the binary carries its type leaves the binary `unlabeled_t`, which systemd cannot run | observed; repo: uninstall.sh |
| F6 | Inside the sandbox the installer already asks systemd to act on units: `systemctl daemon-reload`, and restarts of `linuxio-monitoring.service` and `linuxio-issue.service` (`enable_services`) | repo: install-linuxio-binaries.sh |
| F7 | The updater unit has `PrivateTmp=false`, `NoNewPrivileges=no` and `RuntimeMaxSec=10min`. The installer reaches it on stdin (`/bin/bash -s`) | repo: appupdate.go |
| F8 | The updater runs the *target* release's installer, checked against that release's `.sha256`. Binaries are checked against `SHA256SUMS`. Packaging assets, the module included, come from `main` with no checksum | repo: appupdate.go, install-linuxio-binaries.sh |
| F9 | Every bridge in the field gives that sandbox. `/usr/share/linuxio` and `/etc/systemd/system` are writable in it, and mandatory destinations stay "under paths supported since v0.27.0" | repo: appupdate.go, installer header |
| F10 | `main()` ignores unknown arguments (`*) shift`) | repo: install-linuxio-binaries.sh |
| F11 | The job host restarts `linuxio.target` 500 ms after the task finished, so the update log ends before the restart | repo: app_update_operation.go |
| F12 | systemd creates the auth listening socket in the domain of `linuxio-auth`'s file label, which must be a `daemon`. The socket keeps its old type until the socket unit restarts | repo: docs/process-systemd-architecture.md, SELinux |
| F13 | `control.app_update` itself has never run in the VM | repo: docs/jobs.md |
| F14 | `linuxio-auth@.service` has `Requires=linuxio-auth.socket`. `linuxio-bridge-socket-user.service` has `DefaultDependencies=no` and `Before=linuxio-auth.socket` | repo: packaging/systemd |

## Options

| Option | Who runs `semodule -i` | Domain | systemd protections | Input it trusts | New way to get a module loaded | Helps updates started by bridges in the field | What the admin sees | Size |
|---|---|---|---|---|---|---|---|---|
| A. No sandbox for the whole installer run | Installer bash in `linuxio-updater-<n>` | What `init_t` gives `/bin/bash` [memory M2: `unconfined_service_t`] | None on `/etc` or `/usr` (F3 rules out keeping `ProtectSystem` with `/etc/selinux` writable) | Module fetched by the same run, checked by `selinux_module_valid` | No new trigger, but the whole installer and every unchecked `main` asset get write access to all of `/etc` and `/usr` | No: the sandbox comes from the bridge installed before the update (F9) | `semodule` output in the update log | 1 line + tests |
| B1. The job host starts a second unsandboxed step: the same verified script with a new flag | Bash in a second transient unit | As A | None on `/etc` (F3) | Module fetched again, or a copy left by step 1, checked again | No; but an older target installer ignores the flag (F10) and would run a whole install unsandboxed and without `--defer-restart` | No | Update log (relayed by the job host) | ~80 lines Go + flag |
| B2. The job host starts a shipped oneshot running a `linuxio-worker` word | `linuxio-worker` → `semodule` | `unconfined_service_t` [repo docs show LinuxIO's services there; M2] | No `ProtectSystem` (F3); others possible | Copy the installer put under `/usr/share/linuxio`, checked again by a Go port of the validator | Starting the unit takes root or polkit's manage-units [memory M10]; it loads only the shipped form, from a root-only path | No | Update log | ~600 lines |
| C. A shipped oneshot `linuxio-selinux.service` (`linuxio-worker selinux-module`) at every start of `linuxio.target`, before `linuxio-auth.socket` | As B2 | As B2 | As B2, plus `DefaultDependencies=no` [memory M11] | As B2 | As B2 | Yes: the installer writes the file (F9), and the old bridge's restart starts the unit | **Not** the update log (the load runs after the task ended, F11). Journal, a failed unit in `linuxio status`, an alert | ~600 lines; `semodule -E` on the socket's start path at every boot and restart |
| C′. `ExecStartPre=-` on `linuxio-auth.socket` | As B2 | Unclear for a socket's commands [memory] | — | As B2 | As B2 | Yes, at the restart | Hidden by `-`. The installer cannot run it mid-update: stopping the socket stops every `linuxio-auth@` instance (F14), so every session ends | Rejected |
| **D. The installer loads through a transient unit it starts itself (`systemd-run --wait --pipe`), with the validated file as that unit's stdin** | Bash in `linuxio-selinux-load-<n>.service` | The same exec as the updater (`/bin/bash` under `init_t`), the domain where F1 saw `semodule` reach the store write [M2] | None (see Design) | Exactly the file `selinux_module_valid` passed in the same run, on stdin, not a path | No: only the installer starts it, as root, and only a privileged route or root starts the installer. No unit others can start, no socket, no route | **Yes**: the change is in the target release's installer (F8). `systemd-run` writes nothing on disk, and the unit is outside the sandbox (F6, M1) | Update log, as from a shell: `semodule`'s messages, then OK or NOTICE | ~40 lines installer, ~120 fixture lines, ~80 lines of root test |
| E. D now, C later | — | — | — | — | — | — | — | — |

Also rejected:
- `/etc/selinux` through `ReadWritePaths=` or `BindPaths=`: F3.
- `semodule` run by the job host process itself: `linuxio-jobs@.service` has `NoNewPrivileges=yes`, which would block a transition into `semanage_t` [memory M3], it needs a validator of its own, and only bridges built with it would have it.
- A `.path` unit on the staged file: asynchronous, so the result is not in the update log.

## Decision (recommended): D

- Fixes the root cause (the sandbox) for every update to a release that carries it, whichever bridge starts it. Neither A nor B does that.
- Puts the result in the update log, which C does not.
- Adds no unit, no `linuxio-worker` word and no boot-path step. Respects AGENTS.md: no new socket; the load runs inside an existing root action, the installer. It runs a system tool, not a LinuxIO word for a timer, hook or drop-in.
- The sandbox was not a containment boundary against the installer. It already writes units to `/etc/systemd/system`, binaries that systemd runs as root, and asks systemd to start units (F6). D adds one fixed, checked step outside it.

## Design

### Order in `main`

| # | Step | Change |
|---|---|---|
| 1 | Download and verify (or local check / `--skip-binaries` check) | none |
| 2 | `install_needrestart_override` | none |
| 3 | `install_selinux_module`: fetch, check, compare, load | moved here, after verification and before any binary is replaced; top level, so it also runs with `--skip-binaries` |
| 4 | Step 3/5: licenses, `install_binaries` | none |
| 5 | `relabel_linuxio_auth` (`restorecon`) | split out of `install_selinux_module`; top level, right after the binaries, nothing that can end the install in between |
| 6 | components, saved units, config, PAM, units, enable, restart or defer | none |

### `install_selinux_module` (RHEL family only, as today)

1. `semodule -l` fails: notice if `selinuxenabled`, otherwise silent, as today.
2. Fetch into `$STAGING/linuxio.cil`, then `selinux_module_valid`. A fetch that fails, or a file that fails the check, leaves the store as it is and prints a NOTICE.
3. Read linuxio's state from `semodule --list-modules=full` [M6]:
   - disabled: NOTICE, no load;
   - present at a priority above 400: NOTICE, no load;
   - otherwise: `semodule -E` compare (as today); equal: OK "up to date".
4. Load:
   ```bash
   systemd-run --unit="linuxio-selinux-load-${EPOCHSECONDS}-$$" --description="LinuxIO SELinux module load" \
     --quiet --collect --wait --pipe -p Type=exec -p RuntimeMaxSec=5min \
     /bin/bash -c 'd=$(mktemp -d) && trap "rm -rf \"$d\"" EXIT && cat >"$d/linuxio.cil" && semodule -i "$d/linuxio.cil"' \
     <"$staged"
   ```
   - The stdin is the staged file, never the installer's own stdin, which holds the rest of the script (F7).
   - `--pipe` brings `semodule`'s messages into the update log [M8].
   - The file name stays `linuxio.cil`, so the module stays named `linuxio`.
5. `semodule -E linuxio` again and compare with `$STAGING/linuxio.cil`. Equal: OK "SELinux module linuxio loaded". Different, or step 4 failed: NOTICE "did not load". The result does not rely on the exit status of `systemd-run --wait` [M4].
6. Removed: `SELINUX_DIR`, the `-w` test, its `main` argument, and `selinux_module_load_command`'s `curl … && semodule -i`. Notices now name the checked path:
   - release: `curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash -s -- <version>`;
   - `--local`: `sudo packaging/scripts/localinstall.sh`.

The load unit gets no sandbox:
- No `ProtectSystem`, because of F3.
- `PrivateNetwork`, `ProtectHome` and similar do not guard the policy store, and each is one more way to fail at start (the 226 lesson).
- `NoNewPrivileges` stays `no`, so a transition into `semanage_t`, if the policy has one, is not refused [M3].

### `selinux_module_valid`: required lines

Today a file passes as long as each line has an allowed form and the constraint is there. A file without the types, the file context or `daemon` would pass, and loading it would leave the binary `unlabeled_t` (F5) or the socket unable to listen (F12).

Add: the file must also contain
- `(type linuxio_auth_exec_t)` and `(type linuxio_auth_t)`;
- `(roletype system_r linuxio_auth_t)`;
- `(typeattributeset domain (linuxio_auth_t))` and `(typeattributeset daemon (linuxio_auth_t))`;
- the `entrypoint` allow on `linuxio_auth_exec_t`;
- the `typetransition` line;
- the `filecon` of `${BIN_DIR}/linuxio-auth`;
- the constraint (already required).

A file missing any of them is refused like a foreign line.

### Messages in the update log (also on a terminal)

| Case | Line |
|---|---|
| Same as the store | `[  OK  ] SELinux module linuxio is up to date` |
| Loaded | `[ INFO ] Loading the SELinux module linuxio...`, then `[  OK  ] SELinux module linuxio loaded` |
| Fetch failed / not of the shipped form | As today, without the `curl … semodule -i` remedy |
| Disabled by an admin | `[NOTICE] The SELinux module linuxio is disabled, so it was left as it is; sign-in is refused while SELinux enforces. semodule -e linuxio enables it` |
| Higher priority | `[NOTICE] An SELinux module linuxio at priority N overrides LinuxIO's, so it was left as it is` |
| Did not load | `[NOTICE] The SELinux module linuxio did not load (the message above says why). A module loaded before stays; without one, sign-in is refused while SELinux enforces. To try again, run as root: <installer command>` |

## Trust: the module's path from GitHub to the policy store (D)

| Step | Who | Where the bytes are | Who may write there | Check |
|---|---|---|---|---|
| 1 | Repository | `raw.githubusercontent.com/…/main/packaging/selinux/linuxio.cil` | repository writers | none (F8) |
| 2 | Installer (`curl`), root; in-app inside `linuxio-updater-<n>` | `$STAGING/linuxio.cil`, `mktemp -d /tmp/linuxio-install.XXXXXXXXXX`, mode 0700, root, the host's `/tmp` (F7) | root | — |
| 3 | Installer | same | root | `selinux_module_valid`, form and required lines |
| 4 | Installer | `$STAGING/selinux-loaded/linuxio.cil` from `semodule -E` | root | `cmp`: equal ends here |
| 5 | Installer → PID 1 (`systemd-run`) | the checked file as the load unit's stdin | — | — |
| 6 | Load unit's bash, root | a fresh `mktemp -d`, mode 0700, `linuxio.cil` | root | the same bytes as step 3 |
| 7 | `semodule -i` | `/var/lib/selinux/targeted`, `/etc/selinux/targeted`, kernel | root | compiles; refuses unknown names |
| 8 | Installer | `semodule -E` | root | `cmp` equal = loaded |

Other options:
- **A:** steps 1–4 and 7, in the updater's own unit.
- **B1:** 1–4, then a second fetch or a placed copy, checked again by bash.
- **B2 and C:** 1–3, then `/usr/share/linuxio/selinux/linuxio.cil` (mode 0644, root, writable in every sandbox, F9), at rest until the worker reads it, checks it in Go and loads exactly the bytes it checked.

In every option the content check sits in front of the load.

## Failure behaviour (D)

| Case | Store | `linuxio-auth` label | Auth socket | Update log |
|---|---|---|---|---|
| Fetch fails | unchanged | relabel per the current store | restarted at the end, as today | NOTICE |
| Not of the shipped form or missing a required line | unchanged | as above | as above | NOTICE |
| Disabled by an admin | unchanged, stays disabled | `bin_t` after relabel [M6] | listens: `bin_t` runs as `unconfined_service_t`, as before the module [repo docs]; sign-in refused while enforcing | NOTICE |
| A `linuxio` at a higher priority | unchanged | per that module | per that module | NOTICE |
| Store unreadable, SELinux on | unchanged | relabel | as today | NOTICE "not checked" (as today) |
| `systemd-run` cannot start the unit | unchanged | relabel | as today | its message + NOTICE |
| `semodule -i` fails (compile, lock, disk) | unchanged [observed for read-only, F1; M5] | relabel | as today | `semodule`'s message + NOTICE |
| `-i` "succeeds" but `-E` differs | whatever `semodule` did | relabel | as today | NOTICE |
| Update cancelled during the load | the load unit runs to its end, leaving A or B [M5] | the next install relabels | as today | the update ends canceled |
| `restorecon` fails | — | `bin_t` | listens | NOTICE "Failed to label" (as today) |
| Module B loaded | B | `linuxio_auth_exec_t` (required `filecon`) | recreated in `linuxio_auth_t` at the restart (required `daemon`) | OK |

Invariants:
- The installer never runs `semodule -r` or `-d`.
- Every module it loads declares the binary's type, its file context and `daemon`, so it never leaves `linuxio-auth` `unlabeled_t` (F5) or the socket unable to listen (F12).
- On any failure the module loaded before stays (F1, M5), so sign-in is never more permissive than before.
- A failed load does not fail the update: the binaries are already replaced, and the dialog opens its output when the update finishes.
- No alert is raised. The admin is watching the log, and the `linuxio-worker` the installer could call before step 4 is the previous release's.

## Order relative to the binaries, the relabel and the restart

| Time | Step | Binary | Store | Sign-in |
|---|---|---|---|---|
| 0 | Release verified | A | A | A with A |
| 1 | Module B loaded (~5 s) | A | B | A with B: works if B keeps what A needs |
| 2 | `install_binaries` | B, `bin_t` | B | refused for a few seconds |
| 3 | Relabel | B, `linuxio_auth_exec_t` | B | B with B, already including any rule B needs |
| 4 | Config, PAM, units | B | B | B with B |
| 5 | Restart of `linuxio.target` (job host, or the installer from a shell) | B | B | socket recreated in B's `linuxio_auth_t` |

- **Today:** binaries, then fetch, compare and load, then relabel. The `bin_t` window included the fetch and the 5 s load, and a new binary ran without its new rules until the load.
- **Rule this order needs:** for the seconds of step 1, the previous release's `linuxio-auth` runs with the new module, so a module change keeps what the previous release's `linuxio-auth` needs (one line in AGENTS.md).
- **A failed load at step 1:** binary B runs with module A. Sign-in is refused at the step that needs B's rule, never more allowed; NOTICE.
- **Module removed by an admin:** the next install, in-app or from a shell, loads it again (as today). Until then the binary is `unlabeled_t` (F5), and the auth socket fails at its next start [memory]. C would repair this at the next start; D does not.
- **Module disabled by an admin:** left disabled (see the failure table).

## SELinux switched on later

D covers it with no extra mechanism:
- **Permissive:** `semodule` works, so the in-app update loads the module and relabels. `setenforce 1` then signs in.
- **Disabled:** `semodule` works on the store [repo docs: "On a host with SELinux disabled `semodule` works on the store"; M7], so the module is loaded. Switching SELinux on requires a relabel, which gives the binary its type [repo docs].
- **Remaining case:** a store that `semodule` cannot reach while SELinux is disabled gets no module, and sign-in is refused after SELinux is switched on until the installer runs again. One Known-limits line; C would cover it.

## Verification

### Memory claims and their checks

| # | Claim | Check |
|---|---|---|
| M1 | A unit that the sandboxed updater asks systemd to start is not sandboxed: its `/etc/selinux` is writable | S1 |
| M2 | systemd runs `/bin/bash` (the updater, the load unit) as `unconfined_service_t` | S1 records `id -Z` in both units |
| M3 | `semodule` may transition to `semanage_t`; `NoNewPrivileges=yes` would refuse that | S1: `ps -eZ` during the load |
| M4 | `systemd-run --wait` exits non-zero when the command fails | S3 (D does not rely on it) |
| M5 | A failed or killed `semodule -i` leaves the store and the loaded policy as they were | S3, S4, V2 |
| M6 | `--list-modules=full` prints priority, name, language and `disabled`; how `-E` and `-i` treat a disabled module | S6 |
| M7 | With SELinux disabled, `semodule -l` and `-i` work on the store | S5 (optional) |
| M8 | `systemd-run --pipe` hands the unit its stdin, and the unit's output reaches the update log when nested in the updater | S1, V2 |
| M9 | Go reads the CA bundle once per process | V2 sets up the release mirror before sign-in and before the root job host starts |
| M10–M12 | C only: polkit default for manage-units; a socket unit's implicit `Before=sockets.target` makes a service with default dependencies ordered `Before=linuxio-auth.socket` a cycle (F14 hints at it); a failed oneshot pulled in by `Wants=` and ordered `Before=` does not hold the socket back | only if C is chosen |

### Phase 0: VM spikes

On Fedora, by hand: `make vm-ssh DISTRO=fedora VM_KEEP=1`, then `make vm-reset DISTRO=fedora`.
- **S1:**
  1. Run a script in a unit with the updater's exact properties (copied from `buildInstallCommandArgs`). It prints `id -Z`, then runs the `systemd-run` load from Design, fed the shipped CIL plus one comment line.
  2. Expect: success; `semodule -E linuxio` equal to that file; no denial (`ausearch -m AVC,USER_AVC -ts <marker>`, or `journalctl _TRANSPORT=audit` where auditd is absent).
  3. Repeat from a root shell.
- **S3:** the same with a CIL holding `(allow linuxio_auth_t e2e_no_such_t (file (read)))`, which passes the form check but does not compile. Record `systemd-run`'s exit status and that `-E` is unchanged.
- **S4:** start a load and `systemctl kill` the load unit after 1 s. `-E` must give A or B, and `semodule -l` must still work.
- **S5 (optional):** boot with `selinux=0`, then run `semodule -l` and `-i`.
- **S6:** `semodule -d linuxio`, then `--list-modules=full`, `-E`, `-i` of a changed file, `--list-modules=full` again, `semodule -e linuxio`.

### Installer fixtures (`packaging/scripts/tests/installer-fixtures.sh`)

- A stand-in `systemd-run` that logs its options and runs the command after them, with its stdin. The tools folder gains `mktemp`.
- **Cases:**
  - load through `systemd-run` with `--wait --pipe --collect`, no `ProtectSystem`, and stdin equal to the staged file;
  - `-E` after the load; a load whose `-E` still differs → NOTICE;
  - `systemd-run` fails → NOTICE;
  - disabled → no load, NOTICE; higher priority → no load, NOTICE;
  - required line missing (no `daemon`, no binary `filecon`, no type) → refused;
  - no notice prints `semodule -i`;
  - the Debian family: no `systemd-run`, `semodule` or `restorecon` calls.
- **Removed:** the case "with the SELinux folder read-only" and the `DIR` parameter.
- **`main` order assertions:**
  - `install_selinux_module` comes after the verification block and `install_needrestart_override`, and before `install_binaries`;
  - `relabel_linuxio_auth` sits at top level right after the binaries block, with no `Show 1` in between;
  - both come before `install_systemd_files` and the restart.

### Root test in the updater suite

- `TestSELinuxModuleLoadsThroughUpdaterSandbox` in `backend/bridge/handlers/appupdate/appupdate_integration_test.go`.
- **Gate:** runs only with `LINUXIO_RUN_SELINUX_INTEGRATION=1` and `semodule` present. Only the VM's Fedora updater suite sets that, so a developer's own policy is never touched.
- **What it does:**
  1. Starts the updater's exact `systemd-run` arguments with a script that sources the installer and calls `install_selinux_module` against a checkout holding a changed `linuxio.cil`.
  2. Asserts `semodule -E linuxio` equals that file.
- **Also:** `UPDATER_RUN` in `mk/quality.mk` names it; `suite_updater` pushes the CIL and sets the variable on Fedora.

### Real in-app update A → B (Phase 2)

- **Release mirror.** A rig command `release-mirror DIR` with `release-mirror.py` (new, beside the rig script).
  - Generates a CA and a certificate for `github.com`, `api.github.com` and `raw.githubusercontent.com`.
  - Trusts the CA: Fedora `update-ca-trust`, Ubuntu `update-ca-certificates`.
  - Points the three names at `127.0.0.1` and `::1` in `/etc/hosts`.
  - Serves, as root on 443 in a transient unit: `releases/latest`, `releases/download/<tag>/<file>` and `main/packaging/<path>`.
  - GitHub's redirect to `objects.githubusercontent.com` is not reproduced.
- **`make vm-release-builds`.** Copies the plain build and rebuilds its Go binaries with `GIT_VERSION=<tag>`, as `vm-update-build` does with an overlay, so the webserver reports the tag the page waits for. Per tag it writes the installer, its `.sha256`, `SHA256SUMS`, `LICENSE` and `THIRD_PARTY_NOTICES.md`.
  - Tags: v0.54.98 (served `main` module = shipped plus `(allow linuxio_auth_t e2e_no_such_t (file (read)))`) and v0.54.99 (module B = shipped plus `; e2e: module B`).
  - Both pass `release_version_supported` and `validReleaseVersionRE`.
- **Spec.** `app-update.spec.ts` (new, beside the other VM specs), Playwright project `app-update` (ignored by `specs`), on a VM reverted to clean:
  1. Mirror up, then a rig install of A, then sign in. On Fedora, record `semodule -E` = A, `ls -Z` of the binary, `ss -xlZ` of the auth socket, and an audit marker.
  2. Update dialog → v0.54.98. The log shows the NOTICE "did not load"; after the restart, sign in; store still A; reboot; sign in.
  3. Serve v0.54.99 as latest → update. The log shows "SELinux module linuxio loaded"; sign in; store = B; binary `linuxio_auth_exec_t`; socket `linuxio_auth_t`; no denial since the marker. Reboot; sign in; check the contexts again.
  4. On Ubuntu, the same two updates. No SELinux line in the log, no `linuxio-selinux-load` unit in the journal, no failed `linuxio` unit, sign-in before, after and after reboot.
- **Permissive, by hand on Fedora:** `setenforce 0`, `semodule -r linuxio`, rig install, store = A, `setenforce 1`, `login.spec.ts` passes; reboot; it passes again.

All of these run as the installer puts things and again after a reboot (AGENTS.md).

## Docs and AGENTS.md

- **`docs/process-systemd-architecture.md`:**
  - Build & Install, the SELinux bullet: loaded through a transient unit.
  - The updater-sandbox paragraph in "Login and jobs", after needrestart: one sentence that the module load is the one step that runs outside the sandbox, and why (F3).
  - SELinux → "Install and uninstall", steps 1–4 rewritten: load after verification and before the binaries, through `systemd-run` with the checked file on stdin, `-E` check, disabled and higher priority left alone, relabel right after the binaries.
  - Known limits: delete the in-app bullet. Add one bullet each for: a disabled module left alone; a module an admin removed comes back only at the next install; a store unreachable while SELinux is disabled.
  - File Locations, SELinux row: updated.
- **`docs/jobs.md`**, "Covered at one level only": `control.app_update` is now run by `app-update.spec.ts` on Fedora and Ubuntu.
- **`docs/development.md`**, Test VM: the release mirror, `vm-release-builds`, the `app-update` project, and the Fedora results (updater suite, `app-update.spec.ts`).
- **`docs/TODO/e2e-testing.md`**, Distributions: rows for the runs.
- **`docs/TODO/backlog.md`**, Rejected:
  - `/etc/selinux` in the updater's `ReadWritePaths` (226, `mounton` on `selinux_config_t`);
  - the updater without `ProtectSystem` for the whole run (A);
  - a unit that loads the module at every start (C), if declined.
- **`docs/TODO/packaging.md`**, the RPM line: the package loads the module from its own scripts and does not use the installer's load unit.
- **`docs/TODO/README.md`:** link this plan; remove the link when it lands.
- **AGENTS.md, line 22:** "…and the installer loads the module and relabels the binary after every replace." becomes "…and the installer loads the module only after `selinux_module_valid` passes, before it replaces the binaries and through a transient unit of its own (the in-app updater's sandbox cannot be given `/etc/selinux`), and relabels the binary right after every replace; a module change keeps what the previous release's `linuxio-auth` needs." The rest of the line is unchanged.

## Phases

| Phase | What | Files | Lines | VM runs | Make targets |
|---|---|---|---|---|---|
| 0 | S1, S3, S4, S6 (S5 optional) | none | 0 | 1 Fedora session (~45 min) | `vm-ssh`, `vm-reset` |
| 1 | Installer, fixtures, root test | `install-linuxio-binaries.sh`, `installer-fixtures.sh`, `appupdate_integration_test.go`, `mk/quality.mk`, `linuxio-vm.sh` (`suite_updater`) | ~+60/−45, ~+120/−40, ~+80, 1, ~6 | `test-vm-backend SUITES=updater` on Fedora and on Ubuntu; Fedora install + reboot + `login.spec.ts`; Ubuntu the same | `test-installation-scripts`, `check-backend-quiet`, shellcheck, shfmt |
| 2 | Release mirror and the real in-app spec | `release-mirror.py`, `linuxio-vm.sh`, `mk/vm.mk`, `playwright.vm.config.ts`, `app-update.spec.ts`, `vm.ts` | ~70, ~90, ~35, ~8, ~280, ~30 | Fedora spec (~25 min), Ubuntu spec (~20 min), one rerun; permissive by hand | `check-frontend-quiet`, `test-vm-browser-rerun PLAYWRIGHT_ARGS=--project=app-update` |
| 3 | Docs, AGENTS.md | 9 files | ~120 | — | `check-docs` |
| (4) | C, only if chosen | unit, `linuxio.target`, worker word and test, installer unit list, CLI `journal.go`, docs | ~600 | +3 | — |

Total for 0–3: about 15 files and 900 lines, half of them tests and rig; 6–8 VM runs.

## Risks

| Risk | What happens | Mitigation |
|---|---|---|
| S1 fails: the updater's context cannot start a transient unit, or the load unit cannot reach the store | D does not work | Phase 0 runs before any code; fall back to B2 for future bridges plus C for convergence |
| The load unit reads the installer's stdin | It eats the rest of the script (F7) | stdin is always `<"$staged"`; a fixture asserts it |
| `main` changes the module's form | The target release's validator refuses it, and that release keeps the old module (as today) | A form change ships with its validator in a release before `main` uses it |
| Load before the binaries, then the install stops | Old binaries run with the new module | The additive rule |
| The mirror differs from GitHub (no redirects, no rate limits) | Those paths are not exercised | `curl -L` and Go's client follow redirects; noted in the spec header |
| An installer run without systemd (chroot, container) | The module no longer loads (no direct `semodule`) | NOTICE; LinuxIO needs systemd |
| `--list-modules=full` format differs (M6) | Disabled or overridden is misread | S6 fixes the parser before Phase 1 |

## Decisions for the owner

1. **Mechanism:** recommend D, the installer loading through a transient unit it starts itself.
2. **Start-time unit (C):** recommend not now; revisit with the package work.
3. **Load before replacing the binaries:** recommend yes, with the additive-module line in AGENTS.md.
4. **A failed load:** recommend it does not fail the update; it prints a NOTICE.
5. **Admin-disabled module:** recommend leaving it disabled, with a NOTICE.
6. **Admin-removed module:** recommend loading it again at the next install, as today.
7. **Required lines in `selinux_module_valid`:** recommend yes.
8. **Notice remedy:** recommend naming the installer, not `curl … && semodule -i`.
9. **Load unit:** recommend no sandbox properties, and `NoNewPrivileges` left at no.
10. **Release mirror and `app-update.spec.ts`:** recommend building them; they also run `control.app_update` for the first time.
11. **Permissive and disabled checks:** recommend permissive by hand once; disabled only if S5 is cheap.
12. **No direct `semodule` fallback without systemd:** recommend none.
13. **Alert on a failed load:** recommend none for D.
14. **Release:** recommend v0.54.0, the first release with the module, so no Fedora host updates in-app to a module-carrying release without it.
