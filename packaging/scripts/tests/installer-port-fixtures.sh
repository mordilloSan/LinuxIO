#!/usr/bin/env bash
# Fixture-only checks for installer port handling and the mutable packaging
# asset policy. These tests source installer helpers and never touch /etc,
# systemd, services, or host sockets.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOCAL_INSTALLER="${SCRIPT_DIR}/../localinstall.sh"
RELEASE_INSTALLER="${SCRIPT_DIR}/../install-linuxio-binaries.sh"

fail() {
    echo "❌ $*" >&2
    exit 1
}

assert_eq() {
    local expected="$1"
    local actual="$2"
    local label="$3"

    [[ "$actual" == "$expected" ]] || fail "${label}: expected ${expected}, got ${actual}"
}

run_port_fixtures() {
    local installer="$1"

    (
        # shellcheck disable=SC1090
        source "$installer"

        socket_file=$(mktemp)
        trap 'rm -f "$socket_file"' EXIT

        printf '[Socket]\nListenStream=8094\n' > "$socket_file"
        assert_eq "8094" "$(extract_linuxio_socket_port "$socket_file")" \
            "port-only extraction"
        rewrite_linuxio_socket_port "$socket_file" 8097
        grep -Fqx 'ListenStream=8097' "$socket_file" || fail "port-only rewrite"

        printf '[Socket]\nListenStream=0.0.0.0:8095 # fixture\n' > "$socket_file"
        assert_eq "8095" "$(extract_linuxio_socket_port "$socket_file")" \
            "address-qualified extraction"
        rewrite_linuxio_socket_port "$socket_file" 8098
        grep -Fqx 'ListenStream=0.0.0.0:8098 # fixture' "$socket_file" || \
            fail "address-qualified rewrite"

        LINUXIO_EXISTING_SOCKET_FILE="$socket_file"
        assert_eq "8098" "$(find_existing_linuxio_port)" "existing port preservation"

        is_port_in_use() {
            case "$1" in
                8090|8091) return 0 ;;
                *) return 1 ;;
            esac
        }
        # linuxio_socket_owns_port reaches systemctl through these assertions;
        # stub it out so the host's real LinuxIO socket state cannot leak in.
        # The ownership cases below override this stub explicitly.
        systemctl() { return 1; }
        assert_eq "8092" "$(find_available_port)" "available-port selection"
        assert_eq "8092" "$(find_available_port 8091)" "occupied preferred-port fallback"
        assert_eq "8097" "$(find_available_port 8097)" "preferred-port selection"

        systemctl() {
            case "$1" in
                is-active) return 0 ;;
                show) printf '[::]:8096 (Stream)\n' ;;
                *) return 1 ;;
            esac
        }
        linuxio_socket_owns_port 8096 || fail "active LinuxIO listener ownership"
        if linuxio_socket_owns_port 8097; then
            fail "active LinuxIO listener must not claim a different port"
        fi

        is_port_in_use() { return 0; }
        assert_eq "8096" "$(find_available_port 8096)" "active LinuxIO port preservation"

        linuxio_socket_active() { return 1; }
        is_port_in_use() { return 0; }
        if find_available_port >/dev/null; then
            fail "selection should fail when every supported port is occupied"
        fi
    )
}

run_port_fixtures "$LOCAL_INSTALLER"
printf '   \033[1;32m✓\033[0m %s\n' "local installer port fixtures"
run_port_fixtures "$RELEASE_INSTALLER"
printf '   \033[1;32m✓\033[0m %s\n' "release installer port fixtures"

grep -Fq \
    'CURRENT_MAIN_PACKAGING_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging"' \
    "$RELEASE_INSTALLER" || fail "current-main packaging policy is not explicit"
if grep -Fq '/releases/download/${version}/packaging' "$RELEASE_INSTALLER"; then
    fail "packaging assets must not be forced to the release tag"
fi
printf '   \033[1;32m✓\033[0m %s\n' "recovery-asset packaging policy"

printf '\033[1;32m%s\033[0m\n' "✅ Installer port and recovery-asset fixtures passed!"
