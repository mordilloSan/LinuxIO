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
# Sourcing the installer creates its staging directory; its EXIT trap removes
# it. Directories of that name that exist already are not this run's.
STAGING_BEFORE=$(compgen -G '/tmp/linuxio-install.*' | sort || true)

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
	release_version_supported v0.55.0 || fail "v0.55.0 must be supported"
	release_version_supported v0.55.1 || fail "v0.55.1 must be supported"
	release_version_supported v1.0.0 || fail "v1.0.0 must be supported"
	if release_version_supported v0.54.9; then
		fail "v0.54.9 must be rejected before downloads"
	fi
	# --skip-binaries keeps the installed programs, which must be ones the
	# units start: v0.55.0 is the first whose worker answers --version.
	fixture_dir=$(mktemp -d)
	if installed_binaries_supported "$fixture_dir"; then
		fail "a host with no worker must not pass for a supported install"
	fi
	printf '#!/bin/sh\necho "linuxio-worker: unknown command" >&2\nexit 2\n' >"${fixture_dir}/linuxio-worker"
	chmod 0755 "${fixture_dir}/linuxio-worker"
	if installed_binaries_supported "$fixture_dir"; then
		fail "a v0.54 worker must not pass for a supported install"
	fi
	printf '#!/bin/sh\n[ "$1" = "--version" ] && echo "LinuxIO Worker v0.55.0"\n' >"${fixture_dir}/linuxio-worker"
	installed_binaries_supported "$fixture_dir" || fail "a v0.55 worker must pass for a supported install"
	rm -rf "$fixture_dir"
)
printf '   \033[1;32m✓\033[0m %s\n' "release floor: the first release whose programs take this installer's unit command lines"
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

		# In the installer's staging directory, which its EXIT trap removes.
		socket_file=$(mktemp "${STAGING}/socket.XXXXXX")

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
packaged_units=$(cd "${REPO_ROOT}/packaging/systemd" && printf '%s\n' * | grep -vxE 'linuxio-tmpfiles.conf|linuxio-run-record.conf' | sort)
assert_eq "$packaged_units" "$(printf '%s\n' "$unit_list" | sort)" "installer unit list vs packaging/systemd"
# Every binary a packaged unit starts must be one the installer ships.
while IFS= read -r exec_bin; do
	assert_line "$exec_bin" "$binary_list" "ExecStart binary in packaging/systemd"
done < <(sed -nE 's#^ExecStart=-?/usr/libexec/linuxio/([^ ]+).*#\1#p' "${REPO_ROOT}"/packaging/systemd/*.service | sort -u)
# Workflows must read the manifest from make, not retype the binary list.
if grep -Eq 'linuxio-webserver[ ,]+linuxio-bridge' "${REPO_ROOT}"/.github/workflows/*.yml; then
	fail "workflows must use make print-binaries instead of retyping the binary list"
fi
printf '   \033[1;32m✓\033[0m %s\n' "binary and unit lists match Makefile and packaging/systemd"
# linuxio.target restarts must stop the web socket before starting the auth
# socket: the auth socket's sd-chown helper otherwise holds the old :8090 fd.
grep -Eq '^After=.*\blinuxio-auth\.socket\b' "${REPO_ROOT}/packaging/systemd/linuxio-webserver.socket" ||
	fail "linuxio-webserver.socket must be ordered After=linuxio-auth.socket"
printf '   \033[1;32m✓\033[0m %s\n' "web socket ordered after the auth socket"

admin_config=$(bash -c "source '$installer'; admin_owned_config_files")
for rel in disallowed-users indexer/config.yaml monitoring/config.yaml; do
	assert_line "$rel" "$admin_config" "administrator-owned config list"
	[[ -f "${REPO_ROOT}/packaging/etc/linuxio/${rel}" ]] || fail "missing packaged ${rel}"
done
grep -Fq 'already exists (not overwriting)' "$installer" || fail "installer must not overwrite administrator-owned config"
printf '   \033[1;32m✓\033[0m %s\n' "administrator-owned config is never overwritten"

assert_line linuxio-indexer "$binary_list" "installer binary list"
assert_line linuxio-worker "$binary_list" "installer binary list"
for unit in linuxio-indexer.socket linuxio-indexer.service linuxio-indexer-index.service linuxio-indexer-index.timer linuxio-btrfs-health.service linuxio-btrfs-health.timer; do
	assert_line "$unit" "$unit_list" "installer unit list"
	grep -Fqx 'PartOf=linuxio.target' "${REPO_ROOT}/packaging/systemd/${unit}" || fail "${unit} is not part of linuxio.target"
done
# Boot-time, resync and rebuild jobs must outlive a linuxio.target restart.
for unit in linuxio-btrfs-degraded.service linuxio-btrfs-resync@.service linuxio-btrfs-rebuild@.service; do
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
grep -Fqx 'ExecStart=/usr/libexec/linuxio/linuxio-indexer --config /etc/linuxio/indexer/config.yaml' "$INDEXER_SERVICE" || fail "indexer service must start the managed daemon directly"
grep -Fqx 'ExecStart=/usr/libexec/linuxio/linuxio-indexer --trigger-index --socket-path /run/linuxio/indexer.sock' \
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
grep -Fqx 'ExecStart=/usr/libexec/linuxio/linuxio-bridge' "$JOBS_HOST" || fail "a job host must start without arguments"
grep -Fqx 'Wants=linuxio-jobs@%i.socket' "$JOBS_HOST" || fail "a job host must pull in its socket"
grep -Fqx 'User=%i' "$JOBS_HOST" || fail "a job host must run as its own uid"
grep -Fqx 'StateDirectoryMode=0700' "$JOBS_HOST" || fail "job records must be private to their uid"
grep -Fqx 'NoNewPrivileges=yes' "$JOBS_HOST" || fail "job hosts must not gain privileges"
# A host that outlives the login is bounded; an OOM kill of one job's
# subprocess must not stop the host.
grep -Fqx 'TasksMax=8192' "$JOBS_HOST" || fail "job hosts must bound their tasks"
grep -Fqx 'MemoryMax=2G' "$JOBS_HOST" || fail "job hosts must bound their memory"
grep -Fqx 'OOMPolicy=continue' "$JOBS_HOST" || fail "an OOM kill must not stop a job host"
# A login is not capped: pam_systemd moves it into a logind session scope, where
# limits on the auth worker would not apply, and where it stays in the worker's
# cgroup they would cap the terminal and the assistant's agents.
AUTH_WORKER="${REPO_ROOT}/packaging/systemd/linuxio-auth@.service"
if grep -Eq '^(TasksMax|MemoryMax)=' "$AUTH_WORKER"; then
	fail "the auth worker must not declare a task or memory cap for the login"
fi
# The root host starts at boot to mark, alert and resume what a reboot interrupted.
grep -Fqx 'WantedBy=multi-user.target' "$JOBS_HOST" || fail "the root job host must be enableable at boot"
grep -Fq 'systemctl enable linuxio-jobs@0.service' "$installer" ||
	fail "installer does not enable the root job host at boot"
grep -Fq 'rm -rf "${DATA_DIR}/durable-operations"' "$installer" ||
	fail "installer must remove the orphaned durable-operations store"
printf '   \033[1;32m✓\033[0m %s\n' "job units stay outside linuxio.target"

# Each auth connection stays open as its bridge's transport until the session
# ends, so MaxConnections caps live sessions host-wide, not login attempts.
AUTH_MAX_CONNECTIONS="$(sed -n 's/^MaxConnections=//p' "${REPO_ROOT}/packaging/systemd/linuxio-auth.socket")"
((AUTH_MAX_CONNECTIONS >= 64)) ||
	fail "linuxio-auth.socket MaxConnections=${AUTH_MAX_CONNECTIONS} caps live sessions; keep it at least 64"
printf '   \033[1;32m✓\033[0m %s\n' "auth socket leaves room for live sessions"

# Only root may start the programs systemd runs as root; the rest must stay
# startable by the user they run as.
(
	# shellcheck disable=SC1090
	source "$RELEASE_INSTALLER"
	for binary in linuxio-worker linuxio-docker-update linuxio-auth linuxio-indexer linuxio-monitoring; do
		assert_eq "0700" "$(binary_mode "$binary")" "${binary} mode"
	done
	for binary in linuxio linuxio-bridge linuxio-claude-relay linuxio-webserver; do
		assert_eq "0755" "$(binary_mode "$binary")" "${binary} mode"
	done
	while IFS= read -r binary; do
		[[ "$(binary_mode "$binary")" =~ ^07[05][05]$ ]] || fail "${binary} has no mode"
	done < <(binary_names)
)
printf '   \033[1;32m✓\033[0m %s\n' "root-run programs are installed for root only"

# The installer records each program's version line, since the CLI and the
# webserver may not run the root-only programs. Its list of programs is the
# Go list's.
go_components=$(grep -o 'Binary: "[^"]*"' "$REPO_ROOT/backend/common/version/components.go" | cut -d'"' -f2 | sort)
installer_components=$(bash -c "source '$RELEASE_INSTALLER'; component_binaries" | sort)
assert_eq "$go_components" "$installer_components" "installer component list vs version.Components"
(
	# shellcheck disable=SC1090
	source "$RELEASE_INSTALLER"
	fixture_dir=$(mktemp -d)
	# The CLI in one folder and the other programs in another, as installed.
	mkdir "${fixture_dir}/cli" "${fixture_dir}/libexec"
	printf '#!/bin/sh\n[ "$1 $2" = "version --self" ] && echo "LinuxIO CLI v1.2.3"\n' >"${fixture_dir}/cli/linuxio"
	for name in webserver:"Web Server" bridge:Bridge indexer:Indexer monitoring:Monitoring worker:Worker docker-update:"Docker Updater"; do
		printf '#!/bin/sh\n[ "$1" = "--version" ] && echo "LinuxIO %s v1.2.3" && echo "more"\n' "${name#*:}" >"${fixture_dir}/libexec/linuxio-${name%%:*}"
	done
	# The relay answers only without the variable that names Claude Code, so
	# the installer must not hand it one from its own environment.
	printf '#!/bin/sh\n[ -z "$LINUXIO_CLAUDE_BIN" ] && [ "$1" = "--version" ] && echo "LinuxIO Claude Relay v1.2.3"\n' >"${fixture_dir}/libexec/linuxio-claude-relay"
	export LINUXIO_CLAUDE_BIN=/opt/claude/bin/claude
	# A program that cannot run, such as one built for another machine.
	printf '#!/bin/sh\nexit 1\n' >"${fixture_dir}/libexec/linuxio-auth"
	chmod 0755 "${fixture_dir}/cli/linuxio" "${fixture_dir}/libexec/"*
	write_components_file "${fixture_dir}/cli" "${fixture_dir}/libexec" "${fixture_dir}/share/components" >/dev/null ||
		fail "recording the installed versions failed"
	expected=$'LinuxIO CLI v1.2.3\nLinuxIO Web Server v1.2.3\nLinuxIO Bridge v1.2.3\nLinuxIO Indexer v1.2.3\nLinuxIO Monitoring v1.2.3\nLinuxIO Worker v1.2.3\nLinuxIO Docker Updater v1.2.3\nLinuxIO Claude Relay v1.2.3'
	assert_eq "$expected" "$(cat "${fixture_dir}/share/components")" "recorded versions"
	assert_eq "644" "$(stat -c %a "${fixture_dir}/share/components")" "recorded versions mode"
	assert_eq "755" "$(stat -c %a "${fixture_dir}/share")" "recorded versions directory mode"
	rm -rf "$fixture_dir"
)
printf '   \033[1;32m✓\033[0m %s\n' "installed versions are recorded for the CLI and the webserver"

# Units and drop-ins a saved setting wrote hold the command lines of the
# release that wrote them; the installer rewrites those of earlier releases
# (the words up to v0.54, the /usr/local/bin folder after) and leaves
# everything else as it is.
(
	# shellcheck disable=SC1090
	source "$RELEASE_INSTALLER"
	fixture_dir=$(mktemp -d)
	system="${fixture_dir}/system"
	mkdir -p "${system}/linuxio-webserver.service.d" "${system}/linuxio-monitoring.service.d" \
		"${system}/linuxio-indexer.service.d" \
		"${system}/apt-daily-upgrade.service.d" "${system}/apt-daily.service.d" "${system}/other.service.d"
	printf '[Service]\nType=oneshot\nExecStart=/usr/local/bin/linuxio-btrfs-timer snapshot run\n' >"${system}/linuxio-btrfs-snapshot.service"
	printf '[Service]\nExecStart=/usr/local/bin/linuxio-btrfs-timer replicate run\n' >"${system}/linuxio-btrfs-replicate.service"
	printf '[Service]\nExecStart=/usr/local/bin/linuxio-docker-update run --config /etc/linuxio/docker-update.json\n' >"${system}/linuxio-docker-update.service"
	printf '[Service]\nExecStart=\nExecStart=/usr/local/bin/linuxio-webserver run -verbose\n' >"${system}/linuxio-webserver.service.d/verbose.conf"
	printf '[Service]\nExecStart=\nExecStart=/usr/local/bin/linuxio-monitoring run --config /etc/linuxio/monitoring/config.yaml --verbose\n' >"${system}/linuxio-monitoring.service.d/verbose.conf"
	# v0.54 wrote the indexer's flag as --config-file.
	printf '[Service]\nExecStart=\nExecStart=/usr/local/bin/linuxio-indexer --config-file /etc/linuxio/indexer/config.yaml --verbose\n' >"${system}/linuxio-indexer.service.d/verbose.conf"
	printf '[Service]\nExecStartPre=-/usr/local/bin/linuxio-btrfs-timer snapshot pre-update\nExecStopPost=/usr/local/bin/linuxio alert auto-update --provider apt --policy on_change\n' >"${system}/apt-daily-upgrade.service.d/linuxio-alert.conf"
	printf '[Service]\nExecStopPost=/usr/local/bin/linuxio alert pending-updates --provider apt\n' >"${system}/apt-daily.service.d/linuxio-alert.conf"
	# A drop-in from before the worker was a program of its own.
	mkdir -p "${system}/dnf-automatic.service.d"
	printf '[Service]\nExecStartPre=-/usr/local/bin/linuxio btrfs-snapshot pre-update\n' >"${system}/dnf-automatic.service.d/linuxio-alert.conf"
	# A drop-in with this release's words in the folder earlier releases
	# installed the programs in, and one already in this release's folder.
	mkdir -p "${system}/dnf5-automatic.service.d" "${system}/mintupdate-automation-upgrade.service.d"
	printf '[Service]\nExecStartPre=-/usr/local/bin/linuxio-worker snapshot pre-update\nExecStopPost=/usr/local/bin/linuxio-worker alert auto-update --provider dnf5 --policy always\n' >"${system}/dnf5-automatic.service.d/linuxio-alert.conf"
	printf '[Service]\nExecStopPost=/usr/libexec/linuxio/linuxio-worker alert auto-update --provider mint --policy always\n' >"${system}/mintupdate-automation-upgrade.service.d/linuxio-alert.conf"
	cp "${system}/mintupdate-automation-upgrade.service.d/linuxio-alert.conf" "${fixture_dir}/mint.before"
	# Not a file LinuxIO writes: an administrator's own hook stays as it is.
	printf '[Service]\nExecStopPost=/usr/local/bin/linuxio alert raise --source mine --key k --title T\n' >"${system}/other.service.d/mine.conf"
	cp "${system}/other.service.d/mine.conf" "${fixture_dir}/mine.before"
	chmod 0644 "${system}/linuxio-btrfs-snapshot.service"

	for pass in first second; do
		rewrite_saved_unit_commands "$system" >/dev/null || fail "${pass} rewrite failed"
		assert_eq $'[Service]\nType=oneshot\nExecStart=/usr/libexec/linuxio/linuxio-worker snapshot' \
			"$(cat "${system}/linuxio-btrfs-snapshot.service")" "${pass} rewrite of the snapshot unit"
		assert_eq $'[Service]\nExecStart=/usr/libexec/linuxio/linuxio-worker replicate' \
			"$(cat "${system}/linuxio-btrfs-replicate.service")" "${pass} rewrite of the replication unit"
		assert_eq $'[Service]\nExecStart=/usr/libexec/linuxio/linuxio-docker-update --config /etc/linuxio/docker-update.json' \
			"$(cat "${system}/linuxio-docker-update.service")" "${pass} rewrite of the Docker update unit"
		assert_eq $'[Service]\nExecStart=\nExecStart=/usr/libexec/linuxio/linuxio-webserver --verbose' \
			"$(cat "${system}/linuxio-webserver.service.d/verbose.conf")" "${pass} rewrite of the webserver verbose drop-in"
		assert_eq $'[Service]\nExecStart=\nExecStart=/usr/libexec/linuxio/linuxio-indexer --config /etc/linuxio/indexer/config.yaml --verbose' \
			"$(cat "${system}/linuxio-indexer.service.d/verbose.conf")" "${pass} rewrite of the indexer verbose drop-in"
		assert_eq $'[Service]\nExecStart=\nExecStart=/usr/libexec/linuxio/linuxio-monitoring --config /etc/linuxio/monitoring/config.yaml --verbose' \
			"$(cat "${system}/linuxio-monitoring.service.d/verbose.conf")" "${pass} rewrite of the monitoring verbose drop-in"
		assert_eq $'[Service]\nExecStartPre=-/usr/libexec/linuxio/linuxio-worker snapshot pre-update\nExecStopPost=/usr/libexec/linuxio/linuxio-worker alert auto-update --provider apt --policy on_change' \
			"$(cat "${system}/apt-daily-upgrade.service.d/linuxio-alert.conf")" "${pass} rewrite of the update alert drop-in"
		assert_eq $'[Service]\nExecStopPost=/usr/libexec/linuxio/linuxio-worker alert pending-updates --provider apt' \
			"$(cat "${system}/apt-daily.service.d/linuxio-alert.conf")" "${pass} rewrite of the pending updates drop-in"
		assert_eq $'[Service]\nExecStartPre=-/usr/libexec/linuxio/linuxio-worker snapshot pre-update' \
			"$(cat "${system}/dnf-automatic.service.d/linuxio-alert.conf")" "${pass} rewrite of a drop-in with the oldest words"
		assert_eq $'[Service]\nExecStartPre=-/usr/libexec/linuxio/linuxio-worker snapshot pre-update\nExecStopPost=/usr/libexec/linuxio/linuxio-worker alert auto-update --provider dnf5 --policy always' \
			"$(cat "${system}/dnf5-automatic.service.d/linuxio-alert.conf")" "${pass} rewrite of a drop-in naming the old folder"
		cmp -s "${system}/mintupdate-automation-upgrade.service.d/linuxio-alert.conf" "${fixture_dir}/mint.before" ||
			fail "${pass} rewrite changed a drop-in that already names /usr/libexec/linuxio"
	done
	cmp -s "${system}/other.service.d/mine.conf" "${fixture_dir}/mine.before" || fail "a file LinuxIO did not write was rewritten"
	assert_eq "644" "$(stat -c %a "${system}/linuxio-btrfs-snapshot.service")" "rewritten unit mode"
	# The webserver drop-in as v0.54 wrote it, with no "run" to mark it.
	printf '[Service]\nExecStart=\nExecStart=/usr/local/bin/linuxio-webserver -verbose\n' >"${system}/linuxio-webserver.service.d/verbose.conf"
	rewrite_saved_unit_commands "$system" >/dev/null || fail "rewrite of a v0.54 drop-in failed"
	assert_eq $'[Service]\nExecStart=\nExecStart=/usr/libexec/linuxio/linuxio-webserver --verbose' \
		"$(cat "${system}/linuxio-webserver.service.d/verbose.conf")" "rewrite of the v0.54 webserver verbose drop-in"
	rm -rf "$fixture_dir"
)
printf '   \033[1;32m✓\033[0m %s\n' "saved units and drop-ins get this release's command lines"

# The CLI goes on PATH and the other programs in LinuxIO's own folder; the
# copies earlier releases left in /usr/local/bin go, by name, and nothing else
# there does.
(
	# shellcheck disable=SC1090
	source "$RELEASE_INSTALLER"
	assert_eq "/usr/bin" "$(binary_dir linuxio)" "folder of the CLI"
	while IFS= read -r binary; do
		[[ "$binary" == linuxio ]] && continue
		assert_eq "/usr/libexec/linuxio" "$(binary_dir "$binary")" "folder of ${binary}"
	done < <(binary_names)
	assert_eq "/usr/local/bin" "$OLD_BIN_DIR" "folder earlier releases installed in"
	fixture_dir=$(mktemp -d)
	old="${fixture_dir}/old"
	mkdir "$old"
	while IFS= read -r binary; do
		printf 'old\n' >"${old}/${binary}"
	done < <(binary_names)
	printf 'old\n' >"${old}/linuxio-btrfs-timer"
	# A dangling link goes too, and a missing name is no failure.
	rm -f "${old}/linuxio-worker" "${old}/linuxio-auth"
	ln -s /nonexistent "${old}/linuxio-worker"
	# The administrator's own files, whose names only start like LinuxIO's.
	printf 'mine\n' >"${old}/linuxio-backup.sh"
	printf 'mine\n' >"${old}/linuxio.bak"
	for pass in first second; do
		remove_old_binaries "$old" >/dev/null || fail "${pass} removal of the old copies failed"
		assert_eq $'linuxio-backup.sh\nlinuxio.bak' "$(LC_ALL=C ls -A "$old")" "${pass} removal of the old copies"
	done
	rm -rf "$fixture_dir"
)
printf '   \033[1;32m✓\033[0m %s\n' "programs go to /usr/bin and /usr/libexec/linuxio; the old copies in /usr/local/bin are removed by name"

# Scheduled units record their runs through two hook lines. The schedule units
# are generated, so the lines reach them (and the snapshot, replication and
# Docker update units) through one drop-in; linuxio-btrfs-resync@.service and
# linuxio-btrfs-rebuild@.service carry them themselves.
BEGIN_HOOK='ExecStartPre=-+/usr/libexec/linuxio/linuxio-worker scheduled-run begin --unit %n'
END_HOOK='ExecStopPost=-+/usr/libexec/linuxio/linuxio-worker scheduled-run end --unit %n'
run_record_dropin_fixtures() {
	local checkout="$REPO_ROOT"
	(
		# shellcheck disable=SC1090
		source "$RELEASE_INSTALLER"
		local fixture_dir dropin_dir dropin_file
		fixture_dir=$(mktemp -d)
		LOCAL_INSTALL=1
		# shellcheck disable=SC2030
		REPO_ROOT="$checkout"

		install_run_record_dropins "${fixture_dir}/system" >/dev/null ||
			fail "installing the run-record drop-ins failed"
		local linuxio_dirs="linuxio-schedule-.service.d linuxio-btrfs-snapshot.service.d linuxio-btrfs-replicate.service.d linuxio-docker-update.service.d"
		# The distributions' automatic update services, hooked the same way.
		local update_dirs="apt-daily-upgrade.service.d dnf-automatic.service.d dnf5-automatic.service.d mintupdate-automation-upgrade.service.d"
		for dropin_dir in $linuxio_dirs $update_dirs; do
			dropin_file="${fixture_dir}/system/${dropin_dir}/10-linuxio-run.conf"
			[[ -f "$dropin_file" ]] || fail "missing ${dropin_dir}/10-linuxio-run.conf after install"
			grep -Fqx '[Service]' "$dropin_file" || fail "${dropin_dir} drop-in has no [Service] section"
			grep -Fqx "$BEGIN_HOOK" "$dropin_file" || fail "${dropin_dir} drop-in lacks the begin hook"
			grep -Fqx "$END_HOOK" "$dropin_file" || fail "${dropin_dir} drop-in lacks the end hook"
			assert_eq "644" "$(stat -c %a "$dropin_file")" "${dropin_dir} drop-in mode"
		done
		assert_eq "8" "$(find "${fixture_dir}/system" -mindepth 1 -maxdepth 1 | wc -l)" "installed drop-in directories"
		if compgen -G "${fixture_dir}/system/*/*.new.*" >/dev/null; then
			fail "drop-in install left a temporary file"
		fi

		# Uninstalling removes everything under linuxio*, drop-in directories
		# included, and LinuxIO's two drop-ins from the update services: the
		# run record and the alert hook the bridge writes there. What an
		# administrator put beside them stays.
		mkdir -p "${fixture_dir}/system/unrelated.service.d" "${fixture_dir}/system/apt-daily.service.d"
		: >"${fixture_dir}/system/unrelated.service.d/override.conf"
		: >"${fixture_dir}/system/dnf-automatic.service.d/override.conf"
		: >"${fixture_dir}/system/apt-daily-upgrade.service.d/linuxio-alert.conf"
		: >"${fixture_dir}/system/apt-daily.service.d/linuxio-alert.conf"
		# The schedule the auto-update settings write on the update timers, and
		# an administrator's own override of one of them.
		mkdir -p "${fixture_dir}/system/apt-daily-upgrade.timer.d" "${fixture_dir}/system/dnf-automatic.timer.d" "${fixture_dir}/system/other.timer.d"
		: >"${fixture_dir}/system/apt-daily-upgrade.timer.d/linuxio.conf"
		: >"${fixture_dir}/system/dnf-automatic.timer.d/linuxio.conf"
		: >"${fixture_dir}/system/dnf-automatic.timer.d/override.conf"
		: >"${fixture_dir}/system/other.timer.d/linuxio.conf"
		bash -c "$(grep -E '^(rm -rf /etc/systemd/system/linuxio\*|rm -f /etc/systemd/system/(\*\.service\.d|apt-daily\.timer\.d)/|rmdir --ignore-fail-on-non-empty /etc/systemd/system/)' "$UNINSTALLER" | sed "s#/etc/systemd/system#${fixture_dir}/system#g")"
		[[ ! -e "${fixture_dir}/system/apt-daily-upgrade.timer.d" ]] || fail "uninstall left the update timer's schedule override"
		[[ ! -e "${fixture_dir}/system/dnf-automatic.timer.d/linuxio.conf" ]] || fail "uninstall left the schedule override beside an administrator's file"
		[[ -f "${fixture_dir}/system/dnf-automatic.timer.d/override.conf" ]] || fail "uninstall removed an administrator's drop-in of an update timer"
		[[ -f "${fixture_dir}/system/other.timer.d/linuxio.conf" ]] || fail "uninstall removed a linuxio.conf of a timer that is not an update timer"
		for dropin_dir in $linuxio_dirs apt-daily-upgrade.service.d apt-daily.service.d dnf5-automatic.service.d mintupdate-automation-upgrade.service.d; do
			[[ ! -e "${fixture_dir}/system/${dropin_dir}" ]] || fail "uninstall left ${dropin_dir}"
		done
		[[ ! -e "${fixture_dir}/system/dnf-automatic.service.d/10-linuxio-run.conf" ]] || fail "uninstall left the run-record drop-in beside an administrator's file"
		[[ -f "${fixture_dir}/system/dnf-automatic.service.d/override.conf" ]] || fail "uninstall removed an administrator's drop-in of an update service"
		[[ -f "${fixture_dir}/system/unrelated.service.d/override.conf" ]] || fail "uninstall removed a directory that is not LinuxIO's"
		rm -rf "$fixture_dir"
	)
}
run_record_dropin_fixtures
# shellcheck disable=SC2031
for pool_unit in resync rebuild; do
	POOL_UNIT="${REPO_ROOT}/packaging/systemd/linuxio-btrfs-${pool_unit}@.service"
	grep -Fqx "$BEGIN_HOOK" "$POOL_UNIT" || fail "the ${pool_unit} unit lacks the begin hook"
	grep -Fqx "$END_HOOK" "$POOL_UNIT" || fail "the ${pool_unit} unit lacks the end hook"
done
printf '   \033[1;32m✓\033[0m %s\n' "scheduled units record their runs"

# The needrestart override reaches /etc/needrestart/conf.d only where
# needrestart's directory exists, from the checkout (--local) or current main
# (release), only when its content has the shipped form, and the uninstaller
# removes it.
# shellcheck disable=SC2031
NEEDRESTART_CONF="${REPO_ROOT}/packaging/etc/needrestart/conf.d/linuxio.conf"
needrestart_override_fixtures() {
	# shellcheck disable=SC2031
	local checkout="$REPO_ROOT"
	(
		# shellcheck disable=SC1090
		source "$RELEASE_INSTALLER"
		local fixture_dir conf_d installed fake out status inode bad
		fixture_dir=$(mktemp -d)
		conf_d="${fixture_dir}/needrestart/conf.d"
		installed="${conf_d}/linuxio.conf"
		LOCAL_INSTALL=1
		# shellcheck disable=SC2030
		REPO_ROOT="$checkout"

		needrestart_override_valid "$NEEDRESTART_CONF" || fail "the shipped needrestart override fails its own content check"
		local shipped_entry one="${fixture_dir}/one.conf"
		while IFS= read -r shipped_entry; do
			printf '%s\n' "$shipped_entry" >"$one"
			needrestart_override_valid "$one" || fail "the content check refuses the shipped line ${shipped_entry}"
		done < <(grep '^\$nrconf' "$NEEDRESTART_CONF")
		assert_eq "8" "$(grep -c '^\$nrconf' "$NEEDRESTART_CONF")" "shipped needrestart override entries"
		# Where Perl is present, the file must also compile under strict.
		if command -v perl >/dev/null 2>&1; then
			needrestart_override_loads "$NEEDRESTART_CONF" || fail "the shipped needrestart override does not compile under strict"
			printf '$nrconf{override_rc}->{qr(^linuxio-jobs@abc$)} = 0;\n' >"$one"
			if needrestart_override_loads "$one"; then
				fail "an interpolated @abc compiled under strict"
			fi
		fi
		# Without Perl, the token check alone decides.
		local no_perl="${fixture_dir}/no-perl-bin"
		mkdir -p "$no_perl"
		ln -s "$(command -v grep)" "${no_perl}/grep"
		printf '$nrconf{override_rc}->{qr(^linuxio-jobs@abc\.service$)} = 0;\n' >"$one"
		# The PATH without Perl applies only to the checks, so the installer's
		# EXIT trap still finds rm.
		out=$(bash -c 'source "$1"; PATH="$4"; command -v perl >/dev/null && echo "perl found"; needrestart_override_valid "$2" && echo "shipped accepted"; needrestart_override_valid "$3" || echo "bad refused"; PATH="$5"' \
			_ "$RELEASE_INSTALLER" "$NEEDRESTART_CONF" "$one" "$no_perl" "$PATH" 2>&1) || true
		assert_eq $'shipped accepted\nbad refused' "$out" "the content check without Perl"

		install_needrestart_override "${fixture_dir}/needrestart" >/dev/null ||
			fail "the needrestart override failed without needrestart"
		[[ ! -e "${fixture_dir}/needrestart" ]] || fail "the needrestart override was installed without needrestart"

		mkdir -p "$conf_d"
		printf 'keep\n' >"${conf_d}/admin.conf"
		install_needrestart_override "${fixture_dir}/needrestart" >/dev/null ||
			fail "installing the needrestart override failed"
		cmp -s "$NEEDRESTART_CONF" "$installed" || fail "the installed needrestart override is not the packaged one"
		assert_eq "644" "$(stat -c %a "$installed")" "needrestart override mode"
		if compgen -G "${installed}.new.*" >/dev/null; then
			fail "the needrestart override install left a temporary file"
		fi

		rm -f "$installed"
		LOCAL_INSTALL=0
		curl() {
			printf '%s\n' "$2" >"${fixture_dir}/fetched"
			cp "$NEEDRESTART_CONF" "$4"
		}
		install_needrestart_override "${fixture_dir}/needrestart" >/dev/null ||
			fail "the release install of the needrestart override failed"
		assert_eq "${CURRENT_MAIN_PACKAGING_BASE}/etc/needrestart/conf.d/linuxio.conf" "$(<"${fixture_dir}/fetched")" \
			"release source of the needrestart override"
		cmp -s "$NEEDRESTART_CONF" "$installed" || fail "the release install did not install the needrestart override"

		# Called as main calls it: in a shell of its own, under errexit and
		# outside any condition, with the snippet taken from checkout $2.
		soft_install() {
			bash -c 'source "$1"; LOCAL_INSTALL=1; REPO_ROOT="$2"; install_needrestart_override "$3"; echo "the install went on"' \
				_ "$RELEASE_INSTALLER" "${2:-$checkout}" "$1" 2>&1
		}

		# A fetched file that is not the shipped form is refused with a
		# notice; the install goes on and the override already installed stays
		# as it was. Besides a statement added, an entry for another unit, a
		# captive portal's page and an empty file: patterns Perl cannot load
		# under strict (an interpolated @abc, nested quantifiers, an empty \p),
		# one wider than LinuxIO's work (^linuxio-.+$), one that does not end
		# in \.service$, and entries for the four always-on daemons.
		fake="${fixture_dir}/fake"
		mkdir -p "${fake}/packaging/etc/needrestart/conf.d"
		inode=$(stat -c %i "$installed")
		entry_for() { printf '$nrconf{override_rc}->{qr(^linuxio-%s$)} = 0;\n' "$1"; }
		for bad in statement other-unit html empty at-abc nested-quantifiers empty-p any-linuxio-unit no-service webserver monitoring indexer auth; do
			case "$bad" in
			statement) { cat "$NEEDRESTART_CONF" && echo 'system("id");'; } ;;
			other-unit) { cat "$NEEDRESTART_CONF" && echo '$nrconf{override_rc}->{qr(^ssh\.service$)} = 0;'; } ;;
			html) printf '<!DOCTYPE html>\n<html><body>Sign in to the network</body></html>\n' ;;
			empty) : ;;
			at-abc) entry_for 'jobs@abc\.service' ;;
			nested-quantifiers) entry_for 'a+++\.service' ;;
			empty-p) entry_for '\p\.service' ;;
			any-linuxio-unit) entry_for '.+' ;;
			no-service) entry_for 'jobs@\d+' ;;
			webserver) entry_for 'webserver\.service' ;;
			monitoring) entry_for 'monitoring\.service' ;;
			indexer) entry_for 'indexer\.service' ;;
			auth) entry_for 'auth@.+\.service' ;;
			esac >"${fake}/packaging/etc/needrestart/conf.d/linuxio.conf"
			status=0
			out=$(soft_install "${fixture_dir}/needrestart" "$fake") || status=$?
			((status == 0)) || fail "a ${bad} snippet aborted the install: ${out}"
			[[ $out == *"The needrestart override was not installed: the fetched file is not what LinuxIO ships"*"the install went on"* ]] ||
				fail "a ${bad} snippet was not refused with the notice: ${out}"
			cmp -s "$NEEDRESTART_CONF" "$installed" || fail "a ${bad} snippet changed the installed override"
			assert_eq "$inode" "$(stat -c %i "$installed")" "installed override after a ${bad} snippet"
		done

		# An in-app update launched by a bridge without
		# /etc/needrestart/conf.d among its writable paths runs the installer
		# with it read-only: a notice, and the install goes on, with or
		# without conf.d. Each mode is restored before any check can fail.
		if [[ $EUID -ne 0 ]]; then
			rm -f "$installed"
			chmod 0555 "$conf_d"
			status=0
			out=$(soft_install "${fixture_dir}/needrestart") || status=$?
			chmod 0755 "$conf_d"
			((status == 0)) || fail "a read-only conf.d aborted the install: ${out}"
			[[ $out == *"Could not write ${installed}"*"the install went on"* ]] ||
				fail "a read-only conf.d gave no notice or stopped the install: ${out}"
			[[ ! -e "$installed" ]] || fail "the needrestart override was written into a read-only conf.d"
			cp "$NEEDRESTART_CONF" "$installed"

			mkdir -p "${fixture_dir}/bare"
			chmod 0555 "${fixture_dir}/bare"
			status=0
			out=$(soft_install "${fixture_dir}/bare") || status=$?
			chmod 0755 "${fixture_dir}/bare"
			((status == 0)) || fail "a read-only needrestart directory aborted the install: ${out}"
			[[ $out == *"Could not write ${fixture_dir}/bare/conf.d/linuxio.conf"*"the install went on"* ]] ||
				fail "a read-only needrestart directory gave no notice or stopped the install: ${out}"
			[[ ! -e "${fixture_dir}/bare/conf.d" ]] || fail "conf.d was created in a read-only needrestart directory"
		else
			printf '   \033[1;33m-\033[0m %s\n' "needrestart override in a read-only directory not checked: root writes through the mode"
		fi

		bash -c "$(grep -F 'rm -f /etc/needrestart/conf.d/linuxio.conf' "$UNINSTALLER" | sed "s#/etc/needrestart#${fixture_dir}/needrestart#")"
		[[ ! -e "$installed" ]] || fail "uninstall left the needrestart override"
		[[ -f "${conf_d}/admin.conf" ]] || fail "uninstall removed a needrestart file that is not LinuxIO's"
		rm -rf "$fixture_dir"
	)
}
needrestart_override_fixtures
# Installed after the dry run has exited and the release assets are verified,
# so that a failed verification installs nothing, and before the binaries
# change, so that an apt run during the install finds it.
needrestart_line=$(grep -n -m1 '^[[:space:]]*install_needrestart_override "$NEEDRESTART_DIR"' "$installer" | cut -d: -f1)
dry_run_line=$(grep -n -m1 '^[[:space:]]*if \[\[ $dry_run -eq 1 \]\]; then' "$installer" | cut -d: -f1)
checksums_line=$(grep -n -m1 '^[[:space:]]*if ! verify_checksums; then' "$installer" | cut -d: -f1)
[[ -n "$needrestart_line" && -n "$dry_run_line" && -n "$checksums_line" && -n "$binary_line" ]] ||
	fail "needrestart override ordering could not be resolved"
((dry_run_line < needrestart_line)) || fail "the needrestart override must be installed after the dry run exits"
((checksums_line < needrestart_line)) || fail "the needrestart override must be installed after the checksums are verified"
((needrestart_line < binary_line)) || fail "the needrestart override must be installed before the binaries change"
# The updater's dry run checks that each target is writable in its sandbox,
# where /etc/needrestart is not.
dry_run_body=$(sed -n '/^verify_dry_run_targets() {/,/^}/p' "$installer")
[[ -n "$dry_run_body" ]] || fail "verify_dry_run_targets not found in the installer"
if grep -Fq needrestart <<<"$dry_run_body"; then
	fail "the dry run must not require a writable needrestart directory"
fi
printf '   \033[1;32m✓\033[0m %s\n' "needrestart override checked, installed where needrestart is, removed on uninstall"

# Staged files live in a directory created by mktemp -d (mode 0700, owned by
# the installing user, an unpredictable name), and the EXIT trap removes it.
# The command substitution waits for that shell, and so for its EXIT trap.
staging_stat=$(bash -c 'source "$1"; stat -c "%a %u %n" "$STAGING"' _ "$RELEASE_INSTALLER")
read -r staging_mode staging_owner staging_dir <<<"$staging_stat"
assert_eq "700" "$staging_mode" "staging directory mode"
assert_eq "$(id -u)" "$staging_owner" "staging directory owner"
[[ $staging_dir =~ ^/tmp/linuxio-install\.[[:alnum:]]{10}$ ]] || fail "staging directory ${staging_dir} is not a mktemp name"
[[ ! -e "$staging_dir" ]] || fail "the EXIT trap left ${staging_dir}"
if grep -Eq 'linuxio-install-\$\$|mkdir -p "\$STAGING"' "$installer"; then
	fail "the installer must not name or create a predictable staging directory"
fi
printf '   \033[1;32m✓\033[0m %s\n' "staging directory is private and removed"

# Loaded the way needrestart.conf loads a conf.d file, the override adds to the
# distribution's override_rc and leaves its entries; deciding as needrestart
# does (the first matching expression, in sorted order), it exempts exactly the
# units that run work an update must not interrupt.
if command -v perl >/dev/null 2>&1; then
	perl -w - "$NEEDRESTART_CONF" <<'PERL' || fail "the needrestart override does not exempt exactly the LinuxIO work units"
use strict;
our %nrconf = (override_rc => { qr(^dbus) => 0, qr(^user@\d+\.service) => 0 });
my $fn = shift;
eval do { local(@ARGV, $/) = $fn; <> };
die "Error parsing $fn: $@" if $@;
my $rc = $nrconf{override_rc};
die "override_rc lost the distribution's entries\n" unless 2 == grep { $_ eq qr(^dbus) || $_ eq qr(^user@\d+\.service) } keys %$rc;
die "override_rc has " . scalar(keys %$rc) . " entries, want 10\n" unless 10 == keys %$rc;
sub restarts { my $unit = shift; for my $re (sort keys %$rc) { return $rc->{$re} if $unit =~ /$re/ } 1 }
my $id = "0b7c9a1e-5d2f-4c3b-9a8e-1f2e3d4c5b6a";
for my $unit ("linuxio-jobs\@0.service", "linuxio-jobs\@1001.service", "linuxio-schedule-$id.service",
	"linuxio-btrfs-snapshot.service", "linuxio-btrfs-replicate.service", "linuxio-btrfs-resync\@$id.service", "linuxio-btrfs-rebuild\@$id.service", "linuxio-docker-update.service",
	"linuxio-updater-1794345333839000000.service") {
	die "$unit would be restarted\n" if restarts($unit);
}
for my $unit ("linuxio-webserver.service", "linuxio-monitoring.service", "linuxio-indexer.service",
	"linuxio-auth\@0-1234-0.service", "linuxio-btrfs-health.service", "linuxio-indexer-index.service",
	"linuxio-issue.service", "linuxio-jobs\@0.socket", "linuxio-jobs-reconcile.service") {
	die "$unit would not be restarted\n" unless restarts($unit);
}
PERL
	printf '   \033[1;32m✓\033[0m %s\n' "needrestart override exempts exactly the LinuxIO work units"
else
	printf '   \033[1;33m-\033[0m %s\n' "needrestart override expressions not checked: no perl"
fi

# The PAM file of the host's family: the RHEL family has password-auth and
# postlogin where the Debian family has common-auth and the rest, so
# install_pam_config installs etc/pam.d/linuxio.rhel there. It decides with
# install-dependencies.sh's detect_distro and is_fedora, which the installer
# carries a copy of, kept identical.
# shellcheck disable=SC2031
PAM_DEBIAN="${REPO_ROOT}/packaging/etc/pam.d/linuxio"
# shellcheck disable=SC2031
PAM_RHEL="${REPO_ROOT}/packaging/etc/pam.d/linuxio.rhel"
[[ -f "$PAM_DEBIAN" ]] || fail "missing packaged etc/pam.d/linuxio"
[[ -f "$PAM_RHEL" ]] || fail "missing packaged etc/pam.d/linuxio.rhel"
distro_sum() { sed -n '/^detect_distro() {/,/^}/p;/^is_fedora() {/,/^}/p' "$1" | sha256sum; }
[[ -n "$(sed -n '/^detect_distro() {/,/^}/p' "${SCRIPT_DIR}/../install-dependencies.sh")" ]] ||
	fail "install-dependencies.sh has no detect_distro"
assert_eq "$(distro_sum "${SCRIPT_DIR}/../install-dependencies.sh")" "$(distro_sum "$RELEASE_INSTALLER")" \
	"detect_distro and is_fedora in install-linuxio-binaries.sh"

# The RHEL file is the Debian one line for line and in the same order, with
# each Debian-only name mapped to the RHEL family's. Order is what matters: a
# sufficient line in password-auth ends the stack where it is included, so a
# LinuxIO line moved below an include would no longer run. Columns may differ.
pam_directives() { grep -Ev '^[[:space:]]*(#|$)' "$1" | tr -s ' \t' ' '; }
pam_rhel_expected() {
	local line
	while IFS= read -r line; do
		case "$line" in
		"auth include common-auth") printf '%s\n' "auth substack password-auth" "auth include postlogin" ;;
		"account include common-account") printf '%s\n' "account include password-auth" ;;
		"password include common-password") printf '%s\n' "password include password-auth" ;;
		"session include common-session-noninteractive") printf '%s\n' "session include password-auth" "session include postlogin" ;;
		*) printf '%s\n' "${line//\/etc\/default\/locale//etc/locale.conf}" ;;
		esac
	done < <(pam_directives "$PAM_DEBIAN")
}
if ! pam_diff=$(diff <(pam_rhel_expected) <(pam_directives "$PAM_RHEL")); then
	fail "linuxio.rhel is not linuxio with the RHEL family's names (expected <, linuxio.rhel >):
${pam_diff}"
fi

# install_pam_config installs the family's file whole through a staged copy:
# a download cut half-way leaves the file there as it was, and nothing beside
# it. pam_install DISTRO DIR [cut] runs it into DIR as on DISTRO, from the
# checkout, or as a release whose download is cut.
pam_install() {
	# shellcheck disable=SC2031
	bash -c 'fixture_distro=$2 fixture_dir=$3 fixture_cut=$4 checkout=$5
		source "$1"
		REPO_ROOT=$checkout
		LOCAL_INSTALL=1
		detect_distro() { DISTRO=$fixture_distro; }
		if [[ -n $fixture_cut ]]; then
			LOCAL_INSTALL=0
			curl() {
				head -c 40 "${REPO_ROOT}/packaging/etc/pam.d/linuxio" >"$4"
				return 18
			}
		fi
		install_pam_config "$fixture_dir"' _ "$RELEASE_INSTALLER" "$1" "$2" "${3:-}" "$REPO_ROOT" >/dev/null 2>&1
}
pam_dir=$(mktemp -d)
for distro in ubuntu debian linuxmint pop fedora rhel centos rocky almalinux; do
	rm -f "${pam_dir}/linuxio"
	pam_install "$distro" "$pam_dir" || fail "install_pam_config failed for ${distro}"
	case "$distro" in
	fedora | rhel | centos | rocky | almalinux) pam_want="$PAM_RHEL" ;;
	*) pam_want="$PAM_DEBIAN" ;;
	esac
	cmp -s "$pam_want" "${pam_dir}/linuxio" || fail "PAM file for ${distro} is not $(basename "$pam_want")"
	assert_eq "644" "$(stat -c %a "${pam_dir}/linuxio")" "PAM file mode for ${distro}"
done
printf 'previous\n' >"${pam_dir}/linuxio"
if pam_install fedora "$pam_dir" cut; then
	fail "install_pam_config went on after a cut download"
fi
assert_eq "previous" "$(<"${pam_dir}/linuxio")" "PAM file after a cut download"
if compgen -G "${pam_dir}/linuxio.new.*" >/dev/null; then
	fail "a cut download left a temporary PAM file"
fi
rm -rf "$pam_dir"
printf '   \033[1;32m✓\033[0m %s\n' "each family gets its PAM file, the RHEL one line for line the Debian one, installed whole"

# linuxio-auth's SELinux module (packaging/selinux/linuxio.cil). On the RHEL
# family the installer loads it where semodule lists a policy store's modules,
# unless the store holds the same file, the fetched file is not of the shipped
# form, or SELINUX_DIR is read-only (the in-app updater); and it relabels the
# auth binary whenever restorecon exists. Elsewhere it calls neither.
# Stand-in semodule, restorecon and selinuxenabled log their calls (the last
# answers only); the tools directory holds only what the functions may use
# besides them, so a host's own semodule is never reached.
# shellcheck disable=SC2031
selinux_checkout=$REPO_ROOT
SELINUX_CIL="${selinux_checkout}/packaging/selinux/linuxio.cil"
[[ -f "$SELINUX_CIL" ]] || fail "missing packaging/selinux/linuxio.cil"
selinux_fixture=$(mktemp -d)
selinux_stub="${selinux_fixture}/stub"
selinux_relabel_only="${selinux_fixture}/relabel-only"
selinux_tools="${selinux_fixture}/tools"
selinux_writable="${selinux_fixture}/etc-selinux"
mkdir -p "$selinux_stub" "$selinux_relabel_only" "$selinux_tools" "$selinux_writable"
for tool in bash cp mkdir cmp cat rm basename grep; do
	ln -s "$(command -v "$tool")" "${selinux_tools}/${tool}"
done
cat >"${selinux_stub}/semodule" <<'STUB'
#!/usr/bin/env bash
# FIXTURE_LIST_VERSIONS=1 lists "name<TAB>version", as older semodule does.
suffix=""
[[ ${FIXTURE_LIST_VERSIONS:-0} != 1 ]] || suffix=$'\t1.0.0'
case "$1" in
-l)
	printf 'semodule -l\n' >>"$FIXTURE_LOG"
	[[ $FIXTURE_STORE == 1 ]] || {
		echo "SELinux policy is not managed or store cannot be accessed." >&2
		exit 1
	}
	printf 'abrt%s\n' "$suffix"
	[[ -z $FIXTURE_LOADED ]] || printf 'linuxio%s\n' "$suffix"
	[[ -z ${FIXTURE_LIST_EXTRA:-} ]] || printf '%s%s\n' "$FIXTURE_LIST_EXTRA" "$suffix"
	printf 'zoneminder%s\n' "$suffix"
	;;
-E)
	printf 'semodule -E %s\n' "$2" >>"$FIXTURE_LOG"
	[[ -n $FIXTURE_LOADED ]] || {
		echo "semodule:  Failed on $2!" >&2
		exit 1
	}
	cp "$FIXTURE_LOADED" "./$2.cil"
	;;
-i)
	printf 'semodule -i %s\n' "$(basename "$2")" >>"$FIXTURE_LOG"
	cmp -s "$2" "$FIXTURE_CIL" || {
		echo "semodule -i was given another file" >&2
		exit 1
	}
	[[ $FIXTURE_LOAD_FAILS != 1 ]] || {
		echo "semodule:  Failed!" >&2
		exit 1
	}
	;;
-r) printf 'semodule -r %s\n' "$2" >>"$FIXTURE_LOG" ;;
*) printf 'semodule %s\n' "$*" >>"$FIXTURE_LOG" ;;
esac
STUB
printf '#!/usr/bin/env bash\nprintf "restorecon %%s\\n" "$*" >>"$FIXTURE_LOG"\nexit "$FIXTURE_RESTORECON_EXIT"\n' >"${selinux_stub}/restorecon"
printf '#!/usr/bin/env bash\n[[ $FIXTURE_ENABLED == 1 ]]\n' >"${selinux_stub}/selinuxenabled"
chmod +x "${selinux_stub}/semodule" "${selinux_stub}/restorecon" "${selinux_stub}/selinuxenabled"
cp "${selinux_stub}/restorecon" "${selinux_relabel_only}/restorecon"
printf '; an older module\n' >"${selinux_fixture}/older.cil"
# A checkout without the policy file, so its fetch fails, and one whose policy
# file is not of the shipped form.
mkdir -p "${selinux_fixture}/no-policy/packaging" "${selinux_fixture}/bad-policy/packaging/selinux"
sed 's/^(allow linuxio_auth_t unconfined_t /(allow staff_t unconfined_t /' "$SELINUX_CIL" \
	>"${selinux_fixture}/bad-policy/packaging/selinux/linuxio.cil"

# selinux_install DISTRO [NAME=VALUE...] runs install_selinux_module as on
# DISTRO, from the checkout, with a writable SELinux folder and the stand-ins
# first on PATH. NAME=VALUE changes that: STORE=0 (semodule lists no store),
# LOADED=FILE (the store holds FILE as linuxio), LOAD_FAILS=1, ENABLED=0
# (selinuxenabled says no), RESTORECON_EXIT=1, DIR (the SELinux folder),
# CHECKOUT, TOOLPATH (the PATH). It prints the functions' output, then the
# calls, each after a "call: " mark.
selinux_install() {
	local distro=$1 log="${selinux_fixture}/calls"
	shift
	local STORE=1 LOADED="" LOAD_FAILS=0 ENABLED=1 RESTORECON_EXIT=0
	local DIR=$selinux_writable CHECKOUT=$selinux_checkout TOOLPATH="${selinux_stub}:${selinux_tools}"
	local "$@"
	: >"$log"
	FIXTURE_LOG=$log FIXTURE_STORE=$STORE FIXTURE_LOADED=$LOADED FIXTURE_LOAD_FAILS=$LOAD_FAILS \
		FIXTURE_ENABLED=$ENABLED FIXTURE_RESTORECON_EXIT=$RESTORECON_EXIT FIXTURE_CIL=$SELINUX_CIL \
		bash -c 'fixture_distro=$2 checkout=$3
			source "$1"
			REPO_ROOT=$checkout
			LOCAL_INSTALL=1
			detect_distro() { DISTRO=$fixture_distro; }
			PATH=$4
			install_selinux_module "$5"
			echo "the install went on"' _ "$RELEASE_INSTALLER" "$distro" "$CHECKOUT" "$TOOLPATH" "$DIR" 2>&1
	sed 's/^/call: /' "$log"
}
selinux_calls() { printf '%s\n' "$1" | sed -n 's/^call: //p'; }
# selinux_case LABEL EXPECTED_CALLS NOTICE OUT: the calls are EXPECTED_CALLS,
# the install went on, and the output has NOTICE (a pattern; empty: no notice).
selinux_case() {
	assert_eq "$2" "$(selinux_calls "$4")" "SELinux calls ${1}"
	assert_line "the install went on" "$4" "install ${1}"
	if [[ -n $3 ]]; then
		printf '%s\n' "$4" | grep -Eq "NOTICE.*$3" || fail "no notice ${1}: ${4}"
	elif printf '%s\n' "$4" | grep -q NOTICE; then
		fail "a notice ${1}: ${4}"
	fi
}
loaded="semodule -l
semodule -E linuxio
semodule -i linuxio.cil
restorecon /usr/libexec/linuxio/linuxio-auth"
selinux_case "on Fedora without the module" "$loaded" "" "$(selinux_install fedora)"
selinux_case "with an older module loaded" "$loaded" "" "$(selinux_install rocky LOADED="${selinux_fixture}/older.cil")"
# The store holds the shipped module: no load (about 5 s), only the relabel
# the replaced binary needs.
selinux_case "with the shipped module loaded" "semodule -l
semodule -E linuxio
restorecon /usr/libexec/linuxio/linuxio-auth" "" "$(selinux_install almalinux LOADED="$SELINUX_CIL")"
out=$(selinux_install fedora LOAD_FAILS=1)
selinux_case "when the load fails" "$loaded" "did not load.*semodule -i ${selinux_checkout}/packaging/selinux/linuxio.cil" "$out"
# A new or changed module where the SELinux folder is read-only (the in-app
# updater) is not tried; the notice names the command that loads it.
selinux_case "with the SELinux folder read-only" "semodule -l
semodule -E linuxio
restorecon /usr/libexec/linuxio/linuxio-auth" "read-only here.*semodule -i ${selinux_checkout}/packaging/selinux/linuxio.cil" \
	"$(selinux_install fedora LOADED="${selinux_fixture}/older.cil" DIR="${selinux_fixture}/no-such-dir")"
selinux_case "when the fetch fails" "semodule -l
restorecon /usr/libexec/linuxio/linuxio-auth" "Failed to fetch" "$(selinux_install fedora CHECKOUT="${selinux_fixture}/no-policy")"
selinux_case "with a policy file not of the shipped form" "semodule -l
restorecon /usr/libexec/linuxio/linuxio-auth" "not what LinuxIO ships" "$(selinux_install fedora CHECKOUT="${selinux_fixture}/bad-policy")"
# No policy store: with SELinux on, a notice; either way the relabel.
selinux_case "without a policy store, SELinux on" "semodule -l
restorecon /usr/libexec/linuxio/linuxio-auth" "not checked" "$(selinux_install centos STORE=0)"
selinux_case "without a policy store, SELinux off" "semodule -l
restorecon /usr/libexec/linuxio/linuxio-auth" "" "$(selinux_install centos STORE=0 ENABLED=0)"
selinux_case "without semodule" "restorecon /usr/libexec/linuxio/linuxio-auth" "" \
	"$(selinux_install fedora TOOLPATH="${selinux_relabel_only}:${selinux_tools}")"
selinux_case "without semodule and restorecon" "" "" "$(selinux_install fedora TOOLPATH="$selinux_tools")"
selinux_case "when restorecon fails" "$loaded" "Failed to label" "$(selinux_install fedora RESTORECON_EXIT=1)"
for distro in ubuntu debian linuxmint pop; do
	selinux_case "on ${distro}" "" "" "$(selinux_install "$distro")"
done

# The fetched file is loaded as root with no checksum, so only the shipped
# form passes: LinuxIO's own types and paths, rules from linuxio_auth_t, the
# constraint as shipped.
selinux_valid() {
	bash -c 'source "$1"; selinux_module_valid "$2"' _ "$RELEASE_INSTALLER" "$1"
}
selinux_valid "$SELINUX_CIL" || fail "the shipped linuxio.cil is not of the shipped form"
selinux_refused() {
	local file="${selinux_fixture}/changed.cil"
	printf '%s\n' "$2" >"$file"
	if selinux_valid "$file"; then
		fail "selinux_module_valid accepted ${1}"
	fi
}
selinux_refused "an allow from another domain" "$(sed 's/^(allow linuxio_auth_t unconfined_t /(allow staff_t unconfined_t /' "$SELINUX_CIL")"
selinux_refused "an attribute for another type" "$(sed 's/^(typeattributeset daemon (linuxio_auth_t))/(typeattributeset daemon (staff_t))/' "$SELINUX_CIL")"
selinux_refused "a typepermissive line" "$(
	cat "$SELINUX_CIL"
	echo '(typepermissive linuxio_auth_t)'
)"
selinux_refused "a file context for another path" "$(sed 's#"/run/linuxio/issue"#"/etc/shadow"#' "$SELINUX_CIL")"
selinux_refused "a changed constraint" "$(sed 's/(eq u2 unconfined_u)/(eq u2 staff_u)/' "$SELINUX_CIL")"
selinux_refused "no constraint" "$(grep -v '^(constrain ' "$SELINUX_CIL")"
selinux_refused "a control character" "$(
	cat "$SELINUX_CIL"
	printf '; \001\n'
)"

# In main the module comes right after the binaries, at main's top level (so
# on the --skip-binaries path too and inside no condition), before any later
# step that can end the install and before the systemd steps and the restart.
main_body=$(sed -n '/^main() {/,/^}/p' "$RELEASE_INSTALLER")
main_line() { printf '%s\n' "$main_body" | grep -n -m1 -- "$1" | cut -d: -f1 || true; }
selinux_at=$(main_line '^	install_selinux_module "\$SELINUX_DIR"$')
[[ -n "$selinux_at" ]] || fail "main does not call install_selinux_module at its top level"
(($(main_line 'if ! install_binaries') < selinux_at)) || fail "main loads the SELinux module before the binaries are in place"
skip_block_end=$(printf '%s\n' "$main_body" | awk -v from="$(main_line 'Step 3/5')" 'NR > from && /^\tfi$/ { print NR; exit }')
((skip_block_end < selinux_at)) || fail "main loads the SELinux module only when it installs the binaries"
if printf '%s\n' "$main_body" | sed -n "$((skip_block_end + 1)),$((selinux_at - 1))p" | grep -q 'Show 1'; then
	fail "a step that can end the install runs between the binaries and the SELinux module"
fi
((selinux_at < $(main_line 'install_systemd_files'))) || fail "main loads the SELinux module after the systemd steps"
((selinux_at < $(main_line 'restart_or_start_services'))) || fail "main loads the SELinux module after the restart"

# The uninstaller removes the module after the binaries: removed while
# linuxio-auth is still there, it would leave that file unlabeled_t, which
# systemd cannot run. Only a module that is loaded, whether semodule lists
# bare names or names with versions, and only with semodule.
rm_bin_at=$(grep -n -m1 -Fx 'rm -rf /usr/libexec/linuxio' "$UNINSTALLER" | cut -d: -f1 || true)
[[ -n "$rm_bin_at" ]] || fail "the uninstaller does not remove /usr/libexec/linuxio"
# It removes the copies earlier releases left in /usr/local/bin by the
# installer's names, and nothing else there.
uninstall_old=$(sed -n 's/^for binary in \(linuxio .*\); do$/\1/p' "$UNINSTALLER" | tr ' ' '\n')
assert_eq "$(printf '%s\n' "$binary_list" linuxio-btrfs-timer)" "$uninstall_old" "names the uninstaller removes from /usr/local/bin"
grep -Fqx '	rm -f "/usr/local/bin/${binary}"' "$UNINSTALLER" || fail "the uninstaller does not remove the old copies by name"
grep -Fqx 'rm -f /usr/bin/linuxio' "$UNINSTALLER" || fail "the uninstaller does not remove /usr/bin/linuxio"
rm_module_at=$(grep -n -m1 -F 'semodule -r linuxio' "$UNINSTALLER" | cut -d: -f1 || true)
[[ -n "$rm_module_at" ]] || fail "the uninstaller does not remove the SELinux module"
((rm_bin_at < rm_module_at)) || fail "the uninstaller removes the SELinux module before the binaries"
# selinux_uninstall LOADED PATH [VERSIONS] [EXTRA] runs the uninstaller's
# module block with the uninstaller's shell options.
selinux_uninstall() {
	local log="${selinux_fixture}/calls"
	: >"$log"
	FIXTURE_LOG=$log FIXTURE_STORE=1 FIXTURE_LOADED=$1 FIXTURE_LOAD_FAILS=0 FIXTURE_CIL=$SELINUX_CIL \
		FIXTURE_LIST_VERSIONS=${3:-0} FIXTURE_LIST_EXTRA=${4:-} PATH=$2 \
		bash -c 'set -euo pipefail
			Show() { printf "%s\n" "$*"; }
			'"$(sed -n '/^# ========== REMOVE THE SELINUX MODULE/,/^# ========== REMOVE CONFIGURATION/p' "$UNINSTALLER" | sed '$d')"'
			echo "the uninstall went on"' 2>&1
	sed 's/^/call: /' "$log"
}
for versions in 0 1; do
	out=$(selinux_uninstall "$SELINUX_CIL" "${selinux_stub}:${selinux_tools}" "$versions")
	assert_eq "semodule -l
semodule -r linuxio" "$(selinux_calls "$out")" "uninstall with the module loaded (versions listed: ${versions})"
	assert_line "the uninstall went on" "$out" "uninstall with the module loaded"
	out=$(selinux_uninstall "" "${selinux_stub}:${selinux_tools}" "$versions" linuxio_extra)
	assert_eq "semodule -l" "$(selinux_calls "$out")" "uninstall without the module (versions listed: ${versions})"
	assert_line "the uninstall went on" "$out" "uninstall without the module"
done
out=$(selinux_uninstall "" "$selinux_tools")
assert_eq "" "$(selinux_calls "$out")" "uninstall without semodule"
assert_line "the uninstall went on" "$out" "uninstall without semodule"

# The policy file: its file contexts name only files LinuxIO puts there (the
# auth binary in the installer's LIBEXEC_DIR, the banner update-issue writes); the
# banner's sits in an optional block, so a policy without its type still
# loads the rest; and its parentheses balance outside comments and strings.
libexec_dir=$(bash -c 'source "$1"; printf "%s\n" "$LIBEXEC_DIR"' _ "$RELEASE_INSTALLER")
issue_file=$(sed -n 's/^OUTPUT_FILE="\${OUTPUT_FILE:-\(.*\)}"$/\1/p' "${SCRIPT_DIR}/../update-issue")
[[ -n "$issue_file" ]] || fail "update-issue has no default OUTPUT_FILE"
printf '%s\n' "$binary_list" | grep -Fqx linuxio-auth || fail "linuxio-auth is not an installed binary"
filecon_paths=$(sed -n 's/^[[:blank:]]*(filecon "\([^"]*\)".*/\1/p' "$SELINUX_CIL")
assert_line "${libexec_dir}/linuxio-auth" "$filecon_paths" "file context of the auth binary"
while IFS= read -r path; do
	case "$path" in
	"${libexec_dir}/linuxio-auth" | "$issue_file") ;;
	*) fail "linuxio.cil gives a file context to ${path}, which LinuxIO does not install" ;;
	esac
done <<<"$filecon_paths"
# A file made at run time has its folder's type until something relabels it.
if printf '%s\n' "$filecon_paths" | grep -Fqx "$issue_file"; then
	grep -Fq 'restorecon "${OUTPUT_FILE}"' "${SCRIPT_DIR}/../update-issue" ||
		fail "update-issue does not relabel ${issue_file}, which linuxio.cil gives a type"
	assert_eq "(optional linuxio_banner" "$(grep -B1 -F "\"${issue_file}\"" "$SELINUX_CIL" | head -1)" \
		"the line before the banner's file context"
fi
assert_eq "$(grep -c '^[[:blank:]]*(filecon ' "$SELINUX_CIL")" "$(grep -c '(filecon ' "$SELINUX_CIL")" "filecon lines of linuxio.cil"
cil_depth=$(sed -e 's/;.*//' -e 's/"[^"]*"//g' "$SELINUX_CIL" | awk '
	{ for (i = 1; i <= length($0); i++) { c = substr($0, i, 1); if (c == "(") d++; else if (c == ")") d--; if (d < 0) { print "negative at line " NR; exit } } }
	END { print d }')
assert_eq "0" "$cil_depth" "parenthesis depth at the end of linuxio.cil"
rm -rf "$selinux_fixture"
printf '   \033[1;32m✓\033[0m %s\n' "SELinux module of the shipped form loaded on the RHEL family where it can and differs, the auth binary relabelled wherever restorecon exists, right after the binaries; removed after the binaries on uninstall"

# The standalone scripts each carry their own Show/Header; keep them identical.
ui_sum() { sed -n '/^Show() {/,/^}/p;/^Header() {/,/^}/p;/^readonly COLOUR_RESET/,/^readonly YELLOW/p' "$1" | sha256sum; }
for script in install-dependencies.sh uninstall.sh; do
	assert_eq "$(ui_sum "$RELEASE_INSTALLER")" "$(ui_sum "${SCRIPT_DIR}/../${script}")" "${script} Show/Header styling"
done
printf '   \033[1;32m✓\033[0m %s\n' "standalone scripts share Show/Header styling"

# No fixture may leave the installer's staging directory behind. A directory
# that another run, still going, created goes away within moments; a leaked one
# stays.
for _ in 1 2 3 4 5 6 7 8 9 10; do
	staging_left=$(comm -13 <(printf '%s\n' "$STAGING_BEFORE") <(compgen -G '/tmp/linuxio-install.*' | sort || true))
	[[ -z "$staging_left" ]] && break
	sleep 0.5
done
[[ -z "$staging_left" ]] || fail "the fixtures left staging directories behind: ${staging_left}"
printf '   \033[1;32m✓\033[0m %s\n' "no staging directory left behind"

printf '\033[1;32m%s\033[0m\n' "✅ Installer and recovery-asset fixtures passed!"
