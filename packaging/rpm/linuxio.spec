Name:           linuxio
Version:        0.8.0
Release:        1%{?dist}
Summary:        Modern web-based Linux system administration interface

License:        MIT
URL:            https://github.com/mordilloSan/LinuxIO
Source0:        %{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.25
BuildRequires:  nodejs >= 18
BuildRequires:  npm
BuildRequires:  gcc
BuildRequires:  pam-devel
BuildRequires:  systemd-devel
BuildRequires:  make
BuildRequires:  git

Requires:       docker
Requires:       lm_sensors
Requires:       pam
Requires:       polkit
Requires:       smartmontools
Requires:       systemd
Recommends:     curl

%description
LinuxIO provides a comprehensive web interface for managing Linux systems,
including system monitoring, Docker container management, user management,
and more.

Features:
 * Real-time system monitoring (CPU, memory, disk, network)
 * Docker container management
 * User and group management
 * Service management via systemd
 * Hardware sensors monitoring
 * Web-based terminal access

%prep
%setup -q

%build
# Set build variables
export GOFLAGS="-buildvcs=false -tags=nomsgpack"
export MODULE_PATH="github.com/mordilloSan/LinuxIO"
export GIT_VERSION="%{version}"
export GIT_COMMIT_SHORT="$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
export BUILD_TIME="$(date -u +'%%Y-%%m-%%dT%%H:%%M:%%SZ')"

# Build frontend
cd frontend
npm ci
npx vite build
cd ..

# Build bridge first
cd backend
go build \
    -ldflags "\
        -s -w \
        -X '${MODULE_PATH}/common/config.Version=${GIT_VERSION}' \
        -X '${MODULE_PATH}/common/config.CommitSHA=${GIT_COMMIT_SHORT}' \
        -X '${MODULE_PATH}/common/config.BuildTime=${BUILD_TIME}'" \
    -o ../linuxio-bridge ./bridge
cd ..

# Capture bridge hash
BRIDGE_SHA256=$(sha256sum linuxio-bridge | awk '{print $1}')

# Build backend
cd backend
go build \
    -ldflags "\
        -s -w \
        -X '${MODULE_PATH}/common/config.Version=${GIT_VERSION}' \
        -X '${MODULE_PATH}/common/config.CommitSHA=${GIT_COMMIT_SHORT}' \
        -X '${MODULE_PATH}/common/config.BuildTime=${BUILD_TIME}' \
        -X '${MODULE_PATH}/common/config.BridgeSHA256=${BRIDGE_SHA256}'" \
    -o ../linuxio-webserver ./webserver/
cd ..

# Build auth helper
gcc %{optflags} -DLINUXIO_VERSION=\"%{version}\" \
    -o linuxio-auth backend/auth/linuxio-auth.c \
    -lpam -lsystemd
strip --strip-unneeded linuxio-auth

# Build CLI
cd backend
go build \
    -ldflags "\
        -s -w \
        -X '${MODULE_PATH}/common/config.Version=${GIT_VERSION}' \
        -X '${MODULE_PATH}/common/config.CommitSHA=${GIT_COMMIT_SHORT}' \
        -X '${MODULE_PATH}/common/config.BuildTime=${BUILD_TIME}'" \
    -o ../linuxio ./
cd ..

%install
# Create directories
install -d %{buildroot}%{_bindir}
install -d %{buildroot}%{_sysconfdir}/linuxio
install -d %{buildroot}%{_sysconfdir}/pam.d
install -d %{buildroot}%{_unitdir}
install -d %{buildroot}%{_tmpfilesdir}
install -d %{buildroot}%{_datadir}/linuxio/issue

# Install binaries
install -p -m 0755 linuxio %{buildroot}%{_bindir}/linuxio
install -p -m 0755 linuxio-webserver %{buildroot}%{_bindir}/linuxio-webserver
install -p -m 0755 linuxio-bridge %{buildroot}%{_bindir}/linuxio-bridge
install -p -m 0755 linuxio-auth %{buildroot}%{_bindir}/linuxio-auth

# Install systemd units
install -p -m 0644 packaging/systemd/linuxio.target %{buildroot}%{_unitdir}/
install -p -m 0644 packaging/systemd/linuxio-webserver.service %{buildroot}%{_unitdir}/
install -p -m 0644 packaging/systemd/linuxio-webserver.socket %{buildroot}%{_unitdir}/
install -p -m 0644 packaging/systemd/linuxio-auth.socket %{buildroot}%{_unitdir}/
install -p -m 0644 packaging/systemd/linuxio-auth@.service %{buildroot}%{_unitdir}/
install -p -m 0644 packaging/systemd/linuxio-bridge-socket-user.service %{buildroot}%{_unitdir}/
install -p -m 0644 packaging/systemd/linuxio-issue.service %{buildroot}%{_unitdir}/

# Install tmpfiles.d
install -p -m 0644 packaging/systemd/linuxio-tmpfiles.conf %{buildroot}%{_tmpfilesdir}/linuxio.conf

# Install config files
install -p -m 0644 packaging/etc/linuxio/disallowed-users %{buildroot}%{_sysconfdir}/linuxio/

# Install PAM config
install -p -m 0644 packaging/etc/pam.d/linuxio %{buildroot}%{_sysconfdir}/pam.d/

# Install issue updater
install -p -m 0755 packaging/scripts/update-issue %{buildroot}%{_datadir}/linuxio/issue/

%post
# Create runtime directories
%tmpfiles_create linuxio.conf

# Create motd symlink
if [ -d /etc/motd.d ]; then
    ln -sf ../../run/linuxio/issue /etc/motd.d/linuxio 2>/dev/null || true
fi

# Enable but don't start services
%systemd_post linuxio.target

echo ""
echo "LinuxIO has been installed successfully!"
echo ""
echo "To start LinuxIO, run:"
echo "  sudo systemctl start linuxio.target"
echo ""
echo "Or use the CLI:"
echo "  sudo linuxio start"
echo ""
echo "Access the web interface at: https://localhost:8090"
echo ""

%preun
%systemd_preun linuxio.target

%postun
%systemd_postun_with_restart linuxio.target

if [ $1 -eq 0 ]; then
    # Remove configuration on uninstall
    rm -rf %{_sysconfdir}/linuxio
    rm -f %{_sysconfdir}/pam.d/linuxio
    rm -rf /run/linuxio
    rm -rf /var/lib/linuxIO
    rm -f /etc/motd.d/linuxio 2>/dev/null || true
fi

%files
%license packaging/debian/copyright
%doc README.md
%{_bindir}/linuxio
%{_bindir}/linuxio-webserver
%{_bindir}/linuxio-bridge
%{_bindir}/linuxio-auth
%{_unitdir}/linuxio.target
%{_unitdir}/linuxio-webserver.service
%{_unitdir}/linuxio-webserver.socket
%{_unitdir}/linuxio-auth.socket
%{_unitdir}/linuxio-auth@.service
%{_unitdir}/linuxio-bridge-socket-user.service
%{_unitdir}/linuxio-issue.service
%{_tmpfilesdir}/linuxio.conf
%config(noreplace) %{_sysconfdir}/linuxio/disallowed-users
%config(noreplace) %{_sysconfdir}/pam.d/linuxio
%{_datadir}/linuxio/issue/update-issue

%changelog
* Sun Feb 02 2025 Miguel Mariz <miguelgalizamariz@gmail.com> - 0.8.0-1
- Initial RPM package release
- Web-based system administration interface
- Docker container management
- User and group management
- System monitoring and hardware sensors
