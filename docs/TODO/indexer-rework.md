# Indexer rework: cheaper rescans and updates that are not dropped

## Status

Not built. This is the agreed plan from the audit of 2026-11-22. What exists
today is in [Filesystem Indexer](../indexer.md).

## Problem

Every figure below is from the installed service on one host with 1.6 million
files and 248,000 folders (1.85 million entries), unless it says benchmark.

- **A rescan rewrites the whole index.** The timer runs every hour. One run
  takes about two minutes, uses about two minutes of CPU and writes 9.2 GB,
  which is about 220 GB a day. It spends 62 seconds writing every entry into a
  second copy of the index (3.8 GB) and 59 seconds deleting the old copy and
  compacting the file (5.4 GB). The walk of the filesystem is not the slow
  part: `find` walks 1.05 million entries on that host in 1.7 seconds.
- **It runs at normal priority with no limits**, and its page cache peaks at
  2.4 to 3.9 GB per run. With only about 0.9 GB of cache available, the same
  cycle read 14.1 GB back from disk and took 240 seconds instead of 163
  (benchmark).
- **Updates from the file browser are dropped.** The daemon admits one
  operation at a time and answers 409 to the rest
  (`backend/indexer/internal/daemon/operations.go`), and the bridge logs the
  409 at debug level and goes on
  (`backend/bridge/handlers/filebrowser/indexer_sync.go`). A reindex answers
  202 and keeps running, so the next item of a batch copy or move is refused,
  and so is everything sent while a rescan runs. The index stays wrong until
  the next rescan. Read from the code, not reproduced.
- **A size that was read too early sticks.** The bridge reports an operation
  done and tells the indexer in the background. The page asks for sizes at
  that moment, can get the old value, and keeps it for five minutes
  (`frontend/src/routes/_authenticated/filebrowser/-hooks/useFileDirectorySizeBase.ts`).
- **Files inside running containers are indexed again.** The only Docker rule
  is `/var/lib/docker/overlay2/*/merged`
  (`backend/indexer/internal/indexing/exclusions.go`). With the containerd
  image store the mounts are under `/var/lib/docker/rootfs/overlayfs/`, and
  the host above has 11 of them. Its index reports 888 GB on a disk that
  holds 324 GB.
- **"Network mounts off" misses several kinds.** The list has 12 types,
  mostly NFS and SMB. sshfs, rclone, s3fs, Ceph, GlusterFS, 9p and WebDAV
  mounts are walked on every rescan although the setting is off.
- **The database is larger than it needs to be**: 1.25 GB, about 680 bytes
  per entry. The path is stored three times, one index covers every file
  though only folders are read through it, one index is redundant, and the
  search index is built twice per rescan (trigger per row, then a full
  rebuild at prune).
- **On btrfs the database fragments.** The file on that host is in 89,084
  pieces. That costs nothing on an NVMe disk and minutes of seeking on a
  spinning one.
- **Quarantined databases are never deleted**
  (`backend/indexer/internal/storage/open.go`): each schema change or
  corruption leaves a full copy behind.
- **The timer's unit says nothing about the scan.** It asks the daemon for a
  scan and exits after 70 ms with success. A scan that fails is only in the
  daemon's log.

## Decisions

**A file operation never waits for the index.** The bridge reports an
operation done when the filesystem work is done. The index is a cache of the
filesystem and catches up afterwards; the page shows the new sizes when it
has. Holding "done" back would make a finished copy look stuck while a rescan
runs, and would show a successful operation as failed when the indexer is
slow or down.

**The daemon stays on demand and the socket unit stays.** systemd holds
`/run/linuxio/indexer.sock` while no indexer runs and starts the daemon on
the first connection. An idle daemon costs 15 MB, so running it permanently
would buy nothing, and without a watcher it has nothing to do between
requests.

**The scanner becomes the timer's own unit.** Today the timer starts
`linuxio-indexer --trigger-index`, which knocks on the socket so that the
daemon starts the scanner as its child. After step 5 the timer's unit runs
the scanner itself, as `linuxio-docker-update` is run, and the Index now
button starts the same unit through systemd. `--trigger-index` and
`--socket-path` (`backend/indexer/internal/cli/trigger.go`) are removed: the
daemon takes its listener from systemd and the bridge has the path as a
constant, so the flag could only ever point the trigger at nothing. Daemon
and scanner stay one program, `linuxio-indexer`, in two roles.

**A rescan updates the index in place and writes only what changed.** No
second copy, no prune, no vacuum. Three rules keep it fast on spinning disks
and small machines:

- Compare one folder at a time with a single range read of an index keyed by
  parent and name, not one lookup per entry: 3.9 seconds against 21.1 for the
  compare loop over 1.85 million entries (benchmark).
- Keep no in-memory set of every entry. What is stored for a folder and not
  found on disk is a deletion.
- Read a huge folder in chunks. Today `ReadDir(-1)` loads a whole folder at
  once (`backend/indexer/internal/indexing/scan.go`). A folder larger than
  one chunk falls back to a lookup per entry.

**The network mounts setting stays as it is**: off by default, and switching
it on indexes network mounts. Only the list of types that count as network
mounts grows. Local disks mounted through FUSE (NTFS drives, mergerfs pools)
are not network mounts and stay indexed. Container mounts are a separate
rule and are always skipped.

**The index folder is marked no-CoW on btrfs**, with a tmpfiles line as
systemd does for its journal. btrfs then stops checksumming that file, which
is acceptable for a cache that can be rebuilt. The monitoring database holds
history that cannot be rebuilt and is not part of this.

**The default interval becomes 24 hours** until step 5 makes a rescan cheap.

**From FileBrowser Quantum** (v2.1.6 read for comparison): refreshing a
folder when it is opened and collapsing repeated updates to one path are
taken. Its schedule that adapts between five minutes and twelve hours is not:
it needs a process that always runs. Its scan rewrites a timestamp on every
row to find deletions; this plan writes less.

## Steps

Sizes are estimates and include tests and docs.

### Step 1: stop the bleeding

About 10 files, packaging and a little Go. No new privileged or public
surface. Changes units, so it is verified in the VM as installed and after a
reboot.

- Default interval 24 hours, with a first run soon after boot so a new
  install has an index without waiting a day. A saved interval keeps its
  value.
- The scanner lowers its own CPU and I/O priority.
- Older quarantined databases are deleted; at most the newest is kept.
- `h /var/lib/linuxio/indexer - - - - +C` in the package's tmpfiles file. It
  takes effect for a database created afterwards, which step 4 does.

Effect on the host above: 220 GB a day becomes about 9 GB.

### Step 2: what is left out of the walk

About 5 files, scanner only.

- Container mounts are recognised by filesystem type, not by one path
  layout.
- sshfs, rclone, s3fs, Ceph, GlusterFS, 9p and WebDAV join the network
  types.

### Step 3: updates are not dropped

About 7 files, daemon only. The bridge is not changed, and the batch case in
`backend/bridge/handlers/filebrowser/batch_operations.go` is fixed by it.

- An add, delete or scoped reindex waits for its turn instead of getting a
  409.
- Updates that arrive during a rescan are held and applied when it ends.
- Repeated updates to one path collapse to one running and one waiting.

### Step 3a: sizes catch up on their own

About 10 files: daemon, wire types, bridge, generated contract, two frontend
hooks. It changes the bridge contract, so it needs `make generate` and the
full test run.

The operation still reports done at once. An answer to a size request says
when an update for that folder is still pending, and the page then asks
again shortly after and does not cache that answer.

### Step 3b: refresh a folder when it is opened

About 6 files: one daemon request, the bridge's listing path.

Opening a folder in the file browser refreshes that folder in the index,
without descending into subfolders, so a change made outside LinuxIO shows
up where someone is looking. It applies to sessions that may read the index,
and is skipped or bounded for a folder with a very large number of entries.

Going in and out of a folder does not refresh it every time:

- The refresh rides on the listing request. The page caches a listing for
  two seconds
  (`frontend/src/routes/_authenticated/filebrowser/-hooks/fileBrowserListingQueryOptions.ts`),
  so a return within that time reaches neither the bridge nor the indexer.
- Repeated requests for one folder collapse to one running and one waiting,
  as in step 3.
- The daemon skips a folder it refreshed a short while ago, 30 seconds as a
  starting value. A change made outside LinuxIO inside that window shows up
  one visit later.
- A refresh that finds nothing changed writes nothing: it reads the folder
  from disk, reads its stored entries and compares.

### Step 3c: a move renames rows in the index

About 8 files: one daemon request, a storage function, the bridge's move
notification.

Today a move deletes the old rows and walks the destination to add
everything again (`movePathInIndexer` in
`backend/bridge/handlers/filebrowser/indexer_sync.go`). The files are the
same files, so the index renames the rows of the moved subtree in place, with
no walk of the disk, and moves the size from the old parents to the new ones.
The names below the moved folder do not change, so the search index is not
touched. The gain is not measured.

It falls back to today's delete and walk when:

- the move crossed to another filesystem, which is a copy and a delete, and
  gives the files new device and inode numbers;
- the move merged into an existing folder or finished only partly;
- the source or the destination is a place the index leaves out.

A move into or out of the trash is such a move: it reaches the index through
the same notification (`reconcileMoves` in
`backend/bridge/handlers/filebrowser/trash_operation.go`), so trashing or
restoring a folder renames its rows too.

A delete for good already needs no walk: it removes the rows and subtracts the
folder's stored size from its parents. A copy keeps its walk of the new
folder, since the copies are new files whose times and size on disk can
differ from the source's.

### Step 4: storage version 3

About 16 files: storage and one build file, so it needs the full test run.
On upgrade the old database is quarantined as
incompatible and the index is rebuilt once; search and folder sizes are
unavailable until that first scan finishes.

- Drop the redundant index and the unread `hidden` column; store the type as
  a number.
- Index subfolders only for folders (299 MB becomes 13 MB) and hard links
  only for files with more than one link (39 MB becomes under 1 MB). The
  second also removes the scan of every file that a scoped reindex does for
  its hard link snapshot (`backend/indexer/internal/storage/hardlinks.go`).
- Build the search index once per scan
  (`backend/indexer/internal/storage/generation.go`,
  `backend/indexer/internal/storage/fts.go`).
- Commit 50,000 rows at a time instead of 500.
- Use the trigram index as a prefilter for case-sensitive search: 345 ms
  becomes 10 ms.

Three settings of the SQLite driver (`mattn/go-sqlite3`) belong to this step,
in `backend/indexer/internal/storage/open.go` and `mk/go.mk`:

- **Write transactions start with `_txlock=immediate`.** The driver begins
  every transaction as a plain `BEGIN`. In WAL mode a transaction that reads
  first and writes later then fails at once with "database is locked" when
  another process wrote in between, and the busy timeout does not apply.
  That cannot happen while only one writer is ever active, as today. Step 5
  has the scanner and the daemon writing at the same time, so this must be
  in place before it.
- **`PRAGMA temp_store = MEMORY` runs on each connection.** The driver has no
  connection-string setting for it. With 500-row commits SQLite pushes
  3.6 GB per scan through temp files in `SQLITE_TMPDIR`, which is on disk
  (benchmark); the larger commits above remove most of that and this
  removes the rest.
- **The indexer is built with `sqlite_omit_load_extension`** beside
  `sqlite_fts5`. A root daemon has no use for loading SQLite extensions.
  Every query is parameterised, so this is hardening and closes no known
  hole.

Effect in the benchmark: the database goes from 1.34 GB to 0.99 GB and one
rescan cycle from 13.5 GB written to 3.9 GB.

### Step 5: write only changes, scanner as its own unit

About 30 files: storage, scanner, daemon, the bridge's index task, units,
the smoke test. Changes units, so it is verified in the VM.

- The rescan described under Decisions, with the parent-keyed index it
  needs (111 MB in the benchmark).
- The scan unit, the removal of `--trigger-index` and `--socket-path`, and
  Index now through systemd. The unit's own result, duration and resource
  use then describe the scan.
- The walk opens each folder relative to its parent without following a
  link. Today it decides "folder" from `lstat` and then opens by path, so
  another local user can swap in a symlink in between and send root's scan
  somewhere else.

Effect in the benchmark with 1% of entries changed: 0.27 GB written and 6
seconds, against 13.5 GB and 163 seconds. The estimate for a real rescan on
the host above is 10 to 20 seconds; it is not measured. An hourly interval
is affordable again after this step.

## Measurements

One rescan cycle of 1.85 million entries, rebuilt in a test harness that is
not in the repository (same schema, pragmas and steps; Python, so the times
compare with each other only). It reproduces the real run: 9.9 GB reaching
disk against 9.2 GB observed.

| Variant | Written | Time | Database |
| --- | --- | --- | --- |
| Today | 13.5 GB, 3.6 GB of it SQLite temp files | 163 s | 1.34 GB |
| Today, 50,000-row commits | 9.7 GB | 108 s | 1.34 GB |
| Slim schema | 8.2 GB | 103 s | 0.99 GB |
| Slim schema, search index built once | 4.4 GB | 49 s | 0.99 GB |
| The same with 50,000-row commits | 3.9 GB | 38 s | 0.99 GB |
| New file, bulk load, rename | 1.2 GB | 20 s | 0.95 GB |
| In place, 1% of entries changed | 0.27 GB | 6 s | 0.99 GB |

## To settle while building

- **How the page learns that a size is pending** (step 3a): a flag on the
  answer, as above, or an event from the bridge. No general way for the
  bridge to push an event to the page outside a task was confirmed.
- **The entry limit above which opening a folder does not refresh it, and
  the length of the "refreshed a short while ago" window** (step 3b).
- **Whether the daemon can tell that a move stayed on one filesystem** from
  the stored device numbers, or the bridge says so in the request (step 3c).
- **How the scan unit starts the scanner** (step 5): `linuxio-indexer` with
  one flag, as `linuxio-docker-update` starts, or a word of
  `linuxio-worker`, which would link SQLite and the scanner into the worker.
- **Progress for Index now** once the daemon no longer owns the scanner's
  output: read from a row the scanner updates, or a file under `/run`.
- **The search index with `detail=none`**: 286 MB becomes 99 MB, and the
  queries change from `MATCH` to `LIKE` and `GLOB`, which also serves
  case-sensitive search. A `LIKE` with an `ESCAPE` clause does not use the
  index.
- **Folder details** count files and folders on every request (1.4 seconds
  for `/` in `backend/indexer/internal/storage/queries.go`). Storing the
  counts beside the size makes it constant.
- **Quick scans by folder modified time** between full scans, as FileBrowser
  Quantum does. A folder's modified time changes when entries are added,
  removed or renamed, not when a file grows, so sizes would drift until the
  next full scan.
- **A smaller step 5**: build each scan in a new file and rename it over the
  old one. About 12 files, 91% fewer writes, but it keeps the full rewrite.
- **What `last_indexed` means.** Every add and delete rewrites it, so
  Settings shows the last change, not the last scan.

## Not planned

- **No daemon at all**, with the root bridge reading and writing the
  database. It moves SQLite and the index writes into the bridge, takes the
  sandbox away from scoped scans, and loses the one place that orders
  updates. 50 to 60 files.
- **A filesystem watcher.** It needs a root process that always runs and
  `CAP_SYS_ADMIN` for fanotify on whole filesystems.
- **A schedule that adapts to how often files change.** It needs a process
  that always runs.
- **Search for a session without sudo.** Decided in the
  [backlog](./backlog.md), under Rejected.
