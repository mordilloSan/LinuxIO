# The RPM of LinuxIO, assembled by packaging/scripts/package-rpm.sh from
# programs already built: nothing is compiled here. The script defines
# linuxio_version, linuxio_build_dir (the programs and their
# THIRD_PARTY_NOTICES.md), linuxio_packaging_dir (packaging/),
# linuxio_source_dir (the folder holding LICENSE) and linuxio_changelog (the
# %%changelog text changelog-entry.sh wrote).
#
# The programs go in byte for byte as built: the webserver checks the SHA-256
# of linuxio-bridge recorded at build time. So no debuginfo package (whose
# extraction strips), no build-id links, and none of the brp scripts, which
# strip, rewrite shebangs and normalise files.
%global debug_package %{nil}
%global _build_id_links none
%global __os_install_post %{nil}
# The build time is the build's, not the date of the %%changelog entry, which
# has no time of day.
%global source_date_epoch_from_changelog 0

%global selinuxtype targeted
%global selinux_module %{_datadir}/selinux/packages/%{selinuxtype}/linuxio.cil
# The units enabled on a first install, and started.
%global enabled_units linuxio.target linuxio-indexer-index.timer linuxio-monitoring.service linuxio-jobs@0.service
%global btrfs_units linuxio-btrfs-health.timer linuxio-btrfs-degraded.service
%global started_units linuxio.target linuxio-monitoring.service linuxio-issue.service
# The units LinuxIO writes to /etc/systemd/system when a setting is saved.
%global written_units linuxio-btrfs-snapshot.timer linuxio-shares-recycle.timer linuxio-docker-update.timer linuxio-docker-shutdown.service

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
cp -p '%{linuxio_source_dir}/LICENSE' '%{linuxio_build_dir}/THIRD_PARTY_NOTICES.md' .

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
# The man page, after the file list is made: %%files names it. The brp
# scripts that would compress it are off (__os_install_post above), so it is
# gzipped here.
install -D -m 0644 '%{linuxio_packaging_dir}/man/linuxio.8' %{buildroot}%{_mandir}/man8/linuxio.8
gzip -9n %{buildroot}%{_mandir}/man8/linuxio.8

%pre
# Refuse to install over LinuxIO put in by the install script of v0.54 or
# earlier, which the package would fight over units and programs: it is
# removed first, with that release's uninstall script. Exact paths only, and
# nothing is removed here.
if [ "$1" -eq 1 ]; then
	for sentinel in /usr/local/bin/linuxio /etc/systemd/system/linuxio.target; do
		# A mask (a link to /dev/null) is an administrator's, not the script's.
		if [ "$(readlink "$sentinel" 2>/dev/null)" = /dev/null ]; then
			continue
		fi
		if [ -e "$sentinel" ] || [ -L "$sentinel" ]; then
			echo "linuxio: ${sentinel} is from a LinuxIO installed by its install script." >&2
			echo "linuxio: Remove that install first with the uninstall script of the release it came from, v0.54 or earlier" >&2
			echo "linuxio: (packaging/scripts/uninstall.sh in that release's source; it keeps /var/lib/linuxio), then install this package again." >&2
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
# Scheduled scripts: earlier releases wrote NoNewPrivileges=yes into each
# schedule's unit, which kept a script from sudo, setuid programs and a tool
# AppArmor confines. The line is deleted from the saved units, so they match
# what a save writes and Run now accepts them (docs/scheduled-scripts.md).
# The DEB's postinst does the same.
for unit in %{_sysconfdir}/systemd/system/linuxio-schedule-*.service; do
	if [ -f "$unit" ] && [ ! -L "$unit" ]; then
		sed -i '/^NoNewPrivileges=yes$/d' "$unit" || :
	fi
done
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
# The login banner: SSH logins (pam_motd) and the console (agetty, kmscon)
# show /run/linuxio/issue through these links; the module loaded above lets
# the console read it (docs/process-systemd-architecture.md, "SELinux").
# Made on the first install only, so one an administrator deleted stays
# deleted at an update, and never over a file of someone else's; %%postun
# deletes them while they are still LinuxIO's. The DEB's postinst does the
# same.
if [ "$1" -eq 1 ]; then
	for link in %{_sysconfdir}/motd.d/linuxio %{_sysconfdir}/issue.d/linuxio.issue; do
		if [ ! -e "$link" ] && [ ! -L "$link" ]; then
			mkdir -p "$(dirname "$link")"
			ln -s ../../run/linuxio/issue "$link"
		fi
	done
fi
if [ "$1" -eq 1 ] && [ -d /run/systemd/system ]; then
	systemctl start %{started_units} || :
	# libvirt finds a hook only when its daemon starts. An upgrade leaves
	# the daemon alone: the hook was there when it started, and a daemon
	# with work to finish takes up to 30 seconds to stop, answering nobody
	# meanwhile. The DEB's postinst does the same.
	systemctl try-restart libvirtd.service virtqemud.service >/dev/null 2>&1 || :
fi
# An upgrade restarts LinuxIO from the old package's %%postun, only if it runs.

%preun
# On removal (not on an upgrade): stop LinuxIO and every job host, and
# disable what the package enabled. /var/lib/linuxio is left as it is.
if [ "$1" -eq 0 ] && [ -d /run/systemd/system ]; then
	hosts=$(systemctl list-units --all --plain --no-legend --full 'linuxio-jobs@*' 2>/dev/null | grep -o 'linuxio-jobs@[^ ]*' || :)
	systemctl stop linuxio.target linuxio-webserver.socket linuxio-auth.socket linuxio-indexer.socket \
		linuxio-indexer-index.timer linuxio-btrfs-health.timer linuxio-issue.service $hosts || :
fi
# The units LinuxIO wrote: left running, a timer would start a service this
# removal takes away, and an enabled unit would leave its link behind (the
# Docker shutdown unit's under docker.service.wants; its stop does nothing
# outside a shutdown, so the containers keep running). %%postun deletes their
# files. One at a time: systemctl refuses the whole list when one of them was
# never written.
if [ "$1" -eq 0 ]; then
	for unit in %{written_units}; do
		if [ -d /run/systemd/system ]; then
			systemctl stop "$unit" >/dev/null 2>&1 || :
		fi
		systemctl --no-reload disable "$unit" >/dev/null 2>&1 || :
	done
fi
%systemd_preun %{enabled_units} %{btrfs_units}

%postun
# An upgrade restarts LinuxIO only if it runs, once the transaction is done;
# the job hosts (linuxio-jobs@*) are outside linuxio.target and run on, the
# update that started this among them (docs/jobs.md, Updates).
%systemd_postun_with_restart %{started_units}
# On removal, what LinuxIO itself wrote while it ran for its own settings
# (units and drop-ins under /etc/systemd/system, its APT and time server
# files), and the login banner's links while they are still LinuxIO's: rpm
# removes only the files it installed. Exact paths, kept equal to the DEB's
# list (packaging/debian/postrm); a test of each writer holds both lists to
# the paths it writes. What LinuxIO writes for something an administrator set
# up stays, since it works without LinuxIO: scheduled scripts
# (linuxio-schedule-*), the rsync service, Docker's drop-ins and the memory
# page's files. rpm has no purge, so /etc/linuxio and /var/lib/linuxio stay.
if [ "$1" -eq 0 ]; then
	written_files="
/etc/systemd/system/linuxio-btrfs-snapshot.timer
/etc/systemd/system/linuxio-shares-recycle.timer
/etc/systemd/system/linuxio-docker-update.timer
/etc/systemd/system/linuxio-docker-shutdown.service
/etc/systemd/system/linuxio-indexer-index.timer.d/linuxio.conf
/etc/systemd/system/linuxio-webserver.service.d/verbose.conf
/etc/systemd/system/linuxio-indexer.service.d/verbose.conf
/etc/systemd/system/linuxio-monitoring.service.d/verbose.conf
/etc/systemd/system/apt-daily.service.d/linuxio-alert.conf
/etc/systemd/system/apt-daily-upgrade.service.d/linuxio-alert.conf
/etc/systemd/system/dnf-automatic.service.d/linuxio-alert.conf
/etc/systemd/system/dnf5-automatic.service.d/linuxio-alert.conf
/etc/systemd/system/mintupdate-automation-upgrade.service.d/linuxio-alert.conf
/etc/systemd/system/apt-daily.timer.d/linuxio.conf
/etc/systemd/system/apt-daily-upgrade.timer.d/linuxio.conf
/etc/systemd/system/dnf-automatic.timer.d/linuxio.conf
/etc/systemd/system/dnf5-automatic.timer.d/linuxio.conf
/etc/systemd/system/mintupdate-automation-upgrade.timer.d/linuxio.conf
/etc/apt/apt.conf.d/52linuxio-periodic
/etc/apt/apt.conf.d/52linuxio-unattended-upgrades
/etc/systemd/timesyncd.conf.d/linuxio.conf
/etc/chrony/sources.d/linuxio.sources
"
	for path in $written_files; do
		if [ -e "$path" ] || [ -L "$path" ]; then
			rm -f "$path"
			# A drop-in folder under /etc/systemd/system too, while nothing
			# else is in it; the folders elsewhere are their packages'.
			case "$(dirname "$path")" in
			/etc/systemd/system/*.d) rmdir "$(dirname "$path")" 2>/dev/null || : ;;
			esac
		fi
	done
	for link in %{_sysconfdir}/motd.d/linuxio %{_sysconfdir}/issue.d/linuxio.issue; do
		if [ "$(readlink "$link" 2>/dev/null)" = ../../run/linuxio/issue ]; then
			rm -f "$link"
		fi
	done
	if [ -d /run/systemd/system ]; then
		systemctl daemon-reload || :
	fi
fi
# The module goes only when the package does, after the files: removed while
# linuxio-auth is still there, it would leave the file unlabeled_t.
%selinux_modules_uninstall -s %{selinuxtype} linuxio

%files -f %{_builddir}/linuxio.files
%license LICENSE
%doc THIRD_PARTY_NOTICES.md
%{_mandir}/man8/linuxio.8*

# Read with %%(cat), whose output rpm takes as it is: an %%include'd line would
# have its macros expanded (docs/packaging.md, "Changelog").
%changelog
%(cat %{linuxio_changelog})
