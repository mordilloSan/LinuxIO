# Jobs phase 3: storage safety and btrfs resume — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Storage and Docker jobs leave the host in a known state when they are cancelled, fail or are interrupted, and the four btrfs operations survive a shutdown or reboot.

**Architecture:** Three host-level fixes make cancel and resume trustworthy: a Mutation ends only when its handler returns, with `cancel_requested` written first; the shutdown path flags a resumable job interrupted before its `Suspend` hook runs and records the real outcome; and a job is resumed once, at the first host start after the shutdown or power loss that interrupted it. Blocking btrfs ioctls run on a thread with asynchronous signals blocked and report the kernel's positive refusal codes. Then the three data-safety fixes (remove cancel, pool-create rollback, a Docker storage switch done in one systemd restart behind a clearer warning) and the `Resumable` hooks, built on btrfs-package primitives tested against the fake kernel and, as root, on loop devices.

**Tech Stack:** Go 1.27, `golang.org/x/sys/unix` v0.49 (`PthreadSigmask`, `Tgkill`), btrfs ioctls, systemd D-Bus (`RestartUnit`, the unit `InvocationID` property), React (one confirm dialog's copy).

**Spec:** `docs/TODO/jobs.md` section 4.3, with sections 2.11 (lifecycle, Suspend/Resume table), 2.18 (btrfs suspend and resume tests) and 4.6 (every phase).

## Decisions for review

Each is the plan's recommendation; the user confirms or overrides before Task 1 starts.

- **D1, Docker: decided by the user on 2026-10-29. This replaces the spec's checkpoint.**
  - **The problem.** The route stops Docker and starts it again as two steps of its own. A job that dies in between leaves Docker down: systemd never restarts a deliberately stopped unit, and the route has also stopped `docker.socket`. A checkpoint would not help, because `Resume` runs only after a reboot, when Docker starts by itself anyway.
  - **The decision.** Write the files while Docker still runs; dockerd reads them only at start. Then ask systemd for one restart of `docker.service`. systemd completes that job even if ours dies, so Docker cannot be left down. No checkpoint and no timer.
  - **The warning.** The user asked that people be warned. Stopping every container cuts off anyone who reaches LinuxIO through a reverse proxy, tunnel or VPN running in Docker. That access does not come back by itself, because the proxy's container is hidden on the old storage. Unraid goes further and refuses the change unless Docker is stopped first. The confirm dialog says so (Task 6).
- **D2, balance Suspend pauses on every host stop (spec).** A host stopped outside a shutdown (`systemctl stop linuxio-jobs@0`) leaves the balance paused. A paused balance holds the pool's exclusive operation, so remove, replace and resize are refused until the next mount or `btrfs balance resume`. **Recommended:** keep the spec and document it. Alternative: pause only when systemd's `SystemState` is `stopping`, cancel otherwise.
- **D3, replace is followed, never restarted, on resume (deviates from the spec's "else start it again").** The request names the target by path (`/dev/sdc`), and paths are not stable across reboots; a restart would write to whichever blank disk now has that name. The kernel tracks a replace it resumes by device id, so following it is safe. **Recommended:** Resume follows a replace the kernel resumed (power loss) and otherwise fails, so the host alerts "start the replace again".
- **D4, a resumable job gets one resume (tightens the spec's "once per boot").** Today a job a crash interrupted, or a host stopped by hand, is resumed at the *next* reboot, possibly weeks later: `ReconcileCrashed` and `markLost` keep the dead executor's boot ID. **Recommended:** resume only `shutdown`/`reboot` records of an earlier boot without `cancel_requested`, and clear `Resumable` on every other interrupted record at host start (Task 3). There is no manual resume: Task 12 removes `jobs submit --resume` and the `jobs.resume` call.
- **D5, testing: the direction was decided by the user on 2026-10-29; the VM needs their go-ahead before it is created.** The agent runs the tests itself, in a disposable VM it controls (Task 0.1). The user is in the `libvirt` group, so the agent can create and drive a VM on `qemu:///system` without sudo, and it is root inside the VM. This is Tier 3 of `docs/TODO/e2e-testing.md`, started now. The phase 2 checks run there before phase 3 begins (Task 0.2). The root btrfs tests and the phase 3 kill, reboot and power-loss checks run there too (`virsh destroy` is a power cut). The VM runs the same Ubuntu release as the development host (26.04, kernel 6.19 or later), so the signal behaviour in the facts below is real there.
- **D6, the jobs CLI goes: decided by the user on 2026-10-29.** `linuxio-bridge jobs submit` (with `--route`, `--request`, `--wait` and `--resume`) and the host's `jobs.resume` call exist only for testing. Task 12 removes them, after the VM checks have replaced them. Phase 4 then designs its own way for timers to reach the job store (`docs/TODO/jobs.md` 4.1).
- **Out of scope** (listed so they are not lost):
  - a "canceling" state in the UI; `cancel_requested` stays backend-only, and the snapshot stays `running` until the handler returns;
  - periodic scrub checkpoints, so a power loss rescans from 0;
  - progress for device removal.

## Upstream facts the plan relies on

Sources: kernel = torvalds/linux master (2026-10-28); progs = btrfs-progs master. Versions come from the release tags. An independent review re-checked the ones marked ✓ against the source.

**Device remove**
- **Cancel call.** `BTRFS_IOC_RM_DEV_V2` with `name="cancel"` and `flags=0` cancels a running removal. A set `BTRFS_DEVICE_SPEC_BY_ID` disables the cancel. Kernel 5.14 (`67ae34b69c41`). Source: `ioctl.c` `btrfs_ioctl_rm_dev_v2`.
- **Return values ✓.** The canceller gets `-ECANCELED` when the current exclusive operation is a removal, else `-ENOTCONN`. A cancel arriving before the removal claimed the exclusive operation is therefore lost. A cancel arriving after relocation ended leaves `reloc_cancel_req` set, and that cancels the *next* relocation at its start. Source: `ioctl.c` `exclop_start_or_cancel_reloc`, `relocation.c` `reloc_chunk_start`.
- **State afterwards.** A cancelled, killed or power-lost removal leaves the device a member and moved chunks moved. Nothing resumes it at mount, and re-issuing it skips the moved chunks. Source: `volumes.c` `btrfs_rm_device`, `btrfs_shrink_device`.

**Balance**
- **Pause and cancel.** On pause, the in-flight `BALANCE_V2` returns `-ECANCELED` with `PAUSE_REQ` set in the returned state. A cancel sets `CANCEL_REQ` instead. Source: `volumes.c` `btrfs_balance`, `btrfs_pause_balance`, `btrfs_cancel_balance`, `btrfs_recover_balance`, `btrfs_resume_balance_async`; progs `cmds/balance.c`.
- **Persistence and mount.** The balance item survives a pause. An rw mount resumes it in kthread `btrfs-balance` unless mounted with `skip_balance`, and an unmount pauses a running balance.
- **Resuming.** `BALANCE_V2` with `flags = BTRFS_BALANCE_RESUME (1<<4)` resumes a paused balance. It returns `-EINPROGRESS` if one is running and `-ENOTCONN` if there is none.
- **Reading the state.** `BALANCE_PROGRESS` returns `-ENOTCONN` for none, sets the `RUNNING` bit while running, and returns state 0 while paused.
- **Cancel destroys.** A fatal signal cancels the balance and deletes its item (5.9). `BALANCE_CTL cancel` on a *paused* balance deletes it too.

**Scrub**
- **No kernel state.** The kernel keeps no scrub state.
- **Resuming from an offset.** `SCRUB_PROGRESS` returns the live `last_physical`, or `-ENOTCONN` when that device is idle, whether it finished, failed or never started. Restarting at `start = last_physical` is safe: whole dev extents below it are skipped and the one straddling it is rescanned. btrfs-progs `scrub resume` does exactly this. Source: `scrub.c` `btrfs_scrub_progress`, `scrub_enumerate_chunks`; progs `cmds/scrub.c`.
- **Cancel is filesystem-wide.** `SCRUB_CANCEL` also cancels a running device replace. Source: `scrub.c` `btrfs_scrub_cancel`; `dev-replace.c` `btrfs_dev_replace_cancel`.

**Signals (6.19 and later) ✓**
- **Any signal aborts.** Since 6.19 (`937f99c73613`), any pending signal on the ioctl thread aborts a scrub or a device replace with `-EINTR`, and so does a freeze. An aborted replace ends `CANCELED` with its target destroyed. Relocation waits (`wait_on_bit`, `TASK_INTERRUPTIBLE`) also end on any signal. Source: `scrub.c` `should_cancel_scrub`.
- **Why it hits Go.** Go handles every signal (SIGCHLD, SIGURG, ...), so a child exit in another job can land on our thread.
- **This host runs 7.0.**

**Device replace**
- **Where it runs.** A replace runs in the ioctl caller's thread. Any scrub error ends it `CANCELED`: the target is destroyed and the source kept.
- **Unmount and mount.** An unmount suspends it (`SUSPENDED`). An rw mount resumes it in kthread `btrfs-devrepl`, unless its target is missing or another exclusive operation holds the pool. After power loss it resumes from `committed_cursor_left`.
- **Stale state ✓.** The replace state persists in an item, so `STATUS` can report a *previous* replace's `FINISHED`. Source: `dev-replace.c`.

**Refusal codes ✓**
- btrfs ioctls report refusals as **positive** return values (`enum btrfs_err_code`, for example 8 = `BTRFS_ERROR_DEV_EXCL_RUN_IN_PROGRESS`). Source: `include/uapi/linux/btrfs.h` `btrfs_err_str`.
- Our `fsIoctl` drops positive returns, so a refused remove or balance reads as success today.
- Treating them as errors is safe for every other `fsIoctl` caller.

**Exclusive operation in sysfs**
- `/sys/fs/btrfs/<fsid>/exclusive_operation` reads `none`, `balance`, `balance paused`, `device remove`, `device replace` and so on. Source: `sysfs.c`.

**mkfs and superblocks**
- **Temporary magic.** mkfs.btrfs writes superblocks with magic `0x4D5F536652484221` ("!BHRfS_M"), which blkid and the kernel ignore. It writes the real magic only in its last step, to every copy on every device. So a failed mkfs leaves temporary-magic superblocks, and one killed during that last step may leave real ones on some devices. Source: progs `mkfs/common.c` `make_btrfs`, `kernel-shared/disk-io.c` `close_ctree_fs_info`.
- **Wipe before write.** Before writing, mkfs zeroes 0–2 MiB, each superblock copy and the last 2 MiB of every device, whether or not `-f` is given. Source: progs `common/device-utils.c` `btrfs_prepare_device`.
- **Copy locations.** Superblock copies sit at 64 KiB, 64 MiB and 256 GiB from the start of the block device (the partition, not the disk). A copy is skipped when `offset + 4096` exceeds the device size.
- **Erasing them.** The kernel's own scratch routine zeroes the 8-byte magic of each copy. `wipefs` and libblkid see only the primary copy. `FORGET_DEV` exists since 5.1. Source: kernel `volumes.c` `btrfs_scratch_superblocks`; util-linux `libblkid/src/superblocks/btrfs.c`.

**Prior art**
- **No partition rollback elsewhere.** udisks2 and Cockpit do **not** roll back a partition after a failed format; this plan goes further. Source: udisks `udiskslinuxpartitiontable.c` `handle_create_partition_and_format`; cockpit `pkg/storaged/utils.js`.
- **How fstests interrupts.** fstests fills the filesystem and cancels once the operation is seen running; it uses no dm-delay. Source: `btrfs/011`, `btrfs/049`, `btrfs/213`.

## Global Constraints

- **Make targets only.** Never run `go test`, `gofmt` or `golangci-lint` directly.
  - Focused runs: `make test-go GO_TEST_PKGS='<pkgs relative to backend/>'`.
  - Each task ends with `make check-backend-quiet`.
  - The phase ends with `make test-quiet` (section 4.6).
- **No commits.** Each task ends with a suggested Conventional Commits message: subject of 72 characters or fewer, each body paragraph on one unwrapped line, no Co-Authored-By.
- **Logging per AGENTS.md:**
  - log or return, never both;
  - one Info line, in the past tense, per completed host mutation;
  - the error key is `error`;
  - messages are constant and lowercase.
- **Native ioctls only.** No parsing of btrfs-progs output. Pin every new kernel struct or constant with the request-number test in `ioctl_test.go`.
- **Resumable routes.** They stay `Privileged()` and free of secret fields; `apischema` enforces both.
- **Root tests run in the test VM** (`make vm-test-btrfs-integration`, Task 0.1). Claim kernel-level verification only from that output. A run on the user's own hardware (`make test-btrfs-integration`, sudo) is optional and theirs to start.
- **The VM rig touches only its own domain.** It operates on the libvirt domain `linuxio-e2e` and volumes named `linuxio-e2e-*`. It never lists, stops or changes another domain (the host also runs `HA`).
- **Kernel floors are documented, not worked around.** Remove-cancel needs 5.14; on older kernels the cancel is refused and the removal runs to the end.
- **Generated files.** Do not hand-edit `frontend/src/api/generated/*`; nothing here changes an API type.

## Review Focus

1. **A cancel racing a pause.** The job's own context cancel runs after `Suspend` paused the balance. A `BALANCE_CTL cancel` at that point deletes the paused balance and loses the resume. Pinned in Task 7 by `TestBalancePauseIsNotCanceledByTheJobContext`.
2. **Signals on 6.19 and later.** A signal on the ioctl thread aborts a scrub or destroys a replace's target. Pinned in Task 1 by `TestBlockingIoctlBlocksAsyncSignals` and the `signals` integration step, which sends `tgkill` SIGURG to every thread.
3. **Kernel refusals read as success.** A remove or balance refused with a positive code, for example because a paused balance holds the pool, completes the job. Pinned by `TestRemoveDeviceReportsKernelRefusal` in Task 1 and the `refusedWhilePaused` integration step in Task 7.
4. **Rollback touching what the request did not make.** The rollback must:
   - wipe only superblocks whose fsid is the UUID mkfs was given;
   - wipe them before deleting the partition, because the offsets are partition-relative;
   - delete only partitions this request created;
   - never run once the pool is in fstab.

   Pinned in Task 5 by `TestWipePoolSuperblocksOnlyTouchesItsOwnFSID` and `TestCreateBtrfsPoolUndoesOnFailure`.
5. **Resuming what should stay stopped.** A record left `running` with `cancel_requested`, a crash, or a host stopped by hand in an earlier boot must not be resumed. Pinned in Task 3 by `TestResumeOnlyWhatTheLastShutdownInterrupted`.

---

## Before phase 3: a test VM, and the phase 2 checks

### Task 0.1: A disposable test VM the agent controls (D5)

**Files:**
- Create: `packaging/vm/linuxio-vm.sh` (create, install, ssh, reset, destroy), `packaging/vm/user-data.yaml.in` (cloud-init template), `mk/vm.mk` (included from `Makefile` like the other `mk/*.mk` files)
- Create: `frontend/config/playwright.vm.config.ts`, `frontend/src/test/vm/login.spec.ts`
- Modify: `docs/TODO/e2e-testing.md` (Tier 3: what exists now), `docs/development.md` (how to use the VM)

**Interfaces** (Make targets; state lives in `~/.cache/linuxio-vm/`, never in git):
- **`make vm-create`.**
  - **Image.** Downloads the Ubuntu 26.04 cloud image (the release this development host runs) and checks it against the published `SHA256SUMS`.
  - **Domain.** Creates the libvirt domain `linuxio-e2e` on `qemu:///system`: 4 vCPU, 4 GiB RAM, a 25 GiB qcow2 overlay on the base image, four 8 GiB raw disks (`vdb`–`vde`) for btrfs, on the `default` NAT network.
  - **cloud-init.**
    - **Seed.** The host has no ISO tools, so the seed comes over HTTP. SMBIOS `serial=ds=nocloud;s=http://192.168.122.1:<port>/` points at a `python3 -m http.server` bound to `192.168.122.1` for the first boot only.
    - **User.** It creates user `linuxio-e2e` with a random password (`~/.cache/linuxio-vm/password`, mode 0600), the rig's SSH key and passwordless sudo.
    - **Packages.** It installs `docker.io` and `btrfs-progs`, then runs `packaging/scripts/install-dependencies.sh`.
  - **Snapshot.** When cloud-init has finished, it takes the snapshot `clean`.
- **`make vm-install`.** Runs `make build-nocheck` on the host, copies `bin/` and `packaging/` to the VM, and runs `packaging/scripts/install-linuxio-binaries.sh --local --build-dir <dir>` as root there.
- **`make vm-ssh CMD='…'`**, **`make vm-reset`** (revert to `clean`), **`make vm-destroy`** (stop and undefine the domain, delete its `linuxio-e2e-*` volumes).
- **`make vm-test-btrfs-integration`.** Builds the `common/btrfs` test binary on the host inside the recipe (`go test -c`), copies it to the VM, and runs it as root. It uses `LINUXIO_RUN_BTRFS_INTEGRATION=1` and the same `-test.run` regex as `test-btrfs-integration`.
- **`make test-vm-browser`.** Runs Playwright with `playwright.vm.config.ts` against `https://<vm-ip>:8090`. The VM address comes from `virsh domifaddr`, the login from the cache directory, and specs from `frontend/src/test/vm/`.
- **Guard.** `linuxio-vm.sh` takes no domain name; it is hard-wired to `linuxio-e2e` and its volumes, and refuses to run when `virsh` is not `qemu:///system`.

- [ ] **Step 1: Write the script, the cloud-init template and the targets.** `shellcheck packaging/vm/linuxio-vm.sh && shfmt -d packaging/vm/linuxio-vm.sh` is clean.
- [ ] **Step 2:** `make vm-create`, then `make vm-install`. Expected: `make vm-ssh CMD='systemctl is-active linuxio.target linuxio-jobs@0.socket'` prints `active` twice.
- [ ] **Step 3: Login spec.** `login.spec.ts` signs in as `linuxio-e2e`, sees the dashboard and signs out. Run `make test-vm-browser`. Expected: PASS.
- [ ] **Step 4: Baseline root run** on the current tree, before any phase 3 change: `make vm-test-btrfs-integration`. Expected: PASS, logging the VM kernel version.
- [ ] **Step 5: Docs.** `e2e-testing.md` Tier 3 lists what now exists; `development.md` explains the targets and that the VM costs about 4 GiB of RAM while it runs.
- [ ] **Step 6: Suggested commit:** `test(e2e): add a disposable test VM driven by Make`.

### Task 0.2: Run the phase 2 checks in the VM

**Files:**
- Create: `frontend/src/test/vm/jobs-phase2.spec.ts`, plus fixtures under `frontend/src/test/vm/fixtures/` (a small Compose project, files for copy and upload)
- Modify: `docs/TODO/jobs.md` (section 3: what was verified, and where)

Every job is started through the browser, not `jobs submit`. Host-level events go through `make vm-ssh` and `virsh` on `linuxio-e2e`.

- [ ] **Step 1: Write the specs**, one per check from the phase 2 handoff:
  - **Reload mid package update.** First make an update available by installing an older version of a small package over `vm-ssh`. Start the update, reload the page, and expect the same job picked up again and completed.
  - **Reload mid Compose up.** The fixture service's healthcheck keeps it starting for about 30 seconds. Reload, expect the job picked up, then completed.
  - **Reload mid file copy.** Copy a 2 GiB fixture file, reload, expect the job picked up, then completed.
  - **Close the Compose dialog mid-run.** The job keeps going, shows in the tray and completes.
  - **Upload, batch upload and archive download.** Each completes, and the files on the VM match the originals.
  - **Save a Compose stack over an existing file.** The overwrite prompt appears.
  - **btrfs scrub across logout.** Create a pool on `vdb` and `vdc`, start a scrub, log out and back in. Expect it still running, then completed.
- [ ] **Step 2: Host-event checks.** Replace `jobs.md` section 7 with these (`jobs submit` goes in Task 12). Start a long job in the browser, then:
  - `systemctl stop linuxio-jobs@0`: the job is `interrupted/shutdown`, with an alert;
  - `kill -9` the host: `interrupted/crash`;
  - `virsh reboot`: the root host is active after boot;
  - `virsh destroy`, then `virsh start` (power loss): `interrupted/reboot` and "job marked interrupted" in `journalctl -b -u linuxio-jobs@0.service`.
- [ ] **Step 3: Run** `make test-vm-browser`. Report every check with its result. A failure is reported to the user with its output. Phase 3 does not start until the user has decided how each failure is handled.
- [ ] **Step 4: Docs.** In `docs/TODO/jobs.md`, record the result in section 3, and change the status table's phase 2 row: the last step is committed (`d2871872`).
- [ ] **Step 5: Suggested commit:** `test(jobs): check the phase 2 job behaviour in the test VM`.

## Phase 3

### Task 1: Blocking btrfs ioctls survive signals and report refusals

**Files:**
- Modify: `backend/common/btrfs/ioctl.go` (`fsIoctl`, new `blockingIoctl`, `kernelCode`)
- Modify: `backend/common/btrfs/devices.go` (`RemoveDevice`, `Replace`, `Balance`), `backend/common/btrfs/scrub.go` (per-device scrub)
- Test: `backend/common/btrfs/ioctl_test.go`, `backend/common/btrfs/devices_test.go`
- Create: `backend/common/btrfs/integration_resume_test.go`; Modify: `mk/quality.mk:113` (run regex)

**Interfaces:**
- Produces: `func blockingIoctl(mountpoint string, req uintptr, arg unsafe.Pointer) error`. It runs `fsIoctl` on a goroutine that:
  1. calls `runtime.LockOSThread`;
  2. blocks every signal except SIGSEGV, SIGBUS, SIGFPE, SIGILL, SIGTRAP and SIGSYS with `unix.PthreadSigmask(unix.SIG_BLOCK, set, nil)`. The kernel would turn a blocked synchronous fault into a silent kill instead of a Go panic.
  3. exits **without** unlocking, so the runtime discards the thread and its mask.

  It is used for the long ioctls: scrub, replace start, balance start and resume, and rm-dev. The short cancel and pause calls stay on `fsIoctl`.
- Produces: `type kernelCode uint64`, an `error`.
  - `fsIoctl` returns `kernelCode(r1)` when the syscall returns a positive value.
  - `Error()` uses the `btrfs_err_str` texts from `include/uapi/linux/btrfs.h` for codes 1–10, else `"kernel refused with code %d"`.
  - `Replace` reports a `kernelCode` that is a `replaceResults` key with that table's text.
- Produces: `TestBtrfsResumeIntegration`. It needs root and skips like `TestBtrfsIntegration`. It builds its own pool: two 4 GiB sparse loop disks in raid1, plus a third blank 4 GiB disk.
- Produces these test helpers:
  - `fill(mib int)` writes that many MiB of random data and syncs.
  - `exclusiveOp() string` reads `/sys/fs/btrfs/<uuid>/exclusive_operation`.
  - `runChild(op string) *exec.Cmd` re-executes the test binary with `-test.run=^TestBtrfsIntegrationChild$` and `LINUXIO_BTRFS_IT_CHILD=<op>`.
  - `TestBtrfsIntegrationChild` runs the named operation (`scrub`, `balance`, `remove:<id>` or `replace:<id>:<path>`) and skips without the env var.
  - Steps share the third disk. Each step that uses it leaves it blank for the next (`wipeDeviceEdges`).

- [ ] **Step 1: Write the failing tests.**
  - `TestBlockingIoctlBlocksAsyncSignals`: stub `fsIoctl` to parse `SigBlk` from `/proc/thread-self/status`. Assert the mask is every bit except SIGKILL, SIGSTOP and the six synchronous signals, and that `blockingIoctl` returns the stub's error unchanged.
  - `TestKernelCodeMessages`: `kernelCode(8).Error()` contains `"operation in progress"`, and `kernelCode(99).Error() == "kernel refused with code 99"`.
  - `TestRemoveDeviceReportsKernelRefusal`: with the fake kernel's `fail[iocRmDevV2] = kernelCode(8)`, the `RemoveDevice` error wraps `kernelCode(8)` (`errors.As`).
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/btrfs/...'`. Expected: FAIL, `blockingIoctl` and `kernelCode` undefined.
- [ ] **Step 3: Implement** `blockingIoctl`, `kernelCode` and the `fsIoctl` positive-return branch, and switch the listed call sites.
- [ ] **Step 4: Add the integration scaffold and the `signals` step.**
  - While a full `Scrub` runs, a goroutine sends `unix.Tgkill(pid, tid, SIGURG)` to every thread in `/proc/self/task` in a loop. The runtime ignores an unexpected SIGURG, so only the kernel side can be affected.
  - Assert the scrub status is `finished`, and log `uname -r`.
  - Change the Makefile regex to `'^Test(Btrfs(Degraded|Resume)?|Partition)Integration$$'`.
- [ ] **Step 5: Run** `make test-go GO_TEST_PKGS='./common/btrfs/...'` (PASS; the integration tests skip) and `make check-backend-quiet`.
- [ ] **Step 6: Suggested commit:** `fix(btrfs): block signals on long ioctls and report kernel refusals`.

### Task 2: A cancelled Mutation records the request first and ends when its handler returns

**Files:**
- Modify: `backend/bridge/internal/bridgeipc/router.go:452-468` (`routeRunner`), `backend/bridge/internal/bridgeipc/tasks.go` (`Recorder`, `Task.stop`), `backend/bridge/internal/bridgeipc/mutation_test.go` (`fakeRecorder`)
- Modify: `backend/bridge/internal/jobshost/recorder.go`
- Modify: `backend/common/jobs/store.go:319` (`needsSync`)
- Test: `backend/bridge/internal/bridgeipc/router_test.go` or `tasks_test.go` (wherever the early-return behaviour is asserted today), `backend/bridge/internal/jobshost/host_test.go`, `backend/common/jobs/store_test.go`

**Interfaces:**
- `routeRunner` returns the runner's own result for a `ModeMutation` route, even after the context ended. Calls keep returning `ctx.Err()` at once.
  - So a cancelled or interrupted Mutation turns terminal, and its record is written, only when the handler has returned. That matches section 2.11.
  - The host's idle exit (`rec.running()`) and the shutdown drain (`task.Done()`, bounded by `drainTimeout`) then wait for kernel cancels, rollbacks and Docker restarts.
- Produces: `Recorder.CancelRequested(task *Task)`.
  - `Task.Cancel` calls it synchronously on a **running** task, before cancelling its context.
  - It is never called for `Interrupt` or for a queued task.
  - It reaches every cancel path: `tasks.cancel`, a stream abort, and `CancelTasksForSession`.
- Produces: `(*recorder).CancelRequested`.
  - It sets `CancelRequested = true` on a `running` record, only once `Started` has written it (the `created` flag). It uses a detached context with `storeTimeout`.
  - On a store error it logs Warn `"job cancel request not recorded"` with `job_id` and `error`.
- `needsSync` also returns true when `CancelRequested` changes.

- [ ] **Step 1: Write the failing tests.**
  - `TestCanceledMutationEndsWhenItsHandlerReturns` (bridgeipc): the handler ignores its context for 200 ms after the cancel. The task stays `running` until then, and then becomes `canceled`. Change the existing assertion that a cancelled task turns terminal at once so that it applies to Calls only.
  - `TestCancelNotifiesRecorderBeforeContext`: the fake recorder appends `"cancel_requested"`, and the runner appends `"ctx_done"` when its context ends. After `Cancel`, the order is exactly those two; after `Interrupt`, there is no `"cancel_requested"`.
  - `TestCancelRecordsRequestBeforeHandlerReturns` (jobshost): the handler waits for `ctx.Done()`, reads its record from the store and reports `CancelRequested`. Assert it is `true` and the final state is `canceled`.
  - `TestNeedsSyncOnCancelRequest` (store).
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./bridge/internal/... ./common/jobs/...'`. Expected: FAIL.
- [ ] **Step 3: Implement** as described in Interfaces.
- [ ] **Step 4: Run** the same target (PASS) and `make check-backend-quiet`. Then run `make test-go GO_TEST_PKGS='./bridge/handlers/filebrowser/...'`: the BrowserBound uploads go through the same runner.
- [ ] **Step 5: Docs.** In `docs/TODO/jobs.md`:
  - rewrite the fault-matrix Cancel line: "the record becomes `canceled` when the handler returns";
  - tick the section 5 gap "Cancel marks the record canceled at once without `cancel_requested`".
- [ ] **Step 6: Suggested commit:** `fix(jobs): end a cancelled job when its handler returns`.

### Task 3: Shutdown records the real outcome, and a job is resumed once

**Files:**
- Modify: `backend/bridge/internal/bridgeipc/tasks.go` (new `Task.BeginInterrupt`, `Task.Interrupted`)
- Modify: `backend/bridge/internal/jobshost/lifecycle.go:37-62` (`reconcile`), `:75-90` (`earlierBootResumes`), `:230-256` (`interrupt`)
- Test: `backend/bridge/internal/jobshost/host_test.go`

**Interfaces:**
- Produces: `func (j *Task) BeginInterrupt()`. It sets the interrupted flag without cancelling, so a handler that a `Suspend` hook stops (a paused balance returns an error) ends `interrupted`, not `failed`.
- Produces: `func (j *Task) Interrupted() bool`. It reads the flag under `j.mu`.
- `interrupt` runs in this order:
  1. If the route has `Suspend`, call `task.BeginInterrupt()`.
  2. Get the checkpoint from `suspend` (unchanged: it skips records that are not `running` or have `CancelRequested`).
  3. One `Update`:
     - a `running` record becomes `interrupted/shutdown` and gets the checkpoint;
     - an `interrupted` record with no reason, which the handler's own `Finish` wrote first, gets reason `shutdown` and the checkpoint;
     - any other state is left alone, so a job that completed during `Suspend` stays `completed`.
  4. Call `task.Interrupt()`.
  5. Alert if step 3 changed the record.
- Resume eligibility in `earlierBootResumes`: the record is `interrupted`, `Resumable`, not `CancelRequested`, has reason `shutdown` or `reboot`, and its executor ran in an earlier boot.
- After the list is built, `reconcile` clears `Resumable` on every other `interrupted` record that has it (`"job not resumable after this start"`, Debug). A crash, a host stopped by hand, or a cancelled job is then never resumed by a later boot.

- [ ] **Step 1: Write the failing tests.**
  - `TestSuspendThatStopsTheHandlerKeepsCheckpoint`: a resumable test route's handler blocks until `stop` closes, then returns `errors.New("paused")`. Its `Suspend` closes `stop`, waits until the handler has returned, and returns `{"at":5}`. After SIGTERM: the record is `interrupted` with reason `shutdown` and checkpoint `{"at":5}`, the task snapshot is `interrupted`, and there is one "was interrupted" alert and no "failed" alert.
  - `TestJobThatCompletesDuringSuspendStaysCompleted`: the handler returns success while `Suspend` runs. The record is `completed` and no alert is raised.
  - `TestResumeOnlyWhatTheLastShutdownInterrupted`: earlier-boot resumable records with reason `shutdown`, `reboot`, `crash`, a `shutdown` one with `CancelRequested`, and a this-boot `shutdown` one. Only the first two are resumed, and the other three have `Resumable == false` afterwards.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./bridge/internal/...'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same target (PASS, including the existing suspend and resume tests at `host_test.go:490,585,629`) and `make check-backend-quiet`.
- [ ] **Step 5: Suggested commit:** `fix(jobs): record the real outcome at shutdown and resume a job once`.

### Task 4: Cancelling a device removal cancels it in the kernel

**Files:**
- Modify: `backend/common/btrfs/devices.go:152` (`RemoveDevice`, new `ResumeRemove`), `backend/common/btrfs/ioctl.go` (`rmDevCancel`)
- Modify: `backend/common/btrfs/kernel_test.go`: the fake `iocRmDevV2` gets a `remove func(id uint64) error` hook. A name of `"cancel"` with flags 0 returns `ENOTCONN` until the hook has started; after that it calls `k.cancel()` and returns `ECANCELED`.
- Test: `backend/common/btrfs/devices_test.go`, `integration_resume_test.go`

**Interfaces:**
- `RemoveDevice(ctx, uuid string, id int) error` keeps its signature. It runs the rm-dev ioctl through `blockingIoctl` in a goroutine, inside `waitOp`.
  - Once `ctx` has ended, every progress tick re-sends `RM_DEV_V2` with `Name = "cancel"` and `Flags = 0` until a send returns anything but `ENOTCONN`. That covers a cancel sent before the kernel claimed the exclusive operation.
  - `ECANCELED` becomes `fmt.Errorf("removal of device %d canceled", id)`.
- Produces: `func ResumeRemove(ctx context.Context, uuid string, id int) error`.
  - It returns nil when device `id` is no longer a member: the removal finished before the interruption.
  - It refuses when the pool has a missing device: `"pool %s is degraded; removal not resumed"`.
  - Otherwise it calls `RemoveDevice`.

- [ ] **Step 1: Write the failing tests.**
  - `TestRemoveDeviceCancelsWithContext`: the remove hook blocks on `k.cancelled`. Cancel `ctx` before the hook starts. The first cancel gets `ENOTCONN` and a later tick's cancel lands. The error is `"removal of device 3 canceled"`.
  - `TestResumeRemoveSkipsAGoneDevice`: device 3 absent → nil and no rm-dev call; present → one call; a missing device in the pool → the degraded error.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/btrfs/...'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Integration steps.** Fail (do not skip) when the operation ended before the interruption, with "raise the fill size".
  - `removeCancel`: add the third disk and `fill(1024)`. Start `RemoveDevice` on it, and cancel once `exclusiveOp() == "device remove"`. Expect the canceled error, the disk still a member, a scrub with 0 errors, and then `ResumeRemove` removes it.
  - `removeKill`: add the disk back and run `runChild("remove:<id>")`. SIGKILL the child once `exclusiveOp() == "device remove"` and wait. Expect `exclusiveOp() == "none"` and the disk still a member; then `ResumeRemove` removes it.
- [ ] **Step 5: Run** `make test-go GO_TEST_PKGS='./common/btrfs/...'` and `make check-backend-quiet`.
- [ ] **Step 6: Docs.** In `docs/btrfs.md`, add that cancel needs kernel 5.14. Also note that a cancel which lands just as a removal finishes can make the kernel cancel the next balance or removal at its start ("chunk relocation canceled on start" in dmesg); start it again.
- [ ] **Step 7: Suggested commit:** `fix(btrfs): cancel a device removal in the kernel`.

### Task 5: A failed pool create removes its partitions and superblocks

**Files:**
- Modify: `backend/common/btrfs/pool.go`: `superMagicTemporary`, `superblockOffsets`, `WipePoolSuperblocks`, and `CreatePool` returns its UUID on a mkfs error too.
- Modify: `backend/common/btrfs/partition.go`: `PartitionNumber`; `newPartition` and `PartitionDisk` undo their own entry on a late failure.
- Modify: `backend/bridge/handlers/storage/btrfs_pool.go:28-91` (`CreateBtrfsPool`), `backend/bridge/handlers/storage/partitions.go:21-32` (seams)
- Test: `backend/common/btrfs/pool_test.go`, `backend/common/btrfs/partition_test.go`, `backend/bridge/handlers/storage/btrfs_pool_test.go`, `integration_resume_test.go`

**Interfaces:**
- Produces: `const superMagicTemporary = 0x4D5F536652484221` and `func superblockOffsets(size uint64) []int64`, which lists 64 KiB, 64 MiB and 256 GiB while `off+superSize <= size`.
- Produces: `func WipePoolSuperblocks(device, uuid string) (bool, error)`. It reports whether it wiped anything.
  - It zeroes the 8 magic bytes of every in-range copy whose magic is `superMagic` or `superMagicTemporary` **and** whose fsid is `uuid`.
  - It fsyncs, then forgets the device (`ENOENT` is ignored).
  - The existing `WipeDevice`, used by pool destroy, is unchanged.
- Produces: `func PartitionNumber(part string) (int, error)`, which reads `sysBlockDir/<base>/partition`.
- `newPartition` undoes a late failure. Today it clears the entry only when the kernel refuses the add. On any error after `addKernelPartition`, such as `waitForNode` or `wipeDeviceEdges` failing, it now drops the kernel partition (`dropKernelPartition`) and clears the entry the same way. `PartitionDisk` does the same for its one partition.
- `CreatePool` returns `uuid, err` for every error after it generated the UUID. Its only callers are `CreateBtrfsPool` and the integration test.
- `CreateBtrfsPool`:
  - **Disk locks.** The per-disk `defer unlock()` calls become an `unlocks` slice. One deferred func at the top runs the rollback when the named return error is set, then the unlocks in reverse, so the rollback holds the disk locks.
  - **`committed`.** A `committed` flag is set once `addToFstab` succeeded. An error after that, such as the final `FindFilesystem`, is returned without a rollback.
  - **Rollback.** It runs on `context.WithoutCancel(ctx)` with a 2-minute timeout:
    1. unmount, if this call mounted the pool;
    2. `WipePoolSuperblocks(member, uuid)` for each member, when the UUID is known;
    3. `deletePartition(disk, number)` for each partition this call created, newest first.
  - **Errors.** Rollback errors join the original: `errors.Join(err, fmt.Errorf("undo btrfs pool create: %w", undoErr))`.
  - **Seams.** These are added beside `partitions.go`'s: `partitionDisk`, `addPartition`, `createBtrfsPool`, `mountBtrfsPool`, `wipePoolSuperblocks`, `partitionNumber`, `unmountPool`, `addPoolFstab`. `CreateBtrfsPool` switches to the existing `storageBlockDevices` and `btrfsFilesystems` seams.

- [ ] **Step 1: Write the failing tests.**
  - `TestWipePoolSuperblocksOnlyTouchesItsOwnFSID`: a sparse 257 GiB image holds a real-magic primary with fsid A, a temporary-magic 64 MiB copy with fsid A, and a real-magic 256 GiB copy with fsid B (`controlIoctl` is stubbed). `WipePoolSuperblocks(image, A)` returns true and zeroes only the first two magics; a second call returns false.
  - `TestSuperblockOffsetsSkipsBeyondDevice`: 100 MiB → `[65536, 67108864]`.
  - `TestPartitionNumber` on a `fakeSysfs` tree.
  - `TestAddPartitionUndoesWhenTheNodeNeverAppears`: `waitForNode` fails; the entry is cleared and the kernel partition dropped.
  - `TestCreateBtrfsPoolUndoesOnFailure`: a table over the failing step {second partition, mkfs, mount, fstab}, with two `FreeSpace` disks. Assert:
    - `deletePartition` is called only for the created partitions, newest first;
    - `wipePoolSuperblocks` is called with the UUID for both members, and not at all when partitioning failed;
    - `unmountPool` is called only in the fstab case;
    - the returned error wraps the step's error.
  - `TestCreateBtrfsPoolKeepsAPoolInFstab`: the final `FindFilesystem` fails, and nothing is undone.
  - `TestCreateBtrfsPoolKeepsWholeDevicesOnFailure`: with whole `Devices` and mkfs failing, wipe is called for each device and `deletePartition` never.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/btrfs/... ./bridge/handlers/storage/...'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Integration step `rollback`.** Run `PartitionDisk(third disk)`, `CreatePool` on the partition, `WipePoolSuperblocks(part, uuid)` (expect true) and `DeletePartition(disk, 1)`. Then expect:
  - no partition in `kernelPartitions(disk)`;
  - a zero magic at partition-start + 64 KiB on the raw disk;
  - the UUID absent from `ListFilesystems`.
- [ ] **Step 5: Run** the Step 2 target (PASS) and `make check-backend-quiet`.
- [ ] **Step 6: Docs.** In `docs/btrfs.md` under Create pool, describe the rollback and state that a disk overwritten with `force` is not restored.
- [ ] **Step 7: Suggested commit:** `fix(storage): undo partitions and superblocks of a failed pool create`.

### Task 6: A Docker storage switch is one systemd restart, behind a clear warning (D1)

**Files:**
- Modify: `backend/common/systemd/systemd.go` (new `GetInvocationID`)
- Modify: `backend/bridge/handlers/docker/daemon_storage.go:19-160`
- Modify: `frontend/src/routes/_authenticated/settings/-components/docker/DockerDaemonSettingsSection.tsx:155,411-427`
- Test: `backend/common/systemd/systemd_test.go`, `backend/bridge/handlers/docker/daemon_storage_test.go`, `frontend/src/routes/_authenticated/settings/-components/docker/DockerDaemonSettingsSection.test.tsx:148-164`

**Interfaces:**
- Produces: `func GetInvocationID(ctx context.Context, name string) (string, error)`. It returns the unit's `InvocationID` property (systemd 232 and later) as hex, or `""` for a unit that is not loaded. It uses the same session pattern as `GetActiveState`.
- `applyDaemonStorage` runs in this order:
  1. Validate the request, read the current config and compute the new one (unchanged).
  2. If `docker.service` is active, stop every running container (unchanged). A container that refuses to stop ends the switch with nothing changed.
  3. Switch to `context.WithoutCancel(ctx)`, so the switch never stops half-written.
  4. Write the files. When the data root changes, write the mount drop-in first (`syncDataRootMountOrdering`, unchanged), then `daemon.json`. Docker is still running; dockerd reads both only when it starts.
  5. Read the `InvocationID`, run `ResetFailedUnit("docker.service")`, then `systemdRestartUnit(ctx, "docker.service")` (`systemd.RestartUnit`, which queues the job and returns). From here systemd owns the restart and completes it even if the job host dies. `RestartUnit` also starts a Docker that was stopped or failed, so the route can still revert a broken switch.
  6. Wait until `docker.service` is `failed`, or `active` with a **different** `InvocationID`. `ActiveState` alone still reads `active` before the restart job has run. A `failed` unit keeps today's error text.
- Progress messages: `"stop"` / `"Stopping running containers"`, then `"restart"` / `"Restarting Docker"`.
- Removed:
  - `stopDocker`'s stop of `docker.socket` and `docker.service`, `startDocker`, and the `socketActive` plumbing;
  - the `systemdStopUnit` and `systemdStartUnit` seams, which only this file uses.

  `docker.socket` is left alone: a client that connects during the restart waits on the socket for the new daemon.
- Seams: `systemdRestartUnit`, `systemdInvocationID`.
- **Accepted gap.** If the job host dies after writing the files but before the restart request, Docker keeps running on its old settings and the job is marked interrupted and alerted. Applying again finishes the switch.
- **Frontend copy** (exact), in the existing `ConfirmDialog`:
  - `title`: `"Stop all containers to change Docker storage?"`
  - `message`: `"Every running container stops and Docker restarts on the new storage. Anything on the old storage is kept but hidden, so deploy your stacks again afterwards. If you reach LinuxIO or other apps through a reverse proxy, tunnel or VPN running in Docker, that access stops until its stack is deployed again, so make sure you can reach this server another way first."`
  - `confirmText`: `"Change storage"`; `pendingText`: `"Changing storage…"`
  - Keep `destructive`, `cancelText` and the success toast.
  - Change the comment at `:155` to "Storage only changes with every container stopped and Docker restarted, which needs the user's go-ahead."

- [ ] **Step 1: Write the failing backend tests.**
  - `TestApplyDaemonStorageRestartsDockerOnce`: the recorded calls are, in order: stop containers, write the drop-in, write `daemon.json`, reset-failed, restart `docker.service`. There is no stop or start of `docker.socket` or `docker.service`.
  - `TestApplyDaemonStorageWaitsForANewInvocation`: `active` with the old `InvocationID` for two polls, then a new one. It succeeds only after the third poll.
  - `TestApplyDaemonStorageReportsAFailedStart`: `failed` gives today's error.
  - `TestApplyDaemonStorageFinishesAfterCancel`: cancelling `ctx` after the containers stopped still writes both files and restarts.
  - `TestApplyDaemonStorageStartsAStoppedDocker`: `docker.service` is inactive, so no container is stopped, the restart is issued and the switch succeeds on the new invocation.
  - `TestGetInvocationID`: against the systemd fake manager, as in `TestPackageLevelMethodsCallExpectedManagerMethods`.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/systemd/... ./bridge/handlers/docker/...'`. Expected: FAIL.
- [ ] **Step 3: Implement** the backend.
- [ ] **Step 4: Run** the same target (PASS) and `make check-backend-quiet`.
- [ ] **Step 5: Frontend copy.**
  - Update the dialog.
  - In `DockerDaemonSettingsSection.test.tsx`, replace the old title and button assertions with the new ones, and assert the dialog text contains `"reverse proxy"`.
  - Run `make test-frontend VITEST_FILE=src/routes/_authenticated/settings/-components/docker/DockerDaemonSettingsSection.test.tsx`, then `make check-frontend-quiet`.
- [ ] **Step 6: Docs.**
  - `docs/TODO/jobs.md`:
    - rewrite the 4.3 Docker bullet as done this way;
    - add a decision-log entry for D1;
    - in section 7, add the row "docker switch kill": submit `docker.apply_daemon_storage` and `kill -9` the host once the containers have stopped. Expect Docker still running, or running again, on either the old or the new settings, never stopped, and the job `interrupted/crash` with an alert.
  - `docs/superpowers/specs/2026-10-25-mutation-map.md:161,351`: mark the "Docker and every container stay down" risk as resolved.
- [ ] **Step 7: Suggested commit:** `fix(docker): switch Docker storage with one systemd restart`.

### Task 7: Balance pauses at shutdown and resumes

**Files:**
- Modify: `backend/common/btrfs/devices.go`: `Balance`, `balanceCancel`, and new `PauseBalance` and `ResumeBalance`.
- Modify: `backend/common/btrfs/ioctl.go`: `balanceCtlPause = 1`, `balanceResume = 1 << 4`, `balanceStateCancelReq = 1 << 2`.
- Modify: `kernel_test.go`: pause, the resume flag, and paused progress.
- Modify: `backend/bridge/handlers/storage/btrfs_device_operations.go`: the hooks, and `.Resumable` on `storage.btrfs_balance`.
- Test: `backend/common/btrfs/devices_test.go`, `integration_resume_test.go`

**Interfaces:**
- Produces: `errBalancePaused` and `errBalanceCanceledElsewhere`. `Balance` maps `ECANCELED` by the returned state:
  - `PAUSE_REQ` gives `errBalancePaused`;
  - `CANCEL_REQ` while its own `ctx` is live gives `fmt.Errorf("%w: balance canceled", context.Canceled)`. The cancel button (`storage.btrfs_balance_cancel`) cancels in the kernel, and the job then ends `canceled`, not `failed`.
- The cancel used by `waitOp` becomes `cancelRunningBalance(mnt)`. It reads `BALANCE_PROGRESS` first and cancels only when `RUNNING` is set and `PAUSE_REQ` is not. A paused balance, or one being paused, is left alone. `BalanceCancel` (the user's route) still cancels a paused balance.
- Produces: `func PauseBalance(ctx context.Context, uuid string) error`. `ENOTCONN` returns nil.
- Produces: `func ResumeBalance(ctx context.Context, uuid string, opts BalanceOptions, paused bool, report func(float64)) error`.
  - **None, and `paused` is false:** run `Balance` with the same options, with `Soft` set when a profile is converted.
  - **None, and `paused` is true:** someone cancelled it after this job paused it. Return `fmt.Errorf("%w: the paused balance was canceled", context.Canceled)`.
  - **Paused:** send `BALANCE_V2{Flags: balanceResume}` through `waitOp`, as `Balance` does. `EINPROGRESS` means observe instead.
  - **Running:** observe. Poll until it is no longer running. Return `errBalancePaused` if it ended paused. When `ctx` ends, cancel through `cancelRunningBalance`.
  - **Degraded pool:** refuse before anything else with `"pool %s is degraded; balance not resumed"`.
- Handler:
  - `balanceOptions(req) btrfs.BalanceOptions` is shared by the run and the resume.
  - `Suspend` calls `PauseBalance` and returns the checkpoint `{"paused":true}`.
  - `Resume` decodes `paused`, then calls `ResumeBalance` and `BalanceResult`. Progress goes through `bridgeipc.TaskFromContext(ctx)`. It logs Info `"balanced btrfs filesystem"` with `resumed=true`.

- [ ] **Step 1: Write the failing tests.**
  - `TestBalancePauseIsNotCanceledByTheJobContext`: the balance hook returns `ECANCELED` with `PAUSE_REQ` once a pause arrives, and `ctx` is cancelled right after. `Balance` returns `errBalancePaused`, and `Calls()` has no balance cancel.
  - `TestBalanceCanceledElsewhereEndsCanceled`: `CANCEL_REQ` with a live `ctx` gives an error that `errors.Is(err, context.Canceled)`.
  - `TestResumeBalanceResumesAPausedBalance`: paused progress leads to one `BALANCE_V2` whose flags are exactly `balanceResume`.
  - `TestResumeBalanceObservesAKernelRun`: running twice, then `ENOTCONN`, gives nil and no `BALANCE_V2`.
  - `TestResumeBalanceRerunsOnlyWhenNotPaused`: `ENOTCONN` with `paused=false` leads to one `BALANCE_V2` with convert and soft on data. With `paused=true` it gives a `context.Canceled` error and no `BALANCE_V2`.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/btrfs/...'`. Expected: FAIL.
- [ ] **Step 3: Implement** the btrfs functions and the hooks.
- [ ] **Step 4: Integration steps.** Run `fill(1500)` once for these steps.
  - `balancePauseRemount`: start `Balance` with no filters. Once `exclusiveOp() == "balance"`, call `PauseBalance`. `Balance` returns `errBalancePaused` and `exclusiveOp() == "balance paused"`. Unmount and `MountPool`; `ResumeBalance(paused=true)` returns nil and `exclusiveOp() == "none"`.
  - `balancePauseSameMount`: the same without the remount, which exercises the resume-flag path.
  - `refusedWhilePaused`: with a balance paused, `RemoveDevice` returns an error wrapping `kernelCode(8)`. Then call `ResumeBalance`.
  - `balanceKill`: run `runChild("balance")` and SIGKILL it once running. `BALANCE_PROGRESS` gives `ENOTCONN` because the kernel cancelled it; `ResumeBalance(paused=false)` re-runs it and returns nil.
- [ ] **Step 5: Run** `make test-go GO_TEST_PKGS='./common/btrfs/... ./bridge/handlers/storage/...'` and `make check-backend-quiet`.
- [ ] **Step 6: Suggested commit:** `feat(storage): resume a btrfs balance after a shutdown`.

### Task 8: Scrub suspends at an offset and resumes from it

**Files:**
- Modify: `backend/common/btrfs/scrub.go` (`ScrubFrom`, `ScrubPositions`, `cancelScrub`), `kernel_test.go` (record `args.Start` per device)
- Modify: `backend/bridge/handlers/storage/btrfs_device_operations.go` (status check, hooks, `.Resumable`)
- Test: `backend/common/btrfs/scrub_test.go`, `backend/bridge/handlers/storage/btrfs_device_operations_test.go`, `integration_resume_test.go`

**Interfaces:**
- Produces: `func ScrubFrom(ctx context.Context, uuid string, starts map[int]uint64, report func(BtrfsScrubStatus)) (BtrfsScrubStatus, error)`.
  - **`nil` starts:** every present device is scrubbed from 0. This is today's `Scrub`, which becomes a one-line call to `ScrubFrom`.
  - **A non-nil map:** only its devices are scrubbed, from their offsets.
  - **Merging counters.** With a non-nil map, the outcome is seeded from the last scrub record when that record is `canceled`. It takes that record's `StartedAt` and error and byte counters, so errors the first part found survive in the record.
- Produces: `func ScrubPositions(ctx context.Context, uuid string) (map[int]uint64, error)`. It returns the `last_physical` of every present device whose `SCRUB_PROGRESS` succeeds.
- `cancelScrub(mnt)`, used by `ScrubCancel` and by `ScrubFrom`'s context cancel, refuses while the `DEV_REPLACE` status is `STARTED`, because `SCRUB_CANCEL` would cancel the replace.
- Handler:
  - **Status check.** `runBtrfsScrubTask` checks the status, as `resyncScrub` does. `canceled` while its context is live, which the cancel button does, returns a `context.Canceled`-wrapped error. Any other status that is not `finished` fails.
  - **Checkpoint type.** `type scrubCheckpoint struct{ Remaining map[int]uint64 \`json:"remaining"\` }`, with `func scrubStarts(checkpoint jsontext.Value) (map[int]uint64, error)`.
  - **`Suspend`** calls `ScrubPositions` and then `ScrubCancel` ("no scrub is running" is ignored). It returns `{remaining}`, or a nil checkpoint (a full rescan) when no position was read: `ENOTCONN` cannot tell a finished device from one that never started.
  - **`Resume`** calls `ScrubFrom(scrubStarts(checkpoint))` with the same status check, and logs Info with `resumed=true`.

- [ ] **Step 1: Write the failing tests.**
  - `TestScrubFromStartsAtOffsets`: starts `{1: 4096, 3: 0}` lead to SCRUB on devices 1 and 3 with those `Start` values, and none on 2.
  - `TestScrubFromMergesTheCanceledRecord`: a saved `canceled` record with 2 uncorrectable errors plus a resumed run with 1 gives 3, with the first record's `StartedAt`.
  - `TestScrubPositionsSkipsIdleDevices`.
  - `TestScrubCancelSparesARunningReplace`: with a replace `STARTED`, `ScrubCancel` errors and sends no `SCRUB_CANCEL`.
  - `TestScrubStartsRoundTrip`: `{2: 7}` round-trips, and `nil` stays `nil`.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/btrfs/... ./bridge/handlers/storage/...'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Integration steps.**
  - `scrubResume`: run a full `Scrub` first and record its `BytesScrubbed`. Then start `Scrub`, take `ScrubPositions` once any offset is above 0, and call `ScrubCancel`. The first `Scrub` ends `canceled`. `ScrubFrom(positions)` ends `finished` with 0 errors, and its own run scrubbed fewer bytes than the full run.
  - `scrubKill` (section 2.18, "pool consistent"): run `runChild("scrub")`, SIGKILL it once running, and wait for it to exit. A full `Scrub` ends `finished` with 0 errors.
- [ ] **Step 5: Run** the Step 2 target (PASS) and `make check-backend-quiet`.
- [ ] **Step 6: Suggested commit:** `feat(storage): resume a btrfs scrub from where it stopped`.

### Task 9: A replace the kernel resumed is followed to the end (D3)

**Files:**
- Modify: `backend/common/btrfs/devices.go` (`ResumeReplace`, `CancelReplace`), `backend/common/btrfs/ioctl.go` (`replaceStateCanceled = 3`, `replaceStateSuspended = 4`)
- Modify: `backend/bridge/handlers/storage/btrfs_device_operations.go` (hooks, `.Resumable`)
- Test: `backend/common/btrfs/devices_test.go`

**Interfaces:**
- Produces: `func ResumeReplace(ctx context.Context, uuid string, id int, report func(float64)) error`. It acts on the `STATUS` state:
  - **`STARTED`:** this is the replace the kernel resumed after a power loss. Poll `STATUS` until it leaves `STARTED`. When `ctx` ends, return `ctx.Err()` **without** cancelling; the caller decides. Then:
    - `FINISHED` with device `id` present and not missing → nil;
    - otherwise → an error.
  - **`SUSPENDED`:** `"the kernel suspended the replace of device %d; remount the pool to continue it"`.
  - **Anything else:** `"the replace of device %d did not survive the interruption; start it again"`. This includes `FINISHED`, which may be an older replace's, since the state persists.
  - Paths are not compared, because they can change across a reboot.
- Produces: `func CancelReplace(ctx context.Context, uuid string) error`.
- Handler:
  - `Suspend` returns `nil, nil`. In the original run, the interrupt's context cancel cancels the kernel replace, and the source is kept.
  - `Resume` calls `ResumeReplace`. If it returns a context error and `!task.Interrupted()`, the user cancelled, so call `CancelReplace`. On an interrupt, leave the kernel thread running: the unmount suspends the replace and the next mount resumes it.
  - Then the grow step `runBtrfsDeviceReplaceTask` already does runs, factored into `growReplaced(ctx, uuid, id, fs)`, with Info `resumed=true`.

- [ ] **Step 1: Write the failing tests.**
  - `TestResumeReplaceFollowsAKernelRun`: status `STARTED` twice, then `FINISHED`, gives nil and no start.
  - `TestResumeReplaceRefusesAStaleFinished`: `FINISHED` at once gives the "start it again" error and no `replaceCmdStart`.
  - `TestResumeReplaceDoesNotCancelOnContextEnd`: `STARTED`, then `ctx` is cancelled. The result is a context error and no `replaceCmdCancel`.
  - `TestResumeReplaceReportsSuspended`.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./common/btrfs/...'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Integration step `replaceKill`** (section 2.18, "a killed replace is reported"). Run `runChild("replace:2:<third disk>")`, SIGKILL it once `exclusiveOp() == "device replace"`, and wait. Then, depending on `uname -r`:
  - on 6.19 and later, `ResumeReplace` returns the "start it again" error and device 2 is still the old disk;
  - on older kernels, the child exits only when the replace ends, and `ResumeReplace` reports the same error, because `FINISHED` is not trusted.
- [ ] **Step 5: Run** `make test-go GO_TEST_PKGS='./common/btrfs/... ./bridge/handlers/storage/...'` and `make check-backend-quiet`.
- [ ] **Step 6: Suggested commit:** `feat(storage): follow a btrfs replace the kernel resumed`.

### Task 10: Device removal resumes; the resumable set is pinned

**Files:**
- Modify: `backend/bridge/handlers/storage/btrfs_device_operations.go`: the hooks, `.Resumable` on `storage.btrfs_device_remove`, and seams for the btrfs calls the hooks make.
- Test: `backend/bridge/apischema/schema_test.go`, `backend/bridge/handlers/storage/btrfs_device_operations_test.go`

**Interfaces:**
- Consumes: `ResumeRemove` (Task 4), and the hooks from Tasks 7–9.
- Handler: `Suspend` returns `nil, nil`. The interrupt's context cancel sends the kernel cancel, and moved chunks stay moved. `Resume` calls `ResumeRemove` and then `FindFilesystem`, and logs Info with `resumed=true`.
- Seams: `btrfsPauseBalance`, `btrfsResumeBalance`, `btrfsScrubPositions`, `btrfsScrubCancel`, `btrfsScrubFrom`, `btrfsResumeReplace`, `btrfsCancelReplace`, `btrfsResumeRemove`.

- [ ] **Step 1: Write the failing tests.**
  - `TestResumableRoutes` (apischema, over `handlers.Routes`): the routes with `Resumable` are exactly `storage.btrfs_balance`, `storage.btrfs_scrub_start`, `storage.btrfs_device_replace` and `storage.btrfs_device_remove`.
  - `TestBtrfsResumeHooks` (storage, through the seams): a table over the four routes covering:
    - `Suspend`'s checkpoint: `{"paused":true}`, `{remaining}`, nil, nil;
    - that `Resume` decodes it into the right btrfs call;
    - that a resumed replace cancelled by the user (`task.Interrupted()` false) calls `btrfsCancelReplace`, and an interrupted one does not.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./bridge/apischema/... ./bridge/handlers/storage/...'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** the same target (PASS) and `make check-backend-quiet`.
- [ ] **Step 5: Suggested commit:** `feat(storage): resume a btrfs device removal after a shutdown`.

### Task 11: Phase 3 checks in the VM, documents, full gate

**Files:** `frontend/src/test/vm/jobs-phase3.spec.ts`, `docs/TODO/jobs.md`, `docs/api-contract.md:617`, `docs/btrfs.md:318-320`, `docs/bridge_handler_patterns.md`, `docs/process-systemd-architecture.md:130,158`, `docs/TODO/scheduled-execution.md`, `docs/frontend-task-state-unification.md`, `docs/TODO/api-reliability-roadmap.md`

- [ ] **Step 0: VM checks of the real routes.** Run `make vm-install`, then write and run `jobs-phase3.spec.ts` (`make test-vm-browser`). Jobs are started in the browser; host events go through `vm-ssh` and `virsh`.
  - **Balance across a reboot.** Start a balance, run `systemctl stop linuxio-jobs@0`, then `virsh reboot`. Expect the balance resumed by the kernel at mount and the job completed after boot.
  - **Scrub across a power cut.** Start a scrub, `virsh destroy`, then `virsh start`. Expect the job resumed (a full rescan, no checkpoint) and completed.
  - **Replace across a power cut.** Start a replace onto `vde`, `virsh destroy`, then `virsh start`. Expect the kernel to resume it and the job to follow it to the end.
  - **Cancel, then reboot.** Cancel a scrub, then `virsh reboot`. Expect it not resumed.
  - **Crash, then reboot.** `kill -9` the host mid-scrub, then `virsh reboot`. Expect `interrupted/crash`, an alert, and no resume.
  - **Pool create rollback.** Make the mount fail by first creating a file where the mountpoint goes. Expect the partitions and superblocks gone.
  - **Docker switch kill** (Task 6's row). Docker is never left stopped.
  - Then run `make vm-test-btrfs-integration`. Expected: PASS.

- [ ] **Step 1: Update the documents** (section 4.6 list).
  - **`jobs.md`:**
    - **Status, and the 2.11 Suspend/Resume table** with the kernel facts above:
      - remove: "cancelled by the job's context, not a signal";
      - replace: per D3;
      - balance: "a SIGKILL cancels it; the next resume re-runs it unless it was paused".
    - **The resume rule** of D4.
    - **The fault matrix:** cancel ends when the handler returns.
    - **The 2.18 btrfs line.**
    - **Section 3:** a "Phase 3" entry.
    - **Section 4.3:** mark it done and list what moved out (the out-of-scope items).
    - **Section 5 and the decision log:** D1–D6, the signal mask, the positive return codes, freezing (a suspend to RAM still aborts a scrub or replace on 6.19 and later), and the stale `reloc_cancel_req`.
    - **Section 7:** the Step 0 checks, and how to run them in the VM.
  - **`api-contract.md`:** replace "no route is resumable yet" with the four routes, and change the cancel lifetime.
  - **`btrfs.md`:** the cancel and resume semantics, the paused-balance note (D2) and the rollback.
  - **The other listed documents:** check them, and update wherever they describe cancel timing or resume.
- [ ] **Step 2: Run** `make test-quiet`. Expected: PASS. Report the exact targets and their results.
- [ ] **Step 3: Report** the Step 0 results and the exact Make targets run. Offer the user an optional `make test-btrfs-integration` run on their own hardware.
- [ ] **Step 4: Suggested commit:** `docs(jobs): record phase 3 storage safety and btrfs resume`.

### Task 12: Remove the jobs CLI verbs (D6)

**Files:**
- Delete: `backend/bridge/cmd/jobs.go`, `backend/bridge/cmd/jobs_submit.go`
- Modify: `backend/bridge/cmd/cli.go:11-21` (drop `case "jobs"` and the usage that names it), `backend/bridge/cmd/cli_test.go:73-81`
- Modify: `backend/bridge/internal/jobsclient/client.go` (delete `Submit`, `progressLine` and `Resume`; also `Client.Call` if `rg` finds no other caller), and its tests
- Modify: `backend/bridge/internal/bridgeipc/task_primitives.go:39-40,90,118-130` (delete `ResumeRoute`, `jobResumeRequest`, `handleJobResume`), `backend/bridge/internal/bridgeipc/tasks.go:287-290` (`resumer`, `SetResumer`), the comment at `router.go:422`, and `backend/bridge/internal/jobshost/host.go:140` (no `SetResumer`). `Host.resume` stays as the boot path's own function.
- Docs:
  - `docs/TODO/jobs.md`:
    - the 2.2 row "Jobs with no user";
    - the 2.3 term "Submit CLI" and the 2.4 diagram line;
    - the 2.11 "Submit CLI" paragraph and the 2.18 line;
    - 4.1, where the `jobs submit` decision is resolved as removed and phase 4 owns how timers reach the store;
    - section 5 item "The CLI accepts a malformed `--resume` id" (obsolete).
  - `docs/api-contract.md:722`, `docs/process-systemd-architecture.md:50,56,221`.

**Interfaces:**
- `linuxio-bridge` accepts only `version`, `--version` and `-v`. Anything else exits 2, as an unknown argument does today. The host serves no `jobs.resume`.
- After this task, nothing outside the browser starts a job. Section 7 of `jobs.md` already runs its checks through the VM (Tasks 0.2 and 11).

- [ ] **Step 1: Write the failing tests.**
  - `TestBridgeRefusesTheJobsVerb` (cli): `linuxio-bridge jobs submit --route x` exits 2.
  - `TestNoJobsResumeRoute` (bridgeipc): `jobs.resume` is not a registered route.
- [ ] **Step 2: Run** `make test-go GO_TEST_PKGS='./bridge/...'`. Expected: FAIL.
- [ ] **Step 3: Delete** the code listed above, and every test that only exercised it.
- [ ] **Step 4: Run** the same target (PASS), `make check-backend-quiet`, then `make test-quiet`.
- [ ] **Step 5: Docs** as listed. Then run `rg -n 'jobs submit|jobs\.resume' backend docs packaging` and expect matches only in this plan and the decision log.
- [ ] **Step 6: Suggested commit:** `refactor(jobs): remove the jobs submit verb and the jobs.resume call`.
