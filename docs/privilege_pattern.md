# Privilege Pattern

Privilege is route metadata. Handlers should not perform the normal route privilege gate themselves.

## Rule

Declare privileged operations in the relevant `backend/bridge/handlers/<domain>/handlers.go` binding entry:

```go
var api = apischema.Bindings(
    apischema.Call[apischema.NoRequest, apischema.NoResponse](
        "monitoring.restart",
        apischema.Privileged(),
    ).HandleVoid(handleRestart),
)
```

`apischema` applies that metadata when the route is registered. The dispatcher checks `req.Session.Privileged` before the handler or runner starts. Forbidden starts are typed dispatcher errors and are logged centrally.

## What Belongs In Handlers

Handlers may still validate operation-specific policy:

- whether a requested resource exists
- whether a request field is allowed
- whether a system capability is available
- whether a user-visible operation should be rejected for domain reasons

Handlers should not duplicate the route-level admin check.

## Choosing Privileged Routes

The rule: a route whose handler needs root must be declared `Privileged()`. The bridge runs as the logged-in user and stays root only when sudoers permits it, so a route that writes under `/etc` or `/var/lib`, mounts or unmounts, issues block-device or filesystem ioctls, or talks to systemd, NetworkManager, Docker, libvirt, PackageKit or the account database only works in a privileged bridge. Without the guard a non-sudo session sees the route, starts it, and gets a permission error from the host part-way through instead of a 403 before anything ran. It is not a privilege escalation, but it is a bug: declare the guard with the route, not inside the handler.

Docker is a fixed case. Access to the Docker socket is root-equivalent, so the deployment keeps the `docker` group empty and only a sudo-authorized bridge or the root job host may reach it. Every Docker write declares `Privileged()`: mutations, task routes, and anything else that changes containers, images, networks, volumes, compose stacks or daemon settings. Do not make a Docker write unprivileged on the grounds that members of the `docker` group could run it. Docker reads are not covered by this rule.

Read-only system inventory can usually remain unprivileged unless it exposes sensitive information.

Routes that change the host through polkit-gated D-Bus calls (systemd, NetworkManager, hostname, timedated, login1) are privileged like the rest. polkit might let a user without sudo run some of them, but the answer would differ from host to host; LinuxIO gives one answer, a 403 before anything runs.

### What is enforced

The dispatcher enforces a declared guard, and `TestChangingRoutesArePrivilegedOrListed` in `backend/bridge/apischema/schema_test.go` enforces that the guard is declared. It walks every route: each `Mutation`, each duplex stream and each `Call` that is not `RetrySafe()` must be `Privileged()` or be named in `unprivilegedChanges`, the list in that file, with the reason it needs no root. A stream counts because it can change the host as much as a mutation can: `container.open` starts a process inside a container. The test also fails on a listed route that became privileged or no longer exists, so the list stays true.

A new route that changes the host therefore fails the test until it declares the guard or is added to the list. What is on the list today:

- the file browser's routes, which act on the session user's own files as that user;
- the Assistant routes, which run the session user's own `claude` CLI and write only under that user's home ([Assistant](./AI-assistant.md#plugins));
- `terminal.open`, the session user's own shell;
- `tasks.cancel`, `tasks.watch`, `tasks.data` and `tasks.events`, the caller's own jobs;
- `config.set`, `config.set_ui` and the two `system.dismiss_*` routes, which write the session user's own settings (`config.set` refuses its one root-only Docker field in the handler);
- reads that are not `RetrySafe()`: the Docker update checks and icons, the log and download streams, `network.get_network_info` and `system.get_health_summary`.

### Root acting in the session user's folders

For an admin the file browser's changes run as root, in the root job host or, for uploads, in the root bridge, on folders a process of the session user can rearrange while the change runs. A process that swaps one of that user's folders for a symlink would otherwise point root at any folder on the system. So creating a file or a folder, delete, trash and restore, copy, move and rename, chmod and chown, compress and extract, and every upload and editor save, with or without `owner`, reach the folder they act in one name at a time from `/` and follow a symlink only where nobody but root can have planted it: in a folder root owns that no group or everyone may write, such as `/home -> /var/home` or `/bin -> usr/bin`. A link in any other folder is refused with 403. That covers the session user's own folders, and equally another local user's or a service account's (`/home/v`, a `/var/www` that belongs to `www-data`): the owner of a folder can put a link in it, and an admin who compresses, uploads or restores there would otherwise be sent wherever that account chose. It includes a link made on purpose: an admin whose `~/Documents` is a link to another disk cannot change files through that path as root, and uses the real path instead. The refusal names it, `/home/a/Documents is a link to /mnt/data/Documents. Open that folder to make this change`, which tells the admin nothing `readlink` would not; the page shows it in the operation's error toast, or per item in a batch's failure list, with an "Open the real folder" action that opens what the link points to (`frontend/src/utils/linkRefusal.ts` finds the path by the refusal's wording, and a test there fails when the wording changes). There is no "follow it anyway": the link can change between the question and the change, so the way through is to work in the real folder, where the admin sees what they act on. A save without `owner` writes through a link at the file itself, as an editor does, under the same rule. Below that folder everything is reached through open handles without following a symlink (`openat` with `O_NOFOLLOW`, `renameat2`, `linkat`, `fchownat`, and a chmod or a removal through the handle of the entry or its folder), so a folder swapped for a link mid-operation fails the operation or is never entered. chmod follows a link at the item itself, as `chmod(1)` does, under the same rule. A new file is made with `O_EXCL`, so a create never truncates what is already at its name. An upload writes into a buffer made that way beside its target and reopens it for each chunk without following a link and only while it is still the same file, compared by kernel file handle (`name_to_handle_at`), which carries the inode's generation: ext4 gives a deleted file's inode number to the next file created, so device and inode alone would accept a buffer that a process able to write the folder deleted and recreated as its own. A filesystem that cannot encode handles is compared on device and inode only (`uploadBufferID`). An upload that replaces a file reads the owner and mode it keeps, and removes the buffer of a cancelled upload, through the handle of the folder it commits in, so a folder swapped for a link lends it no other file's owner or setuid bit. A bridge that is not root acts as the user and follows the user's links as any program of theirs would, and a root session has no rule applied, since its user is no less trusted than the bridge (`services.openDirNoUserLinks`, `services.OpenNearestDirNoUserLinks`, `plantable`, `actingFor`).

A compress reads its sources the same way (`services.ArchiveSources`): a source named through a link the user could have planted is refused, and below a source folder everything is read through handles. A link in a folder that is not root's alone is left out of the archive, which root creates readable, and listed in the result's `failed`, so the page reports it as the compress finishing with failures; any other link is read as the archive code always read links, a link to a file adds the file and a link to a folder is left out. A FIFO, socket or device is left out. The size estimate behind the progress bar reaches and walks the sources the same way, so a planted link to `/` is not measured; a link is not counted. A download of several items as one archive (`filebrowser.archive`) goes to the admin's browser, not into the user's folders, and still reads as the user's own programs would.

The trash and its `.trashinfo` are the user's to edit, so `Path=` cannot say where root puts an item back. When a root process trashes an item for the session user it also records where the item came from in `/var/lib/linuxio/trash/<uid>/<dev>-<ino>.json`, a folder only root can write, one file per item named by the trashed item's device and inode (which a rename inside the trash keeps), written atomically, at most 10000 per user (past that stale records are dropped, and failing that the item is trashed without one). A restore on a root bridge uses the record and ignores `Path=`; the record goes when the item is restored or purged, and every purge drops the records of items no longer in the trash, as after another program emptied it. A purge leaves a record less than a minute old alone, since a record is written just before its item moves into the trash; it removes what an interrupted write left in the folder; and it keeps the record of a trash that is missing because its disk is not mounted, until the user reaches the limit. An item without a record, trashed by a desktop file manager or by a bridge that was not root, goes into a folder the session user does not own only when the restore request names that destination for that item in `confirmedDestinations`, a map from the trashed item's path to its destination, so a destination confirmed for one item does not admit another whose `Path=` was rewritten to it: the Trash view gets `confirmRestore` for such an item and asks "Restore to <path>?" before it sends the request, and the server refuses the restore without it. The other protections stay: only an item the user could not have written goes into such a folder, staged under a hidden name first. A bridge that is not root reads no records, as it cannot, and restores as before.

Root cannot sign in to LinuxIO: `linuxio-auth` refuses uid 0 after PAM has resolved the account (`backend/auth/linuxio-auth.c`, "root login is not allowed"), and `packaging/etc/linuxio/disallowed-users` lists root for the PAM stack as well. The root-session case of `actingFor` is a guard, and the trash list and purge, which decide the rule from the process uid alone, agree with it.

### In the frontend

The server's 403 is the gate; the page only avoids offering what will be refused. A control whose route is privileged is hidden for a session without sudo, read from `useAuth().privileged` or a route's `requiresPrivileged` access policy, and a privileged read that a page mounted for such a session would make is not issued. What is gated today:

- the power menu, the dashboard's hostname and clock edits, and the unit actions on the Services page;
- the Accounts page: adding users and groups, editing, password, lock and unlock, group members and delete, and ending a session; the lists and each user's details and activity stay;
- the network interface card, which shows the same settings (connection, IPv4 method, addresses, gateway, DNS, search domains, MTU, optional at boot) as text with no control and no save;
- the Docker pages, the Docker components and the dashboard's Docker card: every container, image, network, volume and compose stack change, Start all, Stop all and Prune, the container terminal and updates; the lists, details, inspect views, logs and update checks stay, and a stack's compose file opens read-only. A user in the `docker` group reaches these pages without sudo, since the capability probe pings the Docker socket as the bridge's user;
- monitoring history (`monitoring.get_*_history`): the live charts on the dashboard and the network page skip their backfill and fill from live samples, and the history cards on the Hardware, Network and Docker pages say the history needs a privileged session (`useHistoryAccess`, `useLiveSeries`);
- the Settings tabs whose reads are all privileged (Indexer, Monitoring with its Logging card, Notifications and Power: `indexer.get_config`, `indexer.get_status`, `monitoring.get_config`, `monitoring.get_status`, `logging.get_debug`, `alerts.get_notifications`, `power.get_status`): the tab is hidden and a `?tab=` link to it falls back to General, so none of those reads is issued;
- a run's saved output (`tasks.output`): the History page shows no output section, and the Schedules tab, whose `schedules.list` is privileged too, is hidden. The dashboard's failed-login alert and the dialog that reads `system.list_failed_login_events` only exist for a privileged session, because `system.get_health_summary` leaves the alert out for any other;
- the file browser's search, folder sizes and Index filesystem (`filebrowser.search`, `filebrowser.subfolders`, `filebrowser.dir_size`, `filebrowser.index`): the search field and the index action are hidden and sizes show as unknown.

Search stays admin-only by decision: the index covers the whole host and only root can reach it, so a session without sudo would need a root component that filters every result by what the user may list.

## Testing

Dispatcher tests should cover:

- privileged route with unprivileged session returns forbidden
- privileged route with privileged session runs
- forbidden starts do not create Tasks
- forbidden starts are typed errors

See [API Contract](./api-contract.md) for route declaration and registration details.
