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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
readonly REPO_ROOT
readonly SOCKET_PATH=/run/linuxio/indexer.sock
readonly CONFIG_PATH=/etc/linuxio/indexer/config.yaml
readonly DB_PATH=/var/lib/linuxio/indexer/indexer.db
readonly TCP_ADDR=127.0.0.1:18080
readonly TCP_URL="http://${TCP_ADDR}"
readonly TCP_SOCKET=/etc/systemd/system/linuxio-indexer-tcp.socket
SMOKE_DIR="$(mktemp -d /tmp/linuxio-indexer-smoke.XXXXXX)"
readonly SMOKE_DIR
readonly INDEX_ROOT="${SMOKE_DIR}/index-root"
readonly SMOKE_CONFIG="${SMOKE_DIR}/config.yaml"
readonly SERVICE_DROPIN=/run/systemd/system/linuxio-indexer.service.d/smoke.conf
readonly INDEX_DROPIN=/run/systemd/system/linuxio-indexer-index.service.d/smoke.conf
readonly TIMER_DROPIN=/run/systemd/system/linuxio-indexer-index.timer.d/smoke.conf
installed=0

cleanup() {
	local status=$?
	if [[ $installed -eq 1 ]]; then
		bash "${REPO_ROOT}/packaging/scripts/uninstall.sh" --remove-data >/dev/null 2>&1 || true
	fi
	rm -f "$SERVICE_DROPIN" "$INDEX_DROPIN" "$TIMER_DROPIN"
	rmdir \
		/run/systemd/system/linuxio-indexer.service.d \
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
	/etc/systemd/system/linuxio-indexer-tcp.socket \
	/etc/systemd/system/linuxio-indexer.service; do
	[[ ! -e "$path" ]] || fail "refusing to run with existing LinuxIO state: ${path}"
done
for binary in linuxio linuxio-webserver linuxio-bridge linuxio-auth linuxio-docker-update linuxio-indexer; do
	[[ -x "${REPO_ROOT}/${binary}" ]] || fail "build ${binary} before running the smoke test"
done

mkdir -p "$INDEX_ROOT"
printf 'before index\n' >"${INDEX_ROOT}/before.txt"
mkdir -p "${INDEX_ROOT}/docs"
printf 'indexed document\n' >"${INDEX_ROOT}/docs/document.txt"

installed=1
if ! bash "${REPO_ROOT}/packaging/scripts/localinstall.sh" >"${SMOKE_DIR}/install.log" 2>&1; then
	tail -n 40 "${SMOKE_DIR}/install.log" >&2 || true
	fail "local installer failed; full log is ${SMOKE_DIR}/install.log"
fi

sed \
	-e "s|^index_path:.*|index_path: ${INDEX_ROOT}|" \
	-e "s|^db_path:.*|db_path: ${SMOKE_DIR}/indexer.db|" \
	-e 's|^idle_timeout:.*|idle_timeout: 2s|' \
	-e 's|^interval:.*|interval: 1h|' \
	-e "s|^listen_addr:.*|listen_addr: \"${TCP_ADDR}\"|" \
	"$CONFIG_PATH" >"$SMOKE_CONFIG"
install -o root -g root -m 0644 "$SMOKE_CONFIG" "$CONFIG_PATH"

mkdir -p "$(dirname "$SERVICE_DROPIN")" "$(dirname "$INDEX_DROPIN")" "$(dirname "$TIMER_DROPIN")"
printf '%s\n' '[Service]' 'ExecStart=' \
	"ExecStart=/usr/local/bin/linuxio-indexer --config-file ${CONFIG_PATH}" \
	"ReadWritePaths=${SMOKE_DIR} ${INDEX_ROOT}" >"$SERVICE_DROPIN"
printf '%s\n' '[Service]' 'ExecStart=' \
	"ExecStart=/usr/local/bin/linuxio-indexer --trigger-index --socket-path ${SOCKET_PATH}" \
	"ReadWritePaths=${SMOKE_DIR} ${INDEX_ROOT}" >"$INDEX_DROPIN"
printf '%s\n' '[Timer]' 'OnActiveSec=' 'OnUnitActiveSec=' 'OnActiveSec=1s' 'OnUnitActiveSec=1s' >"$TIMER_DROPIN"
printf '%s\n' \
	'[Unit]' \
	'Description=LinuxIO Indexer Read-Only TCP API Socket' \
	'PartOf=linuxio.target' \
	'Requires=linuxio-webserver.socket' \
	'After=linuxio-webserver.socket' \
	'BindsTo=linuxio-webserver.socket' \
	'' \
	'[Socket]' \
	"ListenStream=${TCP_ADDR}" \
	'Accept=no' \
	'Service=linuxio-indexer.service' \
	'FileDescriptorName=indexer-tcp' \
	'FlushPending=true' \
	'' \
	'[Install]' \
	'WantedBy=linuxio.target linuxio-webserver.socket' >"$TCP_SOCKET"
systemctl daemon-reload
systemctl enable --now linuxio-indexer-tcp.socket
systemctl stop linuxio-indexer.service 2>/dev/null || true
systemctl start linuxio-indexer.socket
systemctl is-active --quiet linuxio-webserver.socket || fail "indexer socket did not bring up webserver socket"
systemctl is-active --quiet linuxio-indexer.socket || fail "indexer socket is not active"
systemctl is-active --quiet linuxio-indexer-tcp.socket || fail "indexer TCP socket is not active"

curl_unix() {
	local endpoint="$1"
	shift
	curl --fail --silent --show-error --unix-socket "$SOCKET_PATH" "$@" "http://localhost${endpoint}"
}

curl_tcp() {
	local endpoint="$1"
	shift
	curl --fail --silent --show-error "$@" "${TCP_URL}${endpoint}"
}

wait_for_idle() {
	local body
	for _ in {1..30}; do
		body=$(curl_unix /status 2>/dev/null || true)
		if jq -e '.status == "idle"' >/dev/null 2>&1 <<<"$body"; then return 0; fi
		sleep 1
	done
	fail "indexer did not return to idle"
}

trigger_index() {
	curl_unix /index -X POST >/dev/null
	wait_for_idle
}

trigger_index
status=$(curl_unix /status)
jq -e '.num_files >= 2 and .num_dirs >= 1' >/dev/null <<<"$status" || fail "full index did not record the disposable tree"
jq -e '.size >= 1' >/dev/null <<<"$(curl_unix '/dirsize?path=/')" || fail "directory-size query failed"
jq -e 'length > 0' >/dev/null <<<"$(curl_unix '/search?q=document')" || fail "search query failed"
jq -e 'length > 0' >/dev/null <<<"$(curl_tcp '/search?q=document')" || fail "authentication-free TCP search failed"
[[ "$(curl --silent --output /dev/null --write-out '%{http_code}' -X POST "${TCP_URL}/index")" == 403 ]] ||
	fail "TCP mutation was not rejected"

printf 'added through mutation\n' >"${INDEX_ROOT}/added.txt"
curl_unix /add -X POST -H 'Content-Type: application/json' \
	-d "{\"path\":\"/\",\"absPath\":\"${INDEX_ROOT}/added.txt\",\"name\":\"added.txt\",\"size\":22,\"type\":\"file\"}" >/dev/null
jq -e 'length > 0' >/dev/null <<<"$(curl_unix '/search?q=added')" || fail "mutation update was not queryable"

printf 'added through reindex\n' >"${INDEX_ROOT}/reindexed.txt"
curl_unix '/reindex?path=/' -X POST >/dev/null
wait_for_idle
jq -e 'length > 0' >/dev/null <<<"$(curl_unix '/search?q=reindexed')" || fail "scoped reindex was not queryable"

systemctl start linuxio-indexer-index.timer
last_trigger=
for _ in {1..15}; do
	last_trigger=$(systemctl show --value --property=LastTriggerUSec linuxio-indexer-index.timer)
	[[ -n "$last_trigger" && "$last_trigger" != n/a ]] && break
	sleep 1
done
[[ -n "$last_trigger" && "$last_trigger" != n/a ]] || fail "index timer did not trigger"
systemctl stop linuxio-indexer-index.timer

for _ in {1..15}; do
	if ! systemctl is-active --quiet linuxio-indexer.service; then break; fi
	sleep 1
done
systemctl is-active --quiet linuxio-indexer.service && fail "idle indexer did not shut down"
systemctl is-active --quiet linuxio-indexer-tcp.socket || fail "TCP socket stopped with idle daemon"
curl_tcp /status >/dev/null
systemctl is-active --quiet linuxio-indexer.service || fail "indexer did not reactivate from its TCP socket"

if ! bash "${REPO_ROOT}/packaging/scripts/uninstall.sh" --remove-data >"${SMOKE_DIR}/uninstall.log" 2>&1; then
	tail -n 40 "${SMOKE_DIR}/uninstall.log" >&2 || true
	fail "uninstaller failed; full log is ${SMOKE_DIR}/uninstall.log"
fi
installed=0
[[ ! -e "$CONFIG_PATH" && ! -e "$DB_PATH" && ! -e "$SOCKET_PATH" ]] || fail "uninstall left LinuxIO indexer state behind"
echo "✅ disposable-host systemd indexer smoke test passed"
