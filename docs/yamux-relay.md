# Yamux Relay Architecture

## Overview

The LinuxIO server acts as a transparent byte relay between WebSocket clients and the bridge via yamux streams. The server never parses payloads - it only routes bytes by stream ID.

**Ultimate Goal:** One WebSocket connection per session with multiple streams for ALL communication:
- Persistent streams (terminal, logs) - stay open
- Request/response streams (API calls) - open → request → response → close

## Architecture

```
Browser                    Server                         Bridge
   │                         │                              │
   │════ WebSocket ══════════│══════ Yamux Session ════════│
   │                         │                              │
   │── Stream 1 (terminal) ──│────────────────────────────►│ PTY (persistent)
   │── Stream 3 (get_cpu) ───│────────────────────────────►│ → response → close
   │── Stream 5 (docker_ls) ─│────────────────────────────►│ → response → close
   │◄─ Stream 7 (push event) │◄────────────────────────────│ bridge-initiated
```

**Key Points:**
- 1 WebSocket per session (singleton)
- 1 yamux session per user (persistent)
- N streams per session (multiplexed)
- Server is pure byte relay (0 JSON parsing)

## Stream Types

### Persistent Streams (stay open)
| Type | Description | Status |
|------|-------------|--------|
| `terminal` | PTY session (host) | ✅ Done |
| `container` | Container terminal (docker exec) | ✅ Done |
| `container-logs` | Docker log tailing | Planned |
| `file-watch` | File system events | Planned |

### Request/Response Streams (open → close)
| Type | Description | Status |
|------|-------------|--------|
| `api` | JSON API calls (system, docker, dbus, config, filebrowser) | ✅ Done |
| `fb-download` | Binary file transfer | ✅ Done |
| `fb-upload` | Binary file upload | ✅ Done |
| `fb-archive` | Multi-file archive download | ✅ Done |
| `fb-compress` | Create archive from paths | ✅ Done |
| `fb-extract` | Extract archive to destination | ✅ Done |

### Bridge-Initiated Streams (push)
| Type | Description | Status |
|------|-------------|--------|
| `docker-event` | Container state changes | Planned |
| `system-alert` | Disk full, high CPU | Planned |
| `service-status` | systemd unit changes | Planned |

## Protocol Layers

### Layer 1: WebSocket Multiplexer (Frontend ↔ Server)

Binary WebSocket messages:
```
┌─────────────┬─────────────┬─────────────────────────────────┐
│ Stream ID   │ Flags       │ Payload (StreamFrame bytes)     │
│ (4 bytes BE)│ (1 byte)    │ (N bytes)                       │
└─────────────┴─────────────┴─────────────────────────────────┘
```

**Flags:**
```
0x01 = SYN   (open new stream)
0x04 = DATA  (data frame)
0x08 = FIN   (close stream)
0x10 = RST   (abort stream)
```

### Layer 2: StreamFrame (Server ↔ Bridge)

```
┌─────────────┬─────────────┬─────────────┬───────────────────┐
│ Opcode      │ Stream ID   │ Length      │ Payload           │
│ (1 byte)    │ (4 bytes BE)│ (4 bytes BE)│ (N bytes)         │
└─────────────┴─────────────┴─────────────┴───────────────────┘
```

**Opcodes:**
```go
OpStreamOpen   = 0x80  // payload = "type\0arg1\0arg2"
OpStreamData   = 0x81  // payload = raw bytes
OpStreamClose  = 0x82  // payload = empty
OpStreamResize = 0x83  // payload = [cols:2][rows:2]
```

## Request/Response Pattern (Future API Migration)

Current API call (JSON over HTTP):
```
Browser → POST /api/system/cpu → Server → JSON encode → Bridge → response → JSON decode → Browser
         ════════════════════════════════════════════════════════════════════════════════
         Multiple HTTP connections, JSON parsing at every hop
```

Future API call (stream):
```
Browser                          Server                      Bridge
   │                               │                           │
   │── open stream ───────────────►│                           │
   │── [type=api][{"method":"get_cpu"}] ──────────────────────►│
   │                               │                           │── handle
   │◄─────────────────── [{"cpu":45}] ─────────────────────────│
   │── close stream ──────────────►│                           │
   │                               │                           │
   ════════════════════════════════════════════════════════════
   Single WebSocket, server just routes bytes, no JSON parsing
```

**Benefits:**
- Server becomes stateless relay (no request parsing)
- Single connection handles everything
- Cancel any request by closing its stream
- Progress updates on same stream (file uploads)

## Implementation Status

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 1 | Yamux layer in bridge | ✅ Done |
| Phase 2 | Server yamux client | ✅ Done |
| Phase 3 | WebSocket binary relay | ✅ Done |
| Phase 4 | Terminal direct streaming | ✅ Done |
| Phase 5 | Persistent streams (singleton mux) | ✅ Done |
| Phase 6 | Bridge-initiated push | ⏳ Planned |
| Phase 7 | Migrate API calls to streams | ✅ Done |
| Phase 8 | File transfer streams | ✅ Done |
| Phase 9 | Remove legacy `/ws` system | ✅ Done |

## What's Done (Phases 1-5, 8)

### Backend
- `backend/common/ipc/yamux.go` - Yamux session helpers
- `backend/common/ipc/stream_relay.go` - StreamFrame protocol
- `backend/bridge/main.go` - Yamux server with auto-detection
- `backend/bridge/handlers/terminal/stream.go` - PTY streaming
- `backend/server/bridge/bridge.go` - Yamux client session pool
- `backend/server/web/websocket_relay.go` - Pure byte relay

### Frontend
- `frontend/src/services/StreamMultiplexer.ts` - Singleton WebSocket mux
- `frontend/src/hooks/useStreamMux.ts` - React hook
- `frontend/src/pages/main/terminal/Terminal.tsx` - Stream-based terminal

### Features Implemented (Phases 1-5)
- Terminal ~1ms latency (was ~60ms polling)
- Stream persistence across navigation
- 64KB circular scrollback buffer
- Frame buffering for split StreamFrames
- Terminal reset (close stream → new PTY)
- Auto-focus on navigation

### Phase 8: File Transfers
- `backend/bridge/handlers/filebrowser/stream.go` - All file transfer handlers
- `frontend/src/contexts/FileTransferContext.tsx` - Stream-based transfers

**Stream Types:**
- `fb-download` - Single file download (1MB chunks, progress every 2MB)
- `fb-upload` - Single file upload (512KB progress intervals for flow control)
- `fb-archive` - Multi-file archive streaming
- `fb-compress` - Create archive on disk
- `fb-extract` - Extract archive to destination

**Features:**
- Progress tracking via `OpStreamProgress` (0x84)
- Result frames via `OpStreamResult` (0x85)
- Upload flow control (4MB window, 512KB ACK intervals)
- Cancellation via `stream.abort()`

## What's Planned

### Phase 6: Bridge-Initiated Push
Bridge opens streams to push events:
```go
stream, _ := yamuxSession.Open()
ipc.WriteRelayFrame(stream, &StreamFrame{
    Opcode:  OpStreamData,
    Payload: []byte(`{"type":"docker_died","id":"abc123"}`),
})
```

### Phase 7: API Migration (Nearly Complete)
Replace HTTP handlers with stream handlers:
```go
// Instead of: router.GET("/api/system/cpu", handleCPU)
// Bridge handles: stream type "api" with method "get_cpu"
```

**Infrastructure Done:**
- `backend/bridge/handlers/api/stream.go` - API stream handler
- `frontend/src/utils/streamApi.ts` - Stream API client (`streamApi.get()`)
- `frontend/src/hooks/useStreamApi.ts` - React Query hooks (`useStreamQuery`)

**Frontend Migrations Completed:**

| Page/Component | Handler Type | Commands |
|----------------|--------------|----------|
| Dashboard/Processor | system | get_cpu_info |
| Dashboard/Memory | system | get_memory_info |
| Dashboard/Network | system | get_network_info |
| Dashboard/Drive | system | get_disks_info |
| Dashboard/FileSystem | system | get_filesystems_info |
| Dashboard/Gpu | system | get_gpu_info |
| Dashboard/MotherBoard | system | get_motherboard_info |
| Dashboard/System | system | get_host_info |
| Docker/ContainerList | docker | list_containers |
| Docker/ImageList | docker | list_images |
| Docker/NetworkList | docker | list_networks |
| ContainerCard | docker | start/stop/restart/remove_container, get_container_logs |
| Services/ServicesPage | dbus | ListServices, Start/Stop/Restart/Reload/Enable/Disable/Mask/UnmaskService |
| Services/ServiceLogsDrawer | dbus | GetServiceLogs |
| Updates/index | dbus | GetUpdates |
| Updates/UpdateSettings | dbus | GetAutoUpdates, SetAutoUpdates, ApplyOfflineUpdates |
| Network/NetworkInterfaceList | dbus | GetNetworkInfo |
| Network/NetworkInterfaceEditor | dbus | SetIPv4, SetIPv4Manual |
| WireGuard/InterfaceClients | wireguard | list_peers, remove_peer, peer_config_download, peer_qrcode |
| NavbarUserDropdown | dbus | Reboot, PowerOff |
| usePackageUpdater | dbus | InstallPackage, GetUpdates |
| UpdateBanner | control | update |
| UpdateHistoryCard | dbus | GetUpdateHistory |
| WireGuard | wireguard | list_interfaces, delete_interface, add_peer, toggle, create_interface |

#### FileBrowser (fully migrated to streaming)
All filebrowser operations now use streaming:
- File listings, stats, mutations use `streamApi` (filebrowser handler)
- File save uses `fb-upload` stream
- Directory size uses `streamApi.get("filebrowser", "dir_size", ...)`
- File search uses `streamApi`
- Transfers use dedicated streams (`fb-download`, `fb-upload`, `fb-archive`)

#### Auth (stays HTTP - session management)
| File | Endpoint | Description |
|------|----------|-------------|
| `AuthContext.tsx` | `/auth/me`, `/auth/login`, `/auth/logout` | Auth flow |

**Completed Migrations (HTTP handlers removed):**
- Power handlers (`power/`) - Now uses dbus Reboot/PowerOff
- Updates handlers (`updates/`) - Now uses dbus GetUpdates/InstallPackage/GetUpdateHistory
- Control update handler (`control/routes.go:TriggerUpdate`) - Now uses stream API
- System handlers (`system/`) - All 15 endpoints now use stream API
- Docker handlers (`docker/`) - Now uses stream API
- Drives handlers (`drives/`) - Now uses stream API
- Services handlers (`services/`) - Now uses dbus via stream API
- WireGuard handlers (`wireguard/`) - Now uses wireguard handler via stream API
- FileBrowser handlers (`navigator/`) - Fully migrated to streaming (filebrowser handler + fb-* streams)

**Remaining Tasks:**
- None - all API endpoints migrated to stream API

### Phase 9: Legacy `/ws` Cleanup ✅
The old `/ws` WebSocket system has been fully removed:

**Completed:**
- ✅ Container terminal migrated to yamux streams (`container` stream type)
- ✅ `channels.go` removed (was only for terminal route context)
- ✅ `websocket.go` cleaned (only keeps `upgrader` and `isExpectedWSClose` for relay)
- ✅ `progress.go` removed (progress broadcaster was unused)
- ✅ `WebSocketContext.tsx` removed
- ✅ `useWebSocket.ts` hook removed
- ✅ `/ws` route removed from router
- ✅ Theme/config endpoints migrated to stream API

## File Locations

| Component | File |
|-----------|------|
| WebSocket relay | `backend/server/web/websocket_relay.go` |
| Yamux session | `backend/server/bridge/bridge.go` |
| StreamFrame protocol | `backend/common/ipc/stream_relay.go` |
| Yamux helpers | `backend/common/ipc/yamux.go` |
| Bridge routing | `backend/bridge/main.go` |
| Terminal handler | `backend/bridge/handlers/terminal/stream.go` |
| File transfer handler | `backend/bridge/handlers/filebrowser/stream.go` |
| API stream handler | `backend/bridge/handlers/api/stream.go` |
| Config handler | `backend/bridge/handlers/config/handlers.go` |
| Frontend mux | `frontend/src/utils/StreamMultiplexer.ts` |
| Frontend stream API | `frontend/src/utils/streamApi.ts` |
| Frontend hook | `frontend/src/hooks/useStreamMux.ts` |
| Stream API hooks | `frontend/src/hooks/useStreamApi.ts` |
| Config context | `frontend/src/contexts/ConfigContext.tsx` |
| Terminal UI | `frontend/src/pages/main/terminal/Terminal.tsx` |
| File transfer context | `frontend/src/contexts/FileTransferContext.tsx` |

## Performance

| Metric | Current (HTTP) | Streams |
|--------|----------------|---------|
| Connections | N per request | 1 per session |
| Terminal latency | ~60ms | ~1ms |
| JSON ops in server | 6 per request | 0 |
| Request cancellation | Hacky | `stream.close()` |
| Server-push | Not possible | Native |

## Security

1. **Authentication**: WebSocket upgrade requires valid session cookie
2. **Stream isolation**: Each stream tied to authenticated session
4. **Payload opacity**: Server never inspects content
