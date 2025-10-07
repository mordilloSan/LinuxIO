[![CodeQL](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql)

![Logo](frontend/public/Logo.png)

# Linux I/O

A modern dashboard to manage your Linux system — Docker, WireGuard, updates, users, shares, sensors, and more — all in one web UI.

---

## 🧠 Philosophy and Inspiration

Linux I/O is inspired by [Cockpit](https://cockpit-project.org/): a single place to manage your server.  
But I wanted to go further — bring together **Docker management** (like Portainer), **WireGuard UI** (like Unraid), a **file explorer** (FileBrowser Quantum), and a straightforward **system API** (like Glances) into one cohesive app.

The goal: **one tool** to easily manage a “normal” homelab without juggling five different UIs.

Linux, as powerful as it is, suffers from too many distros, each with their set of tools.
I did my best to make this distro-agnostic, but it’s not fully there yet.
Base distro is debian/ubuntu

---

## ⚙️ Stack

- **Frontend**

  - **Framework:** React + Vite + TypeScript
  - **Styling:** Material UI (based on the [Mira Theme](https://mira.bootlab.io))
  - **REST API:** Axios and Tanstack Query

- **Backend**

  - **Language:** Go
  - **Authentication:** PAM modules
  - **HTTP server:** Gin (API routes, middleware, authentication)
  - **WebSocket:** Gorilla

- **Bridge**
  - A helper binary for executing privileged system-level actions securely.

---

## 🚀 Features

- 🔐 PAM authentication
- 📊 Live system stats: CPU, memory, disk, network
- 🌐 NetworkManager integration
- 🔄 Software updates (PackageKit)
- 🧠 Process viewer
- 🐳 Docker manager
- 👤 User accounts
- 📤 Share manager
- 🛡️ WireGuard UI
- 💡 Hardware sensors (lm-sensors, SMART)
- 📁 File Explorer via **FileBrowser Quantum**
- 💻 Streaming command output (terminal view)

---

## Getting Started

### Install dependencies

**For Debian/Ubuntu:**

```bash

sudo apt update
sudo apt install -y make curl git lm-sensors libpam0g-dev policykit-1 smartmontools python3-gi python3-dbus
```

**For Fedora / RHEL / CentOS:**

```bash
sudo dnf install -y make curl git lm_sensors pam-devel dnf-plugins-core smartmontools python3-gi python3-dbus
```

### Clone the repo

```bash
git clone https://github.com/mordilloSan/LinuxIO
cd LinuxIO
```

---

## 🛠️ Available Commands

This repo uses `make` to simplify standard operations.

Run `make` inside the project directory to view available commands

```bash
make setup             # Install Node.js, Go, and frontend dependencies
make lint              # Run ESLint linter on frontend
make tsc               # Run TypeScript type checks
make test              # Run ESLint + TypeScript type checks
make dev               # Start frontend (Vite) and backend (Go) in dev mode
make build             # Build frontend, backend, and bridge for production
make run               # Run production backend server
make build-backend     # Build Go backend binary
make build-bridge      # Build Go bridge binary
make build-vite        # Build frontend static files (Vite) for production
make clean             # Remove build artifacts and node_modules

```

---

## 🔐 Logging In

This project uses **PAM authentication** to log in directly to your Linux system using your own username and password.

---

## 👨‍💼 Development & Deployment Workflow

### 🛠️ Development Mode

```bash
make dev
```

Runs Vite dev server with proxying to Go API

Outputs all API paths and logs (from Gin)

### 🚀 Production Mode

```bash
make build
make run
```

- Compiles frontend via Vite serving static assets

- Compiles bridge Go binary

- Compiles main server Go binary

- All logging done to journald.

### 🔪 How It Works

Under the hood:

- The **React frontend** runs in `frontend/` and talks to the backend via Vite's proxy (see `vite.config.ts`).

---

## 📁 Project Structure

```
LinuxIO/
├── backend/          # Gin powered backend
├── frontend/         # Vite powered React frontend
├── .gitignore        # List of files to be ignored by git
├── LICENSE           # License information
├── makefile          # Automation of builds & setup
├── README.md         # You're reading it!
└── SECURITY.md       # Security  write up
```

---

## 📚 Learn More

- 📖 Wiki for extended docs
- 🔐 Security Policy
- ⚖️ License

## 📊 Status & Roadmap

**Status:** Active development — some features are experimental.  
**Roadmap:** Update UI improvements, WireGuard peer UX, per-feature permissions, .deb/.rpm packaging.
