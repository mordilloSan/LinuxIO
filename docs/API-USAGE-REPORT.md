# LinuxIO Frontend API Usage Report

**Branch:** `dev/v0.8.0`
**Date:** 2026-02-16

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [JSON API (Promise-based, Request/Response)](#2-json-api-promise-based-requestresponse)
   - 2.1 [useQuery — React Query Data Fetching](#21-usequery--react-query-data-fetching)
   - 2.2 [useMutation — React Query Mutations](#22-usemutation--react-query-mutations)
   - 2.3 [.call() — Imperative Promise-based Calls](#23-call--imperative-promise-based-calls)
   - 2.4 [.queryOptions() — QueryClient Integration](#24-queryoptions--queryclient-integration)
   - 2.5 [.queryKey() — Cache Key Management](#25-querykey--cache-key-management)
   - 2.6 [queryClient.fetchQuery() — Imperative Fetching](#26-queryclientfetchquery--imperative-fetching)
   - 2.7 [queryClient.invalidateQueries() — Cache Invalidation](#27-queryclientinvalidatequeries--cache-invalidation)
   - 2.8 [queryClient.removeQueries() — Cache Removal](#28-queryclientremovequeries--cache-removal)
   - 2.9 [useQueries() — Parallel Queries](#29-usequeries--parallel-queries)
   - 2.10 [core.call() — Internal Transport Layer](#210-corecall--internal-transport-layer)
3. [Streaming API (WebSocket, Bidirectional)](#3-streaming-api-websocket-bidirectional)
   - 3.1 [Connection Lifecycle](#31-connection-lifecycle)
   - 3.2 [Connection Status Hooks](#32-connection-status-hooks)
   - 3.3 [Terminal Streams (Bidirectional, Persistent)](#33-terminal-streams-bidirectional-persistent)
   - 3.4 [Log Streams (Read-only, Live)](#34-log-streams-read-only-live)
   - 3.5 [Docker Operation Streams](#35-docker-operation-streams)
   - 3.6 [File Transfer Streams](#36-file-transfer-streams)
   - 3.7 [System Update Streams](#37-system-update-streams)
   - 3.8 [Package Update Streams](#38-package-update-streams)
   - 3.9 [Storage Streams (SMART Tests)](#39-storage-streams-smart-tests)
   - 3.10 [Stream Lifecycle Primitives](#310-stream-lifecycle-primitives)
   - 3.11 [String Encoding/Decoding](#311-string-encodingdecoding)
   - 3.12 [Flow Control Constants](#312-flow-control-constants)
4. [Summary Statistics](#4-summary-statistics)
5. [Complete API Command Inventory](#5-complete-api-command-inventory)
6. [Coherence & Patterns Analysis](#6-coherence--patterns-analysis)

---

## 1. Architecture Overview

The frontend API is built on a **binary WebSocket stream multiplexer** (`StreamMultiplexer`) that provides all communication with the backend over a single WebSocket connection. On top of this, two distinct API paradigms are exposed:

```
                        ┌──────────────────────────────┐
                        │      @/api (index.ts)        │  ← Barrel module
                        └──────────────────────────────┘
                           │                        │
              ┌────────────┘                        └────────────┐
              ▼                                                  ▼
   ┌─────────────────────┐                          ┌─────────────────────┐
   │  JSON API            │                          │  Streaming API       │
   │  (react-query.ts)    │                          │  (linuxio.ts)        │
   │                      │                          │                      │
   │  linuxio.h.c.useQuery│                          │  openTerminalStream  │
   │  linuxio.h.c.useMut. │                          │  openDockerLogsStream│
   │  linuxio.h.c.call()  │                          │  openFileUploadStream│
   │  linuxio.h.c.qOpts() │                          │  ... 16 stream types │
   └──────────┬───────────┘                          └──────────┬──────────┘
              │                                                  │
              ▼                                                  ▼
   ┌─────────────────────┐                          ┌─────────────────────┐
   │  linuxio-core.ts     │                          │  Payload builders    │
   │  call() / spawn()    │                          │  (linuxio.ts)        │
   │  openStream()        │                          │  terminalPayload()   │
   └──────────┬───────────┘                          │  uploadPayload()     │
              │                                      │  ...                 │
              ▼                                      └──────────┬──────────┘
   ┌──────────────────────────────────────────────────────────────┐
   │               StreamMultiplexer.ts                           │
   │          Binary WebSocket with multiplexed streams           │
   │          (bridge, terminal, exec, logs, filetransfer)        │
   └──────────────────────────────────────────────────────────────┘
```

**JSON API** uses `"bridge"` stream type internally — opens a short-lived stream, sends the command, waits for `onResult`, resolves the promise.

**Streaming API** uses specialized stream types (`"terminal"`, `"logs"`, `"exec"`, `"filetransfer"`) — opens long-lived streams with `onData`, `onProgress`, `onResult` callbacks.

---

## 2. JSON API (Promise-based, Request/Response)

### 2.1 useQuery — React Query Data Fetching

Snapshot of `useQuery` hook usage across the codebase.

#### System Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 1 | `pages/main/dashboard/Processor.tsx` | 16 | `linuxio.system.get_cpu_info.useQuery(...)` | CPU info with polling |
| 2 | `pages/main/dashboard/Memory.tsx` | 20 | `linuxio.system.get_memory_info.useQuery(...)` | Memory info with polling |
| 3 | `pages/main/dashboard/Gpu.tsx` | 13 | `linuxio.system.get_gpu_info.useQuery(...)` | GPU info with polling |
| 4 | `pages/main/dashboard/Network.tsx` | 12 | `linuxio.system.get_network_info.useQuery(...)` | Network stats with polling |
| 5 | `pages/main/dashboard/System.tsx` | 52 | `linuxio.system.get_host_info.useQuery(...)` | Host info |
| 6 | `pages/main/dashboard/System.tsx` | 37 | `linuxio.system.get_updates_fast.useQuery({refetchInterval: 50000})` | Quick update count |
| 7 | `pages/main/dashboard/System.tsx` | 47 | `linuxio.system.get_processes.useQuery(...)` | Process list |
| 8 | `pages/main/dashboard/MotherBoard.tsx` | 10 | `linuxio.system.get_motherboard_info.useQuery(...)` | Motherboard info |
| 9 | `pages/main/dashboard/FileSystem.tsx` | 13 | `linuxio.system.get_fs_info.useQuery(...)` | Filesystem info |
| 10 | `pages/main/storage/DiskOverview/index.tsx` | 317 | `linuxio.system.get_fs_info.useQuery({refetchInterval: 10000})` | Filesystem info for disk overview |

#### Storage Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 11 | `pages/main/dashboard/Drive.tsx` | 56 | `linuxio.storage.get_drive_info.useQuery()` | Drive info for dashboard |
| 11b | `pages/main/storage/DiskOverview/index.tsx` | 314 | `linuxio.storage.get_drive_info.useQuery({refetchInterval: 30000})` | Drive info for disk overview |
| 12 | `pages/main/storage/NFSMounts.tsx` | 562 | `linuxio.storage.list_nfs_mounts.useQuery({refetchInterval: 10000})` | List NFS mounts with polling |
| 13 | `pages/main/storage/LVMManagement.tsx` | 553 | `linuxio.storage.list_pvs.useQuery({refetchInterval: 10000})` | List physical volumes with polling |
| 14 | `pages/main/storage/LVMManagement.tsx` | 559 | `linuxio.storage.list_vgs.useQuery({refetchInterval: 10000})` | List volume groups with polling |
| 15 | `pages/main/storage/LVMManagement.tsx` | 565 | `linuxio.storage.list_lvs.useQuery({refetchInterval: 10000})` | List logical volumes with polling |

#### Docker Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 16 | `pages/main/docker/ContainerList.tsx` | 9 | `linuxio.docker.list_containers.useQuery(...)` | Docker containers |
| 17 | `pages/main/docker/NetworkList.tsx` | 242 | `linuxio.docker.list_networks.useQuery(...)` | Docker networks |
| 18 | `pages/main/docker/VolumeList.tsx` | 118 | `linuxio.docker.list_volumes.useQuery(...)` | Docker volumes |
| 19 | `pages/main/docker/ImageList.tsx` | 142 | `linuxio.docker.list_images.useQuery(...)` | Docker images |
| 20 | `pages/main/docker/ComposeStacksPage.tsx` | 97 | `linuxio.docker.list_compose_projects.useQuery({refetchInterval: 5000})` | Compose stacks with polling |
| 21 | `hooks/useDockerIcon.ts` | 10 | `linuxio.docker.get_icon_uri.useQuery({args: [identifier], staleTime: ONE_DAY})` | Docker container icon URI |
| 22 | `components/docker/ReindexDialog.tsx` | 64 | `linuxio.docker.list_compose_projects.useQuery({enabled: open && success})` | Stacks summary after reindex |

#### DBus Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 23 | `pages/main/services/ServicesPage.tsx` | 23 | `linuxio.dbus.list_services.useQuery({refetchInterval: 2000})` | List systemd services with polling |
| 24 | `pages/main/network/NetworkInterfaceList.tsx` | 52 | `linuxio.dbus.get_network_info.useQuery(...)` | Network interfaces |
| 25 | `pages/main/updates/index.tsx` | 20 | `linuxio.dbus.get_updates_basic.useQuery(...)` | Updates page top-level |
| 25b | `pages/main/updates/UpdateList.tsx` | 29 | `linuxio.dbus.get_updates_basic.useQuery()` | List available updates |
| 26 | `pages/main/updates/UpdateHistory.tsx` | 28 | `linuxio.dbus.get_update_history.useQuery()` | Update history |
| 27 | `pages/main/updates/UpdateSettings.tsx` | 46 | `linuxio.dbus.get_auto_updates.useQuery()` | Auto-update settings |
| 28 | `hooks/usePackageUpdater.ts` | 39 | `linuxio.dbus.get_updates_basic.useQuery({enabled: false})` | Manual refetch for post-update |
| 28b | `pages/main/wireguard/CreateInterfaceButton.tsx` | 35 | `linuxio.dbus.get_network_info.useQuery()` | Network info for WG interface creation |

#### Filebrowser Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 29 | `hooks/useFileQueries.ts` | 38 | `linuxio.filebrowser.resource_get.useQuery(normalizedPath, {...})` | Directory listing |
| 29b | `hooks/useFileQueries.ts` | 71 | `linuxio.filebrowser.resource_get.useQuery(detailTarget, "", "true", {...})` | Single file detail with content |
| 29c | `hooks/useFileQueries.ts` | 153 | `linuxio.filebrowser.resource_get.useQuery(editingPath, "", "true", {...})` | File content for editor |
| 30 | `hooks/useFileSearch.ts` | 45 | `linuxio.filebrowser.search.useQuery(query, limit, basePath, {...})` | File search |
| 31 | `hooks/useFileSubfolders.ts` | 47 | `linuxio.filebrowser.subfolders.useQuery(path, {...})` | Subfolder sizes |
| 32 | `hooks/useFileDirectorySize.ts` | 41 | `linuxio.filebrowser.dir_size.useQuery(path, {...})` | Single dir size |
| 32b | `hooks/useFileDirectorySizeBase.ts` | 16 | `linuxio.filebrowser.dir_size.useQuery(dirPath, {...})` | Dir size (base hook) |
| 33 | `hooks/useFileQueries.ts` | 84 | `linuxio.filebrowser.resource_stat.useQuery(detailTarget, {...})` | File stat (permissions, owner) |
| 34 | `components/filebrowser/PermissionsDialog.tsx` | 134 | `linuxio.filebrowser.users_groups.useQuery({enabled: open})` | Users/groups for permissions |

#### Accounts Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 35 | `pages/main/accounts/UsersTab.tsx` | 41 | `linuxio.accounts.list_users.useQuery(...)` | List system users |
| 36 | `pages/main/accounts/GroupsTab.tsx` | 31 | `linuxio.accounts.list_groups.useQuery(...)` | List system groups |
| 37 | `pages/main/accounts/components/CreateUserDialog.tsx` | 40 | `linuxio.accounts.list_shells.useQuery()` | Available shells |
| 37b | `pages/main/accounts/components/CreateUserDialog.tsx` | 41 | `linuxio.accounts.list_groups.useQuery()` | Groups for user creation |
| 38 | `pages/main/accounts/components/EditUserDialog.tsx` | 38 | `linuxio.accounts.list_shells.useQuery()` | Shells for user editing |
| 38b | `pages/main/accounts/components/EditUserDialog.tsx` | 39 | `linuxio.accounts.list_groups.useQuery()` | Groups for user editing |
| 39 | `pages/main/accounts/components/EditGroupMembersDialog.tsx` | 39 | `linuxio.accounts.list_users.useQuery()` | Users for group member editing |

#### Wireguard Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 40 | `pages/main/wireguard/WireguardDashboard.tsx` | 31 | `linuxio.wireguard.list_interfaces.useQuery({refetchInterval: 10000})` | WireGuard interfaces with polling |
| 40b | `pages/main/wireguard/CreateInterfaceButton.tsx` | 38 | `linuxio.wireguard.list_interfaces.useQuery()` | WG interfaces for creation form |
| 41 | `pages/main/wireguard/InterfaceClients.tsx` | 96 | `linuxio.wireguard.list_peers.useQuery(interfaceName, {refetchInterval: 3000})` | WireGuard peers with polling |

#### Terminal Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 42 | `pages/main/terminal/Terminal.tsx` | 26 | `linuxio.terminal.list_shells.useQuery({staleTime: 60000})` | Available shells |
| 43 | `pages/main/docker/TerminalDialog.tsx` | 65 | `linuxio.terminal.list_shells.useQuery(containerId, {enabled})` | Shells for container terminal |

#### Modules Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 44 | `pages/main/modules/ModulesList.tsx` | 23 | `linuxio.modules.get_modules.useQuery()` | Installed modules |
| 44b | `routes.tsx` | 241 | `linuxio.modules.get_modules.useQuery({staleTime, refetchOnMount: false})` | Modules for route building |
| 44c | `routes.tsx` | 293 | `linuxio.modules.get_modules.useQuery({staleTime, refetchOnMount: false})` | Modules for sidebar items |

#### Config Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 45 | `pages/main/docker/ComposeStacksPage.tsx` | 97 | `linuxio.config.get.useQuery({staleTime: ...})` | Docker folder config |

#### Control Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 46 | `components/footer/Footer.tsx` | 11 | `linuxio.control.version.useQuery({staleTime: FIVE_MINUTES})` | App version |

#### Dynamic String-Based (Module SDK)

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| — | `api/react-query.ts` | 78 | `useCall(handler, command, args, options)` | Generic typed query hook used by modules |

---

### 2.2 useMutation — React Query Mutations

Snapshot of `useMutation` hook usage across the codebase.

#### Docker Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 1 | `components/cards/ContainerCard.tsx` | 60 | `linuxio.docker.start_container.useMutation()` | Start container |
| 2 | `components/cards/ContainerCard.tsx` | 75 | `linuxio.docker.stop_container.useMutation()` | Stop container |
| 3 | `components/cards/ContainerCard.tsx` | 90 | `linuxio.docker.restart_container.useMutation()` | Restart container |
| 4 | `components/cards/ContainerCard.tsx` | 105 | `linuxio.docker.remove_container.useMutation()` | Remove container |
| 5 | `pages/main/docker/NetworkList.tsx` | 62 | `linuxio.docker.create_network.useMutation()` | Create network |
| 6 | `pages/main/docker/NetworkList.tsx` | 175 | `linuxio.docker.delete_network.useMutation()` | Delete network |
| 7 | `pages/main/docker/VolumeList.tsx` | 51 | `linuxio.docker.delete_volume.useMutation()` | Delete volume |
| 8 | `pages/main/docker/ImageList.tsx` | 52 | `linuxio.docker.delete_image.useMutation()` | Delete image |
| 9 | `pages/main/docker/ComposeStacksPage.tsx` | 102 | `linuxio.docker.delete_stack.useMutation()` | Delete compose stack |

#### DBus Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 10 | `pages/main/services/ServicesPage.tsx` | 47 | `linuxio.dbus.start_service.useMutation()` | Start service |
| 11 | `pages/main/services/ServicesPage.tsx` | 66 | `linuxio.dbus.stop_service.useMutation()` | Stop service |
| 12 | `pages/main/services/ServicesPage.tsx` | 80 | `linuxio.dbus.restart_service.useMutation()` | Restart service |
| 13 | `pages/main/services/ServicesPage.tsx` | 99 | `linuxio.dbus.reload_service.useMutation()` | Reload service |
| 14 | `pages/main/services/ServicesPage.tsx` | 118 | `linuxio.dbus.enable_service.useMutation()` | Enable service |
| 15 | `pages/main/services/ServicesPage.tsx` | 137 | `linuxio.dbus.disable_service.useMutation()` | Disable service |
| 16 | `pages/main/services/ServicesPage.tsx` | 156 | `linuxio.dbus.mask_service.useMutation()` | Mask service |
| 17 | `pages/main/services/ServicesPage.tsx` | 170 | `linuxio.dbus.unmask_service.useMutation()` | Unmask service |
| 18 | `components/navbar/NavbarUserDropdown.tsx` | 32 | `linuxio.dbus.reboot.useMutation()` | Reboot system |
| 19 | `components/navbar/NavbarUserDropdown.tsx` | 42 | `linuxio.dbus.power_off.useMutation()` | Power off system |
| 20 | `pages/main/updates/UpdateSettings.tsx` | 76 | `linuxio.dbus.set_auto_updates.useMutation()` | Save auto-update settings |
| 21 | `pages/main/updates/UpdateSettings.tsx` | 93 | `linuxio.dbus.apply_offline_updates.useMutation()` | Apply offline updates |
| 22 | `hooks/usePackageUpdater.ts` | 36 | `linuxio.dbus.install_package.useMutation()` | Install single package |

#### Filebrowser Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 23 | `hooks/useFileMutations.ts` | 65 | `linuxio.filebrowser.resource_post.useMutation()` | Create file |
| 24 | `hooks/useFileMutations.ts` | 84 | `linuxio.filebrowser.resource_post.useMutation()` | Create folder |
| 25 | `hooks/useFileMutations.ts` | 103 | `linuxio.filebrowser.resource_delete.useMutation()` | Delete file/folder |
| 26 | `hooks/useFileMutations.ts` | 158 | `linuxio.filebrowser.chmod.useMutation()` | Change permissions |
| 27 | `hooks/useFileMutations.ts` | 188 | `linuxio.filebrowser.resource_patch.useMutation()` | Rename file/folder |

#### Accounts Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 28 | `pages/main/accounts/components/CreateUserDialog.tsx` | 43 | `linuxio.accounts.create_user.useMutation()` | Create user |
| 29 | `pages/main/accounts/components/EditUserDialog.tsx` | 49 | `linuxio.accounts.modify_user.useMutation()` | Modify user |
| 30 | `pages/main/accounts/components/DeleteUserDialog.tsx` | 40 | `linuxio.accounts.delete_user.useMutation()` | Delete user |
| 31 | `pages/main/accounts/components/ChangePasswordDialog.tsx` | 27 | `linuxio.accounts.change_password.useMutation()` | Change password |
| 32 | `pages/main/accounts/UsersTab.tsx` | 114 | `linuxio.accounts.lock_user.useMutation()` | Lock user |
| 33 | `pages/main/accounts/UsersTab.tsx` | 124 | `linuxio.accounts.unlock_user.useMutation()` | Unlock user |
| 34 | `pages/main/accounts/components/CreateGroupDialog.tsx` | 24 | `linuxio.accounts.create_group.useMutation()` | Create group |
| 35 | `pages/main/accounts/components/EditGroupMembersDialog.tsx` | 44 | `linuxio.accounts.modify_group_members.useMutation()` | Edit group members |

#### Storage Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 36 | `pages/main/storage/NFSMounts.tsx` | 80 | `linuxio.storage.mount_nfs.useMutation()` | Mount NFS |
| 37 | `pages/main/storage/NFSMounts.tsx` | 285 | `linuxio.storage.unmount_nfs.useMutation()` | Unmount NFS |
| 38 | `pages/main/storage/NFSMounts.tsx` | 416 | `linuxio.storage.remount_nfs.useMutation()` | Remount NFS |
| 39 | `pages/main/storage/LVMManagement.tsx` | 85 | `linuxio.storage.create_lv.useMutation()` | Create logical volume |
| 40 | `pages/main/storage/LVMManagement.tsx` | 200 | `linuxio.storage.resize_lv.useMutation()` | Resize logical volume |
| 41 | `pages/main/storage/LVMManagement.tsx` | 292 | `linuxio.storage.delete_lv.useMutation()` | Delete logical volume |
| 42 | `pages/main/storage/DiskOverview/index.tsx` | 73 | `linuxio.storage.run_smart_test.useMutation()` | Run SMART test |

#### Wireguard Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 43 | `pages/main/wireguard/CreateInterfaceButton.tsx` | 42 | `linuxio.wireguard.add_interface.useMutation()` | Add interface |
| 44 | `pages/main/wireguard/WireguardDashboard.tsx` | 37 | `linuxio.wireguard.remove_interface.useMutation()` | Remove interface |
| 45 | `pages/main/wireguard/WireguardDashboard.tsx` | 58 | `linuxio.wireguard.add_peer.useMutation()` | Add peer |
| 46 | `pages/main/wireguard/WireguardDashboard.tsx` | 72 | `linuxio.wireguard.up_interface.useMutation()` | Bring interface up |
| 47 | `pages/main/wireguard/WireguardDashboard.tsx` | 90 | `linuxio.wireguard.down_interface.useMutation()` | Bring interface down |
| 48 | `pages/main/wireguard/WireguardDashboard.tsx` | 108 | `linuxio.wireguard.enable_interface.useMutation()` | Enable at boot |
| 49 | `pages/main/wireguard/WireguardDashboard.tsx` | 126 | `linuxio.wireguard.disable_interface.useMutation()` | Disable at boot |
| 50 | `pages/main/wireguard/InterfaceClients.tsx` | 103 | `linuxio.wireguard.remove_peer.useMutation()` | Remove peer |
| 51 | `pages/main/wireguard/InterfaceClients.tsx` | 118 | `linuxio.wireguard.peer_config_download.useMutation()` | Download peer config |
| 52 | `pages/main/wireguard/InterfaceClients.tsx` | 128 | `linuxio.wireguard.peer_qrcode.useMutation()` | Generate peer QR |

#### Network Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 53 | `pages/main/network/NetworkInterfaceEditor.tsx` | 105 | `linuxio.dbus.set_ipv4.useMutation()` | Set DHCP |
| 54 | `pages/main/network/NetworkInterfaceEditor.tsx` | 120 | `linuxio.dbus.set_ipv4_manual.useMutation()` | Set manual IPv4 |
| 55 | `pages/main/network/NetworkInterfaceEditor.tsx` | 140 | `linuxio.dbus.enable_connection.useMutation()` | Enable connection |
| 56 | `pages/main/network/NetworkInterfaceEditor.tsx` | 155 | `linuxio.dbus.disable_connection.useMutation()` | Disable connection |

#### Config Handler

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 57 | `contexts/ConfigContext.tsx` | 87 | `linuxio.config.set.useMutation()` | Save configuration |

#### Modules Handler (via string-based API)

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 58 | `api/react-query.ts` | 110 | `useMutate(handler, command, options)` | Generic mutation hook for modules |

---

### 2.3 .call() — Imperative Promise-based Calls

4 total imperative calls in consumer code (outside API layer).

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 1 | `contexts/AuthContext.tsx` | 166 | `linuxio.system.get_capabilities.call()` | Fetch docker/indexer capabilities on auth + reconnect |
| 2 | `contexts/FileTransferContext.tsx` | 1264 | `linuxio.filebrowser.resource_post.call(dirPath, override)` | Create folders during bulk upload |
| 3 | `components/docker/StackSetupDialog.tsx` | 111 | `linuxio.docker.validate_stack_directory.call(workingDir)` | Validate stack directory path |
| 4 | `pages/main/docker/ComposeStacksPage.tsx` | 279 | `linuxio.docker.validate_compose.call(content)` | Validate compose YAML content |

---

### 2.4 .queryOptions() — QueryClient Integration

8 total `queryOptions()` usages.

| # | File | Line | Call | Description |
|---|------|------|------|-------------|
| 1 | `contexts/ConfigContext.tsx` | 115 | `linuxio.config.get.queryOptions({staleTime: NONE})` | Fetch backend settings at startup |
| 2 | `hooks/useFileQueries.ts` | 114 | `linuxio.filebrowser.resource_get.queryOptions(path, {staleTime: NONE})` | Multi-select file detail fetch |
| 3 | `hooks/useFileMultipleDirectoryDetails.ts` | 48 | `linuxio.filebrowser.dir_size.queryOptions(path, ...)` | Directory sizes via useQueries |
| 4 | `pages/main/filebrowser/index.tsx` | 545 | `linuxio.filebrowser.resource_stat.queryOptions(selectedPath, {staleTime: 5000})` | File stat for permissions dialog |
| 5 | `pages/main/docker/ComposeStacksPage.tsx` | 247 | `linuxio.filebrowser.resource_get.queryOptions(configPath, "", "true", {staleTime: NONE})` | Fetch compose file content |
| 6 | `pages/main/docker/ComposeStacksPage.tsx` | 396 | `linuxio.docker.get_compose_file_path.queryOptions(stackName, {staleTime: NONE})` | Get compose file path |
| 7 | `pages/main/storage/NFSMounts.tsx` | 104 | `linuxio.storage.list_nfs_exports.queryOptions(serverAddress, {staleTime: 30000})` | Fetch NFS exports |
| 8 | `pages/main/updates/UpdateList.tsx` | 50 | `linuxio.dbus.get_update_detail.queryOptions(packageId, {staleTime: 300000})` | Fetch update changelog (5min cache) |

---

### 2.5 .queryKey() — Cache Key Management

41 total `queryKey()` usages for cache invalidation, removal, and key building.

#### Docker Domain (9 keys)

| # | File | Line | Key | Context |
|---|------|------|-----|---------|
| 1-4 | `components/cards/ContainerCard.tsx` | 64,79,94,109 | `linuxio.docker.list_containers.queryKey()` | After start/stop/restart/remove |
| 5-6 | `pages/main/docker/NetworkList.tsx` | 66,194 | `linuxio.docker.list_networks.queryKey()` | After create/delete network |
| 7 | `pages/main/docker/VolumeList.tsx` | 70 | `linuxio.docker.list_volumes.queryKey()` | After delete volume |
| 8 | `pages/main/docker/ImageList.tsx` | 91 | `linuxio.docker.list_images.queryKey()` | After delete image |

#### Accounts Domain (8 keys)

| # | File | Line | Key | Context |
|---|------|------|-----|---------|
| 9-14 | `pages/main/accounts/...` | various | `linuxio.accounts.list_users.queryKey()` | After create/delete/modify/lock/unlock/password change |
| 15-17 | `pages/main/accounts/...` | various | `linuxio.accounts.list_groups.queryKey()` | After create/delete/member edit |

#### Storage Domain (8 keys)

| # | File | Line | Key | Context |
|---|------|------|-----|---------|
| 18-20 | `pages/main/storage/NFSMounts.tsx` | 88,293,424 | `linuxio.storage.list_nfs_mounts.queryKey()` | After mount/unmount/remount |
| 21-26 | `pages/main/storage/LVMManagement.tsx` | various | `linuxio.storage.list_lvs/list_vgs.queryKey()` | After create/resize/delete LV |

#### Services/Network Domain (5 keys)

| # | File | Line | Key | Context |
|---|------|------|-----|---------|
| 27 | `pages/main/services/ServicesPage.tsx` | 29 | `linuxio.dbus.list_services.queryKey()` | Shared invalidation for all 8 service mutations |
| 28-31 | `pages/main/network/NetworkInterfaceEditor.tsx` | 116,133,153,168 | `linuxio.dbus.get_network_info.queryKey()` | After DHCP/manual/enable/disable |

#### Filebrowser Domain (7 keys)

| # | File | Line | Key | Context |
|---|------|------|-----|---------|
| 32 | `hooks/useFileMutations.ts` | 60 | `linuxio.filebrowser.resource_get.queryKey(normalizedPath)` | After any file mutation |
| 33-34 | `hooks/useFileSubfolders.ts` | 98,110 | `linuxio.filebrowser.subfolders.queryKey()` | Clear subfolder caches |
| 35 | `hooks/useFileQueries.ts` | 96 | `linuxio.filebrowser.resource_get.queryKey("multi")` | Composite key for multi-detail |
| 36-38 | `pages/main/filebrowser/index.tsx` | 847,945,968 | `linuxio.filebrowser.resource_get.queryKey(...)` | After file save / listing refresh |

#### Wireguard Domain (2 keys)

| # | File | Line | Key | Context |
|---|------|------|-----|---------|
| 39 | `pages/main/wireguard/CreateInterfaceButton.tsx` | 45 | `linuxio.wireguard.list_interfaces.queryKey()` | After add interface |
| 40 | `pages/main/wireguard/InterfaceClients.tsx` | 106 | `linuxio.wireguard.list_peers.queryKey()` | After remove peer |

---

### 2.6 queryClient.fetchQuery() — Imperative Fetching

7 total usages.

| # | File | Line | Description |
|---|------|------|-------------|
| 1 | `contexts/ConfigContext.tsx` | 114 | Fetch backend settings on initial load |
| 2 | `hooks/useFileQueries.ts` | 113 | Fetch multiple file resources in parallel for multi-select |
| 3 | `pages/main/filebrowser/index.tsx` | 544 | Fetch file stat before opening permissions dialog |
| 4 | `pages/main/docker/ComposeStacksPage.tsx` | 246 | Fetch compose file content for editing |
| 5 | `pages/main/docker/ComposeStacksPage.tsx` | 395 | Get compose file path for stack creation |
| 6 | `pages/main/storage/NFSMounts.tsx` | 103 | Fetch NFS exports for server address |
| 7 | `pages/main/updates/UpdateList.tsx` | 49 | Fetch update changelog for a package |

---

### 2.7 queryClient.invalidateQueries() — Cache Invalidation

38 total invalidation calls. See section 2.5 for the complete mapping (every `queryKey()` usage corresponds to an `invalidateQueries` call).

---

### 2.8 queryClient.removeQueries() — Cache Removal

| # | File | Line | Key | Description |
|---|------|------|-----|-------------|
| 1 | `hooks/useFileSubfolders.ts` | 97 | `linuxio.filebrowser.subfolders.queryKey()` | Clear ALL subfolder caches |
| 2 | `hooks/useFileSubfolders.ts` | 109 | `linuxio.filebrowser.subfolders.queryKey(path)` | Clear specific path cache |

---

### 2.9 useQueries() — Parallel Queries

| # | File | Line | Description |
|---|------|------|-------------|
| 1 | `hooks/useFileMultipleDirectoryDetails.ts` | 46 | Fetch multiple directory sizes in parallel via `linuxio.filebrowser.dir_size.queryOptions()` |

---

### 2.10 core.call() — Internal Transport Layer

5 usages, **all internal to `api/react-query.ts`**. These are the underlying transport that powers every typed endpoint. No consumer code uses `core.call()` directly.

`api/linuxio-core.ts` now standardizes stream completion/handler wiring through `awaitStreamResult()` and `bindStreamHandlers()` from `api/stream-helpers.ts`.

| # | Line | Context |
|---|------|---------|
| 1 | 86 | `queryFn` for `useCall()` |
| 2 | 117 | `mutationFn` for `useMutate()` |
| 3 | 279 | `.call()` implementation |
| 4 | 290 | `.queryOptions()` queryFn |
| 5 | 317 | `.useMutation()` mutationFn |

**Old-style untyped `linuxio.call()` in consumer code: 0 remaining.**

---

## 3. Streaming API (WebSocket, Bidirectional)

### Streaming Consistency Layer

To reduce duplicated `onData/onProgress/onResult/onClose` wiring, frontend stream consumers now use shared helpers from `api/stream-helpers.ts`:

- `awaitStreamResult(stream, options)` — for request-like streams that must end with a result frame.
- `bindStreamHandlers(stream, handlers)` — for long-lived streams (logs/terminal-like) that primarily consume data and close events.
- `writeStreamChunks(stream, data, options)` — for chunked write operations (editor save, compose save, uploads).

This gives one coherent lifecycle contract for:

- handler attachment and cleanup,
- abort/cancel handling,
- close-before-result error behavior for result-based streams.

### 3.1 Connection Lifecycle

| File | Line | Function | Description |
|------|------|----------|-------------|
| `contexts/AuthContext.tsx` | 162 | `initStreamMux()` | Initialize WebSocket mux on successful authentication |
| `contexts/AuthContext.tsx` | 183 | `mux.addStatusListener()` | Listen for `"error"` (session expired), `"open"` (refresh capabilities), `"closed"` (network issue) |
| `contexts/AuthContext.tsx` | 203 | `closeStreamMux()` | Close WebSocket mux on logout / unauthenticated |
| `contexts/ConfigContext.tsx` | 98 | `waitForStreamMux(250)` | Wait for mux ready before fetching config at startup |

---

### 3.2 Connection Status Hooks

#### useStreamMux() — 11 consumers

| # | File | Line | Fields Used | Purpose |
|---|------|------|-------------|---------|
| 1 | `api/react-query.ts` | 81 | `isOpen` | Gate `useCall()` queries |
| 2 | `api/react-query.ts` | 302 | `isOpen` | Gate typed `.useQuery()` |
| 3 | `hooks/useFileQueries.ts` | 31 | `isOpen` | Gate file browser queries |
| 4 | `hooks/useFileMultipleDirectoryDetails.ts` | 31 | `isOpen` | Gate multi-dir size queries |
| 5 | `pages/main/terminal/Terminal.tsx` | 30 | `isOpen, getStream` | Terminal stream connection logic |
| 6 | `pages/main/docker/TerminalDialog.tsx` | 57 | `isOpen` | Container terminal lifecycle |
| 7 | `pages/main/docker/LogsDialog.tsx` | 56 | `isOpen` | Docker log stream lifecycle |
| 8 | `pages/main/services/ServiceLogsDrawer.tsx` | 41 | `isOpen` | Service log stream lifecycle |
| 9 | `pages/main/logs/GeneralLogsPage.tsx` | 144 | `isOpen` | General log stream lifecycle |
| 10 | `components/docker/ComposeOperationDialog.tsx` | 52 | `isOpen` | Compose operation lifecycle |
| 11 | `components/docker/ReindexDialog.tsx` | 61 | `isOpen` | Reindex stream lifecycle |

#### useIsUpdating() — 4 consumers

| # | File | Line | Purpose |
|---|------|------|---------|
| 1 | `api/react-query.ts` | 82 | Disable all React Query fetching during system update |
| 2 | `api/react-query.ts` | 303 | Disable typed `.useQuery()` during update |
| 3 | `hooks/useFileQueries.ts` | 32 | Pause file browser queries during update |
| 4 | `hooks/useFileMultipleDirectoryDetails.ts` | 32 | Pause multi-directory detail queries during update |

#### isConnected() — 11 guard calls

| # | File | Line | Purpose |
|---|------|------|---------|
| 1-8 | `contexts/FileTransferContext.tsx` | 502,631,776,916,1033,1336,1491,1628 | Guard every file transfer operation |
| 9-10 | `pages/main/filebrowser/index.tsx` | 785,883 | Guard file editor save operations |
| 11 | `pages/main/docker/ComposeStacksPage.tsx` | 305 | Guard compose file save |

#### getStreamMux() — Direct access

| # | File | Line | Purpose |
|---|------|------|---------|
| 1 | `contexts/UpdateContext.tsx` | 139,151,298,389 | `setUpdating(false)` — manage pause/resume of API during updates |

---

### 3.3 Terminal Streams (Bidirectional, Persistent)

#### Host Terminal

| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/terminal/Terminal.tsx` | 138 | `openTerminalStream(cols, rows)` | Open new host terminal PTY |
| `pages/main/terminal/Terminal.tsx` | 244 | `openTerminalStream(cols, rows)` | Open fresh terminal on reset |
| `pages/main/terminal/Terminal.tsx` | 147,249 | `bindStreamHandlers(stream, { onData, onClose })` | Attach/detach terminal output handlers coherently |
| `pages/main/terminal/Terminal.tsx` | 100,172,311 | `stream.write(encodeString(text))` | Send keyboard input and pasted text |
| `pages/main/terminal/Terminal.tsx` | 162,180 | `stream.resize(cols, rows)` | Handle terminal resize |

#### Container Terminal

| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/docker/TerminalDialog.tsx` | 188 | `openContainerStream(containerId, shell, cols, rows)` | Open container shell |
| `pages/main/docker/TerminalDialog.tsx` | 193 | `bindStreamHandlers(stream, { onData, onClose })` | Attach/detach container terminal handlers coherently |
| `pages/main/docker/TerminalDialog.tsx` | 166,214,298 | `stream.write(encodeString(text))` | Send keyboard input and pasted text |
| `pages/main/docker/TerminalDialog.tsx` | 208,222 | `stream.resize(cols, rows)` | Handle terminal resize |

---

### 3.4 Log Streams (Read-only, Live)

#### Docker Container Logs

| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/docker/LogsDialog.tsx` | 101 | `openDockerLogsStream(containerId, "100")` | Initial stream (tail 100 lines) |
| `pages/main/docker/LogsDialog.tsx` | 143 | `openDockerLogsStream(containerId, "0")` | Re-enable live mode (new lines only) |
| `pages/main/docker/LogsDialog.tsx` | 112,147 | `bindStreamHandlers(stream, { onData, onClose })` | Parse log lines with standardized lifecycle |

#### Systemd Service Logs

| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/services/ServiceLogsDrawer.tsx` | 84 | `openServiceLogsStream(serviceName, "200")` | Initial stream (tail 200 lines) |
| `pages/main/services/ServiceLogsDrawer.tsx` | 126 | `openServiceLogsStream(serviceName, "0")` | Re-enable live mode |
| `pages/main/services/ServiceLogsDrawer.tsx` | 95,130 | `bindStreamHandlers(stream, { onData, onClose })` | Parse log lines with standardized lifecycle |

#### General System Logs (journalctl)

| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/logs/GeneralLogsPage.tsx` | 265 | `openGeneralLogsStream(lines, timePeriod, priority, identifier)` | Filtered log stream |
| `pages/main/logs/GeneralLogsPage.tsx` | 283 | `bindStreamHandlers(stream, { onData, onClose })` | Parse JSON log entries with standardized lifecycle |

---

### 3.5 Docker Operation Streams

#### Compose Operations (up/down/stop/restart)

| File | Line | Function | Description |
|------|------|----------|-------------|
| `components/docker/ComposeOperationDialog.tsx` | 102 | `openDockerComposeStream(action, projectName, composePath)` | Run compose operation |
| `components/docker/ComposeOperationDialog.tsx` | 114 | `bindStreamHandlers(stream, { onData, onClose })` | Parse stdout/stderr/complete/error messages |

#### Docker Reindex

| File | Line | Function | Description |
|------|------|----------|-------------|
| `components/docker/ReindexDialog.tsx` | 117 | `openDockerReindexStream()` | Reindex compose projects |
| `components/docker/ReindexDialog.tsx` | 130 | `awaitStreamResult(stream, { onProgress })` | Track progress + completion/error in one lifecycle wrapper |

---

### 3.6 File Transfer Streams

All file transfer streams are managed by `contexts/FileTransferContext.tsx`.

#### Upload

| Line | Function | Description |
|------|----------|-------------|
| 982 | `openFileUploadStream(targetPath, file.size)` | Upload file with chunked streaming |
| 1042 | `bindStreamHandlers(stream, { onProgress, onResult, onClose })` | Unified progress/result/close handling |
| 1033-1048 | `STREAM_CHUNK_SIZE` + `UPLOAD_WINDOW_SIZE` | Flow-controlled chunking and backpressure |

Also used for file saves:
| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/filebrowser/index.tsx` | 785 | `openFileUploadStream(path, contentBytes.length)` | Save file editor content |
| `pages/main/filebrowser/index.tsx` | 790,795 | `awaitStreamResult` + `writeStreamChunks` | Coherent save completion + chunk writes |
| `pages/main/docker/ComposeStacksPage.tsx` | 315 | `openFileUploadStream(filePath, contentSize, override)` | Save compose file |
| `pages/main/docker/ComposeStacksPage.tsx` | 322,327 | `awaitStreamResult` + `writeStreamChunks` | Coherent save completion + chunk writes |

#### Download

| Line | Function | Description |
|------|----------|-------------|
| 339 | `openFileDownloadStream(paths)` | Download single file or multi-file zip |
| 352 | `awaitStreamResult(stream, { onData, onProgress })` | Track bytes/total/pct + completion in one lifecycle wrapper |

#### Compress

| Line | Function | Description |
|------|----------|-------------|
| 590 | `openFileCompressStream(paths, fullDestination, format)` | Create archive |
| 600 | `awaitStreamResult(stream, { onProgress })` | Track progress + completion in one lifecycle wrapper |

#### Extract

| Line | Function | Description |
|------|----------|-------------|
| 728 | `openFileExtractStream(archivePath, destination)` | Extract archive |
| 738 | `awaitStreamResult(stream, { onProgress })` | Track progress + completion in one lifecycle wrapper |

#### Reindex

| Line | Function | Description |
|------|----------|-------------|
| 867 | `openFileReindexStream(path)` | Reindex directory |
| 877 | `awaitStreamResult(stream, { onProgress })` | Track progress + completion in one lifecycle wrapper |

#### Copy

| Line | Function | Description |
|------|----------|-------------|
| 1441 | `openFileCopyStream(source, destination)` | Copy files/directories |
| 1453 | `awaitStreamResult(stream, { onProgress })` | Track progress + completion in one lifecycle wrapper |

#### Move

| Line | Function | Description |
|------|----------|-------------|
| 1576 | `openFileMoveStream(source, destination)` | Move files/directories |
| 1588 | `awaitStreamResult(stream, { onProgress })` | Track progress + completion in one lifecycle wrapper |

---

### 3.7 System Update Streams

| File | Line | Function | Description |
|------|------|----------|-------------|
| `contexts/UpdateContext.tsx` | 416 | `openExecStream("bash", ["-c", cmd])` | Run system update script |
| `contexts/UpdateContext.tsx` | 467 | `bindStreamHandlers(stream, { onData, onResult, onClose })` | Parse update output and finalize stream coherently |
| `contexts/UpdateContext.tsx` | 139,151,298,389 | `getStreamMux()?.setUpdating(true/false)` | Pause/resume all API requests during update |

---

### 3.8 Package Update Streams

| File | Line | Function | Description |
|------|------|----------|-------------|
| `hooks/usePackageUpdater.ts` | 122 | `openPackageUpdateStream(packages)` | Stream bulk package update |
| `hooks/usePackageUpdater.ts` | 138 | `awaitStreamResult(stream, { onProgress })` | Track per-package and overall progress with coherent completion |
| `hooks/usePackageUpdater.ts` | 205 | `stream.abort()` | Cancel in-progress package update |

---

### 3.9 Storage Streams (SMART Tests)

| File | Line | Function | Description |
|------|------|----------|-------------|
| `pages/main/storage/DiskOverview/index.tsx` | 107 | `openSmartTestStream(rawDrive.name, testType)` | Start SMART self-test |
| `pages/main/storage/DiskOverview/index.tsx` | 130 | `awaitStreamResult(stream, { onProgress })` | Track test status, progress, and final status in one lifecycle wrapper |

---

### 3.10 Stream Lifecycle Primitives

| Primitive | Purpose | Current Usage Pattern |
|-----------|---------|-----------------------|
| `bindStreamHandlers(stream, handlers)` | Attach coherent `onData/onProgress/onResult/onClose` callbacks with one cleanup function | Long-lived/interactive streams (terminal, logs, compose output, update stream, upload flow-control path) |
| `awaitStreamResult(stream, options)` | Result-oriented stream completion with normalized close/abort/error behavior | Task streams (download, compress/extract/reindex/copy/move, package update, SMART test, docker reindex, core `call`) |
| `writeStreamChunks(stream, data, options)` | Standardized chunked writes with optional yielding and close-on-end | Editor save (`filebrowser/index.tsx`) and compose save (`ComposeStacksPage.tsx`) |

**Current coherence status:** direct `stream.onData/onProgress/onResult/onClose` assignments in app consumer code: **0** (kept only inside API internals and helper implementation).

---

### 3.11 String Encoding/Decoding

`encodeString(text)` — converts string to `Uint8Array` for stream writing.
`decodeString(data)` — converts `Uint8Array` to string for display.

| Consumer | encode | decode | Purpose |
|----------|--------|--------|---------|
| `Terminal.tsx` | keyboard input, paste | terminal output | Host terminal I/O |
| `TerminalDialog.tsx` | keyboard input, paste | container output | Container terminal I/O |
| `LogsDialog.tsx` | — | log data | Docker logs rendering |
| `ServiceLogsDrawer.tsx` | — | log data | Service logs rendering |
| `GeneralLogsPage.tsx` | — | JSON entries | System logs parsing |
| `ComposeOperationDialog.tsx` | — | output data | Compose operation output |
| `UpdateContext.tsx` | — | update output | System update script output |
| `linuxio.ts` (internal) | all payloads | — | Payload builder encoding |
| `linuxio-core.ts` (internal) | bridge payloads | — | Bridge command encoding |

---

### 3.12 Flow Control Constants

| Constant | Value | Used In | Purpose |
|----------|-------|---------|---------|
| `STREAM_CHUNK_SIZE` | 1MB (`1 * 1024 * 1024`) | FileTransferContext, filebrowser/index, ComposeStacksPage | Maximum bytes per write call |
| `UPLOAD_WINDOW_SIZE` | 4MB (`4 * 1024 * 1024`) | FileTransferContext | Max bytes in-flight before backpressure |

---

## 4. Summary Statistics

### JSON API

Counts below are concrete code call sites (API doc-comment examples excluded).

| Category | Count |
|----------|-------|
| `.useQuery()` hooks | 52 |
| `.useMutation()` hooks | 58 |
| `.call()` imperative | 4 |
| `.queryOptions()` | 8 |
| `.queryKey()` | 41 |
| `queryClient.fetchQuery()` | 7 |
| `queryClient.invalidateQueries()` | 38 |
| `queryClient.removeQueries()` | 2 |
| `useQueries()` | 1 |
| `core.call()` (internal only) | 5 |
| Old-style untyped `linuxio.call()` | **0** |
| **Total JSON API touchpoints** | **216** |

### Streaming API

| Category | Count |
|----------|-------|
| Stream open functions (16 types) | 22 invocations |
| `useStreamMux()` consumers | 11 |
| `useIsUpdating()` consumers | 4 |
| `isConnected()` guards | 11 |
| `awaitStreamResult()` call sites | 12 |
| `bindStreamHandlers()` call sites | 12 |
| `writeStreamChunks()` call sites | 2 |
| Direct stream handler assignment in consumers | 0 |
| `encodeString()` calls (consumer) | 6 |
| `decodeString()` calls (consumer) | 10 |
| **Total streaming touchpoints** | **90** |

### By Domain (Pattern View)

| Domain | Dominant JSON Pattern | Dominant Streaming Pattern |
|--------|------------------------|----------------------------|
| `system` | Read-heavy `useQuery` polling | None |
| `storage` | Balanced queries + mutators | SMART test task stream |
| `docker` | Mixed query/mutation control plane | logs, compose/reindex task streams |
| `dbus` | Mutation-heavy service/network control | None |
| `filebrowser` | Query + mutation + imperative validation calls | highest stream diversity (upload/download/archive/copy/move/reindex) |
| `accounts` | Query + mutation forms | None |
| `wireguard` | Query + mutation control plane | None |
| `terminal` | Shell discovery query | interactive bidirectional terminal streams |
| `modules` | query-only route/sidebar hydration | None |
| `config` | one query + one mutation | None |
| `control` | single version query | None |
| `exec` | None | package/system update execution streams |

---

## 5. Complete API Command Inventory

### system
`get_capabilities`, `get_cpu_info`, `get_sensor_info`, `get_motherboard_info`, `get_memory_info`, `get_gpu_info`, `get_fs_info`, `get_network_info`, `get_processes`, `get_host_info`, `get_updates_fast`

### storage
`get_drive_info`, `list_nfs_mounts`, `list_nfs_exports`, `mount_nfs`, `unmount_nfs`, `remount_nfs`, `list_pvs`, `list_vgs`, `list_lvs`, `create_lv`, `resize_lv`, `delete_lv`, `run_smart_test`

### docker
`list_containers`, `list_networks`, `list_volumes`, `list_images`, `list_compose_projects`, `get_icon_uri`, `start_container`, `stop_container`, `restart_container`, `remove_container`, `create_network`, `delete_network`, `delete_volume`, `delete_image`, `delete_stack`, `validate_stack_directory`, `validate_compose`, `get_compose_file_path`

### dbus
`reboot`, `power_off`, `get_updates`, `get_updates_basic`, `get_update_detail`, `install_package`, `get_auto_updates`, `set_auto_updates`, `apply_offline_updates`, `get_update_history`, `list_services`, `get_service_info`, `get_service_logs`, `start_service`, `stop_service`, `restart_service`, `reload_service`, `enable_service`, `disable_service`, `mask_service`, `unmask_service`, `get_network_info`, `set_ipv4_manual`, `set_ipv4`, `set_ipv6`, `set_mtu`, `enable_connection`, `disable_connection`

### filebrowser
`resource_get`, `resource_stat`, `resource_post`, `resource_delete`, `resource_patch`, `chmod`, `search`, `subfolders`, `dir_size`, `users_groups`

### accounts
`list_users`, `list_groups`, `list_shells`, `create_user`, `create_group`, `delete_user`, `delete_group`, `modify_user`, `modify_group_members`, `change_password`, `lock_user`, `unlock_user`

### wireguard
`list_interfaces`, `list_peers`, `add_interface`, `remove_interface`, `add_peer`, `remove_peer`, `up_interface`, `down_interface`, `enable_interface`, `disable_interface`, `peer_config_download`, `peer_qrcode`

### terminal
`list_shells`

### modules
`get_modules`

### config
`get`, `set`

### control
`version`

---

## 6. Coherence & Patterns Analysis

### 6.1 Current Coherence Status (2026-02-16)

- Streaming helper primitives are now the default path:
  - `awaitStreamResult(...)` for result-oriented operations.
  - `bindStreamHandlers(...)` for long-lived interactive/log streams.
  - `writeStreamChunks(...)` for chunked writes.
- Direct `stream.onData/onProgress/onResult/onClose` assignment in app consumer code: **0**.
- Core transport (`api/linuxio-core.ts`) also follows helper primitives for `call()` and spawn lifecycle binding.

### 6.2 Usage Patterns

| Pattern | Primary Primitive | Typical Domains |
|---------|-------------------|-----------------|
| Result-oriented task stream | `awaitStreamResult` | file transfer ops, package updates, SMART tests, docker reindex, core bridge calls |
| Long-lived interactive stream | `bindStreamHandlers` | host/container terminals, logs, compose output, update streaming |
| Chunked writer | `writeStreamChunks` + `awaitStreamResult` | file editor save, compose file save |
| Flow-controlled upload | `bindStreamHandlers` + `STREAM_CHUNK_SIZE`/`UPLOAD_WINDOW_SIZE` | `FileTransferContext` upload path |

### 6.3 Residual Divergences (Intentional)

- `SpawnedProcess.onStream()` / `.progress()` in `api/linuxio-core.ts` still mutate handler fields directly. This is kept for fluent API compatibility.
- Upload flow-control uses a custom send-window loop (`UPLOAD_WINDOW_SIZE`) and therefore uses `bindStreamHandlers` instead of plain `awaitStreamResult`.

### 6.4 Recommended Guardrails

1. For any new result-based stream operation, use `awaitStreamResult` first and only add custom handlers through its options.
2. For any new live/interactive stream, bind callbacks via `bindStreamHandlers` and always store/clear the unbind function on cleanup.
3. For new chunked save/upload paths, use `writeStreamChunks` unless explicit backpressure logic is required.
4. Keep string codec boundaries explicit: `encodeString` on input writes, `decodeString` on render/parsing boundaries.

---

*Report updated on 2026-02-16 from branch `dev/v0.8.0` using current `frontend/src` static usage scan.*
