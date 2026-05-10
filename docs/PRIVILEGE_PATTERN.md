# Privilege Pattern

## Overview

LinuxIO has two separate privilege concepts:

1. Frontend access policy
2. Backend authorization

They are related, but they are not the same thing.

Frontend checks control what the UI shows. Backend checks control what an authenticated session can execute through the bridge. The backend check is the security boundary.

## Frontend Access Policy

Frontend privilege state is exposed through:

- `frontend/src/contexts/AuthContext.tsx`
- `frontend/src/hooks/useCapabilities.ts`
- `frontend/src/routes.tsx`

Examples:

- `requiresPrivileged: true` on a route hides or blocks that page in the app
- `useAccessContext()` exposes the current client-side `privileged` flag
- `useCapability(...)` combines privilege and capability checks for UI affordances

This is useful for UX, but it is not authorization. A hidden page does not prevent an authenticated client from calling a bridge command directly.

## Backend Authorization

Backend privilege enforcement is based on the bridge session created by the auth daemon:

1. The auth daemon validates PAM credentials.
2. The auth daemon checks whether the user can successfully run `sudo -v`.
3. The bridge is spawned for the authenticated session.
4. The session carries an immutable `sess.Privileged` flag.

That flag is the source of truth for privileged bridge commands.

## Current Backend Pattern

Bridge handlers are registered through `backend/bridge/handlers/internal/rpc`:

```go
func RegisterHandlers(rt runtime.Runtime) {
    rpc.Register("power", rt, []rpc.Command{
        {Name: "get_status", Handler: handleGetStatus, Privileged: true},
        {Name: "set_profile", Handler: handleSetProfile, Privileged: true},
    })
}
```

Set `Privileged: true` on each command that requires administrator privileges. `rpc.Register` wraps those commands with `privilege.RequirePrivilegedIPC(rt.Session, handler)` before calling `ipc.RegisterFunc`.

The privilege helper lives in `backend/bridge/privilege/privilege.go`:

```go
func RequirePrivilegedIPC(sess *session.Session, handler ipc.HandlerFunc) ipc.HandlerFunc
```

Most handler packages should not call that helper directly. Use `rpc.Command{Privileged: true}` so privilege policy is visible in the command registration table.

## Current Backend Coverage

Backend commands currently marked privileged:

| Handler | Commands |
|---------|----------|
| `indexer` | `get_config`, `get_status`, `set_config` |
| `power` | `get_status`, `start`, `set_profile`, `disable` |
| `system` | `list_failed_login_events` |

WireGuard is currently frontend-gated with `requiresPrivileged: true`, but its bridge commands are not marked `Privileged: true` in `backend/bridge/handlers/wireguard/handlers.go`. Do not treat that route metadata as backend authorization.

## Important Rule

Do not treat frontend checks as security.

These are not sufficient by themselves:

- `requiresPrivileged: true`
- `useAccessContext().privileged`
- `useCapability(...)`
- client-side session state in `AuthContext`
- `useSessionChecker`

Those are UI and session-state conveniences. They do not protect bridge commands.

## Recommended Usage

For new privileged work:

1. Add frontend gating so the UI behaves correctly for unprivileged users.
2. Add `Privileged: true` to the backend bridge command.
3. Prefer command-level gating in mixed packages where some commands are public reads and others are sensitive mutations.

For package-wide privileged modules, every registered command should be marked `Privileged: true`.

If a handler needs privilege for only part of a workflow, split the privileged operation into its own command when practical. That keeps the registration table honest and auditable.

## Audit Guidance

To audit real privileged operations, do not rely on route metadata alone.

Check:

```bash
rg 'Privileged: true' backend/bridge/handlers
rg 'RequirePrivilegedIPC' backend/bridge
rg 'sess\.Privileged|rt\.Privileged' backend/bridge
rg 'requiresPrivileged' frontend/src
```

Expected interpretation:

| Search | Meaning |
|--------|---------|
| `Privileged: true` | Primary backend command gate |
| `RequirePrivilegedIPC` | Underlying helper, normally used by `rpc.Register` |
| `sess.Privileged` / `rt.Privileged` | Direct privilege-dependent behavior; review case by case |
| `requiresPrivileged` | Frontend route policy only |

## Testing

To verify a privileged handler:

1. Sign in as a user without sudo access.
2. Call the bridge command directly.
3. Expect `operation requires administrator privileges`.
4. Sign in as a privileged user.
5. Verify the same command succeeds.

The direct bridge/API call matters because it proves the backend check exists independently of the UI.
