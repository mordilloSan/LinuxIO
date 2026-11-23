# Filesystem Indexer

LinuxIO ships `linuxio-indexer` as a first-party, root-owned service. It keeps
the SQLite cache used for filesystem search, recursive directory sizes and
counts, and direct-subfolder sizes. The filesystem remains authoritative.
Filesystem validation and mutations belong to `linuxio-bridge`; a stale or
missing index never authorizes or blocks a filesystem mutation.

The implementation follows up on FileBrowser Quantum's
[Apache-2.0-licensed indexer](https://github.com/gtsteffaniak/filebrowser/blob/main/LICENSE).
The attribution is recorded in [Third-Party Notices](THIRD_PARTY_NOTICES.md),
and the licenses of the dependencies in the notices a package installs
([Packages](./packaging.md#third-party-notices)).

## Architecture and trust boundary

```text
Browser
  | authenticated WebSocket
linuxio-webserver (DynamicUser, relays request bytes)
  | per-session yamux connection
linuxio-bridge (enforces session and privileged-route policy)
  | HTTP over root-only Unix socket
linuxio-indexer (root-owned scanner and SQLite cache)
```

The webserver does not parse bridge request payloads. It relays bytes between
the browser WebSocket and the authenticated session's yamux connection. The
bridge router checks the route's privileged metadata and, for an authorized
root session, calls `/run/linuxio/indexer.sock`.

The webserver should not connect to the indexer directly. Its systemd service
uses `DynamicUser=yes`, while the indexer socket is `root:root` mode `0600` and
the daemon also checks that the Unix peer UID is `0`. Giving the webserver
direct access would widen its privileges and move or duplicate bridge route
authorization. Keeping the indexer client in the bridge preserves the existing
webserver, bridge, and authentication boundaries.

| Boundary | Owner |
|----------|-------|
| Scanner, generations, SQLite storage, HTTP/SSE daemon | `linuxio-indexer` (`backend/indexer/internal/{indexing,storage,daemon,searchquery,configfile,cli}`); the `storage.Store` owns generations and the reindex transaction |
| Daemon wire routes and types | `backend/indexer/api` |
| Unix-socket client and LinuxIO API adapters | `backend/bridge/handlers/indexer` |
| Public generated bridge contract | `backend/bridge/apischema` |
| Filesystem validation and mutation policy | `linuxio-bridge` file-browser handlers |
| Authentication and privileged-session creation | `linuxio-auth` and the bridge bootstrap path |
| Browser transport relay | `linuxio-webserver` |

One daemon orders all the work on the index. A full scan is a process of its
own, the same binary in another role, which systemd runs as
`linuxio-indexer-index.service` and which scans only when the daemon has given
it its turn ([Full index](#full-index)).

## Installed files and units

| Path or unit | Purpose |
|--------------|---------|
| `/usr/libexec/linuxio/linuxio-indexer` | The daemon and, with `--index-mode`, the scanner |
| `/etc/linuxio/indexer/config.yaml` | Strict two-field YAML configuration |
| `/var/lib/linuxio/indexer/indexer.db` | Persistent, rebuildable SQLite cache |
| `/run/linuxio/indexer.sock` | Root-only Unix API socket, `root:root`, mode `0600` |
| `linuxio-indexer.socket` | Owns the socket and activates the daemon |
| `linuxio-indexer.service` | The sandboxed daemon: queries, updates, scoped reindexes |
| `linuxio-indexer-index.timer` | Periodic full-index schedule |
| `linuxio-indexer-index.service` | The scanner: one full index, at low priority |

The daemon is socket activated. Starting the webserver only warms the daemon
for queries; it does not request an index. The timer starts its first scan
15 minutes after it starts, so a new install or a rebooted host is not left
without an index, and then repeats at the configured interval, 24 hours by
default. Every start of the timer counts: a boot, a restart of
`linuxio.target`, a package upgrade and a saved interval each bring one full
index 15 minutes later. A monotonic timer has no memory of the last run across
a restart, so it cannot tell that the index is still fresh.
`linuxio-webserver.service` owns `/run/linuxio/webserver`, an activity marker
that keeps the indexer warm while the webserver is running. The marker does not
grant socket access.

When the marker is absent and no request, event stream, or scan is active, the
daemon exits after a 90-second idle grace. The socket remains ready to activate
it again.

On btrfs the state directory is marked no-CoW (`+C`), by a line in the
package's tmpfiles file, as systemd does for its journal. SQLite rewrites
pages in place, which copy-on-write turns into tens of thousands of pieces: a
1.25 GB index on one host was in 89,084. The mark applies to files created
after it is set, so an index that already exists keeps copy-on-write until it
is recreated, and it does nothing on other filesystems. tmpfiles also creates
the directory, with the mode of the service's `StateDirectory`, because at
install the service has not made it yet.

The daemon's service can write only its configuration and state directories,
and the scan unit only the state directory. Both are limited to Unix sockets
and keep `CAP_DAC_READ_SEARCH` alone, for reading the host's files. Shutdown cancels background work, closes HTTP listeners,
waits for the work to exit, and then closes SQLite.

## Fixed policy and configuration

The installed database location is fixed:

```text
/var/lib/linuxio/indexer/indexer.db
```

There is no supported database-path setting or command-line flag. `db_path` is
an unknown YAML/JSON field and is rejected. The daemon and its private worker
both use the compiled path. Tests may pass temporary paths directly to internal
storage functions, but that is not an operator interface.

The persisted configuration contains only operator scan choices:

```yaml
exclude_paths: []
include_network_mounts: false
```

`exclude_paths` adds paths to the mandatory exclusions. The scanner always
excludes `/proc`, `/dev`, `/sys`, `/mnt/c`, and `/var/lib/linuxio/indexer`, so virtual
filesystems and the database cannot index themselves. Mandatory exclusions
cannot be removed through the file or API. Unknown fields and invalid values
fail strict configuration decoding.

Network mounts are skipped unless `include_network_mounts` is true. A mount
counts as one by its filesystem type (NFS, SMB/CIFS, Ceph, GlusterFS, 9p,
sshfs, rclone, s3fs, WebDAV, iSCSI) or by a source that is a share or a URL
(`externalFSTypes` in `backend/indexer/internal/indexing/exclusions.go`). A
local disk reached through FUSE, such as an NTFS drive or a mergerfs pool, is
not a network mount and is indexed.

A container's overlay mount is always skipped, whatever that setting: a
running container's root filesystem shows again what its lower folders
already hold where Docker or Podman stores them, and the engines mount it in
different places (`/var/lib/docker/rootfs/overlayfs/` with the containerd
image store). An overlay is a container's when it is mounted below a
container engine's folder or made of folders an engine keeps its layers in,
which also covers an engine whose data was moved (`isContainerOverlay`).
Other overlays are part of the host and are indexed: a system extension over
`/usr`, one an administrator put over a data folder, a root filesystem that
is one. Docker overlay `merged` trees are skipped by path as well. Directory
symlinks are recorded as entries but are not traversed.

| Setting | Fixed value |
|---------|-------------|
| Index root | `/` |
| Database | `/var/lib/linuxio/indexer/indexer.db` |
| SQLite options | `storage.DefaultOpenOptions()` |
| Full-text search | enabled |
| Hidden entries | included |
| Search result cap | `100` |
| Search query cap | `256` bytes |
| Minimum search length | `3` characters |
| Idle grace | `90s` |
| Idle check interval | `15s` |
| Copies of the index | one, updated in place |
| Scan commit | every 50,000 rows written |

The daemon-side values live in `backend/indexer/internal/daemon/policy.go`; the
database path (`storage.DefaultDBPath`) and SQLite options live in
`backend/indexer/internal/storage/policy.go`.

Both configuration fields apply to the next scan without a daemon restart. The
recurring interval is a systemd timer setting, not a YAML field. LinuxIO writes
it through `indexer.set_timer_interval`, as a drop-in that sets only
`OnUnitActiveSec`; how soon the first index runs after the timer starts stays
what the unit says. A drop-in saved before v0.57 also sets `OnActiveSec` to
the interval and keeps doing so until the interval is saved again.

The default is a day because a full index rewrites the whole database: on a
host with 1.85 million entries one run wrote 9.2 GB.

### Size accounting

Sizes follow GNU `du` semantics within the configured scan scope. Files and
directories use allocated bytes (`st_blocks * 512`), including each
directory's own blocks. Hard-linked file data is counted once per
`(device, inode)` pair, directory symlinks are recorded but not traversed, and
deliberate exclusions remain outside the accounting scope rather than being
reported as scan failures.

The same invariant applies to full scans, scoped reindexes, and individual
add/delete notifications. The database records hardlink identity and which
path contributes the allocation so deleting or reindexing one link can promote
a surviving link without changing the total.

A scan is given the path that carries each group's size today and counts that
path and no other (`WithHardlinkContributors`): a scan that met another link
first would otherwise write rows and folder sizes that the step below then
writes back. Every file's device and inode stay indexed, not only those with
several links, because a file is indexed while it has one link and the link
added to it later is matched to that row by them. A full scan narrows the
snapshot it takes to files recorded with several links, which is nearly none
of them; it rescans every row, so a link count that went stale is corrected.
The reindex of a folder reads every file of it: a file with one link that
was moved into it outside LinuxIO has a row left at its old path, and only
the device and inode show that the two rows are one file. A link whose other
links are not in the index always counts.

Before a scoped reindex commits, it settles every hardlink group with a link
in the subtree: the link that contributed before the rescan keeps
contributing while it still names the file, so a rescan that met another link
first does not move the allocation to another folder. The size changes this
makes are added up per folder and written once per folder; inside the
subtree they stop at the reindexed path, whose own change then moves its
ancestors.

## Operation

### Full index

A full index is the work of the scanner, `linuxio-indexer --index-mode`,
which systemd runs as `linuxio-indexer-index.service`: from the index timer,
or because someone pressed Index now. Daemon and scanner are one program in
two roles. The daemon no longer starts the scanner as its child, and the
timer no longer asks the daemon to: the unit's own result, duration and
resource use are the scan's (`systemctl status linuxio-indexer-index.service`).

The scanner does not scan on its own word. Two writers to one SQLite file
would wait on each other's locks and time out, and a scan that compares a
folder with its rows must not have those rows change under it. So the daemon
stays the one place that orders the work:

1. The scanner connects to the daemon's socket (`POST /scan`), which starts
   the daemon when it is not running. The daemon has by then checked the
   database and set a broken one aside, which is why the scanner never has
   to.
2. The daemon puts the scan in its work queue and answers when its turn has
   come: one line naming the operation. Work that arrives during the scan
   waits behind it, as behind any other work.
3. The scanner scans (below) and reports progress and, at the end, its totals
   or its error (`POST /scan/event`). The daemon passes them on to whoever
   follows the index.
4. The connection of step 1 stays open for the whole scan and is the scan:
   when it closes without a result, because the scanner died, the daemon ends
   the index with an error and takes up the work that waited. A scanner that
   left before its turn takes its index out of the queue with it. When the
   daemon stops, the connection closes and the scanner stops with it, since
   nothing would keep other writes out of its way.
5. A scanner that is told to stop (`systemctl stop`, a shutdown) keeps the
   connection, and with it its turn, until its scan has unwound and it has
   reported how it ended: its transaction is still open while it rolls back,
   and the daemon's next write would wait on it.

Index now asks systemd to start the scan unit and then asks the daemon for
the operation to follow (`POST /index`). Whichever of the scanner and that
request reaches the daemon first puts the index in the queue, and the other
joins it; a start that fails leaves nothing in the queue. Asking again while
an index waits or runs gets that same operation, and systemd does not start a
unit that is running. A request in the instant a scan has completed, while
its unit is still ending, gets the operation that has just completed. An index that was asked for and that no scanner joins
within 30 seconds of its turn fails, with a pointer to the unit's journal.

The scanner:

1. opens the canonical SQLite database;
2. walks `/`, applying mandatory and operator exclusions;
3. compares each folder with what the index holds for it, and inserts,
   updates or deletes only the entries that differ;
4. updates the folder sizes and the hard link accounting, commits, and
   checkpoints SQLite.

There is one copy of the index, updated in place. A full index is the scoped
reindex of `/`: the same code brings a subtree up to date after a file
operation and the whole index up to date on the timer. A scan that finds
nothing changed writes nothing. On a host with 963,000 entries the first scan
took 35 seconds and wrote every row, and the next took 9 seconds and wrote 19
(`BenchmarkRescanOfTree`, run with `make bench-go`). Writing a second copy of
the index on every scan and deleting the first, as the indexer did before,
wrote 9.2 GB a run on a host with 1.85 million entries.

How a scan compares (`backend/indexer/internal/storage/reconcile.go`):

- The scanner sends a folder's files and then the folder itself, subfolders
  first. Storage holds a folder's files until the folder arrives, reads the
  folder's stored children with one range read, and writes what differs.
  What is stored for a folder and was not seen on disk is deleted, a
  subfolder with everything below it. Nothing is remembered beyond the
  folders the scanner is inside of.
- A folder with more than 2,048 files is compared early, by a lookup per
  file, and only the paths are kept until the folder arrives.
- The scanner reads a folder 1,024 names at a time, looks at each entry
  through the folder's handle, and opens a subfolder through it without
  following a symlink (`openDirectory` in
  `backend/indexer/internal/indexing/scan.go`). In a folder another user may
  write, an entry the scan saw as a folder can be a link by the time it is
  opened, and root would walk wherever it points.
- A folder the scan gives up on, because it cannot be read to its end, is
  left out of the index or vanished, loses its rows: the stored ones and
  those its subfolders had already sent. A path that held a folder and now
  holds a file loses what was below it.
- The folder a scan starts at is opened one folder at a time from `/`, so
  that no folder above it is reached through a symlink either. The index
  knows every folder by its real path.
- A full scan commits every 50,000 rows it writes. A rescan that changes less
  is one transaction, which readers see all at once. A first scan, or a
  rescan that changes more, becomes visible in parts, and a scan that is
  stopped half-way leaves what it had committed; the next scan carries on
  from it, and counts the entries that are there when it ends. The reindex of
  a folder is always one transaction.

The scan unit runs at the lowest CPU priority (`Nice=19`) and in the idle I/O
class (`IOSchedulingClass=idle`), so a scan yields the CPU to everything else
on the host, the daemon's searches included. The I/O class covers what the
scanner reads while it walks, on a disk whose scheduler honours it (BFQ,
mq-deadline). It does not cover the database pages the scanner writes: the
kernel flushes those later, outside the scanner's priority.

Queries are answered while a scan runs, from the rows as they are committed.
Ordinary per-entry filesystem
errors, including inaccessible files or subtrees, are logged and counted, then
the walk continues. A stop of the unit or the loss of the connection to the
daemon aborts the scan; a progress report that does not arrive does not.

Until the first scan has completed, status is `uninitialized`: its rows are
in a generation that is published when the scan commits. Search, directory
details, and subfolder queries return `503 Service Unavailable` in that state.

### Scoped reindex

`POST /reindex?path=...` brings one subtree up to date. It scans the subtree,
writes the entries that differ, removes those that no longer exist, updates
ancestor aggregates by the size delta, and commits. A folder the index has no
row for can have rows below it all the same, from the reindex of a folder
inside it, which the folders above already count: its reindex takes what
those rows count as its old size, so that nothing is added twice. If the path no longer exists, its stale cached subtree
is deleted. A request for a regular file is rejected; reindex its parent or use
`POST /add` for the one entry.

### Refresh when a folder is opened

The index has no watcher, so a change made outside LinuxIO is unknown to it
until the next scan. To show it where someone is looking, every folder
listing the file browser asks for (`filebrowser.list_directory`) is followed
by `POST /refresh` for that folder's real path, from a session with sudo; a
session without it cannot use the index and asks for nothing. The listing is
answered first and never waits for the refresh
(`listOpenedDirectory` in `backend/bridge/handlers/filebrowser/resource_read.go`).

The daemon reads the folder's own entries, one level deep, reads their rows
with one range read and writes what differs (`RefreshFolder` in
`backend/indexer/internal/storage/refresh.go`): a file that is new or
changed is written, an entry that is gone loses its row and everything below
it, and the sizes of the folders above follow. It does not go into
subfolders. One it finds new gets a scoped reindex of its own, queued behind
the refresh, and a folder that has no row at all is reindexed whole, once in
the window below. A folder
that did not change costs the read of its entries and their rows, and no
write.

Going in and out of a folder does not read it every time:

- The page caches a listing for two seconds, so a return within that time
  reaches neither the bridge nor the daemon.
- Refreshes of one folder that wait collapse into one, as other work does.
- The daemon does not read again a folder it refreshed in the last 30 seconds
  (`refreshWindow`) and answers `200` without queuing anything. A change made
  outside LinuxIO inside that window shows up one visit later. What LinuxIO
  itself changes reaches the index through the notifications below, whatever
  the window.
- A folder with more than 10,000 entries (`refreshEntryLimit`) is not
  refreshed when it is opened; it waits for the scan.

Like a scan, the refresh reaches the folder one component at a time without
following a symlink, looks at each entry without following one, and leaves
out what the index leaves out, which it decides before it touches the disk.

### Mutation reconciliation

After a successful bridge filesystem mutation, the bridge sends a best-effort
notification:

| Change | Daemon action |
|--------|---------------|
| Create or replace one non-directory entry | `POST /add` |
| Remove a path | `DELETE /delete` |
| Copy or extract a directory tree | scoped reindex |
| Move or rename, into or out of the trash included | `POST /move` |

These notifications update the cache; they are not part of the mutation's
authorization or durability. A failed notification is logged and a later
scoped or full index repairs the cache. There is no durable notification queue.
`POST /add` rejects `/` and directories; use a scoped reindex for directory
trees.

The daemon looks at a path a request names the way a scan reaches it: every
folder on the way is opened without following a symlink (`Lstat` in
`backend/indexer/internal/indexing/folder.go`). `os.Lstat` follows the links
in the folders above the entry, so in a folder another user may write that
user could choose what root records under a path of the tree. A path that
leads through a link names nothing the index holds and loses its rows, as one
that is gone does.

A move renames rows. The files are the same files, so the daemon gives the
row of the old path, and every row below it, its path below the new one, and
moves the size from the folders above the old path to those above the new
one (`MovePath` in `backend/indexer/internal/storage/move.go`). Nothing is
read from disk beyond the two paths themselves, and the search index is
touched only for the moved entry's own name, when that changed. It does so
when the disk and the index show a move that is a rename:

- nothing is left at the old path and the new one is there;
- the index holds a row for the old path whose kind, device and inode are
  those of what is at the new path. A scan records them for folders as well
  as for files. After a move to another filesystem, which is a copy and a
  delete, the numbers differ; after a move into a folder that was already
  there, the folder is the old one and its numbers are its own;
- the folder it moved into has a row, and the index holds no row at the new
  path or below it. Rows there are what the move replaced, or the moved files
  themselves, written by a refresh or a scan that ran since the move and
  which took the size from the rows at the old path;
- neither path is one the index leaves out;
- no other work for a path at or below the old one ran or waited when the
  move was queued. An update for a file created there, still waiting when the
  folder moved, names a path that is gone by the time it runs.

Otherwise the index follows the disk for both paths, as it did for every move
before: the rows of a path that is gone are removed, a file is added, and a
folder is walked by a scoped reindex. `POST /move` is answered at once in
either case.

### Sizes that are still catching up

A file operation reports done when the filesystem work is done and never
waits for the index, which is a cache of the filesystem and catches up
afterwards. Holding "done" back would make a finished copy look stuck while a
scan runs, and would show a successful operation as failed when the indexer
is slow or down. The page asks for the sizes again as soon as the operation
ends, so that request can reach the index before the update does.

A size answer therefore says when it is not final. The daemon sets `pending`
on `/dirsize` and `/subfolders` while work that can change what is counted
below the folder runs or waits: work about a path at or below it (an update,
a move, the refresh of an opened folder), or the reindex of a folder it is
in (`sizePending` in `backend/indexer/internal/daemon/queue.go`). It reads
that before it reads the numbers, so work that ends in between costs one more
request and never passes old numbers off as final. A full index is not
counted, since every size would be pending for as long as a scan runs.

The bridge adds its own part. A finished task tells the indexer of its
changes after it reports done, from a goroutine of its own, and a size asked
for in those milliseconds would find an indexer that has not heard of them.
While such a notification is unanswered the bridge marks every size answer
pending (`indexerUpdatesInFlight` in
`backend/bridge/handlers/filebrowser/indexer_sync.go`); the refresh of an
opened folder counts, so the sizes shown in a folder that was just opened
include what the refresh found.

The page asks again 1.5 seconds after an answer marked pending, for as long
as it is (`getDirectorySizeQueryOptions` in
`frontend/src/routes/_authenticated/filebrowser/-hooks/useFileDirectorySizeBase.ts`),
and keeps a final answer for five minutes as before.

### Concurrency and recovery

One operation runs at a time: a full index, a scoped reindex, the refresh of
an opened folder, a move, or the update of one entry. A request that arrives while another runs waits in the daemon's
work queue for its turn (`backend/indexer/internal/daemon/queue.go`), so an
update the bridge sends during a scan, or right behind another update, is
applied when the work ahead of it ends. Refusing them instead left the index
wrong until the next full index: a batch copy of several folders got only its
first folder indexed.

- An add or a delete on a free daemon answers `200` when it has been applied.
  Behind other work it answers `202` with status `queued` at once and is
  applied when its turn comes: its caller is a file operation in the UI,
  which never waits for a scan. A queued update that fails is logged.
- A reindex answers `202` at once with its operation ID, and its event stream
  can be attached to while it still waits: the client is told it waits, and
  the events start when the work does.
- Repeated requests for one path collapse into the one that is still waiting,
  so many updates to one path are one running and one waiting. A repeated
  reindex request gets the waiting one's operation ID, and a request for a
  full index while one waits or runs gets that one's.
- A configuration write is refused with `409 Conflict` while a full index or
  a reindex runs or waits: a scan reads the exclusions when it starts. The
  updates of file operations and the refreshes of opened folders do not hold
  it back.

An add and a delete both make the index agree with the disk for their path,
whichever the request was: by the time its turn comes, a path that was added
may be gone, and one that was deleted may be there again. A path that is gone
or left out of the index loses its rows, a file is added or replaced, and a
folder gets a reindex of its own, which a request for the same folder that
follows joins, and the update answers `202 queued`. Work with a stream that
fails, or is dropped because the daemon stops, ends its stream with an error
event, since a client takes a stream that just closes for work that is done.

The queue is in memory. Work that waits when the daemon stops is not applied
(a caller still waiting gets `503`), and the next full index makes it good. Active requests, streams, scans and
waiting work pin the daemon so idle shutdown cannot interrupt them.

A transaction takes SQLite's write lock when it begins (`_txlock=immediate`).
One that reads first and writes later is refused outright, not made to wait,
when another connection holds the lock at that moment. Nothing the driver
runs on every new connection may take that lock either: `PRAGMA auto_vacuum`
did, so a search that needed a connection the pool did not have waited behind
a reindex's transaction and failed with "database is locked" when the
five-second busy timeout ran out. That pragma is gone with the second copy it
served, and the WAL journal is set once per open (`prepareDatabase` in
`backend/indexer/internal/storage/open.go`). Temporary data stays in files
under `SQLITE_TMPDIR`: in memory, the statement that deletes a removed folder
of a million entries would hold a copy of every page it touches.

The indexer is built with the driver's `sqlite_fts5` and
`sqlite_omit_load_extension` tags: search needs the first, and a root daemon
that loads no SQLite extension is built without the feature.

SQLite schema compatibility is checked at open through `PRAGMA user_version`.
When SQLite confirms corruption, the daemon moves the database and any WAL/SHM
sidecars to `indexer.db.corrupt-<UTC timestamp>*`. An incompatible schema uses
`indexer.db.incompatible-<UTC timestamp>*`. Schema version 3 is the current
version; a mismatch causes one incompatible-cache quarantine and creates an
empty version 3 cache for the next scan to populate. Busy, timeout, permission,
and ordinary I/O errors preserve the existing files and fail startup instead of
replacing data.

One quarantined copy is kept, the newest. Each is a whole database, so once a
quarantine has set the new copy aside it deletes those of earlier ones
(`removeQuarantinedCopies` in `backend/indexer/internal/storage/open.go`). A
quarantine that fails deletes nothing, and only files named as a quarantine
names them are deleted: a copy someone renamed to keep stays.

The scanner never performs quarantine. This prevents it from renaming a
database behind the daemon's open handle, and it does not need to: its first
step starts the daemon, which has checked the database before it answers.

## Public LinuxIO bridge API

The frontend uses the generated bridge contract, not the daemon's Unix HTTP
API. Every route below is privileged. Unprivileged sessions are rejected by the
bridge before an indexer request is made.

| Route | Kind | Request | Result |
|-------|------|---------|--------|
| `indexer.get_config` | Call | none | `exclude_paths`, `include_network_mounts`, `interval` |
| `indexer.get_status` | Call | none | daemon state, counts, sizes, active operation, warning |
| `indexer.set_config` | Call | partial `exclude_paths` and/or `include_network_mounts` | saved combined config |
| `indexer.set_timer_interval` | Call | `interval` | saved `interval` |
| `filebrowser.index` | session Task, exclusive (one at a time) | optional `path`; absent or empty means `/` | progress plus final index/reindex totals |
| `filebrowser.search` | Call | `query`, optional `basePath`, optional compatibility `limit` | matching filesystem entries |
| `filebrowser.dir_size` | Call | `path` | `size`, `fileCount`, `folderCount`, and `pending` while an update for the folder has not been applied |
| `filebrowser.subfolders` | Call | `path`; empty means `/` | direct subfolder paths and sizes, and `pending` as above |

The daemon always caps search at 100 results. The public `limit` field remains
in the bridge request shape for compatibility but cannot raise or lower that
daemon cap. Searches shorter than three characters are rejected. Name matching
is case-insensitive substring matching by default; the file browser's **Match
case** toggle uses the compatible `case:exact` modifier for case-sensitive
substring matching.

`filebrowser.index` maps daemon SSE events into normal LinuxIO Task progress and
results. The bridge gives every progress frame a `phase` (`running` and
`started` become `indexing`, `scanning` becomes `scan`), and the task tray reads
the frame's `status` only for the daemon's own word `running`. If it finds an already-running matching daemon operation, it attaches
instead of starting another scan. Task observation and recovery use the common
`tasks.*` routes.

The status result contains:

| Field | Meaning |
|-------|---------|
| `status` | `uninitialized`, `running`, or `idle` |
| `running` | whether an index/reindex operation is active |
| `num_dirs`, `num_files`, `total_size` | latest completed generation totals |
| `last_indexed` | completion time in RFC 3339 form, when initialized |
| `database_size` | current SQLite file size in bytes |
| `active_operation`, `active_operation_id`, `active_path` | active work identity, when running |
| `warning` | partial status-read warning, when available |

The daemon's own `/status` also lists `waiting_operations`: the operation,
ID and path of each index or reindex queued behind the one that runs. The
bridge keeps them to itself and uses them to find an operation again that
still waits, for a page that was reloaded.

The daemon's internal `/status` response has the same snake-case fields except
for `running`, which the bridge derives from `status`. Absent string values are
empty or omitted on the internal wire and become nullable fields in the public
bridge result.

## Internal daemon HTTP/SSE API

This is an in-repository service contract defined in `backend/indexer/api`. It
is not a network or third-party API. The server accepts only its systemd Unix
socket and checks `SO_PEERCRED`; every endpoint requires peer UID `0`.

Successful non-streaming responses use JSON. Errors use an HTTP status and a
plain-text message.

| Method and path | Input | Success response | Notes |
|-----------------|-------|------------------|-------|
| `POST /index` | none | `202` operation response | Reserves a full index in the queue, or follows the one that waits or runs; the caller starts `linuxio-indexer-index.service` next |
| `POST /scan` | none | `200`, one line with the operation once the scan's turn has come; the response then stays open until the scan ends | The scanner's connection; `409` while another scanner is attached |
| `POST /scan/event?operation_id=<id>` | strict JSON `{"type":"step"\|"scan"\|"summary"\|"error",...}` | `{"status":"ok"}` | The scanner's progress and result; `409` when no scan runs for that operation |
| `POST /reindex?path=<path>` | absolute path | `202` operation response | Reconciles one directory subtree |
| `GET /status` | none | status object | Returns current state and latest completed totals |
| `GET /status?stream=1&operation=<op>&operation_id=<id>&path=<path>` | exact operation tuple | `text/event-stream` | Attaches to work that runs or waits; a mismatch falls back to JSON status |
| `GET /search?q=<query>&base=<path>` | query and optional base, default `/` | array of entry results | Query max 256 bytes; result cap 100; `limit` is ignored |
| `GET /dirsize?path=<path>` | optional path, default `/` | path, recursive size, file count, directory count, `pending` | Reads cached aggregates; `pending` is set while work that can change them runs or waits |
| `GET /subfolders?path=<path>` | optional path, default `/` | `{"subfolders":[...],"pending":bool}` | Each result includes path, name, size, and modification time |
| `POST /add` | strict JSON `{"path":"/absolute/path"}` | `{"status":"ok"}` | Makes the index agree with the disk for that path once its turn comes; excluded paths are a successful no-op |
| `DELETE /delete?path=<path>` | non-root path | `{"status":"ok"}` | The same as `/add`: a path that is gone loses its rows and its ancestors are adjusted |
| `POST /move` | strict JSON `{"from":"/old/path","to":"/new/path"}`, two different paths below `/` | `202` `{"status":"queued"}` | Renames the rows of a moved path in its turn, or lets the index follow the disk for both paths |
| `POST /refresh?path=<path>` | absolute path of a folder | `202` `{"status":"queued"}`, or `200` `{"status":"ok"}` when the folder was refreshed in the last 30 seconds | Compares the folder's own entries with the disk in its turn; nothing reports the result |
| `GET /config` | none | persisted two-field config | Timer interval is not part of this response |
| `PUT /config` | strict partial config JSON | full saved config | Maximum body 1 MiB; atomic config-file write |

Paths are normalized absolute paths rooted at `/`. Traversal outside that root
is rejected.

An accepted index or reindex returns:

```json
{
  "status": "running",
  "path": "/",
  "operation_id": "<opaque operation ID>"
}
```

Use all three returned identity values to attach to its event stream. SSE event
names and payloads are:

| Event | Payload |
|-------|---------|
| `started` | `status`, `operation`, `operation_id`, `path` |
| `progress` | operation identity plus optional `phase`, `message`, `files_indexed`, `dirs_indexed`, `current_path`, `bytes_indexed` |
| `complete` | operation identity, `duration_ms`, totals, and optional `deleted_entries` |
| `error` | operation identity and `message` |

Search entry results contain `path`, `name`, `type`, `size`, `mod_time`,
`inode`, and directory aggregate fields when applicable. Direct-subfolder
results contain `path`, `name`, `size`, and `mod_time`.

Common error statuses are:

| Status | Meaning |
|--------|---------|
| `400 Bad Request` | invalid path/body/query, missing generation for a mutation, or invalid config |
| `403 Forbidden` | non-Unix transport or peer UID other than root |
| `405 Method Not Allowed` | wrong method on a mutation or config endpoint |
| `409 Conflict` | a configuration write arrived while work runs or waits |
| `500 Internal Server Error` | storage, config write, or unexpected daemon failure |
| `503 Service Unavailable` | cache query before the first completed generation, or an update whose caller was still waiting when the daemon stopped |

## Administration

Start a full scan and inspect status:

```bash
sudo curl --unix-socket /run/linuxio/indexer.sock \
  -X POST http://localhost/index
sudo curl --unix-socket /run/linuxio/indexer.sock \
  http://localhost/status
```

Inspect or change daemon configuration:

```bash
sudo curl --unix-socket /run/linuxio/indexer.sock \
  http://localhost/config
sudo curl --unix-socket /run/linuxio/indexer.sock \
  -H 'Content-Type: application/json' \
  -X PUT \
  -d '{"include_network_mounts":true}' \
  http://localhost/config
```

Useful diagnostics:

```bash
linuxio logs indexer
linuxio version
systemctl status \
  linuxio-indexer.socket \
  linuxio-indexer.service \
  linuxio-indexer-index.timer
journalctl \
  -u linuxio-indexer.service \
  -u linuxio-indexer-index.service \
  --since today
```

`sudo linuxio verbose enable` enables debug logging through service drop-ins for
the webserver, the indexer and the monitoring daemon. `sudo linuxio verbose
disable` removes them. Daemons switch live (SIGHUP reload, no restart); bridges of already-open
sessions switch at the next login. Changes persist across restarts; the same
switch is in Settings.

Upgrades preserve compatible caches. If manual recovery is needed, stop
LinuxIO, move `/var/lib/linuxio/indexer/indexer.db*` to a separate directory,
start LinuxIO, and run a full index. Move rather than delete the files so the
old cache remains available for inspection.

## Known limits

- A request for an index in the instant a scan has failed can itself fail,
  after 30 seconds, with "the scanner did not start": the failed scan has
  left the daemon's queue while its unit has not ended yet, and systemd does
  not start a unit that is running. Asking again works.
- A full scan that is stopped after it has committed part of its rows (it
  commits every 50,000) leaves the folders it had not reached with their old
  sizes, which no longer add up with the rows below them, until the next
  full scan ends.
- A scanner that hangs holds the queue for as long as it lives: once one has
  attached, the daemon waits for its result or for its connection to close,
  with no time limit, since a scan of a large host has no fixed length.
  Stopping `linuxio-indexer-index.service` closes the connection and frees
  the queue.
- Folder details count the files and folders below the folder on every
  request (`DirDetails` in `backend/indexer/internal/storage/queries.go`),
  which for `/` on a host with 1.85 million entries takes over a second. The
  size is read from the folder's row.
- The scanner's idle I/O class gives it the disk only when nothing else wants
  it. On a disk that stays busy a scan takes long, and the updates of file
  operations wait behind it; their sizes are marked pending for that time.
- An update for a path inside a folder that reaches the daemon after the
  folder was renamed on disk names a path that is gone, and is lost: the
  bridge sends the update of a finished upload or extraction detached, so it
  can arrive after a rename that followed it at once, before or after that
  rename's own notification. The file shows up when its folder is opened or
  at the next scan.
- A move renames rows as they are. What the index did not know of the moved
  folder before the move (a change made outside LinuxIO) it does not know
  after it either, where the walk every move used to cause would have found
  it.
- A size the page already holds is kept for five minutes. Opening a folder
  again inside that time refreshes it in the index, and the page shows the
  new sizes when it next asks: after a file operation, or once the five
  minutes have passed.
- A folder opened in the file browser is refreshed one level deep. A change
  made outside LinuxIO further down shows up when that folder is opened, or
  at the next scan; until then the sizes of the folders above it are the old
  ones. A change inside a folder of more than 10,000 entries waits for the
  scan.

- A scan holds the hard link groups of the subtree it scans in memory, one
  path each, to keep their sizes where they are. A tree of backups made of
  millions of hard links costs that many paths.
- The index for one folder's children is keyed by depth and path, so it
  repeats every path: on the host of 1.85 million entries it is about a
  quarter of the database. An index keyed by the parent's row would be
  smaller; it was not built.
- Case-insensitive search folds case as Unicode does. The smaller search
  index (`detail=none`) was not taken: its `LIKE` folds ASCII only, so
  "RELATÓRIO" would no longer find "relatório".
- On btrfs the index is not checksummed, which is what no-CoW costs. It is a
  cache that a full index rebuilds. On a btrfs with mirrored data, a disk that
  dropped out and came back can then serve old pages of the index without
  btrfs noticing; the index is quarantined or wrong until the next full index.
- A second quarantine before a scan has completed keeps the newer, empty copy
  and deletes the one that held an index.
- A scoped reindex reads each hardlink group in its subtree with a query of
  its own. The reconcile step alone took about 1.2 to 1.7 seconds per 100,000
  hardlinked files in a tree of 200,000 on a development machine, with the
  per-link `lstat` stubbed out and without the hardlink snapshot taken when
  the reindex begins (`BenchmarkReconcileHardlinksAfterReindex` in
  `backend/indexer/internal/storage/hardlinks_bench_test.go`).

## Disposable-host smoke test

The repository includes an opt-in systemd smoke test
(`packaging/scripts/tests/systemd-indexer-smoke.sh`). It installs a LinuxIO
package with apt or dnf, checks the indexer's socket activation, idle exit
and index timer, and removes the package again, checking that the removal
stops the indexer and keeps `/var/lib/linuxio` and the index in it. It
requires the package, root, systemd as PID 1, and a disposable host with no
LinuxIO on it; at the end it purges what the package left and deletes
`/etc/linuxio` and `/var/lib/linuxio`. The test VM is one:
`make test-vm-backend SUITES=indexer` builds LinuxIO, creates the VM when it
is missing, reverts it, assembles the package in the VM as `make vm-install`
does and runs the script there with `LINUXIO_SMOKE_PACKAGE` naming it. On
another disposable host:

```bash
make build package-deb   # or package-rpm
sudo LINUXIO_SYSTEMD_SMOKE=1 \
  LINUXIO_SYSTEMD_SMOKE_CONFIRM=disposable-linuxio-host \
  LINUXIO_SMOKE_PACKAGE="$(echo build/packages/linuxio*)" \
  packaging/scripts/tests/systemd-indexer-smoke.sh
```

`make test-indexer-systemd-integration` runs the same script with the
variables of its environment. Without `LINUXIO_SYSTEMD_SMOKE=1`, the script
exits without changing the host.
