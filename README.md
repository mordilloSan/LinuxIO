<div align="center">

[![Release](https://img.shields.io/github/v/release/mordilloSan/LinuxIO)](https://github.com/mordilloSan/LinuxIO/releases/latest)
[![CodeQL](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/mordilloSan/LinuxIO/actions/workflows/github-code-scanning/codeql)
[![License](https://img.shields.io/github/license/mordilloSan/LinuxIO)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/mordilloSan/LinuxIO?filename=backend/go.mod)](backend/go.mod)
[![React](https://img.shields.io/badge/react-19.2.8-61dafb?logo=react&logoColor=white)](frontend/package.json)
[![Platform](https://img.shields.io/badge/platform-linux-fcc624?logo=linux&logoColor=black)](README.md)

<h1>Linux <img src="frontend/public/Logo.png" alt="i/O" height="32" align="absmiddle" /></h1>

A modern web dashboard to manage your Linux system: Docker, WireGuard, updates, users, shares, sensors, and more, from one unified interface.

</div>

---
## Philosophy

Linux I/O is built around a simple idea: your homelab should have a single control plane.

Its primary inspiration is [Cockpit](https://cockpit-project.org/): one place to operate Linux systems end to end.

This project also draws inspiration from many projects across the homelab ecosystem, including [FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser), [Portainer](https://www.portainer.io/), [Homepage](https://gethomepage.dev/), [Unraid](https://unraid.net/), and many others.

The file-management experience and visual style are intentionally very closely
inspired by FileBrowser Quantum, as it fits very nicely into the app's theme so that influence is explicitly acknowledged.

LinuxIO's filesystem indexer follows up on the work done by FileBrowser Quantum
and builds on its indexing code, reworked as a dedicated LinuxIO
service. The goal is to provide a low-memory filesystem index with an on-demand
API, fast(ish) full indexing, and efficient query responses.

Linux I/O combines workflows usually split across multiple tools:

- **System administration** (users, networking, updates, sensors)
- **Container operations** (Portainer-style Docker management)
- **Secure remote access** (WireGuard configuration)
- **File operations** (FileBrowser Quantum-style management)

**Goal:** One tool to rule them all: one login, one dashboard, one workflow for your Linux stack.

---

## Features

- **PAM Authentication** - Login with your Linux credentials
- **Live System Stats** - CPU, memory, disk, and network monitoring
- **Metrics Daemon** - First-party `linuxio-monitoring` collector with metrics history
- **Processes** - Live process and program tables with CPU, memory, disk I/O, and container attribution
- **Docker Manager** - Container management
- **WireGuard UI** - VPN configuration
- **File Explorer** - Integrated file explorer
- **Filesystem Indexer** - Fast search, directory sizes, and scheduled indexing
- **User Accounts** - User management
- **Share Manager** - Samba/NFS shares
- **NetworkManager** - Network configuration
- **Software Updates** - PackageKit integration
- **Hardware Sensors** - `lm-sensors` and SMART monitoring
- **Terminal** - Web-based command execution

---

## Installation

LinuxIO is installed as a native package, `linuxio`, from the project's package
repository, `packages.engmariz.com`, whose metadata and packages are signed.
It is a DEB for the Debian family and an RPM for the RHEL family, for amd64
(`x86_64`) only, for Debian stable, Ubuntu LTS from 24.04 and the current and
previous Fedora releases. It needs Linux 5.9 or newer and a C
library of 2.39 or newer (Ubuntu 24.04's), so RHEL 9 and its rebuilds, and
anything older, cannot run it.

### Debian and Ubuntu

```bash
sudo curl -fsSLo /etc/apt/sources.list.d/linuxio.sources https://packages.engmariz.com/apt/linuxio.sources
sudo apt update
sudo apt install linuxio
```

`linuxio.sources` carries the repository's public key, so apt checks every
download against it.

### Fedora

```bash
sudo curl -fsSLo /etc/yum.repos.d/linuxio.repo https://packages.engmariz.com/rpm/linuxio.repo
sudo dnf install linuxio
```

dnf asks once to import the repository's key; compare its fingerprint with
the one in [Releases](docs/releases.md#owner-setup) before you accept.

The package brings PAM, polkit and PackageKit with it, enables LinuxIO and
starts it. The dashboard is at `https://<your-hostname>:8090`, and the address
is shown at an SSH login. After signing in, install optional capabilities
such as Docker, sensors, SMART monitoring, NFS, or Avahi from LinuxIO
Capability Manager. Docker's entry runs Docker's convenience script and shows
its output. Avahi installation includes the required Debian mDNS NSS
integration. On RHEL-family hosts the responder remains usable if the
optional `nss-mdns` package is unavailable, and LinuxIO warns that EPEL may be
needed for client-side `.local` resolution; the dashboard is then also
reachable at `https://<your-hostname>.local:8090`. Installing lm-sensors from
the Capability Manager also runs the required `sensors-detect --auto`
hardware probe. To listen on another port, see
[Packages](docs/packaging.md#the-avahi-announcement-and-the-port).

### Updating

LinuxIO updates like any other package: it appears among the host's updates
on the Updates page, where a banner points to it, and `apt upgrade`, `dnf
upgrade` and the distribution's automatic updates install it too. The update
restarts LinuxIO if it was running, which signs everyone out; jobs keep
running.

### Removing

```bash
sudo apt remove linuxio    # apt purge linuxio also removes its files under /etc
sudo dnf remove linuxio
```

Removal stops LinuxIO and removes what LinuxIO set up for its own work. Its
data in `/var/lib/linuxio` stays, and so do the scheduled scripts and other
things that run without LinuxIO. With LinuxIO gone, its libvirt hook no
longer runs, so on a host with Docker a VM on a bridge can lose its network
at its next start. [Packages](docs/packaging.md#removal) lists what goes and
what stays.

A LinuxIO installed by the install script of v0.54 or earlier is removed
first with that release's `uninstall.sh`; the package refuses to install over
it.

On first startup, LinuxIO creates a managed self-signed certificate in
`/var/lib/linuxio/webserver/certificates`. It covers localhost, the system
hostname, `<hostname>.local`, and the host's current IP addresses. The same
certificate is reused across restarts, reboots, and updates until it enters its
30-day renewal window.

The package puts the `linuxio` command in `/usr/bin` and eight programs in
`/usr/libexec/linuxio`: `linuxio-webserver`, `linuxio-bridge`, `linuxio-auth`,
`linuxio-docker-update`, `linuxio-indexer`, `linuxio-monitoring`,
`linuxio-worker`, and `linuxio-claude-relay`. Only `linuxio` is a command to
type (`man linuxio`); the programs systemd runs as root are installed so only
root can start them. From a checkout, `make localinstall` builds the binaries
in `build/`, packages them as a DEB or an RPM and installs that package with
`apt` or `dnf`.

The first-party `linuxio-monitoring` binary is installed and enabled with
LinuxIO rather than through Capability Manager. It samples the host, keeps the
metrics history in `/var/lib/linuxio/monitoring/metrics.db`, and serves
read-only metrics on `/run/linuxio/monitoring/api.sock` with commands behind the
root-only `/run/linuxio/monitoring/control.sock`. Its strict YAML configuration
lives at `/etc/linuxio/monitoring/config.yaml`; see the
[monitoring daemon guide](docs/monitoring.md) for the sockets, sampling
semantics, configuration fields, and troubleshooting.

The first-party `linuxio-indexer` binary is installed with LinuxIO rather than
through Capability Manager. Its root-only Unix API is socket activated at
`/run/linuxio/indexer.sock`; the webserver keeps the daemon warm while it is
running, and the daemon exits after a fixed 90-second idle grace once the
webserver and active work are gone. A systemd timer requests periodic full indexing. Its two-field YAML
configuration and rebuildable cache live under `/etc/linuxio/indexer` and
`/var/lib/linuxio/indexer`; see the [filesystem indexer guide](docs/indexer.md)
for permissions, lifecycle, and recovery.

<details>
<summary><strong>What gets installed?</strong></summary>

| Category | Packages | Required |
|----------|----------|----------|
| PAM, polkit, PackageKit, systemd | Authentication, authorization, system updates, and in-app capability installation | Dependencies of the `linuxio` package |
| Docker | Container management, installed from the Capability Manager with Docker's convenience script | Optional |
| Other capabilities | Sensors, SMART monitoring, NFS, Avahi, and other host facilities | Install from LinuxIO Capability Manager |

</details>

---

## Development

See the [development guide](docs/development.md) for setup requirements,
supported targets, and configurable overrides. The [docs index](docs/README.md)
lists the architecture, API-contract, and frontend guides.

---

## Tech Stack

### Frontend

- **React 19.2.4** with TypeScript
- **Vite** for fast builds
- **Material-UI** (Mira theme)
- **TanStack Query** for data fetching

### Backend

- **Go 1.27**
- **Gorilla WebSocket**
- **PAM** authentication

### Architecture

- **Main Server**: Handles HTTP/HTTPS and WebSocket connections
- **Bridge Process**: Per-user privileged operations with security isolation
- **Indexer Process**: Socket-activated SQLite/scanner owner with idle shutdown

---

## Security

- PAM-based authentication
- Session-based auth with secure cookies
- Socket-activated auth worker (no setuid)
- Isolated bridge processes per user
- Persistent managed TLS certificate with a private systemd state directory

See [SECURITY.md](SECURITY.md) for details.

---

## Project Structure

```
LinuxIO/
|- backend/          # Go backend (HTTP + WebSocket)
|- frontend/         # React frontend (Vite + TypeScript)
|- docs/             # Architecture and development docs (plans in docs/TODO)
|- packaging/        # Installation scripts and helpers
|- scripts/          # Shell programs behind the Make targets
|- mk/               # Make targets by concern (go, frontend, auth, quality, install)
|- .github/          # CI/CD workflows
|- Makefile          # Build automation entry point (binaries land in build/)
`- README.md         # This file
```

---

## Contributing

Contributions welcome! Please read the [development guide](docs/development.md) and [AGENTS.md](AGENTS.md) first.

Use [conventional commits](https://www.conventionalcommits.org/) for pull requests:

```bash
feat(docker): add container restart functionality
fix(auth): resolve session timeout issue
docs(readme): update installation instructions
```

---

## License

This project is licensed under the [Apache License 2.0](LICENSE).
The licenses and notices of the third-party work LinuxIO holds are installed
at `/usr/share/doc/linuxio/THIRD_PARTY_NOTICES.md`: the adapted code and icon
sets listed in [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md),
followed by the license of every Go module and frontend package in the build,
which the build writes.

---

## Acknowledgments

- [Cockpit](https://cockpit-project.org/) - Primary product inspiration and unified Linux operations model
- [Arcane](https://github.com/getarcaneapp/arcane) - UI and interaction inspiration
- [FileBrowser Quantum](https://github.com/gtsteffaniak/filebrowser) - File-management UX and visual inspiration, and the foundation for LinuxIO's filesystem indexer
- [Beszel](https://github.com/henrygd/beszel) - Server monitoring inspiration
- [lot-map](https://github.com/alexeyw/lot-map) by Aleksei Volodin - The design of the Assistant's activity map
- [Unraid](https://unraid.net/) - Product and homelab management inspiration
- Many other open source and self-hosted projects that shaped LinuxIO's direction

---

## Support

- [Wiki](https://github.com/mordilloSan/LinuxIO/wiki)
- [Issue Tracker](https://github.com/mordilloSan/LinuxIO/issues)
- [Discussions](https://github.com/mordilloSan/LinuxIO/discussions)
