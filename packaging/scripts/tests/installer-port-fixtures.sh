#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2034,SC2329
# Fixture-only checks for installer port handling and the mutable packaging
# asset policy. These tests source installer helpers and never touch /etc,
# systemd, services, or host sockets.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
LOCAL_INSTALLER="${SCRIPT_DIR}/../localinstall.sh"
RELEASE_INSTALLER="${SCRIPT_DIR}/../install-linuxio-binaries.sh"
UNINSTALLER="${SCRIPT_DIR}/../uninstall.sh"

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

assert_line() {
	local expected="$1"
	local lines="$2"
	local label="$3"

	printf '%s\n' "$lines" | grep -Fqx "$expected" || fail "${label}: missing ${expected}"
}

run_release_architecture_fixtures() {
	(
		# shellcheck disable=SC1090
		source "$RELEASE_INSTALLER"
		LINUXIO_RELEASE_ARCH_OVERRIDE=x86_64 release_architecture_supported ||
			fail "x86_64 must be supported by the release installer"
		LINUXIO_RELEASE_ARCH_OVERRIDE=amd64 release_architecture_supported ||
			fail "amd64 must be supported by the release installer"
		if LINUXIO_RELEASE_ARCH_OVERRIDE=aarch64 release_architecture_supported; then
			fail "aarch64 must not be supported by the release installer"
		fi
	)
}

run_release_integrity_fixtures() {
	(
		# shellcheck disable=SC1090
		source "$RELEASE_INSTALLER"
		mkdir -p "$STAGING"
		printf 'known release asset\n' >"${STAGING}/linuxio-indexer"
		printf '%s  %s\n' "$(sha256sum "${STAGING}/linuxio-indexer" | awk '{print $1}')" linuxio-indexer >"${STAGING}/SHA256SUMS"
		verify_checksums >/dev/null

		printf 'tampered release asset\n' >"$STAGING/linuxio-indexer"
		if (verify_checksums >/dev/null 2>&1); then
			fail "checksum verification accepted a tampered asset"
		fi
	)
}

run_atomic_replacement_fixture() {
	local installer="$1"
	(
		# shellcheck disable=SC1090
		source "$installer"
		local fixture_dir destination source_file
		fixture_dir=$(mktemp -d)
		destination="${fixture_dir}/install/bin/linuxio-indexer"
		source_file="${fixture_dir}/linuxio-indexer.new"
		printf 'new release\n' >"$source_file"
		atomic_replace_file "$source_file" "$destination" 0644
		[[ -d "${fixture_dir}/install/bin" ]] || fail "atomic replacement did not create the destination directory"
		grep -Fqx 'new release' "$destination" || fail "atomic replacement did not install the new asset"
		if compgen -G "${destination}.new.*" >/dev/null; then
			fail "atomic replacement left a temporary asset"
		fi
		if atomic_replace_file "${fixture_dir}/missing" "$destination" 0644 2>/dev/null; then
			fail "atomic replacement accepted a missing source"
		fi
		grep -Fqx 'new release' "$destination" || fail "failed replacement damaged the installed asset"
		rm -rf "$fixture_dir"
	)
}

run_release_architecture_fixtures
printf '   \033[1;32m✓\033[0m %s\n' "release architecture guard"
(
	# shellcheck disable=SC1090
	source "$RELEASE_INSTALLER"
	release_version_supported v0.27.0 || fail "v0.27.0 must be supported"
	release_version_supported v0.27.1 || fail "v0.27.1 must be supported"
	release_version_supported v1.0.0 || fail "v1.0.0 must be supported"
	if release_version_supported v0.26.0; then
		fail "v0.26.0 must be rejected before downloads"
	fi
)
printf '   \033[1;32m✓\033[0m %s\n' "first indexer release floor"
run_release_integrity_fixtures
printf '   \033[1;32m✓\033[0m %s\n' "release checksum verification"
run_atomic_replacement_fixture "$RELEASE_INSTALLER"
run_atomic_replacement_fixture "$LOCAL_INSTALLER"
printf '   \033[1;32m✓\033[0m %s\n' "release and local atomic replacement"

for installer in "$LOCAL_INSTALLER" "$RELEASE_INSTALLER"; do
	doc_dir=$(bash -c "source '$installer'; printf '%s\\n' \"\$DOC_DIR\"")
	assert_eq "/usr/share/linuxio/doc" "$doc_dir" "${installer} legacy-compatible license directory"
	if [[ "$installer" == "$LOCAL_INSTALLER" ]]; then
		license_line=$(grep -n -m1 '^[[:space:]]*Show 2 "Installing licenses' "$installer" | cut -d: -f1)
		binary_line=$(grep -n -m1 '^[[:space:]]*Show 2 "Installing binaries' "$installer" | cut -d: -f1)
	else
		license_line=$(grep -n -m1 '^[[:space:]]*if ! install_license_files; then' "$installer" | cut -d: -f1)
		binary_line=$(grep -n -m1 '^[[:space:]]*if ! install_binaries; then' "$installer" | cut -d: -f1)
	fi
	[[ -n "$license_line" && -n "$binary_line" ]] || fail "${installer} install ordering could not be resolved"
	((license_line < binary_line)) || fail "${installer} replaces binaries before licenses are safely installed"
done
printf '   \033[1;32m✓\033[0m %s\n' "licenses precede binary replacement"

run_port_fixtures() {
	local installer="$1"

	(
		# shellcheck disable=SC1090
		source "$installer"

		socket_file=$(mktemp)
		trap 'rm -f "$socket_file"' EXIT

		printf '[Socket]\nListenStream=8094\n' >"$socket_file"
		assert_eq "8094" "$(extract_linuxio_socket_port "$socket_file")" \
			"port-only extraction"
		rewrite_linuxio_socket_port "$socket_file" 8097
		grep -Fqx 'ListenStream=8097' "$socket_file" || fail "port-only rewrite"

		printf '[Socket]\nListenStream=0.0.0.0:8095 # fixture\n' >"$socket_file"
		assert_eq "8095" "$(extract_linuxio_socket_port "$socket_file")" \
			"address-qualified extraction"
		rewrite_linuxio_socket_port "$socket_file" 8098
		grep -Fqx 'ListenStream=0.0.0.0:8098 # fixture' "$socket_file" ||
			fail "address-qualified rewrite"

		LINUXIO_EXISTING_SOCKET_FILE="$socket_file"
		assert_eq "8098" "$(find_existing_linuxio_port)" "existing port preservation"

		is_port_in_use() {
			case "$1" in
			8090 | 8091) return 0 ;;
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

for installer in "$LOCAL_INSTALLER" "$RELEASE_INSTALLER"; do
	if [[ "$installer" == "$LOCAL_INSTALLER" ]]; then
		binary_list=$(bash -c "source '$installer'; linuxio_binary_names")
		unit_list=$(bash -c "source '$installer'; linuxio_systemd_units")
	else
		binary_list=$(bash -c "source '$installer'; release_binary_names")
		unit_list=$(bash -c "source '$installer'; release_systemd_units")
	fi
	assert_line linuxio-indexer "$binary_list" "${installer} binary list"
	assert_line linuxio-indexer.socket "$unit_list" "${installer} unit list"
	assert_line linuxio-indexer.service "$unit_list" "${installer} unit list"
	assert_line linuxio-indexer-index.service "$unit_list" "${installer} unit list"
	assert_line linuxio-indexer-index.timer "$unit_list" "${installer} unit list"
	if grep -Eq 'sync_indexer_tcp_listener|--tcp-listener-only|linuxio-indexer config' "$installer"; then
		fail "${installer} still mutates indexer runtime configuration"
	fi
	grep -Fq 'THIRD_PARTY_NOTICES.md' "$installer" || fail "${installer} does not install third-party notices"
	grep -Fq 'config.yaml' "$installer" || fail "${installer} does not install YAML configuration"
	grep -Fq 'systemctl enable "$INDEXER_TIMER_UNIT_NAME"' "$installer" || fail "${installer} does not enable the timer on first install"
	if grep -Fq 'indexer.json' "$installer"; then
		fail "${installer} still references removed indexer.json"
	fi
	for unit in linuxio-indexer.socket linuxio-indexer.service linuxio-indexer-index.service linuxio-indexer-index.timer; do
		grep -Fq "$unit" "$installer" || fail "${installer} does not install ${unit}"
		grep -Fqx 'PartOf=linuxio.target' "${REPO_ROOT}/packaging/systemd/${unit}" || fail "${unit} is not part of linuxio.target"
	done
done

INDEXER_SOCKET="${REPO_ROOT}/packaging/systemd/linuxio-indexer.socket"
INDEXER_SERVICE="${REPO_ROOT}/packaging/systemd/linuxio-indexer.service"
INDEXER_TIMER="${REPO_ROOT}/packaging/systemd/linuxio-indexer-index.timer"
INDEXER_TARGET="${REPO_ROOT}/packaging/systemd/linuxio.target"
WEBSERVER_SERVICE="${REPO_ROOT}/packaging/systemd/linuxio-webserver.service"
grep -Fqx 'SocketMode=0600' "$INDEXER_SOCKET" || fail "indexer socket must be root-only"
grep -Fqx 'SocketGroup=root' "$INDEXER_SOCKET" || fail "indexer socket group"
grep -Fqx 'ListenStream=/run/linuxio/indexer.sock' "$INDEXER_SOCKET" || fail "indexer socket path"
if grep -Fqx 'PrivateTmp=true' "$INDEXER_SERVICE"; then
	fail "indexer service must share the host temporary filesystem"
fi
grep -Fqx 'Environment=SQLITE_TMPDIR=/var/lib/linuxio/indexer' "$INDEXER_SERVICE" ||
	fail "indexer service must keep SQLite scratch in its state directory"
grep -Fqx 'RestrictAddressFamilies=AF_UNIX' "$INDEXER_SERVICE" || fail "indexer service must be Unix-only"
if grep -Eq 'linuxio-webserver.socket|linuxio-indexer-tcp.socket' "$INDEXER_SERVICE"; then
	fail "indexer service must depend only on its Unix activation socket"
fi
grep -Fqx 'ExecStart=/usr/local/bin/linuxio-indexer --config-file /etc/linuxio/indexer/config.yaml' "$INDEXER_SERVICE" || fail "indexer service must start the managed daemon directly"
grep -Fqx 'ExecStart=/usr/local/bin/linuxio-indexer --trigger-index --socket-path /run/linuxio/indexer.sock' \
	"${REPO_ROOT}/packaging/systemd/linuxio-indexer-index.service" || fail "index timer must use the private socket trigger"
if rg -q 'linuxio-indexer-socket-user|INDEXER_SOCKET_GROUP' \
	"${REPO_ROOT}/packaging/systemd" "${REPO_ROOT}/backend/auth"; then
	fail "indexer socket access must not modify the auth launcher"
fi
grep -Fqx 'BindsTo=linuxio-webserver.socket' "$INDEXER_SOCKET" || fail "indexer socket lifecycle"
grep -Fq 'Wants=linuxio-webserver.socket linuxio-auth.socket linuxio-indexer.socket' "$INDEXER_TARGET" || fail "linuxio.target does not own the indexer socket"
if grep -Eq '^Wants=.*linuxio-indexer-index.timer' "$INDEXER_TARGET"; then
	fail "linuxio.target must not bypass the timer's enabled state"
fi
grep -Fqx 'WantedBy=linuxio.target' "${REPO_ROOT}/packaging/systemd/linuxio-indexer-index.timer" || fail "index timer install relationship"
grep -Fqx 'OnActiveSec=1h' "$INDEXER_TIMER" || fail "index timer initial delay must match its default interval"
grep -Fqx 'OnUnitActiveSec=1h' "$INDEXER_TIMER" || fail "index timer default recurring interval"
if grep -Fqx 'Persistent=true' "${REPO_ROOT}/packaging/systemd/linuxio-indexer-index.timer"; then
	fail "Persistent has no effect on a monotonic timer"
fi
grep -Fq 'Wants=linuxio-issue.service linuxio-indexer.socket' "${REPO_ROOT}/packaging/systemd/linuxio-webserver.socket" || fail "webserver socket warm dependency"
grep -Fqx 'Wants=linuxio-indexer.service' "$WEBSERVER_SERVICE" || fail "webserver service must warm the indexer"
grep -Eq '^After=.*linuxio-indexer\.service' "$WEBSERVER_SERVICE" || fail "webserver service must start after the indexer"
grep -Fqx 'RuntimeDirectory=linuxio/webserver' "$WEBSERVER_SERVICE" || fail "webserver runtime marker"
if grep -q '^\[Install\]' "$INDEXER_SERVICE"; then
	fail "indexer service must be socket-activated only"
fi
if grep -Fq 'linuxio-indexer.service' "$INDEXER_TARGET"; then
	fail "linuxio.target must not start the indexer service"
fi
[[ -f "${REPO_ROOT}/packaging/etc/linuxio/indexer/config.yaml" ]] || fail "missing packaged indexer config"
if rg -qv '^(exclude_paths:|  - |include_network_mounts:|[[:space:]]*$|#)' "${REPO_ROOT}/packaging/etc/linuxio/indexer/config.yaml"; then
	fail "indexer YAML contains non-scan policy"
fi
grep -Fq -- '--remove-data' "$UNINSTALLER" || fail "uninstaller must preserve data by default"
grep -Fq 'if [[ $REMOVE_DATA -eq 1 ]]; then' "$UNINSTALLER" || fail "uninstaller remove-data branch missing"
grep -Fq 'Persistent data preserved' "$UNINSTALLER" || fail "uninstaller preserve-data behavior missing"
grep -Fq 'rel_path" == "indexer/config.yaml"' "$LOCAL_INSTALLER" || fail "local installer must preserve indexer YAML"
grep -Fq 'DATA_DIR="/var/lib/linuxio"' "$UNINSTALLER" || fail "uninstaller must account for persistent index data"
[[ -f "${REPO_ROOT}/docs/THIRD_PARTY_NOTICES.md" ]] || fail "third-party notices must live under docs"
printf '   \033[1;32m✓\033[0m %s\n' "first-party indexer packaging policy"

printf '\033[1;32m%s\033[0m\n' "✅ Installer and recovery-asset fixtures passed!"
