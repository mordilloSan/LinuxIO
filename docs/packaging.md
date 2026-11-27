# Packages, installation and updates

LinuxIO is installed, updated and removed as one native package named
`linuxio`: a DEB for the Debian family and an RPM for the RHEL family, of the
same version and with the same nine programs, byte for byte. The host's own
package manager does every step, from a signed repository; nothing downloads
or runs an install script. LinuxIO's own update is the package `linuxio`
among the host's other updates on the Updates page.

How a release builds, signs and publishes the packages and the repositories,
the pinned build images and the signing keys are in
[Releases](./releases.md). The processes and units the package installs are
in [Process & Systemd Architecture](./process-systemd-architecture.md).

## Installing

The packages are published in the project's package repository,
`https://packages.engmariz.com` (`PACKAGES_DOMAIN` in
`.github/workflows/release.yml`), with the source file each family's package
manager reads: `apt/linuxio.sources`, deb822 with the public key in
`Signed-By`, and `rpm/linuxio.repo`, with `gpgcheck=1` and `repo_gpgcheck=1`
and the key at `rpm/RPM-GPG-KEY-linuxio`. An administrator puts that file in
`/etc/apt/sources.list.d` or `/etc/yum.repos.d` and runs `apt install linuxio`
or `dnf install linuxio`; the commands are in the [README](../README.md).
From then on the distribution's own updates, a shell's `apt upgrade` or
`dnf upgrade`, and the Updates page all see new versions. A host saves the
repository's address, which is why it is a name under the owner's domain
rather than the hosting provider's.

The supported systems are Debian stable, Ubuntu LTS from 24.04, and the
current and previous Fedora releases, on amd64 (`x86_64`) only. The DEB
depends on `libnss-systemd`, `libpam0g`, `packagekit`, `polkitd |
policykit-1` and `systemd`, with the shared libraries `dh_shlibdeps` finds.
`libnss-systemd` puts systemd in `/etc/nsswitch.conf`, which resolves the
`linuxio-bridge-socket` group the auth socket is created with (a dynamic
user's, [Process & Systemd Architecture](./process-systemd-architecture.md)):
Ubuntu installs it by default and Debian does not, and without it
`linuxio-auth.socket` fails with `216/GROUP` and nobody can sign in (seen on
Debian 13 in the test VM). Fedora's systemd carries the module and its
`nsswitch.conf` names it; the RPM requires `pam`,
`polkit`, `PackageKit` and `systemd`, the libraries rpm finds, and
`selinux-policy` at the floor Fedora's `%selinux_requires_min` sets. Neither
recommends anything: Docker, libvirt and the other optional facilities are
installed later from the Capability Manager
([Capabilities](./capabilities.md)).

## What is installed where

| Path | What | Mode |
|---|---|---|
| `/usr/bin/linuxio` | The administrator's command (`linuxio(8)`) | 0755 |
| `/usr/libexec/linuxio/linuxio-webserver`, `linuxio-bridge`, `linuxio-claude-relay` | Programs that run as someone other than root | 0755 |
| `/usr/libexec/linuxio/linuxio-auth`, `linuxio-worker`, `linuxio-docker-update`, `linuxio-indexer`, `linuxio-monitoring` | Programs systemd runs as root | 0700 |
| `/usr/lib/systemd/system/linuxio*` | Every unit in `packaging/systemd` | 0644 |
| `/usr/lib/systemd/system/<unit>.d/10-linuxio-run.conf` | The run-record drop-in, for the generated schedule units, the snapshot, replication and Docker update services, and `apt-daily-upgrade`, `dnf-automatic`, `dnf5-automatic` and `mintupdate-automation-upgrade` | 0644 |
| `/usr/lib/tmpfiles.d/linuxio.conf` | `/var/lib/linuxio` and its folders, `/run/linuxio/jobs` | 0644 |
| `/usr/share/linuxio/issue/update-issue` | Writes the login banner | 0755 |
| `/usr/share/linuxio/libvirt/linuxio-bridge-netfilter` | The untouched copy of the libvirt hook that Restore copies back | 0644 |
| `/etc/linuxio/disallowed-users`, `indexer/config.yaml`, `monitoring/config.yaml` | Configuration | 0644 |
| `/etc/pam.d/linuxio` | The PAM stack, the family's own (`packaging/etc/pam.d/linuxio`, `linuxio.rhel`) | 0644 |
| `/etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter` | The libvirt hook | 0755 |
| `/etc/avahi/services/linuxio.service` | The Avahi announcement | 0644 |
| `/etc/needrestart/conf.d/linuxio.conf` | The needrestart override, DEB only | 0644 |
| `/usr/share/selinux/packages/targeted/linuxio.cil` | The SELinux module, RPM only | 0644 |
| `/usr/share/man/man8/linuxio.8.gz` | The man page | 0644 |
| `/usr/share/doc/linuxio/` | `copyright` and `THIRD_PARTY_NOTICES.md` (DEB); the RPM has `%license LICENSE` and `%doc THIRD_PARTY_NOTICES.md`. The notices are the build's ([Third-party notices](#third-party-notices)) | 0644 |

Everything is owned by root. Only `linuxio` is on `PATH`; the other programs
are started by their full path by systemd, `linuxio-auth` and each other
(`version.LibexecDir`). The five programs systemd runs as root are 0700 so no
other local user can run a word or flag meant for systemd alone; the other
three run as the signed-in user or a dynamic user and so stay 0755
([Who may start each program](./process-systemd-architecture.md#binaries)).
`dh_fixperms` makes every program 0755, so `packaging/debian/rules` sets the
five back to 0700 after it; the RPM's file list gives them
`%attr(0700,root,root)`.

Nothing is installed under `/usr/local`, and no unit file under
`/etc/systemd/system`: the packages put only the links of the units they
enable there. What LinuxIO itself writes there while it runs (timers, the
verbose drop-ins, the alert and schedule drop-ins on the distribution's
update units, an administrator's scheduled scripts) belongs to the setting
that wrote it ([Removal](#removal)).

Neither package changes a byte of a program: the webserver checks the SHA-256
of `linuxio-bridge` recorded at build time before it starts one
([Privilege Boundaries](./process-systemd-architecture.md#privilege-boundaries-summary)).
So the DEB leaves out `dh_strip` and `dh_dwz`, and the RPM has no debuginfo
package, no build-ID links and no `__os_install_post` (which would strip,
rewrite shebangs and normalise files).

## Configuration files

Every file the package installs under `/etc` is a configuration file: a
conffile in the DEB, `%config(noreplace)` in the RPM. An upgrade replaces one
nobody edited. An edited one is kept: dpkg asks in a terminal when the new
package changed the file too, and keeps the edited copy without asking where
it cannot ask; rpm keeps it and writes the package's copy beside it as
`.rpmnew`. An edited file therefore does not get the fixes of that upgrade,
which matters most for the libvirt hook ([below](#the-libvirt-hook)).

On removal the two families differ. `apt remove` leaves every configuration
file in place, and `apt purge` removes them. An RPM erase removes the ones
nobody edited and keeps an edited one as `.rpmsave`. Files LinuxIO writes
under `/etc/linuxio` at run time (snapshot rules, scheduled scripts, the
rsync module's configuration and the like) are not package files, and stay
on a removal with `/var/lib/linuxio`, LinuxIO's data. A DEB purge deletes
both folders, all but what a setup that outlives LinuxIO reads there, the
memory page's swap file among it; rpm has no purge, so after an erase both
stay ([Removal](#removal)).

## First install

The DEB's `postinst` and the RPM's `%post`, on a first install:

1. (RPM) Load the SELinux module and relabel `linuxio-auth`
   ([below](#the-selinux-module)), before anything starts.
2. Create the folders of `tmpfiles.d/linuxio.conf` (debhelper's
   `dh_installtmpfiles` part of the DEB's script, `%tmpfiles_create` in the
   RPM).
3. Enable `linuxio.target`, `linuxio-indexer-index.timer`,
   `linuxio-monitoring.service` and `linuxio-jobs@0.service`, the root job
   host, which starts at boot to mark, alert and resume what a reboot
   interrupted. Where a btrfs filesystem exists (`lsblk` lists one), also
   `linuxio-btrfs-health.timer` and `linuxio-btrfs-degraded.service`; LinuxIO
   enables them itself for a pool made later ([Btrfs](./btrfs.md)).
4. Make the login banner's links ([below](#the-login-banner)).
5. Reload systemd and start `linuxio.target` and monitoring; the banner
   service starts with the webserver socket. The root job host is enabled
   and not started; it starts at boot or at the first privileged sign-in.
6. Restart libvirt's daemon if it runs, so it finds the hook.

The DEB enables through `deb-systemd-helper`, so a unit an administrator
disabled stays disabled at an upgrade, and starts through
`deb-systemd-invoke`, so `policy-rc.d` and chroots are respected. It cannot
read a template's instance, so `systemctl` enables `linuxio-jobs@0.service`
on a first install only. For the DEB a first install is one with no
`deb-systemd-helper` record of `linuxio.target`, which includes an install
after `apt remove`. debhelper's own unit handling (`dh_installsystemd`) is
left out: it would stop or restart every unit it finds, the job hosts among
them.

The PAM file is the family's own: the RHEL family has `password-auth` and
`postlogin` where the Debian family has the `common-*` files, and
`/etc/locale.conf` for `/etc/default/locale`: `common-auth` is
`substack password-auth` and `include postlogin`, `common-account` and
`common-password` are `password-auth`, and `common-session-noninteractive` is
`password-auth` and `postlogin`. Otherwise the two are the same line for line
and in the same order (`package-files-fixtures.sh` holds them to it);
order matters on the RHEL family, where a `sufficient` line in
`password-auth` ends the stack where it is included.

## Upgrade

An upgrade reloads systemd and restarts `linuxio.target`, monitoring and the
banner, only where they run: a stopped LinuxIO stays stopped. The DEB does it
with `deb-systemd-invoke try-restart` when its own package is configured,
which in a larger upgrade is before the whole transaction ends; the RPM with
`%systemd_postun_with_restart`, which has systemd restart the units when the
dnf transaction ends. A unit an administrator disabled stays disabled.

`linuxio-auth@.service` is `PartOf=linuxio.target`, so the restart ends every
session and each user signs in again. The job hosts are outside the target
and are never stopped or restarted by the package: running jobs go on, among
them the `packages.update` job that installs the upgrade, and an old host
finishes its jobs before the new build takes over
([Jobs](./jobs.md#updates)). An upgrade from a shell, the distribution's
unattended upgrade and the Updates page end the same way, and no
`linuxio restart` is needed. `package-repository.spec.ts` runs the upgrade
with a job running both from a shell and through the distribution's own
unattended service (`apt-daily-upgrade.service` running unattended-upgrades
allowed the LinuxIO origin, or `dnf5-automatic.service` with
`apply_updates`). The whole file passed on Debian 13 and Fedora 44, and its
unattended test on Ubuntu 26.04 and Fedora 45 too (2026-11-24, test VMs);
the DEB's removal, purge and install again and the RPM's erase and install
again were checked by hand on Debian 13 and Fedora 44.

An upgrade does not restart libvirt's daemon
([The libvirt hook](#the-libvirt-hook)).

A command line a unit runs is in a package unit, so it moves with the
package. Two kinds of file LinuxIO writes on save still name a LinuxIO
program, because a setting decides what they hold: the verbose drop-ins
(`backend/common/verbosemode`) and the alert drop-ins on the distribution's
update services ([Alerts](./alerts.md)). A release that changes a command line
LinuxIO writes on save rewrites the saved copies in the package's
post-install step. The one such rewrite so far is in the scheduled scripts'
units: the post-install step deletes the `NoNewPrivileges=yes` line earlier
releases wrote into `/etc/systemd/system/linuxio-schedule-*.service`
([Scheduled scripts](./scheduled-scripts.md#the-units)). The snapshot, replication
and Docker update services are
package units, and a save writes only their timers, which hold a schedule and
no command line.

## Removal

A package manager removes only the files it installed. On removal (not on an
upgrade) the DEB's `prerm` and the RPM's `%preun` stop `linuxio.target`, its
sockets and timers, monitoring, the banner and every job host and job socket,
and disable what the install enabled. They also stop and disable the units
LinuxIO wrote for its own settings: the snapshot and Docker update timers, and
the Docker shutdown unit, whose stop leaves the containers running and whose
disable takes its link out of `docker.service.wants`
([Docker Shutdown](./docker-shutdown.md#removal)).

The DEB's `postrm` and the RPM's `%postun` then delete the files LinuxIO
itself wrote for its own settings while it ran, by exact path, the two lists
kept equal:

- the three timers, `linuxio-btrfs-snapshot.timer`,
  `linuxio-shares-recycle.timer` and `linuxio-docker-update.timer`: left
  behind, each would start a service the removal took away;
- the Docker shutdown unit, `linuxio-docker-shutdown.service`;
- the verbose drop-ins of the webserver, the indexer and monitoring
  (`backend/common/verbosemode`);
- the indexer timer's schedule, `linuxio-indexer-index.timer.d/linuxio.conf`;
- the alert drop-ins `linuxio-alert.conf` on the distribution's update
  services: left behind, their `ExecStopPost=` names a program that is gone
  and the nightly upgrade is marked failed;
- the schedule drop-ins `linuxio.conf` on the distribution's update timers,
  so the distribution's own schedule returns;
- LinuxIO's own APT files, `/etc/apt/apt.conf.d/52linuxio-periodic` and
  `52linuxio-unattended-upgrades`, so the distribution's automatic-update
  settings return with its schedule, on Ubuntu unattended security upgrades
  even where LinuxIO had them off
  ([Automatic updates](./automatic-updates.md#configuration-ownership));
- the time servers' file, `/etc/systemd/timesyncd.conf.d/linuxio.conf`, or
  chrony's `/etc/chrony/sources.d/linuxio.sources` on Debian and Ubuntu, so
  the distribution's servers replace the ones set in LinuxIO when the service
  next starts; until then, or a reboot, it keeps using LinuxIO's.

A drop-in folder under `/etc/systemd/system` goes too when nothing else is in
it. `apt.conf.d`, `timesyncd.conf.d` and `sources.d` stay, even empty: a
package may own them, as apt does `apt.conf.d`. A test of each writer holds
both lists to the paths it writes (`TestPackageRemovalDeletes*`). What
LinuxIO wrote inside another package's own file stays: the time servers in a
`chrony.conf` that names no folder under `/etc` (Fedora's), and the keys of
dnf's `automatic.conf` and of Mint's update files. The banner's links go
while they are still LinuxIO's, and systemd reloads. On the
RPM the SELinux module goes last, after the files: removed while
`linuxio-auth` is still there, it would leave the file `unlabeled_t`.

What LinuxIO writes for something an administrator set up stays, since it
runs without LinuxIO and removing it would stop the administrator's own work
without a word: the scheduled scripts' units (`linuxio-schedule-<id>.service`
and `.timer`, which run `bash` on a script under `/etc/linuxio/schedules` as
their user; the run-record drop-in that hooks them goes with the package), the
rsync service (`linuxio-rsync.service`, `rsync --daemon` on
`/etc/linuxio/rsyncd.conf`) and Docker's drop-ins (`docker.service.d/linuxio-*.conf`:
the mounts its data needs and the addresses it waits for). A new install finds
them with their definitions under `/etc/linuxio`. The WireGuard interfaces
stay too, being wg-quick's own files, and with them
`/etc/sysctl.d/90-linuxio-wireguard.conf`, which keeps IPv4 forwarding on at
boot for those with NAT ([Forwarding](./wireguard.md#forwarding)). So do the
memory page's files, `/etc/sysctl.d/90-linuxio-memory.conf`,
`/etc/tmpfiles.d/linuxio-zswap.conf` and the zram configuration
(`/etc/systemd/zram-generator.conf` or `/etc/default/zramswap`, which are
zram's own files): with the swap file below they are one setup of the host,
and deleting part of it would leave a mix the memory page never makes, such
as zswap back in front of zram at the next boot. The memory page writes no
unit or drop-in under `/etc/systemd/system`; a hold-off drop-in on a swap unit
(`90-linuxio-memory.conf`) that an older LinuxIO left stays as well
([Upgrading](./memory.md#upgrading-from-an-older-linuxio)). The settings whose files the
removal deleted (snapshot rules, replication, Docker updates, the Docker
shutdown unit, verbose logging, the indexer's interval, the automatic-update
options on APT, the schedule and its alerts, the time servers) are switched
on again by hand after a new install.

A DEB purge (`apt purge`) also deletes `/etc/linuxio` and `/var/lib/linuxio`,
once dpkg has deleted the configuration files it installed: the settings and
rules, the TLS certificate, the job records, the file index, the metrics
history, the replication keys and the rest. It keeps what a setup that stays
reads there, the postrm's `admin_state` list: `/etc/linuxio/schedules` (the
scheduled scripts' units run the scripts in it), `/etc/linuxio/rsyncd.conf`
and `rsyncd.secrets` (the rsync service), `/var/lib/linuxio/cifs-credentials`
(named by the CIFS lines in `/etc/fstab`) and `/var/lib/linuxio/swap`. A
test of each writer holds the list to its path (`TestPackagePurgeKeeps*`). A
kept folder goes when it is empty, and so does each of the two folders once
nothing kept is left in it. A top-level entry that is another filesystem (a
btrfs subvolume, a mount) or holds a mount, a bind mount of the same
filesystem included (`/proc/self/mountinfo`), is named and left whole, and
so is a path the purge cannot delete, so the purge still completes. rpm has
no purge: after `dnf remove` both folders stay, and an administrator who
wants them gone deletes them by hand, but for the same paths.

The swap file the memory page makes, `/var/lib/linuxio/swap/swapfile`, and
its `/etc/fstab` line stay too, on a removal and on a purge: systemd turns it
on at boot from the line, without LinuxIO, so the host keeps its swap.
Neither package script touches either.
An administrator who wants them gone turns the file off, removes the line
and deletes the folder, in that order (the kernel refuses to delete a swap
file that is on, and a line left without its file fails its swap unit at
every boot):

```sh
sudo swapoff /var/lib/linuxio/swap/swapfile
sudo sed -i '\#^/var/lib/linuxio/swap/swapfile[[:space:]]#d' /etc/fstab
sudo systemctl daemon-reload
sudo rm -r /var/lib/linuxio/swap
```

## The SELinux module

The RPM carries `packaging/selinux/linuxio.cil` as
`/usr/share/selinux/packages/targeted/linuxio.cil`. It gives `linuxio-auth` a
domain of its own, without which nobody can sign in where SELinux enforces;
what the module allows and why is in
[SELinux](./process-systemd-architecture.md#selinux).

`%post` loads it with Fedora's `%selinux_modules_install` (`semodule -n -X 200
-i`, then `load_policy` where SELinux is on; neither fails the transaction),
then runs `restorecon` on `/usr/libexec/linuxio/linuxio-auth`, both before any
unit is enabled or started. On a first install rpm writes `linuxio-auth`
before the module names its type, so the relabel is what gives it
`linuxio_auth_exec_t`; at an upgrade rpm writes it with the type already. An
upgrade loads the new package's module the same way, because `dnf` runs a
package's scripts with no sandbox: a changed module reaches the host with the
update. Where SELinux is disabled the store is still written, and the relabel
that enabling SELinux asks for labels the file. `%postun` removes the module
with `%selinux_modules_uninstall` on an erase only, after the files. The DEB
carries no module: `unconfined_service_t` is a name of Fedora's policy.

## The login banner

`linuxio-issue.service` writes the addresses LinuxIO answers on into
`/run/linuxio/issue` ([Login banner](./process-systemd-architecture.md#login-banner)).
Both packages show it through two links to `../../run/linuxio/issue`:
`/etc/motd.d/linuxio`, which `pam_motd` prints at an SSH login, and
`/etc/issue.d/linuxio.issue`, which the console prints before its login
prompt (`agetty`; on Fedora's screen console `kmscon`, which reads
`/etc/issue.d` itself). Ubuntu's `sshd` and `login` PAM files read
`/etc/motd.d` (`pam_motd.so noupdate`), so no `update-motd.d` script is
needed. On the RHEL family the SELinux module gives the file the type
`sshd` reads, `motd_var_run_t`, and lets `agetty` and `kmscon` read that type
([SELinux](./process-systemd-architecture.md#selinux)).

The post-install step makes the links on a first install only, never over a
file that is there, so a link an administrator deleted stays deleted at an
update. Neither package owns them: removal deletes each while it is still
LinuxIO's link. An RPM installed before the console link existed does not
get it at an upgrade; `sudo ln -s ../../run/linuxio/issue
/etc/issue.d/linuxio.issue` makes it.

## The libvirt hook

Docker's `FORWARD DROP` policy drops a bridged VM's traffic; the hook
`/etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter` lets it through
([VM Bridge Networking](./vm-bridge-networking.md)). libvirt reads hooks
only from `/etc`, so it is installed there in every install, mode 0755, as a
configuration file of both packages, whether or not libvirt is installed.
libvirt uses a hook only when the file was there as its daemon started, so the
post-install step of a first install runs `try-restart` on `libvirtd.service`
and `virtqemud.service`. An upgrade does not: the hook was there when the
daemon started, libvirt runs the file anew for each event, so a changed hook
needs no restart, and a daemon with work to finish takes up to 30 seconds to
stop. It answers nobody in that time, the sign-in that follows the upgrade
among them, and every program using libvirt loses its connection
(`package-files-fixtures.sh` holds both scripts to it).

Its first line exits when `/usr/bin/linuxio` is missing: `apt remove` leaves
the file until a purge, and with the line removal stops the hook on both
families. A second, untouched copy under `/usr/share/linuxio/libvirt` is what
the Restore button in the hook's warning copies back, since a Debian-family
reinstall does not bring back a configuration file an administrator deleted.

## The Avahi announcement and the port

`linuxio-webserver.socket` listens on port 8090 on every address. The package
does not rewrite it; a different port is a socket drop-in:

```sh
sudo systemctl edit linuxio-webserver.socket
#   [Socket]
#   ListenStream=
#   ListenStream=9443
sudo linuxio restart --full
```

The login banner reads the port from systemd, so it shows the new one.
`/etc/avahi/services/linuxio.service`, which announces LinuxIO as
`_https._tcp` on the local network, names 8090, so after a port change its
`<port>` is edited by hand too (`systemctl reload avahi-daemon` reads it
again). The file is installed whether or not Avahi is: Avahi is a capability
added later, and it reads the file whenever it starts.

## The needrestart override

On Ubuntu, needrestart, which apt runs after every dpkg run, restarts each
service still running a replaced binary, and so would restart the job hosts
and interrupt the jobs an update waits for (seen once in the test VM, during
an unattended-upgrades run). The DEB therefore carries
`packaging/etc/needrestart/conf.d/linuxio.conf` as
`/etc/needrestart/conf.d/linuxio.conf`, whether or not needrestart is
installed; needrestart reads it when it is. The RPM has none: Fedora has no
needrestart.

needrestart evaluates the file as Perl after its own settings. Each line adds
one regular expression with the value 0 ("do not restart") to
`$nrconf{override_rc}` and keeps the distribution's entries:
`linuxio-jobs@<uid>.service`, the timer-run units `linuxio-schedule-*`,
`linuxio-btrfs-snapshot`, `linuxio-btrfs-replicate`, `linuxio-btrfs-resync@*`,
`linuxio-btrfs-rebuild@*` and `linuxio-docker-update`. needrestart then lists
them under "Service restarts being deferred". The daemons the package
restarts itself (`linuxio-webserver`, `linuxio-monitoring`,
`linuxio-indexer`, `linuxio-auth`) are not listed, and needrestart restarts
them as before. `package-files-fixtures.sh` loads the file under `strict` as
needrestart does and checks that it exempts exactly those units.

In the test VM, against needrestart 3.11-1ubuntu2 on Ubuntu 26.04
(`frontend/src/test/vm/jobs-needrestart.spec.ts`): in list mode needrestart
put `linuxio-jobs@0.service` under "Service restarts being deferred" and
`linuxio-monitoring.service` under "Services to be restarted"; in automatic
mode it deferred the job host, whose `MainPID` stayed, and restarted
`linuxio-monitoring.service`, which got a new one.

## Refusing a script install

LinuxIO v0.54 and earlier were installed by a script, with the programs in
`/usr/local/bin` and the units in `/etc/systemd/system`, where they would
fight the package's. The DEB's `preinst` (on every install, also one after
`apt remove`) and the RPM's `%pre` (on a first install) refuse to install
while `/usr/local/bin/linuxio` or `/etc/systemd/system/linuxio.target` is
there, a mask (a link to `/dev/null`) excepted, and say to remove that
install first with the uninstall script of the release it came from
(`packaging/scripts/uninstall.sh` in that release's source), which keeps
`/var/lib/linuxio`. They check exact paths only and remove nothing. There is
no migration: the script install's settings are made again after the
package is installed.

## LinuxIO's own update

The bridge's `control.version` reports the version PackageKit gives for a
pending update of the package `linuxio`, and a sudo session sees a one-line
banner that names it and opens the Updates page. The administrator installs
it there like any other package (`packages.update`, a job in the root job
host); PackageKit runs apt or dnf, which check the repository's and the
package's signatures, and the package restarts LinuxIO as above. The bridge
restarts nothing itself, and a transaction that fails or is cancelled before
the package manager changes the package leaves the old build installed and
running. The details, among them when a cancel does not stop the update, and
what the browser shows across the restart, are in [Jobs](./jobs.md#updates).

On Fedora a repository whose metadata cannot be fetched, or whose signature
fails, is skipped: the distribution sets `skip_if_unavailable` for every
repository that is not its own, so dnf says why and goes on, exiting 0, and
PackageKit's refresh succeeds and says nothing. Nothing from such a
repository is installed, and no LinuxIO update is listed. `linuxio.repo`
leaves the setting alone: turned off there, it fails every dnf transaction
on the host, an unattended upgrade among them, for as long as the repository
is unreachable.

So that the Updates page is not silent, Refresh Sources
(`updates.refresh_cache`) on a host that has `/etc/yum.repos.d/linuxio.repo`
ends by asking dnf about that one repository, with the setting off for that
one command (`checkOwnRepository` in `backend/bridge/handlers/packages`):

```sh
dnf --repo=linuxio --setopt=linuxio.skip_if_unavailable=False --refresh --assumeyes makecache
```

When dnf fails, its last line is the route's `own_repository_error`, and
where dnf has not ended within a minute, that it did not answer is (it waits
two minutes on a server that accepts no connection). The page shows the
reason in a warning while it stays open and until the next refresh starts,
and the refresh's toast warns where it would report success. A dnf that
cannot be started or is killed is logged and says nothing of the repository.
`--assumeyes` accepts the repository's key, the one `linuxio.repo` names,
where dnf has not held it yet: PackageKit keeps its own, so on a host whose
LinuxIO dnf never installed the check would otherwise report a repository
that PackageKit reads. PackageKit's apt backend fails the refresh itself on
an index it cannot fetch or verify, so an apt host has no such check.

## How the packages are built

`packaging/scripts/package-root.sh` (`make package-root DESTDIR=<empty
folder>`) lays the built programs and the files under `packaging/` out as the
tree both packages install: the programs at their modes, the units and the
run-record drop-ins, the tmpfiles file, the banner script, the hook and its
copy, and the configuration under `etc`. It builds nothing, runs without
root, sets modes and not owners, and refuses a folder that is not empty. What
differs between the families is added by each package: the PAM file, the
SELinux module, the needrestart override, the licences and the man page; the
banner's links are made by the post-install step. Its lists of programs,
modes and drop-in folders are the only ones; `package-root-fixtures.sh` holds
the tree to a written manifest, path by path and mode by mode, and its
programs to `make print-binaries`.

`make package-deb` and `make package-rpm` assemble the two packages from the
programs in `build/` into `build/packages/` (`PACKAGES_DIR`) and build
nothing themselves. `packaging/scripts/package-deb.sh` runs debhelper (compat
13) over `packaging/debian`; `packaging/scripts/package-rpm.sh` runs rpmbuild
over `packaging/rpm/linuxio.spec`. Each needs only `packaging/` and `LICENSE`
beside it and, in the build folder, the programs and their
`THIRD_PARTY_NOTICES.md`, so a test VM or a release image runs it from a copy
of those, and each fails with the name of the package to
install when its tool is missing. A release compiles once and puts the same
programs in both packages ([Releases](./releases.md)). The RPM has
`Release: 1` and no distribution tag, one RPM for every Fedora release.

Both carry one version, `PACKAGE_VERSION`
(`packaging/scripts/package-version.sh`): `X.Y.Z` on a commit tagged
`vX.Y.Z`, and `X.Y.Z+devN.gSHA` on a commit `N` after the last such tag,
which sorts after that release and before the next in dpkg and rpm.
`linuxio version` prints the build's version, which is the package's.

`packaging/scripts/tests/package-contents.sh PACKAGE BUILD_DIR` checks a DEB
or an RPM before it is installed: its files and links are exactly the tree
`package-root.sh` stages from the build, at the same modes, with what that
family adds, all owned by root; everything under `/etc` is a configuration
file; nothing is under `/usr/local`; the nine programs have the build's
SHA-256; the third-party notices are the build's; and the changelog opens
with an entry of the package's own version. `make test-packages` runs it on every package in `PACKAGES_DIR`.
`make test-packaging-scripts`, part of `make test`, runs the host-independent
fixtures: `package-root-fixtures.sh`, `package-version-fixtures.sh`,
`third-party-notices-fixtures.sh`, `changelog-entry-fixtures.sh`,
`repo-fixtures.sh` and `package-files-fixtures.sh`, which checks the files both packages ship (how
the units relate to `linuxio.target`, the run-record drop-in, the needrestart
override, the two PAM files, the SELinux policy's file contexts, the icon
sets the notices name).

`make localinstall` builds this checkout and installs it as a package, and
the test VMs assemble the package inside the VM, install it, and update it
from a signed repository of their own
([Development](./development.md#initial-setup),
[Test VM](./development.md#test-vm)). The indexer's smoke test installs and
removes a package under a real systemd ([Indexer](./indexer.md)).

## Changelog

Each package's changelog is its release's notes, the body `gh release-flow
open-pr` writes on the release PR ([Releases](./releases.md#release-notes)):
the release workflow passes them as `RELEASE_NOTES`, and
`packaging/scripts/changelog-entry.sh` writes them as plain text into
`debian/changelog` (installed as `/usr/share/doc/linuxio/changelog.gz`) and
into the spec's `%changelog`. Headings become `Heading:`, bold and links keep
their text, and every list item is one line. A build without notes, a local
one or a dry run, says `LinuxIO VERSION.` alone.

rpm expands the macros of a `%changelog` line that comes through `%include`
(`%{name}` in a note would become `linuxio`), so the spec reads the file with
`%(cat)`, whose output rpm does not expand. It still drops a line whose
first character other than a space is `#`, as a comment, starts a new entry
at a line beginning with `*`, and reads a line beginning with `%` as a
section or a conditional, so the RPM's text turns such a line into a list
item.

The Updates page shows the notes of an update before it is installed, which
neither package manager reads from the package itself:

- apt reads a changelog from where the repository's `Release` names it, in
  its `Changelogs` field; without the field it knows where only for Debian's
  and Ubuntu's own archives, and PackageKit's apt backend says "Changelog for
  this version is not yet available". `repo-apt.sh` writes the field
  (`apt-ftparchive` has no option for it) and publishes the changelog of
  every package in the pool, taken from the package, at
  `changelogs/main/l/linuxio/linuxio_<version>_changelog`, the path apt
  makes of `@CHANGEPATH@`. `apt changelog linuxio` reads it too, and the
  script refuses a DEB without a changelog before it reaches the pool.
- PackageKit's dnf backend sends no changelog at all, only the text of the
  update's advisory in the repository's `updateinfo` as the update text.
  `repo-rpm.sh` writes one advisory per RPM, its `%changelog` text as the
  description and its type from the notes' headings (`security` with a
  Security section, `bugfix` with Bug Fixes, `enhancement` otherwise, which
  `dnf upgrade --security` and `dnf updateinfo` go by), and adds it with
  `modifyrepo_c` before `repomd.xml` is signed. The bridge shows the update
  text where PackageKit sends no changelog (`buildUpdateDetail` in
  `backend/bridge/handlers/packages`).

This holds for every package on the Updates page, not only LinuxIO's
(`getSingleUpdateDetail` in `backend/bridge/handlers/packages`). The card
names the package, with PackageKit's summary of it below. Clicking the card
opens the update: the card, with its Update action, beside a panel headed by
the version, which is the PackageKit package ID's, with the changelog, and
the address keeps the open update (`/updates?package=<package ID>`). The
changelog is every entry PackageKit
sends, which the apt backend limits to the versions newer than the one
installed, so an update over several versions shows all of them. Where the
apt backend has no changelog it sends "Changelog for this version is not yet
available", which the bridge takes for none: the repository publishes none,
as Docker's, gh's and Ubuntu ESM's do not (apt knows where to look only for
Debian's and Ubuntu's archives), Ubuntu's changelog site has not caught up
with an upload yet, or the download failed. With no changelog and no update
text, from either backend, the page says that none is available and links
to the project's page, the `url` PackageKit's `GetDetails` gives (the
package's `Homepage` or `URL`), when it is an http or https address; a
failed `GetDetails` leaves the link out and the rest of the details as they
are.

## Third-party notices

The licences of what LinuxIO holds ask for their text to go with every copy.
A release holds about 50 Go modules, well over a hundred frontend packages,
two fonts and six icon sets, and the set changes with every dependency
update, so the file a package installs is written by the build, from the
build, and not kept by hand.

`make build-binaries` ends with `make third-party-notices`
(`packaging/scripts/third-party-notices.sh`), which writes
`build/THIRD_PARTY_NOTICES.md` beside the programs:

1. `docs/THIRD_PARTY_NOTICES.md`, the part written by hand: the projects
   LinuxIO adapted code from, the icon sets, and where the source of the two
   MPL-2.0 parts is. The icon sets are there because
   `frontend/scripts/generate-icons.mjs` copies their data into LinuxIO's own
   source, where no tool sees a package; `package-files-fixtures.sh` fails
   when an `@iconify-json` set of `frontend/package.json` is not named.
2. Go's licence, for the standard library and runtime in every Go program.
3. Every module the eight Go programs report as linked (`go version -m`),
   with each `LICENSE`, `COPYING` and `NOTICE` file it has in the module
   cache, those of code it carries in a subfolder included, and a link to
   its source at that version. What `go.mod` lists and
   no program links is left out. A file with the same bytes as one already
   written is named, not repeated.
4. The packages in the frontend bundle, from `.vite/license.md`, which Vite
   writes into the frontend folder the webserver embeds (`build.license` in
   `frontend/config/vite.config.ts`): the packages it bundled, and only
   those.

The script fails, and the build with it, when a linked module has no licence
file or a built frontend has no `.vite/license.md`, so a new dependency
cannot ship unnamed. A build over the placeholder frontend of `make dev-prep`
says that it holds no bundle. `linuxio-auth` is C and links only the host's
libpam, libsystemd and libc.

Both package scripts take the file from the build folder, the test VM gets it
with the programs, and a release carries it from the compile job to the RPM
job in the tar of the programs, so the DEB, the RPM, `make localinstall` and
the VMs install the same text. `package-contents.sh` holds a package's copy
to the build's.

## Known limits

- The notices are written only where Go's own `LICENSE` is in the Go root,
  as in Go's downloads and the toolchains Go fetches; a distribution's Go
  package may keep it elsewhere, and the build then fails and says so.
- amd64 (`x86_64`) only, and a C library of 2.39 or newer, Ubuntu 24.04's:
  RHEL 9 and its rebuilds cannot run it
  ([Releases](./releases.md#why-the-compile-image-is-the-oldest-supported-system)).
- Port 8090 unless a socket drop-in changes it, and the Avahi file then names
  the new port only once it is edited by hand.
- `apt remove` keeps every configuration file under `/etc` (the Avahi
  announcement and the libvirt hook among them) until `apt purge`; an RPM
  erase removes the ones nobody edited.
- Removing LinuxIO removes the libvirt hook's effect: a bridged VM on a host
  with Docker can lose its network at its next start.
- On the Debian family a reinstall does not bring back a configuration file
  an administrator deleted; the libvirt hook has its Restore button for that.
- An RPM upgrade brings back a libvirt hook an administrator deleted and does
  not restart libvirt, so the hook is used from the daemon's next start.
- An edited configuration file keeps its edits and misses that upgrade's
  changes to it.
- On Fedora, dnf and PackageKit go on without a LinuxIO repository they
  cannot read ([above](#linuxios-own-update)). The Updates page says so only
  after Refresh Sources and only until it is left; the update banner, an
  unattended upgrade and a `dnf` whose output nobody reads do not.
- `dnf5-automatic` imports no key: on a Fedora host where LinuxIO was not
  installed by the README's `dnf install` (from a file, say), it declines the
  prompt for the repository's metadata key, updates nothing and exits 0, and
  with that key accepted it fails the package on its signature, since rpm
  lacks the key (both seen on Fedora 45 in the test VM). The metadata key is
  dnf's own and the package key rpm's; one `dnf upgrade linuxio` that accepts
  the key puts it in both.
- The check after Refresh Sources reads LinuxIO's repository even where an
  administrator disabled it, and shows any failure of that dnf command as
  the reason, a renamed section in `linuxio.repo` among them.
- An RPM upgraded from a version without the console link shows the login
  banner at an SSH login only, until the link is made by hand
  ([above](#the-login-banner)).
- The console shows the banner at the first prompt of a boot only when
  `linuxio-issue.service` has written it by then: nothing orders the getty
  after it. A later prompt (`agetty`'s after a logout or a login that timed
  out, `kmscon`'s each minute its login times out) reads the file again. On
  the Fedora 45 test VM the first prompt of two boots showed it.
- An upgrade restarts `linuxio.target`, which ends every session.
- A sudoers rule that names `/usr/local/bin/linuxio-bridge`, or an
  administrator's own unit that names a program under `/usr/local/bin`, from
  a script install needs the new path under `/usr/libexec/linuxio`.
- The settings whose files removal deletes are switched on again by hand
  after a new install, and a script install's settings are made again after
  the package replaces it.
- A purge keeps the paths of `admin_state` whether or not a setup still uses
  them, so the secrets of an rsync service or a CIFS mount that was deleted
  by hand rather than from LinuxIO stay until deleted by hand.
- `/var/lib/linuxio/icons` and its folders are writable by every user (the
  Docker icon cache), so a local user who mounts something in one, with
  FUSE say, keeps a purge from deleting `icons` and with it
  `/var/lib/linuxio`; the purge names it and deletes the rest.
- The removal deletes chrony's time server file only in
  `/etc/chrony/sources.d`, Debian's and Ubuntu's folder: on a `chrony.conf`
  edited to name another folder under `/etc`, the `linuxio.sources` or
  `linuxio.conf` LinuxIO wrote there stays.
- A package's changelog holds its own release's notes only, so an update
  over several releases shows the newest one's; the packages of releases
  before the notes were carried say `LinuxIO VERSION.` alone.
