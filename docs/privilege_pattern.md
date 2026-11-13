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

To list the candidates (bindings with neither guard; routes that mutate only through polkit-gated D-Bus calls such as hostname, datetime, login1, systemd and NetworkManager also appear here and are a separate decision):

```sh
rg -n 'apischema\.(Call\[|DuplexRoute\[|Mutation(Void|Task)?\()' backend/bridge/handlers -g '!*_test.go' | rg -v 'Privileged\(\)|RetrySafe\(\)'
```

The command includes `DuplexRoute` because a stream can change the host as much as a mutation can: `container.open` starts a process inside a container.

### What is enforced today

The dispatcher enforces a declared guard. Nothing enforces that a route which needs the guard declares it: `TestRootOnlyStorageAndDockerRoutesArePrivileged` and `TestMonitoringMaintenanceRoutesArePrivileged` in `backend/bridge/apischema/schema_test.go` check routes they name (thirteen in the first), and a route that is not named is not checked. Three routes are known to need the guard and lack it:

- `container.open` and `terminal.list_shells` (`backend/bridge/handlers/terminal/handlers.go`) reach the Docker socket to start processes in a container, which the Docker rule above covers.
- `datetime.set_ntp_servers` (`backend/bridge/handlers/datetime/handlers.go`) writes under `/etc/systemd` or the chrony configuration. The other three datetime mutations go through timedated and are part of the separate polkit decision.

Closing this, with a guard that fails on any changing route that is neither privileged nor a listed exception, is in [the consistency backlog](./TODO/consistency-backlog.md).

The Assistant plugin routes (`assistant.install_plugin`, `uninstall_plugin`, `set_plugin_enabled` and the three `*_marketplace` routes) appear in that list and are unprivileged on purpose: they run the session user's own `claude` CLI as that user and write only under that user's home. See [Assistant](./AI-assistant.md#plugins).

## Testing

Dispatcher tests should cover:

- privileged route with unprivileged session returns forbidden
- privileged route with privileged session runs
- forbidden starts do not create Tasks
- forbidden starts are typed errors

See [API Contract](./api-contract.md) for route declaration and registration details.
