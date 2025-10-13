#!/usr/bin/env bash
# =============================================================================
# LinuxIO Preconfig & Installer (multi-distro)
# PAM, Docker, NM/PackageKit enablement, Debian extras cleanup
# © 2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

# ---------- Colors / UI ----------
readonly COLOUR_RESET='\e[0m'
readonly aCOLOUR=(
  '\e[38;5;154m' # green
  '\e[1m'        # bold white
  '\e[90m'       # grey
  '\e[91m'       # red
  '\e[33m'       # yellow
)
readonly GREEN_LINE="${aCOLOUR[0]}─────────────────────────────────────────────────────${COLOUR_RESET}"

Show() {
  if   (($1==0)); then echo -e "${aCOLOUR[2]}[${COLOUR_RESET}${aCOLOUR[0]}  OK  ${COLOUR_RESET}${aCOLOUR[2]}]${COLOUR_RESET} $2"
  elif (($1==1)); then echo -e "${aCOLOUR[2]}[${COLOUR_RESET}${aCOLOUR[3]}FAILED${COLOUR_RESET}${aCOLOUR[2]}]${COLOUR_RESET} $2"; exit 1
  elif (($1==2)); then echo -e "${aCOLOUR[2]}[${COLOUR_RESET}${aCOLOUR[0]} INFO ${COLOUR_RESET}${aCOLOUR[2]}]${COLOUR_RESET} $2"
  elif (($1==3)); then echo -e "${aCOLOUR[2]}[${COLOUR_RESET}${aCOLOUR[4]}NOTICE${COLOUR_RESET}${aCOLOUR[2]}]${COLOUR_RESET} $2"
  else               echo -e "${aCOLOUR[2]}[${COLOUR_RESET}${aCOLOUR[0]}      ${COLOUR_RESET}${aCOLOUR[2]}]${COLOUR_RESET} $2"; fi
}
GreyStart(){ echo -en "${aCOLOUR[2]}"; }

# shellcheck disable=SC2329
onCtrlC(){ echo -e "${COLOUR_RESET}"; exit 1; }

Check_Success() {
  # usage: Check_Success <rc> "message"
  if [[ ${1:-1} -ne 0 ]]; then
    Show 1 "${2:-Operation} failed!"
  else
    Show 0 "${2:-Operation} success!"
  fi
}

announce_init_system() {
  local pid1; pid1="$(ps -p 1 -o comm= 2>/dev/null || echo "")"
  if [[ "$pid1" != "systemd" ]]; then Show 3 "Init system is '$pid1' (not systemd) — service enable/start will be limited."; fi
}

# ---------- Globals / Detect OS ----------
Start () {
  export DEBIAN_FRONTEND=noninteractive
  # shellcheck disable=SC1091
  source /etc/os-release
  DIST="$ID"; readonly DIST
  ID_LIKE="${ID_LIKE:-}"
  UNAME_M="$(uname -m)"; readonly UNAME_M
  UNAME_U="$(uname -s)"; readonly UNAME_U

  # Best-effort target user (for docker group)
  TARGET_USER="${SUDO_USER:-}"
  TARGET_HOME="$(getent passwd "${SUDO_UID:-0}" | cut -d: -f6 2>/dev/null || echo /root)"
  WORK_DIR="$TARGET_HOME"; mkdir -p "$WORK_DIR"

  # Managed files
  readonly PAM_LINUXIO="/etc/pam.d/linuxio"
  readonly LINUXIO_DIR="/etc/linuxio"
  readonly LINUXIO_DENY="$LINUXIO_DIR/disallowed-users"
  readonly SCRIPT_LINK="https://raw.githubusercontent.com/mordilloSan/ubuntu/main/ubuntu-preconfig.sh"

  # APT cosmetic progress (if apt exists)
  if command -v apt-get >/dev/null 2>&1; then
    if [ ! -f /etc/apt/apt.conf.d/99fancy ] || ! grep -q "Progress-Fancy" /etc/apt/apt.conf.d/99fancy 2>/dev/null; then
      echo 'DPkg::Progress-Fancy "1";' >> /etc/apt/apt.conf.d/99fancy || true
    fi
  fi

  # Package family
  PKG_FAMILY=""
  if   command -v apt-get >/dev/null; then PKG_FAMILY="deb"
  elif command -v dnf     >/dev/null; then PKG_FAMILY="dnf"
  elif command -v zypper  >/dev/null; then PKG_FAMILY="zypper"
  elif command -v pacman  >/dev/null; then PKG_FAMILY="pacman"
  else Show 1 "Unsupported package manager (need apt/dnf/zypper/pacman)"; fi

  # Exact package names per family
  case "$PKG_FAMILY" in
    deb)
      PKGS_COMMON=( iputils-ping lm-sensors nfs-kernel-server nfs-common wireguard-tools packagekit dbus ca-certificates jq curl rsync network-manager )
      PKGS_PAM=( libpam0g libpam-modules libpam-modules-bin )
      PKGS_EXTRA=() # keep unattended-upgrades separate via Install_AutoUpdates
      DOCKER_DEPS=()
      NM_PKG="network-manager"
      PACKAGEKIT_SERVICE="packagekit"
      ;;
    dnf)
      PKGS_COMMON=( iputils lm_sensors nfs-utils wireguard-tools PackageKit ca-certificates jq curl rsync NetworkManager )
      PKGS_PAM=()
      PKGS_EXTRA=() # dnf-automatic installed in Install_AutoUpdates
      DOCKER_DEPS=( dnf-plugins-core )
      NM_PKG="NetworkManager"
      PACKAGEKIT_SERVICE="packagekit"
      ;;
    zypper)
      PKGS_COMMON=( iputils lm_sensors nfs-kernel-server nfs-client wireguard-tools PackageKit dbus-1 ca-certificates jq curl rsync NetworkManager )
      PKGS_PAM=()
      PKGS_EXTRA=() # zypper-automatic installed in Install_AutoUpdates
      DOCKER_DEPS=()
      NM_PKG="NetworkManager"
      PACKAGEKIT_SERVICE="packagekit"
      ;;
    pacman)
      PKGS_COMMON=( iputils lm_sensors nfs-utils wireguard-tools packagekit dbus ca-certificates jq curl rsync networkmanager )
      PKGS_PAM=()
      PKGS_EXTRA=() # Arch: no official unattended upgrades; we only notify
      DOCKER_DEPS=()
      NM_PKG="networkmanager"
      PACKAGEKIT_SERVICE="packagekit"
      ;;
  esac

  PACKAGES=( "${PKGS_COMMON[@]}" "${PKGS_PAM[@]}" "${PKGS_EXTRA[@]}" )
}

Check_Arch(){ case $UNAME_M in *64*) Show 0 "Arch : \e[33m$UNAME_M\e[0m" ;; *) Show 1 "Unsupported architecture: \e[33m$UNAME_M\e[0m" ;; esac; }
Check_OS(){ [[ $UNAME_U == *Linux* ]] && Show 0 "OS   : \e[33m$UNAME_U\e[0m" || Show 1 "This script is only for Linux."; }
Check_Distribution(){ Show 2 "Distro: \e[33m$DIST${ID_LIKE:+ (ID_LIKE=$ID_LIKE)}\e[0m"; }
Check_Permissions(){
  local interpreter; interpreter=$(ps -p $$ | awk '$1!="PID"{print $(NF)}' | tr -d '()')
  [[ "$interpreter" == "bash" ]] || Show 1 "Please run with bash. Current: $interpreter"
  [[ "$EUID" -eq 0 ]] || Show 1 "Please run as root or with sudo."
  Show 0 "Interpreter : \e[33m$interpreter\e[0m"
}
Check_Connection(){ if wget -q --spider http://google.com; then Show 0 "Internet : \e[33mOnline\e[0m"; else Show 1 "No internet connection"; fi; }

have_unit() {
  # 0 if the unit exists (any state), else 1
  local u="$1"
  # (1) quick list check (services/timers/targets)
  if systemctl list-unit-files --all --type=service --type=timer --type=target --no-legend 2>/dev/null \
      | awk '{print $1}' | grep -Fxq "$u"; then
    return 0
  fi
  # (2) vendor path present?
  systemctl cat "$u" >/dev/null 2>&1 && return 0
  # (3) status reports a unit (even if inactive/failed)
  systemctl status "$u" >/dev/null 2>&1 && return 0
  return 1
}

enable_one_of() {
  # usage: enable_one_of "friendly name" unit1 unit2 ...
  local label="$1"; shift
  local unit
  # ensure systemd’s caches are current
  systemctl daemon-reload >/dev/null 2>&1 || true
  for unit in "$@"; do
    if have_unit "$unit"; then
      # some units are static → start is OK, enable may be a no-op
      systemctl enable --now "$unit" >/dev/null 2>&1 || systemctl start "$unit" >/dev/null 2>&1 || true
      Show 0 "Enabled $label ($unit)"
      return 0
    fi
  done

  # last-resort hints (one-shot, not noisy)
  Show 3 "$label unit not found"
  if command -v systemctl >/dev/null 2>&1; then
    echo -e "${aCOLOUR[2]}--- systemctl units matching $label ---${COLOUR_RESET}"
    systemctl list-unit-files --all --no-legend 2>/dev/null | grep -i "$label" || true
    echo -e "${aCOLOUR[2]}--------------------------------------${COLOUR_RESET}"
  fi
  return 1
}

Welcome_Banner() {
  clear
  echo -e "${GREEN_LINE}${aCOLOUR[1]}"
  printf "\033[1mWelcome to the LinuxIO Preconfiguration Script.\033[0m\n"
  echo -e "${GREEN_LINE}${aCOLOUR[1]}\n"
  echo " This will update the system, install Docker and common tools,"
  echo " set LinuxIO PAM, enable NetworkManager/PackageKit,"
  echo " set up automatic updates per distro, and clean up."
  echo -e "\n${GREEN_LINE}${aCOLOUR[1]}"
  Check_Arch; Check_OS; Check_Distribution; Check_Permissions; Check_Connection
  announce_init_system
  Show 2 "Working Dir: \e[33m$WORK_DIR\e[0m"
  echo -e "${GREEN_LINE}${aCOLOUR[1]}\n"
  read -r -p "Are you sure you want to continue? [y/N]: " response </dev/tty
  case $response in [yY]|[yY][eE][sS]) : ;; *) echo "Exiting..."; exit 0;; esac
}

# ---------- Package ops ----------
pkg_update(){ case "$PKG_FAMILY" in
  deb) GreyStart; apt-get -qq update ;;
  dnf) GreyStart; dnf -y -q makecache ;;
  zypper) GreyStart; zypper -q --non-interactive refresh ;;
  pacman) GreyStart; pacman -Sy --noconfirm >/dev/null ;;
esac; Check_Success $? "Package metadata update"; }
pkg_upgrade(){ case "$PKG_FAMILY" in
  deb) GreyStart; apt-get -qq -y upgrade ;;
  dnf) GreyStart; dnf -y -q upgrade --refresh ;;
  zypper) GreyStart; zypper -q --non-interactive update ;;
  pacman) GreyStart; pacman -Su --noconfirm >/dev/null ;;
esac; Check_Success $? "System upgrade"; }
pkg_install(){ case "$PKG_FAMILY" in
  deb) GreyStart; apt-get install -y -qq "$@" ;;
  dnf) GreyStart; dnf install -y -q "$@" ;;
  zypper) GreyStart; zypper -q --non-interactive install -y "$@" ;;
  pacman) GreyStart; pacman -S --needed --noconfirm "$@" ;;
esac; Check_Success $? "Installing: $*"; }
pkg_is_installed(){ case "$PKG_FAMILY" in
  deb) dpkg-query -W -f='${Status}\n' "$1" 2>/dev/null | grep -q "ok installed" ;;
  dnf|zypper) rpm -q "$1" >/dev/null 2>&1 ;;
  pacman) pacman -Qi "$1" >/dev/null 2>&1 ;;
esac; }

Update_System(){ echo; Show 4 "Updating System"; Show 2 "Updating packages"; pkg_update; Show 2 "Upgrading packages"; pkg_upgrade; }

Reboot(){
  if [ -f /var/run/reboot-required ] || [ -f /var/run/reboot-required.pkgs ]; then
    Show 3 "System needs reboot for updates"
    read -r -p "Reboot now? [y/N]: " response </dev/tty
    case $response in
      [yY]|[yY][eE][sS])
        Show 4 "Preparing to reboot..."
        touch "$TARGET_HOME/resume-after-reboot" || true
        if [[ -n "$TARGET_HOME" && -w "$TARGET_HOME/.bashrc" ]]; then
          printf '%s\n' "curl -fsSL $SCRIPT_LINK | sudo bash" | tee -a "$TARGET_HOME/.bashrc" >/dev/null || true
        fi
        reboot </dev/tty
        ;;
    esac
  else
    Show 0 "No reboot required"
  fi
}

# ---------- Docker ----------
Install_Docker() {
  Show 2 "Installing \e[33mDocker\e[0m"
  if command -v docker >/dev/null 2>&1; then
    Docker_Version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "installed")
    Show 0 "Docker: ${Docker_Version}"
  else
    if ((${#DOCKER_DEPS[@]})); then pkg_install "${DOCKER_DEPS[@]}"; fi
    Show 2 "Using Docker convenience script"
    GreyStart; curl -fsSL https://get.docker.com | bash
    command -v docker >/dev/null 2>&1 || Show 1 "Docker installation failed"
    Docker_Version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "installed")
    Show 0 "Docker installed: ${Docker_Version}"
  fi
  if systemctl list-unit-files | grep -q '^docker\.service'; then
    systemctl enable --now docker >/dev/null 2>&1 || true
    Show 0 "docker service enabled"
  fi
  if [[ -n "${TARGET_USER:-}" && "$TARGET_USER" != "root" ]]; then
    if getent group docker >/dev/null 2>&1; then
      if id -nG "$TARGET_USER" | tr ' ' '\n' | grep -qx docker; then
        Show 0 "User $TARGET_USER already in docker group"
      else
        usermod -aG docker "$TARGET_USER" && Show 0 "Added $TARGET_USER" || true
      fi
    fi
  fi
}

# ---------- Packages ----------
Install_Packages() {
  echo; Show 4 "\e[1mInstalling Packages\e[0m"
  Install_Docker
  local TO_INSTALL=() p
  for p in "${PACKAGES[@]}"; do
    if pkg_is_installed "$p"; then Show 0 "$p already installed"; else TO_INSTALL+=("$p"); fi
  done
  if ((${#TO_INSTALL[@]})); then pkg_install "${TO_INSTALL[@]}"; fi
}

# ---------- Services: NetworkManager & PackageKit ----------
Enable_Core_Services() {
  # NetworkManager: try canonical unit, then common legacy/alias forms
  enable_one_of "NetworkManager" \
    "NetworkManager.service" \
    "NetworkManager" \
    "network-manager.service" \
    "network-manager"

  # PackageKit: distros vary in casing; try both
  enable_one_of "PackageKit" \
    "packagekit.service" \
    "PackageKit.service"
}

# ---------- Auto Updates (install) ----------
Install_AutoUpdates() {
  case "$PKG_FAMILY" in
    deb)
      if ! pkg_is_installed unattended-upgrades; then
        Show 2 "Installing unattended-upgrades (Debian/Ubuntu)"
        pkg_install unattended-upgrades
      else
        Show 0 "unattended-upgrades already installed"
      fi
      ;;
    dnf)
      if ! pkg_is_installed dnf-automatic; then
        Show 2 "Installing dnf-automatic (RHEL/Fedora)"
        pkg_install dnf-automatic
      else
        Show 0 "dnf-automatic already installed"
      fi
      ;;
    zypper)
      if ! pkg_is_installed zypper-automatic; then
        Show 2 "Installing zypper-automatic (SUSE)"
        pkg_install zypper-automatic
      else
        Show 0 "zypper-automatic already installed"
      fi
      ;;
    pacman)
      Show 3 "Arch Linux: no official unattended upgrades. Skipping install."
      ;;
  esac
}

# ---------------- GitHub release + packaging fetch ----------------
REPO_OWNER="mordilloSan"
REPO_NAME="LinuxIO"
RELEASE_TAG="${RELEASE_TAG:-}"     # optional override, e.g. v0.3.0
BIN_DIR="/usr/local/bin"
STAGING="/tmp/linuxio-install.$$"

Resolve_Release_Tag() {
  # keep only for logging/packaging ref; not required for binaries anymore
  if [[ -n "$RELEASE_TAG" ]]; then
    Show 0 "Using release tag: $RELEASE_TAG"
    return
  fi
  Show 0 "Using latest release (redirected by GitHub)"
}

PACKAGING_REF=""  # computed ref for raw file downloads

Resolve_Packaging_Ref() {
  # Prefer tag if it actually has the files; otherwise fallback to main.
  if [[ -n "$RELEASE_TAG" ]]; then
    local test_url="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${RELEASE_TAG}/packaging/systemd/linuxio.service"
    if curl -fsI "$test_url" >/dev/null 2>&1; then
      PACKAGING_REF="$RELEASE_TAG"
      Show 0 "Packaging ref: $PACKAGING_REF"
    else
      PACKAGING_REF="main"
      Show 3 "Packaging files not found at tag '$RELEASE_TAG' — falling back to '$PACKAGING_REF'"
    fi
  else
    PACKAGING_REF="main"
    Show 2 "Packaging ref: $PACKAGING_REF"
  fi
}

Download_Binaries() {
  mkdir -p "$STAGING"

  # If RELEASE_TAG override is set: use the tag, else use 'latest' redirector.
  local base
  if [[ -n "$RELEASE_TAG" ]]; then
    base="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${RELEASE_TAG}"
  else
    base="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download"
  fi

  Show 2 "Downloading linuxio components to staging..."
  GreyStart
  curl -fsSL "$base/linuxio"               -o "$STAGING/linuxio"
  curl -fsSL "$base/linuxio-bridge"        -o "$STAGING/linuxio-bridge"
  curl -fsSL "$base/linuxio-auth-helper"   -o "$STAGING/linuxio-auth-helper"
  Check_Success $? "Download binaries"
}


Install_Binaries() {
  mkdir -p "$BIN_DIR"
  Show 2 "Installing binaries to $BIN_DIR"
  install -m 0755 "$STAGING/linuxio"        "$BIN_DIR/linuxio"
  install -m 0755 "$STAGING/linuxio-bridge" "$BIN_DIR/linuxio-bridge"
  install -m 4755 "$STAGING/linuxio-auth-helper" "$BIN_DIR/linuxio-auth-helper"

  # Optional: sanity check that they’re executable and not empty
  if ! "$BIN_DIR/linuxio" --version >/dev/null 2>&1; then
    Show 3 "linuxio did not run with --version; ensure correct arch/build."
  fi
  if ! "$BIN_DIR/linuxio-bridge" --version >/dev/null 2>&1; then
    Show 3 "linuxio-bridge did not run with --version; ensure correct arch/build."
  fi
  Show 0 "Installed linuxio binaries"
}

Download_Packaging_Files() {
  mkdir -p "$STAGING/packaging"
  local raw="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/${PACKAGING_REF}/packaging"

  Show 2 "Downloading packaging files"
  GreyStart
  mkdir -p "$STAGING/packaging/etc/linuxio"
  curl -fsSL "$raw/etc/linuxio/disallowed-users" -o "$STAGING/packaging/etc/linuxio/disallowed-users"

  mkdir -p "$STAGING/packaging/systemd"
  curl -fsSL "$raw/systemd/linuxio.service" -o "$STAGING/packaging/systemd/linuxio.service"
  
  Check_Success $? "Download packaging files"
}

Install_Packaging_Files() {
  # /etc/linuxio
  install -d -m 0755 /etc/linuxio
  if [[ -f "$STAGING/packaging/etc/linuxio/disallowed-users" ]]; then
    install -m 0644 "$STAGING/packaging/etc/linuxio/disallowed-users" /etc/linuxio/disallowed-users
    Show 0 "Installed /etc/linuxio/disallowed-users"
  fi

  # systemd units (install to /etc/systemd/system for admin override)
  install -m 0644 "$STAGING/packaging/systemd/linuxio.service" /etc/systemd/system/linuxio.service
  Show 0 "Installed systemd service"
}

Enable_LinuxIO_Systemd() {
  if ! command -v systemctl >/dev/null; then
    Show 3 "systemd not present; skipping service enable"
    return
  fi
  Show 2 "Reloading systemd and enabling linuxio.service"
  GreyStart
  systemctl daemon-reload || true
  systemctl enable --now linuxio.service || true
  Check_Success $? "Enable linuxio.service"
}

Cleanup_Staging() {
  rm -rf "$STAGING" || true
}


# ---------- Auto Updates (enable/configure) ----------
Enable_AutoUpdates() {
  case "$PKG_FAMILY" in
    deb)
      # Don’t “enable” unattended-upgrades.service (static); enable the timers instead.
      if have_unit "apt-daily.timer"; then
        systemctl enable --now apt-daily.timer >/dev/null 2>&1 || true
        Show 0 "Enabled apt-daily.timer"
      else
        Show 3 "apt-daily.timer not found"
      fi
      if have_unit "apt-daily-upgrade.timer"; then
        systemctl enable --now apt-daily-upgrade.timer >/dev/null 2>&1 || true
        Show 0 "Enabled apt-daily-upgrade.timer"
      else
        Show 3 "apt-daily-upgrade.timer not found"
      fi
      ;;

    dnf)
      # Try the more specific timers first, then the generic
      if ! enable_one_of "dnf-automatic" \
           "dnf-automatic-install.timer" \
           "dnf-automatic-notifyonly.timer" \
           "dnf-automatic-download.timer" \
           "dnf-automatic.timer"; then
        Show 3 "No dnf-automatic timer found (package may be missing)"
      fi
      ;;

    zypper)
      enable_one_of "zypper-automatic" "zypper-automatic.timer" \
        || Show 3 "zypper-automatic.timer not found (package may be missing)"
      ;;

    pacman)
      Show 3 "Arch Linux: auto-updates left disabled (managed by backend)."
      ;;
  esac
}


Ensure_LinuxIO_User() {
  if ! id linuxio >/dev/null 2>&1; then
    Show 2 "Creating system user 'linuxio'"
    if useradd --system linuxio; then
      Show 0 "Created system user linuxio"
    else
      Show 1 "Failed to create linuxio user"
    fi
  else
    Show 2 "System user 'linuxio' already exists"
  fi

  if getent group docker >/dev/null 2>&1; then
    if id -nG linuxio | tr ' ' '\n' | grep -qx docker; then
      Show 2 "User linuxio already in docker group"
    else
      usermod -aG docker linuxio && Show 0 "Added linuxio to docker group" || Show 3 "Could not add linuxio to docker group"
    fi
  fi
}

Clean_Up(){
  echo; Show 4 "\e[1mStarting Clean Up\e[0m"
  sed -i "/curl -fsSL[[:space:]]\+${SCRIPT_LINK//\//\\/}[[:space:]]\+|[[:space:]]\+sudo[[:space:]]\+bash/d" "$TARGET_HOME/.bashrc" 2>/dev/null || true
  rm -f "$TARGET_HOME/resume-after-reboot" || true
  Show 0 "Cleanup done"
}

Wrap_up_Banner() {
  echo
  Show 0 "\e[1mSETUP COMPLETE!\e[0m"
  echo -e "\n${GREEN_LINE}${aCOLOUR[1]}"
  echo -e " LinuxIO prerequisites installed.${COLOUR_RESET}"
  echo -e "${GREEN_LINE}"
  echo -e " PAM service: ${PAM_LINUXIO}"
  echo -e " Deny list : ${LINUXIO_DENY}"
  echo -e " Docker    : $(command -v docker >/dev/null 2>&1 && docker --version | awk '{print $3}' | tr -d , || echo not-installed)"
  echo -e "${COLOUR_RESET}"
  if [[ -n "${TARGET_USER:-}" ]]; then
    echo -e " NOTE: user '${TARGET_USER}' may need to re-login for docker group to take effect."
  fi
}

# ---------- Run ----------
Setup(){
  trap 'onCtrlC' INT
  Start
  Welcome_Banner

  if ! [ -f "$TARGET_HOME/resume-after-reboot" ]; then
    Update_System
    Reboot
  else
    Show 2 "Resuming script after reboot..."
  fi

  Install_Packages
  Enable_Core_Services
  Install_AutoUpdates
  Enable_AutoUpdates
  Ensure_LinuxIO_User

  Resolve_Release_Tag
  Download_Binaries
  Install_Binaries
  Resolve_Packaging_Ref
  Download_Packaging_Files
  Install_Packaging_Files

  Enable_LinuxIO_Systemd
  Cleanup_Staging
  Clean_Up
  Wrap_up_Banner
}

Setup
exit 0
