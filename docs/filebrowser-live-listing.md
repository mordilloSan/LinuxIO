# File Browser Live Listing

The folder the file browser shows updates by itself: a file another program
creates, deletes or renames in it appears or goes without a reload, as an
open folder does in Windows File Explorer. The page reads the listing as
before (`filebrowser.list_directory`); what is new is that the bridge tells
the page when to read it again.

## The watch

While the page shows a folder listing it holds one Channel,
`filebrowser.watch` (request `PathRequest`), for that folder
(`useFolderWatch` in
`frontend/src/routes/_authenticated/filebrowser/-hooks/useFolderWatch.ts`,
called by the folder view in `FileBrowserPage.tsx`). The bridge
(`backend/bridge/handlers/filebrowser/folder_watch.go`):

1. Resolves and opens the folder exactly as the listing does
   (`services.OpenDirectory`, the listing's own `openDirectoryForRead`): the
   path is resolved whole for no owner with `linksafe.Resolve`, so every link
   is followed as the session user's own programs would, and the real folder
   is opened through `os.Root`. A path the listing refuses is refused with the
   same status (a missing path or a file `400`, a dangling link `404`, and a
   link loop fails as the listing fails), and the Channel answers with an
   error result.
2. Adds one watch through `/proc/self/fd/<n>` of the opened folder to the
   bridge's inotify instance, so the folder watched is the one that was
   resolved and no link is followed a second time. The folder's handle is
   closed once the watch is on it: the kernel holds `IN_DELETE_SELF` back
   until the last handle of a deleted folder is closed, so an open handle
   would hide the folder's removal.
3. Waits for the signals the instance's reader leaves it, and paces them
   into frames (below).

A bridge has one inotify instance for all its watches (`folderWatcher`),
because instances are few and shared: `fs.inotify.max_user_instances`, often
128, counts every process of the uid, which for a session with sudo is root's.
Watches are plentiful. The instance (`IN_NONBLOCK | IN_CLOEXEC`) is created
with the first watch and closed when the last one ends; its one reader
goroutine reads it through an `*os.File`, ends when it is closed, and has
returned before a later first watch makes a new instance, so nothing runs
while nothing is watched. The reader hands each event to the Channels on its
watch descriptor and never waits on one: a Channel's signal is a one-slot
channel that a change already left covers, and its own pacer does the timing,
so a Channel that is slow to write delays no other.

Two tabs on one folder get the same watch descriptor, since the kernel keeps
one watch per inode and instance. Each tab is its own Channel and counts
toward the cap; the kernel watch is removed when the last of them ends. A
folder deleted, moved or unmounted is `gone` for every Channel on it; the
descriptor's entry is dropped when its `IN_IGNORED` is read (a moved folder's
watch is removed first, as the kernel keeps it), and a number the kernel
gives again before that skips the stale `IN_IGNORED`. A full event queue is a
change for every Channel. A read of the instance that fails other than by
its close ends every watch: the Channels close without an error result, so
the pages open them again, and the next watch gets a new instance.

The watch reports `IN_CREATE`, `IN_DELETE`, `IN_MOVED_FROM`, `IN_MOVED_TO`,
`IN_CLOSE_WRITE` and `IN_ATTRIB` on the folder's entries, and the folder
itself deleted or moved (`IN_DELETE_SELF`, `IN_MOVE_SELF`, and `IN_UNMOUNT`
or `IN_IGNORED`, which end a watch too), with `IN_ONLYDIR`. It does not
report `IN_MODIFY`: a file being appended to would fire it at every write,
and its new size shows when the writer closes it. A full event queue
(`IN_Q_OVERFLOW`) counts as a change. It is never recursive: a change in a
subfolder changes that subfolder's listing, not this one.

### Frames

The Channel sends data frames, each one line of JSON ending in a newline, as
the log Channels send their lines (`writeFolderWatchFrame`). Not progress
frames: the page counts a progress frame as the user's activity and tells the
webserver, so a tab left on a folder that something keeps changing would keep
its session from ever idling out.

| Frame | Means |
|-------|-------|
| `{"type":"watching"}` | the watch is on; sent once, first. A refused watch sends none |
| `{"type":"changed"}` | the folder's entries changed; it carries no names, the page reads the listing again |
| `{"type":"gone"}` | the folder itself was deleted or moved; an `ok` result and the close follow |

Changes are paced so a busy folder cannot flood the page (`changePacer`). A
change that comes when at least twice the gap has passed since the last frame
is sent at once and sets the gap back to 1 second, so an occasional change,
one every 9 seconds say, always shows at once. Any other change is sent at
once if the gap has passed, else when it ends, and every frame sent so doubles
the gap, up to 10 seconds: a folder that changes steadily, even once every few
seconds (a spool or camera folder), settles at one frame in 10 seconds. A
change that comes inside a gap is never lost: one frame is sent when the gap
ends, and no frame is sent without a change. Writes are synchronous, with no
queue between the watch and the connection.

### The cap

A bridge process holds at most 32 watch Channels (`maxFolderWatches`). One
more is refused with `409` (`ErrConflict`), and that page does not update
live. Each tab showing a folder holds one; a bridge serves one session. An
inotify limit met when the watch is put on is the same refusal, with a message
naming it (`folderWatchError`): `ENOSPC` is the user's
`fs.inotify.max_user_watches`, `EMFILE` the instance or open file limit,
`ENFILE` the system's open files. Asking again would not change it, and each
attempt would read the listing and log an error.

## The page

On `changed` the hook marks that folder's listing stale
(`markListingStale` in `-hooks/useListingInvalidation.ts`) and React Query
reads it again. Only the listing: the listing's own query marks the folder's
sizes stale when its answer arrives (`fileBrowserListingQueryOptions.ts`),
and the bridge asks the indexer to refresh the folder on every listing of a
session with sudo ([Filesystem Indexer](./indexer.md#refresh-when-a-folder-is-opened)).
While the tab is hidden the listing is marked stale without a fetch, and the
hook asks for it when the tab shows again; the app's queries do not refetch on
focus by themselves. A read of the listing that is already running is not
restarted (`cancelRefetch: false`), so a folder whose listing takes longer
than the time between frames still updates; since that read may have started
before the change, the listing is read once more when it settles.

On `gone` it marks the listing stale. The read fails, since the folder is
not there: the app's query error handler (`api/query-client.ts`) shows an
error toast, and the page keeps the last listing it had, because the
listing's suspense query (`useFileQueries.ts`) throws only while it has no
data. The Channel's end opens the watch again after the first pause: a
`rm -rf build && mkdir build`, a folder swapped for another or a filesystem
unmounted from a mount point that stays all have the path back a moment
later. While the answer is `404` the hook asks again through the pauses,
1 s, 2 s, 5 s and 30 s, so a build that makes the folder again within about
40 seconds is watched again, and the `404` after the 30-second pause ends it.
A refused watch reads nothing, so a folder that is really gone gives one
failed read and one toast.

When the Channel ends for any other reason while the folder still shows (the
bridge restarted, the stream was reset, the websocket dropped), the hook opens
it again after a pause that grows: 1 s, 2 s, 5 s, then every 30 s; a watch
that had run for 30 seconds starts the pauses over. Once the new watch says
`watching`, the hook marks the listing stale once, since changes in between
were not seen; a first watch, opened right after the listing was read, does
not. While the websocket is down it waits for it, and reopens as soon as it
is back. Only a refusal that will not change by asking again stops it: an
error result with `400` (not a folder), `403`, `404` (with no `gone` before
it) or `409` (the cap or an inotify limit). Going to another folder and back
tries again. Any other error (a bridge that is restarting) is retried after
the same pauses.

The folder view watches only while it shows a folder listing: not a file,
not the Trash, and not in pickers or dialogs. The watch has no visible UI.

## Why one watch per shown folder, in the session's bridge

The index has no watcher, and a filesystem watcher for the index was turned
down ([backlog](./TODO/backlog.md), Rejected): it needs a root process that
always runs, and fanotify with `CAP_SYS_ADMIN` on whole filesystems. This is
not that and must not become it. It is one inotify watch on the one folder a
page shows, for as long as it shows it, held by that session's own bridge:

- The bridge of a session without sudo runs as that user, so the watch is a
  read with the user's own permissions, like the listing: the route is not
  `Privileged()` ([Privilege Pattern](./privilege_pattern.md)). Nothing new
  runs as root, and no socket is added.
- It ends with the page or the session, so nothing runs when nobody is
  looking, and the cost is one inotify instance per bridge and one watch per shown
  folder.
- The indexer is told nothing by the watch. Each listing the watch causes is
  an ordinary listing, which asks the indexer to refresh that folder; the
  daemon does that at most once in 30 seconds.

## Known limits

- A folder deleted while it shows keeps its last listing on screen, with an
  error toast, until the user navigates.
- A change made between the listing being read and the watch being put on
  (the route's loader reads the listing first, and the page opens the
  Channel after its first render) is not reported until the next change or
  the next read of the listing.
- A change made on the server side of a network mount (NFS, SMB, many FUSE
  filesystems) is not reported by the kernel; the folder updates when the
  page reads it for another reason.
- A file being appended to shows its new size when the writer closes it, not
  while it writes.
- A bridge holds at most 32 watches; a page past that does not update live.
- A hidden tab catches up when it is looked at, not before.
- Path pickers, dialogs and the Trash do not update live.
- A folder that changes steadily updates once in 10 seconds, not at every
  change.
- An inotify limit reached on the host stops the live update of that page
  until the user navigates.
- A folder above the shown one renamed or moved is not reported: the shown
  folder itself was not deleted or moved, and the watch stays on it.

## Tests

- `backend/bridge/handlers/filebrowser/folder_watch_test.go`: real inotify on
  temporary folders, the events that count and those that do not, the pacing,
  the folder gone, `watching` first, the inotify limits as refusals, data
  frames rather than progress, the one shared instance
  (two tabs on a folder, folders apart, a Channel that reads nothing, an
  overflow, a failed read and a watch added after it, the instance closed
  with the last watch and made again), the refusals the listing shares, the
  cap and the route's binding.
- `useFolderWatch.test.tsx`, `useListingInvalidation.test.ts` and
  `FileBrowserPage.test.tsx`: the invalidation, a read not restarted and read
  once more, the hidden tab, `gone` and the reopens through `404`s after it,
  the catch-up on `watching` only, the reopen pauses, which refusals stop it,
  a folder change, a file shown and the Trash,
  with a fake Channel. `StreamMultiplexer.test.ts` pins that a data frame
  sends no activity.
- `frontend/src/test/vm/indexer.spec.ts`: a file made and one deleted in the
  VM appear and go in an open folder without a reload.
