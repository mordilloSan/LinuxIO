# The RPM of LinuxIO, assembled by packaging/scripts/package-rpm.sh from
# programs already built: nothing is compiled here. The script defines
# linuxio_version, linuxio_build_dir (the programs), linuxio_packaging_dir
# (packaging/) and linuxio_source_dir (the folder holding LICENSE and
# docs/THIRD_PARTY_NOTICES.md).
#
# The programs go in byte for byte as built: the webserver checks the SHA-256
# of linuxio-bridge recorded at build time. So no debuginfo package (whose
# extraction strips), no build-id links, and none of the brp scripts, which
# strip, rewrite shebangs and normalise files.
%global debug_package %{nil}
%global _build_id_links none
%global __os_install_post %{nil}
# There is no %%changelog to take a build time from.
%global source_date_epoch_from_changelog 0

%global selinuxtype targeted
%global selinux_module %{_datadir}/selinux/packages/%{selinuxtype}/linuxio.cil
# The units enabled on a first install, and started.
%global enabled_units linuxio.target linuxio-indexer-index.timer linuxio-monitoring.service linuxio-jobs@0.service
%global btrfs_units linuxio-btrfs-health.timer linuxio-btrfs-degraded.service
%global started_units linuxio.target linuxio-monitoring.service linuxio-issue.service

Name:           linuxio
Version:        %{linuxio_version}
Release:        1
Summary:        Web dashboard to administer a Linux server
License:        Apache-2.0
URL:            https://github.com/mordilloSan/LinuxIO
ExclusiveArch:  x86_64

Requires:       pam
Requires:       polkit
Requires:       PackageKit
Requires:       systemd
%{?systemd_requires}
# semodule, restorecon and a policy for the module, at the floor Fedora's
# macros set rather than the version of the machine that assembled this.
%selinux_requires_min

%description
LinuxIO manages a Linux host from the browser: storage and btrfs pools,
Docker, virtual machines, networking, users, shares, updates, sensors and
files, behind one PAM sign-in. It listens on port 8090
(linuxio-webserver.socket).

%prep
cp -p '%{linuxio_source_dir}/LICENSE' '%{linuxio_source_dir}/docs/THIRD_PARTY_NOTICES.md' .

%build

# The tree both packages share, then what only the RHEL family gets. The file
# list is made from the tree, so package-root.sh stays the one list of files:
# everything under /etc is %%config(noreplace), the programs package-root.sh
# made 0700 keep it, and LinuxIO's own folders (a linuxio folder, what is in
# one, and the drop-in folders) are owned.
%install
'%{linuxio_packaging_dir}/scripts/package-root.sh' --build-dir '%{linuxio_build_dir}' --destdir '%{buildroot}'
install -D -m 0644 '%{linuxio_packaging_dir}/etc/pam.d/linuxio.rhel' %{buildroot}%{_sysconfdir}/pam.d/linuxio
install -D -m 0644 '%{linuxio_packaging_dir}/selinux/linuxio.cil' %{buildroot}%{selinux_module}
# The SSH banner: pam_motd reads /etc/motd.d.
install -d -m 0755 %{buildroot}%{_sysconfdir}/motd.d
ln -s ../../run/linuxio/issue %{buildroot}%{_sysconfdir}/motd.d/linuxio
(
	cd %{buildroot}
	find . -mindepth 1 -type d \( -name linuxio -o -path '*/linuxio/*' -o -name '*.service.d' \) -printf '%%%%dir /%%P\n'
	find . -mindepth 1 \( -type f -o -type l \) -printf '%%m /%%P\n' | while read -r mode path; do
		case $path in
		/etc/*) echo "%%config(noreplace) $path" ;;
		*) if [ "$mode" = 700 ]; then echo "%%attr(0700,root,root) $path"; else echo "$path"; fi ;;
		esac
	done
) >%{_builddir}/linuxio.files

%pre
# Refuse to install over LinuxIO put in by the script installer, which the
# package would fight over units and programs: it is removed first. Exact
# paths only, and nothing is removed here.
if [ "$1" -eq 1 ]; then
	for sentinel in /usr/local/bin/linuxio /etc/systemd/system/linuxio.target; do
		# A mask (a link to /dev/null) is an administrator's, not the script's.
		if [ "$(readlink "$sentinel" 2>/dev/null)" = /dev/null ]; then
			continue
		fi
		if [ -e "$sentinel" ] || [ -L "$sentinel" ]; then
			echo "linuxio: ${sentinel} is from a LinuxIO installed by its install script." >&2
			echo "linuxio: Remove that install first with the uninstall script of the release it came from" >&2
			echo "linuxio: (packaging/scripts/uninstall.sh in its source; it keeps /var/lib/linuxio), then install this package again." >&2
			exit 1
		fi
	done
fi

%post
# SELinux before anything starts: linuxio-auth needs its domain, and systemd
# creates the auth socket in the domain of the program it runs
# (docs/process-systemd-architecture.md, "SELinux"). On a first install the
# file was written before the module named its type, so it is relabelled.
# Neither stops the install: where SELinux is disabled the store is still
# written, and the relabel that enabling SELinux asks for labels the file.
%selinux_modules_install -s %{selinuxtype} %{selinux_module}
if command -v restorecon >/dev/null 2>&1; then
	restorecon %{_libexecdir}/linuxio/linuxio-auth || :
fi
%tmpfiles_create linuxio.conf
if [ "$1" -eq 1 ]; then
	systemctl --no-reload enable %{enabled_units} >/dev/null 2>&1 || :
	# LinuxIO enables these itself for a pool made later.
	if lsblk -nro FSTYPE 2>/dev/null | grep -qx btrfs; then
		systemctl --no-reload enable %{btrfs_units} >/dev/null 2>&1 || :
	fi
fi
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || :
fi
if [ "$1" -eq 1 ] && [ -d /run/systemd/system ]; then
	systemctl start %{started_units} || :
fi
# An upgrade restarts LinuxIO from the old package's %%postun, only if it runs.
# libvirt finds its hooks only when its daemon starts.
if [ -d /run/systemd/system ]; then
	systemctl try-restart libvirtd.service virtqemud.service >/dev/null 2>&1 || :
fi

%preun
# On removal (not on an upgrade): stop LinuxIO and every job host, and
# disable what the package enabled. /var/lib/linuxio is left as it is.
if [ "$1" -eq 0 ] && [ -d /run/systemd/system ]; then
	hosts=$(systemctl list-units --all --plain --no-legend --full 'linuxio-jobs@*' 2>/dev/null | grep -o 'linuxio-jobs@[^ ]*' || :)
	systemctl stop linuxio.target linuxio-webserver.socket linuxio-auth.socket linuxio-indexer.socket \
		linuxio-indexer-index.timer linuxio-btrfs-health.timer linuxio-issue.service $hosts || :
fi
%systemd_preun %{enabled_units} %{btrfs_units}

%postun
# An upgrade restarts LinuxIO only if it runs, once the transaction is done;
# the job hosts (linuxio-jobs@*) are outside linuxio.target and run on, the
# update that started this among them (docs/jobs.md, Updates).
%systemd_postun_with_restart %{started_units}
# The module goes only when the package does, after the files: removed while
# linuxio-auth is still there, it would leave the file unlabeled_t.
%selinux_modules_uninstall -s %{selinuxtype} linuxio

%files -f %{_builddir}/linuxio.files
%license LICENSE
%doc THIRD_PARTY_NOTICES.md
