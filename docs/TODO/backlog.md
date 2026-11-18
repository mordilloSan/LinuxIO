# Backlog

Every open item about the backend, the API, jobs, scheduled scripts and
consistency is here, once. What is built is described in
[the docs](../README.md), not here.

An item belongs here only if someone would hit it in normal use or it is a
safety matter: data loss, privilege, another local user. LinuxIO is server
administration software that other people install, so a second local user and
a session without sudo count.

What does not get an item, so that the list stays short enough to work
through:

- A finding that a few lines fix. It is fixed in the change that found it.
- A limit that is by design, or an edge case nobody is expected to hit. It is
  one line under "Known limits" in the doc that covers the feature, and is not
  repeated here.
- A change that was proposed and turned down. If it is likely to be proposed
  again it is one line under [Rejected](#rejected) with its reason.
- An observation with no consequence. It is written nowhere.

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

## Storage safety

Each item here is data-safety work: it starts from what the kernel and
btrfs-progs do, gets an independent review, and adds a root test on loop
devices (`make test-btrfs-integration`, and `make vm-test-btrfs-integration`
after a change that issues ioctls).

## Visible bugs

Memory overflow ([Memory overflow](../memory.md)):

- [ ] **`/etc/default/zramswap` is overwritten without a marker check.** It is a
  `zram-tools` conffile, so an admin's own edits are replaced and dpkg asks at
  the next package upgrade. Checked.
- [ ] **The Memory overflow dialog has no URL.** It opens only from the Swap
  row of the dashboard's Memory card, so the `zram` capability card and its
  toast cannot link back to it after an install, and it cannot be bookmarked. A
  validated search parameter on the dashboard route would give it one. Checked.
- [ ] **`zram-generator` drop-ins are not read.** LinuxIO writes
  `/etc/systemd/zram-generator.conf` and nothing else. A drop-in in
  `zram-generator.conf.d`, or a file in `/run/systemd`, overrides it: a size
  set there wins over the one the page shows, and a device defined there comes
  back at boot after a switch to disk or zswap, which then reads as `mixed`.
  Checked by reading upstream's man page, not on a host. Decided 2026-11-17:
  read them and show each with the size it sets in the dialog, and refuse a
  switch away from zram, naming the file, while a drop-in defines a device.
- [ ] **`/etc/systemd/zram-generator.conf` is replaced without a marker
  check**, like `/etc/default/zramswap`: an admin's own file there is
  overwritten by a switch to zram, and by the off file on a switch to disk or
  zswap, also when zram was not running. That drops every device the file
  defined, a zram mount (`fs-type`, `mount-point`) included. A failed run puts
  it back. Checked. Decided 2026-11-17, for this file and
  `/etc/default/zramswap`: the status says when the file is not LinuxIO's, the
  confirm step names it and where its copy is kept, and the request carries
  that confirmation; a file with only comments needs none.

Trash ([Trash](../trash.md)):

- [ ] **The Trash is a list only and renders every row.** It has no card
  view, and `TrashList.tsx` is not virtualized, so a trash of many thousands
  of items is slow to show. Give it the folder listing's card view and its
  virtual rows (`VirtualDirectoryItems.tsx`).
- [ ] **The Trash's sort and selection are not in the URL.** A reload starts
  with newest deleted first and nothing selected. Put the sort column and
  order in validated search parameters beside `trash`.
- [ ] **A restore that ends after the user left the file browser does not
  ask about taken names.** The prompt belongs to the file browser page
  (`useTrashJobs.ts`), so those items are reported as failures and stay in
  the Trash. Keep the question, as the no-trash prompt's queue does
  (`noTrashPrompts.ts`), and ask it when a file browser shows again.
- [ ] **A trashed folder shows no size.** `trash_list` gives 0 for a folder:
  it would have to walk every trashed folder, and the spec's
  `directorysizes` cache is not read. Decided 2026-11-17: size a folder when
  its properties open; the list keeps no size for a folder.
- [ ] **A drag out of the Trash onto another server's LinuxIO tab fails
  there.** The drop is sent to that server, whose trash does not hold the
  item. Refuse the drop on another server with a message.

Scheduled runs (`backend/common/scheduledrun`):

- [ ] **A timer run whose summary could not be stored reads as a plain
  Completed.** A timer program exits with 0 when its items failed with alerts of
  their own and says so only in its summary; when that write fails (the
  store's lock held for more than 5 seconds, a full disk) the history shows
  the run as completed with no count of failures. The items' alerts are
  raised all the same. Checked.
- [ ] **The log lines of a scheduled Docker pass do not name their
  container in the saved output.** `updated Compose Docker container` carries
  the container and the images as journal fields, which
  `journalctl -o short-iso` and so a run's saved output do not show; the
  report at the end of the output names them. Checked. Decided 2026-11-17:
  these lines name the container in the message, as an exception to the
  logging rule, with an entry in the logging guard and a line in `AGENTS.md`.

Docker updates (`backend/common/dockerupdate`). No doc covers this package:
the change that builds the rollback writes one, and the limits below that are
kept go under its "Known limits".

- [ ] **An updated standalone container keeps the labels of its old image.**
  `StandaloneCreateOptions` (`standalone.go`) copies the old container's
  configuration, and an inspected container's labels include its image's, so
  the replacement carries them as its own and they override the new image's:
  `org.opencontainers.image.version` and the like stay at the old values
  through every later update. The pass report and the alerts read versions
  from the images for that reason. Drop the labels the old image set before
  creating the replacement. Checked by reading the code, not on a host.

- [ ] **A failed Compose update is not rolled back.** A standalone container
  whose replacement does not become ready gets its original container back
  (`rollbackAndClearStandalone`). A Compose service is updated by
  `docker compose pull` and `up` (`updateComposeContainerWithRunner` in
  `apply.go`), which removes the old container, so when the new one fails the
  readiness wait the update is reported as failed and the service stays on the
  new image, in a restart loop if that is how it fails. The old image is still
  on the host, untagged, until it is pruned. A rollback would have to put the
  tag back on that image and run `up` again, and it cannot do that for a
  service with `pull_policy: always`, an image pinned by digest or a `build:`
  section. Until then the way back is to pin the previous tag in the compose
  file and bring the stack up. Checked. Decided 2026-11-17: build it for a
  service whose image is named by tag, say in the pass report that it was
  rolled back, and list the three cases it cannot cover as known limits. A
  rolled-back image is not held back, so the next run tries it again.
- [ ] **A container without a health check counts as ready the moment it
  runs.** `containerReady` (`standalone.go`) accepts a running container with
  no health check on the first read, so one that crashes a few seconds after
  starting passes the wait, the backup of a standalone container is dropped,
  and the restart loop begins after the update was reported as done. The
  three-restart limit only catches a loop that starts before a read sees the
  container running. Checked. Decided 2026-11-17: a container without a
  health check counts as ready once it has run 10 seconds with no new
  restart, the time after which Docker itself counts a start as successful.
- [ ] **Three restarts fail an update even when the container would have
  settled.** The readiness wait stops at `containerRestartLimit` restarts
  counted from its first read and does not look again, so a container that
  crashes three times while something it needs comes up, and would then run,
  is reported as failed: a standalone container is rolled back to the old
  image, and a Compose service is reported as failed although it may be
  running on the new one (the next scheduled run then finds it stable).
  Chosen over waiting the full 90 seconds on every restart loop. Checked.
  By design: it leaves this list as a known limit of the Docker updates doc.

## Tests that are missing

- [ ] **No browser spec draws the assistant's effort slider.** Its thumb, halo
  and invisible track come from `--app-slider-*` variables set in
  `assistant.css`; the fake agent of the browser suite offers no effort option.
  It was looked at by hand in dev mode after it moved to `AppSlider`.
- [ ] **No browser spec for the Memory overflow dialog.** Its single-choice
  list, slider, the confirm step's focus (Back on entry, Apply on the way back)
  and the pencil that opens it were exercised under jsdom
  (`MemoryOverflowDialog.test.tsx`, `Memory.test.tsx`) and looked at by hand in
  dev mode.
- [ ] **The memory root test is narrow.** `TestMemoryIntegration`
  (`backend/bridge/handlers/memory/integration_test.go`) covers a switch to zram
  and back, a drain of the empty swap file and a restart of zram at the
  planned size, and `TestMemoryDrainIntegration` a drain of parked data,
  cancelled and in full; neither covers zswap and its tmpfiles entry,
  swappiness, rollback, TuneD or a reboot, and the cleanup restores the mode
  but not the three files LinuxIO manages.

## Needs a real machine or a measurement

- [ ] **Memory overflow, checks never done on a host.** Try each in the test
  VM or on a machine, then fix the code or the doc, or delete the line:
  - The `zram-generator` path has run only against the fake host. Run
    `make vm-reset` and then
    `make vm-test-memory-integration ZRAM_PROVIDER=zram-generator`, which uses
    Ubuntu's `systemd-zram-generator`. It has never run on Fedora or RHEL:
    check there that Install from Capabilities works, that a host with
    Fedora's default zram reads as `zram` mode, and a switch to zram and back.
    The test VM is Ubuntu only; the Fedora VM this needs is the first step of
    "Distributions" in [Frontend test coverage](./e2e-testing.md#distributions).
  - `memory.get_swap_status` asks TuneD through `power.GetStatus` as the
    session user, and finds `zramswap` through the bridge's `PATH` with the
    `/usr/sbin` fallback. From a bridge without sudo, check that TuneD reads as
    running when it is (a refused D-Bus call counts as not running, and
    `set_swappiness` then writes a value TuneD overrides later) and that
    `zramswap` is found.
  - The fail-fast condition for a zram stop systemd gave up on: a timed-out
    `zramswap stop` should leave the service `failed`. If systemd reports
    otherwise the wait runs to its ten minutes.
  - The error the kernel gives for a zswap compressor it does not have: the code
    accepts both `ENOENT` and `EINVAL` and keeps the compressor it has.
  - A masked fstab swap unit across a reboot: that it stays off, and that
    unmasking and reloading brings back the unit the fstab generator makes.
  - A swap file that fstab names through a symlink: systemd may tie the swap's
    state to the unit of the path in `/proc/swaps` and keep `<fstab path>.swap`
    inactive. A mode change takes the file away by its `/proc/swaps` path and
    masks `<fstab path>.swap`, and a drain starts that unit: check that the
    mask keeps the file off at boot and that the start brings it back.
  - A swap partition fstab names by `UUID=`: a drain takes `/dev/sdXN` away
    and starts `dev-disk-by\x2duuid-<uuid>.swap`, an alias of the device's
    own unit. Check that the alias reads inactive once the partition left
    and that starting it brings the partition back; if it kept reading
    `active`, the drain would wait its 15 seconds and the start would do
    nothing.
- [ ] **Nothing works below a folder link with an absolute target**
  (audit 36). A link such as `/home/u/data -> /mnt/data` is refused by Go's
  `os.Root` for any path that goes through it, and `iteminfo.ResolveSymlinksAt`
  resolves only the last name. Reproduced in the test VM on 2026-11-15, for a
  root session and a session without sudo alike: the link itself lists, and
  everything below it (list, stat, read, the Permissions dialog) fails with
  "path escapes from parent"; a link with a relative target works.
  - Fix: resolve the parent folders at the points where a path enters, and run
    `CheckNotProtected` on the result. Large: 40 `fsroot.Open` calls in 18
    files, and a mistake here lets a path escape. Decided 2026-11-17: fixed
    as a change of its own, after the rest of this list.
- [ ] **Scheduled scripts.** Journal retention over several days was never
  checked on a host. See
  [Scheduled Scripts](../scheduled-scripts.md#testing).

## Later, no plan yet

Do one of these only when something shows it is needed.

- The theme colour `fileBrowserChrome` ("chrome" in `theme.fileBrowser`,
  `--app-file-browser-chrome`) colours nothing: its last reader was the
  breadcrumb's size chip, which is gone. Settings still offers it. Remove it
  from the theme, the colour editor and `userconfig/themecolors.go`, or give
  it a surface to colour.
- The file browser's card-size slider changes how wide a card may get, and so
  how many fit in a row; the icon and the card's height stay. Scale those too
  if a size step should look like one in a file manager. On a screen too
  narrow for two cards at the smallest size (under about 380 px) the slider
  changes nothing and is still shown. It sits on the first section header, so
  it scrolls away with the list and an empty folder has none.
- Name a swap unit exactly as systemd does for an unusual fstab label
  (`encode_devnode_name`) and for a path of 256 bytes or more, which systemd
  names with a hash. Both entries count as unmanaged today, so nothing is
  stopped or drained for them.
- Have LinuxIO make its own swap file in a fixed folder only root can write,
  manage only that one and show any other swap as read-only, the way TrueNAS
  and the Unraid swap plugins manage only swap they made. Today LinuxIO
  manages whatever `/etc/fstab` names, which is why it needs the rules for
  links, folders others can write, a filesystem that is not mounted and a
  second swap file (see "Known limits" in [Memory overflow](../memory.md));
  most of them could then go. It would also move a swap file that sits in a
  folder others can write, which nothing does now. A redesign, for when
  feature work resumes.
- Read the unit's state over one system bus connection for a whole wait: each
  poll of a long `swapoff` opens a new one, about four a second.
- Hoist the `\ooo` unescape helper: `backend/bridge/handlers/memory/host.go`
  holds a fourth copy, beside those in `common/btrfs/mounts.go`,
  `storage/blockdev_sysfs.go` and the indexer.
- Read `/etc/fstab` once per memory change. A mode change and a drain read it
  inside the status and again in `readView`, so the drain reason and the drain
  targets can come from different reads when fstab changes in between; the
  status read could return the matched entries.
- Make the memory success log exact: `swap_file_left_active` is false when an
  unmanaged swap file stays active, and a drain's `devices` counts a device
  that vanished and was skipped.
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
  root still acts at the place that was asked for. A failed copy or an
  aborted extract removes such a folder as the one it made only when it was
  empty when root opened it.
- Filesystems without `RENAME_NOREPLACE` (NFS, FUSE) fall back to a check and
  a plain rename: a swap there affects only the user's own files.
- The "Open the real folder" action reads its destination from the wording
  of the refusal, so a folder name crafted to contain that wording can make
  the button and the text disagree. It gains nothing: whoever plants a link
  already chooses where it points, the button only opens a folder, and the
  server checks that read again.
- "Open the real folder" on an old toast closes the Permissions dialog that
  is open when it is clicked, also one opened since for other
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

Memory overflow:

- A swap partition, or a swap device `/etc/fstab` does not name, is never
  stopped or masked. A host with an active swap partition cannot choose
  compressed memory only, and a device with no fstab entry cannot be drained:
  only a device matched to an entry has a unit that can be started again, and
  the feature never takes away swap it cannot bring back.
- A malformed optional kernel file (a non-numeric sysctl, an `mm_stat` with
  fewer than three fields) fails the status route instead of reading as zero:
  a format change in the kernel should surface.
- `undoZramEnable` reads `/proc/swaps` and then stops the service, so a zram
  device that appears between the two is stopped without a room check. The
  window is a few milliseconds and the device would hold almost nothing.

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
