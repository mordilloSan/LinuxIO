# Architecture Refactor: Separate Auth Service

## Goal
Separate the privileged authentication logic from the web server to enable full systemd hardening.

## Current Architecture
```
linuxio.service (web server)
    │
    ├─► exec(linuxio-auth-helper)  ←── setuid root
    │       │
    │       └─► fork(nanny) → fork(bridge)
    │
    └─► Requires NoNewPrivileges=no (breaks hardening)
```

## New Architecture
```
linuxio.service (web server)                    linuxio-auth.service (auth daemon)
├─ DynamicUser=yes                              ├─ Runs as root
├─ Full hardening (all Protect*, etc.)          ├─ Listens on /run/linuxio/auth.sock
├─ NoNewPrivileges=yes ✓                        ├─ Does PAM auth + spawns bridges
│                                               │
└─► connects to /run/linuxio/auth.sock ────────►│
    sends: {user, password, session_id}         │
    receives: {ok, bridge_socket} or {error}    │
                                                └─► fork(nanny) → fork(bridge)
```

## Benefits
1. Web server is fully hardened - no privilege escalation possible
2. Auth daemon is small/focused - easier to audit
3. Clear separation of concerns
4. If web server compromised, attacker cannot escalate privileges

## Implementation Tasks

### Phase 1: Auth Daemon (C)

#### 1.1 Convert auth-helper to daemon mode
- [ ] Add `--daemon` flag to linuxio-auth-helper
- [ ] Create socket listener at `/run/linuxio/auth.sock`
- [ ] Main loop: accept connection → read request → process → respond
- [ ] Keep existing single-shot mode for backwards compatibility

#### 1.2 Define IPC protocol
```c
// Request (JSON over Unix socket)
{
    "action": "authenticate",
    "user": "username",
    "password": "...",
    "session_id": "uuid",
    "socket_path": "/run/linuxio/1000/linuxio-bridge-xxx.sock"
}

// Response (JSON)
{
    "status": "ok",           // or "error"
    "error": "...",           // if status=error
    "mode": "privileged",     // or "unprivileged"
    "socket_path": "..."      // bridge socket path
}
```

#### 1.3 Security for auth socket
- [ ] Socket owned by `root:linuxio` mode 0660
- [ ] Validate peer credentials (SO_PEERCRED)
- [ ] Only accept connections from linuxio group
- [ ] Rate limiting per client

### Phase 2: Go Server Changes

#### 2.1 New auth client
- [ ] Create `backend/server/auth/client.go`
- [ ] Connect to `/run/linuxio/auth.sock`
- [ ] Send auth request, receive response
- [ ] Handle connection errors/timeouts

#### 2.2 Update bridge.StartBridge()
- [ ] Replace `exec.Command(helperPath)` with socket communication
- [ ] Remove setuid helper spawning logic
- [ ] Keep existing bridge communication (yamux) unchanged

#### 2.3 Fallback mode (optional)
- [ ] If auth socket not available, fall back to exec helper
- [ ] Allows gradual migration

### Phase 3: Systemd Units

#### 3.1 New linuxio-auth.service
```ini
[Unit]
Description=LinuxIO Authentication Service
Before=linuxio.service

[Service]
Type=notify
ExecStart=/usr/local/bin/linuxio-auth-helper --daemon
Restart=on-failure

# Runs as root (required for PAM)
# But with some hardening
PrivateTmp=true
ProtectHome=read-only
ProtectSystem=strict
ReadWritePaths=/run/linuxio

[Install]
WantedBy=multi-user.target
```

#### 3.2 Update linuxio.service
```ini
[Unit]
Description=LinuxIO Web Server
Requires=linuxio.socket linuxio-auth.service
After=linuxio.socket linuxio-auth.service network-online.target

[Service]
DynamicUser=yes
RuntimeDirectory=linuxio
RuntimeDirectoryMode=02771

ExecStart=/usr/local/bin/linuxio run
Restart=on-failure
RestartSec=5s

# FULL hardening now possible!
PrivateTmp=true
ProtectSystem=strict
ProtectHome=false
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
LockPersonality=true
MemoryDenyWriteExecute=true
RestrictNamespaces=false
RestrictRealtime=true
DevicePolicy=closed
PrivateDevices=true
NoNewPrivileges=yes
UMask=0027
ReadOnlyPaths=/etc/linuxio
RemoveIPC=true
SystemCallFilter=~@keyring @cpu-emulation @debug @module @obsolete @raw-io @reboot @swap
SystemCallErrorNumber=EPERM

[Install]
WantedBy=multi-user.target
```

#### 3.3 Update linuxio.socket
- No changes needed

### Phase 4: Install Script Updates

#### 4.1 Update install-linuxio-binaries.sh
- [ ] Download/install new linuxio-auth.service
- [ ] Enable both services
- [ ] Create linuxio user/group (still needed for socket permissions)
- [ ] Update verification checks

### Phase 5: Testing

- [ ] Test fresh install
- [ ] Test upgrade from old architecture
- [ ] Test with multiple concurrent logins
- [ ] Test auth daemon restart while sessions active
- [ ] Test web server restart while sessions active
- [ ] Verify all hardening options are effective
- [ ] Security audit of auth socket

## Migration Path

1. Release with both modes (exec helper + socket daemon)
2. Default to socket daemon if available
3. Fall back to exec helper if daemon not running
4. Future release: remove exec helper fallback

## Files to Modify

### C (auth-helper)
- `packaging/linuxio-auth-helper.c` - add daemon mode

### Go (server)
- `backend/server/auth/client.go` - new file, auth socket client
- `backend/server/bridge/bridge.go` - use auth client instead of exec

### Systemd
- `packaging/systemd/linuxio.service` - update for full hardening
- `packaging/systemd/linuxio-auth.service` - new file

### Install
- `packaging/scripts/install-linuxio-binaries.sh` - add auth service

## Estimated Effort

| Phase | Effort |
|-------|--------|
| Phase 1 (C daemon) | Medium - reuse existing code, add listen loop |
| Phase 2 (Go client) | Small - simple socket client |
| Phase 3 (Systemd) | Small - new unit file |
| Phase 4 (Install) | Small - minor updates |
| Phase 5 (Testing) | Medium - thorough testing needed |

## Open Questions

1. Should auth daemon be socket-activated too? (start on first auth request)
2. Should we support multiple auth backends in the future? (LDAP, OAuth)
3. Should the auth daemon have its own idle-exit timeout?
