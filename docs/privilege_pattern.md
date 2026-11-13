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

For an admin the file browser's changes run as root, in the root job host or, for uploads, in the root bridge, on folders a process of the session user can rearrange while the change runs. A process that swaps one of that user's folders for a symlink would otherwise point root at any folder on the system. So creating a file or a folder, delete, trash and restore, copy, move and rename, chmod and chown, compress and extract, and an upload that sets `owner` reach the folder they act in one name at a time from `/` and follow a symlink only where the session user cannot have planted it: in a folder that is not theirs and that no group or everyone may write, such as `/home -> /var/home`. A link in a folder the user can write is refused with 403, including one the user made on purpose: an admin whose `~/Documents` is a link to another disk cannot change files through that path as root, and uses the real path instead. Below that folder everything is reached through open handles without following a symlink (`openat` with `O_NOFOLLOW`, `renameat2`, `linkat`, `fchownat`, and a chmod or a removal through the handle of the entry or its folder), so a folder swapped for a link mid-operation fails the operation or is never entered. chmod follows a link at the item itself, as `chmod(1)` does, under the same rule. A new file is made with `O_EXCL`, so a create never truncates what is already at its name. A bridge that is not root acts as the user and follows the user's links as any program of theirs would, and a root session has no rule applied, since its user is no less trusted than the bridge (`services.openDirNoUserLinks`, `services.OpenNearestDirNoUserLinks`, `actingFor`).

Not covered: the files a compress reads, a root upload without `owner`, and where a trash restore goes. They are in [the backlog](./TODO/backlog.md#file-browser-root-follows-links-a-user-planted).

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

The pages that do not yet are in [the backlog](./TODO/backlog.md#what-a-user-without-sudo-sees).

## Testing

Dispatcher tests should cover:

- privileged route with unprivileged session returns forbidden
- privileged route with privileged session runs
- forbidden starts do not create Tasks
- forbidden starts are typed errors

See [API Contract](./api-contract.md) for route declaration and registration details.
