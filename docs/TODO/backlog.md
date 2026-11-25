# Backlog

Every open item about the backend, the API, jobs, scheduled scripts, the
packages and consistency is here, once. What is built is described in
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

- A replace from the pool details whose target disk drops out mid-copy
  fails its check ([Btrfs](../btrfs.md#devices-and-profiles), Replace
  device) but, unlike the automatic rebuild, leaves no record, so nothing
  handles that disk when it comes back carrying the replace target's
  superblock (devid 0). Since 7.1 the kernel has finished the replace onto
  the missing disk, and while that disk is connected every later mount of
  the pool fails with `EUCLEAN` ("replace without active item, run 'device
  scan --forget' on the target device", `btrfs_init_dev_replace`), LinuxIO's
  boot check and **Mount degraded** included; 7.0 instead takes the replace
  up at the next mount past every extent, leaving the disk's stale copies in
  the member. Both are shown by `TestBtrfsRebuildProofIntegration`
  (`targetFailsMidReplace`) in the test VM, on 7.0 and on Fedora's 7.3.4.
  Worse, since 7.1, when the disk being replaced is still present the finish
  also takes it out of the pool and erases its superblocks
  (`btrfs_dev_replace_finishing` scratches a writeable source), so a
  two-disk raid1 is left with one copy; read in 7.3.4's source, not run.
  LinuxIO cancels a replace as soon as its target disk goes, which keeps
  the kernel from that finish. Still open, decided 2026-11-25: a replace
  from the pool details keeps a record as the rebuild does, which
  LinuxIO's next mount acts on, since after the cancel too the disk comes
  back with the target's superblock and the mount is refused. **checked**

- Since 7.1 a replace whose target disk drops out mid-copy leaves the
  kernel corrupting its own memory ([Btrfs](../btrfs.md#known-limits)): the
  finish onto the missing disk leaves one more disk on the pool's
  allocation list than it counts writable, and every recalculation of free
  space then writes 32 bytes past an array (`alloc_virtual_chunk`); after
  an unmount, a scan of the returned disk frees a device still on that
  list. Seen with `slub_debug` on Fedora's 7.3.4 in the test VM, during
  LinuxIO's degraded mount in `TestBtrfsAutoRebuildIntegration` ("dies
  again") and with `btrfs` and `mount` alone; two of five runs without it
  ended with the guest crashed. The automatic rebuild reaches it when its
  disk drops out again, and so does a replace from the pool details.
  LinuxIO now cancels such a replace within two seconds, and says to
  restart the server, which the cancel needs too on these kernels: it
  leaves the kernel's count of the pool's writable disks one short, so the
  pool gets no new chunk and cannot be mounted read-write again
  (`btrfs_remove_bdev` takes a replace target off a count it was never
  in). Open: neither bug is
  reported upstream (the steps are `btrfs replace start`, the target
  removed from its bus mid-copy, `umount`, the disk back and scanned,
  `mount`), and a replace the kernel continued at a mount is watched
  without the cancel. **checked**

## Visible bugs

## Tests that are missing

## Needs the owner

## Needs a real machine or a measurement

- The Gemini CLI and Codex profiles (`GEMINI_CLI_HOME`, `CODEX_HOME`,
  [Assistant](../AI-assistant.md#profile)) were read from the pinned packages
  and not run: a sign-in at each vendor from the Assistant should land in
  `~/.local/share/linuxio/profiles/<id>` and leave `~/.gemini` and `~/.codex`
  as they were. Neither has run in its profile folder either: whether its file
  tools reach the home through `--include-directories` (Gemini CLI) and
  `additionalDirectories` (Codex), and whether Codex reads no project settings
  from a folder added that way.

- The automatic btrfs rebuild onto a disk that comes back
  ([Btrfs](../btrfs.md#missing-disks)) ran only on the test VM's virtio
  disks, on 7.0 and 7.3, whose serials the rig sets. A SATA or SAS disk
  pulled and reseated on a real machine (its WWN as udev reads it, `sd`
  naming while the pool holds the dead disk, a hot-plug that resets the link
  rather than removing the disk) is not run.

- Docker's wait for published host IPs
  ([Bridge Configuration Storage](../config-storage.md#wait-for-published-host-ips))
  has its boot test in the test VM, where a unit adds the late address and a
  plain bridge holds the other. Not run on a real machine: a Netplan host
  whose card appears late (turn the setting on, reboot, and confirm the
  container that publishes on the address starts), and a host whose address
  a bridge handoff by LinuxIO moved to a bridge (`systemd-analyze blame`
  must not show `docker.service` waiting 30 s).

- A Compose service that other services join with
  `network_mode: service:<it>` (a VPN container such as Gluetun and the
  services that route through it) is updated with
  `up -d --no-deps --force-recreate <service>`
  ([Docker Updates](../docker-updates.md#compose-services)), and nothing
  looks for those services first, as `validateStandaloneDependents` does for
  a standalone container. Docker records the joined container by its ID, so
  they may be left with no network, and fail with "No such container" at
  their next start. Whether Compose recreates them itself under `--no-deps`
  (an update from the page, or a pass that updates the shared service and not
  them) is not run: a Docker suite case in the test VM should bring such a
  pair up, update the shared service and look at the other. If they break,
  either refuse the update as the standalone path does (a few lines) or
  recreate them with it, which the rollback must then cover too; that choice
  is the owner's. Watchtower fixed the same in v1.24.2 (its pull request
  3991). **checked** for the missing check, **not verified** for the effect.

- The monitoring daemon's NVIDIA paths, inherited from Beszel: the library is
  called under the GPU manager's lock (whether a slow call holds readers up),
  NVML memory is read only above 0% utilisation (kept so an idle laptop GPU
  can stay in D3cold, so an idle GPU shows the memory of its last busy read),
  an entry is never removed, and the size of the process list entry is
  unchecked against the driver's header
  (`backend/monitoring/internal/collector/gpu/nvml.go`). **needs a host**

## Later, no plan yet

Do one of these only when something shows it is needed.

## Rejected

Listed once, with the reason, so they are not raised again.

WireGuard, after interfaces went through their unit (2026-11-23):

- `NoNewPrivileges=no` for the root job host, so that it can run a tool its
  distribution confines with AppArmor (Ubuntu's `wg-quick`). No: the flag is
  also what keeps the commands the root host runs as the signed-in admin
  (npm for a capability, `claude plugin`) away from `sudo`. Such a tool is
  started through its unit instead ([WireGuard Interfaces](../wireguard.md)).
- Adding rules to Ubuntu's AppArmor profile for `wg-quick`
  (`/etc/apparmor.d/local/wg-quick`). No: it belongs to another package and
  to the administrator, and loosens the profile for everyone on the host.
- Taking the WireGuard writes out of the job host. No: every write is a job
  ([Jobs](../jobs.md)), and the host would still be unable to run the tool.

File browser, after the link fix:

- Running a file browser change as the session user when the path is inside
  folders that user owns, so that the user's own folder links are followed.
  Decided 2026-11-18: no. The refusal names the folder the link points to
  and offers to open it, so this is a convenience only, and it would change
  who a root file operation runs as.
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

- A `swapoff` can wake the OOM killer inside a memory-limited cgroup (a
  container, or a unit with a memory limit, whose pages are in swap) although
  the host has free memory and the room check passes: the kernel charges each
  page it reads back to the cgroup that owns it, and the room check reads only
  the host's `MemAvailable`. Seen in the test VM (Ubuntu 26.04, kernel 7.0): a
  unit with `MemoryMax=256M` and about 800 MB in swap was killed by the memory
  cgroup's OOM killer the moment `swapoff` started, and `swapoff` itself
  failed with `Cannot allocate memory` and left the file swapping. It applies
  to the drain, Resize, Remove and the switch to compressed memory alike, and
  older releases had it in the drain and the switch too. Open: a check per
  cgroup, or a warning in the confirm.
- On btrfs, while a drain or a resize has a swap file off, a snapshot of the
  subvolume it is in (snapper, Timeshift) makes the file's blocks shared, and
  `swapon` then refuses the file, now and at boot, until that snapshot is
  deleted; a balance or a device operation running at that moment refuses
  `swapon` too, with `EBUSY`, until it ends (seen on Fedora 45 during a
  balance: `/sys/fs/btrfs/<fsid>/exclusive_operation` reads `balance`, and
  the kernel logs "cannot activate swapfile while exclusive operation is
  running"). Only a file outside a subvolume
  of its own is exposed to the snapshot: LinuxIO's own file is not, and a
  resize that uses the temporary swap file is covered while that file swaps,
  since an active swap file blocks snapshots of its subvolume. The owner's
  choice: always swap on a temporary file beside it in that window, refuse
  the drain and the resize for such a file and offer Move, or leave it.
- A malformed optional kernel file (a non-numeric sysctl, an `mm_stat` with
  fewer than three fields) fails the status route instead of reading as zero:
  a format change in the kernel should surface.
- `undoZramEnable` reads `/proc/swaps` and then stops the service, so a zram
  device that appears between the two is stopped without a room check. The
  window is a few milliseconds and the device would hold almost nothing.

Docker startup:

- Waiting for a network card instead of an IP address before Docker starts.
  A later address on the same card would need no edit, but the card is picked
  by name, its name would be a second free-text value in a shell line root
  runs at boot, and a bridge handoff moves the address off the card, so the
  handoff would have to rewrite the drop-in inside the network manager's
  rollback window
  ([Bridge Configuration Storage](../config-storage.md#wait-for-published-host-ips)).

Indexer, decided with the rework of 2026-11-22:

- No indexer daemon, with the root bridge reading and writing the database.
  It moves SQLite and the index writes into the bridge, takes the sandbox
  away from scoped scans, and loses the one place that orders updates and
  scans ([Filesystem Indexer](../indexer.md#full-index)).
- A filesystem watcher for the index. It needs a root process that always
  runs, and `CAP_SYS_ADMIN` for fanotify on whole filesystems; the indexer
  runs on demand. A folder is refreshed when it is opened instead.
- An index schedule that adapts to how often files change. It needs a process
  that always runs.

Without sudo:

- File browser search for a session without sudo. Decided 2026-11-13: search
  stays admin-only. The index covers the whole host and only root can reach
  it (`packaging/systemd/linuxio-indexer.socket`,
  `backend/indexer/internal/daemon/middleware.go`), so it would need a root
  component that filters every result by what the user may list, and a
  mistake there leaks file names.

Storage and jobs:

- Leaving a data pool on one disk to systemd's unmount, without
  `x-systemd.device-bound=false` on its line ([Pools](../btrfs.md#pools)).
  Decided 2026-11-20: no. With the disk gone, I/O errors on a dead mount are
  safer than writes that land unnoticed on the system disk, and one rule for
  every pool keeps the line right when a second disk is added.
- A signal ending a scrub by itself: since kernel 6.19 a signal pending on
  the calling thread does end `BTRFS_IOC_SCRUB` with `EINTR`, but
  `blockingIoctl` (`backend/common/btrfs/ioctl.go`) issues it on a locked
  thread that blocks every asynchronous signal, and the `signals` step of
  `TestBtrfsResumeIntegration` scrubs to the end under a storm of them. A
  freeze does interrupt one, and the scrub then issues its call again; a
  replace a freeze ended is under "Not built" in
  [Jobs](../jobs.md#not-built).
- A scrub continued from its saved progress, a job's or a pool resync's, or
  issued again after a freeze, has limits that are kept on purpose; "Scrub
  progress" and "Missing disks" in [Btrfs](../btrfs.md#scrub-progress) give
  each with its reason. A resync continues only within its episode: after a
  degraded mount LinuxIO made or saw, or a scrub started between two of its
  attempts, it starts over. It stays a scheduled run rather than a resumable
  job, which would need an owner and a timer-started job. The dev
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
  lands on Shell, and it does not follow `su` fed a here-document or
  `su --command=…`. Each is rare in what agents write, getting them
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

Sign-in where SELinux is enforcing
([Process & Systemd Architecture](../process-systemd-architecture.md#selinux)):

- Dropping `pam_selinux.so open` from the PAM file: every session then runs
  as `unconfined_service_t`, so a user an admin confined to `user_u` or
  `staff_u` gets an unconfined session.
- `SELinuxContext=` on `linuxio-auth@.service`: systemd creates the listening
  socket of `linuxio-auth.socket` in that context, and the policy refuses
  `init_t` an `unconfined_t` socket, so the auth socket fails as installed
  and takes the web server with it. It looked fine as a drop-in on a running
  system.

Packages ([Packages, installation and updates](../packaging.md)):

- `skip_if_unavailable=False` in `linuxio.repo`, so that dnf fails on a
  LinuxIO repository it cannot read. Decided 2026-11-22: no. With it every
  dnf transaction on the host, an unattended upgrade among them, fails while
  the repository is unreachable, and Fedora sets the opposite for every
  repository that is not its own. Refresh Sources reports the repository
  instead ([LinuxIO's own update](../packaging.md#linuxios-own-update)).
- The libvirt hook under `/usr` with a link in `/etc` made at first install,
  like the banner's links: it would deliver every fix and honour a deleted
  link on both families, but a program started through a link runs under the
  SELinux label of the file it points to, not of `/etc/libvirt/hooks`, and
  that needs a Fedora VM running a bridged VM beside Docker to check.
- A bootstrap installer script, or a package that installs the repository's
  source file: the source file is one download into the package manager's
  folder, and running a downloaded script as root is what the packages
  replaced.
- Taking over a script install (v0.54 or earlier) in the package, or links
  left in `/usr/local/bin`: the package refuses one and the administrator
  removes it with its own uninstall script; a migration would have to take
  over files the script wrote under `/etc` and `/usr/local`.
- Choosing a free port at install, or rewriting the packaged socket unit for
  another port: a socket drop-in changes the port and survives every upgrade.
- A build image of LinuxIO's own with the build libraries in it, to pin
  those too: within one release the distribution keeps them compatible, and
  the image would be one more thing to build, store and move.

Docker updates ([Docker Updates](../docker-updates.md#rebuilding-a-built-service)):

- Rebuilding Compose services built on the host in the scheduled pass,
  unattended: a build puts live an image that has never run, differs from
  night to night wherever its inputs are not pinned, and competes for memory
  and disk with the services that are running. A rebuild is a page action an
  administrator watches.
