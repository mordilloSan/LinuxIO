#!/usr/bin/env bash
# Opt-in smoke test for a disposable systemd host.
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
command -v jq >/dev/null || fail "jq is required"
CURL_BIN="$(command -v curl)"
readonly CURL_BIN

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
readonly REPO_ROOT
readonly SOCKET_PATH=/run/linuxio/indexer.sock
readonly CONFIG_PATH=/etc/linuxio/indexer/config.yaml
readonly DB_PATH=/var/lib/linuxio/indexer/indexer.db
SMOKE_DIR="$(mktemp -d /tmp/linuxio-indexer-smoke.XXXXXX)"
readonly SMOKE_DIR
readonly INDEX_DROPIN=/run/systemd/system/linuxio-indexer-index.service.d/smoke.conf
readonly TIMER_DROPIN=/run/systemd/system/linuxio-indexer-index.timer.d/smoke.conf
installed=0

cleanup() {
	local status=$?
	if [[ $installed -eq 1 ]]; then
		bash "${REPO_ROOT}/packaging/scripts/uninstall.sh" --remove-data >/dev/null 2>&1 || true
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

for path in \
	/etc/linuxio \
	/var/lib/linuxio \
	/run/linuxio \
	/usr/local/bin/linuxio-indexer \
	/etc/systemd/system/linuxio.target \
	/etc/systemd/system/linuxio-indexer.socket \
	/etc/systemd/system/linuxio-indexer.service; do
	[[ ! -e "$path" ]] || fail "refusing to run with existing LinuxIO state: ${path}"
done
for binary in linuxio linuxio-webserver linuxio-bridge linuxio-auth linuxio-docker-update linuxio-indexer; do
	[[ -x "${REPO_ROOT}/${binary}" ]] || fail "build ${binary} before running the smoke test"
done

installed=1
if ! bash "${REPO_ROOT}/packaging/scripts/localinstall.sh" >"${SMOKE_DIR}/install.log" 2>&1; then
	tail -n 40 "${SMOKE_DIR}/install.log" >&2 || true
	fail "local installer failed; full log is ${SMOKE_DIR}/install.log"
fi

mkdir -p "$(dirname "$INDEX_DROPIN")" "$(dirname "$TIMER_DROPIN")"
# Exercise timer wiring without launching a full-root scan.
printf '%s\n' '[Service]' 'ExecStart=' \
	"ExecStart=${CURL_BIN} --fail --silent --unix-socket ${SOCKET_PATH} http://localhost/status" >"$INDEX_DROPIN"
printf '%s\n' '[Timer]' 'OnActiveSec=' 'OnActiveSec=1s' >"$TIMER_DROPIN"
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

systemctl start linuxio-indexer-index.timer
last_trigger=
for _ in {1..15}; do
	last_trigger=$(systemctl show --value --property=LastTriggerUSec linuxio-indexer-index.timer)
	[[ -n "$last_trigger" && "$last_trigger" != n/a ]] && break
	sleep 1
done
[[ -n "$last_trigger" && "$last_trigger" != n/a ]] || fail "index timer did not trigger"
systemctl is-active --quiet linuxio-indexer.service || fail "timer request did not activate the indexer"
systemctl stop linuxio-indexer-index.timer

systemctl stop linuxio-indexer.service
curl_unix /status >/dev/null
systemctl is-active --quiet linuxio-indexer.service || fail "indexer did not reactivate from its Unix socket"

if ! bash "${REPO_ROOT}/packaging/scripts/uninstall.sh" --remove-data >"${SMOKE_DIR}/uninstall.log" 2>&1; then
	tail -n 40 "${SMOKE_DIR}/uninstall.log" >&2 || true
	fail "uninstaller failed; full log is ${SMOKE_DIR}/uninstall.log"
fi
installed=0
[[ ! -e "$CONFIG_PATH" && ! -e "$DB_PATH" && ! -e "$SOCKET_PATH" ]] || fail "uninstall left LinuxIO indexer state behind"
echo "✅ disposable-host systemd indexer smoke test passed"
