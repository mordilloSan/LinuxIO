# Privilege Pattern

Privilege is part of bridge route metadata. Handlers should not perform the normal route privilege gate themselves.

## Rule

Declare privileged operations where the route is registered:

```go
router.Job("dbus.reboot", handleReboot, bridgeipc.SingletonSystem, bridgeipc.Privileged)
```

or in command tables:

```go
bridgeipc.RegisterRoutes(router, "dbus", []bridgeipc.Command{
    {Name: "reboot", Mode: bridgeipc.ModeJob, Handler: handleReboot, Privileged: true},
})
```

The dispatcher checks `req.Session.Privileged` before the handler or runner starts. Forbidden starts are typed dispatcher errors and are logged centrally.

## What Belongs In Handlers

Handlers may still validate operation-specific policy:

- whether a requested resource exists
- whether an argument is allowed
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
