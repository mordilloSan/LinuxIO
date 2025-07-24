# Linux i/O

[![CodeQL Advanced](https://github.com/mordilloSan/LinuxIO/actions/workflows/codeql.yml/badge.svg)](https://github.com/mordilloSan/LinuxIO/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/mordilloSan/LinuxIO/backend)](https://goreportcard.com/report/github.com/mordilloSan/LinuxIO/backend)

![Logo](react/public/Logo.png)

**Linux i/O** is a modern dashboard for managing your Linux system using native tools.  
It aims to unify essential functionality in a single web-based interface without reinventing the wheel.

---

## 🧠 Philosophy

Most Linux distributions already come with powerful tools for deploying apps, monitoring and control — `docker`, `systemctl`, `nmcli`, etc.  
This project is about **leveraging those existing tools** by exposing their input/output via a friendly, minimal, and customizable web UI.  

Instead of replacing the Linux experience, **Linux i/O visualizes it.**

---

## ⚙️ Stack

- **Frontend:**  
  - **Framework:** React + Vite + TypeScript  
  - **Styling:** Material UI (based on the [Mira Theme](https://mira.bootlab.io))  
  - **REST API:** Axios  

- **Backend:**  
  - **Language:** Go (with Air for live reloading during development)  
  - **Authentication:** Auth done via existing PAM modules  
  - **HTTP Server:** API routes, middleware, authentication  
  - **REST API:** Gin  
  - **WebSocket:** Gorilla  

---

## 🚀 Features

- 🔐 Authentication via PAM (or other pluggable systems)  
- 📊 System stats dashboard: CPU, memory, disk, network, etc  
- 🌐 Network Manager  
- 🔄 Software Update Manager  
- 🧠 Service Viewer: see running processes live  
- 🐳 Docker Manager  
- 👤 User Accounts  
- 📤 Share Manager  
- 🛡️ WireGuard management UI  
- 💡 Hardware and Sensor Information  
- 📁 Navigator using a File Explorer via [FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser)  
- 💻 Terminal output: view real-time output of Linux commands  

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
make check-env         # Verify .env and required environment variables
make setup             # Install Node.js, Go, and frontend dependencies
make lint              # Run ESLint linter on frontend
make tsc               # Run TypeScript type checks on frontend
make test              # Run ESLint + TypeScript type checks
make dev               # Start frontend (Vite) and backend (Go) in dev mode (hot reload)
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

Runs Air for Go backend auto-reloads

Runs Vite dev server with proxying to Go API

Outputs all API paths and logs (from Gin)

### 🚀 Production Mode

```bash
make prod
```

- Compiles frontend via Vite serving static assets

- Compiles bridge Go binary

- Compiles main server Go binary

- All logging done to journald.

### 🔪 How It Works

Under the hood:

- **Air** watches Go files and rebuilds the backend on changes.
- The **Air config** lives in `backend/.air.toml`.
- The **React frontend** runs in `react/` and talks to the backend via Vite's proxy (see `vite.config.ts`).

💡 You can customize .env for ports, proxy settings, etc.

---

## 📁 Project Structure

```
IO_Linux_Server/
├── backend/          # Gin powered backend
├── frontend/         # Vite powered React frontend
├── .env              # Environment variables
├── .gitignore        # List of files to be ignored by git
├── makefile          # Automation of builds & setup
├── LICENSE           # License information
├── README.md         # You're reading it!
└── SECURITY.md       # Security  write up
```

---


## 📚 Learn More
📖 For additional details, usage tips, or development notes, please visit the Wiki.