[![Release](https://img.shields.io/github/v/release/mordilloSan/LinuxIO)](https://github.com/mordilloSan/LinuxIO/releases/latest)
[![CodeQL](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql)
[![Go Report Card](https://goreportcard.com/badge/github.com/mordilloSan/LinuxIO/backend)](https://goreportcard.com/report/github.com/mordilloSan/LinuxIO/backend)
[![License](https://img.shields.io/github/license/mordilloSan/LinuxIO)](LICENSE)

<table><tr>
  <td><h1>Linux</h1></td>
  <td><img src="frontend/public/Logo.png" alt="i/O" height="40" /></td>
</tr></table>

A modern web dashboard to manage your Linux system — Docker, WireGuard, updates, users, shares, sensors, and more — all from one unified interface.

---

## 🧠 Philosophy

Linux I/O is inspired by [Cockpit](https://cockpit-project.org/) but goes further by integrating:

- **Docker management** (like Portainer)
- **WireGuard VPN** configuration
- **File management** (FileBrowser Quantum inspired)
- **System monitoring** (CPU, RAM, disk, network)

**Goal:** One tool to manage your homelab without juggling multiple UIs.

---

## 🚀 Features

- 🔐 **PAM Authentication** - Login with your Linux credentials
- 📊 **Live System Stats** - CPU, memory, disk, network monitoring
- 🐳 **Docker Manager** - Container management
- 🛡️ **WireGuard UI** - VPN configuration
- 📁 **File Explorer** - Integrated File Explorer
- 👤 **User Accounts** - User management
- 📤 **Share Manager** - Samba/NFS shares
- 🌐 **NetworkManager** - Network configuration
- 🔄 **Software Updates** - PackageKit integration
- 💡 **Hardware Sensors** - lm-sensors & SMART monitoring
- 💻 **Terminal** - Web-based command execution

---

## 📦 Installation

### Quick Install (Recommended)

```bash
# Install dependencies (Docker, lm-sensors, PAM, PolicyKit, smartmontools)
curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-dependencies.sh | sudo bash

# Install LinuxIO binaries
curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash
```

Access the dashboard at: `http://localhost:8090`

### Manual Dependency Installation

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

## 🛠️ Development

### Prerequisites

- Go 1.25+
- Node.js 24+
- Make

### Development Dependencies

**Debian/Ubuntu:**

```bash
sudo apt install -y libpam0g-dev libsystemd-dev
```

**Fedora/RHEL/CentOS:**

```bash
sudo dnf install -y pam-devel systemd-devel
```

> Note: `libsystemd-dev` is optional but recommended - without it, auth-helper logs go to `/dev/null` instead of journald.

### Setup

```bash
# Clone repository
git clone https://github.com/mordilloSan/LinuxIO
cd LinuxIO

# Install dependencies
make setup

# Start development server
make dev
```

The frontend runs on `http://localhost:3000` with API proxy to `:18090`

### Build from Source

```bash
# Build everything (includes linting)
make build

# Or build components individually
make build-backend      # Go backend binary
make build-bridge       # Go bridge binary
make build-auth-helper  # PAM authentication helper
make build-vite         # Frontend static assets

# Run locally
make run
```

### Available Commands

**Setup & Dependencies:**

```bash
make ensure-node       # Install/activate Node.js 24 via nvm
make ensure-go         # Install Go 1.25 (user-local, no sudo)
make setup             # Install frontend dependencies (npm install)
make devinstall        # Install dev binaries (auto-detects if needed)
```

**Development:**

```bash
make dev               # Start dev mode with hot reload
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
make build             # Build everything (frontend + backend + bridge + auth-helper)
make build-vite        # Build frontend only
make build-backend     # Build backend only
make build-bridge      # Build bridge only
make build-auth-helper # Build PAM helper only
```

**Running & Cleaning:**

```bash
make run               # Run production server
make clean             # Remove build artifacts
make clean-dev         # Remove dev binaries and sudo config
make clean-all         # Full cleanup (workspace + dev environment)
```

**Release Workflow:**

```bash
make start-dev         # Create dev/vX.Y.Z branch
make open-pr           # Open release PR
make merge-release     # Merge PR and trigger release
```

**Complete reference:** Run `make help` for full command list with descriptions.

---

## ⚙️ Tech Stack

### Frontend

- **React 18** with TypeScript
- **Vite** for blazing fast builds
- **Material-UI** (Mira theme)
- **TanStack Query** for data fetching

### Backend

- **Go 1.25**
- **Gin** HTTP framework
- **Gorilla WebSocket**
- **PAM** authentication

### Architecture

- **Main Server**: Handles HTTP/HTTPS and WebSocket connections
- **Bridge Process**: Per-user privileged operations with security isolation
- **Docker Integration**: Containerized file browser (no exposed ports)

---

## 🔐 Security

- PAM-based authentication
- Session-based auth with secure cookies
- Setuid helper for privilege management
- Isolated bridge processes per user
- TLS support with self-signed certificates

See [SECURITY.md](SECURITY.md) for details.

---

## 📁 Project Structure

```
LinuxIO/
├── backend/          # Go backend (Gin + WebSocket)
├── frontend/         # React frontend (Vite + TypeScript)
├── packaging/        # Installation scripts & helpers
├── .github/          # CI/CD workflows
├── makefile          # Build automation
└── README.md         # This file
```

---

## 🤝 Contributing

Contributions welcome! Please read our [Contributing Guide](CONTRIBUTING.md) first.

Use [conventional commits](https://www.conventionalcommits.org/) for pull requests:

```bash
feat(docker): add container restart functionality
fix(auth): resolve session timeout issue
docs(readme): update installation instructions
```

---

## 📄 License

This project is licensed under the [Apache License](LICENSE).

---

## 🙏 Acknowledgments

- [Cockpit](https://cockpit-project.org/) - Inspiration
- [FileBrowser Quantum](https://github.com/filebrowser/filebrowser) - File management
- [Mira Theme](https://mira.bootlab.io) - UI design

---

## 📞 Support

- 📖 [Wiki](https://github.com/mordilloSan/LinuxIO/wiki)
- 🐛 [Issue Tracker](https://github.com/mordilloSan/LinuxIO/issues)
- 💬 [Discussions](https://github.com/mordilloSan/LinuxIO/discussions)
