# File Browser Drag and Drop

The file browser's items are dragged with the browser's own drag and drop, so
a drag crosses to any tab of the browser: another LinuxIO tab of the same
server, and also a LinuxIO tab of another server. The two kinds of drag carry
their data as JSON under a type of their own
(`frontend/src/routes/_authenticated/filebrowser/-dragTypes.ts`), read and
written only in `-internalDrag.ts`:

| Type | Dragged from | Carries | A drop |
|------|--------------|---------|--------|
| `application/x-linuxio-paths` | a folder's rows | `{ origin, token, paths }` | moves the items (copies them with Ctrl) into a folder, or moves them to the Trash |
| `application/x-linuxio-trash` | the Trash's rows | `{ origin, token, items: [{ path, name }] }` | restores the items into a folder ([Trash](./trash.md#dragging-out-and-in)) |

Files dragged in from the desktop carry the browser's `Files` type and are
uploaded where the folder shown takes them; neither of the types above is
ever taken for an upload. A trail segment and the Trash's list take no
desktop files, and keep the browser from opening one dropped there in
LinuxIO's place (`refuseFileDrop`); over a folder's page the drop still
reaches the folder's upload, as before.

## Drop sites

| Where | Takes | Does |
|-------|-------|------|
| A folder's row (`DirectoryListing.tsx`) | both | moves or copies into the folder; restores into it |
| The folder a listing shows (`useFileDragAndDrop.ts`) | Trash items | restores into the folder; a folder drag there does nothing |
| A segment of the trail (`Breadcrumbs.tsx`) | both | moves or copies into the folder; restores into it |
| The trail's "Trash" segment (`Breadcrumbs.tsx`) | folder items | moves them to the Trash |
| The Trash's list (`TrashPlace.tsx`) | folder items | moves them to the Trash |
| The Trash button of a folder's header (`FileBrowserHeaderActions.tsx`) | folder items | moves them to the Trash |

While a drag is over a target only its type can be read, so a target that
takes the type shows itself as one; what the drag carries is read on the drop.

## A drag from another server or another site

A drop acts on the paths its drag names, on the server of the page it is
dropped on, and as root on a session with sudo. So a drop is taken only from
a drag this server's own page started, and every drop site reads a drag
through `acceptInternalDrop` or `acceptTrashDrop`, which share one check
(`acceptDragFrom`) and refuse a drag before anything is sent:

| The drag | Told by | The drop |
|----------|---------|----------|
| Started by a page of this server, in this tab or another | `origin` is this page's, `token` is the running drag's | is taken |
| From a tab of another server's LinuxIO | `origin` is another | "Cannot drop 2 items here", naming that server |
| Written by another site, claiming this server's origin | no `token`, or one that is not the running drag's | "…were not dragged from this LinuxIO", with a reload for an old tab |
| From a tab of this server opened before this rule | no `token` | the same, which a reload of that tab mends |
| From another tab of this server while the browser denies LinuxIO its storage | the `token` is not this tab's own | refused, saying a drag between tabs needs the storage; a drag inside one tab still works |

`origin` alone stops honest mix-ups only. Any page can write a drag of these
types with any content, `origin` and paths such as `/etc/pam.d` included, and
an admin lured into dragging from it onto the Trash button or a trail segment
would have the bridge, as root, trash or move those paths. The token is what
such a page cannot write: each drag of this page draws 16 random bytes
(`crypto.getRandomValues`), keeps them in the tab and in `localStorage`,
which the browser shares among the tabs of this origin and with no other
site, and puts them in the payload; a drop is taken only when the payload's
token is the one kept. The token goes when the drag ends at its source, and
the next drag replaces one left over.

## Known limits

- The server a drag comes from is told by its page's origin, so the same
  server opened under two addresses (its name and its IP address) counts as
  two: a drag between those tabs is refused.
- The tab a refused drag came from treats it as dropped: items dragged out of
  the Trash leave its selection there, as after a restore.
- Firefox fires no `dragend` once the dragged row has left the virtual window,
  so there items dragged out of the Trash stay selected after the drop.
