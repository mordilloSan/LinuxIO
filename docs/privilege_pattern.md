# Privilege Pattern

Privilege is route metadata. Handlers should not perform the normal route privilege gate themselves.

## Rule

Declare privileged operations in the relevant `backend/bridge/handlers/<domain>/handlers.go` binding entry:

```go
var api = apischema.Bindings(
    apischema.Job(
        "control.reboot",
        apischema.NoRequest(),
        apischema.NoResponse(),
        apischema.Privileged(),
    ).Handle(handleReboot),
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

Mark a route privileged when it can alter host state, secrets, users, services, storage, networking, packages, containers, or daemon configuration.

Read-only system inventory can usually remain unprivileged unless it exposes sensitive information.

## Testing

Dispatcher tests should cover:

- privileged route with unprivileged session returns forbidden
- privileged route with privileged session runs
- forbidden starts do not create jobs
- forbidden starts are typed errors

See [API Contract](./api-contract.md) for route declaration and registration details.
