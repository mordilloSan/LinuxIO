# Pool resync: continue its scrub after a power cut

## Status

Phases 0 to 3 built for v0.54.0 and reviewed twice, on 2026-11-18 and
2026-11-19, with every finding fixed (F1 to F7, N1, N2); Phases 4 and 5 are
not built. Decided 2026-11-18, from the backlog
item "Continue the scrub of a pool resync after a power cut", now removed.

**Recommended design:** the resync stays a scheduled run. Its next run, which
the health check already starts, continues the scrub it saved, for as long as
the pool has not run with a disk missing again. It does not become a job. The
decisions for the owner are at the end.

How the statements below were checked: the planning host has no man pages
for btrfs, so upstream behaviour was read from `btrfs <cmd> --help`
(btrfs-progs 6.17.1) and the installed kernel uapi header
(`/usr/src/linux-headers-7.0.0-43/include/uapi/linux/btrfs.h`). No kernel or
btrfs-progs source was at hand: each claim about their internals was marked
"Memory" and became Phase 0 work. Phase 0 read the kernel at v6.1, v6.8,
v7.0 and v7.3 (the newest release on 2026-11-18) and btrfs-progs v6.17, from
git.kernel.org and GitHub; the rows say what each tag shows.

## Problem

A pool that ran with a disk missing is resynced once every disk is back. A
scrub of the whole pool rewrites the stale copies on the returned disk. Then
a soft convert gives chunks written without redundancy their second copy.
Until both are done, the returned disk may still hold stale copies.

The scrub saves each device's position every minute, as a job's scrub does.
Nothing reads that position back. After a power cut or a shutdown, the next
resync scrubs every disk from the start again.

## What happened before Phase 1 (from the code)

Phases 1 to 3 changed the Scrub row (`resyncScrub` now calls
`continueScrub` with the episode's run, and `Scrub` is gone) and closed F2;
F1 waits for Phase 4.

| Step | What the code does | Where |
|---|---|---|
| Start | Automatic only. `linuxio-btrfs-health.timer` (`OnBootSec=2min`, `OnUnitActiveSec=10min`) runs `linuxio-worker health`. `raiseResyncAlert` returns true when four things hold: the pool state says degraded, the mounted pool has every disk, fewer than three resyncs failed, and no scrub runs (`fs.Scrub.Status != "running"`). `runHealth` then asks systemd to start `linuxio-btrfs-resync@<uuid>.service`. No route starts a resync. After three failures, mounting the pool again or rebooting re-arms it (`recordMount`). | `backend/worker/health.go`, `backend/common/btrfs/health.go`, `packaging/systemd/linuxio-btrfs-health.timer` |
| Runs as | A oneshot unit, as root, running `linuxio-worker resync <uuid>`, which calls `btrfs.Resync`. The scheduled-run hooks write a `scheduled.btrfs_resync` record in `/var/lib/linuxio/jobs/scheduled/`. The progress reads "Scrubbing" with the scrub's percent. | `packaging/systemd/linuxio-btrfs-resync@.service`, `backend/worker/degraded.go` |
| Scrub | `resyncScrub` calls `Scrub`, which is `ScrubFrom` with run `""` and no resume. `scrubSeed` gets nil starts, so every present device is scrubbed from 0. | `backend/common/btrfs/degraded.go`, `backend/common/btrfs/scrub.go` |
| Saved: scrub record | `/var/lib/linuxio/btrfs-scrub.json`, keyed by pool UUID. It holds the outcome, the counters and `run`, which is empty for a resync ("empty outside a job, as for a pool resync"). Per devid it holds the device UUID, `last_physical` as `offset`, `done`, `aborted` and the running call's counters, plus the writer's pid, start time and boot. It is written at the start, at each call's return, every minute while an offset moved, and at the end. Writes go through `atomicfile.Write` (file and folder fsynced) under `btrfs-scrub.json.lock`. | `scrubRecord`, `save`, `updateScrubRecords` in `scrub.go`; `backend/common/atomicfile/atomicfile.go` |
| Saved: pool state | `/var/lib/linuxio/btrfs-pools.json` holds `degraded`, `present`, `scrubbed` (set only after a finished scrub), `resyncFailures` and `resyncError`. | `poolState` in `degraded.go` |
| What the kernel keeps | Nothing for a scrub. Each `BTRFS_IOC_SCRUB` call counts only itself and returns `last_physical`. | [Btrfs](../btrfs.md#scrub-progress) |
| What resumes from what | The scrub job's resume (`ResumeScrub`) reads LinuxIO's own record when its `run` is the job's ID (`resumable`, `resumePoint`). Without that record it falls back to the Suspend checkpoint (`resumeBtrfsScrub` in `backend/bridge/handlers/storage/btrfs_device_operations.go`). btrfs-progs' `/var/lib/btrfs/scrub.status.<uuid>` is only read for display (`progsScrubStatus`), never written or resumed from. The resync's record is never read back, because `resumable` refuses an empty run. | `scrub.go` |
| Orderly shutdown | systemd stops the unit and SIGTERM cancels the worker's context. The scrub cancels its calls and writes the record `canceled` with each device's offset. `Resync` returns the context error, which does not count as a failed attempt. The end hook records the run `interrupted` with reason `shutdown`. | `backend/worker/main.go`, `Resync`, `outcome` in `backend/common/scheduledrun/hooks.go` |
| Power cut | The record stays `running`, with offsets up to a minute old and a writer from the earlier boot. The scheduled-run record stays `running`. | |
| Next boot | systemd mounts the complete pool from fstab; `linuxio-btrfs-degraded.service` mounts only pools held back by a missing disk or a replace target (`heldFstabPools`). The root job host marks the run `interrupted`/`reboot` (`Liveness.ended`) and raises nothing, since a resync run only alerts on a crash (`markInterrupted`, `KindOf`). It never resumes a scheduled run. The inventory reads a `running` record with a dead writer as `aborted` (`scrubStatus` in `inventory.go`). Two minutes after boot the health check starts the resync again, which scrubs every device from 0 and overwrites the record. | `backend/common/scheduledrun/liveness.go`, `backend/bridge/internal/jobshost/lifecycle.go` |
| Finding F1: cut during the convert | `scrubbed` is already saved, so the next run skips the scrub. The kernel continues the cut balance at mount (U12). The next run's `Balance` then gets `EINPROGRESS`, reported as "a balance is already running" (`Balance` in `devices.go`), and that counts as a failed attempt. Three of these, ten minutes apart, end in the resync error alert until the pool is mounted again, even after the kernel's balance has finished. | `resync`, `Balance` |
| Finding F2: cut between a finished scrub and `scrubbed` | The whole scrub runs again. | `resyncScrub` |

## What upstream guarantees

The Source column says where each statement comes from:

- **Repo:** a file in this repository.
- **Host:** an installed file or command on the planning host.
- **Memory:** recalled, not read; the plan names the check it needs.
- **Phase 0:** read in the upstream source at the tags named.

| # | Statement | Source |
|---|---|---|
| U1 | The kernel keeps no scrub state between calls. A call scrubs one device from `start` to `end` and returns its counters and `last_physical`: "last physical address scrubbed. In case a scrub was aborted, this can be used to restart the scrub". | Host: the uapi header (`struct btrfs_scrub_progress`). Repo: [Btrfs](../btrfs.md#scrub-progress) |
| U2 | `btrfs scrub resume` is described as "Resume previously canceled or interrupted scrub". | Host: `btrfs scrub resume --help`, btrfs-progs 6.17.1 |
| U3 | btrfs-progs saves each device's progress in `/var/lib/btrfs/scrub.status.<uuid>`. Its resume restarts each device that did not finish from that position and adds the saved counters. | Repo: [Btrfs](../btrfs.md#scrub-progress); the file format as `parseProgsScrubStatus` reads it |
| U4 | btrfs-progs resumes from `last_physical` itself, flushes no device before writing its status file, and knows nothing about degraded mounts in between. | Phase 0, confirmed, btrfs-progs v6.17 `cmds/scrub.c`: `scrub_start` resumes with `sp[i].scrub_args.start = last_scrub->p.last_physical;` (no byte back); `scrub_write_progress` writes a temporary file, renames and closes it, with no fsync of it or of any device; nothing in the file mentions a degraded mount |
| U5 | Scrub checks a tree block's generation and logical address against what is expected (`verify_errors`) and data against its checksum (`csum_errors`). It counts data without a checksum (`no_csum`), which it cannot verify. | Host: uapi header comments. Repo: [Btrfs](../btrfs.md#missing-disks) (`nodatacow`) |
| U6 | A device's scrub rewrites a bad copy on that device, in place, from a good copy on another device. | Repo evidence: `TestBtrfsDegradedIntegration` corrects stale copies, after which the returned disk alone holds every file and scrubs clean. Phase 0, read while checking U8: 6.1 `scrub_repair_sector_from_good_copy` writes the good copy to `sblock_bad->dev` at the bad copy's physical address; 6.8+ `scrub_write_sectors(sctx, stripe, repaired, false)` writes the repaired sectors to the stripe's own mirror |
| U7 | A device is scrubbed one dev extent at a time, in rising physical order. An extent that ends at or before `start` is skipped and any other is scrubbed whole. `last_physical` can be reported up to 8 MiB ahead of stripes still queued. | Repo: [Btrfs](../btrfs.md#scrub-progress) ("One byte back"); `TestBtrfsResumeIntegration` (`scrubResume`, `scrubPowerCut`) |
| U8 | A repair write has completed before `last_physical` moves past it. | Phase 0, `fs/btrfs/scrub.c`. Confirmed in the form the design needs, at v6.1, v6.8, v7.0 and v7.3: every repair outside the dev extent holding `last_physical` has completed. Literally refuted at every tag: `scrub_simple_mirror` sets `last_physical = physical + logical_length` ("No more extent, just update the accounting") while stripes of that extent are still queued, and on 6.1 the repair runs later in the read-completion worker (`scrub_repair_sector_from_good_copy`, `submit_bio_wait`). Each repair completes before its stripe counts as done (v6.8+ `scrub_stripe_read_repair_worker`: `scrub_write_sectors(sctx, stripe, repaired, false); wait_scrub_stripe_io(stripe);` then `SCRUB_STRIPE_FLAG_REPAIR_DONE`; v7.0 and v7.3 `flush_scrub_stripes` set `last_physical` only after that wait), and every tag waits for all repairs of a dev extent before the next (6.4+ `scrub_stripe` ends in `flush_scrub_stripes`; 6.1 `scrub_enumerate_chunks` waits for `bios_in_flight == 0` and `workers_pending == 0` after `scrub_chunk`). The one byte back rescans that extent: `found_key.offset + dev_extent_len <= start` is the only skip, at all four tags. D11's fallback is not needed |
| U9 | A repair write carries no FUA or flush. It is durable only after a cache flush. A transaction commit issues one only when a commit is pending (`btrfs_sync_fs`); `fsync` on the block device always does (`blkdev_fsync` → `blkdev_issue_flush`, which ignores EOPNOTSUPP). | Phase 0, confirmed at all four tags. Repair bios are plain writes: 6.1 `bio_init(&bio, sblock_bad->dev->bdev, &bvec, 1, REQ_OP_WRITE)` and `submit_bio_wait`; 6.8+ `scrub_write_sectors` allocates with `REQ_OP_WRITE` for `btrfs_submit_repair_write` (`fs/btrfs/bio.c`), and neither file adds FUA or PREFLUSH to them. `btrfs_scrub_dev` commits only `if (need_commit)`, after superblock errors. `btrfs_sync_fs` (`fs/btrfs/super.c`) returns 0 when `btrfs_attach_transaction_barrier` finds none (`-ENOENT`) unless `BTRFS_FS_NEED_TRANS_COMMIT` (6.1: `pending_changes`). `blkdev_fsync` (`block/fops.c`): `error = blkdev_issue_flush(bdev); if (error == -EOPNOTSUPP) error = 0;`, and `blkdev_issue_flush` (`block/blk-flush.c`) submits `REQ_OP_WRITE \| REQ_PREFLUSH` and waits; `block/blk-core.c` completes it at once on a disk without a write cache. No test rig can show a lost disk cache: loop devices and the VM's host page cache keep completed writes |
| U10 | A replace target takes the source's devid and keeps a device UUID of its own. | Phase 0, **refuted** at all four tags: `btrfs_dev_replace_finishing` (`fs/btrfs/dev-replace.c`) does `tgt_device->devid = src_device->devid;` and `memcpy(tgt_device->uuid, src_device->uuid, ...)`, swapping the UUIDs, so the target keeps the source's UUID. A continuation therefore carries on a replaced device from its offset; U17 is why that is safe. `replacePowerCut` asserts that devid 2 keeps its UUID |
| U11 | A balance, a removal or a convert moves a chunk by writing it anew at a new physical place, from reads checked against checksums that retry the other copy. | Phase 0, confirmed at all four tags for pools without the remap tree. `btrfs_relocate_block_group` (`fs/btrfs/relocation.c`) makes the group read-only (`btrfs_inc_block_group_ro(rc->block_group, true)`), so the copies go to other groups; removal (`btrfs_shrink_device`), balance and convert (`fs/btrfs/volumes.c`) all go through `btrfs_relocate_chunk`. Data is read through the data relocation inode (`btrfs_read_folio`), whose checksums are looked up by disk address (`btrfs_lookup_bio_sums`, the data-reloc branch) and checked, with a retry of the other copy (`btrfs_check_read_bio` → `repair_one_sector`; 6.1 `btrfs_repair_one_sector`); tree blocks are read by `btrfs_read_extent_buffer`, which tries each mirror. Data without checksums is copied unchecked, as a scrub cannot check it either. Since 7.0 a pool with the remap tree (`BTRFS_FEATURE_INCOMPAT_REMAP_TREE`, accepted only with `CONFIG_BTRFS_EXPERIMENTAL`, which Ubuntu's 7.0 leaves off) relocates by `copy_remapped_data`, a raw read with no checksum check (`is_remap`); LinuxIO creates no such pool |
| U12 | The kernel continues a balance item at the next read-write mount. A new `BALANCE_V2` while it runs answers `EINPROGRESS`. | Repo for a paused balance: [Btrfs](../btrfs.md#shutdown-and-resume), root step `balancePauseRemount`. Phase 0, confirmed at all four tags for a cut balance: `insert_balance_item` commits the item when a balance starts; `open_ctree` → `btrfs_recover_balance` finds it (`BTRFS_BALANCE_OBJECTID`) and marks the balance paused; `btrfs_start_pre_rw_mount` → `btrfs_resume_balance_async` continues it unless `skip_balance`. `btrfs_ioctl_balance` (`fs/btrfs/ioctl.c`) answers a new balance with `if (fs_info->balance_ctl) { ret = -EINPROGRESS;`, and `btrfs_try_lock_balance` does so while one runs. The Phase 4 root step still checks it |
| U13 | A mounted pool does not mark a device missing until it is mounted again, and a returned disk is not opened until then. | Repo: [Btrfs](../btrfs.md#missing-disks), `markUnopened`, root step `returnedDiskIsNotUsed` |
| U14 | With a mirror member missing, new chunks are allocated `single` on the present disks. | Repo: [Btrfs](../btrfs.md#missing-disks); asserted in `bootMountsTheSurvivor` |
| U15 | Starting a oneshot unit that is already running merges into its running start job. | Repo: [Btrfs](../btrfs.md#replication) (systemd `job_install`) |
| U16 | Since kernel 6.19 a freeze ends a scrub call with `EINTR`, and LinuxIO issues the call again. | Repo: [Btrfs](../btrfs.md#scrub-progress) |
| U17 | A replace writes the target only from copies checked as a scrub checks them, repaired from another copy, and the kernel refuses a scrub while a replace runs. | Phase 0, all four tags (`fs/btrfs/scrub.c`): 6.1 writes `scrub_write_block_to_dev_replace(sblock_other)` only after `scrub_recheck_block` found that copy good, and fills a sector no copy of which is good with zeros; 6.8+ `flush_scrub_stripes` writes `good` (`has_extent` and not `error`, after the repair worker). `btrfs_scrub_dev` returns `-EINPROGRESS` when `btrfs_dev_replace_is_ongoing` |
| U18 | The data chunk size a pool can be given through sysfs is a multiple of 256 MiB, at least 256 MiB and at most 10% of the pool. | Phase 0, v6.8 and v7.3 `fs/btrfs/sysfs.c` `btrfs_chunk_size_store`: `val &= ~((u64)SZ_256M - 1);` then `if (val < SZ_256M) return -EINVAL;`. The root test sets 256 MiB, not 64 MiB |

Conclusions:

- **When a continuation is complete.** A continued scrub covers the resync's
  purpose when four things hold:
  - a. Nothing was written with a member missing since the scrub began.
  - b. A device continues from its saved offset only if it is the same
    device, judged by devid and device UUID.
  - c. The repairs below a saved offset were on the disk when the offset was
    saved.
  - d. A device whose scrub aborted is scanned again.

  LinuxIO already has (b) and the one-byte-back margin. (a), (c) and (d) are
  new.
- **Data written while the disk was missing, into existing redundant
  chunks.** It leaves stale copies on the returned disk. That disk's own
  scrub finds them by checksum or tree-block generation and rewrites them
  (U5, U6). Copies below the saved offset were rewritten before the cut;
  those above it are rewritten after.
- **Data written into new `single` chunks.** It has no copy on the returned
  disk. The soft convert gives it one, not the scrub. The convert runs whole
  after a cut; being soft, it skips chunks already converted.
- **Ranges below the offset that change after the cut.** Either they were
  written with every disk present, so every copy was written, or a balance,
  removal or convert moved them, so they were written fresh (U11). Neither
  leaves a stale copy for the continuation to skip. An added disk brings a
  device UUID the record does not hold, so it starts from 0. A replace target
  keeps the source's device ID and UUID (U10, refuted as first written), so
  the continuation carries on from the source's offset; the replace wrote it
  only from checked copies (U17), so nothing below that offset is stale.
- **Is a full resync needed after a cut mid-resync?** It depends on what
  happened since:
  - The pool was mounted with every disk: only the rest of the scrub is
    needed, then the convert.
  - The pool was mounted with a disk missing, in a way LinuxIO records: the
    whole scrub, as a new episode. New writes can land anywhere, and the
    stale disk may now be a different one.
  - The pool was not mounted while incomplete: only the rest.
- **The pool generation is no guard here.** It moves with every commit,
  whether or not a disk is missing. The present disks' superblock
  generations stay what the boot check uses to decide a degraded mount.
- **btrfs-progs is less strict.** Its resume has neither the episode rule
  nor the flush (U4). LinuxIO is stricter on purpose.

## Decision: a job, or the resync's own continuation

| | A: the resync becomes a resumable job | B: the resync continues its own scrub (recommended) |
|---|---|---|
| Who starts it | The health check would have to start a job in the root host. That is a timer-started job, which [Jobs](../jobs.md#not-built) lists as not built, through the job host's socket, which timer jobs avoid on purpose ([Scheduled runs](../jobs.md#scheduled-runs)) | Unchanged: the health check starts `linuxio-btrfs-resync@<uuid>.service` |
| Owner | A job needs an owner, whose router the root host builds (`ownerRouter`). A resync has no owner | root, through the unit. The record owner is `root`, as today |
| Who continues it | The root host, once, at the boot right after the interruption. Never after a crash, a host stopped by hand or a cancel | Its next run, which the health check starts 2 minutes after every boot and every 10 minutes, whenever the pool is whole and in the same episode |
| Tray and bell | A job row with Cancel. A cancel means "never resumed", yet the health check would start a new one | A scheduled-run row with Open, as today. The percent continues from the saved one |
| Cancel and pause | Cancel ends it for good, which contradicts a repair that must happen | **Stop check** pauses it until the next health run, which continues (D2) |
| Admin's view after a power cut | The job is interrupted and resumed at boot. If a disk is missing at that boot, the resume fails with the error "could not be resumed" | Run 1 shows Interrupted (reboot). Run 2 continues at most 2 minutes later. With a disk missing, no run starts until the pool is whole, and then it starts over |
| Disk missing at boot, or a degraded mount | The Resume hook would repeat the health check's conditions, and its failure is an error alert | `markDegraded` starts a new episode, and the health check does not start a resync |
| Second request | Same-target admission refuses with 409 | The unit start merges into the running one (U15). The health check waits while a scrub runs |
| Stores | A job record in `/var/lib/linuxio/jobs/0/`. The scheduled-run record, its run alert and its history kind change | `btrfs-pools.json` gains `resyncRun`. The scrub record's `run` is filled in |
| Contract and UI | A new privileged resumable route, the generated client, `useRecoveredTasks` and terminal-feedback entries, and changes to `alerts.md` | None |

Also rejected:

- **A boot unit that starts resyncs right after the mount.** The health
  check 2 minutes later already does it, after checking that the pool is
  whole.
- **Keeping the resync's progress in `btrfs-pools.json`.** That would
  duplicate the scrub record, which already holds offsets, counters and the
  save cadence.

## Design (B)

### Stores

- **`btrfs-pools.json`.** Each pool gains `resyncRun`, the run ID of this
  episode's resync scrub: `resync-` followed by 128 random bits.
  - It is set by the first resync that scrubs in an episode.
  - It is cleared by `markDegraded` (together with `scrubbed`) and by
    `clearDegraded`.
  - An older LinuxIO drops it when it rewrites the file, and the next resync
    then starts over.
- **`btrfs-scrub.json`.** The resync's record carries `run = resyncRun`
  instead of `""`. The format is unchanged.

### Rules

1. `resyncScrub` reads `resyncRun`, creating it under the state lock when it
   is empty, and scrubs as that run.
2. A record of this run that is `finished` counts as the scrub, with no
   kernel call. This closes F2.
3. A record of this run that is `running` (cut, crash or kill), `canceled`
   (shutdown, Stop check, `systemctl stop`) or `aborted` is continued:
   - A present device that is not done, and that the record names with the
     same device UUID, starts one byte below its offset.
   - A done device is skipped.
   - A device whose scrub aborted is scanned again from its offset. A job's
     resume keeps the abort instead (D3).
   - Any other device starts from 0.
   - The start time and the counters carry over.
4. Any other record (a job's, an older build's, another episode's, or none)
   means every device from 0, as today.
5. `scrubbed` is set only while `resyncRun` is still this run. Otherwise the
   scrub returns `errDiskMissing`, because a new episode has begun.
6. Before any record write that moves an offset or marks a device done, and
   before it returns finished, a scrub flushes each device it scrubs:
   `fsync` of the block device, opened read-only so udev's close-write watch
   does not fire. A failed flush skips that save, so the record keeps the
   last flushed offsets. At the end, a failed flush fails the scrub. This
   applies to every LinuxIO scrub (D4). As built: the flush goes to every
   disk the kernel lists in `/sys/fs/btrfs/<uuid>/devices`, by kernel name,
   since a member's DEV_INFO path can name no device node (`/dev/root`); and
   after a failed final flush the record is the last one a flush preceded,
   counters included, marked aborted.
7. Phase 4: the convert follows a balance the kernel continued at mount, via
   `ResumeBalance` with `paused` false, instead of failing with "a balance
   is already running". This fixes F1.

### Events

| Event | What happens |
|---|---|
| Power cut mid-scrub | Run 1 reads Interrupted (reboot), with no alert. `btrfs/resync-<uuid>` stays up as the same occurrence, so it is not delivered again. At most 2 minutes after boot, run 2 continues; the tray shows "Pool resync: Scrubbing" from the saved percent. It ends with "Resynced", and the "whole again" notice counts the corrections from both parts |
| Orderly shutdown or reboot | The same, with run 1 reading Interrupted (shutdown) |
| **Stop check** on the pool | The run is canceled and counts as a failed attempt, as today. The next health run continues the scrub instead of starting over. Three stops end in the error alert, as today |
| Worker killed | The run alert, as today. The next health run continues |
| Disk missing at boot, and the boot check mounts the pool degraded | `markDegraded` clears `resyncRun`. No resync runs while the pool is degraded. Once it is whole again, a new run scrubs everything |
| Disk missing at boot, and the pool is left unmounted | Nothing is written, so the episode is kept. The scrub continues once the pool is whole |
| **Mount degraded**, or a degraded mount by hand that a health run sees | As "disk missing at boot, mounted degraded" |
| A degraded mount by hand, written to and made whole again between two health runs (at most 10 minutes apart) | Not seen. The continuation skips what its scrub covered before the cut. This is a Known limit |
| Scrub job started from the pool details between attempts | It replaces the resync's record, and the resync later starts over. This is a Known limit (D8) |
| Replace, remove or balance between the cut and the continuation | A changed device UUID starts that device from 0. Moved chunks were written fresh |
| First resync after upgrading from a build without the field | It starts over: the old record's run is `""` |

## Safety

| Must never happen | Prevented by | Checked by |
|---|---|---|
| The pool is declared resynced while part of a disk was not scrubbed | `clearDegraded` runs only after this run's scrub finished on every present device. A device counts as done only by this run's record. Aborted devices are scanned again. A device the record does not name, or names with another UUID, starts from 0 | Unit tests; root test: after the cut and the continuation, the returned disk alone holds every file and scrubs clean |
| The pool is declared resynced while repairs below a saved offset were lost in the cut | The flush before every save and before finished (rule 6). One byte back covers stripes still queued (U7) | Phase 0 (U8, U9). The root test's freeze copy covers completed writes, not a disk cache |
| A continuation skips writes made while a disk was missing | `resyncRun` is tied to the episode: LinuxIO's degraded mounts and every health run that sees the pool degraded clear it, and `scrubbed` counts only for the same run | Unit tests; a Known limit for degraded mounts by hand that no health run sees |
| A continuation runs on the wrong pool | The record and the state are keyed by the pool UUID from `FS_INFO`, and the run must match that UUID's state | Unit tests |
| A continuation runs on the wrong device after device names changed | Devices are matched by devid and device UUID (`DEV_INFO`); paths are never used | Unit tests; the root test, whose loop devices change names when unplugged and plugged back |
| A continuation or a finished scrub skips stale copies on a disk whose writes failed while the pool stayed mounted (review finding F1: the kernel marks no attached disk missing at runtime; since 7.1 it marks one whose block device went away, `btrfs_remove_bdev`) | The record keeps each device's write and flush error counters from when its scan began: a continuation scans a device whose counters changed from 0, a finished scrub of the run does not count once they changed, and a resync during whose scrub they changed fails, naming the device. The check runs again right before the pool is declared whole, after the convert and in attempts that skip the scrub, and withdraws `scrubbed` on a change or a record no longer the run's (review N2); it refuses unreadable counters and a disk gone missing (N1). The counters reach the disk at a transaction commit (`btrfs_run_dev_stats`), so a power cut within one commit interval can lose the rise: a Known limit | Unit tests; root test `writeErrorsFailTheResync` (a device-mapper error target under one disk while the pool stays mounted, once during the scrub and once during the convert) |
| A continuation skips stale copies on a replace target | The target keeps the source's UUID (U10), so it continues from the source's offset; the replace wrote it only from checked copies (U17). The kernel refuses a scrub during a replace | Phase 0 (U10, U17); root test: devid 2 keeps its UUID in `replacePowerCut` |
| A continuation runs after a remove, balance or convert | Moved chunks are fresh (U11). A removed devid is no longer in the plan; an added devid starts from 0 | Review of U11 |
| The resync takes another run's record | Run match (`resumable`) | `TestResumeScrubIgnoresAnotherRunsRecord` and a new resync case |

## API and UI

- **Contract:** no change to apischema or the generated contract, and no
  `make generate`.
- **UI:** no dialog text changes. The tray row, the bell and the alerts are
  as today; only the percent continues instead of restarting.
- **Optional:** a "Continuing the scrub" progress message (D10).

## Docs to update when it lands

- **[Btrfs](../btrfs.md):**
  - "Missing disks": replace "Only a scrub that finished counts … is run
    again" with the continuation and the episode rule, and add the Known
    limits below.
  - "Scrub progress": in "What is saved", `run` becomes "a job's ID or the
    resync's run"; in "When", add the flush; in "Resume", add the resync.
  - The `btrfs-pools.json` row of the files table.
  - The test list in the introduction.
- **[Jobs](../jobs.md):**
  - Scheduled runs: one sentence saying a resync continues through its next
    run, not through the job host.
  - Known limits: the line on resumed scrubs.
  - Testing rows.
  - Remove `scheduled.btrfs_resync` from "Covered at one level only".
- **[Backlog](./backlog.md):** delete the item, and extend the Rejected
  entry "A scrub continued from its saved progress…" to cover the resync.
- **This plan:** delete it and its entry in `docs/TODO/README.md` once every
  phase has landed.

## Tests

### Unit tests (fake kernel)

In `backend/common/btrfs/degraded_test.go` and `scrub_test.go`:

- `TestResyncContinuesItsScrubAfterACut`: a `running` record from another
  boot. Each device's SCRUB starts at offset−1, the start time is kept, the
  counters add up, and the state is cleared.
- `TestResyncContinuesAfterAStop`: the same with a `canceled` record.
- `TestResyncScansAnAbortedDeviceAgain`
- `TestResyncCountsItsFinishedScrub`: no SCRUB call, the convert still runs.
- `TestResyncStartsOverInANewEpisode`
- `TestResyncIgnoresAJobsScrubRecord`
- `TestResyncStartsADeviceWithAnotherUUIDAtZero`
- `TestResyncScrubDoesNotCountForANewEpisode`: the fake scrub hook calls
  `markDegraded`; the run ends with `errDiskMissing` and no failure is
  counted.
- `TestScrubFlushesBeforeSavingAnOffset`: the order is poll, flush, write. A
  failed flush keeps the old offsets, and a failed final flush fails the
  scrub. It uses a `flushDevice` seam.
- Phase 4: `TestResyncFollowsABalanceTheKernelContinued`.

### Root test (`make test-btrfs-integration`)

All in `TestBtrfsDegradedIntegration`.

**Setup:**

- The mirror's images and the state folder move onto a shelf ext4, as in
  `replacePowerCut`.
- Before the disk is lost, the data chunk mkfs made is filled and the data
  chunk size set to 256 MiB through `allocation/data/chunk_size`, the
  smallest it takes (U18; 64 MiB was planned). Files are written and every
  other one deleted, so the writes made while the disk is missing land in
  holes in dev extents above the first, and 16 MiB files keep them in the
  size class the holes have (`btrfs_calc_block_group_size_class`).

**The cut.** It replaces the direct `Resync` call in
`resyncRepairsTheReturnedDisk`:

1. Cap the scrub speed (`scrub_speed_max`).
2. Run `Resync` in a child process, through a new `resync` operation in
   `TestBtrfsIntegrationChild`, saving every second.
3. Wait until the record of `resyncRun` is `running` with corrected errors
   above 0 and less than the full amount scrubbed.
4. Freeze the shelf (`fsfreeze`), copy the images and the state sparsely,
   thaw, kill the child, and wait for the kernel to go idle.

**Boot the copies:**

5. Unplug the originals and plug in the copies.
6. Point the state paths at the copied state.
7. Run `MountDegradedPools`, which must leave the complete pool alone, then
   `MountOfflinePool` as systemd would.

**Continue and check:**

8. Run `Resync` in-process.
9. Assert:
   - The record keeps the cut's start time and is `finished`.
   - The continued part read less than `UsedBytes`, and the total is at
     least 99% of it.
   - Errors were corrected after the cut as well (stale copies lay on both
     sides of the cut).
   - The state is cleared and `resynced` is raised.
10. Run the existing `returnedDiskHoldsEverything` on the copies.

The test fails, rather than passing vacuously, when the cut lands before any
correction or after the end.

**Also:**

- `replacePowerCut` asserts that devid 2's UUID changed (U10).
- Phase 4: a step pauses the resync's convert, remounts, and `Resync` must
  follow the balance to the end.

### VM power-cut check

A new test in `frontend/src/test/vm/jobs-phase3.spec.ts`, which owns the
scrub power-cut test and its `scrubRecord`, `recordedBytes` and `usedBytes`
helpers. Steps:

1. `ensurePool`, then convert data and metadata to raid1 with a soft
   convert: earlier tests may leave raid0. Fill with holes, as in the root
   test.
2. Unmount, remove `vdc` in the guest (`/sys/bus/pci/devices/<addr>/remove`),
   run `btrfs device scan --forget`, and
   `systemctl restart linuxio-btrfs-degraded.service`. After its 30 s grace
   it mounts the pool degraded and records the episode.
3. Write and hash files. Unmount, `echo 1 > /sys/bus/pci/rescan`, and mount
   `/mnt/e2e`.
4. Cap `scrub_speed_max` (rate = max(4 MiB, used/2/200)) and
   `systemctl start linuxio-btrfs-health.service`.
5. Poll until the record of `resyncRun` has saved at least 55% and errors
   were corrected. Run `sync`, then `rig power-cut`.
6. Wait for the `scheduled.btrfs_resync` runs: the first
   `interrupted`/`reboot`, the second `completed` with "Resynced".
7. Assert:
   - The record keeps its start time.
   - The continued part read less than `used`, and the total is at least 99%
     of `used`.
   - `btrfs-pools.json` no longer reads degraded.
   - `btrfs/resync` is resolved and `btrfs/resynced` raised.
8. Stop `linuxio-btrfs-health.timer`, remove `vdb`, mount `vdc` with
   `degraded,ro`, check the hashes, and run `btrfs scrub start -B -r` with 0
   errors. Rescan, mount, and start the timer again.
9. Remove the files.

The test takes about 12 minutes (`test.setTimeout(1_200_000)`). Removing a
virtio disk in the guest on q35 is from memory; if the first run shows it
does not work, add rig subcommands that detach and attach a data disk (D6).

### Independent review

A reviewer who did not write the code gets the diff, this plan's U table and
the root test log. They check:

- U4 and U8–U12 against kernel v6.1, v6.8 and v7.0 and btrfs-progs v6.17
  sources.
- That every path that can write with a member missing ends in
  `markDegraded`.
- That run matching never takes a job's record or an older build's.
- Where the flush happens and how its failure is handled.
- From the log, that the root test cut mid-scrub with corrections on both
  sides of the cut.

Findings are fixed in the same change.

## Steps

| Phase | What | Files | Rough size | Verification | VM runs |
|---|---|---|---|---|---|
| 0 | Read upstream (Phase 0 checks in the U table) | none; findings go into this plan | — | Each claim confirmed or refuted, with file and function, at the tags named | 0 |
| 1 | Resync continuation (rules 1–5) | `degraded.go`, `scrub.go`, `degraded_test.go`, `scrub_test.go`, `docs/btrfs.md`, `docs/jobs.md`, `docs/TODO/backlog.md` | +90 code, +250 tests, ±40 docs | `make check-backend-quiet`, `make check-docs` | 0 |
| 2 | Flush before a save (rule 6) | `scrub.go`, `scrub_test.go`, `docs/btrfs.md` | +50, +80, +8 | `make check-backend-quiet` | Covered by Phase 3 |
| 3 | Root test | `degraded_integration_test.go`, `integration_resume_test.go` (child operation, shared `runChild` and scrub-speed helpers, UUID assertion), docs | +220, +40 | `make vm-test-btrfs-integration`, then again with `VM_RELEASE=noble` (kernel 6.8, before 6.19); the owner runs `make test-btrfs-integration` | 2 |
| Review | Independent review of Phases 0–3 | — | — | Findings fixed; Phase 3 run again if the code changed | 0–1 |
| 4 | The convert follows the kernel's balance (rule 7) | `degraded.go`, `degraded_test.go`, `degraded_integration_test.go`, `docs/btrfs.md` | +10, +60, +60, +4 | `make check-backend-quiet`, `make vm-test-btrfs-integration` | 1 |
| 5 | VM power-cut check | `frontend/src/test/vm/jobs-phase3.spec.ts`, maybe `vm.ts`, `docs/jobs.md` | +200, +30, +6 | `make check-frontend-quiet`; `make vm-reset vm-install`; `make test-vm-browser-rerun PLAYWRIGHT_ARGS='login.spec.ts jobs-phase3.spec.ts'` (iterate with `--grep resync`, then the whole file once) | 2–3 |
| 6 | Close | backlog item and this plan removed | — | `make check-docs` | 0 |

Phases 1–3 land as one change, as the data-safety rule in `AGENTS.md`
requires. Phases 4 and 5 can follow separately.

## Known limits to write

In [Btrfs](../btrfs.md#missing-disks):

- A degraded mount made by hand, written to and made whole again between two
  health runs is not seen. A continued resync then skips what its scrub had
  covered. As written after the review (F2): any degraded writes LinuxIO
  does not record, by hand, on another system, between a mount and its
  record, or after a failed record write; a root the initramfs mounted
  degraded is now recorded by the boot check.
- After the review (F1): a disk's error counters reach the disk at a
  transaction commit, so a power cut within one commit interval of a failed
  write can lose the rise.
- A scrub started from the pool details between two resync attempts replaces
  the resync's saved progress. The resync then starts over.
- A continued resync reads the dev extent where each device stopped again
  (1 GiB at most) and counts its errors twice. A power cut loses up to a
  minute of progress.
- Kernels before 6.4 (Debian 12 runs 6.1, from memory) have no test VM. See
  D11.

## Risks and open questions

- **U8 or U9 refuted by Phase 0.** Settled: both hold, U8 in the form the
  one byte back needs, at every tag read.
- **Debian 12's 6.1 kernel uses the scrub code from before the 6.4
  rewrite.** Only source review covers it: D11. Phase 0 read 6.1 and found
  U8 holds there.
- **The in-guest disk removal may not work on the VM.** Then the rig needs
  disk detach and attach subcommands: D6.
- **The `fsync` of a mounted member is a new kind of device open.** It is
  read-only and writes no data. The reviewer should confirm it sits within
  the `AGENTS.md` rule on device writes.

## Decisions for the owner

- **D1. Design.** Recommendation: B, the resync continues its own scrub
  through its next run; not a job.
- **D2. Stop check on a resync.** Recommendation: the next run continues
  from the saved position rather than starting over.
- **D3. An aborted device in a resync scrub.** Recommendation: scan it again
  from its offset; a job's resume would keep the abort and fail every retry
  in the episode.
- **D4. Flush before saving offsets.** Recommendation: do it for every
  LinuxIO scrub, job and resync alike.
- **D5. Fix F1 (convert cut by a power loss) in this release.**
  Recommendation: yes, as Phase 4.
- **D6. Losing a disk in the VM.** Recommendation: in-guest PCI remove and
  rescan first; rig subcommands only if that fails.
- **D7. Where the VM check goes.** Recommendation: extend
  `jobs-phase3.spec.ts`.
- **D8. Count a scrub job that finished meanwhile as the resync's scrub.**
  Recommendation: no, not now.
- **D9. Scrub only the disks that missed writes (those not in `present`).**
  Recommendation: no, not now.
- **D10. New progress or summary text for a continued run.** Recommendation:
  no new text.
- **D11. Kernels before 6.4.** Recommendation: if Phase 0 cannot confirm U8
  there, start over on those kernels instead of continuing. Phase 0
  confirmed U8 at 6.1, so they continue too.

## Files

`backend/common/btrfs/degraded.go`, `scrub.go`, `health.go`, `devices.go`,
`degraded_integration_test.go`, `integration_resume_test.go`;
`backend/worker/degraded.go`; `frontend/src/test/vm/jobs-phase3.spec.ts`;
`docs/btrfs.md`, `docs/jobs.md`, `docs/TODO/backlog.md`.
