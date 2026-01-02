# Generic Handlers - Cockpit-Style Modularity

## Overview

LinuxIO now includes two generic handlers that enable full frontend modularity without backend changes:

1. **`command/exec`** - Execute arbitrary shell commands
2. **`generic_dbus/call`** - Call arbitrary DBus methods

This follows the same pattern as [Cockpit](https://cockpit-project.org/) - the frontend is trusted and can define custom functionality purely in TypeScript.

## Usage from Frontend

### Generic Command Execution

Execute any shell command from the frontend:

```tsx
// Simple command
const { data } = await linuxio.request("command", "exec", [
  "df -h / | tail -1 | awk '{print $5}'"
]);
// data = { exitCode: 0, stdout: "45%" }

// Command with custom timeout (in seconds)
const { data } = await linuxio.request("command", "exec", [
  "ping -c 5 google.com",
  "15"  // 15 second timeout
]);

// Command that outputs JSON
const { data } = await linuxio.request("command", "exec", [
  "echo '{\"status\": \"ok\", \"value\": 42}'"
]);
// data = { status: "ok", value: 42 }  (auto-parsed)

// Failed command
const { data } = await linuxio.request("command", "exec", ["false"]);
// data = { exitCode: 1, stdout: "", error: "exit status 1" }
```

**Arguments:**
- `args[0]`: Command string (executed via `sh -c`)
- `args[1]`: Timeout in seconds (optional, default: 10)

**Returns:**
- If output is valid JSON → parsed JSON object
- Otherwise → `{ exitCode: number, stdout: string, error?: string }`

### Generic DBus Calls

Call any DBus method:

```tsx
// Reboot system
await linuxio.request("generic_dbus", "call", [
  "system",                           // bus ("system" or "session")
  "org.freedesktop.login1",           // destination
  "/org/freedesktop/login1",          // path
  "org.freedesktop.login1.Manager",   // interface
  "Reboot",                           // method
  "true"                              // method args (optional)
]);

// Get network connectivity
const { data } = await linuxio.request("generic_dbus", "call", [
  "system",
  "org.freedesktop.NetworkManager",
  "/org/freedesktop/NetworkManager",
  "org.freedesktop.DBus.Properties",
  "Get",
  "org.freedesktop.NetworkManager",
  "Connectivity"
]);
```

**Arguments:**
- `args[0]`: Bus type (`"system"` or `"session"`)
- `args[1]`: Destination (e.g., `"org.freedesktop.login1"`)
- `args[2]`: Object path (e.g., `"/org/freedesktop/login1"`)
- `args[3]`: Interface (e.g., `"org.freedesktop.login1.Manager"`)
- `args[4]`: Method name (e.g., `"Reboot"`)
- `args[5+]`: Method arguments (optional, passed as strings)

**Returns:**
- First value from DBus response body (or `null` if no return value)

## Creating Modular Frontend APIs

The generic handlers enable you to create clean, type-safe APIs purely in the frontend:

### Example: Custom Monitoring Module

**File:** `frontend/src/api/handlers/monitoring.ts`

```tsx
import { linuxio } from '../linuxio';

export const monitoringHandlers = {
  /**
   * Check disk usage for a path
   */
  checkDiskUsage: async (path: string = "/"): Promise<string> => {
    const { data } = await linuxio.request("command", "exec", [
      `df -h ${path} | tail -1 | awk '{print $5}'`
    ]);
    return data.stdout.trim();
  },

  /**
   * Ping a host and return latency in ms
   */
  pingHost: async (host: string): Promise<number> => {
    const { data } = await linuxio.request("command", "exec", [
      `ping -c 1 ${host} | grep 'time=' | sed 's/.*time=\\([0-9.]*\\).*/\\1/'`
    ]);
    return parseFloat(data.stdout);
  },

  /**
   * Get memory usage percentage
   */
  getMemoryUsage: async (): Promise<number> => {
    const { data } = await linuxio.request("command", "exec", [
      "free -m | grep Mem | awk '{print $3/$2 * 100.0}'"
    ]);
    return parseFloat(data.stdout);
  },

  /**
   * Get systemd service status
   */
  getServiceStatus: async (service: string): Promise<string> => {
    const { data } = await linuxio.request("command", "exec", [
      `systemctl is-active ${service}`
    ]);
    return data.stdout.trim();
  },

  /**
   * Reboot the system via DBus
   */
  rebootSystem: async (): Promise<void> => {
    await linuxio.request("generic_dbus", "call", [
      "system",
      "org.freedesktop.login1",
      "/org/freedesktop/login1",
      "org.freedesktop.login1.Manager",
      "Reboot",
      "true"
    ]);
  },
};
```

### Registering Custom Handlers

**File:** `frontend/src/api/index.ts`

```tsx
import { linuxio as baseLinuxio } from './linuxio';
import { systemHandlers } from './handlers/system';
import { dockerHandlers } from './handlers/docker';
import { monitoringHandlers } from './handlers/monitoring';

// Enhanced linuxio object with namespaced handlers
export const linuxio = {
  ...baseLinuxio,  // Keep original useCall, request

  // Namespaced handlers
  system: systemHandlers,
  docker: dockerHandlers,
  monitoring: monitoringHandlers,  // Your custom handlers!
};
```

### Using Custom Handlers in Components

```tsx
import { linuxio } from '@/api';

export function CustomMonitoringWidget() {
  // Use your custom API
  const { data: diskUsage } = linuxio.useCall("command", "exec", [
    "df -h / | tail -1 | awk '{print $5}'"
  ], { refetchInterval: 5000 });

  const { data: memUsage } = linuxio.useCall("command", "exec", [
    "free -m | grep Mem | awk '{print $3/$2 * 100.0}'"
  ], { refetchInterval: 2000 });

  return (
    <div>
      <h3>System Monitoring</h3>
      <p>Disk Usage: {diskUsage?.stdout}</p>
      <p>Memory Usage: {memUsage?.stdout}%</p>
    </div>
  );
}

// Or use the modular API (if you created monitoringHandlers)
export function ModularMonitoringWidget() {
  const [diskUsage, setDiskUsage] = useState<string>('');
  const [memUsage, setMemUsage] = useState<number>(0);

  useEffect(() => {
    const fetchData = async () => {
      const disk = await linuxio.monitoring.checkDiskUsage('/');
      const mem = await linuxio.monitoring.getMemoryUsage();
      setDiskUsage(disk);
      setMemUsage(mem);
    };

    fetchData();
    const interval = setInterval(fetchData, 5000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div>
      <h3>System Monitoring</h3>
      <p>Disk Usage: {diskUsage}</p>
      <p>Memory Usage: {memUsage.toFixed(1)}%</p>
    </div>
  );
}
```

## Benefits

### ✅ Frontend Modularity
- Create custom handlers without touching backend code
- Organize handlers by domain (monitoring, backup, custom services)
- Share handler modules across projects

### ✅ No Backend Changes
- Add new functionality by writing TypeScript
- No Go compilation required
- Faster development iteration

### ✅ Type Safety (Optional)
- Wrap generic handlers with type-safe functions
- Use TypeScript to enforce parameter types
- Get IDE autocomplete for custom APIs

### ✅ Flexibility
- Execute any shell command
- Call any DBus method
- Compose complex operations from simple building blocks

## Security Considerations

**⚠️ Important: This approach trusts the frontend completely.**

Since the bridge executes whatever commands/DBus calls the frontend sends:

1. **Authentication**: WebSocket connection MUST be authenticated
2. **User Permissions**: Bridge runs as the authenticated user (not root by default)
3. **Sudo for Privileged Ops**: Use `sudo` in commands for privileged operations
4. **Command Injection**: Frontend should sanitize user input before passing to commands
5. **Trust Model**: Same as Cockpit - frontend is part of the application, not user-provided code

### Example: Safe Command Execution

```tsx
// ❌ UNSAFE - User input directly in command
const unsafeHandler = async (userPath: string) => {
  return linuxio.request("command", "exec", [
    `df -h ${userPath}`  // Vulnerable to command injection!
  ]);
};

// ✅ SAFE - Validate/sanitize user input
const safeHandler = async (userPath: string) => {
  // Only allow absolute paths
  if (!userPath.match(/^\/[\w\/-]+$/)) {
    throw new Error("Invalid path");
  }

  return linuxio.request("command", "exec", [
    `df -h ${userPath}`
  ]);
};
```

## Examples

### System Monitoring
```tsx
// CPU temperature
const { data } = await linuxio.request("command", "exec", [
  "sensors | grep 'Core 0' | awk '{print $3}'"
]);

// Network speed
const { data } = await linuxio.request("command", "exec", [
  "cat /proc/net/dev | grep eth0 | awk '{print $2, $10}'"
]);

// Active connections
const { data } = await linuxio.request("command", "exec", [
  "ss -tunap | wc -l"
]);
```

### Service Management
```tsx
// Check service status
const { data } = await linuxio.request("command", "exec", [
  "systemctl is-active nginx"
]);

// Get service logs
const { data } = await linuxio.request("command", "exec", [
  "journalctl -u nginx -n 20 --no-pager"
]);

// Restart service (requires sudo)
await linuxio.request("command", "exec", [
  "sudo systemctl restart nginx"
]);
```

### Custom Integrations
```tsx
// Check GitHub API
const { data } = await linuxio.request("command", "exec", [
  "curl -s https://api.github.com/repos/owner/repo | jq '.stargazers_count'"
]);

// Run custom script
const { data } = await linuxio.request("command", "exec", [
  "/opt/custom-app/monitor.sh"
]);

// Database query
const { data } = await linuxio.request("command", "exec", [
  "mysql -u user -p'password' -e 'SELECT COUNT(*) FROM users;' dbname"
]);
```

## Comparison with Existing Handlers

### Before (Hardcoded Backend)
To add new functionality, you had to:
1. Modify Go code in `backend/bridge/handlers/`
2. Add handler to `register.go`
3. Recompile backend
4. Update frontend

### After (Generic Handlers)
To add new functionality, you only:
1. Write TypeScript handler in `frontend/src/api/handlers/`
2. Done! No backend changes.

### When to Use Each Approach

**Use Existing Handlers** when:
- Handler already exists (e.g., `system/get_cpu_info`)
- Complex logic requiring Go libraries (Docker SDK, etc.)
- Performance-critical operations
- Stateful operations (terminals, file streaming)

**Use Generic Handlers** when:
- Custom monitoring/reporting
- Simple shell commands
- DBus service integrations
- User-specific workflows
- Rapid prototyping

## Implementation Details

### Backend Files
- [backend/bridge/handlers/generic/command.go](../backend/bridge/handlers/generic/command.go)
- [backend/bridge/handlers/generic/dbus.go](../backend/bridge/handlers/generic/dbus.go)
- [backend/bridge/handlers/register.go](../backend/bridge/handlers/register.go)

### Command Handler
- Executes commands via `sh -c`
- Default timeout: 10 seconds
- Auto-detects JSON output
- Returns exit code and stdout/stderr

### DBus Handler
- Maintains persistent system/session bus connections
- Supports method calls with arbitrary arguments
- Returns first value from response body

## Related Documentation
- [Frontend API](./frontendAPI.md) - Frontend usage patterns
- [Bridge Handler API](./bridge-handler-api.md) - Bridge architecture
- [Cockpit Documentation](https://cockpit-project.org/guide/latest/development.html) - Inspiration for this approach
