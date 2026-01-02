# Bridge Module System

## Overview

LinuxIO features a YAML-based module system that enables extending bridge functionality without modifying core code. This system provides:

- **Security-first design**: Commands must be whitelisted in YAML manifests
- **Cockpit-style architecture**: Generic command and DBus execution
- **Zero backend changes**: Add features by creating YAML files
- **Type-safe contracts**: Define arguments, timeouts, and return types
- **Module discovery**: Automatic loading from system and user directories

## Architecture

### Security Model

The module system uses a **whitelist-only** approach:

1. **Direct execution is DISABLED**: Generic handlers reject all direct calls from frontend
2. **YAML-based whitelisting**: Only commands defined in `module.yaml` can execute
3. **Command registry**: Runtime validation against registered modules
4. **Template substitution**: Safe argument injection via placeholders

```
┌─────────────┐
│   Frontend  │
│  useCall()  │
└──────┬──────┘
       │ request("module.monitoring", "check_disk", ["/"])
       ▼
┌─────────────────┐
│  Bridge Handler │ ──► Check if "monitoring:check_disk" is whitelisted
│     Registry    │     in CommandRegistry
└────────┬────────┘
         │ ✓ Found in whitelist
         ▼
┌─────────────────┐
│ Generic Handler │ ──► Execute: df -h {{.path}}
│ ExecDirect()    │     with template substitution
└─────────────────┘
```

### Components

- **Module Manifest** (`module.yaml`): Defines commands, DBus calls, UI config
- **Command Registry**: In-memory whitelist of allowed operations
- **Module Loader**: Scans directories and registers handlers
- **Generic Handlers**: Execute whitelisted commands/DBus calls

## Creating Modules

### Directory Structure

Modules are discovered in two locations:

```
/etc/linuxio/modules/              # System-wide modules
└── monitoring/
    └── module.yaml

~/.config/linuxio/modules/         # User modules (override system)
└── custom-monitoring/
    └── module.yaml
```

User modules with the same name override system modules.

### Module Manifest Format

**File:** `~/.config/linuxio/modules/monitoring/module.yaml`

```yaml
name: monitoring
version: 1.0.0
title: System Monitoring
description: Monitor system resources and services

ui:
  route: /monitoring
  icon: activity
  sidebar:
    enabled: true
    position: 5

handlers:
  # Shell command handlers
  commands:
    check_disk:
      description: Check disk usage for a path
      command: "df -h {{.path}} | tail -1 | awk '{print $5}'"
      timeout: 5
      args:
        - name: path
          type: string
          required: false
          default: "/"
      returns:
        type: object
        schema:
          exitCode: number
          stdout: string

    cpu_temperature:
      description: Get CPU temperature from sensors
      command: "sensors 2>/dev/null | grep 'Core 0' | awk '{print $3}'"
      timeout: 5
      returns:
        type: string

    service_status:
      description: Check systemd service status
      command: "systemctl is-active {{.service}}"
      timeout: 3
      args:
        - name: service
          type: string
          required: true

  # DBus call handlers
  dbus:
    system_uptime:
      description: Get system uptime via DBus
      bus: system
      destination: org.freedesktop.systemd1
      path: /org/freedesktop/systemd1
      interface: org.freedesktop.DBus.Properties
      method: Get
      args:
        - org.freedesktop.systemd1.Manager
        - UserspaceTimestampMonotonic

    reboot:
      description: Reboot system via DBus
      bus: system
      destination: org.freedesktop.login1
      path: /org/freedesktop/login1
      interface: org.freedesktop.login1.Manager
      method: Reboot
      args:
        - "true"

permissions:
  - shell_exec
  - dbus_system

settings:
  - key: refresh_interval
    title: Refresh Interval
    type: number
    default: 5000
    description: How often to refresh metrics (ms)
```

### Manifest Schema

#### Top-Level Fields

- **`name`** (required): Unique module identifier (alphanumeric, hyphens, underscores)
- **`version`** (required): Semantic version (e.g., `1.0.0`)
- **`title`** (required): Human-readable name
- **`description`**: Module description
- **`ui`**: Frontend integration config
- **`handlers`**: Command and DBus handlers
- **`permissions`**: Required permissions
- **`settings`**: User-configurable settings

#### UI Configuration

```yaml
ui:
  route: /monitoring          # Frontend route
  icon: activity              # Lucide icon name
  sidebar:
    enabled: true             # Show in sidebar
    position: 5               # Sidebar order (lower = higher)
```

#### Command Handler

```yaml
handlers:
  commands:
    command_name:
      description: What this command does
      command: "shell command with {{.placeholder}}"
      timeout: 10             # Seconds (default: 10)
      args:
        - name: placeholder   # Matches {{.placeholder}}
          type: string
          required: true
          default: "value"
      returns:
        type: object          # object | string | number
        schema:               # TypeScript-like schema
          key: type
```

**Template Placeholders:**
- **Named**: `{{.path}}`, `{{.service}}` - matches `args[].name`
- **Numeric**: `{{.arg0}}`, `{{.arg1}}` - matches argument position

#### DBus Handler

```yaml
handlers:
  dbus:
    handler_name:
      description: What this does
      bus: system             # "system" or "session"
      destination: org.freedesktop.login1
      path: /org/freedesktop/login1
      interface: org.freedesktop.login1.Manager
      method: Reboot
      args:                   # Static args passed to method
        - "true"
```

## Frontend Integration

### Using Module Handlers

Once a module is loaded, its handlers are available under the `module.{name}` namespace:

```tsx
import { useCall } from '@/api/linuxio';

export function MonitoringWidget() {
  // Call module handler: module.monitoring:check_disk
  const { data: diskUsage } = useCall(
    'module.monitoring',
    'check_disk',
    ['/home'],
    { refetchInterval: 5000 }
  );

  // Call module handler: module.monitoring:cpu_temperature
  const { data: cpuTemp } = useCall(
    'module.monitoring',
    'cpu_temperature',
    []
  );

  // Call module handler: module.monitoring:service_status
  const { data: nginxStatus } = useCall(
    'module.monitoring',
    'service_status',
    ['nginx']
  );

  return (
    <div>
      <p>Disk: {diskUsage?.stdout}</p>
      <p>CPU: {cpuTemp?.stdout}</p>
      <p>Nginx: {nginxStatus?.stdout}</p>
    </div>
  );
}
```

### Type-Safe Wrapper (Recommended)

Create a type-safe API wrapper for better developer experience:

**File:** `frontend/src/api/modules/monitoring.ts`

```tsx
import { linuxio } from '../linuxio';

export const monitoring = {
  /**
   * Check disk usage for a path
   */
  async checkDisk(path: string = '/'): Promise<string> {
    const { data } = await linuxio.request(
      'module.monitoring',
      'check_disk',
      [path]
    );
    return data.stdout.trim();
  },

  /**
   * Get CPU temperature
   */
  async getCpuTemp(): Promise<string> {
    const { data } = await linuxio.request(
      'module.monitoring',
      'cpu_temperature',
      []
    );
    return data.stdout.trim();
  },

  /**
   * Check if a service is active
   */
  async isServiceActive(service: string): Promise<boolean> {
    const { data } = await linuxio.request(
      'module.monitoring',
      'service_status',
      [service]
    );
    return data.stdout.trim() === 'active';
  },

  /**
   * Reboot system
   */
  async reboot(): Promise<void> {
    await linuxio.request('module.monitoring', 'reboot', []);
  },
};
```

**Usage:**

```tsx
import { monitoring } from '@/api/modules/monitoring';

// Type-safe, autocomplete-friendly
const diskUsage = await monitoring.checkDisk('/home');
const isNginxRunning = await monitoring.isServiceActive('nginx');
```

### React Query Hooks

```tsx
import { useQuery } from '@tanstack/react-query';
import { monitoring } from '@/api/modules/monitoring';

export function useMonitoring() {
  const { data: diskUsage } = useQuery({
    queryKey: ['monitoring', 'disk', '/'],
    queryFn: () => monitoring.checkDisk('/'),
    refetchInterval: 5000,
  });

  const { data: cpuTemp } = useQuery({
    queryKey: ['monitoring', 'cpu'],
    queryFn: () => monitoring.getCpuTemp(),
    refetchInterval: 2000,
  });

  return { diskUsage, cpuTemp };
}
```

## Security Considerations

### Whitelist-Only Execution

The module system prevents arbitrary command execution:

```tsx
// ❌ BLOCKED - Direct command execution is disabled
await linuxio.request('command', 'exec', ['rm -rf /']);
// Error: "direct command execution is disabled - commands must be defined in module YAML files"

// ❌ BLOCKED - Direct DBus calls are disabled
await linuxio.request('generic_dbus', 'call', [...]);
// Error: "direct DBus calls are disabled - calls must be defined in module YAML files"

// ✅ ALLOWED - Command is whitelisted in monitoring module
await linuxio.request('module.monitoring', 'check_disk', ['/']);
// Executes: df -h / | tail -1 | awk '{print $5}'
```

### Security Best Practices

1. **Minimize Command Scope**: Only whitelist necessary commands
2. **Validate Arguments**: Use template placeholders, not string concatenation
3. **Limit Timeouts**: Set appropriate timeouts (default: 10s)
4. **Principle of Least Privilege**: Don't use `sudo` unless necessary
5. **Review Modules**: Audit YAML files before deployment

### Safe vs Unsafe Patterns

**✅ SAFE - Template placeholders:**

```yaml
# YAML manifest
command: "df -h {{.path}}"
```

The module loader substitutes `{{.path}}` with the argument value. Frontend passes:

```tsx
await linuxio.request('module.monitoring', 'check_disk', ['/home']);
```

**❌ UNSAFE - If you were concatenating in frontend** (Don't do this with raw commands):

```tsx
// This is why generic handlers are disabled!
const userPath = getUserInput(); // Could be: "; rm -rf /"
await linuxio.request('command', 'exec', [`df -h ${userPath}`]);
```

With the module system, the frontend can't execute arbitrary commands - it can only call pre-defined handlers with validated templates.

## Module Development Workflow

### 1. Create Module Directory

```bash
mkdir -p ~/.config/linuxio/modules/mymodule
```

### 2. Write Manifest

**File:** `~/.config/linuxio/modules/mymodule/module.yaml`

```yaml
name: mymodule
version: 1.0.0
title: My Custom Module

handlers:
  commands:
    hello:
      command: "echo 'Hello {{.name}}'"
      args:
        - name: name
          type: string
          default: "World"
```

### 3. Restart LinuxIO Backend

```bash
sudo systemctl restart linuxio
```

The module loader scans directories on startup and registers handlers.

### 4. Test from Frontend

```tsx
const { data } = await linuxio.request('module.mymodule', 'hello', ['Alice']);
// data = { exitCode: 0, stdout: "Hello Alice\n" }
```

### 5. Check Logs

```bash
journalctl -u linuxio -f
```

Look for:
```
Loaded module: My Custom Module v1.0.0
```

## Examples

### System Monitoring Module

**File:** `/etc/linuxio/modules/monitoring/module.yaml`

```yaml
name: monitoring
version: 1.0.0
title: System Monitoring

handlers:
  commands:
    cpu_usage:
      command: "top -bn1 | grep 'Cpu(s)' | awk '{print $2}' | cut -d% -f1"
      timeout: 3

    memory_usage:
      command: "free -m | grep Mem | awk '{print $3/$2 * 100.0}'"
      timeout: 3

    disk_usage:
      command: "df -h {{.path}} | tail -1 | awk '{print $5}'"
      args:
        - name: path
          type: string
          default: "/"

    active_connections:
      command: "ss -tunap | wc -l"
      timeout: 3
```

### Service Management Module

**File:** `~/.config/linuxio/modules/services/module.yaml`

```yaml
name: services
version: 1.0.0
title: Service Manager

handlers:
  commands:
    status:
      command: "systemctl is-active {{.service}}"
      args:
        - name: service
          type: string
          required: true

    logs:
      command: "journalctl -u {{.service}} -n {{.lines}} --no-pager"
      timeout: 10
      args:
        - name: service
          type: string
          required: true
        - name: lines
          type: string
          default: "20"

    restart:
      description: "Restart a service (requires sudo)"
      command: "sudo systemctl restart {{.service}}"
      args:
        - name: service
          type: string
          required: true

  dbus:
    list_units:
      bus: system
      destination: org.freedesktop.systemd1
      path: /org/freedesktop/systemd1
      interface: org.freedesktop.systemd1.Manager
      method: ListUnits
```

### Custom Integration Module

**File:** `~/.config/linuxio/modules/github/module.yaml`

```yaml
name: github
version: 1.0.0
title: GitHub Integration

handlers:
  commands:
    repo_stars:
      description: Get star count for a GitHub repo
      command: "curl -s https://api.github.com/repos/{{.owner}}/{{.repo}} | jq -r '.stargazers_count'"
      timeout: 10
      args:
        - name: owner
          type: string
          required: true
        - name: repo
          type: string
          required: true

    latest_release:
      description: Get latest release version
      command: "curl -s https://api.github.com/repos/{{.owner}}/{{.repo}}/releases/latest | jq -r '.tag_name'"
      timeout: 10
      args:
        - name: owner
          type: string
          required: true
        - name: repo
          type: string
          required: true
```

**Frontend Usage:**

```tsx
const { data: stars } = await linuxio.request(
  'module.github',
  'repo_stars',
  ['torvalds', 'linux']
);
```

## How It Works Under the Hood

### Module Loading Process

1. **Startup**: Bridge calls `modules.LoadModules()` in `register.go`
2. **Discovery**: Scans `/etc/linuxio/modules/` and `~/.config/linuxio/modules/`
3. **Parsing**: Reads each `module.yaml` and validates manifest
4. **Registration**:
   - Adds commands to `CommandRegistry` whitelist
   - Creates handler functions under `module.{name}` namespace
   - Registers handlers in `HandlersByType` map
5. **Ready**: Frontend can now call `module.{name}:{command}`

### Handler Execution Flow

When frontend calls `module.monitoring:check_disk` with args `["/home"]`:

1. **Bridge receives request**: `{ type: "module.monitoring", command: "check_disk", args: ["/home"] }`
2. **Handler lookup**: Finds handler in `HandlersByType["module.monitoring"]["check_disk"]`
3. **Whitelist check**: Verifies `monitoring:check_disk` exists in `CommandRegistry`
4. **Template substitution**:
   - Template: `df -h {{.path}}`
   - Args: `["/home"]`
   - Result: `df -h /home`
5. **Execution**: Calls `generic.ExecCommandDirect(command, timeout)`
6. **Response**: Returns `{ exitCode: 0, stdout: "..." }` to frontend

### Generic Handlers (Implementation Detail)

The module system is built on two generic handlers that are **disabled by default**:

- **`generic.ExecCommandDirect()`**: Execute shell commands
- **`generic.CallDbusMethodDirect()`**: Call DBus methods

These functions are NOT exposed to the frontend. Only the module loader can call them when executing whitelisted commands.

**File:** [backend/bridge/handlers/generic/command.go](../backend/bridge/handlers/generic/command.go)

```go
// Direct handler is DISABLED - returns error
func CommandHandlers() map[string]func([]string) (any, error) {
    return map[string]func([]string) (any, error){
        "exec": disabledExecHandler,  // Always returns error
    }
}

// Only accessible to module loader
func ExecCommandDirect(command, timeoutStr string) (any, error) {
    // ... actual execution
}
```

## Comparison with Existing Handlers

### Before: Hardcoded Handlers

To add disk monitoring:
1. Create `backend/bridge/handlers/disk/disk.go`
2. Implement `CheckDiskUsage(args []string)`
3. Add to `register.go`
4. Recompile backend
5. Deploy new binary

### After: Module System

To add disk monitoring:
1. Create `~/.config/linuxio/modules/monitoring/module.yaml`
2. Restart backend (reads YAML)
3. Done!

### When to Use Each

**Use Hardcoded Handlers** for:
- Complex stateful operations (terminals, file streaming)
- Performance-critical code
- Operations requiring Go libraries (Docker SDK, database drivers)
- Core system functionality

**Use Module System** for:
- Simple shell commands
- DBus integrations
- Custom monitoring/reporting
- User-specific workflows
- Rapid prototyping
- Third-party integrations

## Backend Implementation

### Files

- [backend/bridge/handlers/modules/manifest.go](../backend/bridge/handlers/modules/manifest.go) - Type definitions
- [backend/bridge/handlers/modules/registry.go](../backend/bridge/handlers/modules/registry.go) - Command whitelist
- [backend/bridge/handlers/modules/loader.go](../backend/bridge/handlers/modules/loader.go) - Module discovery
- [backend/bridge/handlers/generic/command.go](../backend/bridge/handlers/generic/command.go) - Command executor
- [backend/bridge/handlers/generic/dbus.go](../backend/bridge/handlers/generic/dbus.go) - DBus caller

### Key Types

```go
type ModuleManifest struct {
    Name        string
    Version     string
    Title       string
    Handlers    HandlerConfig
}

type CommandRegistry struct {
    commands map[string]*RegisteredCommand  // Whitelist
}

type RegisteredCommand struct {
    ModuleName  string
    CommandName string
    Template    string    // "df -h {{.path}}"
    Timeout     int
    Args        []HandlerArg
}
```

## Troubleshooting

### Module Not Loading

**Check logs:**
```bash
journalctl -u linuxio | grep -i module
```

**Common issues:**
- Invalid YAML syntax
- Missing required fields (`name`, `version`, `title`)
- Module directory not in `/etc/linuxio/modules/` or `~/.config/linuxio/modules/`

### Handler Not Found

**Error:** `command monitoring:check_disk not found in registry`

**Causes:**
- Module failed to load (check logs)
- Typo in module name or command name
- Backend not restarted after adding module

### Command Fails

**Check command manually:**
```bash
# Test the exact command from your YAML
df -h / | tail -1 | awk '{print $5}'
```

**Common issues:**
- Command not found (missing package)
- Permissions issue (need sudo)
- Timeout too short

## Future Enhancements

Potential improvements to the module system:

1. **Hot Reload**: Reload modules without restarting backend
2. **Module Marketplace**: Share/download community modules
3. **Type Generation**: Auto-generate TypeScript types from YAML
4. **Permission System**: Granular permission checks (filesystem, network, sudo)
5. **Module Dependencies**: Allow modules to depend on other modules
6. **Validation**: Runtime argument validation based on `args[].type`
7. **Module CLI**: `linuxio module install/list/enable/disable`

## Related Documentation

- [Module System Specification](./module-system.md) - Complete technical spec
- [Frontend API](./frontendAPI.md) - Frontend usage patterns
- [Bridge Handler API](./bridge-handler-api.md) - Bridge architecture
- [Cockpit Documentation](https://cockpit-project.org/guide/latest/development.html) - Architectural inspiration
