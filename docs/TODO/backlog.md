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
- [ ] **An upload buffer on a filesystem without file handles is matched by
  inode number alone.** `uploadBufferID`
  (`backend/bridge/handlers/filebrowser/upload_owner.go`) falls back to
  device and inode when `name_to_handle_at` returns `EOPNOTSUPP` or
  `EOVERFLOW`. Where such a filesystem reuses inode numbers, a process that
  can write the folder can delete the buffer between chunks, create its own
  file at the name, keep it open and receive the rest of the upload, which
  root then commits as the target. Either keep the buffer open across chunks
  instead of reopening it, or refuse to reopen there. Which filesystems users
  upload to give no handles was not checked. Checked 2026-11-13.

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
- [ ] **A scrub starts over after a power cut.** It is checkpointed only by
  its `Suspend` hook at an orderly stop
  (`backend/bridge/handlers/storage/btrfs_device_operations.go`). Checked.
- [ ] **A scrub might end by itself.** The kernel stops a scrub on any signal
  to its thread, and nothing in `backend/common/btrfs` retries `EINTR`. An
  inference; never observed. Needs a test.

## Visible bugs

- [ ] **A Compose stack saved by a sudo user got a folder owned by root.** The
  stack folder was root's, mode `0700`, inside that user's home. Not
  reproduced. Read 2026-11-13: the save is a file browser upload with
  `owner: "session"`, which hands a new folder to the user only when the
  folder it is made in already belongs to them (`mkdirAt` in
  `backend/bridge/handlers/filebrowser/services/handover.go`). A parent that
  is root's, such as a `~/docker` made earlier without the hand-over, would
  explain it. If it happens again, check who owns the nearest existing parent.
- [ ] **Release notes.** The release that ships jobs should state three
  behaviour changes: a stored Docker `never` no longer silences failures; an
  interruption by a shutdown is a warning, so "Errors only" no longer emails it
  unless *Warnings* is ticked for *Email*; and a save from the settings page
  over an unreadable `notifications.json` drops every schedule's choice and the
  SMTP password and keeps the old file as `notifications.json.unreadable`
  ([Alerts](../alerts.md#notification-settings)). Release notes are not kept
  in the repository, so this cannot be checked here.

- [ ] **"Open the real folder" opens behind a dialog.** After a refusal in
  the Trash or Permissions dialog the dialog stays open, so the file browser
  moves to the real folder behind it and the admin has to close the dialog to
  see it (`TrashDialog.tsx`, the permissions dialog in
  `frontend/src/routes/_authenticated/filebrowser`). Close the dialog when the
  action is clicked. Checked.
- [ ] **A refusal past the fifth failure gets no button.** A batch toast lists
  the first five failures (`describeBatchFailures` in
  `frontend/src/tasks/batchFailures.ts`) and the action is found in that text,
  so a link refusal further down the list offers no "Open the real folder";
  an upload toast shows only its first failure, with the same limit. Look for
  the refusal in the whole `failed` list. Checked.
- [ ] **Toast links are never shown.** Many toasts carry a link in `meta`
  (`FILES_TOAST_META` "Open files", `useScopedToast`, the `toast` option of
  `useCallMutation`, task definitions' `route`), but `ToastProvider.tsx`
  mounts a plain sonner `Toaster` and nothing reads `meta`, so none of those
  links appears. Render them, or remove `meta` everywhere. Checked
  2026-11-13.

## Tests that are missing

- [ ] No browser test signs in as a user without sudo. The pages gated for
  such a session are covered by unit tests only, and the test VM has one
  account, with sudo.
- [ ] The relay tests in `backend/bridge/cmd/jobs_relay_test.go` skip when run
  as root, and a privileged login skipping the host start has no hermetic test
  (`backend/auth/linuxio-auth-pam_test.c`).
- [ ] Not run in the test VM (`frontend/src/test/vm/jobs-phase5a.spec.ts`): a
  real update from a release that has job hosts, more than one waiting job, a
  user's host as the older host, `tasks.watch`, `tasks.cancel` and
  `tasks.output` during the wait, and uploads during it.
- [ ] The "Open the real folder" action was never clicked in a browser: unit
  tests cover it (`frontend/src/utils/linkRefusal.test.ts` and the tests of
  the hooks that use it). A browser test needs a link refusal from a root
  bridge, so it belongs in the test VM.
- [ ] A relay speed test: a no-op mutation through the relay many times, with
  a bound on the median. No baseline numbers exist either for call latency,
  job start, alert listing or schedule status.

## Needs a real machine or a measurement

- [ ] **Seen once and not explained.** Try each in the test VM once, then fix
  it or delete the line:
  - A progress frame published between a stream's subscribe and its first
    snapshot may be skipped (`backend/bridge/internal/bridgeipc/tasks.go`,
    `subscribeWithReplayStatus`).
  - An abort while the client waits for the stream mux after its job started
    may not reach the server (`frontend/src/api/linuxio-core.ts`).
  - After a failed first history load, a non-admin's notice observer refetches
    on focus and ignores the result (`useAwayNotice.ts`).
  - A session bridge logged `route failed: write frame: i/o deadline reached`
    while a scheduled script ran.
  - The journal field `LINUXIO_REASON` held the reason JSON-quoted
    (`"reboot"`).
- [ ] **Nothing works below a folder link with an absolute target**
  (audit 36). A link such as `/home/u/data -> /mnt/data` is refused by Go's
  `os.Root` for any path that goes through it, and `iteminfo.ResolveSymlinksAt`
  resolves only the last name. Checked in the Go 1.27.4 source; needs a host
  to reproduce first.
  - Fix: resolve the parent folders at the points where a path enters, and run
    `CheckNotProtected` on the result. Large: 37 `fsroot.Open` call sites, and
    a mistake here lets a path escape.
- [ ] **Scheduled scripts.** A run that passes its timeout, catching up a
  missed run (`Persistent=`), journal retention over several days, and a
  bridge restart during a run. See
  [Scheduled Scripts](../scheduled-scripts.md#testing).
- [ ] **Subagent sessions: one case no chat has shown.** Whether a
  subagent Claude Code moves to the background by itself gets its launch
  note as a report. See
  [AI Assistant](../AI-assistant.md#subagent-sessions).
- [ ] **What happens after a route cuts its own connection is not written
  down.** The old roadmap's rule was: send it once; after reconnect, reboot
  compares the boot identity, a network change reads back the interface, and
  power-off reports "acknowledged" or "unknown", never success just because the
  host went quiet. Whether the frontend does this was not checked. Check it,
  then describe it in [API Contract](../api-contract.md).
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
- Pick up a SMART test again from the drive's own state.
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
- The checks before an operation go by path: they decide names and sizes
  only, and the change itself walks again through handles. The same holds for
  an upload's "already exists" and editor version checks: the commit links
  without replacing unless overwrite was asked, in the folder it opened.
- A trash record written for a disk whose device number changes when it is
  mounted again no longer matches its item: the restore asks first, as for an
  item another program trashed.
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
