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

Read-only system inventory can usually remain unprivileged unless it exposes sensitive information.

To list the candidates (mutations with neither guard; routes that mutate only through polkit-gated D-Bus calls such as hostname, datetime, login1, systemd and NetworkManager also appear here and are a separate decision):

```sh
rg -n 'apischema\.(Call|Task|Job)\[' backend/bridge/handlers -g '!*_test.go' | rg -v 'Privileged\(\)|RetrySafe\(\)'
```

## Testing

Dispatcher tests should cover:

- privileged route with unprivileged session returns forbidden
- privileged route with privileged session runs
- forbidden starts do not create Tasks
- forbidden starts are typed errors

See [API Contract](./api-contract.md) for route declaration and registration details.
