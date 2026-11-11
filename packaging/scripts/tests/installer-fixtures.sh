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

# Scheduled units record their runs through two hook lines. The schedule units
# are generated, so the lines reach them (and the two packaged units) through one
# drop-in; linuxio-btrfs-resync@.service carries them itself.
BEGIN_HOOK='ExecStartPre=-+/usr/local/bin/linuxio scheduled-run begin --unit %n'
END_HOOK='ExecStopPost=-+/usr/local/bin/linuxio scheduled-run end --unit %n'
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
		for dropin_dir in linuxio-schedule-.service.d linuxio-btrfs-snapshot.service.d linuxio-docker-update.service.d; do
			dropin_file="${fixture_dir}/system/${dropin_dir}/10-linuxio-run.conf"
			[[ -f "$dropin_file" ]] || fail "missing ${dropin_dir}/10-linuxio-run.conf after install"
			grep -Fqx '[Service]' "$dropin_file" || fail "${dropin_dir} drop-in has no [Service] section"
			grep -Fqx "$BEGIN_HOOK" "$dropin_file" || fail "${dropin_dir} drop-in lacks the begin hook"
			grep -Fqx "$END_HOOK" "$dropin_file" || fail "${dropin_dir} drop-in lacks the end hook"
			assert_eq "644" "$(stat -c %a "$dropin_file")" "${dropin_dir} drop-in mode"
		done
		assert_eq "3" "$(find "${fixture_dir}/system" -mindepth 1 -maxdepth 1 | wc -l)" "installed drop-in directories"
		if compgen -G "${fixture_dir}/system/*/*.new.*" >/dev/null; then
			fail "drop-in install left a temporary file"
		fi

		# Uninstalling removes everything under linuxio*, drop-in directories included.
		mkdir -p "${fixture_dir}/system/unrelated.service.d"
		bash -c "$(grep -F 'rm -rf /etc/systemd/system/linuxio*' "$UNINSTALLER" | sed "s#/etc/systemd/system#${fixture_dir}/system#")"
		for dropin_dir in linuxio-schedule-.service.d linuxio-btrfs-snapshot.service.d linuxio-docker-update.service.d; do
			[[ ! -e "${fixture_dir}/system/${dropin_dir}" ]] || fail "uninstall left ${dropin_dir}"
		done
		[[ -d "${fixture_dir}/system/unrelated.service.d" ]] || fail "uninstall removed a directory that is not LinuxIO's"
		rm -rf "$fixture_dir"
	)
}
run_record_dropin_fixtures
# shellcheck disable=SC2031
RESYNC_UNIT="${REPO_ROOT}/packaging/systemd/linuxio-btrfs-resync@.service"
grep -Fqx "$BEGIN_HOOK" "$RESYNC_UNIT" || fail "the resync unit lacks the begin hook"
grep -Fqx "$END_HOOK" "$RESYNC_UNIT" || fail "the resync unit lacks the end hook"
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
		assert_eq "6" "$(grep -c '^\$nrconf' "$NEEDRESTART_CONF")" "shipped needrestart override entries"
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
die "override_rc has " . scalar(keys %$rc) . " entries, want 8\n" unless 8 == keys %$rc;
sub restarts { my $unit = shift; for my $re (sort keys %$rc) { return $rc->{$re} if $unit =~ /$re/ } 1 }
my $id = "0b7c9a1e-5d2f-4c3b-9a8e-1f2e3d4c5b6a";
for my $unit ("linuxio-jobs\@0.service", "linuxio-jobs\@1001.service", "linuxio-schedule-$id.service",
	"linuxio-btrfs-snapshot.service", "linuxio-btrfs-resync\@$id.service", "linuxio-docker-update.service",
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
