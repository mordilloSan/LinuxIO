# LinuxIO Package Building Guide

This guide explains how to build and distribute LinuxIO as native packages for Debian/Ubuntu and RHEL/Fedora systems.

## Overview

LinuxIO supports two package formats:
- **DEB** packages for Debian, Ubuntu, Linux Mint, Pop!_OS, etc.
- **RPM** packages for RHEL, Fedora, CentOS, Rocky Linux, AlmaLinux, etc.

## Prerequisites

### For Building DEB Packages (Debian/Ubuntu)

```bash
sudo apt-get update
sudo apt-get install -y \
    dpkg-dev \
    debhelper \
    build-essential \
    golang-go \
    nodejs \
    npm \
    gcc \
    libpam0g-dev \
    libsystemd-dev \
    git
```

### For Building RPM Packages (RHEL/Fedora)

```bash
# Fedora/RHEL 8+
sudo dnf install -y \
    rpm-build \
    rpmdevtools \
    golang \
    nodejs \
    npm \
    gcc \
    pam-devel \
    systemd-devel \
    git

# CentOS 7
sudo yum install -y \
    rpm-build \
    rpmdevtools \
    golang \
    nodejs \
    npm \
    gcc \
    pam-devel \
    systemd-devel \
    git
```

## Building Packages

### Build DEB Package

```bash
# From the project root
make build-deb
```

This will:
1. Install Go and Node.js if needed
2. Build the frontend and all backend components
3. Create a `.deb` package in the parent directory
4. Output: `../linuxio_<version>-1_amd64.deb`

### Build RPM Package

```bash
# From the project root
make build-rpm
```

This will:
1. Install Go and Node.js if needed
2. Build the frontend and all backend components
3. Create a `.rpm` package in `~/rpmbuild/RPMS/x86_64/`
4. Output: `~/rpmbuild/RPMS/x86_64/linuxio-<version>-1.<arch>.rpm`

### Build Both Packages

```bash
make build-packages
```

This will build both DEB and RPM packages sequentially.

## Installing Packages

### Install DEB Package

```bash
# Install the package
sudo dpkg -i ../linuxio_<version>-1_amd64.deb

# Fix any missing dependencies
sudo apt-get install -f

# Start LinuxIO
sudo systemctl start linuxio.target
```

### Install RPM Package

```bash
# Using rpm
sudo rpm -ivh ~/rpmbuild/RPMS/x86_64/linuxio-<version>-1.<arch>.rpm

# Or using dnf (recommended - handles dependencies)
sudo dnf install ~/rpmbuild/RPMS/x86_64/linuxio-<version>-1.<arch>.rpm

# Start LinuxIO
sudo systemctl start linuxio.target
```

## Package Structure

### Files Installed

- **Binaries** (`/usr/local/bin/` for DEB, `/usr/bin/` for RPM):
  - `linuxio` - CLI tool
  - `linuxio-webserver` - Web server
  - `linuxio-bridge` - Bridge service
  - `linuxio-auth` - PAM authentication helper

- **Systemd Units** (`/etc/systemd/system/`):
  - `linuxio.target`
  - `linuxio-webserver.service`
  - `linuxio-webserver.socket`
  - `linuxio-auth.socket`
  - `linuxio-auth@.service`
  - `linuxio-bridge-socket-user.service`
  - `linuxio-issue.service`

- **Configuration** (`/etc/linuxio/`):
  - `disallowed-users` - List of users denied access

- **PAM Configuration** (`/etc/pam.d/`):
  - `linuxio` - PAM authentication configuration

- **Tmpfiles** (`/usr/lib/tmpfiles.d/`):
  - `linuxio.conf` - Runtime directory configuration

- **Scripts** (`/usr/share/linuxio/issue/`):
  - `update-issue` - SSH login banner updater

### Post-Installation

Both packages automatically:
1. Create runtime directories via tmpfiles.d
2. Enable the LinuxIO systemd target
3. Create SSH login banner symlink (if motd.d exists)
4. Reload systemd daemon

The services are **enabled but not started** automatically. Users must manually start:
```bash
sudo systemctl start linuxio.target
# or
sudo linuxio start
```

## Uninstalling Packages

### Uninstall DEB Package

```bash
# Remove package (keep config files)
sudo apt-get remove linuxio

# Remove package and config files
sudo apt-get purge linuxio
```

### Uninstall RPM Package

```bash
# Remove package
sudo dnf remove linuxio
# or
sudo rpm -e linuxio
```

## Package Dependencies

### Runtime Dependencies (DEB)
- `docker.io` or `docker-ce` - Container management
- `lm-sensors` - Hardware monitoring
- `libpam0g` - PAM authentication
- `policykit-1` - Authorization framework
- `smartmontools` - Disk SMART data
- `systemd` - Service management

### Runtime Dependencies (RPM)
- `docker` - Container management
- `lm_sensors` - Hardware monitoring
- `pam` - PAM authentication
- `polkit` - Authorization framework
- `smartmontools` - Disk SMART data
- `systemd` - Service management

## Versioning

Package versions are determined from:
1. Git tags (if on a tagged commit)
2. Git branch name (for dev branches like `dev/v0.8.0`)
3. Git commit hash (as fallback)

The version is embedded in the binaries at build time.

## Customizing Packages

### Debian Package

Edit files in `packaging/debian/`:
- `control` - Package metadata and dependencies
- `rules` - Build instructions
- `changelog` - Version history
- `postinst` - Post-installation script
- `prerm` - Pre-removal script
- `postrm` - Post-removal script

### RPM Package

Edit the spec file at `packaging/rpm/linuxio.spec`:
- `%description` - Package description
- `BuildRequires` - Build-time dependencies
- `Requires` - Runtime dependencies
- `%build` - Build instructions
- `%install` - Installation instructions
- `%post` - Post-installation script
- `%preun` - Pre-uninstallation script
- `%postun` - Post-uninstallation script

## Troubleshooting

### DEB Build Fails

1. **Missing build dependencies**:
   ```bash
   sudo apt-get build-dep linuxio
   ```

2. **Go version too old**:
   ```bash
   make ensure-go  # Installs correct Go version
   ```

3. **Node.js version too old**:
   ```bash
   make ensure-node  # Installs correct Node.js version
   ```

### RPM Build Fails

1. **Missing rpmbuild**:
   ```bash
   sudo dnf install rpm-build rpmdevtools
   ```

2. **Build tree not set up**:
   ```bash
   rpmdev-setuptree
   ```

3. **Source tarball missing**:
   The build process creates it automatically from Git. Ensure you're in a Git repository.

## Distribution

### Debian Repository (Advanced)

To create a proper APT repository:

```bash
# Install reprepro
sudo apt-get install reprepro

# Create repository structure
mkdir -p deb-repo/conf

# Create conf/distributions
cat > deb-repo/conf/distributions <<EOF
Origin: LinuxIO
Label: LinuxIO
Codename: stable
Architectures: amd64 arm64
Components: main
Description: LinuxIO System Administration
SignWith: your-gpg-key-id
EOF

# Add package
reprepro -b deb-repo includedeb stable ../linuxio_*.deb

# Serve via HTTP/HTTPS
# Users can then add: deb https://your-domain/deb-repo stable main
```

### RPM Repository (Advanced)

To create a proper YUM/DNF repository:

```bash
# Install createrepo
sudo dnf install createrepo

# Create repository structure
mkdir -p rpm-repo

# Copy RPM packages
cp ~/rpmbuild/RPMS/x86_64/linuxio-*.rpm rpm-repo/

# Create repository metadata
createrepo rpm-repo/

# Serve via HTTP/HTTPS
# Users can then add a .repo file pointing to your repository
```

## CI/CD Integration

Example GitHub Actions workflow:

```yaml
name: Build Packages

on:
  push:
    tags:
      - 'v*'

jobs:
  build-deb:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Build DEB
        run: make build-deb
      - uses: actions/upload-artifact@v3
        with:
          name: deb-package
          path: ../linuxio_*.deb

  build-rpm:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Install RPM tools
        run: sudo apt-get install rpm
      - name: Build RPM
        run: make build-rpm
      - uses: actions/upload-artifact@v3
        with:
          name: rpm-package
          path: ~/rpmbuild/RPMS/x86_64/linuxio-*.rpm
```

## License

LinuxIO is released under the MIT License. See the copyright file included in the packages for details.

## Support

For issues with packages:
- GitHub Issues: https://github.com/mordilloSan/LinuxIO/issues
- Documentation: https://github.com/mordilloSan/LinuxIO

## Maintainer

Miguel Mariz <miguelgalizamariz@gmail.com>
