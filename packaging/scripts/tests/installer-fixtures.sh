#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2034,SC2329
# Fixture-only checks for the installer (port handling, the mutable packaging
# asset policy, and that its binary and unit lists match the Makefile and
# packaging/systemd). These tests source installer helpers and never touch
# /etc, systemd, services, or host sockets.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
LOCAL_WRAPPER="${SCRIPT_DIR}/../localinstall.sh"
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
	release_version_supported v0.28.0 || fail "v0.28.0 must be supported"
	release_version_supported v0.28.1 || fail "v0.28.1 must be supported"
	release_version_supported v1.0.0 || fail "v1.0.0 must be supported"
	if release_version_supported v0.27.0; then
		fail "v0.27.0 must be rejected before downloads"
	fi
)
printf '   \033[1;32m✓\033[0m %s\n' "first monitoring release floor"
run_release_integrity_fixtures
printf '   \033[1;32m✓\033[0m %s\n' "release checksum verification"
run_atomic_replacement_fixture "$RELEASE_INSTALLER"
printf '   \033[1;32m✓\033[0m %s\n' "atomic replacement"

grep -Fq -- 'install-linuxio-binaries.sh" --local "$@"' "$LOCAL_WRAPPER" ||
	fail "localinstall.sh must be a thin wrapper around install-linuxio-binaries.sh --local"
if [[ $(wc -l <"$LOCAL_WRAPPER") -gt 12 ]]; then
	fail "localinstall.sh must stay a thin wrapper"
fi
printf '   \033[1;32m✓\033[0m %s\n' "local installer is a thin wrapper"

installer="$RELEASE_INSTALLER"
doc_dir=$(bash -c "source '$installer'; printf '%s\\n' \"\$DOC_DIR\"")
assert_eq "/usr/share/linuxio/doc" "$doc_dir" "legacy-compatible license directory"
license_line=$(grep -n -m1 '^[[:space:]]*if ! install_license_files; then' "$installer" | cut -d: -f1)
binary_line=$(grep -n -m1 '^[[:space:]]*if ! install_binaries; then' "$installer" | cut -d: -f1)
[[ -n "$license_line" && -n "$binary_line" ]] || fail "install ordering could not be resolved"
((license_line < binary_line)) || fail "binaries are replaced before licenses are safely installed"
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

run_port_fixtures "$RELEASE_INSTALLER"
printf '   \033[1;32m✓\033[0m %s\n' "installer port fixtures"

grep -Fq \
	'CURRENT_MAIN_PACKAGING_BASE="https://raw.githubusercontent.com/${REPO_OWNER}/${REPO_NAME}/main/packaging"' \
	"$RELEASE_INSTALLER" || fail "current-main packaging policy is not explicit"
if grep -Fq '/releases/download/${version}/packaging' "$RELEASE_INSTALLER"; then
	fail "packaging assets must not be forced to the release tag"
fi
printf '   \033[1;32m✓\033[0m %s\n' "recovery-asset packaging policy"

binary_list=$(bash -c "source '$installer'; binary_names")
unit_list=$(bash -c "source '$installer'; systemd_units")

# The Makefile owns the binary manifest; the installer must ship exactly it.
make_binaries=$(make -s -C "$REPO_ROOT" print-binaries | sort)
assert_eq "$make_binaries" "$(printf '%s\n' "$binary_list" | sort)" "installer binary list vs make print-binaries"
# Every non-unit file in packaging/systemd is installed separately.
packaged_units=$(cd "${REPO_ROOT}/packaging/systemd" && printf '%s\n' * | grep -vx 'linuxio-tmpfiles.conf' | sort)
assert_eq "$packaged_units" "$(printf '%s\n' "$unit_list" | sort)" "installer unit list vs packaging/systemd"
# Every binary a packaged unit starts must be one the installer ships.
while IFS= read -r exec_bin; do
	assert_line "$exec_bin" "$binary_list" "ExecStart binary in packaging/systemd"
done < <(sed -nE 's#^ExecStart=-?/usr/local/bin/([^ ]+).*#\1#p' "${REPO_ROOT}"/packaging/systemd/*.service | sort -u)
# Workflows must read the manifest from make, not retype the binary list.
if grep -Eq 'linuxio-webserver[ ,]+linuxio-bridge' "${REPO_ROOT}"/.github/workflows/*.yml; then
	fail "workflows must use make print-binaries instead of retyping the binary list"
fi
printf '   \033[1;32m✓\033[0m %s\n' "binary and unit lists match Makefile and packaging/systemd"

admin_config=$(bash -c "source '$installer'; admin_owned_config_files")
for rel in disallowed-users indexer/config.yaml monitoring/config.yaml; do
	assert_line "$rel" "$admin_config" "administrator-owned config list"
	[[ -f "${REPO_ROOT}/packaging/etc/linuxio/${rel}" ]] || fail "missing packaged ${rel}"
done
grep -Fq 'already exists (not overwriting)' "$installer" || fail "installer must not overwrite administrator-owned config"
printf '   \033[1;32m✓\033[0m %s\n' "administrator-owned config is never overwritten"

assert_line linuxio-indexer "$binary_list" "installer binary list"
assert_line linuxio-btrfs-timer "$binary_list" "installer binary list"
for unit in linuxio-indexer.socket linuxio-indexer.service linuxio-indexer-index.service linuxio-indexer-index.timer linuxio-btrfs-health.service linuxio-btrfs-health.timer; do
	assert_line "$unit" "$unit_list" "installer unit list"
	grep -Fqx 'PartOf=linuxio.target' "${REPO_ROOT}/packaging/systemd/${unit}" || fail "${unit} is not part of linuxio.target"
done
# Boot-time and resync jobs must outlive a linuxio.target restart.
for unit in linuxio-btrfs-degraded.service linuxio-btrfs-resync@.service; do
	assert_line "$unit" "$unit_list" "installer unit list"
	if grep -Fq 'PartOf=linuxio.target' "${REPO_ROOT}/packaging/systemd/${unit}"; then
		fail "${unit} must not stop with linuxio.target"
	fi
done
if grep -Eq 'sync_indexer_tcp_listener|--tcp-listener-only|linuxio-indexer config' "$installer"; then
	fail "installer still mutates indexer runtime configuration"
fi
grep -Fq 'THIRD_PARTY_NOTICES.md' "$installer" || fail "installer does not install third-party notices"
grep -Fq 'systemctl enable "$INDEXER_TIMER_UNIT_NAME"' "$installer" || fail "installer does not enable the timer on first install"
grep -Fq 'for unit in "$BTRFS_HEALTH_TIMER_UNIT_NAME" "$BTRFS_DEGRADED_UNIT_NAME"; do' "$installer" || fail "installer does not enable the btrfs health timer and degraded mount"
if grep -Fq 'indexer.json' "$installer"; then
	fail "installer still references removed indexer.json"
fi

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
if grep -rEq 'linuxio-indexer-socket-user|INDEXER_SOCKET_GROUP' \
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
if grep -Evq '^(exclude_paths:|  - |include_network_mounts:|[[:space:]]*$|#)' "${REPO_ROOT}/packaging/etc/linuxio/indexer/config.yaml"; then
	fail "indexer YAML contains non-scan policy"
fi
grep -Fq -- '--remove-data' "$UNINSTALLER" || fail "uninstaller must preserve data by default"
grep -Fq 'if [[ $REMOVE_DATA -eq 1 ]]; then' "$UNINSTALLER" || fail "uninstaller remove-data branch missing"
grep -Fq 'Persistent data preserved' "$UNINSTALLER" || fail "uninstaller preserve-data behavior missing"
grep -Fq 'DATA_DIR="/var/lib/linuxio"' "$UNINSTALLER" || fail "uninstaller must account for persistent index data"
[[ -f "${REPO_ROOT}/docs/THIRD_PARTY_NOTICES.md" ]] || fail "third-party notices must live under docs"
printf '   \033[1;32m✓\033[0m %s\n' "first-party indexer packaging policy"

# Updates and linuxio.target restarts must never stop a running job.
for unit in linuxio-jobs@.service linuxio-jobs@.socket; do
	assert_line "$unit" "$unit_list" "installer unit list"
	if grep -Eq '^PartOf=.*linuxio\.target' "${REPO_ROOT}/packaging/systemd/${unit}"; then
		fail "${unit} must not stop with linuxio.target"
	fi
done
if grep -Fq 'linuxio-jobs' "${REPO_ROOT}/packaging/systemd/linuxio.target"; then
	fail "linuxio.target must not pull in job hosts or their sockets"
fi
# The pre-release launcher and boot reconcile are retired on upgrade.
for retired in linuxio-jobs-launcher.socket linuxio-jobs-reconcile.service; do
	[[ ! -e "${REPO_ROOT}/packaging/systemd/${retired}" ]] || fail "${retired} is retired"
done
grep -Fq 'systemctl disable --now linuxio-jobs-launcher.socket' "$installer" ||
	fail "installer must retire the pre-release job launcher socket"
grep -Fq 'systemctl disable --now linuxio-jobs-reconcile.service' "$installer" ||
	fail "installer must retire the boot reconcile"
JOBS_HOST="${REPO_ROOT}/packaging/systemd/linuxio-jobs@.service"
JOBS_SOCKET="${REPO_ROOT}/packaging/systemd/linuxio-jobs@.socket"
# Only the uid's own processes reach its host, and connecting is what starts it.
grep -Fqx 'ListenStream=/run/linuxio/jobs/%i.sock' "$JOBS_SOCKET" || fail "job host socket path"
grep -Fqx 'SocketUser=%i' "$JOBS_SOCKET" || fail "a job host socket must belong to its uid"
grep -Fqx 'SocketMode=0600' "$JOBS_SOCKET" || fail "a job host socket must be private to its uid"
grep -Fqx 'Accept=no' "$JOBS_SOCKET" || fail "one job host serves every connection of its uid"
# The bridge recognizes the host by this name; the host takes no arguments.
grep -Fqx 'FileDescriptorName=linuxio-jobs' "$JOBS_SOCKET" || fail "the job host socket must carry its name"
grep -Fqx 'ExecStart=/usr/local/bin/linuxio-bridge' "$JOBS_HOST" || fail "a job host must start without arguments"
grep -Fqx 'Wants=linuxio-jobs@%i.socket' "$JOBS_HOST" || fail "a job host must pull in its socket"
grep -Fqx 'User=%i' "$JOBS_HOST" || fail "a job host must run as its own uid"
grep -Fqx 'StateDirectoryMode=0700' "$JOBS_HOST" || fail "job records must be private to their uid"
grep -Fqx 'NoNewPrivileges=yes' "$JOBS_HOST" || fail "job hosts must not gain privileges"
# A host that outlives the login is bounded; an OOM kill of one job's
# subprocess must not stop the host.
grep -Fqx 'TasksMax=8192' "$JOBS_HOST" || fail "job hosts must bound their tasks"
grep -Fqx 'MemoryMax=2G' "$JOBS_HOST" || fail "job hosts must bound their memory"
grep -Fqx 'OOMPolicy=continue' "$JOBS_HOST" || fail "an OOM kill must not stop a job host"
# The root host starts at boot to mark, alert and resume what a reboot interrupted.
grep -Fqx 'WantedBy=multi-user.target' "$JOBS_HOST" || fail "the root job host must be enableable at boot"
grep -Fq 'systemctl enable linuxio-jobs@0.service' "$installer" ||
	fail "installer does not enable the root job host at boot"
grep -Fq 'rm -rf "${DATA_DIR}/durable-operations"' "$installer" ||
	fail "installer must remove the orphaned durable-operations store"
printf '   \033[1;32m✓\033[0m %s\n' "job units stay outside linuxio.target"

# The standalone scripts each carry their own Show/Header; keep them identical.
ui_sum() { sed -n '/^Show() {/,/^}/p;/^Header() {/,/^}/p;/^readonly COLOUR_RESET/,/^readonly YELLOW/p' "$1" | sha256sum; }
for script in install-dependencies.sh uninstall.sh; do
	assert_eq "$(ui_sum "$RELEASE_INSTALLER")" "$(ui_sum "${SCRIPT_DIR}/../${script}")" "${script} Show/Header styling"
done
printf '   \033[1;32m✓\033[0m %s\n' "standalone scripts share Show/Header styling"

printf '\033[1;32m%s\033[0m\n' "✅ Installer and recovery-asset fixtures passed!"
