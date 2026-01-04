# LinuxIO Modules

**Quick Reference Guide**

Dynamic module system for extending LinuxIO functionality with React/TypeScript components featuring **Hot Module Replacement (HMR)** during development.

> 📖 **For detailed documentation**, see [MODULE_DEVELOPMENT.md](MODULE_DEVELOPMENT.md)

## Quick Start

### Development Mode (with HMR)

```bash
# 1. Create a new module from template (auto-links and restarts)
make create-module MODULE=my-dashboard

# 2. Start dev server with HMR
make dev

# 3. Edit modules/my-dashboard/src/index.tsx
#    → Changes reflect instantly with HMR! ⚡

# 4. When done, deploy to production (auto-unlinks, builds, and restarts)
make deploy-module MODULE=my-dashboard
```

### Production Deployment

```bash
# Create, edit, and deploy (no HMR)
make create-module MODULE=my-module
# Edit modules/my-module/module.yaml and src/index.tsx
make deploy-module MODULE=my-module  # Auto-builds and restarts
```

## Available Commands

### Module Creation & Linking
```bash
make create-module MODULE=<name>       # Create new module from template
make link-module MODULE=<name>         # Symlink to /etc for HMR development
make unlink-module MODULE=<name>       # Remove development symlink
```

### Building & Deployment
```bash
make build-module MODULE=<name>        # Production build (minified, optimized)
make deploy-module MODULE=<name>       # Build + deploy to /etc/linuxio/modules
make clean-module MODULE=<name>        # Remove build artifacts
make uninstall-module MODULE=<name>    # Remove from system
```

### Development & Monitoring
```bash
make dev                               # Start Vite dev server (HMR enabled)
make list-modules                      # List project modules
linuxio modules                        # List installed system modules
linuxio restart                        # Restart LinuxIO services
linuxio logs                           # View all logs
```

## Module Structure

Each module contains:

```
my-module/
├── src/
│   └── index.tsx          # Entry point (React component)
├── module.yaml            # Manifest (metadata + backend handlers)
├── tsconfig.json          # TypeScript config (pre-configured)
├── node_modules/          # Symlink to ../../frontend/node_modules
└── dist/                  # Build output (generated, gitignored)
    └── component.js       # Bundled JavaScript (production only)
```

**No per-module npm dependencies!** Modules use LinuxIO's shared dependencies via symlink.

## Development Workflow

### Two Development Modes

**1. HMR Development (Recommended)**
- Changes reflect instantly in browser
- Full TypeScript support with IntelliSense
- No rebuild needed
- Module loaded from source via Vite

**2. Production Testing**
- Test optimized bundle
- Build required after each change
- Module loaded from `/etc/linuxio/modules`

### Workflow Comparison

| Step | HMR Mode | Production Mode |
|------|----------|-----------------|
| 1. Create | `make create-module` | `make create-module` |
| 2. Setup | `make link-module` + restart | Edit code |
| 3. Develop | `make dev` → edit → see changes instantly | `make build-module` after each edit |
| 4. Test | http://localhost:8090 | Copy to `/etc` + restart |
| 5. Deploy | `make unlink-module` + `make deploy-module` | `make deploy-module` |

## Features

✅ **Hot Module Replacement (HMR)** - Edit and see changes instantly
✅ **Write modern JSX/TypeScript** - Just like core LinuxIO components
✅ **Import LinuxIO components** - RootCard, MetricBar, etc.
✅ **Access theme** - `useTheme()` hook
✅ **Use utilities** - formatFileSize, formatDate, etc.
✅ **Call backend handlers** - Via module.yaml manifest
✅ **Auto sidebar integration** - Appears automatically
✅ **Production optimizations** - Minified bundles (~4-8 KB)

## Build Modes

### Development Mode (HMR)
- **No build needed** - Vite serves source directly
- **Hot Module Replacement** - Instant updates on save
- **Full TypeScript checking** - Real-time errors
- **Source maps** - Debug original code
- **Enabled with**: `make link-module` + `make dev`

### Production Mode
- **ESBuild minification** - Smaller bundles
- **Tree-shaking** - Removes unused code
- **IIFE format** - Self-contained module
- **No source maps** - Optimized for deployment

```bash
# Production build
make build-module MODULE=my-module
# Output: ~4-8 KB minified (2-4 KB gzipped)
```

## How HMR Works

```
Development (make link-module + make dev)
┌──────────────────────────────────────┐
│  /etc/linuxio/modules/my-module/     │
│  (symlink to project)                │
│         ↓                            │
│  modules/my-module/src/index.tsx     │
│         ↓                            │
│  Vite Dev Server (HMR)              │
│         ↓                            │
│  Browser loads from source           │
│  ✅ Edit → Save → Instant update     │
└──────────────────────────────────────┘

Production (make deploy-module)
┌──────────────────────────────────────┐
│  modules/my-module/src/index.tsx     │
│         ↓                            │
│  Vite build → component.js (IIFE)   │
│         ↓                            │
│  Copy to /etc/linuxio/modules/       │
│         ↓                            │
│  Browser loads minified bundle       │
└──────────────────────────────────────┘
```

## Documentation

See [MODULE_DEVELOPMENT.md](../docs/MODULE_DEVELOPMENT.md) for complete guide including:
- Component imports (RootCard, MetricBar, etc.)
- Theme access patterns
- API calls and backend handlers
- TypeScript configuration
- Troubleshooting

## Example Module

Check [modules/.template/](../modules/.template/) for reference implementation showing:
- Proper component structure
- Global type declarations
- Window exports for production
- Material-UI integration
- LinuxIO component usage

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     LinuxIO Frontend                         │
│  • Exposes React, Material-UI as window globals             │
│  • DEV:  Loads modules via import.meta.glob() with HMR      │
│  • PROD: Loads module component.js files via script tags    │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│                      Module (Development)                    │
│  • Loaded from: modules/*/src/index.tsx                     │
│  • Format: ESM (import.meta)                                │
│  • HMR: Enabled via Vite                                    │
│  • TypeScript: Checked in real-time                         │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│                    Module (Production)                       │
│  • Loaded from: /etc/linuxio/modules/*/ui/component.js      │
│  • Format: IIFE (self-executing)                            │
│  • Uses: window.React, window.MaterialUI                    │
│  • Exports: window.LinuxIOModules['module-name']            │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│                    LinuxIO Backend                           │
│  Bridge: Loads module.yaml, registers handlers              │
│  Webserver: Serves /modules/*/ui/component.js (prod)        │
│             or proxies to Vite (dev)                         │
└─────────────────────────────────────────────────────────────┘
```

## Security

- ✅ Commands whitelisted in `module.yaml`
- ✅ No arbitrary code execution
- ✅ Session authentication required
- ✅ Directory traversal protection
- ✅ Symlinks require sudo (development only)

## Troubleshooting

### Module not appearing in sidebar?
```bash
# Check if module is linked/deployed
ls -la /etc/linuxio/modules/my-module

# Check backend logs
linuxio logs | grep -i module

# Restart services
sudo systemctl restart linuxio.target
```

### HMR not working?
```bash
# 1. Ensure module is linked (not deployed)
ls -la /etc/linuxio/modules/my-module
# Should show: my-module -> /path/to/project/modules/my-module

# 2. Restart backend after linking
sudo systemctl restart linuxio.target

# 3. Start Vite dev server
make dev

# 4. Check browser console for errors
```

### Module works in dev but not production?
```bash
# Build and check for errors
make build-module MODULE=my-module

# Verify bundle exists
ls -la modules/my-module/dist/component.js

# Deploy and restart
make deploy-module MODULE=my-module
sudo systemctl restart linuxio.target
```

### TypeScript errors?
```bash
# Check module TypeScript
cd modules/my-module
npx tsc --noEmit

# Verify frontend types
cd frontend
npx tsc --noEmit
```

---

**LinuxIO Module System** - Write once, deploy anywhere. Develop with HMR, deploy optimized.
