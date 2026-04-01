# Privilege Pattern

## Overview

LinuxIO has two separate privilege concepts:

1. Frontend access policy
2. Backend authorization

They are related, but they are not the same thing.

Frontend checks control what the UI shows. Backend checks control what an authenticated session is actually allowed to execute through the bridge. The backend check is the security boundary.

## Frontend Access Policy

Frontend privilege state is exposed through:

- `frontend/src/contexts/AuthContext.tsx`
- `frontend/src/hooks/useCapabilities.ts`
- `frontend/src/routes.tsx`

Examples:

- `requiresPrivileged: true` on a route hides or blocks that page in the app
- `useAccessContext()` exposes the current client-side `privileged` flag

This is useful for UX, but it is not authorization. A hidden page does not prevent an authenticated client from calling a bridge command directly.

## Backend Authorization

Backend privilege enforcement is based on the bridge session created by the auth daemon:

1. The auth daemon validates PAM credentials
2. The auth daemon checks whether the user can successfully run `sudo -v`
3. The bridge is spawned either privileged or unprivileged
4. The session carries an immutable `sess.Privileged` flag

That flag is the source of truth for privileged operations.

## Current Backend Helpers

The privilege helper lives here:

- `backend/bridge/privilege/privilege.go`

It provides:

```go
func RequirePrivileged(
    sess *session.Session,
    handler func([]string) (any, error),
) func([]string) (any, error)

func RequirePrivilegedAll(
    sess *session.Session,
    handlers map[string]func([]string) (any, error),
) map[string]func([]string) (any, error)
```

These helpers are valid when handler registration already has direct access to `sess *session.Session`.

## Current IPC Handler Model

The active bridge handler system is the `ipc.RegisterFunc(...)` model, not the older `JsonHandlers[...]` map style.

Session context is injected centrally in:

- `backend/bridge/handlers/generic/bridge.go`

That means there are now two valid backend enforcement patterns.

## Pattern A: Registration-Time Enforcement

Use this when the package registration function already receives `sess *session.Session`.

This is the cleanest option for package-wide privilege rules.

Example shape:

```go
func RegisterHandlers(sess *session.Session) {
    guarded := privilege.RequirePrivileged(sess, func(args []string) (any, error) {
        return DangerousOperation(args)
    })

    ipc.RegisterFunc("example", "dangerous", func(ctx context.Context, args []string, emit ipc.Events) error {
        result, err := guarded(args)
        if err != nil {
            return err
        }
        return emit.Result(result)
    })
}
```

Use this when:

- the whole package is privileged
- the package already takes `sess`
- you want the protection to be obvious at registration time

## Pattern B: Context-Based Enforcement

Use this when handlers are registered through `ipc.RegisterFunc(...)` and the package does not receive `sess *session.Session` during registration.

In that case, the handler must read the session from `ctx` and check `sess.Privileged` at execution time.

Example shape:

```go
func handleDangerous(ctx context.Context, args []string, emit ipc.Events) error {
    sess, ok := generic.SessionFromContext(ctx)
    if !ok || !sess.Privileged {
        return fmt.Errorf("operation requires administrator privileges")
    }

    result, err := DangerousOperation(args)
    if err != nil {
        return err
    }
    return emit.Result(result)
}
```

Use this when:

- only a few handlers in a package need privilege
- the package currently has `RegisterHandlers()` with no session parameter
- refactoring registration to thread `sess` through the package is not worth it yet

## Choosing A vs B

Prefer Pattern A when the package already accepts `sess` or when an entire package is privileged.

Prefer Pattern B when:

- the package is mixed public and privileged
- the current registration signature is `RegisterHandlers()`
- you need a small, local authorization check without refactoring the whole package

## Important Rule

Do not treat frontend checks as security.

This is not sufficient by itself:

- `requiresPrivileged: true`
- `useAccessContext().privileged`
- client-side session state in `AuthContext`
- `useSessionChecker`

Those are UI and session-state conveniences. They do not protect bridge commands.

## Current Reality in This Repo

As of now:

- `frontend/src/routes.tsx` marks WireGuard as `requiresPrivileged`
- `frontend/src/hooks/useCapabilities.ts` enforces that policy in the UI
- `backend/bridge/privilege/privilege.go` exists
- `backend/bridge/handlers/wireguard/handlers.go` currently registers raw handlers directly with `ipc.RegisterFunc(...)`

So WireGuard currently has frontend privilege gating, but this document should not assume that all WireGuard bridge commands are already backend-protected by the helper.

If a handler must be privileged, the backend must check `sess.Privileged` explicitly through one of the two patterns above.

## Recommended Usage

For new work:

- Always add frontend gating when the feature is privileged
- Always add backend authorization for the actual bridge command

For package-wide privileged modules:

- pass `sess` into registration and use registration-time enforcement

For mixed modules like `system`:

- keep public reads public
- gate sensitive operations in the backend at the individual handler level
- use context-based enforcement if the package does not currently accept `sess`

## Audit Guidance

To audit real privileged operations, do not rely on route metadata alone.

Check:

- calls to `RequirePrivileged(...)`
- calls to `RequirePrivilegedAll(...)`
- handler code that reads the bridge session from `ctx`
- direct checks against `sess.Privileged`

## Testing

To verify a privileged handler:

1. Sign in as a user without sudo access
2. Call the bridge command directly
3. Expect `operation requires administrator privileges`
4. Sign in as a privileged user
5. Verify the same command succeeds

The direct bridge/API call matters because it proves the backend check exists independently of the UI.
