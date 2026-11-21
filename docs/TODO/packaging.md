# Package-managed installation and updates

LinuxIO will make a clean break from its script-based installer and updater.
The first package-managed release will ship simultaneously as DEB and RPM,
use signed LinuxIO repositories, and update through the existing PackageKit
integration.

Existing script-installed systems are not supported by this cutover. They
must be removed manually or replaced with a fresh installation. The new
packages must not delete administrator-owned files under `/usr/local` or
`/etc`.

## Decisions

- [ ] Ship one package named `linuxio` containing all nine programs and
      system integration.
- [ ] Publish DEB and RPM together. A failure in either format blocks the
      release.
- [ ] Initially support `amd64`/`x86_64`, matching the current release.
- [ ] Support Debian stable, Ubuntu LTS from 24.04 on (decided 2026-11-21),
      and the current and previous Fedora releases as release-gated
      platforms.
- [ ] Publish only a `stable` channel initially.
- [ ] Host both repositories as a static GitHub Pages deployment behind a
      name under the owner's domain `engmariz.com` (working name
      `packages.engmariz.com`), never the default `github.io` address. An
      installed host saves the repository address, so only a name the owner
      controls lets the hosting move later without every administrator
      editing that file. Decided 2026-11-21; the owner does not own
      `linuxio.io`. A project domain, if one is wanted, is chosen before the
      first other person installs.
- [ ] Use PackageKit for update discovery and installation; never download or
      execute an installer script from the application.
- [ ] LinuxIO's own update is the package `linuxio` among the host's other
      updates on the Updates page. There is no separate update dialog and no
      `control.app_update` route; a one-line banner says a new version is
      available and opens the Updates page. Decided 2026-11-21.
- [ ] The package restarts LinuxIO when it is upgraded, as Cockpit's packages
      restart their service, so an upgrade from a shell, an unattended
      upgrade and an update from the Updates page all end the same way. The
      bridge has no restart path of its own. Decided 2026-11-21.
- [ ] Do not add legacy migration, compatibility symlinks, source packages,
      extra release channels, or additional architectures in this work.

## Build order

The work is on the branch `feat/packaging`, which no release is cut from
until the last step lands.

1. [x] The new folders: the compiled paths, the units' command lines, the
   SELinux file context, and `make package-root` with its test.
2. [x] The DEB, and `make localinstall` installing it.
3. [x] The RPM, with the SELinux module.
4. [x] The test VMs install packages.
5. [ ] The login banner, the job units in the package, removal, the hook's
   Restore button.
6. [x] LinuxIO's own update through the Updates page; the app updater
   deleted.
7. [ ] Signed repositories (the scripts and the test VMs' repository are
   done), pinned build images, the release workflow.
8. [ ] The installer scripts deleted, this plan rewritten as a doc.

Until the packages replace it, the script installer on this branch installs
the new folders itself: `linuxio` in `/usr/bin`, the other programs in
`/usr/libexec/linuxio`, the units still in `/etc/systemd/system`. It removes
the nine programs and `linuxio-btrfs-timer` from `/usr/local/bin` by name and
rewrites the saved units and drop-ins that name them there.

The branch merges into `main` only with step 8, never part-way:

- A published installer (v0.54, v0.55) fetches the units and the SELinux
  module from `main` and puts its programs in `/usr/local/bin`. With this
  branch's units on `main`, an install or in-app update made by one of them
  would leave every service naming a program that is not there.
- This branch's installer, run without `--local`, would download a release
  built for `/usr/local/bin` into the new folders, where its `linuxio-auth`
  finds no bridge. On this branch it is run with `--local` only, by the
  indexer smoke test.

Not working on this branch:

- A sudoers rule that grants web administration by naming
  `/usr/local/bin/linuxio-bridge`, and an administrator's own unit that
  names `/usr/local/bin/linuxio-worker`: both need the new path.

## Required changes

### 1. Keep the CLI name

- [ ] Keep the current build output in `mk/go.mk:5`:

  ```make
  cli_binary := $(bin_dir)/linuxio
  ```

- [ ] Install that file as `/usr/bin/linuxio` from the package-root target.
- [ ] Do not build `linuxio-cli` or add a compatibility symlink.

### 2. Separate public and private paths

- [x] Replace `BinDir` in `backend/common/version/paths.go` with the
      private executable location:

  ```go
  const LibexecDir = "/usr/libexec/linuxio"
  ```

- [x] Do not add a `CLIPath` constant unless code genuinely needs to invoke
      the CLI.
- [x] Use `LibexecDir` when the webserver spawns the bridge in
      `backend/webserver/bridge/bridge.go:59`.
- [x] Update the Docker updater path in
      `backend/common/dockerupdate/sandbox.go:11` (`RunnerPath`).
- [x] Update the authentication bridge path in
      `backend/auth/linuxio-auth.c:64`.
- [x] Update every package-owned systemd `ExecStart` entry.
- [x] Update the verbose systemd drop-ins written by
      `backend/cli/verbose.go:70`.

### 3. Simplify `linuxio version`

Native packaging guarantees that every file in the package has one version.

- [x] Remove `versionExecCommand` and the private-binary subprocess
      probes from `backend/cli/version.go`. The record of one version per
      program that replaced them is gone too
      ([Versions](../process-systemd-architecture.md#binaries)).
- [x] Remove `version --self`.
- [x] Make `linuxio version` print the package/build version.
- [x] Support the conventional `linuxio --version` with the same result.
- [x] Remove component-version probing from the webserver: `/api/version`
      (`backend/webserver/versionapi`) answers with its own build version.

### 4. Clarify help text

- [x] Use this simpler command summary:

  ```text
  LinuxIO system administration

  linuxio status
  linuxio logs [component] [lines]
  linuxio start
  linuxio stop
  linuxio restart [--full]
  linuxio verbose enable|disable|status
  linuxio version
  ```

### 5. Add one handwritten man page

- [x] Add `packaging/man/linuxio.8` in plain roff (the earlier draft was deleted). Do not add a documentation
      generator.
- [x] Document the synopsis, commands, required privileges, files, systemd
      units, and examples.
- [x] Install it in the DEB through `debian/linuxio.manpages`.
- [x] Include `%{_mandir}/man8/linuxio.8*` in the RPM. debhelper compresses it; the RPM gzips
      it in `%install`, because the spec switches off the scripts that would
      (they also strip programs).

### 6. Update tests and documentation

- [x] Remove component-probing and `--self` tests.
- [x] Test both `linuxio version` and `linuxio --version`.
- [x] Update systemd path assertions.
- [x] Update the binary-location paragraph in `docs/process-systemd-architecture.md`.
- [x] Add the man page to package-content tests.

## Development workflow

Keep `make localinstall` as the developer entry point, but make it exercise the
same native package layout as a release. It must no longer copy binaries or
systemd units itself.

```text
make localinstall
  -> make fastbuild
  -> select DEB or RPM from /etc/os-release
  -> build the native package for the current distribution
  -> install or reinstall that exact package with apt or dnf
  -> start linuxio.target after the package transaction
```

- [x] Make package construction consume the exact outputs from `fastbuild`, so
      all installed binaries carry the same build version.
- [x] Run the build and package construction unprivileged; elevate only the
      package-manager install and the post-transaction start.
      - A start, not a restart: the package restarts a running LinuxIO
        itself, so a restart would restart it twice; the start brings up one
        that was stopped.
      - Not yet run on a host: the machine this was written on has no
        debhelper. The same scripts assemble and install in the test VMs.
- [ ] Keep `make dev` as the frontend hot-reload workflow against the installed
      systemd backend.
- [x] Use `make localinstall` for the first development install and after
      backend changes. Individual build targets only build; they do not
      install.
- [x] Delete `packaging/scripts/localinstall.sh` (today a thin wrapper over
      `install-linuxio-binaries.sh --local`) after the package-based target
      replaces it.
- [x] Remove `make reinstall`. The VS Code reinstall key runs
      `make localinstall`.
- [ ] Remove `make uninstall` with the installer (step 8). It stays until
      then: the package refuses to install over a script install, which
      `make uninstall` removes.
- [ ] Implement package tooling and the package-based `make localinstall`
      before simplifying version reporting; the one-version assumption is
      valid only after every supported install path installs one complete
      package.

### Test VMs

The rig (`packaging/vm/linuxio-vm.sh`, `mk/vm.mk`) builds on the developer's
machine, copies the build and `packaging/` into the VM, and the VM assembles
and installs the package. Decided 2026-11-21.

- [x] `install` assembles the DEB or the RPM inside the VM from the
      developer's build, with the script `make localinstall` and a release
      run (`package-deb.sh` on Ubuntu, `package-rpm.sh` on Fedora), checks it
      with `package-contents.sh` and installs it with `apt` or `dnf`. A
      Debian-family host cannot assemble an RPM, and the VM's own release
      then assembles it as a release's image does; one compile goes in both
      packages, as in a release.
      - The VMs' `clean` snapshots made before the seed had the assembling
        tools lack them; `install` adds them each time (about 30 s) until
        `make vm-update` takes the snapshot again with them.
- [x] The update specs (`jobs-update.spec.ts`, `jobs-phase5a.spec.ts`)
      install package A and update to package B from a signed repository
      inside the VM, the path of the release gate:
      `linuxio-vm.sh repo-publish` publishes a build there with the
      release's `repo-apt.sh` or `repo-rpm.sh` and a throwaway key made in
      the VM, served by `file://`. `package-repository.spec.ts` updates from
      it in a shell and checks that a changed index or package is refused.
      `vm-update-build` gives the update build a version of its own.
- [x] The updater suite went with the app updater it tested.
- [ ] No separate native build is a gate: the Fedora VM runs on the release
      packages are.

## 1. Establish the package-owned filesystem layout

Use this common layout:

```text
/usr/bin/linuxio
/usr/libexec/linuxio/
    linuxio-webserver
    linuxio-bridge
    linuxio-auth
    linuxio-docker-update
    linuxio-indexer
    linuxio-monitoring
    linuxio-worker
    linuxio-claude-relay
/usr/lib/systemd/system/linuxio*
/usr/lib/tmpfiles.d/linuxio.conf
/usr/share/linuxio/
/etc/linuxio/
/etc/pam.d/linuxio
/var/lib/linuxio
```

- [x] Add one shared `make package-root DESTDIR=...` target that installs the
      common package tree.
- [x] Replace compiled `/usr/local/bin` paths with `/usr/bin` for the CLI and
      `/usr/libexec/linuxio` for private executables.
- [x] Update the C authentication helper's bridge path.
- [x] Update package-owned systemd units to execute binaries from
      `/usr/libexec/linuxio`.
- [x] Keep the five programs systemd runs as root (`linuxio-worker`,
      `linuxio-docker-update`, `linuxio-auth`, `linuxio-indexer`,
      `linuxio-monitoring`) at mode 0700, owned by root, as the installer's
      `binary_mode` sets them today; the other programs stay 0755. Decided
      2026-11-21. Cockpit installs every helper 0755; LinuxIO is stricter on
      purpose, so no other local user can run a private word or flag.
      - RPM: `%attr(0700,root,root)` on each of the five. The spec's file
        list is made from the staged tree, so the five are the ones
        `package-root.sh` makes 0700.
      - DEB: `dh_fixperms` resets every program to 0755, so the rules set
        0700 again after it, on the files `package-root.sh` made 0700.
      - The package-content test (`package-contents.sh`) checks the nine
        modes.
      - [ ] The `AGENTS.md` rule then names the packages in place of
        `binary_mode` in the installer.
      - What each program answers to `--help` and `--version`, and to a
        user who is not root, is one rule since v0.55
        ([Binary invocation map](../process-systemd-architecture.md#binary-invocation-map)):
        a root-only program answers "Permission denied" to another user.
- [x] Ship the SELinux module (`packaging/selinux/linuxio.cil`) in the RPM
      and load it from the package, with its file context changed to the
      installed path of `linuxio-auth`, `/usr/libexec/linuxio/linuxio-auth`
      ([Process & Systemd Architecture](../process-systemd-architecture.md#selinux)).
      - This is what makes a changed module reach a host at an update: `dnf`
        runs the package's own scripts with no sandbox, where the in-app
        updater today has `/etc/selinux` read-only. It replaces the fix
        planned in
        [SELinux module in in-app updates](./selinux-module-in-app-update.md),
        which is not built. Decided 2026-11-21.
      - It is `/usr/share/selinux/packages/targeted/linuxio.cil`, loaded in
        the post-install step by Fedora's `%selinux_modules_install`
        (`semodule -n -X 200 -i`, then `load_policy` where SELinux is on;
        neither fails the transaction), then `restorecon` on
        `linuxio-auth`, all before any unit is enabled or started. On a first
        install rpm writes `linuxio-auth` before the module names its type,
        so the relabel is what gives it `linuxio_auth_exec_t`; at an upgrade
        rpm writes it with the type already. `%selinux_modules_uninstall`
        removes the module in the post-uninstall step on an erase only,
        after the files. The requirements are `%selinux_requires_min`:
        `selinux-policy` at Fedora's stable floor, not at the version of the
        machine that assembled the RPM.
      - [ ] When the in-app updater is deleted (step 6), delete that file and
        its link in `README.md` here, delete the Known limits line "An in-app
        update cannot load a module that is new or changed", and reword the
        `AGENTS.md` rule that says the installer loads the module and
        relabels the binary.
- [x] Install package-owned units under `/usr/lib/systemd/system`.
- [x] Continue using `/etc/systemd/system` only for administrator or
      runtime-generated drop-ins. The packages put only the links of the
      units they enable there.
- [ ] Ship the three job units whose text is the same on every host as
      package units, as `linuxio-btrfs-health.timer` and
      `linuxio-btrfs-degraded.service` already are:
      `linuxio-btrfs-snapshot.service`, `linuxio-btrfs-replicate.service`
      (`backend/common/btrfs/unit.go`) and `linuxio-docker-update.service`
      (`backend/common/dockerupdate/timer.go`). Decided 2026-11-21.
      - Saving a setting then writes or enables only the timer, which holds
        a schedule and no command line, so an update can never leave one of
        these three naming a program that moved.
      - Cockpit gets the same result another way: a timer it writes runs the
        administrator's command through `/bin/sh` and never a Cockpit
        program. LinuxIO's timers run LinuxIO's own work, so the unit that
        names the program comes with the package instead.
      - Two kinds of saved file still name a LinuxIO program, because a
        setting decides what they hold: the verbose drop-ins
        (`backend/common/verbosemode`) and the alert drop-ins on the
        distribution's update services
        (`backend/bridge/handlers/packages/internal/autoupdate/notify.go`).
- [x] Ship the run-record drop-in (`packaging/systemd/linuxio-run-record.conf`)
      as a package file in each of the eight folders of the installer's
      `run_record_dropin_dirs`, under `/usr/lib/systemd/system`, with the
      program's new path. It is the same file everywhere and depends on no
      setting, so it updates and is removed with the package.
- [x] Treat `/etc/linuxio`, `/etc/pam.d/linuxio`, and other administrator
      configuration as preserved package configuration: every file under
      `/etc` is a conffile in the DEB and `%config(noreplace)` in the RPM.
      - An RPM erase removes the ones nobody changed and keeps a changed one
        as `.rpmsave`, so `/etc/linuxio` goes with an untouched install; the
        DEB keeps them all until `apt purge`.
- [x] Carry the needrestart override that the script installer installs
      today (`packaging/etc/needrestart/conf.d/linuxio.conf` as
      `/etc/needrestart/conf.d/linuxio.conf`): a package update runs through
      dpkg, after which Ubuntu's needrestart would restart the job hosts and
      their jobs ([Jobs](../jobs.md#updates)). The DEB ships it whether or
      not needrestart is installed, which reads it when it is; the RPM has
      none.
- [ ] Ship the Avahi announcement
      (`packaging/etc/avahi/services/linuxio.service`) as
      `/etc/avahi/services/linuxio.service` in every install, as a
      configuration file, whether or not Avahi is installed: Avahi is a
      capability people add later, and it reads the file whenever it starts.
      Today the installer skips the file when Avahi's folder is missing.
      Decided 2026-11-21. Cockpit ships no such file. The file names port
      8090, so a port changed in a drop-in is changed here by hand too: one
      line under Known limits and in the port-change instructions. On the
      Debian family `apt remove` leaves the file, so the announcement stays
      until `apt purge`.
- [ ] Ship the libvirt hook
      (`packaging/libvirt/hooks/qemu.d/linuxio-bridge-netfilter`) as
      `/etc/libvirt/hooks/qemu.d/linuxio-bridge-netfilter`, mode 0755, in
      every install, as the installer does today; libvirt reads hooks only
      from `/etc`. Decided 2026-11-21. Cockpit ships no hook.
      - It is a plain file, marked as configuration in both packages
        (`%config(noreplace)` in the RPM, a conffile in the DEB), so the two
        behave alike: an untouched copy is replaced at an upgrade, and a copy
        an administrator edited is kept, without that upgrade's fix.
      - libvirt finds its hooks only when its daemon starts, so the
        package's post-install step runs `systemctl try-restart
        libvirtd.service virtqemud.service` after the file is in place, as
        the installer does today.
      - Its first line exits when `/usr/bin/linuxio` is missing. `apt remove`
        leaves the file until `apt purge`, and libvirt would otherwise go on
        running it; with the line, removal stops the hook on both families.
      - Removing LinuxIO removes the fix: a bridged VM on a host with Docker
        can lose its network at its next start, as after the uninstaller
        today. One line in the removal instructions.
      - Considered and not chosen: the script under `/usr` with a link in
        `/etc` made at first install, the pattern of the banner links. It
        would deliver every fix and honour a deleted link on both families,
        but a program started through a link runs under the SELinux label of
        the file it points to, not of `/etc/libvirt/hooks`, and checking that
        needs a Fedora VM running a bridged VM beside Docker.
- [x] Ship a second, untouched copy of the hook under
      `/usr/share/linuxio/libvirt/` and give the hook warning a Restore
      button in place of "reinstall LinuxIO". Decided 2026-11-21. On the
      Debian family a reinstall does not bring back a file deleted under
      `/etc` (from memory, to be checked in the Ubuntu VM), so the advice the
      warning gives today stops being true there.
      - The warning is the one in bridge options and in the VM preflight
        ([VM Bridge Networking](../vm-bridge-networking.md)). It is offered a
        Restore in the two cases it reports: the hook is missing, or it is
        not an executable regular file.
      - Restore is a new privileged mutation that takes no path from the
        request: it copies the fixed file under `/usr/share/linuxio` to the
        fixed hook path, mode 0755 and owned by root, through a temporary
        file and a rename, restarts the two libvirt daemons the same way so
        libvirt finds the hook, and logs one Info line.
      - The warning is a plain string in `warnings` today. A button needs the
        hook's state as a field of its own in the answers of `virt.preflight`
        and `network.get_bridge_options`, so the contract changes
        (`make generate`), and the Virtual Machines page and the two bridge
        dialogs change with it.
- [x] Keep `/var/lib/linuxio` across package removal, on a purge too.
- [x] Install licenses using each distribution's native locations:
  - DEB: `/usr/share/doc/linuxio/copyright` and third-party notices.
  - RPM: `%license LICENSE` and `%doc THIRD_PARTY_NOTICES.md`.
- [ ] Stop rewriting the package-owned socket unit during installation. Use
      port `8090` by default and document a systemd socket drop-in for changing
      it.
- [ ] Remove installer behavior that adds the invoking user to journal groups.
- [x] Add a minimal pre-install guard for exact legacy sentinels such as
      `/usr/local/bin/linuxio` or `/etc/systemd/system/linuxio.target`.
      Refuse the install with a clean-install message; do not remove them.
      The DEB's `preinst` checks on every install (also one after
      `apt remove`), the RPM's `%pre` on a first install; the message names
      the uninstall script.

### Login banner

The banner keeps its design, which is Cockpit's (`src/systemd/update-issue`,
`src/systemd/cockpit.socket.in`, `tools/cockpit.spec` and
`tools/debian/cockpit-ws.postinst` on Cockpit's `main`, read 2026-11-21).
Decided 2026-11-21: take the following from Cockpit.

- [ ] Ship `packaging/scripts/update-issue` as the package file
      `/usr/share/linuxio/issue/update-issue`; nothing fetches it.
- [ ] Read the port from systemd (the last line of
      `systemctl show --property Listen linuxio-webserver.socket`) instead of
      parsing the unit file, so a port set in a drop-in is the port shown.
- [ ] Show the address before the login prompt as well: a link
      `/etc/issue.d/linuxio.issue` beside `/etc/motd.d/linuxio`, both to
      `../../run/linuxio/issue`.
- [ ] Use the same two links on the Debian family and delete
      `packaging/etc/update-motd.d/60-linuxio`, if the Ubuntu check below
      passes; otherwise that script stays.
- [ ] Create the links in the package's post-install step, on first install
      only, so an administrator who deletes them does not get them back at
      an update. Removal deletes them only while they are still LinuxIO's
      links.
- [ ] When LinuxIO stops, write "Start LinuxIO with: linuxio start" into the
      same file. One file, not Cockpit's active and inactive files with a
      link switched between them: the SELinux module names
      `/run/linuxio/issue`, and one file leaves the module as it is.

Kept from LinuxIO's script, which Cockpit's does not have:

- [ ] The fallback address for a host with no default route (the first
      global IPv4 address on an interface that is not `lo`, `docker*`,
      `virbr*` or `br-*`).
- [ ] The `restorecon` of the file, without which `sshd` may not read it on
      an enforcing RHEL-family host.
- [ ] The script stays `bash` and runs once, from `linuxio-issue.service`;
      its hostname and address lookups stay as they are.

Checks, each in the VM as the package installs it and again after a reboot:

- [ ] Ubuntu: a link in `/etc/motd.d` shows at SSH login.
- [ ] Fedora, enforcing: the console shows the banner through `/etc/issue.d`
      with no denial. The module labels the file `motd_var_run_t` for `sshd`,
      and whether the console's login program may read that type is not
      known. If it is refused, either the module gains a rule or the RHEL
      family gets no link in `/etc/issue.d`.

## 2. Build native DEB and RPM packages

- [x] Add native Debian packaging metadata using debhelper/dpkg tooling
      (`packaging/debian`, debhelper compat 13, one binary package `linuxio`
      for amd64, assembled by `packaging/scripts/package-deb.sh`).
- [x] Add `packaging/rpm/linuxio.spec` using `rpmbuild`
      (`packaging/scripts/package-rpm.sh`; `ExclusiveArch: x86_64`,
      `Release: 1` with no distribution tag, one RPM for every Fedora
      release).
- [x] Make both formats consume the shared package-root target instead of
      maintaining two file-copy implementations.
- [x] Neither package changes a byte of a program, since the webserver
      checks the SHA-256 of the bridge recorded at build time: the DEB's
      rules leave out `dh_strip` and `dh_dwz`; the spec has no debuginfo
      package, no build-id links and no `__os_install_post` (no `brp-strip`,
      no shebang rewrite, no `add-det`). `package-contents.sh` compares the
      nine programs' SHA-256 with the build's.
- [x] Both carry one version (`packaging/scripts/package-version.sh`,
      `PACKAGE_VERSION`): `X.Y.Z` on a commit tagged `vX.Y.Z`,
      `X.Y.Z+devN.gSHA` after it.
- [ ] The DEB's maintainer and changelog address is the placeholder
      `packages@linuxio.invalid` until the owner names one.
- [x] Compile the programs once and put the same programs in both packages.
      Decided 2026-11-21; it replaces "build the RPM on Fedora and never
      package Ubuntu-built programs for Fedora". Releases already ship one
      build to both families through the installer, and the test VMs run it
      on Fedora.
      - The compile runs on the supported system with the oldest C library,
        since a program runs where the libraries are at least as new as
        where it was built. That is not Debian by default: Ubuntu 24.04 has
        C library 2.39 and Debian 13 has 2.41 (from memory; read both when
        the image is chosen). With Ubuntu 24.04 supported, the compile image
        is Ubuntu 24.04 unless those numbers turn out otherwise.
      - A DEB assembled on Ubuntu takes the names of the packages it
        depends on from Ubuntu. The fresh-install gate on Debian stable
        shows whether Debian has them under the same names.
      - The DEB is assembled in that same image. The RPM is assembled from
        the compiled programs in a Fedora image, which has the RPM tools and
        the systemd and SELinux macros the spec uses; nothing is compiled
        there.
      - The other libraries the programs load (`libpam.so.0`,
        `libsystemd.so.0`, `libaudit.so.1`, `libcap-ng.so.0`) have the same
        names on both families today. Each package lists the libraries it
        needs, so a family that lacks one refuses the install, which the
        Fedora release gate would show before a release.
- [x] Let native tooling derive shared-library requirements
      (`dh_shlibdeps`: `libc6`, `libpam0g`, `libsystemd0`; rpm's ELF
      dependencies: `libc.so.6`, `libpam.so.0`, `libsystemd.so.0`).
- [x] Declare PackageKit, PAM, polkit, systemd, and other runtime dependencies
      with the correct distribution-specific package names: the DEB depends
      on `libpam0g`, `packagekit`, `polkitd | policykit-1` and `systemd`, the
      RPM requires `pam`, `polkit`, `PackageKit` and `systemd`, the mandatory
      set of `install-dependencies.sh`. That script installs nothing
      optional, so neither package recommends or suggests anything; the
      RPM's one weak dependency, a newer `selinux-policy`, comes from
      `%selinux_requires_min`.
- [x] Mark Debian configuration as conffiles and RPM configuration as
      `%config(noreplace)`.
- [x] On first install, create runtime directories, reload systemd, enable and
      start `linuxio.target`, and enable the indexer timer.
      - Enabled as the installer does: `linuxio.target`,
        `linuxio-indexer-index.timer`, `linuxio-monitoring.service`,
        `linuxio-jobs@0.service`, and `linuxio-btrfs-health.timer` with
        `linuxio-btrfs-degraded.service` where a btrfs filesystem exists;
        started: `linuxio.target`, monitoring and the login banner. The root
        job host is enabled, not started, and starts at boot or at a sign-in.
      - The DEB enables through `deb-systemd-helper`, so an administrator's
        disable holds at an upgrade, except the job host's template
        instance, which it cannot read: `systemctl` enables that on a first
        install only. It starts and restarts through `deb-systemd-invoke`, so
        `policy-rc.d` and chroots are respected. Its first install is one
        with no `deb-systemd-helper` record of `linuxio.target`, which
        includes an install after `apt remove`.
      - debhelper's own unit handling (`dh_installsystemd`) is left out:
        it would stop or restart the units it finds, the job hosts among
        them. `dh_installtmpfiles` stays and creates the folders first.
- [x] On upgrade, reload systemd and restart `linuxio.target` if it is
      running: `deb-systemd-invoke try-restart` in the DEB's post-install
      script and `%systemd_postun_with_restart` in the RPM, as Cockpit's
      packages do for `cockpit.service`. A stopped LinuxIO stays stopped.
      - The job hosts are not part of `linuxio.target`, so running jobs go
        on, among them the `packages.update` job that started the upgrade
        ([Jobs](../jobs.md#updates)).
      - `linuxio-auth@.service` is `PartOf=linuxio.target`, so every session
        ends and its user signs in again.
      - The DEB restarts when LinuxIO's own package is configured, which in
        a larger upgrade is before the transaction ends. The RPM's
        `%systemd_postun_with_restart` marks the units, and systemd restarts
        them when the dnf transaction ends.
      - Monitoring and the login banner are restarted the same way.
- [x] On removal, stop and disable services while preserving application data.
      The target, the sockets, monitoring, the banner and every job host and
      job socket stop; what the install enabled is disabled.
- [ ] On removal, also delete what LinuxIO itself wrote under
      `/etc/systemd/system` while it ran, as `uninstall.sh` does today; a
      package manager removes only the files it installed. Decided
      2026-11-21. `/etc/linuxio` and `/var/lib/linuxio` stay.
      - The snapshot and Docker update timers, stopped and disabled first:
        left behind, each would start a service the package just removed.
      - The verbose drop-ins.
      - The alert drop-ins on the distribution's update services
        (`linuxio-alert.conf`): left behind, their `ExecStopPost=` names a
        program that is gone, and the nightly upgrade is marked failed.
      - The schedule drop-ins on the distribution's update timers
        (`linuxio.conf`). They hold only a time and would break nothing;
        they go too, so removal leaves nothing of LinuxIO's there, and the
        distribution's own schedule returns.
      - After a removal and a new install these settings are switched on
        again by hand, as after the uninstaller today.
- [x] Add `make package-deb` and `make package-rpm`. Each fails, naming the
      package to install, on a host without its tool.
- [x] Replace the current `make localinstall` implementation with the
      package-based development workflow above.

### Pinned build environment

A program records which system libraries it needs, and at least which
version, from the machine that built it. The build of 2026-11-21 on the
owner's Ubuntu (C library 2.43) gives a `linuxio-auth` that needs C library
2.38 or newer, with PAM, systemd, audit and libcap-ng; all nine programs need
the system's C library. A release therefore runs only where the libraries are
at least as new as on its build machine, and what that machine is must not
change by itself. Decided 2026-11-21: everything a release is built from is
pinned in the repository, and one Make target moves the pins.

State on 2026-11-21:

| What | Pinned | Where |
| --- | --- | --- |
| Go | yes, `1.27.4` | `backend/go.mod` |
| Go modules | yes | `go.sum`, moved by `make update-go-deps` |
| npm packages | yes | the lock file, moved by `make update-deps` |
| nvm | yes, `0.40.2` | `Makefile` |
| Release build machine | no, `ubuntu-latest` | `.github/workflows/release.yml` |
| System build libraries (PAM, D-Bus, systemd, the compiler) | no, whatever `apt-get install` gives that day | the same workflow |
| Node | major only, `26` | `frontend/package.json` |
| Go check tools (golangci-lint, modernize, deadcode, govulncheck) | no, `latest` | `mk/toolchain.mk` |
| GitHub Actions | 2 by commit, 5 by major tag | `.github/workflows` |

- [ ] Name the two images in one file of the repository, each as a
      distribution release and its image digest: the one that compiles the
      programs and assembles the DEB, and the Fedora one that assembles the
      RPM. Releases are built only in them (`container:` in the workflow).
      The release, not the day, then decides the library versions a package
      asks for; inside one release the distribution keeps those libraries
      compatible. Decided 2026-11-21: the images are the distributions' own,
      named by digest; an image of LinuxIO's own with the build libraries
      already in it, which would pin those too, is not built.
- [ ] Stop building releases on `ubuntu-latest`.
- [ ] Pin Node to a full version, the Go check tools to versions, and every
      GitHub Action to a commit.
- [ ] Add one Make target (working name `make update-build-pins`) that moves
      the image digests, Node, the check tools and the action commits to
      their newest, shows what changed and prints a commit message, as
      `make update-deps` does for npm. Go modules and npm keep their own
      targets.
- [ ] `make localinstall` and the test VMs build on the developer's machine
      and are not pinned; only a release is.

## 3. Publish signed static repositories

### APT

Publish:

```text
apt/
  linuxio.sources
  pool/main/l/linuxio/linuxio_<version>_amd64.deb
  dists/stable/InRelease
  dists/stable/Release
  dists/stable/Release.gpg
  dists/stable/main/binary-amd64/Packages.gz
```

`packaging/scripts/repo-apt.sh` (`make repo-apt`) adds the DEBs of a folder
to the tree, keeps the packages already there, regenerates the metadata over
all of them and signs it with a key the caller's keyring holds; it never
creates or imports a key. A version already published with other bytes is
refused.

- [x] Generate `Packages` and `Release` metadata with `apt-ftparchive`.
- [x] Publish SHA256 by-hash metadata paths (SHA-256 sums only, so apt
      fetches by those; earlier copies stay for clients mid-update).
- [x] Sign `Release` as both `InRelease` and `Release.gpg`.
- [x] Publish `linuxio.sources` with an embedded `Signed-By` public key.
- [x] Do not use `trusted=yes` or otherwise permit unsigned repositories.

### RPM

Publish:

```text
rpm/
  linuxio.repo
  RPM-GPG-KEY-linuxio
  stable/x86_64/linuxio-<version>-1.x86_64.rpm
  stable/x86_64/repodata/
```

`packaging/scripts/repo-rpm.sh` (`make repo-rpm`) does the same for the
RPMs, signing each copy it adds; a version already published from another
build (another header digest) is refused.

- [x] Sign every RPM with `rpmsign`.
- [x] Generate repository metadata with `createrepo_c`.
- [x] Sign `repodata/repomd.xml`.
- [x] Set `gpgcheck=1` and `repo_gpgcheck=1` in `linuxio.repo`.

### Trust and hosting

- [ ] Generate an offline OpenPGP master key and a CI-only signing subkey.
- [ ] Commit the public key and document its full fingerprint.
- [ ] Store the signing subkey only in a protected GitHub release environment
      with required approval.
- [ ] Ensure pull requests cannot access signing secrets.
- [ ] Configure the name under `engmariz.com` (one DNS record, with GitHub
      Pages' own certificate) so hosting can move away from GitHub Pages
      later without changing installed clients.
- [ ] Publish only the last few versions in the repository tree; older
      packages stay on GitHub Releases. The built binaries are 89 MB
      (2026-11-21), and GitHub Pages limits the size of a published site
      (about 1 GB, from memory): read the current limit before fixing how
      many versions stay.
- [ ] Provide documented commands that install the static `.sources` or
      `.repo` file and then invoke `apt install linuxio` or
      `dnf install linuxio`.
- [ ] Do not create an executable bootstrap installer or a repository bootstrap
      package.

## 4. Replace the application updater with the Updates page

LinuxIO's own update is the package `linuxio` among the host's other updates.
The successful update path must be:

```text
Browser
  -> the banner "LinuxIO <version> is available" opens the Updates page
  -> the administrator updates there (packages.update, a job in the root job host)
  -> PackageKit invokes apt or dnf
  -> the package manager verifies the signed repository and package
  -> the package's own post-install step restarts linuxio.target
  -> the job, outside linuxio.target, runs to its end and is recorded
  -> the browser signs in again and finds the job in the task tray
```

- [x] Keep all privileged package operations in `linuxio-bridge`.
- [x] Delete `control.app_update`, its operation
      (the `appupdate` bridge handler), the update dialog
      (`AppUpdateDialog.tsx`) and the code only they use; arbitrary download
      URLs and version selection go with them.
- [x] Reduce `AppUpdateBanner.tsx` to one line that names the new version and
      has a button to the Updates page.
- [x] Export a narrow `FindUpdateByName` from the existing packages handler
      for the banner. No `UpdateByName`: the update itself is the Updates
      page's own `packages.update`.
- [x] Replace the GitHub release check with that PackageKit lookup, in the
      bridge's `control.version`. The webserver's `/api/update-info` and
      `/api/update-status` are deleted, not ported: the webserver is
      unprivileged and does not reach PackageKit.
- [x] Have the authenticated frontend query `control.version` through the
      bridge for update availability.
- [x] Keep `/api/version` only for detecting that the restarted webserver is
      running.
- [x] Delete the `/run/linuxio/update-status.json` record
      (the `updatestatus` package) and its reader in the webserver: only
      the app-update operation wrote it. The job's own record carries the
      result.
- [x] The bridge restarts nothing after an update. The package does it (see
      "Build native DEB and RPM packages"), and a failed or cancelled
      transaction leaves the package as it was, so no restart follows.
- [x] Keep `JobsUpdateBanner.tsx`: it tells signed-in users that an update
      waits for running jobs, whichever way the update arrived.
- [x] Regenerate API contracts with `make generate`.

## 5. Delete obsolete installation and update machinery

- [ ] Delete `packaging/scripts/install-linuxio-binaries.sh`.
- [ ] Do not carry its `rewrite_saved_unit_commands` into the packages.
      Decided 2026-11-21. Its replacements fix command lines that releases
      up to v0.53 wrote into saved unit files, and no such file reaches a
      package install, which starts on a host with no script install. The
      `AGENTS.md` rule that names it then says that a release which changes
      a command line LinuxIO writes on save rewrites the saved copies in the
      package's post-install step.
- [ ] Delete `packaging/scripts/install-dependencies.sh`.
- [x] Delete `packaging/scripts/localinstall.sh`.
- [ ] Delete `packaging/scripts/uninstall.sh`.
- [ ] Delete or replace their installer fixture tests.
- [ ] Keep `packaging/scripts/tests/systemd-indexer-smoke.sh`: it tests the
      indexer under a real systemd (socket activation, idle exit, the index
      timer), not the installer. Replace its `localinstall.sh` and
      `uninstall.sh` calls with a package install and a package removal, and
      drop its last check, that removal leaves no indexer state, or move it
      to a purge: removal keeps `/var/lib/linuxio`.
- [x] Delete the app updater with its GitHub download, checksum, and
      transient installer-unit code.
- [ ] Remove raw binaries, the tarball, installer scripts, and installer
      checksums from GitHub release assets.
- [ ] Keep unrelated runtime and development scripts, including the changelog
      generator and issue-banner helper.
- [ ] Replace README `curl | sudo bash` instructions with native repository and
      package-manager commands.
- [ ] Update development, systemd architecture, storage, installation, update,
      removal, and release documentation.

GitHub releases should contain only the DEB, RPM, checksums, and release notes.

## 6. Replace the release workflow

The release sequence must be:

```text
one compile
  -> DEB assembled, RPM assembled
  -> package tests
  -> signed repository assembly
  -> Pages deployment
  -> GitHub release publish
```

- [ ] Make the workflow call repository Make targets instead of invoking Go,
      Node, or their linters/test runners directly.
- [x] Add `make test-packages-quiet`, `make repo-apt`, and `make repo-rpm`
      (`mk/package.mk`; `REPO_DIR`, `REPO_SIGNING_KEY`, `REPO_BASE_URL`).
- [ ] Compile once in the pinned compile image, then assemble the DEB there
      and the RPM in the pinned Fedora image (see "Pinned build
      environment").
- [ ] Test both artifacts before importing signing credentials.
- [ ] Sign only immutable release-tag artifacts.
- [ ] Assemble and validate both repositories before deployment.
- [ ] Deploy the complete repository tree as one Pages artifact.
- [ ] Publish the GitHub release only after repository deployment succeeds.
- [ ] Make either packaging platform's failure block the entire release.

## Release gates

- [ ] Fresh install, upgrade, removal, and configuration-preservation tests on
      Debian stable and Ubuntu LTS.
- [ ] The same tests on the current and previous Fedora releases.
- [ ] Fedora runtime smoke test with SELinux enforcing.
- [ ] On Fedora with SELinux enforcing, update A to B where B carries a
      changed SELinux module: after the update sign-in works, `semodule -E
      linuxio` gives B's module, `linuxio-auth` and the auth socket have
      their types and no denial is logged, and all of it holds after a
      reboot. That packages deliver a changed module is expected from how
      `dnf` runs package scripts and has not been seen; this gate is where
      it is seen.
      - Seen 2026-11-21 on Fedora 45 in the test VM, B installed on the
        Updates page through PackageKit (`jobs-update.spec.ts`): B's module,
        `linuxio_auth_exec_t`, sign-in and no denial, after the update and
        after a reboot. The auth socket's type is not checked by the spec.
- [ ] Package contents and ownership match the intended common layout.
- [ ] The package exposes only `/usr/bin/linuxio`; all private executables are
      under `/usr/libexec/linuxio` and are not required in `$PATH`.
- [ ] `linuxio version` and `linuxio --version` report the package/build
      version without executing another LinuxIO binary.
- [ ] The DEB and RPM contain the `linuxio(8)` man page.
- [ ] No package-owned files exist under `/usr/local`.
- [ ] No package-owned base units exist under `/etc/systemd/system`.
- [ ] `make localinstall` installs the package through APT or DNF and does not
      copy package-owned files directly.
- [ ] Install version A from each local signed repository, publish version B,
      and update A to B from the Updates page.
- [ ] Verify on DEB and RPM that the banner names B and opens the Updates
      page, that the package restarts LinuxIO, that the update job runs to
      its end across that restart, and that after signing in again the
      browser shows the job finished and the host runs B.
      - Both seen 2026-11-21 on Ubuntu 26.04 and Fedora 45 in the test VMs
        (`jobs-update.spec.ts`), with jobs of an older protocol keeping B
        waiting; not on Debian or the previous Fedora, which have no test
        VM.
- [ ] Update A to B from a shell (`apt`, `dnf`) and through the
      distribution's unattended upgrade, with a job running: LinuxIO runs B
      afterwards with no `linuxio restart`, the job goes on, and a LinuxIO
      that was stopped stays stopped.
      - From a shell, seen 2026-11-21 on Ubuntu 26.04 and Fedora 45
        (`package-repository.spec.ts`, `jobs-phase5a.spec.ts`); the
        unattended upgrade is not run.
- [ ] Verify failed and cancelled updates do not restart LinuxIO.
- [ ] Pass the two login banner checks in "Login banner".
- [ ] Verify APT and DNF reject tampered metadata and packages.
      - Seen 2026-11-21 (`package-repository.spec.ts`): apt fails with Hash
        Sum mismatch on a changed `Packages` and a changed DEB; dnf fails on
        a changed RPM (checksum), and on a changed `repomd.xml` reports the
        bad signature and goes on without the repository, exiting 0 with
        nothing to upgrade. `linuxio.repo` does not set
        `skip_if_unavailable=False`, which would make it exit 1 but would
        stop every dnf transaction on the host while the repository is
        unreachable.
- [ ] Verify package removal preserves `/var/lib/linuxio`.
- [ ] Run `shellcheck` and `shfmt -d` on all relevant remaining shell files.
- [ ] Run `actionlint` after workflow changes.
- [ ] Run `make test-packages-quiet`.
- [ ] Run `make test-frontend-browser-quiet` for the restart/reconnect flow.
- [ ] Finish with `make test-quiet`.

## Completion criteria

This work is complete only when a release publishes installable, signed DEB
and RPM repositories together; fresh Debian/Ubuntu and Fedora hosts can install
LinuxIO using only their native package managers; and a privileged logged-in
user can update LinuxIO through PackageKit without downloading or executing an
installer script.
