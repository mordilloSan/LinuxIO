# Backend Audit Checklist

Read-only audit of `backend/**/*.go` (C auth daemon excluded), done 2026-10-23
on `dev/v0.48.0`, re-scanned 2026-10-24 on `dev/v0.49.0`, and worked through
on 2026-10-26 on `dev/v0.52.0`: every item whose fix was fully specified has
landed with its test and is removed (fixes are in git history). Numbers are
stable, so gaps are finished or rejected items. What is left needs a product
or policy decision, a host that this machine is not, or a measurement first.

How to use: take an item, decide, fix it with a test, run
`make check-backend-quiet` (or `make test-quiet` when the frontend contract
moves), and remove it. Remove this file once every item is done or rejected.

Verification legend:

- `[source]` – the full code path was read; the failing case follows from the code.
- `[host]` – also reproduced on this machine (systemd 259, qemu-img, apt).
- `[lib]` – behaviour of a library confirmed from its source/docs
  (Go 1.27.3 stdlib, SQLite docs).
- `[worker]` – found and line-checked by a scoped sweep; the cited lines were
  spot-checked but not every caller re-traced.
- `[unverified]` – needs a host check before acting; stated where.

"Behaviour change" says what a user or client will observe differently after the
fix, so each item can be accepted or rejected on that basis.

---

## A. Needs a product or policy decision

- [ ] **33. Btrfs: replication shares the hourly snapshot service, so a long send delays snapshots**
  `backend/btrfs-timer/snapshot.go:~74` (`replicateJobs` inside the oneshot), `backend/common/btrfs/unit.go`, `bridge/handlers/storage/btrfs_timer.go:77`
  - `[worker]`: while a multi-hour first send runs, the oneshot service is busy and timer activations are skipped. The units are rendered at runtime (`RenderUnit`/`RenderTimer`, written by `syncBtrfsTimer`), so nothing is packaged.
  - Fix: render a second `linuxio-btrfs-replicate.service`/timer pair with a `replicate run` subcommand, or start it `--no-block` at the end of the snapshot pass. Ordering and cadence are a product decision; existing installs get the new units on the next rule/job save.
  - Behaviour change: snapshots keep their cadence during sends. Not done 2026-10-26: the cadence/ordering of the second unit is the user's call, and the units can only be verified by the root integration test.

- [ ] **99. `apischema.Privileged()` on the remaining mutation routes**
  ca4e5d31 flagged the 13 routes that fail without root (LVM, NFS/CIFS mounts, unmount, btrfs maintenance, SMART tests, Docker auto-update). Still unflagged: `accounts.*` (useradd/usermod via subprocess), NFS/Samba `shares.*` (write `/etc/exports`, `smb.conf`), `wireguard.*` (`/etc/wireguard`), `network.set_*`/`enable_connection`/`disable_connection`, `systemd.*`, `docker.*`, `hostname.set_hostname`, `datetime.*`, `control.app_update`, `control.reboot`/`power_off`, `filebrowser.resource_post`, `config.set*`. Several of these go through polkit-gated D-Bus (systemd, NetworkManager, hostname, datetime, login1) and may work for a non-admin today.
  - Policy decision first: flagging removes anything polkit lets a non-admin do. See `docs/privilege_pattern.md` for the survey command.
  - Behaviour change: a non-sudo session gets 403 before anything runs instead of a host permission error part-way through.

- [ ] **131. An owned upload follows symlinks in the folder path**
  `backend/bridge/handlers/filebrowser/upload_owner.go` (`openUploadDir`), `upload_operation.go` (`prepare`), `upload_batch_operation.go` (`ensurePrepared`)
  - `[source]`: an upload that sets `owner` (the Compose save) refuses a symlink at the final name (409), but `openUploadDir` resolves the folder path through `os.Root`, which follows relative links anywhere under `/`. A process running as the user can replace `~/docker/web` with a relative link to another root-writable folder (`../../../opt/stacks/prod`) and redirect an admin's owned save there. The names are fixed (`docker-compose.yml`, `.env`) and the content is the admin's own; an edit save stops on the version check, but where the files are absent the save creates them without a prompt, and an overwrite the admin confirms replaces them.
  - Fix (proposed): for owned uploads, refuse a symlink in any path component whose parent folder the session user can write, the only place the user can plant one. Links in folders the user cannot write (`/home -> /var/home`) keep working. Check each component through the handles of the walk, not by path.
  - Behaviour change: an owned save under a user-planted folder link fails with 409 instead of landing in the link's target.
- [ ] **132. A trash restore goes where the user-editable `.trashinfo` says**
  `backend/bridge/handlers/filebrowser/services/trash.go` (`RestoreFromTrash`, `userCouldWrite`)
  - `[source]`: on a root bridge a restore into a folder the session user does not own takes only an item the user neither owns nor can write (any group- or world-writable mode counts), staged under a hidden name and re-checked there. `Path=` stays the user's to edit, so a process running as the user can still send such an item (one an admin trashed from `/etc`, or another user's file it could rename into the trash) to a different system folder; the Trash view shows that path before the admin restores. A trashed folder is judged at its top only. The admin's own file trashed from a shared folder such as `/srv` cannot be restored there, as it is indistinguishable from a planted one.
  - Fix (proposed): record the original location root-side at trash time (a root-owned record beside the user's trash) and restore only to it, or confirm a restore whose destination is a system folder. `services.openDirNoUserLinks` is the walk item 131 proposes for uploads.
  - Behaviour change: a restore to a system folder needs the recorded location or a confirmation.

- [ ] **134. High: copy, move, rename, chmod and chown on a root bridge follow links a user process swaps in**
  `backend/bridge/handlers/filebrowser/services/file_operations.go` (`transferEntry`, `mergeDirectory`, `copyEntryWithCallbacks`, `copySingleFileWithCallbacks`, `moveWithoutReplace`, `prepareDestination`, `ChangePermissionsCtx`, `ChangeOwnershipCtx` and their recursive walks)
  - `[source]`: the delete (item 133) now resolves the item's folder with `openDirNoUserLinks` and removes through `removeAllAt`. The other mutations still hand `os.Root` the full path of every entry, and `os.Root` follows relative symlinks in the folders on the way, folders the session user's processes can write. With a subfolder swapped for a link to `../../../../etc` mid-operation: a recursive chown gives the matching names there to the uid the admin picked (`Root.Lchown(walkRel)`); a recursive chmod sets their mode (`Root.Chmod(walkRel)`, which also follows a link at the last name); a move across filesystems unlinks them (`Root.Remove` of each copied source); a copy creates files with the source's content, owner and mode bits, setuid included, in the folder the link names (`Root.OpenFile(destRel, O_CREATE|O_EXCL)`); and a copy whose source folder is swapped reads root-only files into a copy owned like the entry listed before the swap. Rename and a same-filesystem move (`Root.Link`, `Root.Rename`) and the overwrite aside (`Root.Rename`, `Root.RemoveAll`) resolve both parents the same way, as do a chmod or chown of a single path. Not traced: compress and extract (`archive_operations.go`).
  - Fix (proposed): as the delete and the trash do. On a root bridge open the source and destination parents with `openDirNoUserLinks`, then work through handles: `openat` with `O_NOFOLLOW` per level, `fchownat`/`fchmod` on the entry, `renameat2`/`linkat` between folder handles, `removeAllAt` for the source of a move and for the aside.
  - Behaviour change: none for a normal operation; one through a link the user could have planted is refused with 403.

## B. Needs a host or a measurement first

- [ ] **36. File operations fail below a directory symlink whose target is absolute**
  `backend/bridge/handlers/filebrowser/fsroot/fsroot.go`, `services/info_operations.go`, `download_stream.go`, `services/safe_open.go`
  - `[lib]`: Go 1.27.3 `os/root.go` `splitPathInRoot` returns `errPathEscapes` for any link target starting with `/`, even at root `/`; `iteminfo.ResolveSymlinksAt` resolves only the last component, so anything below `/home/u/data -> /mnt/data` fails at the intermediate hop. `[unverified]` on a host — reproduce first.
  - Fix: resolve the parent chain with `filepath.EvalSymlinks` at the path-normalisation entry points (parent only, so the link itself can still be moved/deleted) and run `CheckNotProtected` on the resolved path. Large: ~37 `fsroot.Open` sites, path-containment risk.
  - Behaviour change: files under symlinked mounts become usable.

- [ ] **57.** Hardlink reconcile runs N+1 queries in one transaction — `backend/indexer/internal/storage/hardlinks.go:425` (`reconcileHardlinksAfterReindex`), `mutations.go:80`. Fix: measure first; then aggregate ancestor deltas per directory. Risk: silent wrong directory sizes.

- [ ] **124.** `[unverified]` The unversioned `nvmlDeviceGetPciInfo` symbol may be an older ABI than the struct it fills — `monitoring/internal/collector/gpu/nvml.go`. 2026-10-26: the Go struct now has the v3 layout (68 bytes, the largest), so any ABI writes within bounds; what is left is a host check on an NVIDIA machine that the bus id still reads correctly, or registering `nvmlDeviceGetPciInfo_v3` explicitly.

## G. Not read / outside this audit

- `_test.go` files were grepped for coverage only, not read.
- The C auth daemon (`backend/auth`) except username validation and result-code mapping.
- Runtime behaviour of exportfs, wg-quick, NetworkManager, netplan, libvirt, qemu-img and dnf logs is from documentation/source reading, except the host checks noted (`journalctl -n`, `qemu-img` VMDK descriptor, apt term.log).

## Rejected 2026-10-26 (listed once so they are not re-raised)

- 48 (SHA-256 of the bridge binary per login): ~50 ms per login is not worth a cache keyed on inode metadata.
- 55 (lazy marshal of the collector hand-off): the eager marshal is what detaches the sample from later mutation of `data`; "Behaviour: none" is not worth that guarantee.
- 96 (ctx-first in `filebrowser/services`): ~57 call and test sites for no user value, as the item said.
- 97 (`hostcmd.LookPath` everywhere): the sbin fallback would make `smartctl`/`dmidecode` "found" on a non-root Debian bridge whose PATH lacks sbin, turning "feature unavailable" into permission errors.
- 102 (logging `component`/`subsystem` attributes) and 108 (`sort.Slice` to `slices.Sort*`): no guard rule, no user value; convert when touching a file.
