#!/usr/bin/env bash
# =============================================================================
# LinuxIO Dependencies Installer
# Installs mandatory runtime dependencies and optionally installs extras
#  2025 Miguel Mariz (mordilloSan)
# =============================================================================
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive
trap 'echo -e "\e[0m"; exit 1' INT

# ---------- Colors & Styling ----------
readonly COLOUR_RESET='\e[0m'
readonly GREEN='\e[38;5;154m'
readonly BOLD='\e[1m'
readonly GREY='\e[90m'
readonly RED='\e[91m'
readonly YELLOW='\e[33m'

readonly LINE=" ${GREEN}───────────────────────────────────────────────────────${COLOUR_RESET}"
readonly BULLET=" ${GREEN}-${COLOUR_RESET}"

Show() {
    local status="$1"
    shift
    case "$status" in
        0) echo -e " ${GREY}[${GREEN}  OK  ${GREY}]${COLOUR_RESET} $*" ;;
        1) echo -e " ${GREY}[${RED}FAILED${GREY}]${COLOUR_RESET} $*"; exit 1 ;;
        2) echo -e " ${GREY}[${BOLD} INFO ${GREY}]${COLOUR_RESET} $*" ;;
        3) echo -e " ${GREY}[${YELLOW}NOTICE${GREY}]${COLOUR_RESET} $*" ;;
    esac
}

Header() {
    echo ""
    echo -e "${LINE}"
    echo -e " ${BOLD} $*${COLOUR_RESET}"
    echo -e "${LINE}"
    echo ""
}

# ---------- Distro Detection ----------
DISTRO=""
detect_distro() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        DISTRO="${ID:-unknown}"
    elif [[ -f /etc/debian_version ]]; then
        DISTRO="debian"
    elif [[ -f /etc/redhat-release ]]; then
        DISTRO="rhel"
    else
        DISTRO="unknown"
    fi
}

is_debian() {
    case "$DISTRO" in
        ubuntu|debian|linuxmint|pop) return 0 ;;
        *) return 1 ;;
    esac
}

is_fedora() {
    case "$DISTRO" in
        fedora|rhel|centos|rocky|almalinux) return 0 ;;
        *) return 1 ;;
    esac
}

# ---------- Package helpers ----------
# Check if a package is already installed
pkg_installed() {
    if is_debian; then
        dpkg -s "$1" &>/dev/null
    elif is_fedora; then
        rpm -q "$1" &>/dev/null
    fi
}

# Install packages quietly — stdout hidden, stderr captured for error reporting
pkg_install() {
    local err
    if is_debian; then
        err=$(apt-get install -y -qq "$@" 2>&1 >/dev/null) || { echo "$err" >&2; return 1; }
    elif is_fedora; then
        err=$(dnf install -y -q "$@" 2>&1 >/dev/null) || { echo "$err" >&2; return 1; }
    fi
}

# Install a named dependency. Tries each candidate in order until one
# succeeds. When every candidate fails, prints a NOTICE with the consequence
# (arg 4, optional) and returns 0 — install_pkg never aborts the script.
# Usage: install_pkg <display_name> <debian_pkg_candidates> <fedora_pkg_candidates> [consequence]
install_pkg() {
    local name="$1" deb_pkgs="$2" fed_pkgs="$3" consequence="${4:-feature disabled}"
    local pkgs="" candidate=""

    if is_debian; then pkgs="$deb_pkgs"
    elif is_fedora; then pkgs="$fed_pkgs"
    fi

    for candidate in $pkgs; do
        if pkg_installed "$candidate"; then
            Show 0 "${name} ${GREY}already installed${COLOUR_RESET}"
            return 0
        fi
    done

    for candidate in $pkgs; do
        Show 2 "Installing ${name} (${candidate})..."
        if pkg_install "$candidate" 2>/dev/null; then
            Show 0 "${name} installed (${candidate})"
            return 0
        fi
    done

    Show 3 "${name}: not available — ${consequence}"
}

# ---------- Mandatory Dependencies ----------
install_mandatory() {
    Header "Mandatory Dependencies"

    if ! is_debian && ! is_fedora; then
        Show 1 "Unsupported distribution: ${DISTRO}"
    fi

    if is_debian; then
        Show 2 "Updating package lists..."
        if ! apt-get update -qq >/dev/null 2>&1; then
            Show 1 "Failed to update package lists"
        fi
        Show 0 "Package lists updated"
    fi

    install_pkg "PAM libraries" "libpam0g" "pam"
    install_pkg "PolicyKit" "polkitd policykit-1" "polkit"
    install_pkg "PackageKit" "packagekit" "PackageKit"
}

# ---------- Optional Dependencies ----------
install_lm_sensors() {
    install_pkg "lm-sensors" "lm-sensors" "lm_sensors"
    if command -v sensors-detect &>/dev/null; then
        Show 2 "Detecting hardware sensors..."
        yes "" | sensors-detect --auto &>/dev/null || true
        Show 0 "Sensors configured"
    fi
}

install_smartmontools() {
    install_pkg "smartmontools" "smartmontools" "smartmontools"
}

install_nfs_client() {
    # Fedora 44+/Rawhide split client tools into nfs-client-utils; older
    # Fedora and RHEL-family ship them in unified nfs-utils.
    install_pkg "NFS client utilities" "nfs-common" "nfs-client-utils nfs-utils"
}

install_nfs_server() {
    # Server daemon (rpc.nfsd, exportfs) lives in nfs-utils on every Fedora
    # version, before and after the client/server split.
    install_pkg "NFS server utilities" "nfs-kernel-server" "nfs-utils"
}

install_tuned() {
    install_pkg "TuneD" "tuned" "tuned"
}

install_docker() {
    if command -v docker &>/dev/null; then
        Show 0 "Docker ${GREY}already installed ($(docker --version 2>/dev/null | sed 's/Docker version //'))${COLOUR_RESET}"
        return 0
    fi

    Show 2 "Installing Docker..."
    if ! curl -fsSL https://get.docker.com 2>/dev/null | sh >/dev/null 2>&1; then
        Show 3 "Docker installation failed"
        return 1
    fi

    if command -v systemctl &>/dev/null; then
        systemctl enable docker >/dev/null 2>&1
        systemctl start docker >/dev/null 2>&1
    fi

    Show 0 "Docker installed"
}

install_indexer() {
    if command -v indexer &>/dev/null; then
        Show 0 "Indexer ${GREY}already installed${COLOUR_RESET}"
        return 0
    fi

    Show 2 "Installing Indexer..."
    if ! curl -fsSL https://github.com/mordilloSan/indexer/releases/latest/download/indexer-install.sh 2>/dev/null | bash >/dev/null 2>&1; then
        Show 3 "Indexer installation failed"
        return 1
    fi

    Show 0 "Indexer installed"
}

install_all_optional() {
    Header "Optional Dependencies"

    install_lm_sensors
    install_smartmontools
    install_nfs_client
    install_nfs_server
    install_tuned
    install_docker
    install_indexer
}

# ---------- Optional prompt ----------
# Optional dependency catalogue. Parallel arrays so we can render a checklist
# without parsing colon-delimited records (descriptions contain spaces).
OPT_LABELS=(
    "lm-sensors"
    "smartmontools"
    "NFS client"
    "NFS server"
    "TuneD"
    "Docker"
    "Indexer"
)
OPT_DESCS=(
    "hardware temperature/voltage monitoring"
    "disk SMART health data"
    "mount and browse NFS shares from other hosts"
    "export NFS shares from this host"
    "power and performance profile management"
    "container management"
    "file search and directory size indexing"
)
OPT_FUNCS=(
    "install_lm_sensors"
    "install_smartmontools"
    "install_nfs_client"
    "install_nfs_server"
    "install_tuned"
    "install_docker"
    "install_indexer"
)

run_selected_optional() {
    local selected=("$@")
    local any=0
    Header "Optional Dependencies"
    local i
    for ((i=0; i<${#OPT_FUNCS[@]}; i++)); do
        if [[ ${selected[i]} -eq 1 ]]; then
            "${OPT_FUNCS[i]}"
            any=1
        fi
    done
    if [[ $any -eq 0 ]]; then
        Show 3 "No optional dependencies selected — you can install them later"
    fi
}

# Render a checklist with arrow-key navigation. Both the checklist and the
# yes/no fallback read from /dev/tty, so the interactive UI works even when
# the script is piped through `curl … | sudo bash` (stdin is the pipe, but
# the controlling terminal is still reachable as /dev/tty).
prompt_optional() {
    if [[ -r /dev/tty && -w /dev/tty ]]; then
        prompt_optional_checklist
    else
        # No TTY at all (CI, headless service): default to skipping. The
        # `--all` flag remains the way to install everything non-interactively.
        Show 3 "No TTY available — skipping optional dependencies (use --all to install everything)"
    fi
}

prompt_optional_checklist() {
    local n=${#OPT_LABELS[@]}
    local cursor=0
    local selected=()
    local i
    for ((i=0; i<n; i++)); do selected+=(1); done   # default: all selected

    # Hide cursor; restore on any exit path. Exit cleanly on Ctrl+C so the
    # script doesn't continue with a half-rendered checklist.
    tput civis
    trap 'tput cnorm; exit 130' INT TERM
    trap 'tput cnorm' EXIT

    local lines_per_render=$((n + 3))   # title + n rows + footer + blank
    local first_render=1
    local key rest

    while true; do
        if [[ $first_render -eq 0 ]]; then
            tput cuu "$lines_per_render"
        fi
        first_render=0

        echo ""
        printf "\r\033[K %b%s%b\n" \
            "$BOLD" "Optional dependencies — pick what to install:" "$COLOUR_RESET"
        for ((i=0; i<n; i++)); do
            local mark prefix
            if [[ ${selected[i]} -eq 1 ]]; then
                mark="${GREEN}[x]${COLOUR_RESET}"
            else
                mark="[ ]"
            fi
            if [[ $i -eq $cursor ]]; then
                prefix="${GREEN}>${COLOUR_RESET}"
            else
                prefix=" "
            fi
            printf "\r\033[K %b %b %-16s ${GREY}%s${COLOUR_RESET}\n" \
                "$prefix" "$mark" "${OPT_LABELS[i]}" "${OPT_DESCS[i]}"
        done
        printf "\r\033[K ${GREY}↑/↓ move · Space toggle · A all · N none · Enter confirm · Q skip${COLOUR_RESET}\n"

        IFS= read -rsn1 key </dev/tty
        if [[ $key == $'\x1b' ]]; then
            read -rsn2 -t 0.05 rest </dev/tty || rest=""
            key+="$rest"
        fi

        case $key in
            $'\x1b[A')  # Up
                (( cursor > 0 )) && cursor=$((cursor - 1))
                ;;
            $'\x1b[B')  # Down
                (( cursor < n - 1 )) && cursor=$((cursor + 1))
                ;;
            ' ')
                if [[ ${selected[cursor]} -eq 1 ]]; then
                    selected[cursor]=0
                else
                    selected[cursor]=1
                fi
                ;;
            a|A)
                for ((i=0; i<n; i++)); do selected[i]=1; done
                ;;
            n|N)
                for ((i=0; i<n; i++)); do selected[i]=0; done
                ;;
            ''|$'\n')
                break
                ;;
            q|Q)
                for ((i=0; i<n; i++)); do selected[i]=0; done
                break
                ;;
        esac
    done

    tput cnorm
    trap - EXIT INT TERM

    run_selected_optional "${selected[@]}"
}

# ---------- Main ----------
main() {
    local install_all=0

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -a|--all) install_all=1; shift ;;
            -h|--help) show_help; exit 0 ;;
            *) shift ;;
        esac
    done

    if [[ $EUID -ne 0 ]]; then
        Show 1 "This script must be run as root"
    fi

    Header "LinuxIO ${GREY}· Dependencies Installer${COLOUR_RESET}"

    detect_distro
    Show 2 "Detected distribution: ${BOLD}${DISTRO}${COLOUR_RESET}"

    install_mandatory

    if [[ $install_all -eq 1 ]]; then
        install_all_optional
    else
        prompt_optional
    fi

    echo ""
    echo -e "${LINE}"
    echo -e " ${GREEN}${BOLD}Installation complete!${COLOUR_RESET}"
    echo -e "${LINE}"
    echo ""
    echo -e " ${BOLD}Next step:${COLOUR_RESET} Install LinuxIO binaries with:"
    echo -e " ${GREY}curl -fsSL https://raw.githubusercontent.com/mordilloSan/LinuxIO/main/packaging/scripts/install-linuxio-binaries.sh | sudo bash${COLOUR_RESET}"
    echo ""
}

# ---------- Usage ----------
show_help() {
    cat <<EOF
Usage: $(basename "$0") [OPTIONS]

Installs dependencies required by LinuxIO.

Options:
  -a, --all     Install everything (mandatory + optional) without prompting
  -h, --help    Show this help message

Mandatory (installed automatically):
  - PAM libraries    (authentication)
  - PolicyKit        (authorization)
  - PackageKit       (software updates)

Optional (prompted interactively, or use --all):
  - lm-sensors       (hardware temperature/voltage monitoring)
  - smartmontools    (disk SMART health data)
  - NFS utilities    (mount/browse and export NFS shares)
  - TuneD            (power and performance profile management)
  - Docker           (container management)
  - Indexer          (file search and directory size indexing)

This script must be run as root.
EOF
}

main "$@"
