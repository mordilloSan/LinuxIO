# Backend Audit Checklist

Read-only audit of `backend/**/*.go` (C auth daemon excluded), done 2026-10-23
on `dev/v0.48.0`. Six scoped sweeps, then every must-fix and should-fix item
was re-read in full against the working tree. Nothing in this list has been
changed yet.

How to use: items are worked one at a time, in order unless you pick a number.
Tick the box when the fix has landed with its test and `make check-backend-quiet`
(or `make test-quiet` when the frontend contract moves). Remove this file once
every item is ticked or rejected.

Verification legend:

- `[source]` – I read the full code path; the failing case follows from the code.
- `[host]` – also reproduced on this machine (systemd 259, qemu-img).
- `[lib]` – behaviour of a library confirmed from its source/docs
  (ini.v1 v1.67.3, gopsutil v4.26.9, Go stdlib, SQLite docs).
- `[worker]` – found and line-checked by a scoped sweep; I spot-checked the cited
  lines but did not re-trace every caller.
- `[unverified]` – needs a host check before acting; stated where.

"Behaviour change" says what a user or client will observe differently after the
fix, so each item can be accepted or rejected on that basis.

---

## A. Must fix

- [x] **1. Batch move/copy with "Replace" onto an ancestor deletes the source**
  `backend/bridge/handlers/filebrowser/services/file_operations.go:813-870` (`validateMoveDestination`, `prepareDestination`, `tryRenameMove` L448), `batch_operations.go:65-101` (`planBatchTransfer`)
  - Verified `[source]`: `planBatchTransfer` marks an item `replaced` when the destination exists and types match; `MoveFileWithCallbacks` and `copyWithCallbacksAndRoot` (L65) both call `validateMoveDestination` (only checks "dst inside src") then `prepareDestination` → `RemoveAll(dst)`. Source `/data/foo/foo` into `/data` → dst `/data/foo` is removed, source included. Single `resource_patch` never overwrites (comment L19), so only batch copy/move with Replace is affected. `tryRenameMove` falls back to copy on *any* rename error.
  - Fix: reject `dst` equal to or an ancestor of `src`; fall back to copy only on `EXDEV`; rename the old destination aside and remove it after success. Test `src=/a/b/b, dst=/a/b, overwrite=true`.
  - Behaviour change: that item now fails with a per-item error instead of deleting data. Rename failures other than cross-device (EACCES, EBUSY, …) are reported instead of silently copying — a few moves that "worked" via copy+delete will now error.
  - Done 2026-10-23: `prepareDestination` moves the replaced entry aside (`<dst>.linuxio-replaced-<nanos>`) and the returned `finish` removes it on success or restores it on failure; a source inside the destination is read from its relocated path, so `/data/foo/foo` → `/data/foo` with Replace now works (outer becomes the inner tree; the inner is consumed with the replaced tree, for copy as well as move). `tryRenameMove` falls back to copy only on `EXDEV`. Tests: `TestTransferOntoAncestorReplacesIt`, `TestPrepareDestinationRestoresReplacedEntryOnFailure`, `TestPrepareDestinationRelocatesSourceInsideDestination`.

- [x] **2. `/etc/fstab` is truncated-then-written; mount options are unvalidated**
  `backend/bridge/handlers/storage/fstab.go:156,182`, `mounts_common.go:61-110`
  - Verified `[source]`: `updateFstabEntry`/`removeFromFstab` use `os.WriteFile`; `addToFstab` appends. `cleanMountOptions` only trims; `rejectSensitiveCustomOptions` checks the key per element, so one element `ro,password=x` passes; no check for whitespace/newlines. The UI splits custom options on commas (`CIFSMounts.tsx:100-111`), so the comma case is API-only; the newline case is API-only too. `atomicfile.Write` already exists and is used in `mounts_common.go:228`.
  - Fix: `atomicfile.Write` (keep 0644); one `validateMountOptions` (split on `,`, reject whitespace/control chars, run the sensitive-key check per element) used by NFS and CIFS; skip `#` lines in `addToFstab`/`removeFromFstab`.
  - Behaviour change: none for UI flows. API requests with commas, spaces or control characters inside one option get 400. fstab inode changes on write (temp + rename).
  - Done 2026-10-23: all three fstab writers go through `writeFstab` → `atomicfile.Write(…, 0o644)`; `addToFstab`/`removeFromFstab` skip comment lines; `cleanMountOptions` now splits comma-joined entries and rejects whitespace/control characters (returns an error); CIFS wraps it in `cifsCustomOptions` with the sensitive-key check, NFS uses it alone (NFS legitimately needs `sec=`). Tests in `fstab_test.go`.

- [x] **3. Container label becomes a file path and a URL with no validation** *(re-rated: should-fix, see note)*
  `backend/bridge/handlers/docker/icons.go:82-116` (`parseIconIdentifier`), `:135`, `:385` (`filepath.Join(userIconsDir, value)`), `:240-262` (`fetchURLIcon`), `container.go:254-257`
  - Verified `[source]`: identifier comes from the image label `io.linuxio.container.icon`; the UI requests `docker.get_icon_uri` for every container (`frontend/src/hooks/useDockerIcon.ts:13`). Anything ending in `.png/.svg/.jpg/.jpeg/.webp` is read via `filepath.Join`, which collapses `..`, so `../../../../x.png` reads any such file the bridge can read. `http(s)://` identifiers are fetched by the bridge (5 MB cap).
  - Note on severity: the bytes go back only to the logged-in user, who can already read files and run `curl` through the UI. The real exposure is a GET-based SSRF from the host, triggered just by rendering a container whose image ships the label. Hardening, not privilege escalation.
  - Fix: `^[a-z0-9][a-z0-9._-]*$` for `si:`/`di:`/derived names; `filepath.IsLocal` and no `/` for file names; for URLs resolve the host and reject loopback/private/link-local, with `CheckRedirect`. Table test. Check `docs/docker-icons.md` for the documented forms.
  - Behaviour change: identifiers with `/`, `..`, odd characters, or URLs to internal addresses fall back to the bundled Docker icon.
  - Done 2026-10-23: `parseIconIdentifier` requires names and file names to match `^[A-Za-z0-9][A-Za-z0-9._-]*$` (else `IconTypeUnknown`); the icon `http.Client` dials through `refuseLocalDial`, which rejects loopback, link-local and unspecified addresses (covers redirects and DNS rebinding). Private LAN hosts stay allowed on purpose — homelab icon servers are a real use. Tests: `TestParseIconIdentifierRejectsUnsafeNames`, `TestRefuseLocalDial`. `docs/docker-icons.md` updated.

- [x] **4. Login holds one of 8 auth slots while reading an unbounded body with no timeout**
  `backend/webserver/auth/auth.go:48-59`, `backend/webserver/cmd/root.go:124-127`
  - Verified `[source]`: `Login` takes `authSem` then `jsonv2.UnmarshalRead(r.Body)`; no `MaxBytesReader` anywhere in `webserver/`; the server sets `ReadHeaderTimeout` and `IdleTimeout` only (`WriteTimeout` 0 is intentional for streams).
  - Fix: read and decode the body before taking the slot, wrapped in `http.MaxBytesReader(w, r.Body, 8<<10)` and `http.NewResponseController(w).SetReadDeadline(...)` (needs an `Unwrap()` on `web.responseWriter`, `web/middleware.go:75`).
  - Behaviour change: login bodies over 8 KiB → 400 (limits are 255-byte user / 2047-byte password, so nothing legitimate is affected); slow-body clients are cut off after the deadline.
  - Done 2026-10-23: `Login` decodes the body through `http.MaxBytesReader(8 KiB)` under a 10 s read deadline (`http.NewResponseController`, best effort) *before* taking an `authSem` slot; `web.responseWriter` gained `Unwrap()` so the controller reaches the server's writer. Test: `TestLoginRejectsOversizedBody`.

- [x] **5. Adding a WireGuard peer reuses a disabled peer's IP and overwrites its key file**
  `backend/bridge/handlers/wireguard/add_peer.go:94` (`findNextAvailable(cfg.Peers)`), `update_peer.go:72` (disable → `replacePeerSection(…, nil)`), `peer_file.go:199` (ID = `Peer<offset>`), `:233` (`exportPeerFile` → `SaveTo`, overwrites)
  - Verified `[source]`: used IPs come only from the server config; a disabled peer is removed from it but its exported file (the only copy of the client private key) stays and is still listed by `list_peers` (`loadExportedPeers`). The next add gets the same offset → same `PeerN` ID → same file path → overwritten.
  - Fix: include exported peer files' addresses in the used set (`loadExportedPeers`), and make `exportPeerFile` refuse to overwrite. Test: add, disable, add → different ID and IP.
  - Behaviour change: new peer IDs/IPs skip those held by disabled peers (IDs may become non-contiguous).
  - Done 2026-10-23: `AddPeer` loads the exported peer files and `createNextPeer` reserves their addresses alongside the server config's; `exportPeerFile` refuses to overwrite an existing peer file. Test: `TestAddPeerKeepsDisabledPeerAddressAndFile`.

- [x] **6. Every WireGuard rewrite cuts hook lines at `;`/`#` and drops unknown keys**
  `backend/bridge/handlers/wireguard/config.go:23-26,76-79` (`ini.LoadOptions` without `IgnoreInlineComment`), `peer_file.go:73,107`
  - Verified `[lib]`: ini.v1 `parser.go:280` cuts an unquoted value at the first `#` or `;`; `file.go:464` writes a value containing `#`/`;` wrapped in backticks, which wg-quick's `eval` treats as command substitution. The model has no `Table`, `FwMark`, `SaveConfig`, `DNS` keys, so an adopted config loses them on the first rewrite.
  - Nuance for the fix: wg-quick's own parser strips everything after `#`, so LinuxIO must keep stripping `#` comments itself (e.g. `ListenPort = 51820 # main`) and only preserve `;`. `IgnoreInlineComment: true` plus a manual `#` cut matches wg-quick exactly. Reject `\r\n` in hooks. Longer term, edit the loaded `ini.File` instead of regenerating.
  - Behaviour change: hooks containing `;` survive rewrites. Configs already truncated are not repaired.
  - Done 2026-10-23: `loadWGINI` strips inline `#` comments on value lines itself (wg-quick semantics, whole-line metadata comments untouched) and loads with `IgnoreInlineComment`, so `;` is plain text; both writers use `IgnoreInlineComment` (no backtick wrapping); unknown `[Interface]` keys (`Table`, `FwMark`, `SaveConfig`, `DNS`, …) round-trip through `WireGuardConfig.Extra`; `cleanLines` rejects hooks containing `#`, backticks or line breaks. Tests: `TestWireGuardConfigKeepsSemicolonHooksInlineCommentsAndUnknownKeys`, `TestCleanLinesRejectsHooksTheConfigCannotCarry`.

- [x] **7a. Btrfs prune stops at the first snapshot it cannot delete**
  `backend/common/btrfs/runner.go:103-108`
  - Verified `[source]`: `runRule` returns on the first `DeleteSnapshot` error; `Run` records the error, raises the alert and moves to the next rule. A mounted (browsed) oldest snapshot blocks all newer pruning for that rule, every run.
  - Fix: continue the loop, `errors.Join` the failures, keep appending to `report.Pruned`.
  - Behaviour change: the run report and alert text list every failed snapshot; everything deletable is pruned.
  - Done 2026-10-23: `runRule` keeps pruning past a failed `DeleteSnapshot` and returns `errors.Join` of the failures (`report.Pruned` keeps the successes; a done ctx still returns `ctx.Err()`). Test: `TestRunPruneContinuesPastUndeletableSnapshot`.

- [x] **7b. Local prune can delete the incremental parent, so replication falls back to full sends**
  `backend/btrfs-timer/snapshot.go:58-73` (prune runs before `replicateJobs`), `backend/common/btrfs/replication.go:207` (`ChooseParent` only uses a local snapshot the target already holds)
  - Verified `[source]`: nothing protects the last-sent snapshot (`rg LastSent|protect` finds nothing). With `keep.hourly=1` the previous snapshot is pruned before the send.
  - Fix: either replicate before pruning in the timer pass, or exclude the newest snapshot the target holds from `Prune`'s result for rules that have a replication job.
  - Behaviour change: one extra local snapshot is kept per replicated subvolume, or timer ordering changes.
  - Done 2026-10-23: no persistence and no timer change — `protectReplicationParent` keeps the two newest timeline snapshots out of the prune result when an enabled timeline replication job matches the rule's (UUID, path); covers the timer pass and the bridge's run-now path, since both go through `Run`. A broken jobs file is logged and pruning proceeds as before. Test: `TestRunKeepsReplicationParent`.

- [ ] **8. `journalctl -n <lines>` takes the user's string as the next argv**
  `backend/bridge/handlers/logs/journal_args.go:155-156`, `service.go:33,59-61`
  - Verified `[host]`: `journalctl -n --version` on this machine prints the version, i.e. a non-numeric token after `-n` is parsed as an option. `lines:"--vacuum-time=1s"` or `--rotate` runs under a root bridge. Also found: `service.go` always passes `-n <lines>`, so `lines:"all"` for service logs would be treated as a positional match and fail (general logs omit `-n` for "all").
  - Fix: accept only `all` or `^[0-9]{1,6}$` in both parsers; for service logs omit `-n` when `all`.
  - Behaviour change: any other `lines` value → 400. The frontend sends numeric strings (`useLogStream.ts` defaults "200"/"0", `PAGE_SIZE`).

- [ ] **9. Create and rename URL-decode raw paths; read/list/stat do not**
  `backend/bridge/handlers/filebrowser/resource_create.go:29`, `resource_patch.go:34-38`
  - Verified `[source]`: the only three `QueryUnescape` calls in the package. Callers send raw paths: `frontend/src/routes/_authenticated/filebrowser/-hooks/useFileMutations.ts:83,107,256`, `vm/-components/CreateVMDialog.tsx:227`, `settings/-components/docker/DockerSettingsSection.tsx:90` — no `encodeURIComponent` in any of them.
  - Fix: delete the three calls; one test with `+` and `%` in a name.
  - Behaviour change: `c++.txt` is created as `c++.txt` (was `c  .txt`); `100%.txt` works (was "invalid path encoding"). A client that *did* percent-encode would now get literal `%2F` in names — none in this repo.

## B. Should fix

- [x] **10a. Docker standalone update: a failed stop clears the recovery journal and leaves the container down**
  `backend/common/dockerupdate/standalone.go:228-230`
  - Verified `[source]`: stop-error branch → `errors.Join(err, clearStandaloneJournal(journal))`, no restore; the rename-error branch two lines below calls `restoreOriginalRunningState` then `clearJournalAfterRollback`. A ctx cancel/timeout after dockerd already stopped the container hits the first branch.
  - Fix: mirror the rename branch.
  - Behaviour change: after a stop failure the container is restarted when it was running; the journal is cleared only after that rollback attempt.
  - Done 2026-10-23: `parkStandaloneOriginal` calls `restoreOriginalRunningState` then `clearJournalAfterRollback` on a stop error, like the rename branch. Covered by the "stop" case of `TestUpdateStandaloneContainerStopsBeforeMutationWhenPreparationFails`.

- [x] **10b. `ctr.Names[0]` indexed without a length check**
  `backend/common/dockerupdate/compose_target.go:244,255` (guard exists in `status_store.go:349` `PrimaryContainerName`)
  - Verified `[source]`. A container with an empty `Names` slice panics the root update worker.
  - Fix: use `PrimaryContainerName`.
  - Behaviour change: none.
  - Done 2026-10-23: both sites in `compose_target.go` use `PrimaryContainerName`.

- [x] **11. A progress/error message cut mid-rune makes a durable-task record unreadable forever**
  `backend/common/durabletask/store.go:159-166` (`AppendProgress` byte cut), `:394-403` (`ApplyExecutorResult`, no clamp), `:608-611` (`readRecord` validates), `:635` (>512), `:646` (>1024); `backend/common/dockerupdate/durable_worker.go:49-53` (`BoundedMessage` byte cut)
  - Verified `[source]+[lib]`: `encoding/json` replaces each invalid UTF-8 byte with U+FFFD (3 bytes). `ListActiveForUID` (L272) and `activeRouteExists` (L502) return the first read error, so one bad record blocks every listing and every `ExclusiveRoute` claim; prune skips unreadable records.
  - Fix: one rune-safe clamp (`strings.ToValidUTF8` + cut on a rune boundary; `alerts.truncate` already does this) used in `AppendProgress`, `ApplyExecutorResult` and `BoundedMessage`. Optionally make the two listings skip-and-log like `terminalRecordsByUID`.
  - Behaviour change: none visible (messages are clamped to the same limits, now rune-safe).
  - Done 2026-10-23: `durabletask.ClampMessage` (`strings.ToValidUTF8` + cut on a rune boundary) used by `AppendProgress` (512), `ApplyExecutorResult` (1024) and `dockerupdate.BoundedMessage` (1024). Test: `TestClampMessageKeepsRecordReadable` (write → `readRecord` round trip). The optional skip-and-log in the two listings was not done.

- [x] **12a. PackageKit loops spin at 100% CPU on a closed signal channel; collectors treat timeout as "finished"**
  `backend/bridge/handlers/packages/internal/packagekit/packagekit.go:176` (`CollectPackageIDs`), `package_update_operation.go:330` (`awaitPackageUpdateSignals`), `updates.go:197,235` (`isTransactionFinished`, `collectUpdatePackages`)
  - Verified `[source]`: `dbusclient/signals.go:261,291` close `sub.ch` on bus drop; a closed channel yields nil immediately, forever; `AwaitFinished` (L153) already returns an error.
  - Fix: `sig, ok := <-ch; if !ok { return err }`; collectors return `ctx.Err()` on timeout/close.
  - Behaviour change: a bus drop fails the operation with an error (was: spin until the 30 min/30 s timeout); a slow update check returns an error instead of a partial/empty "up to date".
  - Done 2026-10-23: `pkgkit.ErrSignalsClosed`; `CollectPackageIDs`, `awaitPackageUpdateSignals` (now takes the signal channel), `collectUpdatePackages` and `collectSingleUpdateDetail` return it on a closed channel and `ctx.Err()` on timeout; `isTransactionFinished` removed; `getUpdatesBasic` propagates the error. Tests: `TestCollectPackageIDsFailsOnClosedChannel`, `TestPackageKitSignalLoopsFailOnClosedChannel`, `TestCollectUpdatePackagesReturnsContextErrorOnTimeout`.

- [x] **12b. Cancelling a capability installer does not stop its children**
  `backend/bridge/handlers/packages/install_capability.go:382-383` (`readers.Wait()` before `cmd.Wait()`)
  - Verified `[source]`: pipe readers block until every child holding the pipe exits; `cmd.Cancel`/`WaitDelay` never get a chance.
  - Fix: `cmd.Stdout/Stderr` writers plus `cmd.WaitDelay`, or `Setpgid` with `cmd.Cancel` signalling the group.
  - Behaviour change: cancel/timeouts actually kill `apt-get`/`dnf` children.
  - Done 2026-10-23: `runCapabilityProcess` streams through `io.Pipe` writers, sets `cmd.WaitDelay` (5 s), runs the child in its own process group (`Setpgid` unless `Setsid` is already set) and `cmd.Cancel` sends SIGTERM to the group. Test: `TestRunCapabilityProcessCancelStopsPipeHoldingChild`.

- [x] **13. Indexer dir-size uses case-insensitive, non-indexed `LIKE`; blank search terms sort the whole table**
  `backend/indexer/internal/storage/queries.go:85,146-156` (`appendSearchBaseFilter`, `DirDetails`), `:89-111` (`buildSearchFilter` → `1 = 1`), `paths.go:23-30` (`subtreeBounds`, already explains the LIKE problem)
  - Verified `[source]+[lib]`: SQLite `LIKE` is ASCII case-insensitive by default and no `case_sensitive_like` pragma is set; the two COUNT subqueries scan the whole generation. `q=|||` passes the 3-rune check and all terms trim to empty.
  - Fix: `countEntriesUnderPath`/`subtreeBounds` in `DirDetails` and the base filter; return empty (or 400) when no term is non-blank; delete `SubtreeLikePattern`. Test with `/a/Data` vs `/a/data` and `"|||"`.
  - Behaviour change: dir sizes become case-sensitive (correct); blank queries return nothing (was 100 arbitrary newest entries).
  - Done 2026-10-23: `DirDetails` and `appendSearchBaseFilter` use `subtreeBounds` range filters (indexed, case-sensitive); `SearchEntriesUnder` returns empty when no term is non-blank; `SubtreeLikePattern` removed. Tests: `TestDirDetailsIsCaseSensitive`, `TestSearchEntriesUnderBlankTermsReturnNothing`.

- [x] **14a. Monitoring: `statfs` runs under the app mutex**
  `backend/monitoring/internal/live/filesystems.go:53-90`, `app/live.go:140` (passes the `App` as the `sync.Locker`)
  - Verified `[source]+[lib]`: gopsutil `UsageWithContext` ignores ctx (`disk/disk_unix.go:13`); a hung NFS mount blocks in D-state while holding the lock; the collector tick blocks behind it.
  - Fix: take the tracked snapshot under the lock, run the statfs calls outside it, guard `f.cache` with its own small mutex.
  - Behaviour change: none in data; the daemon stays responsive with a stale NAS.
  - Done 2026-10-23: `Filesystems.Info` holds the app lock only for `freshTrackedFilesystems`, runs statfs outside it, cache guarded by `Filesystems.mu`. Test: `TestFilesystemInfoDoesNotHoldAppLockDuringUsage`.

- [x] **14b. Failing SMART refresh retries every tick, inline in the collector**
  `backend/monitoring/internal/app/runtime.go:403,413-425,451`, `collector/smart/smart.go:81-91`
  - Verified `[source]`: `MarkRefreshed` only after success, so `RefreshDue` stays true; `smartctl --scan`/`-a` (15 s timeout each) runs on every tick on hosts with no SMART data, blocking the collector goroutine.
  - Fix: `MarkRefreshed` regardless of result; run the refresh from its own goroutine owned by `collectorWG`.
  - Behaviour change: a failed refresh is retried once per `smart_refresh_interval`; SMART data timing shifts slightly.
  - Done 2026-10-23: `refreshSmart` calls `MarkRefreshed` regardless of the result; `runSmartRefresher` goroutine on `collectorWG` (1-minute due check) replaces the inline `refreshSmartIfDue`; the first refresh now runs just after startup instead of inside it. Test: `TestFailedSmartRefreshWaitsOneInterval`.

- [x] **15a. Network total bandwidth underflows when an interface disappears**
  `backend/monitoring/internal/collector/network/network.go:264-271` (`computeBytesPerSecond`), `:273-295` (`applyNetworkTotals`)
  - Verified `[source]`: `(sum now − sum before)` on `uint64` over the NICs present now; per-NIC deltas (L232-247) already tolerate resets. On VM stop (`vnetN` gone) → WARN, `Initialize()` wipes all baselines, 0 sample stored.
  - Fix: sum the per-NIC `upDelta`/`downDelta`; delete `computeBytesPerSecond` and the reset path.
  - Behaviour change: no spurious zero/WARN on NIC removal; otherwise identical numbers.
  - Done 2026-10-23: `sumAndTrackPerNicDeltas` returns the summed per-NIC rates; `computeBytesPerSecond`, `applyNetworkTotals` and the 10 GB/s reset path are gone; `ioStats` keeps only `Time`. Test: `TestSumAndTrackPerNicDeltasToleratesVanishedInterface`.

- [x] **15b. Docker per-container baselines are never pruned for removed containers**
  `backend/monitoring/internal/collector/docker/docker.go:625-640` (only `deleteContainerStatsSync`), `:140` (called only for still-listed containers)
  - Verified `[source]`: three `map[uint16]map[string]…` grow with every container ever seen per cache key; `ReseedFromCollector` clones them.
  - Fix: at the end of `GetStats`, delete entries for ids not in `validIds`.
  - Behaviour change: none.
  - Done 2026-10-23: `pruneBaselinesLocked` at the end of `GetStats` under `containerStatsMutex`. Test: `TestPruneBaselinesDropsRemovedContainersAcrossCacheKeys`.

- [x] **16. VMDK import lets a crafted descriptor read host files into the guest disk**
  `backend/bridge/handlers/virt/import_disk.go:21-28,120-130` (qcow2-only guard), `templates.go:257-285` (`validateTemplateDisk`)
  - Verified `[host]`: a descriptor with `RW 8 FLAT "/abs/path/file" 0` converted with `qemu-img convert -f vmdk` on this machine produced an image containing that file's bytes (scratchpad test, harmless files). An OVA's disk is the realistic route.
  - Fix: require the `KDMV` magic at offset 0 for `.vmdk` (direct and OVA-extracted); reject text descriptors. Test with a descriptor file.
  - Behaviour change: flat/split VMDK descriptors are rejected; sparse and streamOptimized (what OVA exports use) still import.
  - Done 2026-10-23: `validateVMDKDisk` requires the `KDMV` magic; `importDiskFile` runs it for `.vmdk` (direct and OVA-extracted) before `qemu-img convert`. Test: `TestImportDiskFileRejectsVMDKDescriptor`. Limit: only the magic is checked; an embedded descriptor inside a sparse file is not inspected.

- [x] **17a. New share directories are created 0700**
  `backend/bridge/handlers/shares/samba.go:71,141`, `nfs.go:85` (`MkdirAll(path, 0755)`), `backend/bridge/cmd/root.go:74` (`Umask(0o077)`)
  - Verified `[source]`: no chmod after mkdir; `schedules/store.go` already works around the umask.
  - Fix: `Chmod(0o755)` when the directory was newly created.
  - Behaviour change: new share directories are 0755 (non-root clients can enter them).
  - Done 2026-10-23: `mkdirShareDir` (chmod 0755 only when newly created) in `ensureSmbConfDir`, `CreateSambaShare`, `CreateNFSShare`. Tests: `TestCreateSambaShareDirectoryIsWorldTraversableDespiteUmask`, `TestCreateNFSShareDirectoryIsWorldTraversableDespiteUmask`.

- [x] **17b. `smb.conf` and `/etc/exports` rewritten with `os.WriteFile`; append may glue lines**
  `samba.go:208,231`, `nfs.go:142,179` (truncating writes), `nfs.go:91`, `samba.go:147` (append without checking the trailing newline)
  - Verified `[source]`. `rsync.go` in the same package already uses `atomicfile` + `filelock`.
  - Fix: `atomicfile.Write(path, data, 0o644)`; for create, read-append-write and prefix `\n` when needed.
  - Behaviour change: file inode changes on write; otherwise none.
  - Done 2026-10-23: `atomicfile.Write` in Samba/NFS update and delete; `appendToConfFile` (fresh-line append, atomic) for both create paths. Tests: `TestCreateSambaShareAppendsOnFreshLine`, `TestCreateNFSShareAppendsOnFreshLine`, `TestNFSShareRewriteKeepsOtherLines`.

- [x] **17c. Samba delete/update accept reserved sections (`[global]`, `[homes]`, `[printers]`)**
  `samba.go:162-166` (checks `newName` only), `:216` (`DeleteSambaShare`, no check)
  - Verified `[source]`.
  - Fix: reserved-name check on `oldName` and `name` at the top of both.
  - Behaviour change: those calls return an error (the UI never offers them).
  - Done 2026-10-23: reserved check on `oldName` in `UpdateSambaShare` and `name` in `DeleteSambaShare`. Test: `TestSambaUpdateAndDeleteRejectReservedSections`.

- [x] **17d. NFS export with no clients writes a host-less line; hosts/options unvalidated**
  `nfs.go:287-296` (`formatExportLine`), `:66-101` (`CreateNFSShare`)
  - Verified `[source]`: `clients: []` writes `"/srv/x\t"`; `parseExportLine` then skips it as malformed, so it is invisible in the UI. Hosts/options with whitespace or `\n` inject extra lines. `[unverified]`: that nfs-utils treats a host-less line as `*` — `exportfs` is not installed here; check `man 5 exports` on a target host before relying on the "world export" claim (the invisible-line part is verified regardless).
  - Fix: require `len(clients) > 0`; validate host (`^[A-Za-z0-9_.:*/@\[\]-]+$`) and options (`^[a-z_]+(=[0-9A-Za-z_]+)?$`); `filepath.Clean` the path and reject `/`.
  - Behaviour change: empty client lists and odd host/option strings → 400.
  - Done 2026-10-23: `validateNFSExport` (clean path, reject `/`, ≥1 client, host/option regexes) in `CreateNFSShare`/`UpdateNFSShare`; duplicate and update matching compare cleaned paths. `exportsFile`, `nfsServerAvailable`, `applyNFSExports` are now test seams. Tests: `TestNFSShareRejectsUnsafeInput`, `TestCreateNFSShareDuplicateCheckUsesCleanedPath`. `DeleteNFSShare` still matches the raw path.

- [x] **18a. Account existence checks use the login-user listing**
  `backend/bridge/handlers/accounts/passwd.go:83-88` (filter: UID ≥ 1000 or root, login shell), `:128-136` (`GetUser` → `ListUsers`), callers `user_commands.go:77,102,150,169,190`, `groups.go:249`, `user_details.go:65`
  - Verified `[source]`: editing a group with a system member (`adm`/`syslog`, `docker`/service users) fails "user not found"; a user switched to `nologin` can no longer be deleted/unlocked/modified; `CreateUser` rollback via `DeleteUser` has the same filter. `ListUsers` also runs the full login-history dump (`getLastLogins` → `loginhistory.FetchByUser`) per call, once per member in `ModifyGroupMembers`.
  - Fix: `user.Lookup` (os/user) for existence checks; keep the filter and the history dump in `ListUsers` only.
  - Behaviour change: system/nologin accounts become manageable through the API (the UI still lists only login users); group edits with system members succeed.
  - Done 2026-10-23: `requireUser` (`os/user` lookup behind a test seam) replaces `GetUser` in the five mutation paths and `ModifyGroupMembers`; `ListUsers` keeps its filter and the history dump; `GetUser` remains only for `GetUserDetails`. Test: `TestRequireUserAcceptsSystemAccounts`.

- [x] **18b. `modify_user` ignores "remove all secondary groups"**
  `user_commands.go:121` (`len(req.Groups) > 0`), `frontend/src/routes/_authenticated/accounts/-components/EditUserDialog.tsx:87-90` (sends `selectedGroups`, possibly `[]`)
  - Verified `[source]`.
  - Fix: `req.Groups != nil` → `-G` with the joined list (empty string clears).
  - Behaviour change: unticking the last group now works (`usermod -G ""`).
  - Done 2026-10-23: `modifyUserArgs` (pure, testable) uses `req.Groups != nil`. Test: `TestModifyUserArgsEmptyGroupsClearsSecondaryGroups`.

- [x] **19a. SMTP envelope uses the display-name form**
  `backend/common/alerts/deliver.go:143-148`, `settings.go:107-118`
  - Verified `[source]+[lib]`: `validate` accepts `Name <addr>`; `net/smtp` `Mail(from)` sends `MAIL FROM:<from>` verbatim → 501/553.
  - Fix: use `mail.ParseAddress(...).Address` for `Mail`/`Rcpt`; keep the raw string for headers.
  - Behaviour change: emails with display names are delivered; headers unchanged.
  - Done 2026-10-23: `envelopeAddress` feeds `Mail`/`Rcpt` in `sendEmail`. Test: `TestSendEmailEnvelopeUsesBareAddresses` (strict fake SMTP server).

- [x] **19b. Webhook secret URL logged and returned on transport errors**
  `deliver.go:83-85` (`return err` from `Do`), `:90` (status branch already redacts)
  - Verified `[lib]`: `*url.Error.Error()` includes the full URL.
  - Fix: unwrap `*url.Error` to its `Err` before returning.
  - Behaviour change: log/test-notification text no longer contains the URL.
  - Done 2026-10-23: `postWebhook` unwraps `*url.Error` to `.Err`. Test: `TestPostWebhookTransportErrorOmitsURL`.

- [x] **19c. Alert store hard-fails at 200 alerts, nothing evicts**
  `store.go:32,227-228` (`Raise`), `:260-271` (`Dismiss` only sets `DismissedAt`)
  - Verified `[source]`.
  - Fix: when full, drop the oldest dismissed alert (then the oldest by `LastOccurrence`) before appending.
  - Behaviour change: old dismissed alerts disappear from history once the store is full; new alerts are never lost.
  - Done 2026-10-23: `Raise` evicts via `evictIndex` (oldest dismissed, else oldest) when full. Test: `TestRaiseEvictsWhenStoreIsFull`.

- [x] **20a. Public `/api/version` forks 4 processes per request, uncached**
  `backend/webserver/appupdate/routes.go:12` (no `RequireSession`), `version.go:129-175`
  - Verified `[source]`; the frontend polls it to detect a restart (`frontend/src/api/webserver.ts:94-110`).
  - Fix: cache the map for a few seconds behind a mutex, or `singleflight`.
  - Behaviour change: version may be ≤ TTL stale; a restart clears the cache, so restart detection is unaffected.
  - Done 2026-10-23: `cachedComponentVersions` (5 s TTL behind a mutex; a cancelled request is not cached) fronts `handleVersion`. Test: `TestVersionHandlerCachesProbesWithinTTL`.

- [x] **20b. TLS renewal writes key then cert non-atomically; mismatch bricks startup**
  `backend/webserver/tlsserver/certificate.go:173-183` (`persistCertificatePair`), `:84-96` (`loadCertificatePair` error is fatal)
  - Verified `[source]`: a crash between the two writes leaves new key + old cert; `X509KeyPair` fails on every start.
  - Fix: on a pair mismatch regenerate (fall through to `generateSelfSignedCert`); keep the "incomplete pair" error for a single missing file.
  - Behaviour change: self-heals with a new self-signed cert (browser trust prompt again) instead of failing to start.
  - Done 2026-10-23: `loadCertificatePair` returns `errCertificatePairInvalid` on `X509KeyPair` failure and `loadOrCreateSelfSignedCert` regenerates (Warn log); any unparseable pair, not only a mismatch, is regenerated. Test: `TestLoadOrCreateSelfSignedCertRegeneratesMismatchedPair`.

- [x] **20c. Login ignores the request context; cancelled logins leave orphan bridges**
  `backend/webserver/authclient/client.go:55-57` (`DialTimeout`, no ctx), `bridge/bridge.go:63` (`StartBridge`, no ctx), `auth/auth.go:69`
  - Verified `[source]`: the handler keeps going after the client is gone, creates the session, spawns the bridge; the cookie is never delivered; the bridge lives until the 15 min idle GC and holds a login slot meanwhile.
  - Fix: `Authenticate(ctx, …)` with `DialContext` + `context.AfterFunc` close; `StartBridge(ctx, …)`; delete the session when `r.Context().Err() != nil`. Update the `startBridge` test seam.
  - Behaviour change: cancelled login requests create no session.
  - Done 2026-10-23: `authclient.Authenticate(ctx, …)` (`DialContext` + `context.AfterFunc` close, disarmed before the yamux handoff), `bridge.StartBridge(ctx, …)`, `Login` deletes the session when the request context is already done. Tests: `TestAuthenticateCancelledContextClosesConnection`, `TestLogin_CancelledRequest_LeavesNoSession`.

- [x] **21. Bad login input returns 500 "could not start the session bridge"**
  `backend/webserver/auth/auth.go:72-99` (only `IsUnauthorized` codes map to 401/403), `backend/common/ipc/auth/auth.go:75-92` (`checkField`), C daemon `PROTO_RESULT_BAD_REQUEST` for empty/space/control-char usernames
  - Verified `[worker]` for the C side; `[source]` for the Go mapping. A trailing space from paste (`"bob "`) → HTTP 500, an Error-level log, and a false "bridge failed" message.
  - Fix: map `authipc.ResultBadRequest` and a new `ErrInvalidField` sentinel to 400 `invalid_request` at Warn level.
  - Behaviour change: such logins get 400 instead of 500.
  - Done 2026-10-23: `authipc.ErrInvalidField` (wrapped by `checkField`) and `ResultBadRequest` map to 400 `invalid_request` at Warn in the new `writeStartBridgeError`. Tests: `TestLogin_InvalidInput_MapsTo400`, `TestWriteAuthRequest_RejectsUnrepresentableFields`.

## C. Should fix (from the sweeps, line-checked, not re-traced)

- [x] **22. Router: cancelled queued tasks still occupy the route queue**
  `backend/bridge/internal/bridgeipc/router.go:536,577,690-706`, `tasks.go` (`Cancel`)
  - `[source]`: `queuedByRoute` entries are only removed by `dequeueStartLocked`; cancelling 16 queued tasks behind one long task → `ErrQueueFull` (429) until it ends.
  - Fix: prune terminal entries in `reserveTaskAdmission` before the limit check. Router test.
  - Behaviour change: new tasks can start after cancelling queued ones.
  - Done 2026-10-23: `reserveTaskAdmission` drops terminal entries from `queuedByRoute` before the queue-limit check. Test: `TestRouterCanceledQueuedTasksFreeQueueSlots`.

- [x] **23. Unsynchronised memo map in `GetNetworkInfo`**
  `backend/bridge/handlers/network/log_unit.go:38-66`, `network.go:47`
  - `[source]`: `networkLogUnitCache` is a plain map written on first use; `GetNetworkInfo` runs outside `networkMutationMu` on concurrent request goroutines → possible `fatal error: concurrent map writes` (kills the bridge).
  - Fix: `sync.Mutex` or `sync.OnceValue` per backend.
  - Behaviour change: none.
  - Done 2026-10-23: `resolveNetworkLogUnit` holds `networkLogUnitCacheMu` for its whole body. Test: `TestResolveNetworkLogUnitIsSafeForConcurrentCallers` (race detector).

- [x] **24. NTP server strings written into root-owned config unvalidated; `datetime` has no tests**
  `backend/bridge/handlers/datetime/datetime.go:211,365`
  - `[worker]`: an entry with `\n` injects directives into `timesyncd.conf.d/linuxio.conf` or `chrony.conf`.
  - Fix: `^[A-Za-z0-9.:\[\]-]{1,253}$` per entry; table tests for `replaceManagedBlock`, `findManagedBlock`, parsers.
  - Behaviour change: invalid entries → 400.
  - Done 2026-10-23: `validNTPServer` check in `SetNTPServers` (400 via `bridgeipc.NewError`); new `datetime_test.go` with tables for the validation, `findManagedBlock`, `replaceManagedBlock`, `parseTimesyncdServers`.

- [x] **25. WireGuard `add_interface` can overwrite a live interface; port/MTU/addresses unvalidated; export errors swallowed**
  `backend/bridge/handlers/wireguard/add_interface.go:28-62,74-125`
  - `[worker]`: no existence check on `configPath(name)`; `interfaceUsingPort` only in update; `listenPort` unranged; `normalizeAddresses` keeps non-CIDR; `exportInterfacePeerConfigs` logs and continues.
  - Fix: stat-and-refuse; reuse the update validators; return export errors; `net.ParseCIDR` up front.
  - Behaviour change: duplicate names and invalid values → error instead of partial writes.
  - Done 2026-10-23: `AddInterface` stats the config first (409 if it exists); `parseAddInterfaceRequest` validates CIDRs (`validateCIDRs`), port range + `interfaceUsingPort`, MTU range; `exportInterfacePeerConfigs` returns export errors and a failed add removes its orphan peer dir. Tests: `TestAddInterfaceRejectsInvalidRequestsBeforeWriting`, `TestAddInterfaceRefusesExistingInterface`, `TestAddInterfaceReturnsPeerExportError`. The stat-then-write window is not locked.

- [x] **26. ifcfg backend strips shell quotes on every rewrite**
  `backend/bridge/handlers/network/internal/ifconfig/backend_ifcfg.go:189-215,279-296`
  - `[worker]`: `NAME="System eth0"` → `NAME=System eth0` after any edit (sourced by shell).
  - Fix: keep raw text for untouched keys, or re-quote values with whitespace/special chars. Round-trip test.
  - Behaviour change: none for well-formed files.
  - Done 2026-10-23: `parseKeyValueDoc` keeps the raw line; `set` clears it; `render` re-renders only changed pairs, so untouched quoting and inline comments survive. Test: `TestIfcfgSetIPv4ManualWritesExpectedKeysAndRunsIfup` (extended).

- [x] **27. ifupdown `SetIPv4Manual` leaves a stale `netmask`/`broadcast`/`network`**
  `backend/bridge/handlers/network/internal/ifconfig/backend_ifupdown.go:102-118`
  - `[worker]`: `address a.b.c.d/16` next to the old `netmask` → ifup error after `ifdown` already ran.
  - Fix: delete those options before setting `address`; same in the DHCP path.
  - Behaviour change: none for CIDR-only stanzas.
  - Done 2026-10-23: `SetIPv4Manual` deletes `netmask`/`broadcast`/`network` before setting `address`; `SetIPv4DHCP` drops them too. Test: `TestIfupdownSetIPv4ManualDropsLegacyNetmask`.

- [x] **28. Slow guest-agent lookup kills the libvirt connection before metrics are fetched**
  `backend/bridge/handlers/virt/domains.go:30-31,53-54`, `domain_addresses.go:14-30`
  - `[worker]`: the 3 s interrupt closes the shared transport; `enrichMetrics` then fails silently for every VM.
  - Fix: call `enrichMetrics` before `enrichBridgeAddresses` (two-line swap in `ListVMs`/`GetVM`).
  - Behaviour change: none except metrics present.
  - Done 2026-10-23: `ListVMs`/`GetVM` call `enrichMetrics` before `enrichBridgeAddresses`. Test: `TestListAndGetVMsFetchMetricsBeforeBridgeAddresses`.

- [x] **29. chmod 0666/0644 fallback on VM disks and on a seed holding a plaintext password**
  `backend/bridge/handlers/virt/storage_permissions.go:18-40`, `cloud_init.go` (`buildCloudInitUserData`)
  - `[worker]`: when neither `libvirt-qemu` nor `qemu` exists, disks become world-writable and the seed world-readable.
  - Fix: fail clearly or `0640` + `chgrp`; prefer hashed passwords in the seed.
  - Behaviour change: on such distros VM creation fails with a clear error instead of loosening permissions.
  - Done 2026-10-23: `makeManagedFileAccessible` drops the 0666/0644 fallback and the root gate (every `virt.*` route is `Privileged()`, so the bridge is root here) and errors naming the qemu accounts tried; disks 0660, seeds 0640. Cloud-init content unchanged. Test: `TestMakeManagedFileAccessibleFailsWithoutQEMUAccountAndKeepsMode`.

- [x] **30. `delete_stack` with `deleteDirectory` can `RemoveAll` a shared directory**
  `backend/bridge/handlers/docker/compose.go:412-430`
  - `[source]`: guard is only `/`, `$HOME`, `/home`; a compose file directly in a configured docker folder has `workingDir` = that folder.
  - Fix: allow directory deletion only when `workingDir` is a direct child of a configured docker folder; otherwise delete just the compose file. Reuse the host-path translation used by `ComposeRestart`.
  - Behaviour change: deletion of a stack whose compose file sits in a shared folder removes only the file.
  - Done 2026-10-23: `deleteStackFiles` removes the directory only for a strict descendant of a configured docker folder (`isStrictChildOfAny`, `deletableDockerFolders`), otherwise just the compose file; live-project label paths are host-translated via `resolveComposeRestartTargetFromProject`. Tests: `TestDeleteStackFilesKeepsSharedDirectory`, `TestDeleteStackFilesDeletesChildDirectory`. `filepath.Rel` is lexical: a symlinked working dir is not resolved.

- [x] **31. Btrfs: no cross-process lock around snapshot runs or replication**
  `backend/common/btrfs/replication_run.go:156-230`, `backend/bridge/handlers/storage/btrfs_replication_operation.go:45`, `backend/btrfs-timer/snapshot.go`
  - `[worker]`: timer and "Run now" can overlap; the second `removePartial` deletes the first run's in-flight copy.
  - Fix: `filelock.RunExclusive` per job / per rule with a short timeout and a clear "already running" error.
  - Behaviour change: an overlapping run reports "already running".
  - Done 2026-10-23: `runLocked` (`filelock.RunExclusive`, 2 s wait) wraps `Replicate` per job (`ReplicationDir/<id>.lock`) and `runRule` per filesystem UUID (`RulesPath.<uuid>.lock`); the losing run goes through the normal failure path (alert), which the winner resolves. Tests: `TestReplicateAlreadyRunning`, `TestRunReportsRuleAlreadyRunning`.

- [x] **32. Btrfs: a partial receive left by a crash makes the next replication run fail once**
  `backend/common/btrfs/replication_run.go:181-216` (`removePartial` only after a failed send)
  - `[source]`: the newest snapshot's name exists on the target → receive fails → alert; run 2 succeeds.
  - Fix: call `removePartial` for the chosen snapshot before `sendReceive`.
  - Behaviour change: one fewer false alert.
  - Done 2026-10-23: `deletePartial` (split from `removePartial`) runs on the listing already in hand before `sendReceive`. Test: `TestReplicateRemovesPartialCopyBeforeSend`.

- [ ] **33. Btrfs: replication shares the hourly snapshot service, so a long send delays snapshots**
  `backend/common/btrfs/unit.go:15-19`, `backend/btrfs-timer/snapshot.go:72`
  - `[worker]`: while a multi-hour first send runs, the oneshot service is busy and timer activations are skipped.
  - Fix: separate `linuxio-btrfs-replicate.service`/timer or `systemctl start --no-block`.
  - Behaviour change: snapshots keep their cadence during sends; new unit to package.

- [x] **34. GPU collectors give up permanently and then serve the frozen average as live data**
  `backend/monitoring/internal/collector/gpu/manager.go:113-132,690-711,~455`
  - `[worker]`: rocm loop's failure counter never resets; Intel exits after ~25 s of failures; `GetCurrentData` returns `lastAvgData` with no staleness signal.
  - Fix: reset on success; back off instead of returning; return zero data when stale.
  - Behaviour change: GPU panels recover after transient failures; stale readings become zeros.
  - Done 2026-10-23: `runRetryingCollector` backs off (5 s doubling to 5 min) and never exits on failures; the rocm-smi loop reuses it; `sampleStale` makes `GetCurrentData` return zeros for a GPU with no new sample for 30 s. The AMD sysfs poll (`amd.go`) still stops after 5 failures but is covered by the stale guard. Tests: `TestRocmSmiCollectorRecoversAfterTransientFailures`, `TestGetCurrentDataZeroesStaleGPU`.

- [x] **35. Upload commit exposes the wrong mode briefly; no-overwrite check is stale**
  `backend/bridge/handlers/filebrowser/upload_operation.go:97-121,301-349`, `upload_batch_operation.go:411-430`
  - `[worker]`: `.part` (0664) is renamed over a 0600 target before chmod; chmod/chown failures only Debug-logged; `overwrite=false` checked at task start, rename at the end replaces anything that appeared.
  - Fix: chmod/chown the temp fd before rename and return errors; commit with `Link` + remove for `!overwrite`.
  - Behaviour change: a chmod/chown failure now fails the upload; a file that appeared mid-upload is not overwritten.
  - Done 2026-10-23: `restoreUploadedFile` applies mode/ownership to the 0600 temp buffer before commit and returns errors; `commitUploadBuffer` links (EEXIST → 409) when `!overwrite`/keep-both, falling back to stat-then-rename on filesystems without hard links (FAT, SMB, some FUSE). Tests: `TestUploadTaskCommit{KeepsRestrictiveTargetMode,FailsWhenOwnershipCannotBeRestored,DoesNotReplaceFileCreatedAfterStart,WithoutOverwriteLinksNewFile}`.

- [ ] **36. File operations fail below a directory symlink whose target is absolute**
  `backend/bridge/handlers/filebrowser/fsroot/fsroot.go`, `services/info_operations.go:119-133`, `download_stream.go:67`, `services/safe_open.go:102-112`
  - `[worker]+[lib]`: `os.Root` at `/` rejects absolute symlink targets (`errPathEscapes`); listing works, open/stat/edit/copy/delete under `/home/u/data -> /mnt/data` fail. `[unverified]` on a host — reproduce first; the fix touches many call sites.
  - Fix: one `fsroot.Resolve(abs)` using `filepath.EvalSymlinks` on the parent at the entry points.
  - Behaviour change: files under symlinked mounts become usable.

- [x] **37. Recursive chmod follows symlinks; recursive chmod/chown report success on partial failure**
  `backend/bridge/handlers/filebrowser/services/file_operations.go:743-784`
  - `[worker]`: `Root.Chmod` follows links (chown uses `Lchown`); per-entry failures are logged and nil returned.
  - Fix: skip symlinks; count failures and return an error.
  - Behaviour change: a chmod -R over a tree with links no longer touches link targets; partial failures surface.
  - Done 2026-10-23: `changePermissionsRecursive` skips symlink entries; both walks count failures and return "N entries failed" via `recursiveResult`. Tests: `TestChangePermissionsCtxRecursiveDoesNotFollowSymlinks`, `TestRecursivePermissionAndOwnershipReportFailedEntries` (skipped as root).

- [x] **38. One dangling symlink or one growing file aborts the whole archive/download**
  `backend/bridge/handlers/filebrowser/services/archive_operations.go:749-804,667-698`
  - `[worker]`: `Root.Open` ENOENT on a dangling link fails the archive; tar `write too long` on a growing log.
  - Fix: skip-and-log on `ErrNotExist`/ELOOP/`ErrPermission`; `io.LimitReader(file, header.Size)`.
  - Behaviour change: archives succeed with skipped entries logged.
  - Done 2026-10-23: `skipArchiveEntry` (ENOENT/EACCES/ELOOP) via `walkEntryError` and `addSingleFileOrSkip` in `addDirectory`; tar body through `io.LimitReader(header.Size)`. Tests: `TestCreateArchiveSkipsDanglingSymlinkInDirectory`, `TestCreateTarGzKeepsHeaderSizeWhenFileGrows`.

- [x] **39. Extraction drops permission bits; zip worker pool ignores errors**
  `backend/bridge/handlers/filebrowser/services/archive_operations.go:520,621,456-481`
  - `[worker]`: every file becomes 0664 / dir 0775 (`+x` lost); `extractZipFiles` keeps launching workers after an error and returns without `g.Wait()`.
  - Fix: apply `header.Mode().Perm()` masked to 0777; `errgroup.WithContext` + `g.Wait()` on every path.
  - Behaviour change: extracted executables keep `+x`; setuid/sticky bits are still stripped.
  - Done 2026-10-23: `extractPerm` (perm bits minus other-write; dirs floored at 0700) for zip and tar; `extractZipFiles` stops launching on the first failure (atomic flag, since `ExtractArchive` has no ctx) and `g.Wait()`s on every path; zip directory entries now carry a mode. Tests: `TestExtractArchiveKeepsExecutableBit`, `TestExtractZipStopsWorkersAfterFirstError`.

- [x] **40. Package update-history parsers record wrong data**
  `backend/bridge/handlers/packages/updates.go:571-596,617-656`
  - `[worker]`: dpkg takes the old version for upgrades; dnf splits `python3-requests-2.28.1-1` at the first hyphen; no tests. `[unverified]`: check a real `dnf.log` line first.
  - Fix: dpkg fifth token for upgrades; dnf NEVRA from the right; table tests.
  - Behaviour change: history shows correct names/versions.
  - Done 2026-10-23: `applyDpkgLogLine` records the new version for `upgrade` lines (`newDpkgLogPatterns`); `parseDnfHistory` splits NEVRA from the right (`splitNEVRA`) and keys by day like dpkg. Tests: `TestApplyDpkgLogLineRecordsNewVersion`, `TestParseDnfHistorySplitsNEVRAAndKeysByDate`. The dnf.log line format is still from documentation, not a real host.

- [x] **41. LVM listing hides all failures; mount lookup keys on the wrong device name**
  `backend/bridge/handlers/storage/lvm.go:26-33,61-66,99-104,112-134`
  - `[worker]`: any `pvs/vgs/lvs` error (incl. not on PATH) → `[]` with no error; `mountMap[lv.LVPath]` uses `/dev/vg/lv` while mountinfo shows `/dev/mapper/vg-lv`. `[unverified]`: confirm the mapper name on a host.
  - Fix: return errors except `exec.ErrNotFound`/exit 5; use `lv_dm_path`; `make([]T, 0)`.
  - Behaviour change: real failures surface instead of "no LVM"; `null` becomes `[]`.
  - Done 2026-10-23: `lvmReport`/`runLVMCommand` (`hostcmd.LookPath`; only `exec.ErrNotFound` and exit 5 are "no volumes"), `getPVNamesByVG` returns its error, slices are `make([]T, 0)`, mounts matched by `lv_dm_path`. `DeleteLogicalVolume`'s "is mounted" check still compares `lv_path` (safe only because `lvremove` refuses an LV in use). Tests: `TestListLogicalVolumesReturnsCommandError`, `TestListLVMAbsentToolReturnsEmptyList`, `TestListLogicalVolumesMatchesMountByMapperPath`.

- [x] **42. SMART self-test task never finishes if it misses `in_progress`**
  `backend/bridge/handlers/storage/smart_test_operation.go:~110-155`
  - `[worker]`: terminal states are ignored until `seenInProgress`; first poll is 15 s after start; no overall deadline.
  - Fix: accept a terminal state after N polls or a deadline.
  - Behaviour change: short tests complete instead of hanging.
  - Done 2026-10-23: a terminal state is accepted after 3 polls even without `in_progress` (`completed` finishes, `failed`/`aborted` fail, `idle` fails with "never started"); 6 h overall deadline via `context.WithTimeoutCause`. Tests: `TestSmartTestAcceptsTerminalStateAfterNPolls`, `TestSmartTestFailsWhenNeverStarted`, `TestSmartTestDeadlineFailsTask`.

- [x] **43. Download of non-regular files (FIFO, `/dev/zero`)**
  `backend/bridge/handlers/filebrowser/download_stream.go:55-84`
  - `[worker]`: only `IsDir` is rejected; a FIFO blocks `Open` with no ctx escape.
  - Fix: require `Mode().IsRegular()`.
  - Behaviour change: such downloads → 400.
  - Done 2026-10-23: `openDownloadFile` opens `O_RDONLY|O_NONBLOCK` and rejects non-regular files with 400. Test: `TestStreamFileDownloadRejectsNonRegular`.

- [x] **44. `ComputeCopySize` ignores ctx and runs before an instant rename**
  `backend/bridge/handlers/filebrowser/size_estimate.go:125`, `services/file_operations.go:342-390`
  - `[source]`: `ComputeCopySize(path)` has no ctx; a 2M-file move stalls in "preparing" and cannot be cancelled.
  - Fix: pass ctx; compute lazily only after a rename fails.
  - Behaviour change: same-filesystem moves start instantly; cancel works during sizing.
  - Done 2026-10-23: `ComputeCopySize(ctx, …)` checks ctx in the walk; `tryRenameMove` no longer walks after a rename; `transferPlan.item` and `resourcePatchWithProgress` size only cross-device transfers (same-filesystem moves report no byte progress). Tests: `TestComputeCopySizeStopsOnCancelledContext`, `TestMoveFileWithCallbacksSkipsSizeWalkWithoutKnownSize`, `TestPlanBatchTransferSkipsSizeForSameFilesystemMove`.

- [x] **45. `ApplyOfflineNow` swallows a download failure and still schedules the offline update**
  `backend/bridge/handlers/packages/internal/autoupdate/backend_pkgkit.go:171-174`
  - `[worker]`. Fix: only "no updates available" is non-fatal.
  - Behaviour change: a failed download reports failure instead of "scheduled".
  - Done 2026-10-23: `ApplyOfflineNow` fails on any download error except the new `errNoUpdates` sentinel returned by `pkTransactionCallWithUpdates`. No test: `packagekit.ClientSession` wraps a real D-Bus session and has no fake (source-verified only).

## D. Nice to have

- [ ] **46.** WebSocket reads have no `SetReadLimit` — `backend/webserver/ws/ws.go:291`. Fix: `SetReadLimit(17<<20)` after Upgrade. Behaviour: frames > 17 MiB close the socket.
- [ ] **47.** No clickjacking/hardening headers — `backend/webserver/web/router.go:26-52`. Fix: `X-Frame-Options: DENY`, `frame-ancestors 'none'`, `nosniff`, `Referrer-Policy`. Behaviour: UI cannot be framed.
- [ ] **48.** Bridge binary SHA-256'd on every login attempt — `backend/webserver/bridge/bridge.go:30-58`. Fix: cache by `(size, mtime, inode)`. Behaviour: none.
- [ ] **49.** Crashed scrub leaves its record "running" forever — `backend/common/btrfs/scrub.go:46,148-180`, `inventory.go:259-290`. Fix: map a stale "running" record to "aborted" when nothing is live. Behaviour: UI stops showing a phantom scrub.
- [ ] **50.** Remount failure after unmount during rollback leaves the mountpoint empty — `backend/common/btrfs/rollback.go:271-292`, `mounts.go` (`splitMountOptions`). Fix: retry with `subvol=` + vfs flags only. Behaviour: safer rollback.
- [ ] **51.** `DeleteSnapshot` re-lists the whole filesystem per delete — `backend/common/btrfs/snapshots.go:150`. Fix: drop `findSubvolume`, treat `ENOENT` as gone; list once per filesystem in `Run`. Behaviour: faster prunes.
- [ ] **52.** Alerts of deleted/disabled btrfs rules and jobs are never resolved — `runner.go:38-71`, `replication_run.go:351`, `bridge/handlers/storage/btrfs_timer.go:206`. Fix: `alerts.Resolve` in the delete/disable paths. Behaviour: stale alerts clear.
- [ ] **53.** `RunComposeProject` returns on a scanner error without draining/waiting the child — `backend/common/dockerupdate/compose_cli.go:212-214`. Fix: drain + `cmd.Wait()`. Behaviour: none normally.
- [ ] **54.** Per-process `MemoryPercentWithContext` re-parses `/proc/meminfo` per process per tick — `backend/monitoring/internal/collector/process/process.go:158`. Fix: compute from total memory read once. Behaviour: none.
- [ ] **55.** Every tick JSON-marshals every payload twice and the whole process list for no reader — `backend/monitoring/internal/app/handoff.go:56-100`, `runtime.go:~385-399`. Fix: build the handoff sample lazily / reuse one `Snapshot`. Behaviour: none.
- [ ] **56.** 30 s schema timeout can make a large index permanently unopenable (FTS rebuild); leftover-generation delete is slow — `backend/indexer/internal/storage/open.go:109`, `fts.go` (`ftsRebuild`), `generation.go` (`BeginGeneration`). Fix: own context for the rebuild; reuse `deleteIndexesWithFTSRebuild`. Behaviour: recovery instead of a startup loop.
- [ ] **57.** Hardlink reconcile runs N+1 queries in one transaction — `backend/indexer/internal/storage/hardlinks.go:~330`. Fix: batch group loads, aggregate ancestor deltas per directory. Behaviour: faster reindex, smaller WAL.
- [ ] **58.** Monitoring store sets PRAGMAs with one-off `Exec` on a `MaxOpenConns(1)` pool; a replaced connection loses `busy_timeout`/`synchronous`; path not URI-escaped — `backend/monitoring/internal/store/store.go` (`init`) vs indexer DSN. Fix: put them in the DSN. Behaviour: none normally.
- [ ] **59.** Task progress log copies ~256 KB per event once full — `backend/bridge/internal/bridgeipc/tasks.go:701`. Fix: re-slice with `clear` on the dropped prefix. Behaviour: none.
- [ ] **60.** `GetIcon` re-runs five `MkdirAll`s per request — `backend/bridge/handlers/docker/icons.go:322`. Fix: rely on `prepareDockerHandlers`. Behaviour: none.
- [ ] **61.** Dir-listing comparator is not a strict weak ordering (`9 < 10 < 1a < 9`); allocates per compare — `backend/bridge/handlers/filebrowser/iteminfo/utils.go:15-35`. Fix: precomputed key + `slices.SortStableFunc` + `strings.Compare` tie-break. Behaviour: stable order for case-only ties.
- [ ] **62.** Task-archive temp file in `/tmp` (tmpfs) — `backend/bridge/handlers/filebrowser/archive_download_operation.go:86`. Fix: disk-backed temp dir. Behaviour: large folder downloads stop eating RAM.
- [ ] **63.** Cancelled batch skips indexer reconciliation; `reconcileDetached` has one 10 s timeout for all items — `backend/bridge/handlers/filebrowser/batch_operations.go:154-176`. Fix: reconcile completed items on cancel; per-item deadline. Behaviour: index stays correct after cancel.
- [ ] **64.** Hostnames may start with `-` and reach `showmount -e <server>` — `backend/bridge/handlers/storage/mounts_common.go:23`, `nfs.go:129`. Fix: reject leading `-` or pass `--`. Behaviour: such hosts → 400.
- [ ] **65.** Pre-update snapshot status says "Took snapshot" when only errors exist — `backend/bridge/handlers/packages/package_update_operation.go:199-201`. Fix: `len(Taken) > 0`. Behaviour: honest status.
- [ ] **66.** `accounts.list_sessions` kill: `pid, _ := strconv.Atoi(req.PID)` then `SIGHUP` any PID — `backend/bridge/handlers/accounts/handlers.go:50`, `user_details.go:203`. Fix: verify the PID belongs to the named session/user (`/proc/<pid>/loginuid` or logind `Leader`). Behaviour: mismatched PIDs → 400.
- [ ] **67.** `wtmpdb last … <username>` positional from `list_user_logins` unvalidated — `loginhistory.go:270`. Fix: `--` before the name; reject leading `-`. Behaviour: none.
- [ ] **68.** Health summary calls `ListServices` (≈4 D-Bus reads per unit) just to count, and ignores its error → "0 failed" — `backend/bridge/handlers/system/services_systemctl.go:20`, `health.go:23`. Fix: `ListUnits` only; surface an error flag. Behaviour: faster, honest card.
- [ ] **69.** Indexer timer interval accepts `1ns`/`500ms` and writes an invalid `OnUnitActiveSec` — `backend/bridge/handlers/indexer/timer.go:77`. Fix: minimum 1 min or validate with `systemd-analyze timespan`. Behaviour: tiny intervals → 400.
- [ ] **70.** `cli/version.go` has no ctx/timeout and five copy-pasted probe blocks (one missing its indent) — `backend/cli/version.go:12-70`. Fix: table + loop + `CommandContext`. Behaviour: aligned output; a hung binary no longer blocks `linuxio version`.
- [ ] **71.** `wireguard/public_ip.go:15` uses `http.Client.Get` with no ctx (5 s stall offline, not cancellable). Fix: `NewRequestWithContext`. Behaviour: none.
- [x] **72.** Firmware NVRAM path from domain XML checked with `HasPrefix` only (no `..` check) before `removeFile` — `backend/bridge/handlers/virt/update.go` (`updateDomainFirmware`; was `firmware.go:77-83`) (`isSafeVNCSocket` in `path_validation.go:38` does it right). Fix: `filepath.Clean` + prefix. Behaviour: none. Done 2026-10-23: `virt.set_firmware` was folded into `virt.update` (the VM edit dialog); the stale NVRAM path must be clean and under the NVRAM dir. Test: `TestUpdateVMSwitchesToBIOSAndRemovesNVRAM`.
- [ ] **73.** `docker/logs_follow_all.go:60` `dockerLogLineWriter.partial` is uncapped for a stream line with no newline. Fix: cap and flush. Behaviour: none.
- [ ] **74.** `docker/prune.go`: if a later prune step fails, earlier steps' results are dropped. Fix: return partial results with the error. Behaviour: better reporting.
- [ ] **75.** ifupdown detect scans `interfaces.d/*` including editor backups (`eth0.bak`) → "ambiguous" error blocks edits. Fix: skip non-stanza files. Behaviour: edits work with backups present.
- [ ] **76.** `wireguard/list_peers.go:105` `rateCache` is never pruned (tiny); `config.go` `addPeerSection` writes `# Name` comments that are stripped from every other peer on rewrite (cosmetic).
- [ ] **77.** `docker.handleSetIPv4` error text says "dhcp or static" but "static" is rejected — wording fix.
- [ ] **78.** `[unverified]` `monitoring/internal/plugins/aggregate.go` `averageGPUs`/`averageTemperatures`/`averageFilesystems` divide by the total sample count even when a sensor appears in fewer samples (worker judged edge-case; not traced).

## E. Lean (delete / merge; grep-verified by the sweeps)

- [x] **79.** `docker.delete_compose_stack` + `DeleteComposeStack`, `deleteComposeFiles`, `tryRemoveEmptyDir` — no frontend caller (only generated metadata), duplicates `delete_stack` — `backend/bridge/handlers/docker/compose.go:1432-1524`, `handlers.go:49,277-279` (~95 lines; needs `make generate`).
  - Done 2026-10-24: removed `DeleteComposeStack`, `deleteComposeFiles`, `tryRemoveEmptyDir`, `handleDeleteComposeStack` and the `docker.delete_compose_stack` route; call-route count in `TestAllTaskRoutesUseTaskRunner` bumped; client regenerated.
- [x] **80.** `PeersJSON` / `parseOptionalPeersValue` — frontend only sends `numPeers`; unvalidated input path — `wireguard/add_interface.go:94,250-264`, `apischema/wireguard.go:105` (~20 lines; hardening win; `make generate`).
  - Done 2026-10-24: removed `WireGuardAddInterfaceRequest.PeersJSON`, `parseOptionalPeersValue`, `resolveInterfacePeers`; `AddInterface` calls `generatePeers` inline. The frontend *did* send `peersJson: "[]"` (`CreateInterfaceButton.tsx`) — that line is removed and the client regenerated.
- [x] **81.** Eight near-identical `common/systemd/systemd.go` wrappers each calling `requireUnitName` — fold into `callUnitJob`/one `unitFilesOp` (~60 lines).
  - Done 2026-10-24: `callUnitJob` checks the unit name; one `unitFilesOp` replaces the four `*UnitFiles` helpers; `GetUnitFileState` now wraps its error. Existing tests cover it.
- [x] **82.** Indexer `OpenOptions`/`NormalizeOpenOptions` — only ever `DefaultOpenOptions()` (`fullscan.go:83`, `daemon.go`); `policy.go` says values are fixed (~40 lines). Also monitoring `store.Vacuum` (`store/store.go:315`) called only from its own test.
  - Done 2026-10-24: `OpenOptions`/`NormalizeOpenOptions`/`DefaultOpenOptions` replaced by constants and DSN literals (same values); monitoring `Store.Vacuum` and its test call removed.
- [x] **83.** Dead `bridgeipc` surface: `TaskOwner.Empty` (`tasks.go:65`), `TaskService.List` (`:394`), `ListActive` (`:428`); `Create`/`CreateForOwner`/`CreateForOwnerWithLifetime`, `Task.Subscribe`/`SubscribeWithReplay` only used by tests (~60 lines; convert tests).
  - Done 2026-10-24: deleted `TaskOwner.Empty`, `TaskService.List/ListActive/Create/CreateForOwner/CreateForOwnerWithLifetime` and `Task.Subscribe`; tests converted to `ClaimForOwnerWithIdentity` / `subscribeWithReplayStatus`. `Task.SubscribeWithReplay` kept: tests in `handlers/packages` and `handlers/docker` use it.
- [x] **84.** `systemd/timer_time.go:19` `timerNextElapseUsec` and `:31` `timerNextElapseVariantUsec` — same algorithm; keep one.
  - Done 2026-10-24: the variant-property implementation is now `timerNextElapseUsec`; the generic-property duplicate is deleted.
- [x] **85.** `validateOwnership` byte-identical in `common/atomicfile/atomicfile.go:66` and `common/filelock/filelock.go:135`.
  - Done 2026-10-24: `atomicfile.ValidateOwnership` exported; the `filelock` copy deleted.
- [x] **86.** Btrfs `alertSource`/`raiseAlert` (`runner.go:14,135`) duplicate `AlertSource`/`raise` (`health.go:16,28`) (~8 lines).
  - Done 2026-10-24: `alertSource`/`raiseAlert` removed; `runner.go` and `RecordReplicationOutcome` use `AlertSource`/`raise`.
- [x] **87.** Two `shellQuote` helpers with different rules — `common/btrfs/replication_run.go:~40`, `cli/journal.go:~25`.
  - Done 2026-10-24: both quoted a POSIX sh word; one strict `hostcmd.ShellQuote` replaces them (the cli `sg -c` string now quotes every argument; same argv). `TestShellQuote` moved to `hostcmd`.
- [x] **88.** `globSorted` hand-rolled O(n²) sort — `network/internal/ifconfig/types.go:118-135`; `filepath.Glob` already sorts per directory.
  - Done 2026-10-24: `globSorted` removed; the four ifconfig backends call `filepath.Glob` (single-directory patterns, already lexical).
- [x] **89.** `docker/container.go`: seven one-method interfaces (`containerStarter`…`containerRemover`) exist only so tests can check an `fmt.Errorf` wrap.
  - Kept 2026-10-24: the only way to drop the interfaces deleted the tests pinning `kill` → `SIGKILL` and `remove` → `Force`; the interfaces are that test seam, so they stay.
- [x] **90.** `session.Session.Capabilities` never set or read (`common/session/session.go`); `authipc.MaxError` unreferenced (`common/ipc/auth/auth.go:23`); `YamuxSession.conn` stored never read (`common/ipc/relay/yamux.go:24`); `authclient.BuildRequest` single-caller struct fill (`webserver/authclient/client.go:111`); `registerYamuxSession` `old != nil` branch unreachable (`webserver/bridge/bridge.go:94-105`); embedded `Logo.png` unreferenced and `isStaticAsset` labels for files that do not exist (`webserver/web`).
  - Done 2026-10-24: removed `Session.Capabilities`, `authipc.MaxError`, `YamuxSession.conn`, `authclient.BuildRequest` (inlined), the unreachable `registerYamuxSession` branch (+ its test) and the dead `isStaticAsset` labels. `Logo.png` kept: the embed dir is vite output and `README.md` uses `frontend/public/Logo.png`.
- [x] **91.** `EditorFileInfo.IsRegularFile/Size/CanOpenAsText` read only by tests (`filebrowser/services/safe_open.go:30-37`); `RunSmartTest` result map discarded by its only caller (`storage/smart.go:18-59`); `searchResponseFromIndexer(_ string, …)` unused param; `deleteBatchItem`'s `ok` always true; `detachedIndexerUpdates` waited on only in tests.
  - Done 2026-10-24: removed `EditorFileInfo.IsRegularFile/Size/CanOpenAsText`, the `RunSmartTest` result map, the unused `searchResponseFromIndexer` param and `deleteBatchItem`'s always-true `ok`. `detachedIndexerUpdates` kept: seven tests sync on it.
- [x] **92.** `streamrelay.ErrorCode` used only inside `streamrelay/errors.go`; `bridgeipc.statusCode` does the same mapping (merge).
  - Done 2026-10-24: `streamrelay.ErrorCode` removed; `bridgeipc.StatusCode` exported and used by `streamrelay.WriteError`. Stream-relay errors now carry the sentinel codes (400/403/404/409/429/504) instead of 500 — the union of the two mappings.
- [x] **93.** `indexer/daemon/sse.go` declares both `sseEventWriter` and `sseEventSender`, differing only by `SendError`.
  - Not applicable 2026-10-24: `*sseWriter` implements only `SendEvent`, so the two interfaces are not duplicates; collapsing them does not compile without adding code.

## F. Consistency patterns to unify (one convention each; fix when touching the file, or as one sweep)

- [x] **94. `errors.Is` everywhere.** String matching in `webserver/ws/ws.go:151-165` (`"use of closed network connection"`, `"i/o timeout"`, `"stream reset"`) → `net.ErrClosed`, `os.ErrDeadlineExceeded`, `yamux.ErrStreamReset`; `err == io.EOF` in `indexer/internal/daemon/wire.go:67`; `os.IsNotExist` in `filebrowser/services/file_operations.go:124,844`, `resource_patch.go:352`, `common/updatestatus/updatestatus.go:75`, `wireguard/remove_interface.go:34`, `docker/compose.go:435` vs `errors.Is(err, fs.ErrNotExist)` elsewhere.
  - Done 2026-10-24: `ws.go` matches `net.ErrClosed`/`os.ErrDeadlineExceeded` and the yamux sentinels (the string fallbacks were dead); every `os.IsNotExist` (30 sites) and `err == io.EOF` (9) is `errors.Is`; the two filebrowser "is a directory" string matches use `errArchiveDestinationIsDir`/`syscall.EISDIR`. The libvirt message matches in `virt/default_resources.go` stay (no sentinel). Same change: handlers no longer log an error they return; the router and task runner log each failed route/task once (`TestHandlersReturnOrLogErrorsNotBoth` guards it).
- [ ] **95. Delete nil-context guards** (all callers pass ctx): `common/filelock/filelock.go:91` (substitutes Background), `common/dbusclient/client.go:231-235` (panics — against the no-panic rule), `webserver/appupdate/version.go:130` (returns empty), `indexer/internal/storage/queries.go:19`, `open.go:76`, `paths.go` (`ensureContext`), `packagekit.go:146,168` ("nil context" errors).
- [ ] **96. ctx first in `filebrowser/services`**: `CopyFileWithCallbacks`, `MoveFileWithCallbacks`, `ExtractArchive`, `CreateZip`, `ComputeCopySize`, `ComputeExtractSize` use `opts.Cancel` or nothing while `DeleteFilesWithProgress`/`ChangePermissionsCtx` take ctx; `computeTransferSize(ctx, …)` drops it. `btrfs/mounts.go:48` `ListMounts(ctx)` ignores its ctx.
- [ ] **97. One subprocess path**: `hostcmd.LookPath` + ctx (sbin fallback) is the documented way (`shares`, `storage/nfs`, `ifconfig/runner.go`); `exec.LookPath` in `storage/smart.go:34,265`, `capabilities/probes.go:22`, `wireguard/interface_power.go:79`, `install_capability.go:269`; bare names in `storage/lvm.go:27,164`, `mounts_common.go:116`, `dockerupdate/compose_cli.go:98`, `accounts/*`, `samba.go:138`; no ctx at all in `cli/version.go:12`, `cli/logs.go:~65`.
- [ ] **98. `atomicfile.Write` for every system config**: used by `datetime.go:212,254`, `shares/rsync.go`, `mounts_common.go:228`, `tlsserver`; missing in `storage/fstab.go`, `shares/samba.go`, `shares/nfs.go` (items 2, 17b).
- [ ] **99. `apischema.Privileged()` gating is uneven**: every btrfs mutation is flagged (`storage/handlers.go:31`); `storage.create_lv/delete_lv/resize_lv/mount_nfs/mount_cifs/unmount_*/remount_*/set_btrfs_maintenance`, all `accounts.*`, NFS/Samba routes (`shares/handlers.go:17-25`; rsync is flagged) and `control.app_update` are not — a non-root bridge does side effects (mkdir, credential files) before the OS refuses. See `docs/privilege_pattern.md`.
- [ ] **100. Validation errors → 400 consistently**: `fmt.Errorf("username is required")` (500) in `accounts/user_commands.go:14`, plain errors in `shares/samba.go:89` vs `bridgeipc.ErrInvalidArgs`/`NewError(msg, 400)`; the `"bad_request:"` string prefix in `filebrowser/resource_read.go:31`, `resource_create.go:216` is also used for 500-class failures (`resource_create.go:251`); `resourceStat` maps `ErrNotExist` to 404 while `listDirectory` wraps everything as bad_request.
- [ ] **101. Error wrapping keeps the chain**: `%s`/`%v` for inner errors in `accounts/user_commands.go:46-48`, `shares/nfs.go:306-308`, `docker/images.go:20`, `btrfs/rollback.go:167`, `network/network.go:35` (bare return), `ifconfig/backend_ifcfg.go:135-146` (no path context), `common/systemd/systemd.go:128` (`GetUnitFileState` raw) → `%w` / two `%w` / `errors.Join`; `sessions.go:303,310` non-sentinel "session not found"/"expired" → sentinels.
- [ ] **102. Logging shape**: `component`/`subsystem` attributes on `filebrowser/services`, `dockerupdate` and `webserver/auth`; none on `btrfs`, `storage`, `wireguard`, `monitoring` collectors; `wireguard` both logs and returns the same error (`remove_interface.go:17-30`, `add_peer.go:44-47`) while docker/network/virt leave logging to the caller.
- [ ] **103. Interface/host name rules in one place**: `wireguard/validate.go:15` (`[A-Za-z0-9_-]{1,15}`), `hostbridge/bridge.go:678` (also `.`), `network/network.go` mutations (non-empty only; name reaches `ifcfg-<iface>` paths and `ifup`), `virt/networks.go:253`; `mounts_common.go:23` hostnames allow a leading `-`.
- [ ] **104. JSON codec**: `webserver/auth/auth.go` uses `encoding/json/v2`; `web/response.go`, `download`, `alerts`, `durabletask` use v1. Pick one per binary.
- [ ] **105. Rune-safe truncation helper**: `alerts.truncate` is right; `durabletask/store.go:161` and `dockerupdate.BoundedMessage` byte-slice (item 11).
- [ ] **106. Same rule in two places**: `config/apply_operation_settings.go` requires `heavyArchiveConcurrency > 0` while `userconfig/rules.go:213` allows 0 (mapped to default); `datetime` `EnabledRequest.Enabled` is a string compared with `== "true"` (`"True"`/`"1"` silently disable NTP); `ComposeUp/Down/Stop/DeleteStack` use raw label paths while `ComposeRestart` host-translates them; `filebrowser` 400 signalling (item 100).
- [ ] **107. SQLite conventions (indexer vs monitoring)**: monitoring `WriteSnapshot`/`WriteSmartDevices`/`RunMaintenance` use `db.Begin()`/`tx.Exec` without ctx and the fragile `if err != nil { Rollback }` pattern that `indexer/generation.go:rollbackTx` comments on; indexer hand-rolls the rollback in `writer.go`, `fts.go`, `generation.go` instead of using its own helper; `rows.Close` logged in `queries.go` but bare in `hardlinks.go`.
- [ ] **108. Sorting helpers**: `sort.Slice` (`docker/networks.go:43`, `shares/samba.go`), `sort.Strings` (`virt/preflight.go`) vs `slices.SortFunc` (`docker/container.go:62`).
- [ ] **109. Docs/comment drift**: `monitoring/internal/daemon/daemon.go:13` says commands are never served off the control socket but the listener is built with `{"metrics","commands"}`; `cli/verbose.go:15-35` hardcodes `ExecStart=` lines that silently revert packaged unit flags when verbose mode is on; `version.IsNewer` compares non-numeric segments lexically (`0.48.0-rc1` > `0.48.0`; no pre-release tags exist today).
- [ ] **110. Ownership of created files under a root bridge**: uploads chown to the session user (`upload_operation.go:107`); `resourcePost`, `ExtractArchive`, compress outputs and upload-batch directories do not (`upload_batch_operation.go:227,337`).
- [ ] **111. Bounded reads**: `handlers/indexer/http.go:263` errors on overflow; `handlers/monitoring/client.go:83` `LimitReader` silently truncates (shows up as "unexpected end of JSON").
- [ ] **112. PackageKit timeouts for the same kind of work**: 120 s (`updates.go:694`), 30 min (`package_update_operation.go:302`), 300 s (offline download).

## G. Not read / outside this audit

- `backend/bridge/internal/apigen/main.go`; `network/internal/hostbridge/handoff.go` and `networkmanager.go:200-415`; `storage/btrfs_settings.go`, `btrfs_subvolumes.go`, `btrfs_rollback_operation.go`, `btrfs_replication_operation.go`, half of `btrfs_pool.go`; `monitoring` `sensors`, `gpu/{amd,intel,nvml,nvtop}`, `host/battery.go`, `smart/{emmc,mdraid}`, `domain/*`, `live/{gpu,sensors}`, `plugins/{payload,plugins}`, tail of `aggregate.go`, `httpapi/plugin.go` after L200; `userconfig/{types,validator,csscolor,themecolors}.go`; `packages/internal/autoupdate/backend_mint.go`; `common/dbusclient/{interfaces,types}.go`; `common/hwprobe/gpu.go` after L80.
- `_test.go` files were grepped for coverage only, not read.
- The C auth daemon (`backend/auth`) except username validation and result-code mapping.
- Runtime behaviour of exportfs, wg-quick, NetworkManager, netplan, libvirt and dnf logs is from documentation/source reading, except the two host checks noted (`journalctl -n`, `qemu-img` VMDK descriptor).
