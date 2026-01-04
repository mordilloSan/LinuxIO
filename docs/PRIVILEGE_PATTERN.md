# Handler Privilege Checking Pattern

## Overview

LinuxIO uses a decorator pattern for enforcing privilege requirements on bridge handlers. This provides:
- **Fine-grained control**: Per-handler privilege enforcement
- **Clear audit trail**: Easy to identify privileged operations with `grep RequirePrivileged`
- **No boilerplate**: Single-line wrapper instead of repeated if-checks
- **Type safety**: Compile-time verification

## Implementation

### 1. Privilege Middleware

Location: `backend/common/middleware/privilege.go`

```go
// RequirePrivileged - wraps a single handler to enforce privilege checking
func RequirePrivileged(sess *session.Session, handler func([]string) (any, error)) func([]string) (any, error)

// RequirePrivilegedAll - wraps all handlers in a map to enforce privilege checking
func RequirePrivilegedAll(sess *session.Session, handlers map[string]func([]string) (any, error)) map[string]func([]string) (any, error)
```

Use these two functions for two patterns:
- **Fine-grained**: Use `RequirePrivileged` to wrap individual handlers in handler package (for mixed public/privileged)
- **Package-wide**: Use `RequirePrivilegedAll` in register.go to wrap entire handler map (for all-privileged packages)

### 2. Handler Registration Patterns

#### Pattern A: Package-Wide Privilege (Recommended for all-privileged packages)

**When to use:** ALL handlers in the package require privilege (e.g., WireGuard, Docker)

**Step 1**: Keep handler package unchanged (no session parameter needed):

```go
// wireguard/handlers.go
package wireguard

func WireguardHandlers() map[string]func([]string) (any, error) {
    return map[string]func([]string) (any, error){
        "list_interfaces":  ListInterfaces,
        "add_interface":    AddInterface,
        "remove_interface": RemoveInterface,
        // ...
    }
}
```

**Step 2**: Wrap all handlers in `register.go` using `RequirePrivilegedAll`:

```go
// register.go
import "github.com/mordilloSan/LinuxIO/backend/common/middleware"

func RegisterAllHandlers(shutdownChan chan string, sess *session.Session) {
    // WireGuard handlers - all operations require administrator privileges
    HandlersByType["wireguard"] = middleware.RequirePrivilegedAll(sess, wireguard.WireguardHandlers())
}
```

**Pros:**
- ✅ Handler package remains simple - no session dependency
- ✅ Clear intent: entire package is privileged
- ✅ Single point of enforcement in register.go
- ✅ No import of middleware in handler package

#### Pattern B: Fine-Grained Privilege (Recommended for mixed packages)

**When to use:** Some handlers are public, some require privilege (e.g., modules)

**Step 1**: Add `sess *session.Session` parameter to handler constructor:

```go
// modules/handlers.go
import (
    "github.com/mordilloSan/LinuxIO/backend/common/middleware"
    "github.com/mordilloSan/LinuxIO/backend/common/session"
)

func ModuleHandlers(sess *session.Session, handlerRegistry ...) map[string]func([]string) (any, error) {
    getDetailsHandler := func(args []string) (any, error) {
        // ... handler logic
    }

    return map[string]func([]string) (any, error){
        // Public - no privilege required
        "GetModules": GetLoadedModulesForFrontend,

        // Privileged - wrapped individually
        "GetModuleDetails": middleware.RequirePrivileged(sess, getDetailsHandler),
        "UninstallModule":  middleware.RequirePrivileged(sess, uninstallHandler),
    }
}
```

**Step 2**: Update handler registration in `register.go`:

```go
func RegisterAllHandlers(shutdownChan chan string, sess *session.Session) {
    HandlersByType["modules"] = modules.ModuleHandlers(sess, HandlersByType)
}
```

**Pros:**
- ✅ Explicit per-handler control
- ✅ Clear distinction between public and privileged operations
- ✅ Flexible for packages with mixed access requirements

## Example: WireGuard (Package-Wide Pattern)

**Handler package** (simple, no privilege logic):
```go
// wireguard/handlers.go
package wireguard

func WireguardHandlers() map[string]func([]string) (any, error) {
    return map[string]func([]string) (any, error){
        "list_interfaces":  ListInterfaces,
        "add_interface":    AddInterface,
        "remove_interface": RemoveInterface,
        "up_interface":     UpInterface,
        "down_interface":   DownInterface,
    }
}
```

**Registration** (privilege enforcement):
```go
// register.go
HandlersByType["wireguard"] = middleware.RequirePrivilegedAll(sess, wireguard.WireguardHandlers())
```

Result: **All WireGuard operations require administrator privileges**

## Security Model

### Authentication Flow

1. **Login**: User provides credentials
2. **PAM Auth**: System verifies password
3. **Sudo Check**: Auth daemon runs `sudo -v` with password
4. **Bootstrap**: Bridge spawned with `Privileged` flag in binary protocol
5. **Session**: Bridge has immutable `sess.Privileged` from bootstrap
6. **Handler Check**: Middleware verifies privilege before execution

### Attack Surface

- ❌ **Cannot bypass**: Client never controls `sess.Privileged`
- ❌ **Cannot forge**: Bootstrap comes from root auth daemon via stdin pipe
- ❌ **Cannot replay**: Session ID is UUID, tied to bridge process lifecycle
- ✅ **Auditable**: `grep RequirePrivileged` shows all protected operations

### Privilege Determination

A session is privileged if:
1. User authenticated via PAM successfully
2. User has sudo rights in `/etc/sudoers`
3. Password works with `sudo -v` command
4. Auth daemon (running as root) confirmed this

## Current Handlers with Privilege Checking

| Handler Package | Pattern | Public Handlers | Privileged Handlers |
|----------------|---------|-----------------|-------------------|
| **wireguard**  | Package-Wide | *none* | **All** (list_interfaces, add_interface, remove_interface, list_peers, add_peer, remove_peer, peer_qrcode, peer_config_download, get_keys, up_interface, down_interface) |
| **modules**    | Fine-Grained | GetModules | GetModuleDetails, UninstallModule, InstallModule, ValidateModule |

## Adding Privilege Checks to Existing Handlers

**TODO - Handlers needing privilege enforcement:**

- [ ] **docker**: All operations (use package-wide loop pattern in register.go)
- [ ] **filebrowser**: Write operations (use fine-grained pattern - read is public, write is privileged)
- [ ] **control**: Shutdown/reboot operations (likely all privileged, use package-wide loop)
- [ ] **system**: Package management operations (use fine-grained - info is public, install/remove is privileged)

**Pattern Selection Guide:**
- Use **Package-Wide** (loop in register.go) if: All handlers need privilege (docker, wireguard, control)
- Use **Fine-Grained** (wrap in handler package) if: Mix of public and privileged handlers (modules, filebrowser, system)

## Testing

To verify privilege enforcement:

1. **Login as non-privileged user** (no sudo access)
2. **Attempt privileged operation** via UI or API
3. **Expected result**: "operation requires administrator privileges" error

Example test:
```bash
# As non-sudo user, try to uninstall a module
curl -X POST https://localhost:8443/api/bridge \
  -H "Cookie: session_id=<session_id>" \
  -d '{"type":"modules","command":"UninstallModule","args":["some-module"]}'

# Expected: {"error": "operation requires administrator privileges"}
```

## Migration Checklist

### For Package-Wide Privilege (Recommended for all-privileged packages):

- [ ] **Handler package**: Keep unchanged (no modifications needed)
- [ ] **register.go**: Import `middleware` package
- [ ] **register.go**: Use `middleware.RequirePrivilegedAll(sess, package.PackageHandlers())`
- [ ] Test with non-privileged user
- [ ] Update this document

Example pattern:
```go
HandlersByType["package"] = middleware.RequirePrivilegedAll(sess, package.PackageHandlers())
```

### For Fine-Grained Privilege (For mixed public/privileged packages):

- [ ] **Handler package**: Import `middleware` and `session` packages
- [ ] **Handler package**: Add `sess *session.Session` parameter to constructor
- [ ] **Handler package**: Identify read-only vs write operations
- [ ] **Handler package**: Wrap privileged handlers with `middleware.RequirePrivileged(sess, handler)`
- [ ] **register.go**: Pass `sess` to handler constructor
- [ ] Test with non-privileged user
- [ ] Update this document

## Notes

- **No import cycles**: Middleware is in `common/middleware`, accessible by all handler packages
- **Minimal changes**: Only handler constructor signature changes
- **Backward compatible**: Can add privilege checks incrementally
- **Clear separation**: Public vs privileged operations clearly visible in code
