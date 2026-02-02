# LinuxIO Packaging

This directory contains all files needed to package LinuxIO for distribution.

## Quick Start

### Build DEB Package (Debian/Ubuntu)
```bash
make build-deb
```

### Build RPM Package (RHEL/Fedora)
```bash
make build-rpm
```

### Build Both Packages
```bash
make build-packages
```

Artifacts are written to `dist/`.

## Directory Structure

```
packaging/
├── debian/          # Debian package configuration
│   ├── control.in   # Package metadata template
│   ├── conffiles    # Config files (dpkg conffiles)
│   ├── postinst     # Post-installation script
│   ├── prerm        # Pre-removal script
│   └── postrm       # Post-removal script
├── rpm/             # RPM package configuration
│   └── linuxio.spec.in # RPM spec template
├── systemd/         # Systemd unit files
│   ├── linuxio.target
│   ├── linuxio-webserver.service
│   ├── linuxio-webserver.socket
│   ├── linuxio-auth.socket
│   ├── linuxio-auth@.service
│   ├── linuxio-bridge-socket-user.service
│   ├── linuxio-issue.service
│   └── linuxio-tmpfiles.conf
├── etc/             # Configuration files
│   ├── linuxio/
│   │   └── disallowed-users
│   └── pam.d/
│       └── linuxio
├── scripts/         # Installation and management scripts
│   ├── localinstall.sh
│   ├── uninstall.sh
│   ├── install-dependencies.sh
│   ├── build-deb.sh
│   ├── build-rpm.sh
│   ├── package-stage.sh
│   └── update-issue
├── PACKAGING.md     # Detailed packaging guide
└── README.md        # This file
```

## Documentation

For detailed instructions on building, customizing, and distributing packages, see:
- **[PACKAGING.md](PACKAGING.md)** - Complete packaging guide

## Package Contents

Both DEB and RPM packages include:
- CLI tool (`linuxio`)
- Web server (`linuxio-webserver`)
- Bridge service (`linuxio-bridge`)
- Auth helper (`linuxio-auth`)
- Systemd units
- Configuration files
- PAM configuration

## Installation

After building, install with:

**Debian/Ubuntu:**
```bash
sudo dpkg -i ../linuxio_*.deb
sudo apt-get install -f  # Fix dependencies
```

**RHEL/Fedora:**
```bash
sudo dnf install ~/rpmbuild/RPMS/x86_64/linuxio-*.rpm
```

## Requirements

### Build Requirements
- Go >= 1.25
- Node.js >= 18
- npm
- gcc
- PAM development libraries
- systemd development libraries
- dpkg-deb (Debian/Ubuntu)
- rpmbuild (RHEL/Fedora)

### Runtime Requirements
- Docker
- lm-sensors
- PAM libraries
- PolicyKit
- smartmontools
- systemd

## Maintainer

Miguel Mariz <miguelgalizamariz@gmail.com>
