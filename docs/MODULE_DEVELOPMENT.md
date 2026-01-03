# LinuxIO Module Development Guide

**Comprehensive Technical Reference**

Write modules with JSX/TypeScript just like core LinuxIO components!

> 📋 **Quick reference**: See [MODULES.md](MODULES.md) for a quick start guide

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Module Structure](#module-structure)
- [Development Workflow](#development-workflow)
- [Manifest Configuration](#manifest-configuration)
- [Building Modules](#building-modules)
- [Backend Handlers](#backend-handlers)
- [Security Model](#security-model)
- [Examples](#examples)
- [Troubleshooting](#troubleshooting)

---

## Overview

LinuxIO's module system allows you to extend functionality using the **same development experience as core components**:

✅ **Modern React Development:**
- Write JSX/TSX syntax
- Use TypeScript for type safety
- Import Material-UI components normally
- Hot module replacement during development
- Full ES6+ support

✅ **Seamless Integration:**
- Auto-appears in sidebar
- Auto-registered in router
- Shares React/Material-UI from main app
- Protected by authentication

✅ **Secure by Design:**
- Commands whitelisted in manifest
- DBus calls pre-approved
- No arbitrary code execution

### How It Works

```
Development (Your Code)              Production (Deployed)
┌────────────────────────┐          ┌─────────────────────┐
│  src/index.tsx (JSX)   │          │  component.js       │
│  import { Box } from   │  build   │  (Browser bundle)   │
│  '@mui/material';      │  ─────>  │                     │
│                        │          │  Uses window.React  │
│  <Box sx={{ p: 4 }}>  │          │  Uses window.       │
│    My Module           │          │    MaterialUI       │
│  </Box>                │          │                     │
└────────────────────────┘          └─────────────────────┘
```

---

## Quick Start

### 1. Create Module From Template

```bash
# Navigate to LinuxIO repository
cd /path/to/LinuxIO

# Copy the template
cp -r module-template modules/my-module

# Edit your module
cd modules/my-module

# No npm dependencies needed - uses LinuxIO's!
```

### 2. No Configuration Needed!

**Modules use the central build script** - no individual vite config needed!

The LinuxIO project includes a `packaging/scripts/build-module.sh` script that handles all the build configuration automatically.

### 3. Write Your Component

**`src/index.tsx`:**

```tsx
import React, { useState } from 'react';
import { Box, Typography, Button, useTheme } from '@mui/material';

// Import LinuxIO components (default exports - note .tsx extension)
import RootCard from '@/components/cards/RootCard.tsx';
import MetricBar from '@/components/gauge/MetricBar.tsx';

// Import utilities (named exports - note .ts extension)
import { formatFileSize } from '@/utils/formaters.ts';

function MyModule() {
  const [message, setMessage] = useState('');
  const theme = useTheme(); // Access LinuxIO theme

  const handleClick = () => {
    setMessage('Hello from My Module!');
  };

  return (
    <Box sx={{ p: 4 }}>
      <Typography variant="h3" gutterBottom>
        🧩 My Module
      </Typography>

      {/* Use LinuxIO components directly */}
      <RootCard sx={{ mt: 2 }}>
        <Typography gutterBottom>
          This module has full access to LinuxIO components and theme!
        </Typography>

        <MetricBar
          label="Example Metric"
          value={75}
          max={100}
          color={theme.palette.primary.main}
        />

        <Button
          variant="contained"
          onClick={handleClick}
          sx={{ mt: 2 }}
        >
          Click Me
        </Button>

        {message && (
          <Typography sx={{ mt: 2, color: 'success.main' }}>
            ✅ {message}
          </Typography>
        )}

        <Typography variant="body2" sx={{ mt: 2 }}>
          Formatted file size: {formatFileSize(1073741824)}
        </Typography>
      </RootCard>
    </Box>
  );
}

// Export to global namespace (REQUIRED for module loading)
declare global {
  interface Window {
    LinuxIOModules: Record<string, { default: React.ComponentType }>;
  }
}

window.LinuxIOModules = window.LinuxIOModules || {};
window.LinuxIOModules['my-module'] = { default: MyModule };

console.log('✅ My Module loaded successfully');

export default MyModule;
```

**Important Notes:**

1. **File Extensions Required:** Include `.tsx` or `.ts` in imports
2. **Default vs Named Exports:**
   - Most components use `export default` → import as `import ComponentName from '...'`
   - Utilities use named exports → import as `import { functionName } from '...'`
3. **Module Name Must Match:** The key in `window.LinuxIOModules['my-module']` must exactly match the `name` field in `module.yaml`

### 4. Create Manifest

**`module.yaml`:**

```yaml
name: my-module
version: 1.0.0
title: My Module
description: A demonstration module
author: Your Name

ui:
  route: /my-module
  icon: "mdi:puzzle-outline"
  sidebar:
    enabled: true
    position: 120

handlers:
  commands:
    hello:
      description: Says hello
      command: echo "Hello from backend!"
      timeout: 5
```

### 5. Build and Deploy

**Using Make (Recommended):**

Run make commands from the LinuxIO project root:

```bash
# From LinuxIO project root
cd /path/to/LinuxIO

# Production build (optimized, minified)
make build-module MODULE=my-module

# Development build (source maps, no minification)
make build-module-dev MODULE=my-module

# Watch mode (auto-rebuild on changes)
make watch-module MODULE=my-module

# Build + deploy to system
make deploy-module MODULE=my-module

# Clean build artifacts
make clean-module MODULE=my-module

# List all modules
make list-modules
```

**Example output:**
```
$ make build-module MODULE=my-module
Building my-module in production mode...
🔨 Building module: my-module
   Mode: production
   Module dir: /home/user/LinuxIO/modules/my-module
   LinuxIO root: /home/user/LinuxIO
   Optimizations: Minification, tree-shaking, compression
vite v7.3.0 building client environment for production...
transforming...
✓ 35 modules transformed.
rendering chunks...
computing gzip size...
../modules/my-module/dist/component.js  19.20 kB │ gzip: 7.82 kB
✓ built in 265ms

✅ Build complete: /home/user/LinuxIO/modules/my-module/dist/component.js
   Bundle size: 20K
```

**Complete deployment workflow:**

```bash
# From LinuxIO project root
cd /path/to/LinuxIO

# Build and deploy in one command
make deploy-module MODULE=my-module

# Restart LinuxIO to load the module
sudo systemctl restart linuxio.target
```

**Manual deployment (if not using Make):**

```bash
# Build
cd /path/to/LinuxIO
packaging/scripts/build-module.sh my-module

# Install
sudo mkdir -p /etc/linuxio/modules/my-module/ui
sudo cp modules/my-module/module.yaml /etc/linuxio/modules/my-module/
sudo cp modules/my-module/dist/component.js /etc/linuxio/modules/my-module/ui/
sudo chmod -R 755 /etc/linuxio/modules/my-module

# Restart
sudo systemctl restart linuxio.target
```

**Done!** Your module will appear in the sidebar with full access to LinuxIO components and theme.

---

## Module Structure

### Modules Live in LinuxIO Repository

**Modules are developed inside the LinuxIO project structure:**

```
LinuxIO/
├── frontend/
│   ├── src/
│   │   ├── components/      # ← You can import these!
│   │   ├── pages/
│   │   ├── theme/           # ← Access theme here
│   │   └── ...
│   ├── tsconfig.json        # ← Shared config
│   └── vite.config.ts
├── Makefile                 # ← Main build system (includes module targets)
├── packaging/
│   └── scripts/
│       └── build-module.sh  # ← Build script
├── modules/                 # ← Your modules here (copy from template)
│   └── my-module/           # Your custom modules
│       ├── src/
│       │   └── index.tsx    # Import from @/components, @/theme, etc.
│       ├── module.yaml      # Manifest
│       └── dist/            # Build output (gitignored)
│           └── component.js
├── module-template/         # ← Template for creating new modules
│   ├── src/index.tsx        # Reference implementation
│   ├── module.yaml
│   └── dist/
└── backend/
```

### Benefits of This Structure

✅ **Full Access to LinuxIO Components:**
```tsx
import { RootCard } from '@/components/cards/RootCard';
import { MetricBar } from '@/components/metrics/MetricBar';
import { TabSelector } from '@/components/tabs/TabSelector';
```

✅ **Access to Theme:**
```tsx
import { useTheme } from '@mui/material';
import theme from '@/theme';
```

✅ **Access to Utilities:**
```tsx
import { formatBytes } from '@/utils/formatters';
import { linuxio } from '@/api/linuxio';
```

✅ **Shared TypeScript Config:**
- Use project's tsconfig.json
- All path aliases work (`@/components`, `@/utils`, etc.)
- Type checking with LinuxIO types

### Available LinuxIO Components

**IMPORTANT:** Most LinuxIO components use `export default`, so import without braces.

**Cards:**
```tsx
import RootCard from '@/components/cards/RootCard.tsx';
import GeneralCard from '@/components/cards/GeneralCard.tsx';
import ContainerCard from '@/components/cards/ContainerCard.tsx';
import WireguardInterfaceCard from '@/components/cards/WireguardInterfaceCard.tsx';
```

**Gauges & Metrics:**
```tsx
import MetricBar from '@/components/gauge/MetricBar.tsx';
import CircularGauge from '@/components/gauge/CirularGauge.tsx';
```

**Theme:**
```tsx
import { useTheme } from '@mui/material';

function MyModule() {
  const theme = useTheme();

  return (
    <Box sx={{
      backgroundColor: theme.palette.background.paper,
      color: theme.palette.text.primary,
      borderRadius: theme.shape.borderRadius,
      p: theme.spacing(4)
    }}>
      Themed content
    </Box>
  );
}
```

**Utilities (Named Exports):**
```tsx
import { formatFileSize, formatDate } from '@/utils/formaters.ts';

// Format bytes: 1024 → "1 KB"
const formatted = formatFileSize(1024);

// Format date
const date = formatDate('2024-01-01');
```

**Toast Notifications:**
```tsx
import { toast } from 'sonner';

// Show success message
toast.success('Module action completed!');

// Show error
toast.error('Something went wrong');

// Show with description
toast('Notification', {
  description: 'This is a detailed message'
});
```

**API Client:**
```tsx
import { linuxio } from '@/api/linuxio.ts';

// Call backend handlers (WebSocket-based)
const result = await linuxio.call('system', 'GetHostname', []);

// Call module handler
const moduleResult = await linuxio.call('module.my-module', 'commandName', [arg1, arg2]);
```

**Discovering Available Components:**
```bash
# From LinuxIO root - list all components
find frontend/src/components -name "*.tsx" -not -name "*.test.tsx"

# Check component exports
grep "export default" frontend/src/components/cards/RootCard.tsx
```

**Common Import Patterns:**

```tsx
// Material-UI (from window global - always available)
import { Box, Typography, Button, Grid, Paper } from '@mui/material';

// LinuxIO components (bundled into module - default exports)
import RootCard from '@/components/cards/RootCard.tsx';
import MetricBar from '@/components/gauge/MetricBar.tsx';

// LinuxIO utilities (bundled into module - named exports)
import { formatFileSize } from '@/utils/formaters.ts';

// LinuxIO API (bundled into module)
import { linuxio } from '@/api/linuxio.ts';
```

### File Permissions

```bash
# System modules (production)
sudo chown -R root:root /etc/linuxio/modules
sudo chmod -R 755 /etc/linuxio/modules

# Critical: Files must be readable by 'linuxio' user (webserver runs as linuxio)
```

---

## Development Workflow

### Local Development

**Option 1: Hot Reload (Recommended)**

```bash
# Run Vite dev server
npm run dev

# Opens http://localhost:5173
# Edit src/index.tsx and see changes instantly
```

**Option 2: Watch Mode**

```bash
# Auto-rebuild on changes
npm run build -- --watch

# In another terminal
while true; do
  sudo cp dist/component.js /etc/linuxio/modules/my-module/ui/
  sleep 2
done
```

### Deployment Workflow

**Development:**
```bash
~/.config/linuxio/modules/my-module/
```

**Production:**
```bash
/etc/linuxio/modules/my-module/
```

**Deploy script** (`deploy.sh`):

```bash
#!/bin/bash
set -e

echo "🔨 Building module..."
npm run build

echo "📦 Deploying to /etc/linuxio/modules/my-module..."
sudo mkdir -p /etc/linuxio/modules/my-module/ui
sudo cp module.yaml /etc/linuxio/modules/my-module/
sudo cp dist/component.js /etc/linuxio/modules/my-module/ui/
sudo chmod -R 755 /etc/linuxio/modules/my-module

echo "🔄 Restarting LinuxIO..."
sudo systemctl restart linuxio.target

echo "✅ Module deployed! Open https://localhost:8090/my-module"
```

```bash
chmod +x deploy.sh
./deploy.sh
```

---

## Manifest Configuration

### Complete Reference

```yaml
# ============================================================================
# Basic Information (Required)
# ============================================================================
name: example-module          # Unique identifier (kebab-case)
version: 1.0.0               # Semantic versioning
title: Example Module        # Display name
description: A demonstration module
author: Your Name
homepage: https://github.com/yourusername/example-module
license: MIT

# ============================================================================
# UI Configuration
# ============================================================================
ui:
  route: /example            # URL path (must start with /)
  icon: "mdi:puzzle-outline" # Iconify icon (https://icon-sets.iconify.design/)
  sidebar:
    enabled: true            # Show in sidebar
    position: 115            # Sidebar order (0-999, higher = lower in list)

# Position reference:
# Dashboard: 0, Network: 10, Updates: 20, Drives: 30, Docker: 40
# File Browser: 50, Users: 60, Services: 70, WireGuard: 80
# Advanced: 90, Configuration: 100, Terminal: 110
# Your modules: 115+

# ============================================================================
# Backend Handlers (Optional)
# ============================================================================
handlers:
  # Shell commands
  commands:
    hello:
      description: Returns a greeting
      command: echo "Hello {{.name}}!"
      timeout: 5             # Seconds
      args:
        - name: name
          type: string
          required: true
      returns:
        type: string
        description: Greeting message

    get-disk-usage:
      command: df -h {{.path}}
      timeout: 10
      args:
        - name: path
          type: string
          required: true

  # DBus calls
  dbus:
    get-hostname:
      bus: system            # 'system' or 'session'
      destination: org.freedesktop.hostname1
      path: /org/freedesktop/hostname1
      interface: org.freedesktop.DBus.Properties
      method: Get
      args:
        - org.freedesktop.hostname1
        - Hostname

# ============================================================================
# Security (Optional - future use)
# ============================================================================
permissions:
  - read:system
  - read:network
  - execute:commands
```

---

## Building Modules

### Central Build Script

LinuxIO provides a central build script at `packaging/scripts/build-module.sh` that handles all module builds.

**How it works:**

1. Creates a temporary Vite config in the frontend directory
2. Configures all LinuxIO path aliases (`@/components`, `@/utils`, etc.)
3. Externalizes React and Material-UI (uses window globals)
4. Bundles LinuxIO components into your module
5. Outputs IIFE format to `dist/component.js`

**Usage:**

```bash
# From LinuxIO project root (recommended)
make build-module MODULE=my-module

# Or using the script directly
packaging/scripts/build-module.sh my-module
```

### Build Configuration Details

The build script automatically configures:

```typescript
// vite.config.ts
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    lib: {
      entry: 'src/index.tsx',
      formats: ['iife'],              // ← Self-executing function
      name: 'MyModule',               // ← Global variable name (not used)
      fileName: () => 'component.js'  // ← Output filename
    },
    rollupOptions: {
      // Don't bundle these - use from window
      external: [
        'react',
        'react-dom',
        '@mui/material',
        '@emotion/react',
        '@emotion/styled'
      ],
      output: {
        globals: {
          react: 'window.React',
          'react-dom': 'window.ReactDOM',
          '@mui/material': 'window.MaterialUI'
        }
      }
    }
  }
});
```

### Entry Point Pattern

```tsx
// src/index.tsx

import React from 'react';
import { Box } from '@mui/material';

// Your component
function MyModule() {
  return <Box>My Module</Box>;
}

// REQUIRED: Export to window.LinuxIOModules
declare global {
  interface Window {
    LinuxIOModules: Record<string, { default: React.ComponentType }>;
  }
}

window.LinuxIOModules = window.LinuxIOModules || {};
window.LinuxIOModules['my-module'] = { default: MyModule };

// Optional: Export for type checking
export default MyModule;
```

**Critical**: The key `'my-module'` must match the `name` field in `module.yaml`.

### Available Dependencies

LinuxIO exposes these on `window`:

```typescript
// In your source (import normally)
import React, { useState, useEffect } from 'react';
import { Box, Typography, Button } from '@mui/material';

// At runtime (externalized by Vite)
window.React         // React library
window.ReactDOM      // React DOM
window.MaterialUI    // All @mui/material components
```

All Material-UI v6 components are available:
- Layout: `Box`, `Container`, `Grid`, `Stack`
- Inputs: `Button`, `TextField`, `Select`, `Checkbox`, `Switch`
- Feedback: `Alert`, `CircularProgress`, `Snackbar`
- Data Display: `Table`, `Paper`, `Card`, `Chip`, `Avatar`
- Navigation: `Tabs`, `Drawer`, `Menu`
- And 100+ more...

---

## Backend Handlers

### Command Handlers

Execute whitelisted shell commands.

```yaml
handlers:
  commands:
    check-port:
      description: Check if port is listening
      command: ss -tuln | grep :{{.port}}
      timeout: 5
      args:
        - name: port
          type: integer
          required: true
      returns:
        type: string
        description: Port status
```

**Calling from frontend** (to be implemented):

```tsx
// Future API
import { linuxio } from '@/api/linuxio';

const result = await linuxio.call('module.my-module', 'check-port', ['8080']);
```

### DBus Handlers

Call system DBus methods.

```yaml
handlers:
  dbus:
    get-timezone:
      bus: system
      destination: org.freedesktop.timedate1
      path: /org/freedesktop/timedate1
      interface: org.freedesktop.DBus.Properties
      method: Get
      args:
        - org.freedesktop.timedate1
        - Timezone
```

### Template Variables

```yaml
# Positional
command: echo "{{.arg0}} {{.arg1}}"

# Named (recommended)
command: ping -c {{.count}} {{.host}}
args:
  - name: count
    type: integer
  - name: host
    type: string

# Frontend call (args in order)
await call('module.my-module', 'ping', ['4', 'google.com']);
```

---

## Security Model

### Whitelist System

Only commands in `module.yaml` can execute:

```yaml
handlers:
  commands:
    safe-command:
      command: echo "safe"  # ✅ Allowed

# ❌ Modules CANNOT execute:
# - rm -rf /
# - curl malicious.com | sh
# - Any command not in manifest
```

### Directory Traversal Protection

```go
// Backend blocks: /modules/../../etc/passwd
if strings.Contains(urlPath, "..") {
    http.Error(w, "Invalid path", http.StatusBadRequest)
    return
}
```

### Authentication

Module files require valid session:

```go
mux.Handle("/modules/", sm.RequireSession(http.HandlerFunc(ServeModuleFiles)))
```

---

## Backend Architecture

### Security-First Design

The module system uses a **whitelist-only** approach for maximum security:

1. **Direct execution is DISABLED**: Generic handlers reject all direct calls from frontend
2. **YAML-based whitelisting**: Only commands defined in `module.yaml` can execute
3. **Command registry**: Runtime validation against registered modules
4. **Template substitution**: Safe argument injection via placeholders

### Request Flow

```
┌─────────────┐
│   Frontend  │
│  useCall()  │
└──────┬──────┘
       │ request("module.my-module", "check_disk", ["/"])
       ▼
┌─────────────────┐
│  Bridge Handler │ ──► Check if "my-module:check_disk" is whitelisted
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
- **Module Loader**: Scans directories and registers handlers at startup
- **Generic Handlers**: Execute whitelisted commands/DBus calls

### Module Discovery

Modules are discovered in two locations (in order of precedence):

1. **System modules**: `/etc/linuxio/modules/` (root user only)
2. **User modules**: `~/.config/linuxio/modules/` (override system modules)

User modules with the same name override system modules.

### Runtime Validation

When frontend calls `linuxio.call('module.my-module', 'commandName', [args])`:

1. **Module Check**: Bridge verifies module "my-module" is loaded
2. **Command Check**: Bridge verifies "commandName" exists in module's manifest
3. **Argument Validation**: Arguments are validated against manifest definitions
4. **Execution**: Command executes with template-substituted arguments
5. **Response**: Output is returned to frontend

This ensures **no arbitrary code execution** - only pre-defined, whitelisted operations can run.

---

## Examples

### Example 1: System Monitor

**Directory structure:**

```
system-monitor/
├── src/
│   ├── index.tsx
│   ├── components/
│   │   ├── CpuCard.tsx
│   │   ├── MemoryCard.tsx
│   │   └── DiskCard.tsx
│   └── hooks/
│       └── useSystemStats.ts
├── module.yaml
├── package.json
├── tsconfig.json
└── vite.config.ts
```

**`src/index.tsx`:**

```tsx
import React from 'react';
import { Box, Typography, Grid } from '@mui/material';
import CpuCard from './components/CpuCard';
import MemoryCard from './components/MemoryCard';
import DiskCard from './components/DiskCard';

function SystemMonitor() {
  return (
    <Box sx={{ p: 4 }}>
      <Typography variant="h3" gutterBottom>
        📊 System Monitor
      </Typography>

      <Grid container spacing={3}>
        <Grid item xs={12} md={4}>
          <CpuCard />
        </Grid>
        <Grid item xs={12} md={4}>
          <MemoryCard />
        </Grid>
        <Grid item xs={12} md={4}>
          <DiskCard />
        </Grid>
      </Grid>
    </Box>
  );
}

window.LinuxIOModules = window.LinuxIOModules || {};
window.LinuxIOModules['system-monitor'] = { default: SystemMonitor };

export default SystemMonitor;
```

**`src/components/CpuCard.tsx`:**

```tsx
import React, { useState, useEffect } from 'react';
import { Paper, Typography, CircularProgress } from '@mui/material';

export default function CpuCard() {
  const [cpuInfo, setCpuInfo] = useState<string>('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    // TODO: Call backend handler
    // const data = await linuxio.call('module.system-monitor', 'get-cpu-info');
    // setCpuInfo(data);

    // Mock data
    setTimeout(() => {
      setCpuInfo('Intel Core i7\n8 CPUs, 16 Threads');
      setLoading(false);
    }, 500);
  }, []);

  if (loading) {
    return (
      <Paper sx={{ p: 3, display: 'flex', justifyContent: 'center' }}>
        <CircularProgress />
      </Paper>
    );
  }

  return (
    <Paper sx={{ p: 3 }}>
      <Typography variant="h6" gutterBottom>
        CPU
      </Typography>
      <Typography
        component="pre"
        sx={{ fontSize: '0.875rem', whiteSpace: 'pre-wrap' }}
      >
        {cpuInfo}
      </Typography>
    </Paper>
  );
}
```

**`module.yaml`:**

```yaml
name: system-monitor
version: 1.0.0
title: System Monitor
description: Real-time system metrics

ui:
  route: /system-monitor
  icon: "mdi:monitor-dashboard"
  sidebar:
    enabled: true
    position: 125

handlers:
  commands:
    get-cpu-info:
      command: lscpu | grep -E '^(Model name|CPU\(s\)|Thread|Core)'
      timeout: 3
    get-memory:
      command: free -h
      timeout: 3
    get-disk:
      command: df -h /
      timeout: 3
```

### Example 2: Docker Manager

**`src/index.tsx`:**

```tsx
import React, { useState } from 'react';
import {
  Box,
  Typography,
  Button,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Paper,
  Chip
} from '@mui/material';

interface Container {
  id: string;
  name: string;
  status: string;
  image: string;
}

function DockerManager() {
  const [containers, setContainers] = useState<Container[]>([]);

  const loadContainers = async () => {
    // TODO: Call backend
    // const data = await linuxio.call('module.docker-manager', 'list-containers');

    // Mock data
    setContainers([
      { id: '1', name: 'nginx', status: 'running', image: 'nginx:latest' },
      { id: '2', name: 'redis', status: 'running', image: 'redis:alpine' }
    ]);
  };

  return (
    <Box sx={{ p: 4 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 3 }}>
        <Typography variant="h3">
          🐳 Docker Manager
        </Typography>
        <Button variant="contained" onClick={loadContainers}>
          Refresh
        </Button>
      </Box>

      <Paper>
        <Table>
          <TableHead>
            <TableRow>
              <TableCell>Name</TableCell>
              <TableCell>Image</TableCell>
              <TableCell>Status</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {containers.map((container) => (
              <TableRow key={container.id}>
                <TableCell>{container.name}</TableCell>
                <TableCell>{container.image}</TableCell>
                <TableCell>
                  <Chip
                    label={container.status}
                    color={container.status === 'running' ? 'success' : 'default'}
                    size="small"
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Paper>
    </Box>
  );
}

window.LinuxIOModules = window.LinuxIOModules || {};
window.LinuxIOModules['docker-manager'] = { default: DockerManager };

export default DockerManager;
```

---

## Troubleshooting

### Build Issues

**Error: `"ComponentName" is not exported by "path/to/component.tsx"`**

This means you're using named import syntax for a default export.

```tsx
// ❌ WRONG - trying to use named import for default export
import { RootCard } from '@/components/cards/RootCard.tsx';

// ✅ CORRECT - use default import
import RootCard from '@/components/cards/RootCard.tsx';

// To check what a component exports:
grep "export" frontend/src/components/cards/RootCard.tsx
// If you see "export default", use default import
```

**Error: `Could not load /path/to/component (imported by ...)`**

Missing file extension or wrong path.

```tsx
// ❌ WRONG - missing file extension
import RootCard from '@/components/cards/RootCard';

// ✅ CORRECT - include .tsx extension
import RootCard from '@/components/cards/RootCard.tsx';

// For utilities:
import { formatFileSize } from '@/utils/formaters.ts'; // Note .ts extension
```

**Error: `"formatBytes" is not exported by "src/utils/formaters.ts"`**

Wrong function name or typo.

```tsx
// ❌ WRONG - function doesn't exist
import { formatBytes } from '@/utils/formaters.ts';

// ✅ CORRECT - check actual export name
import { formatFileSize } from '@/utils/formaters.ts';

// To discover available exports:
grep "^export" frontend/src/utils/formaters.ts
```

**Build succeeds but blank page in browser**

Check browser console (F12) for errors. Common issues:

1. **Module name mismatch:**
```tsx
// In module.yaml
name: my-module

// In src/index.tsx - MUST MATCH EXACTLY
window.LinuxIOModules['my-module'] = { default: MyModule };
```

2. **Forgot to export to window:**
```tsx
// ❌ WRONG - missing window export
export default MyModule;

// ✅ CORRECT - must have both
window.LinuxIOModules['my-module'] = { default: MyModule };
export default MyModule;
```

3. **Missing file extensions in imports**

### Module Not Loading

**Module not in sidebar:**

```bash
# 1. Check if bridge loaded the module
sudo journalctl -u linuxio-bridge-socket-user -n 50 | grep -i module
# Should see: "Loaded module: My Module v1.0.0"

# 2. Check module location (bridge runs as root)
ls -la /etc/linuxio/modules/my-module/
# NOT ~/.config/linuxio/modules/ (that's for development only)

# 3. Check manifest
cat /etc/linuxio/modules/my-module/module.yaml
# Verify ui.sidebar.enabled: true
```

**500 Error when loading component.js:**

This means the webserver (running as `linuxio` user) cannot read the file.

```bash
# Fix permissions
sudo chmod -R 755 /etc/linuxio/modules
sudo linuxio restart
```

**404 Error for component.js:**

```bash
# Check file exists at correct path
ls -la /etc/linuxio/modules/my-module/ui/component.js

# Check module name matches in manifest
cat /etc/linuxio/modules/my-module/module.yaml | grep "^name:"

# The path served is: /modules/{name}/ui/component.js
# So file must be at: /etc/linuxio/modules/{name}/ui/component.js
```

**401 Unauthorized on page refresh:**

This is expected behavior - module routes require authentication. User will be redirected to login.

### Development Tips

**TypeScript errors in IDE but build works:**

Add `/// <reference types="vite/client" />` to `src/vite-env.d.ts`:

```typescript
/// <reference types="vite/client" />
```

**Hot reload not working:**

Vite dev server is for local development only. For testing in LinuxIO, use watch mode:

```bash
npm run build -- --watch
```

---

## Best Practices

### 1. Component Organization

```tsx
// ✅ Good - organized structure
src/
  index.tsx          // Entry point, exports to window
  App.tsx            // Main component
  components/        // Reusable components
  hooks/             // Custom hooks
  utils/             // Helpers
  types/             // TypeScript types

// ❌ Bad - everything in one file
src/
  index.tsx          // 1000 lines of code
```

### 2. Error Handling

```tsx
function MyModule() {
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchData = async () => {
    setLoading(true);
    setError(null);

    try {
      // Call backend
      const data = await api.call('module.my-module', 'getData');
      // Handle data
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  if (error) {
    return <Alert severity="error">{error}</Alert>;
  }

  if (loading) {
    return <CircularProgress />;
  }

  return <Box>Content</Box>;
}
```

### 3. Performance

```tsx
// Memoize expensive computations
const processedData = useMemo(() => {
  return expensiveOperation(rawData);
}, [rawData]);

// Debounce user input
const debouncedSearch = useDebounce(searchTerm, 300);
```

### 4. Accessibility

```tsx
<Button
  aria-label="Refresh data"
  onClick={handleRefresh}
>
  Refresh
</Button>

<TextField
  label="Search"
  aria-describedby="search-help"
/>
```

---

## Quick Reference

### Common Commands

```bash
# Create module from template
cd /path/to/LinuxIO
cp -r module-template modules/my-module

# List available templates and modules
make list-modules

# Build module (production - optimized)
make build-module MODULE=my-module

# Build module (development - with source maps)
make build-module-dev MODULE=my-module

# Watch mode (auto-rebuild on changes)
make watch-module MODULE=my-module

# Deploy module (build + install to system)
make deploy-module MODULE=my-module

# Clean build artifacts
make clean-module MODULE=my-module

# Restart LinuxIO services
make restart

# Check if module loaded
make logs-bridge | grep -i "Loaded module"

# View webserver logs
make logs-webserver

# View all logs
make logs
```

### Import Patterns Cheatsheet

```tsx
// ============================================================================
// React and Material-UI (always available from window)
// ============================================================================
import React, { useState, useEffect, useMemo } from 'react';
import {
  Box, Typography, Button, Grid, Paper,
  TextField, Select, MenuItem, Chip,
  Table, TableBody, TableCell, TableHead, TableRow,
  CircularProgress, Alert, Card, CardContent
} from '@mui/material';
import { useTheme } from '@mui/material';

// ============================================================================
// LinuxIO Components (DEFAULT EXPORTS - no braces, include .tsx)
// ============================================================================
import RootCard from '@/components/cards/RootCard.tsx';
import GeneralCard from '@/components/cards/GeneralCard.tsx';
import ContainerCard from '@/components/cards/ContainerCard.tsx';
import MetricBar from '@/components/gauge/MetricBar.tsx';
import CircularGauge from '@/components/gauge/CirularGauge.tsx';

// ============================================================================
// LinuxIO Utilities (NAMED EXPORTS - use braces, include .ts)
// ============================================================================
import { formatFileSize, formatDate } from '@/utils/formaters.ts';

// ============================================================================
// LinuxIO API (default export, include .ts)
// ============================================================================
import { linuxio } from '@/api/linuxio.ts';

// ============================================================================
// Toast Notifications (third-party library)
// ============================================================================
import { toast } from 'sonner';
```

### Minimal Module Template

```tsx
// src/index.tsx
import React from 'react';
import { Box, Typography } from '@mui/material';
import RootCard from '@/components/cards/RootCard.tsx';

function MyModule() {
  return (
    <Box sx={{ p: 4 }}>
      <Typography variant="h3" gutterBottom>
        My Module
      </Typography>
      <RootCard>
        <Typography>Hello, LinuxIO!</Typography>
      </RootCard>
    </Box>
  );
}

// REQUIRED: Export to window
declare global {
  interface Window {
    LinuxIOModules: Record<string, { default: React.ComponentType }>;
  }
}
window.LinuxIOModules = window.LinuxIOModules || {};
window.LinuxIOModules['my-module'] = { default: MyModule };
export default MyModule;
```

```yaml
# module.yaml
name: my-module
version: 1.0.0
title: My Module
description: A simple module
author: Your Name

ui:
  route: /my-module
  icon: "mdi:puzzle-outline"
  sidebar:
    enabled: true
    position: 120
```

### Troubleshooting Checklist

**Module not appearing in sidebar?**
- [ ] Check bridge logs: `sudo journalctl -u linuxio-bridge-socket-user -n 50 | grep -i module`
- [ ] Verify file exists: `ls -la /etc/linuxio/modules/my-module/module.yaml`
- [ ] Check `ui.sidebar.enabled: true` in module.yaml
- [ ] Restart LinuxIO: `sudo systemctl restart linuxio.target`

**500 error loading component.js?**
- [ ] Fix permissions: `sudo chmod -R 755 /etc/linuxio/modules`
- [ ] Verify file exists: `ls -la /etc/linuxio/modules/my-module/ui/component.js`
- [ ] Check webserver logs: `sudo journalctl -u linuxio-webserver-socket-user -f`

**Build errors?**
- [ ] Check import syntax: Default exports use `import X from`, named use `import { X } from`
- [ ] Verify file extensions: Always include `.tsx` or `.ts` in imports
- [ ] Check function names: Use exact export names (e.g., `formatFileSize` not `formatBytes`)
- [ ] Module name matches: Key in `window.LinuxIOModules['name']` must match `module.yaml` name field

**Blank page in browser?**
- [ ] Open browser console (F12) for error messages
- [ ] Verify window export: `window.LinuxIOModules['my-module'] = { default: MyModule };`
- [ ] Check module name matches exactly in both module.yaml and window export
- [ ] Clear browser cache and hard reload (Ctrl+Shift+R)

### File Structure Reference

```
LinuxIO/
├── Makefile                     # Main build system (includes module targets)
├── packaging/
│   └── scripts/
│       └── build-module.sh      # Build script
├── modules/                     # Your modules (copy from template)
│   └── my-module/               # Your custom module
│       ├── src/
│       │   └── index.tsx        # Entry point (your code)
│       ├── module.yaml          # Manifest
│       └── dist/                # Build output (gitignored)
│           └── component.js
├── module-template/             # Template for creating new modules
│   ├── src/index.tsx            # Reference implementation
│   ├── module.yaml
│   └── dist/
├── docs/
│   ├── MODULE_DEVELOPMENT.md    # Complete guide
│   └── MODULES.md               # Quick reference
│
/etc/linuxio/modules/            # Production location
└── my-module/
    ├── module.yaml              # Loaded by bridge (root user)
    └── ui/
        └── component.js         # Served by webserver (linuxio user)
```

### Key Locations

| Component | User | Config Location | Module Location |
|-----------|------|----------------|-----------------|
| Bridge | `root` | `/root/.config/linuxio/` | `/etc/linuxio/modules/` (primary)<br>`~/.config/linuxio/modules/` (fallback) |
| Webserver | `linuxio` | `/etc/linuxio/` | Serves from `/etc/linuxio/modules/` |
| Frontend | Browser | - | Loads from `/modules/{name}/ui/component.js` |

### Environment Variables

The build script uses these internally:
```bash
MODULE_NAME="my-module"           # From command line argument
MODULE_DIR="/full/path/to/module" # Resolved absolute path
```

---

## Resources

- **LinuxIO GitHub**: [https://github.com/mordilloSan/LinuxIO](https://github.com/mordilloSan/LinuxIO)
- **Material-UI Docs**: [https://mui.com/](https://mui.com/)
- **React Docs**: [https://react.dev/](https://react.dev/)
- **Vite Guide**: [https://vitejs.dev/guide/](https://vitejs.dev/guide/)
- **TypeScript Handbook**: [https://www.typescriptlang.org/docs/](https://www.typescriptlang.org/docs/)
- **Iconify Icons**: [https://icon-sets.iconify.design/](https://icon-sets.iconify.design/)

---

**Last Updated**: January 2026
**LinuxIO Version**: v0.6.1+
**License**: MIT
