# LinuxIO Module System

## Overview

A complete module system allowing users to extend LinuxIO with custom functionality packaged as self-contained modules. Each module includes:
- **Manifest** (YAML) - metadata, permissions, commands
- **Frontend** - React components/pages
- **Backend** - Generic handlers (shell/dbus) or custom Go code

## Module Structure

### Directory Layout

```
/etc/linuxio/modules/           # System modules (require root)
~/.config/linuxio/modules/      # User modules
  └── monitoring/               # Module name
      ├── module.yaml           # Manifest
      ├── frontend/             # Frontend code
      │   ├── index.tsx         # Main component/page
      │   ├── components/       # Sub-components
      │   └── package.json      # Dependencies (optional)
      └── backend/              # Backend (optional)
          ├── handlers.go       # Custom Go handlers
          └── scripts/          # Shell scripts
```

### Module Manifest (`module.yaml`)

```yaml
# Module metadata
name: monitoring
version: 1.0.0
title: System Monitoring
description: Advanced system monitoring with custom metrics
author: Your Name
homepage: https://github.com/yourname/linuxio-monitoring
license: MIT

# UI integration
ui:
  route: /monitoring           # URL path
  icon: activity               # Lucide icon or iconify icon
  sidebar:
    enabled: true
    position: 5                # Position in sidebar (optional)
    section: monitoring        # Group in sidebar (optional)

# Backend handlers
handlers:
  # Generic shell commands
  commands:
    check_disk:
      description: Check disk usage
      command: "df -h {{.path}} | tail -1 | awk '{print $5}'"
      timeout: 5
      args:
        - name: path
          type: string
          required: false
          default: "/"
      returns:
        type: string
        description: Disk usage percentage

    ping_host:
      description: Ping host and return latency
      command: "ping -c 1 {{.host}} | grep 'time=' | sed 's/.*time=\\([0-9.]*\\).*/\\1/'"
      timeout: 10
      args:
        - name: host
          type: string
          required: true
      returns:
        type: number
        description: Latency in milliseconds

  # Generic DBus calls
  dbus:
    get_brightness:
      description: Get screen brightness
      bus: system
      destination: org.freedesktop.UPower
      path: /org/freedesktop/UPower/devices/DisplayDevice
      interface: org.freedesktop.DBus.Properties
      method: Get
      args:
        - org.freedesktop.UPower.Device
        - Percentage

  # Custom Go handlers (optional)
  custom:
    - name: analyze_logs
      description: Analyze system logs
      # Implemented in backend/handlers.go

# Permissions
permissions:
  - shell_exec      # Execute shell commands
  - dbus_system     # Access system DBus
  - dbus_session    # Access session DBus
  - network         # Make network requests
  - filesystem_read # Read files
  - filesystem_write # Write files
  - sudo            # Execute sudo commands (requires user auth)

# Frontend dependencies (optional)
dependencies:
  npm:
    - recharts: "^2.5.0"
    - date-fns: "^2.30.0"

# Module settings (configurable by user)
settings:
  - name: refresh_interval
    type: number
    default: 5000
    description: Data refresh interval in milliseconds
    min: 1000
    max: 60000

  - name: monitored_paths
    type: array
    default: ["/", "/home"]
    description: Paths to monitor

  - name: alert_threshold
    type: number
    default: 80
    description: Alert when usage exceeds this percentage
```

## Frontend Integration

### Module Entry Point (`frontend/index.tsx`)

```tsx
import React from 'react';
import { linuxio } from '@/api';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { useModuleConfig } from '@/hooks/useModuleConfig';

// Module receives configuration and linuxio API
interface MonitoringModuleProps {
  config: {
    refresh_interval: number;
    monitored_paths: string[];
    alert_threshold: number;
  };
}

export default function MonitoringModule({ config }: MonitoringModuleProps) {
  // Use module-specific handlers
  const { data: diskUsage } = linuxio.useCall(
    "module.monitoring",  // Auto-prefixed namespace
    "check_disk",
    ["/"],
    { refetchInterval: config.refresh_interval }
  );

  return (
    <div>
      <h1>System Monitoring</h1>
      <Card>
        <CardHeader>Disk Usage</CardHeader>
        <CardContent>
          {diskUsage && <div>{diskUsage.stdout}</div>}
        </CardContent>
      </Card>
    </div>
  );
}

// Export module metadata (optional)
export const metadata = {
  name: 'monitoring',
  version: '1.0.0',
};
```

### Auto-Generated API Types

The backend generates TypeScript types from the manifest:

```tsx
// Auto-generated: frontend/src/api/modules/monitoring.ts
export const monitoringHandlers = {
  checkDisk: (path: string = "/") =>
    linuxio.useCall<{ stdout: string }>(
      "module.monitoring",
      "check_disk",
      [path]
    ),

  pingHost: (host: string) =>
    linuxio.useCall<number>(
      "module.monitoring",
      "ping_host",
      [host]
    ),

  getBrightness: () =>
    linuxio.useCall<number>(
      "module.monitoring",
      "get_brightness",
      []
    ),
};

// Usage in components
import { monitoringHandlers } from '@/api/modules/monitoring';

const { data: diskUsage } = monitoringHandlers.checkDisk("/home");
```

## Backend Integration

### Module Handler Registration

**Backend:** `backend/bridge/handlers/modules/loader.go`

```go
package modules

import (
    "os"
    "path/filepath"
    "gopkg.in/yaml.v3"
    "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
)

type ModuleManifest struct {
    Name        string                  `yaml:"name"`
    Version     string                  `yaml:"version"`
    Title       string                  `yaml:"title"`
    Description string                  `yaml:"description"`
    Handlers    ModuleHandlers          `yaml:"handlers"`
    Permissions []string                `yaml:"permissions"`
}

type ModuleHandlers struct {
    Commands map[string]CommandHandler `yaml:"commands"`
    Dbus     map[string]DbusHandler    `yaml:"dbus"`
    Custom   []CustomHandler           `yaml:"custom"`
}

type CommandHandler struct {
    Description string                 `yaml:"description"`
    Command     string                 `yaml:"command"`
    Timeout     int                    `yaml:"timeout"`
    Args        []HandlerArg           `yaml:"args"`
}

// LoadModules discovers and registers all modules
func LoadModules(handlerRegistry map[string]map[string]func([]string) (any, error)) error {
    // Load from system directory
    systemModules, _ := loadModulesFromDir("/etc/linuxio/modules")

    // Load from user directory
    userHome := os.Getenv("HOME")
    userModules, _ := loadModulesFromDir(filepath.Join(userHome, ".config/linuxio/modules"))

    // Merge modules (user overrides system)
    modules := mergeModules(systemModules, userModules)

    // Register each module
    for _, module := range modules {
        if err := registerModule(module, handlerRegistry); err != nil {
            log.Printf("Failed to load module %s: %v", module.Name, err)
            continue
        }
    }

    return nil
}

func registerModule(module *ModuleManifest, registry map[string]map[string]func([]string) (any, error)) error {
    namespace := "module." + module.Name
    registry[namespace] = make(map[string]func([]string) (any, error))

    // Register shell command handlers
    for name, cmd := range module.Handlers.Commands {
        registry[namespace][name] = createCommandHandler(cmd)
    }

    // Register DBus handlers
    for name, dbus := range module.Handlers.Dbus {
        registry[namespace][name] = createDbusHandler(dbus)
    }

    return nil
}

func createCommandHandler(cmd CommandHandler) func([]string) (any, error) {
    return func(args []string) (any, error) {
        // Template substitution: {{.arg0}}, {{.arg1}}, etc.
        command := cmd.Command
        for i, arg := range args {
            placeholder := fmt.Sprintf("{{.arg%d}}", i)
            command = strings.ReplaceAll(command, placeholder, arg)
        }

        // Use generic command executor
        timeout := cmd.Timeout
        if timeout == 0 {
            timeout = 10
        }

        return generic.ExecCommand([]string{command, strconv.Itoa(timeout)})
    }
}
```

### Modified `register.go`

```go
func RegisterAllHandlers(shutdownChan chan string, sess *session.Session) {
    // Existing handlers
    HandlersByType["dbus"] = dbus.DbusHandlers()
    HandlersByType["system"] = system.SystemHandlers()
    // ... etc

    // Generic handlers
    HandlersByType["command"] = generic.CommandHandlers()
    HandlersByType["generic_dbus"] = generic.DbusHandlers()

    // NEW: Load modules
    if err := modules.LoadModules(HandlersByType); err != nil {
        log.Printf("Failed to load modules: %v", err)
    }
}
```

## Frontend Module Discovery

### Module Registry API

**New backend endpoint:** `GET /api/modules`

```go
// Returns list of available modules
type ModuleInfo struct {
    Name        string   `json:"name"`
    Version     string   `json:"version"`
    Title       string   `json:"title"`
    Description string   `json:"description"`
    Route       string   `json:"route"`
    Icon        string   `json:"icon"`
    Enabled     bool     `json:"enabled"`
}

func ListModules() ([]ModuleInfo, error) {
    // Return all loaded modules
}
```

### Dynamic Route Registration

**Frontend:** `frontend/src/routes/modules.tsx`

```tsx
import { lazy, useEffect, useState } from 'react';
import { RouteObject } from 'react-router-dom';

// Fetch available modules at startup
export function useModuleRoutes(): RouteObject[] {
  const [routes, setRoutes] = useState<RouteObject[]>([]);

  useEffect(() => {
    fetch('/api/modules')
      .then(res => res.json())
      .then((modules: ModuleInfo[]) => {
        const moduleRoutes = modules.map(module => ({
          path: module.route,
          element: lazy(() =>
            import(`@/modules/${module.name}/frontend/index.tsx`)
          ),
        }));
        setRoutes(moduleRoutes);
      });
  }, []);

  return routes;
}
```

### Dynamic Sidebar Items

**Frontend:** `frontend/src/components/sidebar/SidebarItems.tsx`

```tsx
import { useModules } from '@/hooks/useModules';

function SidebarItems() {
  const { modules } = useModules();

  const staticItems = [
    { href: "/", icon: Home, title: "Dashboard" },
    { href: "/network", icon: Network, title: "Network" },
    // ... existing items
  ];

  const moduleItems = modules
    .filter(m => m.ui.sidebar.enabled)
    .map(m => ({
      href: m.ui.route,
      icon: getIcon(m.ui.icon),
      title: m.title,
    }));

  return [...staticItems, ...moduleItems];
}
```

## Module Installation

### Via CLI

```bash
# Install from directory
linuxio module install ./my-module

# Install from URL
linuxio module install https://github.com/user/linuxio-module-monitoring

# List installed modules
linuxio module list

# Enable/disable module
linuxio module enable monitoring
linuxio module disable monitoring

# Remove module
linuxio module remove monitoring
```

### Module Validation

Before installation, validate:
1. **Manifest schema** - Check YAML is valid
2. **Permissions** - Warn about dangerous permissions (sudo, filesystem_write)
3. **Dependencies** - Check if npm packages are available
4. **Name conflicts** - Ensure module name doesn't conflict
5. **Frontend code** - Basic TypeScript/JSX validation

## Security Model

### Permission System

Modules declare required permissions in manifest. Users approve on install.

```yaml
permissions:
  - shell_exec       # Execute shell commands
  - dbus_system      # System DBus access
  - filesystem_read  # Read files
  - network          # HTTP requests
  - sudo             # Requires re-auth for each sudo command
```

### Sandboxing (Future)

- Execute module commands in restricted shell
- Limit filesystem access to specific directories
- Rate limiting on API calls
- Resource limits (CPU, memory)

## Module Marketplace (Future)

### Module Repository

```yaml
# module-registry.yaml
modules:
  - name: monitoring
    title: System Monitoring
    description: Advanced system monitoring dashboard
    author: community
    repo: https://github.com/linuxio-modules/monitoring
    downloads: 1250
    rating: 4.8
    tags: [monitoring, dashboard, metrics]
```

### Discovery in UI

```tsx
// Module Store page
function ModuleStore() {
  const { data: availableModules } = useQuery(['module-store'],
    () => fetch('https://modules.linuxio.org/registry.json')
  );

  return (
    <div>
      <h1>Module Store</h1>
      {availableModules.map(module => (
        <ModuleCard
          module={module}
          onInstall={() => installModule(module.repo)}
        />
      ))}
    </div>
  );
}
```

## Example Modules

### 1. System Monitoring Module

```yaml
name: monitoring
title: Advanced Monitoring
handlers:
  commands:
    cpu_temp:
      command: "sensors | grep 'Core 0' | awk '{print $3}'"
    network_speed:
      command: "cat /proc/net/dev | grep eth0 | awk '{print $2, $10}'"
```

### 2. Backup Module

```yaml
name: backup
title: Backup Manager
handlers:
  commands:
    create_backup:
      command: "tar -czf /backup/{{.name}}.tar.gz {{.path}}"
      timeout: 300
    list_backups:
      command: "ls -lh /backup/*.tar.gz"
```

### 3. Database Management Module

```yaml
name: database
title: Database Manager
handlers:
  commands:
    mysql_status:
      command: "systemctl is-active mysql"
    postgres_status:
      command: "systemctl is-active postgresql"
    query_db:
      command: "mysql -u{{.user}} -p{{.pass}} -e '{{.query}}' {{.db}}"
permissions:
  - shell_exec
  - filesystem_read
```

## Implementation Roadmap

### Phase 1: Core Infrastructure ✅
- [x] Generic command handler
- [x] Generic DBus handler
- [x] Documentation

### Phase 2: Module Loading (Current)
- [ ] Module manifest parser (YAML)
- [ ] Module loader (backend)
- [ ] Dynamic handler registration
- [ ] Module discovery API

### Phase 3: Frontend Integration
- [ ] Dynamic route loading
- [ ] Dynamic sidebar items
- [ ] Module settings UI
- [ ] Module API type generation

### Phase 4: Module Management
- [ ] CLI for module installation
- [ ] Module validation
- [ ] Enable/disable modules
- [ ] Module updates

### Phase 5: Marketplace
- [ ] Module registry
- [ ] Module store UI
- [ ] Installation from URL
- [ ] Ratings and reviews

## Benefits

✅ **Extensibility** - Add functionality without forking
✅ **Modularity** - Clean separation of concerns
✅ **Community** - Share and discover modules
✅ **Safety** - Permission system and validation
✅ **Simplicity** - YAML manifest + React components

## Files to Create

### Backend
- `backend/bridge/handlers/modules/loader.go` - Module loading
- `backend/bridge/handlers/modules/manifest.go` - Manifest types
- `backend/bridge/handlers/modules/validator.go` - Module validation
- `backend/webserver/handlers/modules.go` - Module API endpoints

### Frontend
- `frontend/src/hooks/useModules.ts` - Module discovery hook
- `frontend/src/api/modules/` - Auto-generated module APIs
- `frontend/src/pages/modules/` - Module management UI

### CLI
- `backend/cmd/linuxio-module/main.go` - Module CLI tool

This creates a complete, extensible module system similar to VS Code extensions or browser add-ons!

## Related Documentation

- [Bridge Module System](./bridge-modules.md) - Complete guide to using the YAML-based module system
- [Frontend API](./frontendAPI.md) - Frontend usage patterns
- [Bridge Handler API](./bridge-handler-api.md) - Bridge architecture
