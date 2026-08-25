#!/usr/bin/env bash
# Focused, host-independent coverage for install-dependencies.sh.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER="${SCRIPT_DIR}/install-dependencies.sh"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT

fail() {
    echo "❌ $*" >&2
    exit 1
}

assert_contains() {
    local haystack="$1" needle="$2"
    [[ "$haystack" == *"$needle"* ]] || fail "expected output to contain: ${needle}"
}

assert_not_contains() {
    local haystack="$1" needle="$2"
    [[ "$haystack" != *"$needle"* ]] || fail "unexpected output: ${needle}"
}

assert_file_contains() {
    local file="$1" needle="$2"
    grep -Fq -- "$needle" "$file" || fail "expected ${file} to contain: ${needle}"
}

test_package_mappings() {
    local debian_log="${TEST_DIR}/debian-mapping.log"
    local rhel_log="${TEST_DIR}/rhel-mapping.log"

    (
        source "$INSTALLER"
        DISTRO=debian
        apt-get() { [[ "$1" == update ]]; }
        install_pkg() { printf '%s|%s|%s\n' "$1" "$2" "$3" >>"$debian_log"; }
        install_mandatory
    ) >/dev/null

    assert_file_contains "$debian_log" "PAM libraries|libpam0g|pam"
    assert_file_contains "$debian_log" "PolicyKit|polkitd policykit-1|polkit"
    assert_file_contains "$debian_log" "PackageKit|packagekit|PackageKit"

    (
        source "$INSTALLER"
        DISTRO=rhel
        install_pkg() { printf '%s|%s|%s\n' "$1" "$2" "$3" >>"$rhel_log"; }
        install_mandatory
    ) >/dev/null

    assert_file_contains "$rhel_log" "PAM libraries|libpam0g|pam"
    assert_file_contains "$rhel_log" "PolicyKit|polkitd policykit-1|polkit"
    assert_file_contains "$rhel_log" "PackageKit|packagekit|PackageKit"
}

test_mandatory_failure() {
    local output

    if output=$(
        source "$INSTALLER"
        DISTRO=debian
        apt-get() { :; }
        pkg_installed() {
            case "$1" in
                libpam0g|polkitd) return 0 ;;
                *) return 1 ;;
            esac
        }
        pkg_install() { return 1; }
        install_mandatory
    ) 2>&1; then
        fail "mandatory dependency failure unexpectedly succeeded"
    fi

    assert_contains "$output" "PackageKit: installation failed"
    assert_contains "$output" "PackageKit is required for in-app capability installation"
}

run_stubbed_main() {
    local log="$1"
    shift
    source "$INSTALLER"
    require_root() { :; }
    detect_distro() { DISTRO=debian; }
    install_mandatory() { echo mandatory >>"$log"; }
    main "$@"
}

test_cli_mode() {
    local default_log="${TEST_DIR}/default.log"
    local default_output removed_output

    : >"$default_log"
    default_output="$(run_stubbed_main "$default_log")"
    assert_file_contains "$default_log" mandatory
    assert_contains "$default_output" "Optional capabilities are installed later in LinuxIO Capability Manager"

    if removed_output=$("$INSTALLER" --all 2>&1); then
        fail "removed --all option unexpectedly succeeded"
    fi
    assert_contains "$removed_output" "Unknown option: --all"
}

test_help() {
    local output installer_source
    output="$("$INSTALLER" --help)"
    assert_contains "$output" "PackageKit"
    assert_contains "$output" "Capability Manager"
    assert_contains "$output" "Docker is not installed by this script"
    assert_not_contains "$output" "--all"
    assert_not_contains "$output" "Optional (prompted interactively"
    assert_not_contains "$output" "lm-sensors"

    installer_source="$(<"$INSTALLER")"
    assert_not_contains "$installer_source" "get.docker.com"
    assert_not_contains "$installer_source" "install_docker"
}

test_obsolete_dev_updater_is_removed() {
    local term matches obsolete_script
    local obsolete_terms=(
        "dev-test-"update".sh"
        "SCRIPT_SERVER_"PORT
        "SCRIPT_SERVER_"PID
        ".script-"server".pid"
    )

    obsolete_script="${SCRIPT_DIR}/dev-test-"update".sh"
    [[ ! -e "$obsolete_script" ]] ||
        fail "obsolete development updater still exists"
    for term in "${obsolete_terms[@]}"; do
        if matches=$(git -C "$REPO_ROOT" grep -n -F "$term" -- . \
            ':(exclude)docs/TODO/installation-capability-cleanup.md'); then
            fail "obsolete development-updater reference remains: ${matches}"
        fi
    done
}

run_test() {
    local name="$1"
    shift
    "$@"
    printf '   \033[1;32m✓\033[0m %s\n' "${name}"
}

run_test "Debian and RHEL package mappings" test_package_mappings
run_test "mandatory dependency failures are fatal" test_mandatory_failure
run_test "mandatory-only CLI behavior" test_cli_mode
run_test "help text" test_help
run_test "obsolete development updater is removed" test_obsolete_dev_updater_is_removed
echo "✅ All install-dependencies fixture tests passed!"
