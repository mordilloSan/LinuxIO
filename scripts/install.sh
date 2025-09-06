#!/usr/bin/env bash
# =============================================================================
# LinuxIO Preconfig & Installer (multi-distro)
# PAM, Docker, basics, NM/PackageKit enablement
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

is_systemd_pid1() {
  # true if systemd is PID 1 (required for systemctl to work normally)
  local pid1
  pid1="$(ps -p 1 -o comm= 2>/dev/null || true)"
  [[ "$pid1" == "systemd" ]]
}

find_unit_file() {
  # usage: find_unit_file <unit-name1> [<unit-name2> ...]
  # prints the FIRST matching unit name; returns 0 if found, 1 otherwise
  local u path

  # 1) Try systemctl catalogue first (services & sockets), only if systemd is PID 1
  if is_systemd_pid1 && command -v systemctl >/dev/null 2>&1; then
    # Pre-list once to avoid running systemctl per unit
    local listed
    listed="$(systemctl list-unit-files --type=service --type=socket --no-legend 2>/dev/null | awk '{print $1}')"
    for u in "$@"; do
      if printf '%s\n' "$listed" | grep -Fxq "$u"; then
        printf '%s\n' "$u"
        return 0
      fi
    done
  fi

  # 2) Fall back to checking common unit file paths directly
  for u in "$@"; do
    for path in \
      "/etc/systemd/system/$u" \
      "/lib/systemd/system/$u" \
      "/usr/lib/systemd/system/$u"
    do
      if [ -f "$path" ]; then
        printf '%s\n' "$u"
        return 0
      fi
    done
  done

  return 1
}

Check_Success() {
  # usage: Check_Success <rc> "message"
  if [[ ${1:-1} -ne 0 ]]; then
    Show 1 "${2:-Operation} failed!"
  else
    Show 0 "${2:-Operation} success!"
  fi
}

announce_init_system() {
  local pid1
  pid1="$(ps -p 1 -o comm= 2>/dev/null || echo "")"
  if [[ "$pid1" != "systemd" ]]; then
    Show 3 "Init system is '$pid1' (not systemd) — service enable/start will be limited."
  fi
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
  WORK_DIR="$(getent passwd "${SUDO_UID:-0}" | cut -d: -f6 2>/dev/null || echo /root)"; mkdir -p "$WORK_DIR"

  # Managed files
  readonly PAM_LINUXIO="/etc/pam.d/linuxio"
  readonly LINUXIO_DENY="/etc/linuxio/disallowed-users"
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
      PKGS_EXTRA=( unattended-upgrades )
      DOCKER_DEPS=()
      NM_PKG="network-manager"
      ;;
    dnf)
      PKGS_COMMON=( iputils lm_sensors nfs-utils wireguard-tools PackageKit ca-certificates jq curl rsync NetworkManager )
      PKGS_PAM=()
      PKGS_EXTRA=()
      DOCKER_DEPS=( dnf-plugins-core )
      NM_PKG="NetworkManager"
      ;;
    zypper)
      PKGS_COMMON=( iputils lm_sensors nfs-kernel-server nfs-client wireguard-tools PackageKit dbus-1 ca-certificates jq curl rsync NetworkManager )
      PKGS_PAM=()
      PKGS_EXTRA=()
      DOCKER_DEPS=()
      NM_PKG="NetworkManager"
      ;;
    pacman)
      PKGS_COMMON=( iputils lm_sensors nfs-utils wireguard-tools packagekit dbus ca-certificates jq curl rsync networkmanager )
      PKGS_PAM=()
      PKGS_EXTRA=()
      DOCKER_DEPS=()
      NM_PKG="networkmanager"
      ;;
  esac

  PACKAGES=( "${PKGS_COMMON[@]}" "${PKGS_PAM[@]}" "${PKGS_EXTRA[@]}" )
}

Check_Arch() {
  case $UNAME_M in *64*) Show 0 "Arch : \e[33m$UNAME_M\e[0m" ;; *) Show 1 "Unsupported architecture: \e[33m$UNAME_M\e[0m" ;; esac
}
Check_OS() {
  if [[ $UNAME_U == *Linux* ]]; then
    Show 0 "OS   : \e[33m$UNAME_U\e[0m"
  else
    Show 1 "This script is only for Linux."
  fi
}
Check_Distribution() { Show 2 "Distro: \e[33m$DIST${ID_LIKE:+ (ID_LIKE=$ID_LIKE)}\e[0m"; }
Check_Permissions() {
  local interpreter
  interpreter=$(ps -p $$ | awk '$1!="PID"{print $(NF)}' | tr -d '()')
  [[ "$interpreter" == "bash" ]] || Show 1 "Please run with bash. Current: $interpreter"
  [[ "$EUID" -eq 0 ]] || Show 1 "Please run as root or with sudo."
  Show 0 "Interpreter : \e[33m$interpreter\e[0m"
}
Check_Connection() {
  if wget -q --spider http://google.com; then
    Show 0 "Internet : \e[33mOnline\e[0m"
  else
    Show 1 "No internet connection"
  fi
}

Welcome_Banner() {
  clear
  echo -e "${GREEN_LINE}${aCOLOUR[1]}"
  printf "\033[1mWelcome to the LinuxIO Preconfiguration Script.\033[0m\n"
  echo -e "${GREEN_LINE}${aCOLOUR[1]}\n"
  echo " This will update the system, install Docker and common tools,"
  echo " set LinuxIO PAM , enable NetworkManager/PackageKit,"
  echo " (Debian/Ubuntu) remove cloud-init and snapd, and clean up."
  echo -e "\n${GREEN_LINE}${aCOLOUR[1]}"
  Check_Arch; Check_OS; Check_Distribution; Check_Permissions; Check_Connection
  announce_init_system
  Show 2 "Working Dir: \e[33m$WORK_DIR\e[0m"
  echo -e "${GREEN_LINE}${aCOLOUR[1]}\n"
  read -r -p "Are you sure you want to continue? [y/N]: " response </dev/tty
  case $response in [yY]|[yY][eE][sS]) : ;; *) echo "Exiting..."; exit 0;; esac
}

# ---------- Package ops ----------
pkg_update() {
  case "$PKG_FAMILY" in
    deb)    GreyStart; apt-get -qq update ;;
    dnf)    GreyStart; dnf -y -q makecache ;;
    zypper) GreyStart; zypper -q --non-interactive refresh ;;
    pacman) GreyStart; pacman -Sy --noconfirm >/dev/null ;;
  esac
  Check_Success $? "Package metadata update"
}
pkg_upgrade() {
  case "$PKG_FAMILY" in
    deb)    GreyStart; apt-get -qq -y upgrade ;;
    dnf)    GreyStart; dnf -y -q upgrade --refresh ;;
    zypper) GreyStart; zypper -q --non-interactive update ;;
    pacman) GreyStart; pacman -Su --noconfirm >/dev/null ;;
  esac
  Check_Success $? "System upgrade"
}
pkg_install() {
  case "$PKG_FAMILY" in
    deb)    GreyStart; apt-get install -y -qq "$@" ;;
    dnf)    GreyStart; dnf install -y -q "$@" ;;
    zypper) GreyStart; zypper -q --non-interactive install -y "$@" ;;
    pacman) GreyStart; pacman -S --needed --noconfirm "$@" ;;
  esac
  Check_Success $? "Installing: $*"
}
pkg_is_installed() {
  case "$PKG_FAMILY" in
    deb)    dpkg-query -W -f='${Status}\n' "$1" 2>/dev/null | grep -q "ok installed" ;;
    dnf)    rpm -q "$1" >/dev/null 2>&1 ;;
    zypper) rpm -q "$1" >/dev/null 2>&1 ;;
    pacman) pacman -Qi "$1" >/dev/null 2>&1 ;;
  esac
}

Update_System() { echo; Show 4 "Updating System"; Show 2 "Updating packages"; pkg_update; Show 2 "Upgrading packages"; pkg_upgrade; }

Reboot(){
  if [ -f /var/run/reboot-required ] || [ -f /var/run/reboot-required.pkgs ]; then
    Show 3 "System needs reboot for updates"
    read -r -p "Reboot now? [y/N]: " response </dev/tty
    case $response in
      [yY]|[yY][eE][sS])
        Show 4 "Preparing to reboot..."
        touch ~/resume-after-reboot || true
        printf '%s\n' "curl -fsSL $SCRIPT_LINK | sudo bash" | tee -a ~/.bashrc >/dev/null || true
        reboot </dev/tty
        ;;
    esac
  else
    Show 0 "No reboot required"
  fi
}

# ---------- PAM ----------
Setup_PAM_LinuxIO() {
  echo; Show 4 "\e[1mWriting /etc/pam.d/linuxio (distro-safe)\e[0m"
  install -d -m 0755 /etc/pam.d /etc/linuxio
  [[ -f "${LINUXIO_DENY}" ]] || { printf '%s\n' root >"${LINUXIO_DENY}.tmp"; chmod 0644 "${LINUXIO_DENY}.tmp"; chown root:root "${LINUXIO_DENY}.tmp"; mv -f "${LINUXIO_DENY}.tmp" "${LINUXIO_DENY}"; }

  # Choose include targets that exist
  AUTH_INC=""
  ACCT_INC=""
  PASS_INC=""
  SESS_INC=""

  if [[ -f /etc/pam.d/common-auth ]]; then
    AUTH_INC="include common-auth"
    ACCT_INC="include common-account"
    PASS_INC="include common-password"
    SESS_INC="include common-session-noninteractive"
  elif [[ -f /etc/pam.d/system-auth ]]; then
    AUTH_INC="include system-auth"
    ACCT_INC="include system-auth"
    PASS_INC="include system-auth"
    # RHEL/Fedora often split session into password-auth too, but system-auth is fine here
    SESS_INC="include system-auth"
  fi

  cat > "${PAM_LINUXIO}.tmp" <<PAM
#%PAM-1.0
# LinuxIO PAM stack (managed)
# AUTH
auth       [success=ok ignore=ignore module_unknown=ignore default=bad] pam_sepermit.so
${AUTH_INC:+auth       $AUTH_INC}
auth       [success=ok ignore=ignore module_unknown=ignore default=bad] pam_ssh_add.so optional
# ACCOUNT
account    required     pam_listfile.so item=user sense=deny file=/etc/linuxio/disallowed-users onerr=succeed
account    required     pam_nologin.so
${ACCT_INC:+account    $ACCT_INC}
# PASSWORD
${PASS_INC:+password   $PASS_INC}
# SESSION
session    [success=ok ignore=ignore module_unknown=ignore default=bad] pam_selinux.so close
session    required     pam_loginuid.so
session    [success=ok ignore=ignore module_unknown=ignore default=bad] pam_selinux.so open env_params
session    optional     pam_keyinit.so force revoke
session    [success=ok ignore=ignore module_unknown=ignore default=bad] pam_ssh_add.so optional
${SESS_INC:+session    $SESS_INC}
session    required     pam_env.so
session    required     pam_env.so user_readenv=1 envfile=/etc/default/locale
PAM

  chmod 0644 "${PAM_LINUXIO}.tmp"; chown root:root "${PAM_LINUXIO}.tmp"
  mv -f "${PAM_LINUXIO}.tmp" "${PAM_LINUXIO}"
  command -v restorecon >/dev/null 2>&1 && restorecon -F "${PAM_LINUXIO}" || true
  Show 0 "PAM service ready at ${PAM_LINUXIO}"
}


# ---------- Docker ----------
Install_Docker() {
  Show 2 "Installing \e[33mDocker\e[0m"
  if command -v docker >/dev/null 2>&1; then
    Docker_Version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "installed")
    Show 0 "Docker: ${Docker_Version}"
    return
  fi
  # Repo helpers for dnf
  if ((${#DOCKER_DEPS[@]})); then pkg_install "${DOCKER_DEPS[@]}"; fi
  Show 2 "Using Docker convenience script"
  GreyStart
  curl -fsSL https://get.docker.com | bash
  if command -v docker >/dev/null 2>&1; then
    Docker_Version=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "installed")
    Show 0 "Docker installed: ${Docker_Version}"
  else
    Show 1 "Docker installation failed"
  fi
}

# ---------- Packages ----------
Install_Packages() {
  echo; Show 4 "\e[1mInstalling Packages\e[0m"
  Install_Docker
  local TO_INSTALL=()
  local p
  for p in "${PACKAGES[@]}"; do
    if pkg_is_installed "$p"; then Show 0 "$p already installed"; else TO_INSTALL+=("$p"); fi
  done
  if ((${#TO_INSTALL[@]})); then pkg_install "${TO_INSTALL[@]}"; fi
}

# ---------- Auto updates across distros ----------
Enable_Auto_Updates() {
  case "$PKG_FAMILY" in
    deb)
      # Debian/Ubuntu: unattended-upgrades
      if ! pkg_is_installed unattended-upgrades; then
        pkg_install unattended-upgrades
      fi

      # Minimal, noninteractive enablement
      install -d -m 0755 /etc/apt/apt.conf.d
      cat > /etc/apt/apt.conf.d/20auto-upgrades <<'CONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Download-Upgradeable-Packages "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
Unattended-Upgrade::Automatic-Reboot "true";
CONF
      # Just in case the package wants reconfigure hooks
      if command -v dpkg-reconfigure >/dev/null 2>&1; then
        DEBIAN_FRONTEND=noninteractive dpkg-reconfigure -f noninteractive unattended-upgrades || true
      fi
      Show 0 "Automatic updates enabled via unattended-upgrades (Debian/Ubuntu)."
      ;;

    dnf)
      if command -v dnf5 >/dev/null 2>&1; then
        # DNF5
        pkg_is_installed dnf5-plugin-automatic || pkg_install dnf5-plugin-automatic
        # Ensure it *applies* updates (not just downloads)
        install -d -m 0755 /etc/dnf
        awk 'BEGIN{print "[commands]\napply_updates = yes"}' >/etc/dnf/automatic.conf
        systemctl enable --now dnf5-automatic.timer 2>/dev/null || true
        Show 0 "Automatic updates enabled via dnf5-automatic."
      else
        # DNF4
        pkg_is_installed dnf-automatic || pkg_install dnf-automatic
        # Default dnf-automatic also uses /etc/dnf/automatic.conf
        install -d -m 0755 /etc/dnf
        awk 'BEGIN{print "[commands]\napply_updates = yes"}' >/etc/dnf/automatic.conf
        systemctl enable --now dnf-automatic.timer 2>/dev/null || true
        Show 0 "Automatic updates enabled via dnf-automatic."
      fi
      ;;

    zypper)
      # openSUSE Leap/Tumbleweed: zypper-automatic (preferred) or YaST online update config
      if pkg_is_installed zypper-automatic; then
        systemctl enable --now zypper-automatic.timer 2>/dev/null || true
        Show 0 "Automatic updates enabled via zypper-automatic."
      else
        # Fallback to YaST Online Update (patches) if zypper-automatic not available
        if pkg_is_installed yast2-online-update-configuration || pkg_install yast2-online-update-configuration; then
          # Non-interactive defaults: weekly patches
          sed -i 's/^CHECK_ONLY=.*/CHECK_ONLY="no"/' /etc/sysconfig/online-update 2>/dev/null || true
          sed -i 's/^AUTO_ONLINE_UPDATE=.*/AUTO_ONLINE_UPDATE="yes"/' /etc/sysconfig/online-update 2>/dev/null || true
          systemctl enable --now online-update.timer 2>/dev/null || true
          Show 0 "Automatic updates enabled via YaST Online Update (patches)."
        else
          Show 3 "zypper-automatic not found and YaST config unavailable; skipping auto-updates."
        fi
      fi
      ;;

    pacman)
      # Arch: default to AUTO-DOWNLOAD; opt-in install with LINUXIO_PACMAN_AUTOINSTALL=1
      install -d -m 0755 /etc/systemd/system

      cat > /etc/systemd/system/pacman-download.service <<'UNIT'
[Unit]
Description=Download updated packages (pacman -Syuw)

[Service]
Type=oneshot
ExecStart=/usr/bin/pacman -Syuw --noconfirm
Nice=10
UNIT

      cat > /etc/systemd/system/pacman-download.timer <<'UNIT'
[Unit]
Description=Daily download of updated packages

[Timer]
OnBootSec=15min
OnUnitActiveSec=1d
Persistent=true

[Install]
WantedBy=timers.target
UNIT

      systemctl enable --now pacman-download.timer 2>/dev/null || true
      Show 0 "Automatic package download enabled (Arch)."

      if [[ "${LINUXIO_PACMAN_AUTOINSTALL:-0}" == "1" ]]; then
        cat > /etc/systemd/system/pacman-autoupgrade.service <<'UNIT'
[Unit]
Description=Unattended pacman upgrade (dangerous; read ArchWiki)

[Service]
Type=oneshot
Environment=LC_ALL=C
ExecStart=/usr/bin/pacman -Syu --noconfirm
Nice=10
UNIT

        cat > /etc/systemd/system/pacman-autoupgrade.timer <<'UNIT'
[Unit]
Description=Daily unattended pacman upgrade

[Timer]
OnBootSec=30min
OnUnitActiveSec=1d
Persistent=true

[Install]
WantedBy=timers.target
UNIT
        systemctl enable --now pacman-autoupgrade.timer 2>/dev/null || true
        Show 3 "Enabled unattended pacman upgrades (YOU opted in). Be aware this is not recommended by Arch."
      else
        Show 3 "Arch: only downloads by default. Set LINUXIO_PACMAN_AUTOINSTALL=1 to auto-install (not recommended)."
      fi
      ;;

    *)
      Show 3 "Auto-updates not supported for PKG_FAMILY=$PKG_FAMILY"
      ;;
  esac
}

# ---------- NetworkManager enable + Ubuntu netplan override ----------
Ensure_NetworkManager() {
  Show 2 "Ensuring NetworkManager is installed & active (for D‑Bus control)"
  if ! pkg_is_installed "$NM_PKG"; then
    pkg_install "$NM_PKG"
  fi

  # If systemd is not PID 1, we can't manage units; just drop the netplan override on Ubuntu
  if ! is_systemd_pid1; then
    if [[ "$DIST" == "ubuntu" ]]; then
      ensure_networkmanager_renderer_ubuntu
    fi
    Show 3 "systemd is not PID 1 — cannot manage NetworkManager service with systemctl (container/WSL/chroot?)."
    Show 2 "You can start NM manually (foreground) with: /usr/sbin/NetworkManager --no-daemon"
    return 0
  fi

  # Prefer real unit names; avoid non-installable aliases
  local NM_UNIT
  NM_UNIT="$(find_unit_file "NetworkManager.service" "network-manager.service")" || {
    Show 3 "No NetworkManager unit file found; continuing."
    return 0
  }

  # Avoid conflicts with networkd
  systemctl disable --now systemd-networkd.service systemd-networkd-wait-online.service 2>/dev/null || true

  # Ubuntu/netplan override (safe, minimal)
  if [[ "$DIST" == "ubuntu" ]]; then
    ensure_networkmanager_renderer_ubuntu
  fi

  systemctl daemon-reload || true
  systemctl unmask "$NM_UNIT" 2>/dev/null || true

  # If the unit is static, don't 'enable' it — just start/restart
  if systemctl is-enabled "$NM_UNIT" 2>/dev/null | grep -qx static; then
    systemctl start "$NM_UNIT" || true
  else
    systemctl enable --now "$NM_UNIT" || true
  fi
  systemctl restart "$NM_UNIT" || true

  if systemctl is-active --quiet "$NM_UNIT"; then
    Show 0 "NetworkManager is running"
  else
    Show 3 "NetworkManager installed but not running (non‑fatal)"
    Show 2 "Hint: journalctl -u $NM_UNIT -b --no-pager"
  fi
}

ensure_networkmanager_renderer_ubuntu() {
  if ! command -v netplan >/dev/null 2>&1; then
    Show 2 "Netplan not present; skipping renderer change."
    return 0
  fi
  install -d -m 0755 /etc/netplan
  local fn="/etc/netplan/10-linuxio-networkmanager.yaml"
  cat > "$fn.tmp" <<'YAML'
# Managed by LinuxIO installer: prefer NetworkManager as renderer
network:
  version: 2
  renderer: NetworkManager
YAML
  chmod 0600 "$fn.tmp"; chown root:root "$fn.tmp"
  mv -f "$fn.tmp" "$fn"

  # Make sure resolved & resolv.conf are coherent (unless Pi-hole section changes later)
  if systemctl list-unit-files | grep -q '^systemd-resolved\.service'; then
    ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf || true
    systemctl enable --now systemd-resolved || true
  fi

  if netplan generate; then
    if ! netplan apply; then
      netplan try || true
    fi
    Show 0 "Netplan set to NetworkManager"
  else
    Show 3 "netplan generate failed; not applying"
  fi
}

# ---------- PackageKit service (updates via D‑Bus) ----------
Ensure_PackageKit() {
  local PK_SVC PK_SOCK
  PK_SVC="$(find_unit_file "packagekit.service" || true)"
  PK_SOCK="$(find_unit_file "packagekit.socket"  || true)"

  if ! is_systemd_pid1; then
    Show 3 "systemd is not PID 1 — skipping PackageKit unit management."
    return 0
  fi

  if [[ -n "$PK_SVC" ]]; then
    if systemctl is-enabled "$PK_SVC" 2>/dev/null | grep -qx static; then
      systemctl start "$PK_SVC" || true
      Show 0 "PackageKit service started (static unit)"
    else
      systemctl enable --now "$PK_SVC" || true
      Show 0 "PackageKit service enabled"
    fi
  elif [[ -n "$PK_SOCK" ]]; then
    if systemctl is-enabled "$PK_SOCK" 2>/dev/null | grep -qx static; then
      systemctl start "$PK_SOCK" || true
      Show 0 "PackageKit socket started (static unit)"
    else
      systemctl enable --now "$PK_SOCK" || true
      Show 0 "PackageKit socket enabled"
    fi
  else
    Show 2 "PackageKit unit files not found (may be on‑demand or not provided)."
  fi
}


Clean_Up(){
  echo; Show 4 "\e[1mStarting Clean Up\e[0m"
  sed -i "/curl -fsSL/d" ~/.bashrc || true
  rm -f ~/resume-after-reboot || true
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
  echo -e "${COLOUR_RESET}"
}

# ---------- Run ----------
Setup(){
  trap 'onCtrlC' INT
  Start
  Welcome_Banner

  if ! [ -f ~/resume-after-reboot ]; then
    Update_System

    Reboot
  else
    Show 2 "Resuming script after reboot..."
  fi

  Install_Packages
  
  Enable_Auto_Updates
  Ensure_NetworkManager
  Ensure_PackageKit

  Setup_PAM_LinuxIO

  Clean_Up
  Wrap_up_Banner
}

Setup
exit 0
