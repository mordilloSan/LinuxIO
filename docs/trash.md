# Trash

The file browser's Delete moves items to the session user's trash, the
FreeDesktop.org trash a desktop file manager uses, so an item LinuxIO trashed
shows in the desktop's trash and the other way round. From the trash an item
goes back where it came from, into a folder the user picks, or away for good.
It works like the Windows Recycle Bin.

The trash belongs to the session user. On a session with sudo the work runs
as root acting for that user, so it follows the rules in
[Privilege Pattern](./privilege_pattern.md#root-acting-in-the-session-users-folders):
folders are reached without following a link anyone but root could have
planted, and everything below them is reached through open handles.

## Routes

| Route | Request | What it does |
|-------|---------|--------------|
| `filebrowser.trash_batch` | `BatchPathRequest` | Moves each path to the trash. |
| `filebrowser.trash_restore` | `TrashRestoreRequest` | Puts each trashed item back, or into `destination`. |
| `filebrowser.trash_purge` | `BatchPathRequest` | Deletes each trashed item for good. |
| `filebrowser.trash_list` | none | Lists everything in the user's trashes, newest first. |
| `filebrowser.trash_size` | `PathRequest` | Sizes one trashed folder: its files' total size and how many files and folders it holds (`DirectorySizeData`). |
| `filebrowser.trash_usage` | none | What the user's trashes take on disk, as the index holds it (`TrashUsageResponse`). Privileged. |

The three changes are jobs that report a count of items and answer with
`FileBatchResult`: one failed item does not stop the others, and each failure
in `failed` carries the item's path, a message, and for the two cases below a
`reason` the page can act on without reading the message. Of the routes only
`filebrowser.trash_usage` declares `Privileged()`, since it reads the index,
which only root reaches (see [Disk space](#disk-space)): the trash is the
user's own, and a session without sudo runs the others as the user.

| `reason` | Route | Meaning | What the page can offer |
|----------|-------|---------|-------------------------|
| `exists` | `trash_restore` | Something is at the name the item goes back under, and the request chose neither `overwrite` nor `keepBoth` for it. | Ask, then send those items again with the choices. |
| `no_trash` | `trash_batch` | The item's filesystem cannot hold a trash for this user. | Delete it for good instead. |

Any other failure has no `reason`.

## Where an item goes

An item on the same filesystem as the user's home goes to the home trash,
`~/.local/share/Trash`. An item on another filesystem goes to that
filesystem's own trash in its top folder: `.Trash/<uid>` when the top folder
has a shared `.Trash` with the sticky bit, otherwise `.Trash-<uid>`, made for
the user as the spec has it. The item is renamed into `files/` and a
`.trashinfo` beside it in `info/` records where it came from (`Path=`,
relative to the top folder for a per-filesystem trash) and when
(`DeletionDate=`). A name already in the trash gets `.2`, `.3` and so on.
Nothing is ever copied between filesystems: a move to the trash is a rename.
An item the page names through a link, `~/data/report` where `~/data` leads
to `/mnt/data`, is taken where the link leads: it goes to the trash of the
filesystem `/mnt/data` is on, and its `.trashinfo` says it came from
`/mnt/data/report`, since the folders of a path are resolved where it enters
(see [Privilege Pattern](./privilege_pattern.md#paths-through-links)).

On a root bridge the move is also recorded where only root can write, since
the trash and its `.trashinfo` are the user's to edit; see
[Privilege Pattern](./privilege_pattern.md#root-acting-in-the-session-users-folders)
and [Configuration and Storage Layout](./configuration-storage-layout.md).

### A filesystem without a trash

When the item's filesystem cannot hold a trash for the user, the item stays
where it is and fails with reason `no_trash`. That is the case when making or
opening the trash is refused for lack of permission (a top folder the user
may not write, such as a data disk mounted root-owned on a session without
sudo), when something that is not a folder is where a folder of the trash
goes, or when the kernel refuses the rename into it across filesystems. The
page can then offer to delete the item for good, which is a request of its
own; the server never deletes or copies in its place. A process of the user
can make their own trash unusable that way (a file or a link where a folder
of the trash goes), so the page asks before it deletes anything for good.

Every other failure on the way to the trash keeps its own message and has no
reason: a refusal under the link and ownership rules (403: a link the user
could have planted, a trash folder another user owns), a read-only
filesystem, where nothing can be deleted for good either, and a failure a
retry may get past (a full disk, an I/O error). A protected folder (`/etc`, a
mount point) and a missing item fail for their own reasons.

## The list

`filebrowser.trash_list` lists the home trash and the trash of every mounted
filesystem that has one for the user, so items trashed by other programs show
too. Each `TrashEntry` has:

- `path`, where the item sits in a trash's `files/`, which restore and purge
  take;
- `name`, its name in the trash;
- `originalPath`, where it came from: on a root bridge the record of the move
  when there is one, otherwise `Path=`; empty when neither is known;
- `deletedAt` (RFC 3339, empty when unknown), `isDir` and `size` (a file's
  size, 0 for a folder);
- `confirmRestore`, set when going back to `originalPath` needs the user's
  confirmation (see below).

The page shows an item under the base name of `originalPath`, or `name` when
that is empty: a restore puts it back under that name, and a restore into a
chosen folder sends it in `names`. The original location is the folder of
`originalPath`. Sorting is the page's.

The list gives no size for a folder, since it would have to walk every
trashed folder. The page fills the sizes in from the index instead (see
[Disk space](#disk-space)). `filebrowser.trash_size` sizes one by walking it
when the page asks, which it does when the folder's properties open. It takes only an item directly in
the `files/` folder of one of the session user's own trashes, the ones the
list reads, so it measures nothing the list would not show. What it answers:

| Case | Status |
|------|--------|
| A path that is not an item of the user's trashes | 400 |
| An item that is not a folder (a link included) | 400 |
| A missing item | 404 |
| A folder of the trash another user owns | 403 |
| A link where a folder of the trash goes, on a root bridge | 403, with the error purge and restore give for it |
| The item itself cannot be opened or listed | its error (403 when permission is refused), never a size of 0 |

It reaches the item as a purge does, from the trash's own folders without
following a link the user could have planted there on a root bridge, and
walks it through handles without following a symlink: a link inside the
folder counts as a file and is never entered, so nothing outside the item is
measured. A folder of another mount below it (a mount point, a bind mount)
counts as a folder and is not entered. That goes by the mount, read with
`statx` `STATX_MNT_ID` (Linux 5.8 and later, under LinuxIO's 5.9 floor;
without it the size is refused rather than walked), where `du -x` goes by
the device number: every btrfs subvolume has a device number of its own, and
a subvolume nested in the folder is part of it. On a session with sudo the
walk is root's, so it also counts what the user could not read inside the
item, as sizes and counts only, never names. A folder below the item that
cannot be opened or listed counts as a folder and adds nothing.

The size is the size on disk, counted as the indexer counts a folder for the
folder listing: the blocks every entry takes, every folder's own blocks
included, the trashed folder's as well, so a sparse file counts what it holds
and a file with several links in the folder counts once. Every entry that is
not a folder counts as a file, each link included. The walk stops when the
request is cancelled. Like the list it is a read of the user's own trash
and declares no `Privileged()`; a session without sudo runs it as the user.

## Disk space

A move to the trash is a rename on the item's own filesystem, so it frees
nothing: the item takes the same space until it is deleted for good. LinuxIO
says so where the move is made and shows what the trash holds wherever the
Trash is reached, so that a full disk is not "emptied" into its own trash
without anyone seeing it.

The sizes come from the index, as a folder's do in a listing
([Indexer](./indexer.md#sizes-that-are-still-catching-up)), and nothing walks
a trash for them:

- **A trashed folder's size.** The index keeps the size of every folder, so
  the sizes of the subfolders of a trash's `files/` folder are the sizes of
  every folder trashed there. The page asks for them with
  `filebrowser.subfolders`, once per trash that holds a folder, and fills
  them into the list (`useTrashFolderSizes`, `withFolderSizes`).
- **What the trash takes.** `filebrowser.trash_usage` adds up, for every
  trash of the user that holds an item, the size the index has for its
  `files/` folder, read from the subfolders of the trash, a read of rows the
  index keeps sizes for, where the folder's own details would count every
  entry below it. It answers `size`, `pending` as a folder's size does, and
  `partial` when the index holds nothing for a trash in use (an excluded
  path, a network mount, a trash not indexed yet): `size` is then a lower
  bound and the page says "at least". A trash made since the last scan, as
  by the first move into it, has rows in the index for its items and none
  for its own folders ([Indexer](./indexer.md#known-limits)): the route then
  asks the indexer to refresh the trash, as it does for a folder someone
  opens, which reads a folder it has no row for whole, at most once in 30
  seconds, and asks for the size again, so the answer is `pending` while
  that read waits and the page asks once more. A trash with nothing in it is not asked
  about, so an emptied Trash takes nothing and not the few blocks of its
  empty folders. It reaches the trashes as the list does, so one a root
  bridge refuses (a link where a folder of the trash goes, another user's
  folder) is not measured, and asks the index about each by the real path of
  the handle it opened (`TrashFilesInUse`), never by a name looked up again.
  Every folder the file browser shows asks for it, so unlike the list it
  looks for trashes on local mounts only (`localMountPoints`): not on a
  network filesystem, which blocks whoever touches it while its server does
  not answer, and not on an automount point with nothing mounted on it, which
  looking at would mount. The index's own rule says which filesystems are
  network ones (`ExternalFilesystem` in `backend/indexer/api`).

Listing the Trash tells the indexer to refresh the `files/` folder of each
trash it read an item from (`refreshTrashesInIndexer`), as listing a folder
does, so an item another program trashed is indexed and gets a size; the list
does not wait for it. The page marks the sizes it holds stale when the list
arrives (`markTrashSizesStale`) and after every trash job, and asks again
while an answer says `pending`.

The index is root's and its socket takes no other user, so all of this is for
a session with sudo. Without it the page asks the index for nothing: a
trashed folder shows no size and the Trash no total, as a folder's listing
shows no sizes there, and a trashed folder's properties still walk it.

What the page shows:

- **In a folder**, beside the header's Trash button, what the Trash takes
  ("34 GB", "at least 34 GB"), and in the button's name ("Trash, 34 GB");
  on a phone, where the button sits in the actions menu, in its name alone.
  Nothing is shown for an empty Trash or while no size is known.
- **When items are deleted**, the question says it: "Move go-build to Trash?
  It stays on the disk (34 GB) until the Trash is emptied.", with the size
  when the page knows it for every item (a file's from the folder's listing,
  a folder's from the index: `knownDiskSize`, read as the delete is asked
  for) and without it otherwise, as while the index is still catching up
  with the folder, when the size it holds may be from before a copy. **Delete permanently instead** turns the
  same dialog into the question of a permanent delete, about the same items,
  so deleting for good still takes its own answer.
- **When the move ends**, the toast says what still takes space, "Moved 3
  items to Trash, 34 GB still used", when every item moved and their sizes
  were known as the job started, and **Open Trash** leads to the Trash from
  whatever page the toast is read on.
- **In the Trash**, beside its header's actions, how many items it holds and
  what they take ("12 items, 41 GB"), which is what Empty Trash frees.

`frontend/src/test/vm/trash-size.spec.ts` follows all of it on a real host,
with the daemon answering: `make test-vm-browser VMS=1
PLAYWRIGHT_ARGS=trash-size.spec.ts`.

## Restore

By default an item goes back to where it came from, and missing folders on
the way are made again, handed to the user inside a folder of theirs. The
`.trashinfo` and the record go once it is back.

On a root bridge the destination comes from the record of the move, not from
`Path=`, which the user could have rewritten. An item without a record,
trashed by another program, goes back into a folder the user does not own
only when the request confirms that destination for that item in
`confirmedDestinations` (the list sets `confirmRestore` so the page asks
first), and only an item the user could not have written goes into such a
folder at all, staged and checked in a folder only root can reach before it
gets its name. The reasons are in
[Privilege Pattern](./privilege_pattern.md#root-acting-in-the-session-users-folders).

### When the name is taken

If something is at the name an item goes back under, the item fails with
reason `exists` and stays in the trash. The page asks, as it does for a paste,
and sends those items again with the user's choices; an item the user skips
is left out of the request, and `overwrite` covers every other item of the
request, so it goes only in a request of items the user was asked about. The
choices are the ones `filebrowser.copy_batch` and `filebrowser.move_batch`
take:

- `keepBoth` lists, by trashed path, the items that go in beside what is
  there under the first free `name (copy).ext`, then `name (copy 2).ext`, as
  a paste names a copy;
- with `overwrite`, every other item replaces what is there, as a move with
  overwrite does: a file replaces the file, which is set aside until the
  item is in and then deleted for good; a folder merges into the folder
  there, entry by entry, its files replacing files of the same name; a file
  and a folder never replace each other, and that item fails.

The choice changes only the name or what is replaced, never which folders an
item may enter: an item refused back into a folder the user does not own is
refused before anything at its name is touched, and one admitted there is
staged and checked first whatever the choice. On a root bridge two more rules
hold for a replace back where the item came from, both refused with 403:

- an item without a record replaces only the destination the request
  confirms for it in `confirmedDestinations`, so the page sends the
  `originalPath` it asked about with every such item. Otherwise a process of
  the user could rewrite `Path=` between the question and the answer and
  choose what root replaces, say a folder root owns inside one of the user's
  folders;
- a folder does not merge into a folder the user does not own, nor into any
  folder inside one, since the check that the user could not have written
  the item covers its top only, not what a merge moves in below it, and a
  merge would put the user's entries where the user may not write. Keep
  both, or a restore into a chosen folder, still works there.

A replace works through the handle of the folder, so a link the user put at a
name inside it is replaced, never written through.

### Restore into a chosen folder

With `destination` set, every item of the request goes into that folder
instead of where it came from: dragging an item out of the Recycle Bin into a
folder. Each item goes in under the name `names` gives it by its trashed path,
which is the name the page shows it under, or else under its name in the
trash. The name is the request's and is never read from the trash, which the
user could edit after the page listed it. The folder must exist; it is
checked once for the whole request, as a move's destination is, and again for
each item when it is opened. A taken name gets the same choices.

Folder and name are the user's choice in this request, not a claim read from
the trash, so a root bridge treats it as a move the user asks for now, which
is what `filebrowser.move_batch` does when the user moves an item out of the
trash folder by hand: the folder is reached from `/` without following a link
anyone but root could have planted (a planted link is refused with 403, in
the same words as every other such refusal), and the item goes in by rename,
never over what is at its name unless the request said `overwrite`.
`confirmedDestinations` and the rule that only an item the user could not
have written enters a folder they do not own do not apply: they exist because
`Path=` and the record of an item the user could have swapped are not the
user's choice. For the same reason the page sends `destination` only for a
folder the user picked, never on its own to get round a refused restore.

A restore does not move an item to another filesystem: a folder on another
filesystem than the item's trash is refused and the item stays in the trash.
Inside a merge an entry that meets a mount point in the folder it merges into
is copied and removed, as a move does.

## Delete for good

`filebrowser.trash_purge` deletes each trashed item and its `.trashinfo`,
walking a folder through handles so that a link inside it is removed, never
followed. Each purge also drops the records of items no longer in the trash,
as after another program emptied it.

## The Trash view

The Trash is a place in the file browser, as the Recycle Bin is a window in
Windows: the header's Trash button opens it where the folder was, and the
browser's Back returns. Its URL is the folder's with `?trash=true`
(`/filebrowser/srv/projects?trash=true`), a search parameter because a path
segment would hide a real folder of that name; a reload stays in the Trash.
The folder it was opened from stays in the path: the trail shows that
folder's segments and then "Trash", so a segment leads back to the folders,
and Backspace or Alt+Up return to that folder. While the Trash shows, the
route lists no folder; the Trash reads `filebrowser.trash_list` itself and
shows its own loading rows, "Trash is empty." when there is nothing, and a
failed read with its error and a Retry button. A Refresh button reads it
again, as for items another program trashed meanwhile.

The items show as a folder's do, as cards or as a list, in the folder
listing's virtual rows (`VirtualItems` in `VirtualDirectoryItems.tsx`), so
only the rows in view are in the page however many items the Trash holds.
The header's view button switches between the two, and it is the folder
listing's own setting: the Trash opens in the view the folders show, a
switch in the Trash carries over to the folders, and the card size, set
with the slider beside the view button
([File Browser Card View](./filebrowser-card-view.md)), is shared with them
too. The items are one listbox of options, the heading outside it, and each
option says where it stands in the whole Trash (`aria-posinset`,
`aria-setsize`), so a screen reader hears the full count however few rows
are in the page. A card shows the shown name, the original location
("Unknown" without one), the size and the date deleted. The
list has the columns Name (the shown name, see [The list](#the-list)),
Original location (the folder of `originalPath`, "Unknown" without one),
Date deleted ("Unknown" without one) and Size (a file's, and a folder's when
the index holds one; see [Disk space](#disk-space)). A card shows a folder's
size the same way. Folders and files are one list under one heading, Items, sorted by
any column, again by the same column the other way, newest deleted first
when the Trash opens. The sort is in the URL beside `trash`, `sort`
(`name`, `originalPath`, `deletedAt` or `size`) and `order` (`asc` or
`desc`), each left out at its default (`deletedAt`, `desc`), so a reload or
a link opens the Trash in the same order; an unknown value falls back to the
default, and a folder's URL carries no sort. A new sort replaces the URL
instead of adding a step, so Back still leads to the folder. Selection works
as in a folder: click, Ctrl and Shift click, a marquee, Ctrl+A, the arrow
keys, typing a name's first letters, Escape. A trashed item is not opened: a
double click or Enter shows its Properties, never the inside of a trashed
folder or an editor. Properties of one item show its name, original
location, date deleted, size and kind (folder or file); a folder's size and
how many files and folders it holds are read from `filebrowser.trash_size`
as the properties open ("Calculating…" meanwhile, "Unavailable" with the
reason on hover when it fails), and read again by a trash job that ends. Of
several items Properties show how many, how many of them are files and
folders, and the total size of the files.

What can be done is the same in the header (where a folder's options sit),
the context menu and Properties:

- **Restore** sends the items back where they came from. An item with
  `confirmRestore` is named first, "Restore to <path>?", and its destination
  goes in `confirmedDestinations`.
- **Restore to…** picks a folder, starting at the one the Trash was opened
  from, and sends it as `destination` with each item's shown name in
  `names`. It confirms nothing: the folder is the user's choice.
- **Delete permanently**, also from Delete and Shift+Delete, asks first and
  purges the selected items. **Empty Trash** asks first and purges every item
  listed when it asked.

The folder's own actions (new file or folder, upload, paste, rename, copy,
cut, compress, permissions, search, hidden files) are not in the Trash. Each action is a job as in [Jobs](./jobs.md#frontend): its dialog
closes as it starts, its items leave the selection, any number run at once
with a row each in the task tray, each reports its own result with failures
named per item, and the list is read again when it ends, also after a
failure or a cancel.

### Restore when the name is taken

Items of a restore that fail with `exists` are asked about with the prompt a
paste asks with (Skip, Keep both, Overwrite or Merge, each for one item or
for all), naming the folder they go back to, and are sent again in one
`trash_restore`: skipped items are left out, keep-both items go in
`keepBoth`, `overwrite` is set when any item is to be replaced, and the
first request's `destination`, `names` and `confirmedDestinations` go along
for the items sent. Back where it came from, each replaced item also gets the
destination the user was just asked about in `confirmedDestinations`, which
an item LinuxIO has no record of trashing needs for a replace even in a
folder of the user's own. Two trashed items that go back under one name are
asked about once; with replace only the first is sent and the others stay in
the Trash, since the second would go over the first, which would then be
gone for good. Only that second request carries `overwrite`, and a name
taken again by then fails it like any other failure; nothing is asked twice.
An item whose destination the page does not know is reported as a failure
instead. Restores and their prompt belong to the file browser page, not to
the Trash or a folder, and the questions wait outside the page
(`frontend/src/routes/_authenticated/filebrowser/-hooks/takenNamesPrompts.ts`),
one per restore in the order the restores ended, as the questions about a
disk without a trash do. They are asked one after another anywhere in the
file browser: in a folder, in the Trash, or when the user comes back to the
file browser after leaving it, and a question whose page closed before it
was answered is asked again there. Until it is answered its items stay in
the Trash. A restore that ends with such a question while no file browser
shows says so, "2 items wait in the Trash", with the file browser asking
what to do with them when it shows, and the toast's link to it.

### Dragging out and in

A row of the Trash is dragged as a restore, under its own drag type
(`application/x-linuxio-trash`) that carries each item's trashed path and
shown name, so no drop target mistakes it for a move. Dropped on a folder's
row, on the folder a listing shows, or on a segment of the trail, in this
tab (the Trash's own trail) or in the file browser of another LinuxIO tab,
the items are restored into that folder with `destination` and `names`,
never moved with `filebrowser.move_batch`, which would leave the
`.trashinfo` behind and keep the name the item has in the trash. The items
leave the selection once dropped. The drag also carries the origin of the
page it started on, which says whose Trash holds the items, and the drag's
token, which only this server's own pages know: dropped in a tab of another
server it restores nothing there, where the server's trash does not hold them
(or holds another item at the same path), and says "Cannot drop 2 items
here", naming the server they come from; a drag another site forged with this
server's origin is refused as well, and a drag from a folder the same way
([File Browser Drag and Drop](./filebrowser-drag-and-drop.md#a-drag-from-another-server-or-another-site)).
The other way, items dragged from a
folder onto the Trash's list, onto its "Trash" segment, or onto the Trash
button of a folder's header move to the Trash, without the question Delete
asks: the move is the user's gesture, and the Trash gives it back.

### A disk without a trash

A move to Trash that ends with `no_trash` items asks once for that job:
"report.pdf in /mnt/usb cannot go to the Trash. Delete it permanently?", or
"3 items in /mnt/usb …" for several (the folder is named when they share
one). Delete starts a permanent delete of exactly those paths; Cancel leaves
them where they are. They are not in the job's failure toast; the job's
other failures are, and the items that moved get the success toast when
nothing else failed. The question may come after the user has moved on, so
it names the items itself and does not depend on the folder shown or the
selection. The questions wait outside the page
(`frontend/src/routes/_authenticated/filebrowser/-hooks/noTrashPrompts.ts`),
one per job in the order the jobs ended, and are asked anywhere in the file
browser: in another folder, in the Trash, or when the user comes back to the
file browser after leaving it. Each opens only after the one before has
faded out, so a quick second click never answers a question not read.

## Known limits

Open work on the Trash is in the [backlog](./TODO/backlog.md).

- A name taken between the check and the rename fails the item with the
  rename's own message and no `exists` reason.
- A replace that merges a folder and stops part-way (a file where a folder
  is, a special file it cannot move) leaves what it moved in place and the
  rest in the trash, as a move does; the search index learns of the part that
  moved at its next full index.
- The header's Trash button takes dropped items on a wide screen only; on a
  phone it sits in the actions menu.
- In the tab that shows the Trash the only folders to drag an item onto are
  the segments of the trail; any folder of another tab of the same server
  takes it.
- An item dragged out of the Trash and dropped on the desktop or on another
  program does nothing: the drag carries the item's place in the trash, not
  its content, which would have to be downloaded first.
- The server a drag comes from is told by its page's origin, so the same
  server opened under two addresses (its name and its IP address) counts as
  two: a drag between those tabs is refused.
- A move to Trash that fails for other items as well as moving some reports
  the failures and not the items that moved, as a partial delete does.
- The selection is not in the URL: a reload starts with nothing selected.
- A question about taken names waits in the browser tab that ran the
  restore: a reload or a sign-out drops it, and its items stay in the Trash.
- A trashed folder whose walk takes longer than a request may wait (30
  seconds) shows "Unavailable" with "Request timeout"; it is walked once, not
  again, until its properties open again.
- A session without sudo sees no size for a trashed folder in the list and
  no total for the Trash, since both come from the index; a folder's
  properties measure it there.
- The Trash's total and a trashed folder's size in the list are the index's:
  they lag a change made outside LinuxIO until the Trash is next opened, and
  say nothing for a trash on a path the index leaves out, where the total
  reads "at least".
- A trash on a network mount is listed in the Trash and not counted in the
  Trash's total, also where the index is set to include network mounts; the
  total does not say so.
- The size in the delete question and its toast is the size the page held
  when the delete was asked for, a file's as listed and a folder's as
  indexed; it is left out when one of the items has none.
- A folder inside a trashed folder that cannot be opened or read (on a
  session without sudo, one the user may not read) adds nothing to its size,
  and the size does not say it is partial.
- Sizing a trashed tree of millions of hard-linked files keeps one entry per
  such file in memory for the length of the walk.
