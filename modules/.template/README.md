# LinuxIO Module Template

This template demonstrates how to create custom modules for LinuxIO with various types of handlers.

## Module Structure

```
example-module/
├── module.yaml          # Module configuration and handler definitions
└── src/
    ├── index.tsx        # Module entry point (imports and exports component)
    └── component.jsx    # React component for the UI (REQUIRED if ui.sidebar.enabled: true)
```

**Important:** If your module has `ui.sidebar.enabled: true` in module.yaml, you **MUST** provide UI component files (`src/index.tsx` and `src/component.jsx`). Otherwise, you'll get a 404 error when the module loads.

**No UI needed?** Set `ui.sidebar.enabled: false` in module.yaml to create a backend-only module (handlers accessible via API only).

## Module Types

**UI-Only Modules** (like the example):
- Use existing LinuxIO handlers: `linuxio.useCall("system", "get_cpu_info")`
- Execute commands via exec stream: `linuxio.useStream("exec", payload, callbacks)`
- Access all LinuxIO components, theme, and utilities
- No backend handlers needed

**Backend Modules** (define new handlers in `module.yaml`):
- Define whitelisted commands, DBus calls, or streams in `handlers:` section
- Call them via: `linuxio.useCall("module.{name}", "command_name", args)`
- Useful for system-specific commands or custom integrations

## Frontend API Patterns

### 1. JSON RPC Handlers (useCall)
For existing handlers that return JSON responses:

```javascript
const { data, refetch } = linuxio.useCall("system", "get_cpu_info", [], { enabled: false });

// Trigger manually
await refetch();
```

### 2. Exec Stream (useStream)
For executing commands and streaming stdout/stderr:

```javascript
// Unified signature matching useCall
linuxio.useStream('exec', 'ls', ['-lh', '/home'], {
  onData: (data) => {
    const text = linuxio.decodeString(data);
    console.log(text); // stdout output
  },
  onResult: (result) => {
    console.log('Exit code:', result.data?.exitCode);
  },
  onClose: () => {
    console.log('Stream closed');
  }
});

// Other examples
linuxio.useStream('exec', 'df', ['-h', '/'], callbacks);
linuxio.useStream('exec', 'tail', ['-f', '/var/log/syslog'], callbacks);
```

## Handler Types

### 1. Command Handlers

Execute shell commands and return JSON responses.

**Example: CPU Information**
```yaml
get_cpu_info:
  description: Get detailed CPU information
  command: lscpu -J
  timeout: 10
```

**Frontend Usage:**
```typescript
const { data, isLoading } = linuxio.useCall("module.example-module", "get_cpu_info");
```

**Example: List Directory (with arguments)**
```yaml
list_directory:
  description: List directory contents with details
  command: ls -lh {{.arg0}}
  timeout: 5
```

**Frontend Usage:**
```typescript
const { data } = linuxio.useCall("module.example-module", "list_directory", ["/home"]);
```

### 2. DBus Handlers

Make DBus method calls and return responses.

**Example: Get Hostname**
```yaml
get_hostname:
  description: Get system hostname from systemd
  bus: system
  destination: org.freedesktop.hostname1
  path: /org/freedesktop/hostname1
  interface: org.freedesktop.hostname1
  method: org.freedesktop.DBus.Properties.Get
  args:
    - org.freedesktop.hostname1
    - Hostname
  timeout: 5
```

**Frontend Usage:**
```typescript
const { data } = linuxio.useCall("module.example-module", "get_hostname");
```

### 3. DBus Stream Handlers

Subscribe to DBus signals and stream responses in real-time.

**Example: Monitor Systemd**
```yaml
monitor_systemd:
  description: Monitor systemd unit state changes
  bus: system
  destination: org.freedesktop.systemd1
  path: /org/freedesktop/systemd1
  interface: org.freedesktop.systemd1.Manager
  method: Subscribe
  signals:
    - UnitNew
    - UnitRemoved
  timeout: 1800
```

**Frontend Usage:**
```typescript
const stream = linuxio.openStream("module.example-module.monitor_systemd");
stream.onData = (data) => {
  console.log("Signal received:", data);
};
```

## Template Placeholders

Use placeholders in commands to pass arguments from the frontend:

- `{{.arg0}}` - First argument
- `{{.arg1}}` - Second argument
- `{{.argN}}` - Nth argument

**Example:**
```yaml
command: df -h {{.arg0}}
```

**Frontend call:**
```typescript
linuxio.useCall("module.example-module", "get_disk_usage", ["/home"]);
```

## Installing Your Module

### Development Mode (Symlink)

```bash
# Install with symlink for development
curl -X POST http://localhost:9090/api/json \
  -H "Content-Type: application/json" \
  -d '{
    "handler": "modules",
    "command": "InstallModule",
    "args": ["/path/to/your/module", "my-module", "true"]
  }'
```

**Or use the frontend:**
```typescript
const { mutate } = linuxio.useMutate("modules", "InstallModule");
mutate(["/path/to/your/module", "my-module", "true"]);
```

### Production Mode (Copy)

```bash
# Copy module to modules directory
curl -X POST http://localhost:9090/api/json \
  -H "Content-Type: application/json" \
  -d '{
    "handler": "modules",
    "command": "InstallModule",
    "args": ["/path/to/your/module", "my-module", "false"]
  }'
```

## Testing Your Handlers

After installing, test your handlers from the browser console:

```typescript
// Test CPU info
const cpuInfo = await linuxio.request("module.example-module", "get_cpu_info");
console.log(cpuInfo);

// Test ls -l
const dirListing = await linuxio.request("module.example-module", "list_directory", ["/home"]);
console.log(dirListing);

// Test with React hooks
const { data, isLoading, error } = linuxio.useCall(
  "module.example-module",
  "get_cpu_info"
);
```

## Example Responses

### get_cpu_info (lscpu -J)
```json
{
  "lscpu": [
    {"field": "Architecture:", "data": "x86_64"},
    {"field": "CPU op-mode(s):", "data": "32-bit, 64-bit"},
    {"field": "Model name:", "data": "Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz"}
  ]
}
```

### list_directory (ls -lh /home)
```json
{
  "exitCode": 0,
  "stdout": "total 4.0K\ndrwxr-xr-x 25 user user 4.0K Jan  4 10:30 user\n"
}
```

### get_memory (free -h --json)
```json
{
  "memory": {
    "total": 16777216,
    "used": 8388608,
    "free": 4194304
  }
}
```

## Module Permissions

Declare required permissions in your module.yaml:

```yaml
permissions:
  - read_system_info    # Read system information
  - execute_commands    # Execute shell commands
  - network_access      # Make network requests
  - filesystem_write    # Write to filesystem
```

## Module Settings

Allow users to configure your module:

```yaml
settings:
  - key: default_directory
    type: string
    default: /home
    description: Default directory for list_directory command
  - key: refresh_interval
    type: number
    default: 5000
    description: Refresh interval in milliseconds
```

## Best Practices

1. **Always set timeouts** - Prevent hanging commands
2. **Validate arguments** - Use command templates safely
3. **Return JSON when possible** - Use `-J` or `--json` flags
4. **Handle errors gracefully** - Check exit codes
5. **Test thoroughly** - Verify all handlers before deployment
6. **Document your handlers** - Add clear descriptions
7. **Use appropriate permissions** - Request only what you need

## Security Notes

- All commands are executed server-side with the LinuxIO process privileges
- Commands must be whitelisted in module.yaml - dynamic execution is disabled
- Template placeholders are substituted but NOT shell-escaped
- Only install modules from trusted sources
- System modules (in /etc/linuxio/modules) cannot be uninstalled

## Troubleshooting

**Module not showing up:**
- Check logs: `journalctl -u linuxio -f`
- Verify module.yaml syntax
- Ensure UI route is unique

**Handler not working:**
- Check timeout values
- Verify command syntax
- Test command manually: `linuxio-webserver` logs show exact commands
- Check permissions

**DBus errors:**
- Verify bus type (system vs session)
- Check destination/path/interface names
- Test with `busctl` or `gdbus`
