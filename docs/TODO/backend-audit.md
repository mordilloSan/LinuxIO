# Backend Audit Checklist

Read-only audit of `backend/**/*.go` (C auth daemon excluded), done 2026-10-23
on `dev/v0.48.0`, re-scanned 2026-10-24 on `dev/v0.49.0`: every open item was
re-checked against the working tree, and the code changed since the audit plus
the files the first pass never read were reviewed again. Finished items are
removed (their fixes and tests are in git history); only open work is listed.
Numbers are stable, so gaps are finished items.

How to use: items are worked one at a time, in order unless you pick a number.
Remove an item when the fix has landed with its test and
`make check-backend-quiet` (or `make test-quiet` when the frontend contract
moves). Remove this file once every item is done or rejected.

Verification legend:

- `[source]` – the full code path was read; the failing case follows from the code.
- `[host]` – also reproduced on this machine (systemd 259, qemu-img, apt).
- `[lib]` – behaviour of a library confirmed from its source/docs
  (ini.v1 v1.67.3, gopsutil v4.26.9, Go 1.27.3 stdlib, SQLite docs).
- `[worker]` – found and line-checked by a scoped sweep; the cited lines were
  spot-checked but not every caller re-traced.
- `[unverified]` – needs a host check before acting; stated where.

"Behaviour change" says what a user or client will observe differently after the
fix, so each item can be accepted or rejected on that basis.

---

## A. Must fix

- [ ] **8. `journalctl -n <lines>` takes the user's string as the next argv**
  `backend/bridge/handlers/logs/journal_args.go:155-156`, `service.go:33`
  - Verified `[host]`: `journalctl -n --version` on this machine prints the version, i.e. a non-numeric token after `-n` is parsed as an option. `lines:"--vacuum-time=1s"` or `--rotate` runs under a root bridge. Also: `service.go` always passes `-n <lines>`, so `lines:"all"` for service logs is treated as a positional match and fails (general logs omit `-n` for "all").
  - Fix: accept only `all` or `^[0-9]{1,6}$` in both parsers, wrapping `bridgeipc.ErrInvalidArgs` (the general parser has no error return; reuse its `scopeError` field); for service logs omit `-n` when `all`.
  - Behaviour change: any other `lines` value → 400. The frontend only sends numeric strings ("0", "200", "1000", "1500").

- [ ] **9. Create and rename URL-decode raw paths; read/list/stat do not**
  `backend/bridge/handlers/filebrowser/resource_create.go:35`, `resource_patch.go:35,39`
  - Verified `[source]`: the only three `QueryUnescape` calls in the package. Callers send raw paths (`useFileMutations.ts`, `CreateVMDialog.tsx`, `DockerSettingsSection.tsx`) — no `encodeURIComponent` anywhere.
  - Fix: delete the three calls and their error branches; one test with `+` and `%` in a name.
  - Behaviour change: `c++.txt` is created as `c++.txt` (was `c  .txt`); `100%.txt` works (was "invalid path encoding").

## B. Should fix

- [ ] **113. Destroying a pool leaves fstab lines that do not use `UUID=<uuid>`**
  `backend/bridge/handlers/storage/btrfs_pool.go:119-121`
  - Verified `[source]`: only entries whose source equals `UUID=<uuid>` are removed. A pool mounted by `/dev/disk/by-uuid/<uuid>` (curtin's form) or `LABEL=` keeps its line after `btrfs_pool_destroy` wipes the disks; without `nofail` the next boot fails `local-fs.target` and drops to the emergency shell.
  - Fix: before unmounting, remove the entries `btrfsFstabEntries(uuid, mounts)` returns (matches by mountpoint or `UUID=`); for offline pools also match `/dev/disk/by-uuid/<uuid>`.
  - Behaviour change: destroy removes every fstab line that mounted the pool.

- [ ] **114. "Mount" / "Mount degraded" on an offline pool ignores its fstab options**
  `backend/bridge/handlers/storage/btrfs_pool.go:200-212`, `backend/common/btrfs/pool.go:193`
  - Verified `[source]`: the mountpoint comes from fstab, but `btrfs.MountPool` passes only `MS_NOATIME` and `degraded`. With `UUID=x /srv/data btrfs subvol=@data,compress=zstd` the default subvolume (usually the top level) is mounted at /srv/data uncompressed; services then write into the top level and the data looks lost once `@data` is mounted again.
  - Fix: when an fstab entry exists, mount through it (`mount [-o degraded] <mountpoint>` via the existing runner) or pass its filesystem options to `MountPool`.
  - Behaviour change: the pool mounts exactly as fstab describes.

- [ ] **115. The "real dpkg output" panel is empty on real hosts**
  `backend/bridge/handlers/packages/apt_term_log.go:62-63`
  - Verified `[host]`: term.log lines come from a pty and end in `\r\n` (2709 of 3036 lines on this machine); after splitting on `\n`, `line[LastIndexByte(line, '\r')+1:]` is empty and every line is dropped. The test fixture is LF-only.
  - Fix: `strings.TrimSuffix(line, "\r")` before keeping the last `\r` redraw; a CRLF test case.
  - Behaviour change: "Unpacking…", "Setting up…" lines appear in update and capability panels.

- [ ] **116. A failed `wireguard.add_interface` deletes a peer directory that already existed**
  `backend/bridge/handlers/wireguard/add_interface.go:37,58-61`
  - Verified `[source]`: only the config file is checked for existence; on an export failure `os.RemoveAll(peerDirPath(name))` wipes `/etc/wireguard/<name>/`, including older client private keys left there (config moved aside by hand, or a failed best-effort removal). A failed `WriteWireGuardConfig` leaves the exported files behind.
  - Fix: treat an existing peer directory like an existing config (409) and run the same cleanup when `WriteWireGuardConfig` fails.
  - Behaviour change: adding an interface whose name has a leftover peer directory → 409 instead of deleting it.

- [ ] **117. NetworkManager bridge hand-off sends settings NetworkManager rejects**
  `backend/bridge/handlers/network/internal/hostbridge/networkmanager.go:30,146,346-347,362`
  - `[source]` + `[unverified]`: `cloned-mac-address` is sent as a string (D-Bus type is `ay`; the string form is `assigned-mac-address`), `autoconnect-priority` is `int32(1<<31-1)` (range -999..999), and `"bridge": {}` keeps STP on (~30 s before the port forwards, longer than `handoffVerifyTimeout` 20 s). Likely every NetworkManager hand-off fails and rolls back (safely). `handoff_test.go:299` asserts the string form.
  - Host check: one hand-off on a NetworkManager host.
  - Fix: `assigned-mac-address`, priority 999, `"bridge": {"stp": false}` (and `parameters: {stp: false}` for Netplan with the NetworkManager renderer).
  - Behaviour change: NetworkManager hand-offs can complete; hand-off bridges run without STP.

- [ ] **118. VMDK import guard can be bypassed through an embedded descriptor**
  `backend/bridge/handlers/virt/import_disk.go:87-99`
  - `[source]` + `[unverified]`: `validateVMDKDisk` checks only the `KDMV` magic. A sparse header with `capacity = 0` and a `desc_offset` pointing at an embedded descriptor (`RW 8 FLAT "/etc/shadow" 0`), or a `parentFileNameHint`, may make `qemu-img convert -f vmdk` read host files again.
  - Host check: build that header plus a descriptor naming a harmless file, run `qemu-img convert -f vmdk`, grep the output.
  - Fix: `qemu-img info --output=json -f vmdk` first; reject unless there is no `backing-filename` and every `format-specific.data.extents[].filename` is the source file.
  - Behaviour change: such VMDKs → 400; normal sparse/streamOptimized exports import as before.

## C. Should fix (from the sweeps, line-checked, not re-traced)

- [ ] **33. Btrfs: replication shares the hourly snapshot service, so a long send delays snapshots**
  `backend/btrfs-timer/snapshot.go:~74` (`replicateJobs` inside the oneshot), `backend/common/btrfs/unit.go`, `bridge/handlers/storage/btrfs_timer.go:77`
  - `[worker]`: while a multi-hour first send runs, the oneshot service is busy and timer activations are skipped. The units are rendered at runtime (`RenderUnit`/`RenderTimer`, written by `syncBtrfsTimer`), so nothing is packaged.
  - Fix: render a second `linuxio-btrfs-replicate.service`/timer pair with a `replicate run` subcommand, or start it `--no-block` at the end of the snapshot pass. Ordering and cadence are a product decision; existing installs get the new units on the next rule/job save.
  - Behaviour change: snapshots keep their cadence during sends.

- [ ] **36. File operations fail below a directory symlink whose target is absolute**
  `backend/bridge/handlers/filebrowser/fsroot/fsroot.go`, `services/info_operations.go`, `download_stream.go`, `services/safe_open.go`
  - `[lib]`: Go 1.27.3 `os/root.go` `splitPathInRoot` returns `errPathEscapes` for any link target starting with `/`, even at root `/`; `iteminfo.ResolveSymlinksAt` resolves only the last component, so anything below `/home/u/data -> /mnt/data` fails at the intermediate hop. `[unverified]` on a host — reproduce first.
  - Fix: resolve the parent chain with `filepath.EvalSymlinks` at the path-normalisation entry points (parent only, so the link itself can still be moved/deleted) and run `CheckNotProtected` on the resolved path. Large: ~37 `fsroot.Open` sites, path-containment risk.
  - Behaviour change: files under symlinked mounts become usable.

## D. Nice to have

- [ ] **46.** WebSocket reads have no `SetReadLimit` — `backend/webserver/ws/ws.go:279` (upgrades at 166, 219). Upload chunks may be up to 32 MiB (`userconfig/rules.go:43` `ValidChunkSizeMB`), one frame each, so the limit must be ≥ 32 MiB + header: `SetReadLimit(33<<20)` after both upgrades. Behaviour: larger frames close the socket.
- [ ] **47.** No clickjacking/hardening headers — `backend/webserver/web/router.go:26-52` (only `download.go` sets `nosniff`). Nothing in the frontend frames the app. Fix: `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `nosniff`, `Referrer-Policy` in `BuildRouter`; no full CSP. Behaviour: the UI cannot be framed.
- [ ] **48.** Bridge binary (~21 MiB) SHA-256'd on every login — `backend/webserver/bridge/bridge.go:30-58`. Saves ~50 ms per login. If done, cache only successes keyed on `(dev, ino, size, mtime, ctime)` (ctime closes same-size in-place rewrites with a forged mtime). Behaviour: none. Low priority.
- [ ] **49.** Crashed scrub leaves its record "running" forever — `backend/common/btrfs/scrub.go:41`, `inventory.go:259-290` (`scrubStatus` returns `lastScrub` unchanged when no ioctl is live). Fix: map a non-live `"running"` record to `"aborted"`. Behaviour: UI stops showing a phantom scrub.
- [ ] **50.** Remount failure after unmount during rollback leaves the mountpoint empty — `backend/common/btrfs/rollback.go:~271` (`remountStep`). Fix: on `mountFS` failure retry once with only the VFS flags plus `subvol=/<source>` (needs a seam for `mountFS`). Behaviour: safer rollback. `[unverified]`: root integration test.
- [ ] **51.** `DeleteSnapshot` re-lists the whole filesystem per delete — `backend/common/btrfs/snapshots.go:150`. Fix: drop `findSubvolume`, treat `ENOENT` from `subvolOps.destroy` as gone (handle it in `DeleteSnapshot` only — `deleteUnmounted` is shared with rollback; wrap its error with `%w`). Behaviour: faster prunes.
- [ ] **52.** Alerts of deleted/disabled btrfs rules and jobs are never resolved — `resolveStale` (`health.go:134`) covers only health/scrub/space/offline; `snapshotAlertKey` (`runner.go`) and `replication-<id>` (`replication_run.go`) keys are never resolved by `DeleteBtrfsSnapshotRule` (`btrfs_timer.go:~204`) or `DeleteBtrfsReplicationJob` (`btrfs_replication.go:87`). Fix: exported `ResolveRuleAlert`/`ResolveJobAlert` called from delete and from save-as-disabled (not when `BeforeUpdates` still runs it). Behaviour: stale alerts clear.
- [ ] **55.** Every tick marshals every plugin payload and the process list for the collector hand-off, then `WriteSnapshot` marshals the history plugins again — `backend/monitoring/internal/app/handoff.go`, `runtime.go:~391-411`. The sample is read only by requests that arrive during the collector run (`current.go` `awaitCollectorSample`). Fix: hold `data` and marshal lazily in `pluginPayload`/`systemSummary` (`sync.Once`). Behaviour: none.
- [ ] **56.** 30 s schema timeout can make a large index permanently unopenable (FTS rebuild); leftover-generation delete is slow — `backend/indexer/internal/storage/open.go:49` (both `initSchema` and `ensureFTS` under `schemaTimeout`, `policy.go:21`), `generation.go:42` (`BeginGeneration` deletes with FTS triggers active, inside a tx). Fix: run the rebuild on the parent ctx; for the delete, reuse the trigger-bypass path above `ftsPruneBypassEntries` (needs a tx-aware variant). Behaviour: recovery instead of a startup loop.
- [ ] **57.** Hardlink reconcile runs N+1 queries in one transaction — `backend/indexer/internal/storage/hardlinks.go:425` (`reconcileHardlinksAfterReindex`), `mutations.go:80`. Fix: measure first; then aggregate ancestor deltas per directory. Risk: silent wrong directory sizes.
- [ ] **61.** Dir-listing comparator is not a strict weak ordering (`9 < 10 < 1a < 9`); `sort.Slice` leaves case-only ties unordered — `backend/bridge/handlers/filebrowser/iteminfo/utils.go:15-35`. Fix: numeric-stem names first (by number, then string), others by lowercased string, `strings.Compare` tie-break, `slices.SortStableFunc`. Behaviour: stable order for case-only ties.
- [ ] **62.** Task-archive temp file in `/tmp` (tmpfs) — `backend/bridge/handlers/filebrowser/archive_download_operation.go:86` (`os.CreateTemp("", …)`; no unit sets `TMPDIR`). Fix: `/var/tmp`. Behaviour: large folder downloads stop eating RAM.
- [ ] **63.** Cancelled batch skips indexer reconciliation; `reconcileDetached` has one 10 s timeout for all items — `backend/bridge/handlers/filebrowser/batch_operations.go:225-244` (`executeBatchTransfer` returns nil updates on abort; `runDeleteBatchTask` too), `indexer_sync.go:126`. Fix: return the accumulated updates with the abort error and reconcile them; per-item `WithTimeout`. Behaviour: index stays correct after cancel.
- [ ] **64.** Hostnames may start with `-` and reach `showmount -e <server>` — `backend/bridge/handlers/storage/mounts_common.go:26` (`validHostname`), used by `nfs.go:118,316` and `cifs.go:93,370`. Fix: `^[a-zA-Z0-9][a-zA-Z0-9.-]*$` and wrap those errors in `bridgeipc.ErrInvalidArgs` (today plain → 500). Behaviour: such hosts → 400.
- [ ] **66.** `accounts.terminate_session`: `pid, _ := strconv.Atoi(req.PID)` then `SIGHUP` any PID when `sessionId` is empty or logind `TerminateSession` fails — `backend/bridge/handlers/accounts/handlers.go:50`, `user_details.go:204`. The request has no username; the UI always sends a logind `sessionId`. Fix: require `sessionId` (`ErrInvalidArgs`), drop the PID fallback. Behaviour: API-only.
- [ ] **68.** Health summary calls `ListServices` (≈3 extra D-Bus reads per unit) just to count, and ignores its error → "0 failed" — `backend/bridge/handlers/system/services_systemctl.go` (uses D-Bus despite the name), `health.go:23`. Fix: list units without per-unit properties (`ListUnits` already has active/sub state); an error flag needs a `SystemHealthSummary` field (`make generate`, `SystemHealth.tsx`). Behaviour: faster, honest card.
- [ ] **69.** Indexer timer interval accepts `1ns`/`500ms` and writes an invalid `OnUnitActiveSec` — `backend/bridge/handlers/indexer/timer.go:77-97` (errors are plain → 500). Fix: minimum 1 min, errors wrapped in `ErrInvalidArgs`. Behaviour: tiny intervals → 400.
- [ ] **74.** `docker/prune.go:33-49`: the first failing prune step returns `nil, err`, dropping earlier steps' results; `DockerSystemPruneResponse` has no partial/error field. Fix: name what was already pruned in the error text (no contract change), or add a field (`make generate`). Design call first.
- [ ] **75.** ifupdown detect reads every file in `interfaces.d/*` (`ifconfig/backend_ifupdown.go:28`), so `eth0.bak` duplicates the stanza → "ambiguous" blocks edits. Debian's own `source interfaces.d/*` loads backups too (only `source-directory` filters). Fix: skip `~`, `.bak`, `.orig`, `.dpkg-*`, `.swp`, `.save` and dotfiles (not a strict name filter — `*.conf` is real). Behaviour: edits work with backups present.
- [ ] **76.** WireGuard peer names are lost on rewrite: `ParseWireGuardConfig` (`wireguard/config.go:53-80`) never sets `pc.Name` from the `[Peer]` comment that `addPeerSection` writes (`config.go:294`), so every add/update rewrites the other peers without their `# Name`. Fix: set `pc.Name` from the trimmed `# ` comment; round-trip test (keep `TestWriteWireGuardConfigWritesPeerNameAsComment`). Behaviour: peer names survive rewrites.
- [ ] **78.** History averages divide by the total sample count even for keys present in only some samples — `backend/monitoring/internal/plugins/aggregate.go:194` (`count := len(items)`), `averageTemperatures`/`averageFilesystems`/`averageGPUs` (`:367,405,450`) and `averageNetworkInterfaces` (`:349`). Verified `[source]`: a key seen in k of n samples is diluted to k/n. Fix: per-key sample counts in the accumulators. Behaviour: correct history after rollup.
- [ ] **119.** Concurrent `wireguard.add_peer` can lose a peer — `wireguard/peer_file.go:240-243` checks with `Lstat`, then `SaveTo` truncates; the package has no mutex around read-modify-write. Fix: create peer files `O_CREATE|O_EXCL` and serialize the WireGuard mutations with a package mutex. Behaviour: concurrent adds run one after another or get 409.
- [ ] **120.** A merge-move whose source holds a special file (socket, fifo) reports the item failed and skips indexer reconciliation, though its regular children moved — `filebrowser/services/file_operations.go:126-162` (`mergeDirectory`, `forEachChild` skips specials, `Remove` → ENOTEMPTY), `batch_operations.go:255-263`. Fix: record the batch indexer update for the moved children before returning the error. Behaviour: index reflects the move immediately.
- [ ] **121.** `..` in NFS/CIFS mountpoints bypasses the system-path guard — `storage/mounts_common.go:27` (`validPath` allows `..`), `isSystemPath` (`:85-91`) compares the uncleaned path; `/mnt/../etc`, `//etc`, `/etc/.` pass. Fix: the `filepath.Clean(p) != p` check `checkPoolMountpoint` already uses, in `validateNFSMountRequest`/`validateCIFSMountRequest`. Behaviour: such paths → 400.
- [ ] **122.** A cancelled manual SMART refresh postpones the scheduled one for a full interval — `monitoring/internal/app/runtime.go:460-462` marks refreshed after `Refresh` returned early on ctx. Fix: skip `MarkRefreshed` when `ctx.Err() != nil`.
- [ ] **123.** mdraid and eMMC capacity is 8× too large with 4K logical sectors — `monitoring/internal/collector/smart/mdraid.go:214-224`, `emmc.go:173-183` multiply `/sys/block/*/size` (always 512-byte units) by the logical block size. Fix: `sectors * 512`. The UI does not read `capacity_bytes` today.
- [ ] **124.** `[unverified]` NVML PCI info struct may be smaller than what the unversioned `nvmlDeviceGetPciInfo` writes — `monitoring/internal/collector/gpu/nvml.go:75-82,142,162` (36-byte struct; the old `nvmlPciInfo_t` had 4 trailing reserved uints). Host check on an NVIDIA machine, or register `nvmlDeviceGetPciInfo_v3` with its layout.
- [ ] **125.** An unsafe icon label (`My App`, `a/b`) or a missing local icon file still returns 500 and logs "route failed" at Error — `docker/icons.go:566` (`unknown icon type`), `:494` (ENOENT not mapped). Fix: wrap both in `bridgeipc.ErrNotFound`, as b6f6293f intended.
- [ ] **126.** A failed app update is only logged at Warn — `appupdate/app_update_operation.go:142` uses `bridgeipc.NewError(…, exitCode)` (1) and `failureLevel` (`router.go:~755`) treats codes below 500 as Warn; durable Docker update failures carry exit codes too. Fix: Error for codes below 400 as well, or 500 here.
- [ ] **127.** `[unverified]` dpkg history records the old version for `install` lines that carry one (reinstall of a removed-with-config package) — `packages/updates.go:582-583` only takes the new version for `upgrade`. Fix: use the new-version field whenever it is present and not `<none>`.
- [ ] **128.** LVM exit status 5 is lvm2's generic `ECMD_FAILED`, so lock/permission/metadata failures show as "no LVM" — `storage/lvm.go:33-34,53`. Verified `[host]`: as a non-root user `vgs nosuchvg` fails on `/dev/mapper/control` and `/run/lock/lvm` permissions and exits 5, the same code as root's "not found"; `pvs` as non-root fails on `/run/lock/lvm/P_global`. LinuxIO lists all objects (no names), which exits 0 with an empty report when nothing exists. Fix: keep only `exec.ErrNotFound` as "no volumes". Behaviour change: a non-root bridge's LVM listing errors instead of showing an empty list — decide together with 99 (flag the LVM routes privileged).
- [ ] **129.** `[unverified]` External-tool decoders lost v1's tolerance for duplicate keys and invalid UTF-8 under `encoding/json/v2` — `monitoring/internal/live/sensors.go:37-40` (`sensors -j` repeats labels on some drivers, e.g. dell_smm), smartctl/rocm-smi/nvtop/docker-stats, `storage` lsblk parsing. This server's `sensors -j` (checked 2026-10-24) has no duplicate keys and is valid UTF-8, so it needs a machine with an affected driver; `jq` is not a valid check (it silently keeps the last duplicate). Fix: `jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true)` on those decodes.
- [ ] **130.** Samba/exports/fstab rewrites are read-modify-rename without a file lock (since items 2/17b replaced `O_APPEND`), so two simultaneous adds can drop one entry — `shares/samba.go`, `shares/nfs.go` (`appendToConfFile`), `storage/fstab.go`. Fix: `filelock` around each read-modify-write, as `shares/rsync.go` does.

## F. Consistency patterns to unify (one convention each; fix when touching the file, or as one sweep)

- [ ] **95. Delete nil-context guards** (no caller passes a nil ctx): 28 `ctx == nil` guards in 16 files plus `ensureContext` in `indexer/internal/storage` (~25 call sites) and `dbusclient.requireContext` (panics). Disjoint chunks: (a) `indexer/internal/{storage,indexing,daemon}`, (b) `bridge/handlers/packages/**`, (c) `common/filelock`, `common/dbusclient`, `systemd/unitlist.go`, `appupdate/appupdate.go`. In `autoupdate/backend.go:83` and `backend_pkgkit.go:21` drop only the `ctx == nil ||` half.
- [ ] **96. ctx first in `filebrowser/services`**: `CopyFileWithCallbacks`, `MoveFileWithCallbacks`, `ExtractArchive`, `CreateZip`, `ComputeExtractSize` still use `opts.Cancel` (set only in `progress.go:133`) or nothing (`ComputeCopySize`/`computeTransferSize` already take ctx). Callers map `services.ErrAborted` to `abortErr` — keep that working. ~10 call sites, ~47 test sites; conflicts with 9, 63, 110. Low value.
- [ ] **97. One subprocess path**: `hostcmd.LookPath` (no ctx; it is `exec.LookPath` plus a `/usr/sbin`, `/sbin`, `/usr/bin`, `/bin` fallback) vs `exec.LookPath` in `capabilities/probes.go:22`, `wireguard/interface_power.go:70`, `storage/smart.go:34,259`, `virt/preflight.go:260`, `schedules/native.go:98` (and test seams in `install_capability.go`, `virt/appliance_images.go`, `shares/rsync.go`), and ~20 bare command names (`accounts`, `storage/lvm`, `block_devices`, `mounts_common`, `samba`, `appupdate`, `docker`/`dockerupdate` compose, `logs/journal_page.go`, `schedules`). Only behaviour change: tools that live only in sbin start working under a stripped PATH. Chunk by package.
- [ ] **99. `apischema.Privileged()` gating is uneven**: every btrfs mutation is flagged; not flagged: `storage.create_lv/delete_lv/resize_lv/mount_nfs/mount_cifs/unmount_*/remount_*/set_btrfs_maintenance/unmount_filesystem`, all `accounts.*`, NFS/Samba routes (`shares/handlers.go`; rsync is flagged), `network.set_ipv4_manual/set_ipv4/set_ipv6/set_mtu/set_optional/enable_connection/disable_connection`, `control.app_update` (a TaskRunner). A non-root bridge does side effects before the OS refuses. Policy decision first: flagging removes anything polkit lets a non-admin do today. See `docs/privilege_pattern.md`.
- [ ] **102. Logging shape**: `component`/`subsystem` attributes exist on ~138 of 747 slog calls; none on `btrfs`, `storage`, most `monitoring` collectors. Neither `logguard` nor AGENTS.md requires them. Recommend rejecting unless a guard rule is added.
- [ ] **103. Interface name rules**: `network/*` mutations only check non-empty (`network.go:113`, `ifconfig/detect.go:15`); `detect.go:53` joins `"ifcfg-"+iface` (`x/../../y` escapes the directory), `ifup`/`ifdown` get the name as an argument (leading `-`); `virt/networks.go:222-235` allows a leading `-`. Fix: reject `/`, leading `-`, whitespace/control characters and > 15 bytes in `ifconfig.OpenBackend`, allowing `.` and `:` (VLAN/alias names). One shared package is a separate design choice. Hostnames: item 64.
- [ ] **106. Same rule in two places**: (a) `config/apply_operation_settings.go:12` rejects `heavyArchiveConcurrency <= 0` (plain error → 500) while `userconfig/rules.go:87` allows 0 and load/limiter map it to the default — drop the handler check; (b) `datetime/handlers.go:32` compares `EnabledRequest.Enabled == "true"` (`"True"`/`"1"` silently disable NTP) — `strconv.ParseBool` + `ErrInvalidArgs`, or a `bool` contract (`make generate`). The compose label-path part was wrong: config files and `DeleteStack` already host-translate.
- [ ] **107. SQLite conventions (indexer vs monitoring)**: monitoring `store.go:204,351,396`, `maintenance.go:12` use `db.Begin()`/`tx.Exec` without ctx and the fragile `if err != nil { Rollback }`; indexer hand-rolls the rollback in `writer.go:180`, `fts.go:194`, `generation.go:223` instead of `rollbackTx`; bare `defer rows.Close()` in `hardlinks.go:91,127,284,471`. Split indexer/monitoring halves; overlaps 56 and 57.
- [ ] **108. Sorting helpers**: 46 `sort.Slice`/`sort.Strings`/`sort.SliceStable` calls vs 35 `slices.Sort*`. No user value; recommend converting only when touching a file.
- [ ] **109. Drift**: `common/verbosemode/verbosemode.go:34-55` hardcodes the units' `ExecStart=` lines (equal to `packaging/systemd/*.service` plus the verbose flag today) — add a test that pins them to the packaged units; `version.IsNewer` (`compare.go:36`) compares non-numeric parts lexically (`0.48.0-rc1` > `0.48.0`; no pre-release tags exist) — sort any pre-release below its release.
- [ ] **110. Ownership of created files under a root bridge**: only uploads chown to the session user (`upload_operation.go`, root only); `resourcePost` (`resource_create.go`, not a task route, no owner today), upload-batch `MkdirAll` (`upload_batch_operation.go:242,355`), compress output and `ExtractArchive` do not. Product decision: should admin sessions' files be handed to the session user too? Use `Lchown` so extracted symlinks are never followed.
- [ ] **112. PackageKit timeouts for the same kind of work**: single-package install 120 s (`updates.go:729`), single-package update 30 min (`package_update_operation.go:315`, inside a 2 h task), offline download of everything 300 s (`autoupdate/backend_pkgkit.go:148`). Fix: one or two shared constants; values to decide (suggested 30 min each).

## G. Not read / outside this audit

- `_test.go` files were grepped for coverage only, not read.
- The C auth daemon (`backend/auth`) except username validation and result-code mapping.
- Runtime behaviour of exportfs, wg-quick, NetworkManager, netplan, libvirt, qemu-img and dnf logs is from documentation/source reading, except the host checks noted (`journalctl -n`, `qemu-img` VMDK descriptor, apt term.log).
