# Linux i/O

[![CodeQL Advanced](https://github.com/mordilloSan/LinuxIO/actions/workflows/codeql.yml/badge.svg)](https://github.com/mordilloSan/LinuxIO/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mordilloSan/LinuxIO/go-backend)](https://goreportcard.com/report/github.com/mordilloSan/LinuxIO/go-backend)

![Logo](react/public/Logo.png)

**Linux i/O** is a modern dashboard for managing your Linux system using native tools.  
It aims to unify essential functionality in a single web-based interface without reinventing the wheel.

---

## 🧠 Philosophy

Most Linux distributions already come with powerful tools for monitoring and control — `docker`, `systemctl`, `nmcli`, etc.  
This project is about **leveraging those existing tools** by exposing their input/output via a friendly, minimal, and customizable web UI.  
As such we aim to rely on D-Bus connectivity, docker SDK and parsing linux commands. Hence the i/O meaning input/output

Instead of replacing the Linux experience, **Linux i/O visualizes it.**

---

## ⚙️ Stack

- **Frontend:** React (Vite + MUI - based on [Mira Pro theme](https://mira.bootlab.io/))
- **Backend:** Go + Air (for development)
- **Go Rest API:** Gin
- **Go Websocket:** gorilla

---

## 🚀 Features

- 🖥️ System stats dashboard: CPU, memory, disk, network
- 🧠 Process viewer: see running processes live
- 💻 Terminal output: view real-time output of Linux commands
- 🔐 Authentication via PAM (or other pluggable systems)
- 🧱 Static frontend serving in production
- 🛡️ WireGuard management UI
- 🐳 Docker Compose manager

---

## 📦 Getting Started

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
git clone https://github.com/mordilloSan/IO-Linux-Server
cd IO-Linux-Server
```

---

## 🛠️ Available Commands

This repo uses `make` to simplify standard operations.

✅ Run `make` inside the project directory to view available commands

```bash
make check-env        # Verify .env and required environment variables
make setup            # Install Node.js, Go and frontend dependencies
make test             # Run Vite linter + TypeScript type checks
make dev              # Start frontend (Vite) and backend (Go) in dev mode
make prod             # Build Vite production files and smake tart backend (Go) in production mode
make run              # Build Go binary and runs full production mode
make build-vite-dev   # Build frontend static files (Vite) for Go in development mode
make build-vite-prod  # Build frontend static files (Vite) for Go in production mode
make build-backend    # Build Go binary and runs it
make clean            # Remove build artifacts

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

Runs Air for Go backend auto-reloads

Runs Vite dev server with proxying to Go API

Outputs all API paths and logs (from Gin)

### 🚀 Production Mode

```bash
make prod
```

- Compiles frontend via Vite

- Serves static assets using go run .

- No logging enabled by default

### 📦 Binary Mode

```bash
make binary
```

- Produces a compiled, self-contained Go binary

- Frontend is bundled inside

- Suitable for systemd and production deployment

### 🔪 How It Works

Under the hood:

- **Air** watches Go files and rebuilds the backend on changes.
- The **Air config** lives in `go-backend/.air.toml`.
- The **React frontend** runs in `react/` and talks to the backend via Vite's proxy (see `vite.config.ts`).

💡 You can customize .env for ports, proxy settings, etc.

---

## 📁 Project Structure

```
IO_Linux_Server/
├── go-backend/       # Gin powered backend
├── react/            # Vite powered React frontend
├── .env              # Environment variables
├── .gitignore        # List of files to be ignored by git
├── makefile          # Automation of builds & setup
├── README.md         # You're reading it!
├── secret.env        # File to write your sudo password
└── SECURITY.md       # Security  write up
```

---

## 📃 License

MIT License — feel free to use, fork, or contribute!

---

## 🙋‍♂️ Author

Created by [@mordilloSan](https://github.com/mordilloSan)  
📧 miguelgalizamariz@gmail.com
