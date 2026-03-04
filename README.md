<div align="center">

[![Release](https://img.shields.io/github/v/release/mordilloSan/LinuxIO)](https://github.com/mordilloSan/LinuxIO/releases/latest)
[![CodeQL](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql)
[![Go Report Card](https://goreportcard.com/badge/github.com/mordilloSan/LinuxIO/backend)](https://goreportcard.com/report/github.com/mordilloSan/LinuxIO/backend)
[![License](https://img.shields.io/github/license/mordilloSan/LinuxIO)](LICENSE)

<h1>Linux <img src="frontend/public/Logo.png" alt="i/O" height="32" align="center" /></h1>

A modern web dashboard to manage your Linux system: Docker, WireGuard, updates, users, shares, sensors, and more, from one unified interface.

</div>

---
## Philosophy

Linux I/O is inspired by [Cockpit](https://cockpit-project.org/) but goes further by integrating:

- **Docker management** (like Portainer)
- **WireGuard VPN** configuration
- **File management** (FileBrowser Quantum inspired)

**Goal:** One tool to manage your homelab without juggling multiple UIs.

---

## Features

- **PAM Authentication** - Login with your Linux credentials
- **Live System Stats** - CPU, memory, disk, and network monitoring
- **Docker Manager** - Container management
- **WireGuard UI** - VPN configuration
- **File Explorer** - Integrated file explorer
- **User Accounts** - User management
- **Share Manager** - Samba/NFS shares
- **NetworkManager** - Network configuration
- **Software Updates** - PackageKit integration
- **Hardware Sensors** - `lm-sensors` and SMART monitoring
- **Terminal** - Web-based command execution

---

## Installation

LinuxIO supports Linux kernels 5.9+.

### Quick install (recommended)

```bash
# Install dependencies (Docker, lm-sensors, PAM, PolicyKit, smartmontools)
curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-dependencies.sh | sudo bash

# Install LinuxIO binaries
curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash
```

Access the dashboard at `https://localhost:8090`.

### Manual dependency installation

If you prefer to install dependencies manually:

**Debian/Ubuntu:**

```bash
sudo apt update
sudo apt install -y lm-sensors libpam0g policykit-1 smartmontools
curl -fsSL https://get.docker.com | sudo sh
```

**Fedora/RHEL/CentOS:**

```bash
sudo dnf install -y lm_sensors pam polkit smartmontools
curl -fsSL https://get.docker.com | sudo sh
```

---

## Development

### Prerequisites

- Go 1.26+
- Node.js 24+
- Make

### Development Dependencies

**Debian/Ubuntu:**

```bash
sudo apt install -y libpam0g-dev libsystemd-dev cppcheck clang-tools bear
```

**Fedora/RHEL/CentOS:**

```bash
sudo dnf install -y pam-devel systemd-devel cppcheck clang-tools-extra bear
```

> Note: `libsystemd-dev` is optional but recommended - without it, auth worker logs fall back to syslog instead of journald. `scan-build` ships with `clang-tools`/`clang-tools-extra` and is optional for `make analyze-auth`.

### Initial Setup

```bash
# Clone repository
git clone https://github.com/mordilloSan/LinuxIO
cd LinuxIO

# Build everything (backend, bridge, auth, frontend)
make build

# Install to system (installs binaries + systemd services)
make localinstall
```

### Frontend Development

Once installed, frontend development uses hot reload while the backend runs via systemd:

```bash
# Start frontend dev server (vite with hot reload)
make dev
```

- Frontend runs on `http://localhost:3000`
- Vite proxies `/ws` and `/auth` to the backend on `:8090`
- Backend must be running via systemd (`sudo systemctl start linuxio`)
- `make dev` starts Vite detached (logs at `frontend/.vite-dev.log`) and tails `linuxio logs 50`

### Backend/Bridge Changes

If you modify Go code (backend, bridge, or auth), rebuild and reinstall:

```bash
make fastbuild
make localinstall
```

### Build Components Individually

```bash
make build-backend      # Go backend binary
make build-bridge       # Go bridge binary
make build-auth         # PAM authentication worker
make build-vite         # Frontend static assets
```

### Available Commands

**Setup and dependencies:**

```bash
make ensure-node       # Install/activate Node.js 24 via nvm
make ensure-go         # Install/activate Go from backend/go.mod (currently 1.26.0)
make setup             # Install frontend dependencies (npm install)
```

**Development:**

```bash
make dev               # Start frontend dev server (requires backend via systemd)
make dev-prep          # Create placeholder assets for dev server
```

**Quality Checks:**

```bash
make lint              # Run ESLint on frontend
make tsc               # TypeScript type checking
make golint            # Run gofmt + golangci-lint on backend
make test              # Run all linters (lint + tsc + golint)
make test-backend      # Run Go unit tests in backend
```

**Building:**

```bash
make build             # Build everything (frontend + backend + bridge + auth worker)
make build-vite        # Build frontend only
make build-backend     # Build backend only
make build-bridge      # Build bridge only
make build-auth        # Build PAM worker only
```

**Installation:**

```bash
make localinstall      # Install binaries + systemd services (requires sudo)
make reinstall         # Uninstall + rebuild + install
make uninstall         # Remove LinuxIO from system
```

**Running & Cleaning:**

```bash
make clean             # Remove build artifacts
```

> **Note:** The server runs via systemd. A helper CLI is included for management and logs. Run `linuxio --help` for options.

**Complete reference:** Run `make help` for full command list with descriptions.

---

## Tech Stack

### Frontend

- **React 19** with TypeScript
- **Vite** for fast builds
- **Material-UI** (Mira theme)
- **TanStack Query** for data fetching

### Backend

- **Go 1.26**
- **Gorilla WebSocket**
- **PAM** authentication

### Architecture

- **Main Server**: Handles HTTP/HTTPS and WebSocket connections
- **Bridge Process**: Per-user privileged operations with security isolation

---

## Security

- PAM-based authentication
- Session-based auth with secure cookies
- Socket-activated auth worker (no setuid)
- Isolated bridge processes per user
- TLS support with self-signed certificates

See [SECURITY.md](SECURITY.md) for details.

---

## Project Structure

```
LinuxIO/
|- backend/          # Go backend (HTTP + WebSocket)
|- frontend/         # React frontend (Vite + TypeScript)
|- packaging/        # Installation scripts and helpers
|- .github/          # CI/CD workflows
|- Makefile          # Build automation
`- README.md         # This file
```

---

## Contributing

Contributions welcome! Please read our [Contributing Guide](CONTRIBUTING.md) first.

Use [conventional commits](https://www.conventionalcommits.org/) for pull requests:

```bash
feat(docker): add container restart functionality
fix(auth): resolve session timeout issue
docs(readme): update installation instructions
```

---

## License

This project is licensed under the [Apache License](LICENSE).

---

## Acknowledgments

- [Cockpit](https://cockpit-project.org/) - Inspiration
- [FileBrowser Quantum](https://github.com/filebrowser/filebrowser) - File management
- [Mira Theme](https://mira.bootlab.io) - UI design

---

## Support

- [Wiki](https://github.com/mordilloSan/LinuxIO/wiki)
- [Issue Tracker](https://github.com/mordilloSan/LinuxIO/issues)
- [Discussions](https://github.com/mordilloSan/LinuxIO/discussions)
