# LinuxIO Modules

**Quick Reference Guide**

Dynamic module system for extending LinuxIO functionality with React/TypeScript components.

> 📖 **For detailed documentation**, see [MODULE_DEVELOPMENT.md](MODULE_DEVELOPMENT.md)

## Quick Start

```bash
# Create a new module from template
cp -r module-template modules/my-module
cd modules/my-module

# Edit module.yaml with your module details
nano module.yaml
# Update: name, title, description, route, icon, position

# Edit src/index.tsx with your component code
nano src/index.tsx
# Update the component and window export name

# Build and deploy
cd ../..  # Back to LinuxIO root
make deploy-module MODULE=my-module
sudo systemctl restart linuxio.target
```

## Available Commands

```bash
make build MODULE=name         # Production build (optimized)
make build-dev MODULE=name     # Development build (with source maps)
make watch MODULE=name         # Auto-rebuild on changes
make deploy MODULE=name        # Build + deploy to system
make clean MODULE=name         # Remove build artifacts
make list                      # List all modules
make restart                   # Restart LinuxIO services
make logs                      # View all logs
make logs-bridge               # View bridge logs (module loading)
make logs-webserver            # View webserver logs (module serving)
```

## Module Structure

Each module contains:

```
my-module/
├── src/
│   └── index.tsx          # Entry point (React component)
├── module.yaml            # Manifest (metadata + backend handlers)
└── dist/                  # Build output (generated, gitignored)
    └── component.js       # Bundled JavaScript
```

**No per-module configs needed!** The build system uses central configuration.

## Development Workflow

1. **Fork the LinuxIO repository** - Modules are developed inside the repo
2. **Write your component** - Full access to LinuxIO components, theme, utilities
3. **Build with Makefile** - `make build MODULE=my-module`
4. **Deploy to system** - `make deploy MODULE=my-module`
5. **Test in browser** - Navigate to `https://localhost:8090/my-module`

## Features

✅ **Write modern JSX/TypeScript** - Just like core LinuxIO components
✅ **Import LinuxIO components** - RootCard, MetricBar, etc.
✅ **Access theme** - `useTheme()` hook
✅ **Use utilities** - formatFileSize, formatDate, etc.
✅ **Call backend handlers** - Via module.yaml manifest
✅ **Auto sidebar integration** - Appears automatically
✅ **Production optimizations** - 23% smaller bundles with minification

## Build Modes

### Production (Default)
- ESBuild minification
- Tree-shaking (removes unused code)
- No source maps

```bash
make build MODULE=my-module
# Output: 19.20 kB (7.82 kB gzipped)
```

### Development
- No minification
- Source maps for debugging
- Readable code
- Faster builds

```bash
make build-dev MODULE=my-module
# Output: 99 kB + 156 kB source map
```

## Documentation

See [MODULE_DEVELOPMENT.md](../docs/MODULE_DEVELOPMENT.md) for complete guide.

## Example Module

Check [module-template/](../module-template/) for a working reference implementation.

To create your own module:
```bash
cp -r module-template modules/my-module
# Edit modules/my-module/module.yaml and modules/my-module/src/index.tsx
make build-module MODULE=my-module
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     LinuxIO Frontend                         │
│  • Exposes React, Material-UI as window globals             │
│  • Loads module component.js files                          │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│                      Module Bundle                           │
│  • IIFE format (self-executing)                             │
│  • Uses window.React, window.MaterialUI                     │
│  • Bundles LinuxIO components (RootCard, etc.)              │
│  • Exports to window.LinuxIOModules['module-name']          │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│                    LinuxIO Backend                           │
│  Bridge: Loads module.yaml, registers handlers              │
│  Webserver: Serves /modules/*/ui/component.js               │
└─────────────────────────────────────────────────────────────┘
```

## Security

- ✅ Commands whitelisted in `module.yaml`
- ✅ No arbitrary code execution
- ✅ Session authentication required
- ✅ Directory traversal protection

## Troubleshooting

```bash
# Module not appearing?
make logs-bridge | grep -i module

# Build errors?
make clean MODULE=my-module
make build-dev MODULE=my-module  # Check source maps

# 500 error loading component?
sudo chmod -R 755 /etc/linuxio/modules
```

---

**LinuxIO Module System** - Write once, deploy anywhere.
