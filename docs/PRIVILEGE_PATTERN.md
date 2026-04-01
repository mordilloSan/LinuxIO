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
func RequirePrivilegedIPC(
    sess *session.Session,
    handler ipc.HandlerFunc,
) ipc.HandlerFunc
```

These helpers are valid when handler registration already has direct access to `sess *session.Session`.

## Current IPC Handler Model

The active bridge handler system is the `ipc.RegisterFunc(...)` model, not the older `JsonHandlers[...]` map style.

## Pattern A: Registration-Time Enforcement

Use this when the package registration function already receives `sess *session.Session`.

This is the cleanest option for package-wide privilege rules, and it is the preferred pattern in the current codebase.

Example shape:

```go
func RegisterHandlers(sess *session.Session) {
    ipc.RegisterFunc(
        "example",
        "dangerous",
        privilege.RequirePrivilegedIPC(sess, handleDangerous),
    )
}
```

Use this when:

- the whole package is privileged
- the package already takes `sess`
- you want the protection to be obvious at registration time

If a package does not currently receive `sess`, the preferred fix is to thread `sess` into that package's `RegisterHandlers(...)` function rather than rely on a frontend route guard.

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

If a handler must be privileged, the backend must check `sess.Privileged` explicitly through registration-time enforcement.

## Recommended Usage

For new work:

- Always add frontend gating when the feature is privileged
- Always add backend authorization for the actual bridge command

For package-wide privileged modules:

- pass `sess` into registration and use registration-time enforcement

For mixed modules like `system`:

- keep public reads public
- gate sensitive operations in the backend at the individual handler level
- thread `sess` into registration and wrap only the sensitive handlers

## Audit Guidance

To audit real privileged operations, do not rely on route metadata alone.

Check:

- calls to `RequirePrivilegedIPC(...)`
- direct checks against `sess.Privileged`

## Testing

To verify a privileged handler:

1. Sign in as a user without sudo access
2. Call the bridge command directly
3. Expect `operation requires administrator privileges`
4. Sign in as a privileged user
5. Verify the same command succeeds

The direct bridge/API call matters because it proves the backend check exists independently of the UI.
