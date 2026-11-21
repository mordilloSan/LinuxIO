#!/usr/bin/env bash
# Opt-in smoke test for a disposable systemd host: installs the LinuxIO
# package LINUXIO_SMOKE_PACKAGE (a DEB or an RPM, made by make package-deb or
# package-rpm) with apt or dnf, checks the indexer's socket activation, idle
# exit and index timer under systemd, and removes the package again.
set -euo pipefail

if [[ "${LINUXIO_SYSTEMD_SMOKE:-}" != 1 ]]; then
	echo "systemd indexer smoke test skipped (set LINUXIO_SYSTEMD_SMOKE=1 to enable)"
	exit 0
fi

fail() {
	echo "❌ $*" >&2
	exit 1
}

[[ "${LINUXIO_SYSTEMD_SMOKE_CONFIRM:-}" == "disposable-linuxio-host" ]] ||
	fail "set LINUXIO_SYSTEMD_SMOKE_CONFIRM=disposable-linuxio-host on a disposable host"
[[ $EUID -eq 0 ]] || fail "systemd smoke test must run as root"
[[ "$(ps -p 1 -o comm=)" == systemd ]] || fail "PID 1 is not systemd"
command -v curl >/dev/null || fail "curl is required"
package="${LINUXIO_SMOKE_PACKAGE:-}"
[[ -n "$package" ]] || fail "set LINUXIO_SMOKE_PACKAGE to the DEB or RPM to install (make package-deb or package-rpm)"
[[ -f "$package" ]] || fail "no package at ${package}"
package="$(realpath -- "$package")"
case "$package" in
*.deb) command -v apt-get >/dev/null || fail "apt-get is required for ${package}" ;;
*.rpm) command -v dnf >/dev/null || fail "dnf is required for ${package}" ;;
*) fail "${package} is neither a DEB nor an RPM" ;;
esac
command -v jq >/dev/null || fail "jq is required"
CURL_BIN="$(command -v curl)"
readonly CURL_BIN

readonly SOCKET_PATH=/run/linuxio/indexer.sock
readonly DB_PATH=/var/lib/linuxio/indexer/indexer.db
SMOKE_DIR="$(mktemp -d /tmp/linuxio-indexer-smoke.XXXXXX)"
readonly SMOKE_DIR
readonly INDEX_DROPIN=/run/systemd/system/linuxio-indexer-index.service.d/smoke.conf
readonly TIMER_DROPIN=/run/systemd/system/linuxio-indexer-index.timer.d/smoke.conf
installed=0
owns_state=0

# remove_package [purge]: the package manager's removal; purge also drops the
# Debian family's configuration files, which a plain removal keeps.
remove_package() {
	case "$package" in
	*.deb) env DEBIAN_FRONTEND=noninteractive apt-get "${1:-remove}" -y -q linuxio ;;
	*.rpm) dnf remove -y -q linuxio ;;
	esac
}

cleanup() {
	local status=$?
	if [[ $installed -eq 1 ]]; then
		remove_package >/dev/null 2>&1 || true
	fi
	# What the package keeps on removal.
	if [[ $owns_state -eq 1 ]]; then
		[[ "$package" != *.deb ]] || remove_package purge >/dev/null 2>&1 || true
		rm -rf /var/lib/linuxio /etc/linuxio
	fi
	rm -f "$INDEX_DROPIN" "$TIMER_DROPIN"
	rmdir \
		/run/systemd/system/linuxio-indexer-index.service.d \
		/run/systemd/system/linuxio-indexer-index.timer.d 2>/dev/null || true
	systemctl daemon-reload >/dev/null 2>&1 || true
	rm -rf "$SMOKE_DIR"
	exit "$status"
}
trap cleanup EXIT INT TERM

if dpkg-query -W -f='${db:Status-Status}' linuxio 2>/dev/null | grep -qx installed || rpm -q linuxio >/dev/null 2>&1; then
	fail "refusing to run with the linuxio package installed"
fi
for path in \
	/etc/linuxio \
	/var/lib/linuxio \
	/run/linuxio \
	/usr/libexec/linuxio/linuxio-indexer \
	/usr/local/bin/linuxio \
	/etc/systemd/system/linuxio.target; do
	[[ ! -e "$path" ]] || fail "refusing to run with existing LinuxIO state: ${path}"
done

# The package's first install enables and starts linuxio.target. From here
# on the LinuxIO state on the host is this run's, and cleanup removes it.
installed=1
owns_state=1
case "$package" in
*.deb) install_cmd=(env DEBIAN_FRONTEND=noninteractive apt-get install -y "$package") ;;
*.rpm) install_cmd=(dnf install -y "$package") ;;
esac
if ! "${install_cmd[@]}" >"${SMOKE_DIR}/install.log" 2>&1; then
	tail -n 40 "${SMOKE_DIR}/install.log" >&2 || true
	fail "the package install failed; full log is ${SMOKE_DIR}/install.log"
fi

mkdir -p "$(dirname "$INDEX_DROPIN")" "$(dirname "$TIMER_DROPIN")"
# Exercise timer wiring without launching a full-root scan.
printf '%s\n' '[Service]' 'ExecStart=' \
	"ExecStart=${CURL_BIN} --fail --silent --unix-socket ${SOCKET_PATH} http://localhost/status" >"$INDEX_DROPIN"
systemctl daemon-reload
systemctl stop linuxio-indexer.service 2>/dev/null || true
systemctl start linuxio-indexer.socket
systemctl is-active --quiet linuxio-webserver.socket || fail "indexer socket did not bring up webserver socket"
systemctl is-active --quiet linuxio-indexer.socket || fail "indexer socket is not active"
systemctl is-active --quiet linuxio-indexer.service && fail "indexer service started before an API request"

curl_unix() {
	local endpoint="$1"
	curl --fail --silent --show-error --unix-socket "$SOCKET_PATH" "http://localhost${endpoint}"
}

status=$(curl_unix /status)
jq -e '.status == "uninitialized"' >/dev/null <<<"$status" || fail "fresh indexer did not report uninitialized"
systemctl is-active --quiet linuxio-indexer.service || fail "Unix request did not activate the indexer"

for _ in {1..150}; do
	if ! systemctl is-active --quiet linuxio-indexer.service; then break; fi
	sleep 1
done
systemctl is-active --quiet linuxio-indexer.service && fail "idle indexer did not shut down"

# The timer came up with the socket (linuxio.target wants it) on its hourly
# schedule. Only now, with the indexer stopped, is it made to fire 1s after a
# restart: firing earlier would start the indexer before the checks above, and
# the unit's 1min accuracy would let systemd put the firing off past the wait.
printf '%s\n' '[Timer]' 'OnActiveSec=' 'OnActiveSec=1s' 'AccuracySec=1s' >"$TIMER_DROPIN"
systemctl daemon-reload
systemctl restart linuxio-indexer-index.timer
last_trigger=
for _ in {1..15}; do
	last_trigger=$(systemctl show --value --property=LastTriggerUSec linuxio-indexer-index.timer)
	[[ -n "$last_trigger" && "$last_trigger" != n/a ]] && break
	sleep 1
done
[[ -n "$last_trigger" && "$last_trigger" != n/a ]] || fail "index timer did not trigger"
# The index service's request reaches the socket just after the trigger.
for _ in {1..10}; do
	systemctl is-active --quiet linuxio-indexer.service && break
	sleep 1
done
systemctl is-active --quiet linuxio-indexer.service || fail "timer request did not activate the indexer"
systemctl stop linuxio-indexer-index.timer

systemctl stop linuxio-indexer.service
curl_unix /status >/dev/null
systemctl is-active --quiet linuxio-indexer.service || fail "indexer did not reactivate from its Unix socket"

# Removal stops and removes the units and keeps the indexer's data, as it
# keeps all of /var/lib/linuxio.
db_before=0
[[ ! -e "$DB_PATH" ]] || db_before=1
if ! remove_package >"${SMOKE_DIR}/remove.log" 2>&1; then
	tail -n 40 "${SMOKE_DIR}/remove.log" >&2 || true
	fail "the package removal failed; full log is ${SMOKE_DIR}/remove.log"
fi
installed=0
systemctl is-active --quiet linuxio-indexer.service && fail "the indexer runs after the package was removed"
[[ ! -e /usr/lib/systemd/system/linuxio-indexer.socket ]] || fail "removal left the indexer socket unit"
[[ ! -e "$SOCKET_PATH" ]] || fail "removal left ${SOCKET_PATH}"
[[ -d /var/lib/linuxio ]] || fail "removal deleted /var/lib/linuxio"
[[ $db_before -eq 0 || -e "$DB_PATH" ]] || fail "removal deleted the indexer database"
echo "✅ disposable-host systemd indexer smoke test passed"
