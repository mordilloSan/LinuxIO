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

The three changes are jobs that report a count of items and answer with
`FileBatchResult`: one failed item does not stop the others, and each failure
in `failed` carries the item's path, a message, and for the two cases below a
`reason` the page can act on without reading the message. None of the routes
declares `Privileged()`: the trash is the user's own, and a session without
sudo runs them as the user.

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

The list has the columns Name (the shown name, see [The list](#the-list)),
Original location (the folder of `originalPath`, "Unknown" without one),
Date deleted ("Unknown" without one) and Size (a file's; a folder's is not
listed). Folders and files are one list, sorted by any column, again by the
same column the other way, newest deleted first when the Trash opens.
Selection works as in a folder: click, Ctrl and Shift click, a marquee,
Ctrl+A, the arrow keys, typing a name's first letters, Escape. A trashed
item is not opened: a double click or Enter shows its Properties, never the
inside of a trashed folder or an editor. Properties of one item show its
name, original location, date deleted, size and kind (folder or file); of
several, how many, how many of them are files and folders, and the total
size of the files.

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
cut, compress, permissions, search, hidden files, the card view) are not in
the Trash. Each action is a job as in [Jobs](./jobs.md#frontend): its dialog
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
The questions of several restores are asked one after another. Restores and
their prompt belong to the file browser page, not to the Trash or a folder,
so a restore that ends after the user went from the Trash to a folder still
asks; one that ends after the user left the file browser reports its taken
names as failures, and those items stay in the Trash.

### Dragging out and in

A row of the Trash is dragged as a restore, under its own drag type
(`application/x-linuxio-trash`) that carries each item's trashed path and
shown name, so no drop target mistakes it for a move. Dropped on a folder's
row, on the folder a listing shows, or on a segment of the trail, in this
tab (the Trash's own trail) or in the file browser of another LinuxIO tab,
the items are restored into that folder with `destination` and `names`,
never moved with `filebrowser.move_batch`, which would leave the
`.trashinfo` behind and keep the name the item has in the trash. The items
leave the selection once dropped. The other way, items dragged from a
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
  the segments of the trail; any folder of another tab takes it.
- A move to Trash that fails for other items as well as moving some reports
  the failures and not the items that moved, as a partial delete does.
