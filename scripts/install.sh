#!/usr/bin/bash
# Global Variables #
Start (){
    # SYSTEM INFO
    export DEBIAN_FRONTEND=noninteractive
    # shellcheck disable=SC1091
    source /etc/os-release
    DIST="$ID"
    readonly DIST
    UNAME_M="$(uname -m)"
    readonly UNAME_M
    UNAME_U="$(uname -s)"
    readonly UNAME_U
    WORK_DIR="/home/$(logname)"
    if [[ ! -d "$WORK_DIR" ]]; then
        mkdir "$WORK_DIR"
    fi

    # Packages (no backports target; plain apt install)
    readonly PACKAGES=(
        "iputils-ping"
        "lm-sensors"
        "nfs-kernel-server"
        "nfs-common"
        "wireguard-tools"
        "unattended-upgrades"
        "libpam0g"
        "libpam-modules"
        "libpam-modules-bin"
        "packagekit"
        "dbus"
        "ca-certificates"
    )

    # --- LinuxIO integration (no policykit) ---
    readonly LINUXIO_WEB_BIN="/usr/local/bin/linuxio-webserver"  # adjust if different
    readonly LINUXIO_SERVICE="linuxio-webserver.service"
    readonly PAM_LINUXIO="/etc/pam.d/linuxio"

    # COLORS
    readonly COLOUR_RESET='\e[0m'
    readonly aCOLOUR=(
        '\e[38;5;154m' # green      | Lines, bullets and separators
        '\e[1m'        # Bold white | Main descriptions
        '\e[90m'       # Grey       | Credits
        '\e[91m'       # Red        | Update notifications Alert
        '\e[33m'       # Yellow     | Emphasis
    )
    readonly GREEN_LINE="${aCOLOUR[0]}─────────────────────────────────────────────────────$COLOUR_RESET"

    # Script link (used for resume-after-reboot)
    readonly SCRIPT_LINK="https://raw.githubusercontent.com/mordilloSan/ubuntu/main/ubuntu-preconfig.sh"

    # Enable apt-get progress bar, create file if missing or key not present
    if [ ! -f /etc/apt/apt.conf.d/99fancy ] || ! grep -q "Progress-Fancy" "/etc/apt/apt.conf.d/99fancy"; then
        echo 'DPkg::Progress-Fancy "1";' >> /etc/apt/apt.conf.d/99fancy
    fi
}

# shellcheck disable=SC2317
onCtrlC() { 
    echo -e "${COLOUR_RESET}"
    exit 1
}

# Colors #
Show() {
    # OK
    if (($1 == 0)); then
        echo -e "${aCOLOUR[2]}[$COLOUR_RESET${aCOLOUR[0]}  OK  $COLOUR_RESET${aCOLOUR[2]}]$COLOUR_RESET $2"
    # FAILED
    elif (($1 == 1)); then
        echo -e "${aCOLOUR[2]}[$COLOUR_RESET${aCOLOUR[3]}FAILED$COLOUR_RESET${aCOLOUR[2]}]$COLOUR_RESET $2"
        exit 1
    # INFO
    elif (($1 == 2)); then
        echo -e "${aCOLOUR[2]}[$COLOUR_RESET${aCOLOUR[0]} INFO $COLOUR_RESET${aCOLOUR[2]}]$COLOUR_RESET $2"
    # NOTICE
    elif (($1 == 3)); then
        echo -e "${aCOLOUR[2]}[$COLOUR_RESET${aCOLOUR[4]}NOTICE$COLOUR_RESET${aCOLOUR[2]}]$COLOUR_RESET $2"
    # MENTION
    elif (($1 == 4)); then
        echo -e "${aCOLOUR[2]}[$COLOUR_RESET${aCOLOUR[0]}      $COLOUR_RESET${aCOLOUR[2]}]$COLOUR_RESET $2"
    fi
}

GreyStart() {
    echo -e "${aCOLOUR[2]}\c"
}

# Check Functions #
Check_Arch() {
    case $UNAME_M in
    *64*)
        Show 0 "Your hardware architecture is : \e[33m$UNAME_M\e[0m"
        ;;
    *)
        Show 1 "Aborted, unsupported or unknown architecture: \e[33m$UNAME_M\e[0m"
        exit 1
        ;;
    esac
}

Check_OS() {
    if [[ $UNAME_U == *Linux* ]]; then
        Show 0 "Your OS is : \e[33m$UNAME_U\e[0m"
    else
        Show 1 "This script is only for Linux."
        exit 1
    fi
}

Check_Distribution() {
    if [[ $DIST == *ubuntu* ]]; then
        Show 0 "Your Linux Distribution is : \e[33m$DIST\e[0m"
    else
        Show 1 "Aborted, installation is only supported in linux ubuntu."
        exit 1
    fi
}

Check_Permissions() {
    interpreter=$(ps -p $$ | awk '$1 != "PID" {print $(NF)}' | tr -d '()')
    if [ "$interpreter" != "bash" ]; then
        Show 1 "Please run with bash. (./ubuntu-preconfig.sh or bash ubuntu-preconfig.sh)"
        Show 1 "Current interpreter: \e[33m$interpreter\e[0m"
        exit 1
    fi
    if [[ "$EUID" != 0 ]]; then
        Show 1 "Please run as root or with sudo."
        exit 1
    fi
    Show 0 "Current interpreter : \e[33m$interpreter\e[0m"
}

Check_Connection(){
    internet=$(wget -q --spider http://google.com ; echo $?)
    if [ "$internet" != 0 ]; then
        Show 1 "No internet connection"
        exit 1
    fi
    Show 0 "Internet : \e[33mOnline\e[0m"
}

Check_Success(){
    if [[ $1 != 0 ]]; then
        Show 1 "$2 failed!"
        exit "$1"
    else
        Show 0 "$2 sucess!"
    fi
}

# Start Functions #
Welcome_Banner() {
    clear
    echo -e "\e[0m\c"
    set -e
    echo -e "${GREEN_LINE}${aCOLOUR[1]}"
    printf "\033[1mWelcome to the Ubuntu Preconfiguration Script.\033[0m\n"
    echo -e "${GREEN_LINE}${aCOLOUR[1]}"
    echo ""
    echo " This will update the system, install docker, install general tools,"
    echo " remove cloud-init and snapd, remove backup&temp files"
    echo ""
    echo -e "${GREEN_LINE}${aCOLOUR[1]}"
    Check_Arch
    Check_OS
    Check_Distribution
    Check_Permissions
    Check_Connection
    Show 2 "Current Working Directory - \e[33m$WORK_DIR\e[0m"
    echo -e "${GREEN_LINE}${aCOLOUR[1]}"
    echo ""
    echo "Are you sure you want to continue? [y/N]: "
    read -r response  </dev/tty # OR < /proc/$$/fd/0
    case $response in
        [yY]|[yY][eE][sS])
            echo
            ;;
        *)
            echo "Exiting..."
            exit 0
            ;;
    esac
    return 0
}

Update_System() {
    echo ""
    Show 4 "Updating System"
    Show 2 "Updating packages"
    GreyStart
    apt-get -qq update
    Check_Success $? "Package update"
    Show 2 "Upgrading packages"
    GreyStart
    apt-get -qq -y upgrade
    Check_Success $? "System Update"
}

Reboot(){
    if [ -f /var/run/reboot-required ] || [ -f /var/run/reboot-required.pkgs ]; then
        if [ -z "$(grep -h 'linux-image' /var/run/reboot-required* 2>/dev/null | sed -e 's/^linux-image-//')" ]; then
            Show 3 "System needs to be restarted for $(cat /var/run/reboot-required.pkgs)"
        else
            Show 3 "System needs to be restarted for new Kernel"
            echo "Current Kernel Version - $(uname -a | awk '{print "linux-image-"$3}' | sed -e "s/^linux-image-//")"
            echo "Available Kernel Version - $(grep -h 'linux-image' /var/run/reboot-required* | sed -e "s/^linux-image-//")"
        fi
        echo "Reboot system now? [y/N]: "
        read -r response  </dev/tty # OR < /proc/$$/fd/0
        case $response in
            [yY]|[yY][eE][sS])
                Show 4 "Preparing to reboot..."
                # create a flag file to signal that we are resuming from reboot.
                if ! [ -f ~/resume-after-reboot ]; then
                    touch ~/resume-after-reboot
                    Check_Success $? "Flag file to resume after reboot"
                fi
                # add the link to bashrc to start the script on login
                echo "curl -fsSL $SCRIPT_LINK | sudo bash" >> ~/.bashrc
                Check_Success $? "Setting up run script on boot"
                reboot </dev/tty
                ;;
        esac
    else
        Show 0 "No reboot required"
    fi
}

# Create /etc/pam.d/linuxio (overwrite; no backup) ---
Setup_PAM_LinuxIO() {
    echo ""
    Show 4 "\e[1mWriting /etc/pam.d/linuxio\e[0m"
    install -d -m 0755 /etc/pam.d

    # Desired PAM content
    read -r -d '' _PAM_CONTENT <<'PAM'
# Managed by LinuxIO - DO NOT EDIT
# Explicit, portable, minimal PAM stack for a non-TTY service.

auth      required  pam_unix.so
account   required  pam_unix.so
password  required  pam_unix.so
session   required  pam_unix.so
PAM

    # Write atomically, overwrite existing, do NOT backup
    printf "%s\n" "$_PAM_CONTENT" > "${PAM_LINUXIO}.tmp"
    chmod 0644 "${PAM_LINUXIO}.tmp"; chown root:root "${PAM_LINUXIO}.tmp"
    mv -f "${PAM_LINUXIO}.tmp" "${PAM_LINUXIO}"

    # If restorecon exists (SELinux relabel tool), apply label; '|| true' ignores failures
    command -v restorecon >/dev/null 2>&1 && restorecon -F "${PAM_LINUXIO}" || true

    Show 0 "PAM service ready at ${PAM_LINUXIO}"
}

# Package Section #
Install_Docker() {
    Show 2 "Installing \e[33mDocker\e[0m"
    if [[ -x "$(command -v docker)" ]]; then
        Docker_Version=$(docker version --format '{{.Server.Version}}')
        Show 0 "Current Docker verison is ${Docker_Version}."
    else
        Show 2 "Docker not installed. Installing."
        GreyStart
        curl -fsSL https://get.docker.com | bash
        Check_Docker_Install
    fi
}

Check_Docker_Install() {
    if [[ -x "$(command -v docker)" ]]; then
        Docker_Version=$(docker version --format '{{.Server.Version}}')
        Show 0 "Current Docker verison is ${Docker_Version}."
    else
        Show 1 "Installation failed, please uninstall docker"
    fi
}

Install_Packages() {
    echo ""
    Show 4 "\e[1mInstalling Packages\e[0m"
    Install_Docker
    for packagesNeeded in "${PACKAGES[@]}"; do
        Show 2 "Prepare the necessary dependencie: \e[33m$packagesNeeded\e[0m"
        if [ "$(dpkg-query -W -f='${Status}' "$packagesNeeded" 2>/dev/null | grep -c "ok installed")" -eq 0 ]; then
            Show 2 "$packagesNeeded not installed. Installing..."
            GreyStart
            apt-get install -y -qq "$packagesNeeded"
            Check_Success $? "$packagesNeeded installation"
        else
            Show 0 "$packagesNeeded already installed"
        fi
    done
}

Pihole_DNS(){
    echo ""
    Show 4 "\e[1mPreparing for Pihole\e[0m"
    Show 2 "Disabling stub resolver"
    GreyStart
    sed -r -i.orig 's/#?DNSStubListener=yes/DNSStubListener=no/g' /etc/systemd/resolved.conf
    Check_Success $? "Disabling stub resolver"
    Show 2 "Pointing symlink to /run/systemd/resolve/resolv.conf"
    sh -c 'rm /etc/resolv.conf && ln -s /run/systemd/resolve/resolv.conf /etc/resolv.conf'
    Check_Success $? "Pointing symlink"
    systemctl restart systemd-resolved
    Check_Success $? "Restarting systemd-resolved"
}

# Finish Section #
Remove_cloudinit(){
    Show 2 "Removing cloud-init"
    GreyStart
    if [ "$(dpkg-query -W -f='${Status}' "cloud-init" 2>/dev/null | grep -c "ok installed")" -eq 0 ]; then
        Show 0 "cloud-init not installed."
    else
        apt-get autoremove -q -y --purge cloud-init 
        Check_Success $? "Removing cloud-init"
        rm -rf /etc/cloud/
        rm -rf /var/lib/cloud/
    fi
}

Remove_snap(){
    Show 2 "Removing snap"
    local SNAP_LIST
    if [ "$(dpkg-query -W -f='${Status}' "snapd" 2>/dev/null | grep -c "ok installed")" -eq 0 ]; then
        Show 0 "snap not installed"
    else
        GreyStart
        systemctl disable snapd.socket
        systemctl disable snapd.service
        # Getting List of snaps installed
        SNAP_LIST=$(snap list | sed '1d' | grep -Eo '^[^ ]+')
        for i in $SNAP_LIST; do
            if [ "${i}" != "core" ] && [ "${i}" != "snapd" ] && [ "${i}" != "core22" ]; then
                snap remove --purge "$i"
            fi
        done
        SNAP_LIST=$(snap list | sed '1d' | grep -Eo '^[^ ]+')
        for i in $SNAP_LIST; do
            snap remove --purge "$i"
        done
        apt-get autoremove --purge snapd -y
        rm -rf /var/cache/snapd/
        rm -rf ~/snap
        Show 0 "snap removed"
    fi
}

Clean_Up(){
    echo ""
    Show 4 "\e[1mStarting Clean Up\e[0m"
    Remove_cloudinit
    Remove_snap
    # Remove the line that we added in bashrc
    sed -i "/curl -fsSL/d" ~/.bashrc
    Check_Success $? "Disabling Start script at boot"
    # remove the temporary file that we created to check for reboot
    rm -f ~/resume-after-reboot
}

Wrap_up_Banner() {
    echo -e ""
    Show 0 "\e[1mSETUP COMPLETE!\e[0m"
    echo -e ""
    echo -e "${GREEN_LINE}${aCOLOUR[1]}"
    echo -e " LinuxIO prerequisites installed.${COLOUR_RESET}"
    echo -e "${GREEN_LINE}"
    echo -e " PAM service: ${PAM_LINUXIO}"
    echo -e " Docker: $(docker --version 2>/dev/null || echo 'not installed')"
    echo -e "${COLOUR_RESET}"
}

# Execute Everything
Setup(){
    Start
    trap 'onCtrlC' INT
    Welcome_Banner
    # check if the resume flag file exists. 
    if ! [ -f ~/resume-after-reboot ]; then
        Update_System
        Reboot
    else
        Show 2 "Resuming script after reboot..."
        Setup_PAM_LinuxIO
    fi
    Install_Packages
    Reboot
    Pihole_DNS
    Clean_Up
    Wrap_up_Banner
}

Setup
exit 0
