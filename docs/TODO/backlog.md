# Backlog

Every open item about the backend, the API, jobs, scheduled scripts and
consistency is here, once. What is built is described in
[the docs](../README.md), not here.

An item belongs here only if someone could be harmed by it or would notice it.
LinuxIO is server administration software that other people install, so a
second local user and a session without sudo count. An observation with no
consequence does not get an item; if it is likely to be raised again it gets
one line under [Rejected](#rejected) with its reason.

How to use it: take an item, fix it with a test, run the Make target
`AGENTS.md` asks for, and delete the item. Add a new item in the section it
belongs to, in the same shape.

Each item says how sure we are:

- **checked**: the code was read and the problem follows from it.
- **reported**: a scan or a review said so and nobody has re-read the code.
- **not verified**: seen once in a review or a test run and not explained.
- **needs a host**: only a real machine can confirm it.

The sections are in the order to work them. A number in brackets, such as
(audit 132), is the item's number in the old backend audit, for looking it up
in git.

## File browser: root follows links a user planted

When an admin uses the file browser, the work runs as root, and root follows
a link only in a folder root owns
([Privilege Pattern](../privilege_pattern.md#root-acting-in-the-session-users-folders)).
What is left:

- [ ] **Extract opens the archive by path.** `runExtractTask`
  (`backend/bridge/handlers/filebrowser/archive_operations.go`) and
  `ExtractArchive` (`services/archive_operations.go`) stat and open the
  archive through `root.Root`, with or without an owner, so the link rule does
  not apply to it. A local user who links `backup.zip` in their folder to an
  archive only root can read gets its content when an admin extracts it
  there. Reach the archive with `entryFollowingNoUserLinks` and read it
  through that handle. Reported by the review of 2026-11-13.

## Storage safety

Each item here is data-safety work: it starts from what the kernel and
btrfs-progs do, gets an independent review, and adds a root test on loop
devices (`make test-btrfs-integration`, and `make vm-test-btrfs-integration`
after a change that issues ioctls).

- [ ] **Membership is checked before the disk lock and not again.** The
  zeroing before a forced add, an Initialize and a new partition now opens the
  device exclusively, so a device claimed in the meantime is refused. Left:
  Format and Delete confirm a member on the inventory read before the lock;
  `writeTable` reopens the disk without `O_EXCL` to write the GPT after its
  exclusive wipe; `WipeDevice` and `WipePoolSuperblocks`
  (`backend/common/btrfs/pool.go`) open without it; and md and LVM membership
  comes from the udev database, so a signature udev has not probed is not
  seen. Reported; each window is short.

## Visible bugs

- [ ] **A browser page that closes is logged as a failed route.** With a
  stream open (a live log or output follow), the session bridge logs
  `route failed: write frame: i/o deadline reached` or `stream reset` at Error
  a few ms after the page goes away. Reproduced in the test VM. Tell a client
  that left from a failure in `Router.Dispatch`
  (`backend/bridge/internal/bridgeipc/router.go`), for reads and writes on the
  client's stream only: marking it in `relay.WriteRelayFrame` was tried and
  taken out, because that also writes to the job host and a crashed host
  would then be logged as a client leaving.
- [ ] **A refusal past the fifth failure gets no button.** A batch toast lists
  the first five failures (`describeBatchFailures` in
  `frontend/src/tasks/batchFailures.ts`) and the action is found in that text,
  so a link refusal further down the list offers no "Open the real folder";
  an upload toast shows only its first failure, with the same limit. Look for
  the refusal in the whole `failed` list. Checked.

## Tests that are missing

- [ ] Not run in the test VM: an update that waits for jobs, started from the
  in-app updater. `control.app_update` installs only a published release, and
  no published release has a higher `jobs.Protocol` than v0.53.0, so
  `frontend/src/test/vm/jobs-update.spec.ts` updates through the rig's
  installer to a protocol-bumped build of the tree.
- [ ] A relay speed test: a no-op mutation through the relay many times, with
  a bound on the median. No baseline numbers exist either for call latency,
  job start, alert listing or schedule status.

## Needs a real machine or a measurement

- [ ] **Seen once and not explained.** Try each in the test VM once, then fix
  it or delete the line:
  - `TestBtrfsDegradedIntegration` failed once in six runs on the noble VM
    (kernel 6.8) with "the refusal raised no alert explaining it", the boot
    check's alert in `backend/common/btrfs/degraded.go`; the rerun passed.
- [ ] **Nothing works below a folder link with an absolute target**
  (audit 36). A link such as `/home/u/data -> /mnt/data` is refused by Go's
  `os.Root` for any path that goes through it, and `iteminfo.ResolveSymlinksAt`
  resolves only the last name. Reproduced in the test VM on 2026-11-15, for a
  root session and a session without sudo alike: the link itself lists, and
  everything below it (list, stat, read, the Permissions dialog) fails with
  "path escapes from parent"; a link with a relative target works.
  - Fix: resolve the parent folders at the points where a path enters, and run
    `CheckNotProtected` on the result. Large: 37 `fsroot.Open` call sites, and
    a mistake here lets a path escape.
- [ ] **Scheduled scripts.** Journal retention over several days was never
  checked on a host. See
  [Scheduled Scripts](../scheduled-scripts.md#testing).
- [ ] **Hardlink reconcile runs one query per file** in one transaction
  (audit 57): `reconcileHardlinksAfterReindex` in
  `backend/indexer/internal/storage/hardlinks.go`. Measure first, then add up
  the changes per folder. The risk of a wrong fix is silent wrong folder
  sizes.
- [ ] **NVIDIA bus id** (audit 124). The unversioned `nvmlDeviceGetPciInfo`
  may be an older ABI than the struct it fills
  (`backend/monitoring/internal/collector/gpu/nvml.go`). The struct has the
  largest layout, so nothing is written out of bounds; what is left is to
  check on an NVIDIA machine that the bus id reads correctly, or to register
  `nvmlDeviceGetPciInfo_v3` by name.

## Later, no plan yet

Do one of these only when something shows it is needed.

- Run a file browser change as the session user when the path is inside
  folders that user owns, so the user's own folder links work instead of being
  refused with the folder they point to. The refusal offers to open that
  folder, so this is a convenience only.
- Pick up a package update again after the job host dies, by asking
  PackageKit.
- Retry the web socket's start in `linuxio restart --full`.
  `linuxio-webserver.socket` is ordered after `linuxio-auth.socket`, which
  ended the "Address already in use" failures (0 of 50 restarts), but any
  other unit that forks a helper in that window, such as a user's job host
  socket at a login, can still hold the old listener for a moment.
- Pick up a SMART test again from the drive's own state.
- Continue the scrub of a pool resync after a power cut. It saves its progress
  as a job's scrub does, but it is not a job, so nothing continues it and the
  resync scrubs from the start again.
- More alert sources and other ways to deliver them: see "Not in this slice"
  in [Alerts](../alerts.md).
- What jobs do not do: see "Not built" in [Jobs](../jobs.md#not-built).

## Rejected

Listed once, with the reason, so they are not raised again.

File browser, after the link fix:

- A real folder (not a link) moved into the tree while root works on it, or
  put at a name between root's `mkdirat` and `openat`: no link is followed and
  root still acts at the place that was asked for.
- Filesystems without `RENAME_NOREPLACE` (NFS, FUSE) fall back to a check and
  a plain rename: a swap there affects only the user's own files.
- The "Open the real folder" action reads its destination from the wording
  of the refusal, so a folder name crafted to contain that wording can make
  the button and the text disagree. It gains nothing: whoever plants a link
  already chooses where it points, the button only opens a folder, and the
  server checks that read again.
- "Open the real folder" on an old toast closes the Trash or Permissions
  dialog that is open when it is clicked, also one opened since for other
  items or with an operation running: the click asks to leave for another
  folder, the operation is a job that goes on, and telling the dialogs apart
  would need an identity per dialog opening.
- The checks before an operation go by path: they decide names and sizes
  only, and the change itself walks again through handles. The same holds for
  an upload's "already exists" and editor version checks: the commit links
  without replacing unless overwrite was asked, in the folder it opened.
- A trash record written for a disk whose device number changes when it is
  mounted again no longer matches its item: the restore asks first, as for an
  item another program trashed.
- An upload's commit goes by name right after it checked that the name is
  still the buffer it holds open (`checkUploadBuffer` in
  `backend/bridge/handlers/filebrowser/upload_owner.go`), so a swap in between
  commits the swapper's file and reports success: whoever can swap the name
  can write the target themselves, and root never writes to that file or
  changes its owner or mode. Linking from the open file would close it for a
  new file only; an overwrite has no rename that takes an open file.
- An upload waiting for its client to reconnect keeps its buffer open for up
  to five minutes, so the disk it is on cannot be unmounted meanwhile, and a
  buffer someone deleted fails the upload only at the commit: both end with
  the upload, and reopening by name is what let another file take its place.
- An overwriting upload gives the new file the owner and mode of what is at
  the target's name, also when a user hard-linked another account's setuid
  file there: that link needs `fs.protected_hardlinks=0`, which no supported
  distribution ships, and on such a host the link alone already misleads.
- A cancelled upload whose folder was swapped for a link meanwhile leaves its
  hidden `.…linuxio-upload-….part` buffer behind: removing it by path could
  remove another file, and the leftover sits in the user's own folder.
- The size estimate of a compress for the session user does not count what a
  link points to, so the progress bar can reach 100% a little early: counting
  it would mean following the link the estimate exists to avoid.

Without sudo:

- File browser search for a session without sudo. Decided 2026-11-13: search
  stays admin-only. The index covers the whole host and only root can reach
  it (`packaging/systemd/linuxio-indexer.socket`,
  `backend/indexer/internal/daemon/middleware.go`), so it would need a root
  component that filters every result by what the user may list, and a
  mistake there leaks file names.

Storage and jobs:

- A signal ending a scrub by itself: since kernel 6.19 a signal pending on
  the calling thread does end `BTRFS_IOC_SCRUB` with `EINTR`, but
  `blockingIoctl` (`backend/common/btrfs/ioctl.go`) issues it on a locked
  thread that blocks every asynchronous signal, and the `signals` step of
  `TestBtrfsResumeIntegration` scrubs to the end under a storm of them. A
  freeze does interrupt one, and the scrub then issues its call again; a
  replace a freeze ended is under "Not built" in
  [Jobs](../jobs.md#not-built).
- A scrub continued from its saved progress, or issued again after a freeze,
  has limits that are kept on purpose; "Scrub progress" in
  [Btrfs](../btrfs.md#scrub-progress) gives each with its reason. The dev
  extent where a disk stopped is read and counted twice, and a power cut
  loses up to a minute. `btrfs scrub status` does not show LinuxIO's scrubs.
  A disk interrupted three times in a row before it moves is aborted. A scrub
  is refused when a disk cannot be read, or while a record whose last save
  failed still says "running". A job host that dies without a reboot is not
  resumed, as for every job ([Jobs](../jobs.md)). No test suspends to RAM:
  the test VM wakes at once from every suspend after its first, so one was
  checked by hand on kernel 7.0 (the scrub was interrupted, issued again and
  finished); a frozen cgroup and a frozen filesystem are tested.
- After a reboot, power-off or network change the client compares no boot
  identity and reads no plain network change back
  ([API Contract](../api-contract.md#routes-that-cut-their-own-connection)):
  the first needs a new route open before sign-in, and the page instead waits
  to see the webserver go away and come back; only the bridge handoff, which
  can lock an admin out, is verified.
- A D-Bus error can lose one replication run's record when the last job is
  disabled during a send: it needs both at once, and it loses a record, not
  data.
- The job recorder writes progress files while it holds its mutex,
  `Store.Finish` rewrites an already finished record unchanged, and store
  reads `Lstat` and then open in two steps: no effect was seen, and the store
  folders belong to root or to the uid itself.
- A user's job host records the owner's username from the bridge's hello, and
  a resend is compared byte for byte: both are the same user's own processes,
  and the browser resends identical bytes.
- `linuxio logs` lists template units by a name no instance has: the lines
  are found through the program's log identifier.

One thing named in many places, with no silent consequence. Convert one when
touching its file:

- Sets: job settings, the monitoring settings form, indexer status fields,
  alert notification settings, journal filters, archive formats, compose file
  names, UnitInfo properties, GPU collector sources, transfer kinds, palette
  roles, editor languages, syslog priorities, Docker prune kinds, the accounts
  toast constant, view-mode surface ids.
- The same rule on both sides, in step today: notification kinds and levels,
  btrfs compression algorithms, filesystem label limits and minimum sizes, VM
  image presets, scheduled-run kinds, Docker auto-update defaults, the stream
  flags and opcodes of the binary protocol, the Claude relay limits, the
  run-record drop-in units and socket paths.
- Fallbacks that cannot run (theme primary and secondary, view and storage
  mode in `useConfig.ts`, the dock gradient range, the alternative field names
  in `NetworkInterfaceEditor.tsx`) and the monitoring summary's fallback to a
  mirror of the same stats: dead code, removed when the file is touched.

Assistant activity map:

- With more than eight calls still running past the sixteen rows, the last
  held row's `+N` counts the rest at any station, not only its own
  (`fold` in `workmap/workMap.ts`), as lot-map does: the list has no more
  rows to spend, and the picture shows each station's own `×N`.
- The agents view names a browser call from a linked cloud session by its
  real server, `remote-devices`, where the activity map files it under
  Browser: the view shows what was called, the map where it went.
- The shell reader (`workmap/shell.ts`) reads a call to a function defined
  as `f() { … }` as a command of its own, so file work done through one
  lands on Shell; it does not follow `su` fed a here-document or
  `su --command=…`, and reads the first alternatives of a `case` pattern
  with `|` as commands. Each is rare in what agents write, getting them
  right needs a shell grammar rather than a reader, and the cost is one
  row at the wrong station.

From the backend audit of October 2026:

- SHA-256 of the bridge binary on every login (audit 48): about 50 ms is not
  worth a cache keyed on inode metadata.
- Lazy marshal of the collector hand-off (audit 55): the eager marshal is what
  detaches the sample from later changes to `data`.
- Context as the first parameter across `filebrowser/services` (audit 96):
  about 57 call and test sites for no user value.
- `hostcmd.LookPath` everywhere (audit 97): the sbin fallback would make
  `smartctl` and `dmidecode` "found" on a non-root Debian bridge, turning
  "feature unavailable" into permission errors.
- Logging `component`/`subsystem` attributes (audit 102) and `sort.Slice` to
  `slices.Sort*` (audit 108): convert when touching a file.
