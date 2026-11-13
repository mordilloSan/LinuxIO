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

### In the frontend

The server's 403 is the gate; the page only avoids offering what will be refused. A control whose route is privileged is hidden for a session without sudo, read from `useAuth().privileged` or a route's `requiresPrivileged` access policy. The power menu, the dashboard's hostname and clock edits, the unit actions on the Services page and the network interface editor do this. The pages that do not yet are in [the backlog](./TODO/backlog.md#2-admin-only-routes).

## Testing

Dispatcher tests should cover:

- privileged route with unprivileged session returns forbidden
- privileged route with privileged session runs
- forbidden starts do not create Tasks
- forbidden starts are typed errors

See [API Contract](./api-contract.md) for route declaration and registration details.
