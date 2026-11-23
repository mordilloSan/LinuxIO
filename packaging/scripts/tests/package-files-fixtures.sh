#!/usr/bin/env bash
# shellcheck disable=SC2016 # sed and grep patterns hold a literal $
# Fixture-only checks of the files both packages ship from packaging/: how the
# units relate to linuxio.target and to each other, the run-record drop-in,
# the libvirt restart of the post-install steps, the needrestart override,
# the two PAM files and the SELinux policy. Nothing
# is installed and nothing on the host is read but the checkout.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
PACKAGING="${REPO_ROOT}/packaging"
UNITS="${PACKAGING}/systemd"

fail() {
	echo "❌ $*" >&2
	exit 1
}

pass() {
	printf '   \033[1;32m✓\033[0m %s\n' "$1"
}

# Every LinuxIO program a unit starts is one the build makes, and the
# workflows read that list from make instead of typing it again.
binary_list=$(make -s -C "$REPO_ROOT" print-binaries)
while IFS= read -r exec_bin; do
	printf '%s\n' "$binary_list" | grep -Fqx "$exec_bin" || fail "a unit in packaging/systemd starts ${exec_bin}, which make print-binaries does not list"
done < <(sed -nE 's#^ExecStart=-?/usr/libexec/linuxio/([^ ]+).*#\1#p' "${UNITS}"/*.service | sort -u)
if grep -Eq 'linuxio-webserver[ ,]+linuxio-bridge' "${REPO_ROOT}"/.github/workflows/*.yml; then
	fail "workflows must use make print-binaries instead of retyping the binary list"
fi
pass "every program a unit starts is in make print-binaries"

# A linuxio.target restart stops the web socket before it starts the auth
# socket: the auth socket's sd-chown helper otherwise holds the old :8090 fd.
grep -Eq '^After=.*\blinuxio-auth\.socket\b' "${UNITS}/linuxio-webserver.socket" ||
	fail "linuxio-webserver.socket must be ordered After=linuxio-auth.socket"
pass "web socket ordered after the auth socket"

# The indexer: socket-activated on a root-only Unix socket, started by
# linuxio.target's sockets and its own timer, never by the target directly.
for unit in linuxio-indexer.socket linuxio-indexer.service linuxio-indexer-index.service linuxio-indexer-index.timer linuxio-btrfs-health.service linuxio-btrfs-health.timer; do
	grep -Fqx 'PartOf=linuxio.target' "${UNITS}/${unit}" || fail "${unit} is not part of linuxio.target"
done
# Boot-time, resync and rebuild jobs outlive a linuxio.target restart.
for unit in linuxio-btrfs-degraded.service linuxio-btrfs-resync@.service linuxio-btrfs-rebuild@.service; do
	if grep -Fq 'PartOf=linuxio.target' "${UNITS}/${unit}"; then
		fail "${unit} must not stop with linuxio.target"
	fi
done
INDEXER_SOCKET="${UNITS}/linuxio-indexer.socket"
INDEXER_SERVICE="${UNITS}/linuxio-indexer.service"
INDEXER_TIMER="${UNITS}/linuxio-indexer-index.timer"
TARGET="${UNITS}/linuxio.target"
WEBSERVER_SERVICE="${UNITS}/linuxio-webserver.service"
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
grep -Fqx 'ExecStart=/usr/libexec/linuxio/linuxio-indexer --config /etc/linuxio/indexer/config.yaml' "$INDEXER_SERVICE" ||
	fail "indexer service must start the managed daemon directly"
INDEXER_SCAN="${UNITS}/linuxio-indexer-index.service"
grep -Fqx 'ExecStart=/usr/libexec/linuxio/linuxio-indexer --index-mode --config /etc/linuxio/indexer/config.yaml' \
	"$INDEXER_SCAN" || fail "index timer must run the scanner itself"
grep -Fqx 'Type=oneshot' "$INDEXER_SCAN" || fail "the scan unit must be a oneshot, so that its result is the scan's"
grep -Fqx 'Nice=19' "$INDEXER_SCAN" || fail "the scanner must run at the lowest CPU priority"
grep -Fqx 'IOSchedulingClass=idle' "$INDEXER_SCAN" || fail "the scanner must run in the idle I/O class"
grep -Fqx 'ReadWritePaths=/var/lib/linuxio/indexer' "$INDEXER_SCAN" || fail "the scanner must write only the index"
grep -Fqx 'RestrictAddressFamilies=AF_UNIX' "$INDEXER_SCAN" || fail "the scanner must be Unix-only"
grep -Fqx 'CapabilityBoundingSet=CAP_DAC_READ_SEARCH' "$INDEXER_SCAN" || fail "the scanner must keep only the capability to read the host's files"
grep -Fqx 'Requisite=linuxio-indexer.socket' "$INDEXER_SCAN" || fail "the scanner must not run without the daemon's socket"
if grep -rEq 'linuxio-indexer-socket-user|INDEXER_SOCKET_GROUP' "$UNITS" "${REPO_ROOT}/backend/auth"; then
	fail "indexer socket access must not modify the auth launcher"
fi
grep -Fqx 'BindsTo=linuxio-webserver.socket' "$INDEXER_SOCKET" || fail "indexer socket lifecycle"
grep -Fq 'Wants=linuxio-webserver.socket linuxio-auth.socket linuxio-indexer.socket' "$TARGET" || fail "linuxio.target does not own the indexer socket"
if grep -Eq '^Wants=.*linuxio-indexer-index.timer' "$TARGET"; then
	fail "linuxio.target must not bypass the timer's enabled state"
fi
grep -Fqx 'WantedBy=linuxio.target' "$INDEXER_TIMER" || fail "index timer install relationship"
grep -Fqx 'OnActiveSec=15min' "$INDEXER_TIMER" || fail "index timer must make its first run soon after it starts"
grep -Fqx 'OnUnitActiveSec=24h' "$INDEXER_TIMER" || fail "index timer default recurring interval"
# The index folder is no-CoW on btrfs. tmpfiles creates it first: at install the
# service's StateDirectory has not made it yet, and "h" skips a missing path.
index_folder_line=$(grep -Fnx 'd /var/lib/linuxio/indexer 0750 root root -' "${UNITS}/linuxio-tmpfiles.conf" | cut -d: -f1)
index_nocow_line=$(grep -Fnx 'h /var/lib/linuxio/indexer - - - - +C' "${UNITS}/linuxio-tmpfiles.conf" | cut -d: -f1)
[[ -n "$index_folder_line" ]] || fail "tmpfiles must create the index folder"
[[ -n "$index_nocow_line" ]] || fail "tmpfiles must mark the index folder no-CoW"
((index_folder_line < index_nocow_line)) || fail "tmpfiles must create the index folder before marking it"
if grep -Fqx 'Persistent=true' "$INDEXER_TIMER"; then
	fail "Persistent has no effect on a monotonic timer"
fi
grep -Fq 'Wants=linuxio-issue.service linuxio-indexer.socket' "${UNITS}/linuxio-webserver.socket" || fail "webserver socket warm dependency"
grep -Fqx 'Wants=linuxio-indexer.service' "$WEBSERVER_SERVICE" || fail "webserver service must warm the indexer"
grep -Eq '^After=.*linuxio-indexer\.service' "$WEBSERVER_SERVICE" || fail "webserver service must start after the indexer"
grep -Fqx 'RuntimeDirectory=linuxio/webserver' "$WEBSERVER_SERVICE" || fail "webserver runtime marker"
if grep -q '^\[Install\]' "$INDEXER_SERVICE"; then
	fail "indexer service must be socket-activated only"
fi
if grep -Fq 'linuxio-indexer.service' "$TARGET"; then
	fail "linuxio.target must not start the indexer service"
fi
if grep -Evq '^(exclude_paths:|  - |include_network_mounts:|[[:space:]]*$|#)' "${PACKAGING}/etc/linuxio/indexer/config.yaml"; then
	fail "indexer YAML contains non-scan policy"
fi
# The hand-written part of the notices names every icon set the page copies
# icons from: their data ends up in LinuxIO's own source, where neither Vite
# nor the Go build sees a package to list.
[[ -f "${REPO_ROOT}/docs/THIRD_PARTY_NOTICES.md" ]] || fail "the hand-written third-party notices must live under docs"
icon_sets=$(sed -n 's|^ *"@iconify-json/\([^"]*\)":.*|\1|p' "${REPO_ROOT}/frontend/package.json")
[[ -n $icon_sets ]] || fail "found no @iconify-json icon set in frontend/package.json"
for set in $icon_sets; do
	grep -Fq "(\`${set}\`)" "${REPO_ROOT}/docs/THIRD_PARTY_NOTICES.md" ||
		fail "icon set ${set} is not named in docs/THIRD_PARTY_NOTICES.md"
done
pass "indexer units: socket-activated, root-only socket, timer enabled on its own"

# An upgrade restarts linuxio.target and never a job: the job hosts and their
# sockets are not part of it.
for unit in linuxio-jobs@.service linuxio-jobs@.socket; do
	if grep -Eq '^PartOf=.*linuxio\.target' "${UNITS}/${unit}"; then
		fail "${unit} must not stop with linuxio.target"
	fi
done
if grep -Fq 'linuxio-jobs' "$TARGET"; then
	fail "linuxio.target must not pull in job hosts or their sockets"
fi
for retired in linuxio-jobs-launcher.socket linuxio-jobs-reconcile.service; do
	[[ ! -e "${UNITS}/${retired}" ]] || fail "${retired} is retired"
done
JOBS_HOST="${UNITS}/linuxio-jobs@.service"
JOBS_SOCKET="${UNITS}/linuxio-jobs@.socket"
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
if grep -Eq '^(TasksMax|MemoryMax)=' "${UNITS}/linuxio-auth@.service"; then
	fail "the auth worker must not declare a task or memory cap for the login"
fi
# The root host starts at boot to mark, alert and resume what a reboot interrupted.
grep -Fqx 'WantedBy=multi-user.target' "$JOBS_HOST" || fail "the root job host must be enableable at boot"
pass "job units stay outside linuxio.target"

# Each auth connection stays open as its bridge's transport until the session
# ends, so MaxConnections caps live sessions host-wide, not login attempts.
auth_max_connections="$(sed -n 's/^MaxConnections=//p' "${UNITS}/linuxio-auth.socket")"
((auth_max_connections >= 64)) ||
	fail "linuxio-auth.socket MaxConnections=${auth_max_connections} caps live sessions; keep it at least 64"
pass "auth socket leaves room for live sessions"

# Scheduled units record their runs through two hook lines. The generated
# schedule units, the snapshot, replication and Docker update units and the
# distributions' update services get them from the run-record drop-in
# (package-root.sh stages it in each folder); the resync and rebuild units
# carry them themselves.
BEGIN_HOOK='ExecStartPre=-+/usr/libexec/linuxio/linuxio-worker scheduled-run begin --unit %n'
END_HOOK='ExecStopPost=-+/usr/libexec/linuxio/linuxio-worker scheduled-run end --unit %n'
for unit in linuxio-run-record.conf linuxio-btrfs-resync@.service linuxio-btrfs-rebuild@.service; do
	grep -Fqx "$BEGIN_HOOK" "${UNITS}/${unit}" || fail "${unit} lacks the begin hook"
	grep -Fqx "$END_HOOK" "${UNITS}/${unit}" || fail "${unit} lacks the end hook"
done
grep -Fqx '[Service]' "${UNITS}/linuxio-run-record.conf" || fail "the run-record drop-in has no [Service] section"
pass "scheduled units record their runs"

# libvirt's daemon is restarted for the hook on a first install only. At an
# upgrade a daemon with work to finish takes up to 30 seconds to stop and
# answers nobody meanwhile, the sign-in that follows the upgrade among them.
libvirt_restart_on_first_install_only() {
	awk -v first_start="$2" -v first_end="$3" '
		index($0, first_start) { first = 1 }
		$0 ~ first_end { first = 0 }
		/try-restart libvirtd\.service virtqemud\.service/ { restarts++; if (!first) outside = 1 }
		END { exit !(restarts == 1 && !outside) }
	' "$1"
}
libvirt_restart_on_first_install_only "${PACKAGING}/debian/postinst" 'deb-systemd-invoke start $started_units' '^[[:space:]]*else$' ||
	fail "the DEB's postinst must restart libvirt once, beside the start of a first install"
libvirt_restart_on_first_install_only "${PACKAGING}/rpm/linuxio.spec" 'systemctl start %{started_units}' '^fi$' ||
	fail "the RPM's %post must restart libvirt once, beside the start of a first install"
pass "libvirt is restarted on a first install only"

# Loaded the way needrestart.conf loads a conf.d file, the override adds to the
# distribution's override_rc and leaves its entries; deciding as needrestart
# does (the first matching expression, in sorted order), it exempts exactly the
# units that run work an update must not interrupt. Under strict, so an
# interpolated @ in an expression fails.
NEEDRESTART_CONF="${PACKAGING}/etc/needrestart/conf.d/linuxio.conf"
[[ "$(grep -c '^\$nrconf' "$NEEDRESTART_CONF")" == 7 ]] || fail "the needrestart override does not have its 7 entries"
if command -v perl >/dev/null 2>&1; then
	perl -w - "$NEEDRESTART_CONF" <<'PERL' || fail "the needrestart override does not exempt exactly the LinuxIO work units"
use strict;
our %nrconf = (override_rc => { qr(^dbus) => 0, qr(^user@\d+\.service) => 0 });
my $fn = shift;
eval do { local(@ARGV, $/) = $fn; <> };
die "Error parsing $fn: $@" if $@;
my $rc = $nrconf{override_rc};
die "override_rc lost the distribution's entries\n" unless 2 == grep { $_ eq qr(^dbus) || $_ eq qr(^user@\d+\.service) } keys %$rc;
die "override_rc has " . scalar(keys %$rc) . " entries, want 9\n" unless 9 == keys %$rc;
sub restarts { my $unit = shift; for my $re (sort keys %$rc) { return $rc->{$re} if $unit =~ /$re/ } 1 }
my $id = "0b7c9a1e-5d2f-4c3b-9a8e-1f2e3d4c5b6a";
for my $unit ("linuxio-jobs\@0.service", "linuxio-jobs\@1001.service", "linuxio-schedule-$id.service",
	"linuxio-btrfs-snapshot.service", "linuxio-btrfs-replicate.service", "linuxio-btrfs-resync\@$id.service", "linuxio-btrfs-rebuild\@$id.service", "linuxio-docker-update.service") {
	die "$unit would be restarted\n" if restarts($unit);
}
for my $unit ("linuxio-webserver.service", "linuxio-monitoring.service", "linuxio-indexer.service",
	"linuxio-auth\@0-1234-0.service", "linuxio-btrfs-health.service", "linuxio-indexer-index.service",
	"linuxio-issue.service", "linuxio-jobs\@0.socket", "linuxio-jobs-reconcile.service") {
	die "$unit would not be restarted\n" unless restarts($unit);
}
PERL
	pass "needrestart override exempts exactly the LinuxIO work units"
else
	printf '   \033[1;33m-\033[0m %s\n' "needrestart override expressions not checked: no perl"
fi

# The RHEL family's PAM file is the Debian one line for line and in the same
# order, with each Debian-only name mapped to the RHEL family's. Order is what
# matters: a sufficient line in password-auth ends the stack where it is
# included, so a LinuxIO line moved below an include would no longer run.
# Columns may differ.
PAM_DEBIAN="${PACKAGING}/etc/pam.d/linuxio"
PAM_RHEL="${PACKAGING}/etc/pam.d/linuxio.rhel"
[[ -f "$PAM_DEBIAN" ]] || fail "missing packaged etc/pam.d/linuxio"
[[ -f "$PAM_RHEL" ]] || fail "missing packaged etc/pam.d/linuxio.rhel"
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
pass "the RHEL PAM file is the Debian one line for line"

# The SELinux policy: its file contexts name only files LinuxIO puts there (the
# auth binary, the banner update-issue writes, the memory page's swap files);
# the banner's and the swap files' sit in optional blocks, so a policy without
# their types still loads the rest; and its parentheses balance outside
# comments and strings.
SELINUX_CIL="${PACKAGING}/selinux/linuxio.cil"
UPDATE_ISSUE="${PACKAGING}/scripts/update-issue"
auth_binary=/usr/libexec/linuxio/linuxio-auth
printf '%s\n' "$binary_list" | grep -Fqx linuxio-auth || fail "linuxio-auth is not an installed binary"
issue_file=$(sed -n 's/^OUTPUT_FILE="\${OUTPUT_FILE:-\(.*\)}"$/\1/p' "$UPDATE_ISSUE")
[[ -n "$issue_file" ]] || fail "update-issue has no default OUTPUT_FILE"
filecon_paths=$(sed -n 's/^[[:blank:]]*(filecon "\([^"]*\)".*/\1/p' "$SELINUX_CIL")
printf '%s\n' "$filecon_paths" | grep -Fqx "$auth_binary" || fail "linuxio.cil gives no file context to ${auth_binary}"
# The memory page's swap file and the file a resize turns on beside it, as
# file-context regular expressions (docs/process-systemd-architecture.md,
# "SELinux").
swap_file_contexts=$(printf '%s\n' '/var/lib/linuxio/swap/swapfile' '/var/lib/linuxio/swap/swapfile\.linuxio-resize')
while IFS= read -r path; do
	case "$path" in
	"$auth_binary" | "$issue_file") ;;
	*) printf '%s\n' "$swap_file_contexts" | grep -Fqx -- "$path" ||
		fail "linuxio.cil gives a file context to ${path}, which LinuxIO does not install" ;;
	esac
done <<<"$filecon_paths"
# The swap files' contexts sit alone in the optional block linuxio_swapfile,
# with swapfile_t, the type mkswap gives them, and nothing else is in it.
swap_block=$(sed -e 's/;.*//' "$SELINUX_CIL" | awk '
	/^\(optional linuxio_swapfile$/ { inside = 1; next }
	inside && /^\)$/ { exit }
	inside && NF { line = $0; sub(/^[[:blank:]]+/, "", line); print line }')
expected_swap_block=$(while IFS= read -r path; do
	printf '(filecon "%s" file (system_u object_r swapfile_t ((s0) (s0))))\n' "$path"
done <<<"$swap_file_contexts")
if ! swap_diff=$(diff <(printf '%s\n' "$expected_swap_block") <(printf '%s\n' "$swap_block")); then
	fail "linuxio.cil's optional block linuxio_swapfile changed (expected <, linuxio.cil >):
${swap_diff}"
fi
# A file made at run time has its folder's type until something relabels it.
if printf '%s\n' "$filecon_paths" | grep -Fqx "$issue_file"; then
	grep -Fq 'restorecon "${OUTPUT_FILE}"' "$UPDATE_ISSUE" ||
		fail "update-issue does not relabel ${issue_file}, which linuxio.cil gives a type"
	[[ "$(grep -B1 -F "\"${issue_file}\"" "$SELINUX_CIL" | head -1)" == "(optional linuxio_banner" ]] ||
		fail "the banner's file context is not the first line of the optional block linuxio_banner"
fi
[[ "$(grep -c '^[[:blank:]]*(filecon ' "$SELINUX_CIL")" == "$(grep -c '(filecon ' "$SELINUX_CIL")" ]] ||
	fail "a filecon line of linuxio.cil does not start its line"
cil_depth=$(sed -e 's/;.*//' -e 's/"[^"]*"//g' "$SELINUX_CIL" | awk '
	{ for (i = 1; i <= length($0); i++) { c = substr($0, i, 1); if (c == "(") d++; else if (c == ")") d--; if (d < 0) { print "negative at line " NR; exit } } }
	END { print d }')
[[ "$cil_depth" == 0 ]] || fail "parenthesis depth at the end of linuxio.cil: ${cil_depth}"
# Every allow rule, with the optional block it sits in ("-" for none). The
# module gives a domain that is not LinuxIO's these two permissions only, each
# in its own optional block, so the console reads the banner and a policy
# without one of the types drops only that block
# (docs/process-systemd-architecture.md, "SELinux").
cil_allows=$(sed -e 's/;.*//' "$SELINUX_CIL" | awk '
	/^\(optional / { block = $2 }
	/^[[:blank:]]*\(allow/ { line = $0; sub(/^[[:blank:]]+/, "", line); sub(/[[:blank:]]+$/, "", line); print (block == "" ? "-" : block) "\t" line }
	{ for (i = 1; i <= length($0); i++) { c = substr($0, i, 1); if (c == "(") d++; else if (c == ")") d-- } if (d == 0) block = "" }')
other_domain_allows=$(printf '%s\n' "$cil_allows" | grep -v $'^[^\t]*\t(allow linuxio_auth_t ' || true)
expected_other_domain_allows=$(printf '%s\t%s\n' \
	linuxio_banner '(allow getty_t motd_var_run_t (file (getattr open read)))' \
	linuxio_banner_kmscon '(allow kmscon_t motd_var_run_t (file (getattr open read)))')
if ! allow_diff=$(diff <(printf '%s\n' "$expected_other_domain_allows") <(printf '%s\n' "$other_domain_allows")); then
	fail "linuxio.cil's permissions for domains that are not LinuxIO's changed (expected <, linuxio.cil >):
${allow_diff}"
fi
[[ "$(sed -e 's/;.*//' "$SELINUX_CIL" | grep -c -w -E 'getty_t|kmscon_t')" == 2 ]] ||
	fail "linuxio.cil names getty_t or kmscon_t outside the banner's two allow rules"
pass "SELinux policy names only LinuxIO's files, gives other domains only the banner's reads and balances"

printf '\033[1;32m%s\033[0m\n' "✅ package file fixtures passed!"
