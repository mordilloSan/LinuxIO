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

- [ ] Ship one package named `linuxio` containing all six binaries and system
      integration.
- [ ] Publish DEB and RPM together. A failure in either format blocks the
      release.
- [ ] Initially support `amd64`/`x86_64`, matching the current release.
- [ ] Support Debian stable, Ubuntu LTS, and the current and previous Fedora
      releases as release-gated platforms.
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

## Required changes

### 1. Keep the CLI name

- [ ] Keep the current build output in `mk/go.mk:5`:

  ```make
  cli_binary := $(bin_dir)/linuxio
  ```

- [ ] Install that file as `/usr/bin/linuxio` from the package-root target.
- [ ] Do not build `linuxio-cli` or add a compatibility symlink.

### 2. Separate public and private paths

- [ ] Replace `BinDir` in `backend/common/version/paths.go` with the
      private executable location:

  ```go
  const LibexecDir = "/usr/libexec/linuxio"
  ```

- [ ] Do not add a `CLIPath` constant unless code genuinely needs to invoke
      the CLI.
- [ ] Use `LibexecDir` when the webserver spawns the bridge in
      `backend/webserver/bridge/bridge.go:59`.
- [ ] Update the Docker updater path in
      `backend/common/dockerupdate/sandbox.go:11` (`RunnerPath`).
- [ ] Update the authentication bridge path in
      `backend/auth/linuxio-auth.c:64`.
- [ ] Update every package-owned systemd `ExecStart` entry.
- [ ] Update the verbose systemd drop-ins written by
      `backend/cli/verbose.go:70`.

### 3. Simplify `linuxio version`

Native packaging guarantees that every file in the package has one version.

- [x] Remove `versionExecCommand` and the private-binary subprocess
      probes from `backend/cli/version.go`. Done with `linuxio-worker`: the
      CLI and the webserver read the installer's record,
      `/usr/share/linuxio/components`
      ([Versions](../process-systemd-architecture.md#binaries)).
- [ ] Remove `version --self`.
- [ ] Make `linuxio version` print the package/build version.
- [x] Support the conventional `linuxio --version` with the same result.
- [ ] Remove component-version probing from
      `backend/webserver/appupdate/version.go:129`; it must not depend on all
      LinuxIO binaries sharing one executable search path.

### 4. Clarify help text

- [ ] Use this simpler command summary:

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

- [ ] Add `packaging/man/linuxio.8` in plain roff (the earlier draft was deleted). Do not add a documentation
      generator.
- [ ] Document the synopsis, commands, required privileges, files, systemd
      units, and examples.
- [ ] Install it in the DEB through `debian/linuxio.manpages`.
- [ ] Include `%{_mandir}/man8/linuxio.8*` in the RPM. Let debhelper and RPM
      packaging handle compression.

### 6. Update tests and documentation

- [ ] Remove component-probing and `--self` tests.
- [x] Test both `linuxio version` and `linuxio --version`.
- [ ] Update systemd path assertions.
- [ ] Update the binary-location paragraph in `docs/process-systemd-architecture.md`.
- [ ] Add the man page to package-content tests.

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
  -> restart linuxio.target after the package transaction
```

- [ ] Make package construction consume the exact outputs from `fastbuild`, so
      all installed binaries carry the same build version.
- [ ] Run the build and package construction unprivileged; elevate only the
      package-manager install and the post-transaction service restart.
- [ ] Keep `make dev` as the frontend hot-reload workflow against the installed
      systemd backend.
- [ ] Use `make localinstall` for the first development install and after
      backend changes. Individual build targets only build; they do not
      install.
- [ ] Delete `packaging/scripts/localinstall.sh` (today a thin wrapper over
      `install-linuxio-binaries.sh --local`) after the package-based target
      replaces it.
- [ ] Remove `make reinstall` and the custom uninstall target. Native package
      managers own reinstall and removal.
- [ ] Implement package tooling and the package-based `make localinstall`
      before simplifying version reporting; the one-version assumption is
      valid only after every supported install path installs one complete
      package.

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
/usr/lib/systemd/system/linuxio*
/usr/lib/tmpfiles.d/linuxio.conf
/usr/share/linuxio/
/etc/linuxio/
/etc/pam.d/linuxio
/var/lib/linuxio
```

- [ ] Add one shared `make package-root DESTDIR=...` target that installs the
      common package tree.
- [ ] Replace compiled `/usr/local/bin` paths with `/usr/bin` for the CLI and
      `/usr/libexec/linuxio` for private executables.
- [ ] Update the C authentication helper's bridge path.
- [ ] Update package-owned systemd units to execute binaries from
      `/usr/libexec/linuxio`.
- [ ] Keep the five programs systemd runs as root (`linuxio-worker`,
      `linuxio-docker-update`, `linuxio-auth`, `linuxio-indexer`,
      `linuxio-monitoring`) at mode 0700, owned by root, as the installer's
      `binary_mode` sets them today; the other programs stay 0755. Decided
      2026-11-21. Cockpit installs every helper 0755; LinuxIO is stricter on
      purpose, so no other local user can run a private word or flag.
      - RPM: `%attr(0700,root,root)` on each of the five.
      - DEB: `dh_fixperms` resets every program to 0755, so the rules set
        0700 again after it.
      - The package-content test checks the nine modes.
      - The `AGENTS.md` rule then names the packages in place of
        `binary_mode` in the installer.
      - What each program answers to `--help` and `--version`, and to a
        user who is not root, is one rule since v0.55
        ([Binary invocation map](../process-systemd-architecture.md#binary-invocation-map)):
        a root-only program answers "Permission denied" to another user.
- [ ] Ship the SELinux module (`packaging/selinux/linuxio.cil`) in the RPM
      and load it from the package, with its file context changed to the
      installed path of `linuxio-auth`, `/usr/libexec/linuxio/linuxio-auth`
      ([Process & Systemd Architecture](../process-systemd-architecture.md#selinux)).
- [ ] Install package-owned units under `/usr/lib/systemd/system`.
- [ ] Continue using `/etc/systemd/system` only for administrator or
      runtime-generated drop-ins.
- [ ] Treat `/etc/linuxio`, `/etc/pam.d/linuxio`, and other administrator
      configuration as preserved package configuration.
- [ ] Carry the needrestart override that the script installer installs
      today (`packaging/etc/needrestart/conf.d/linuxio.conf` as
      `/etc/needrestart/conf.d/linuxio.conf`, only where `/etc/needrestart`
      exists): a package update runs through dpkg, after which Ubuntu's
      needrestart would restart the job hosts and their jobs
      ([Jobs](../jobs.md#updates)).
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
- [ ] Ship a second, untouched copy of the hook under
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
- [ ] Keep `/var/lib/linuxio` across package removal.
- [ ] Install licenses using each distribution's native locations:
  - DEB: `/usr/share/doc/linuxio/copyright` and third-party notices.
  - RPM: `%license LICENSE` and `%doc THIRD_PARTY_NOTICES.md`.
- [ ] Stop rewriting the package-owned socket unit during installation. Use
      port `8090` by default and document a systemd socket drop-in for changing
      it.
- [ ] Remove installer behavior that adds the invoking user to journal groups.
- [ ] Add a minimal pre-install guard for exact legacy sentinels such as
      `/usr/local/bin/linuxio` or `/etc/systemd/system/linuxio.target`.
      Refuse the install with a clean-install message; do not remove them.

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

- [ ] Add native Debian packaging metadata using debhelper/dpkg tooling.
- [ ] Add `packaging/rpm/linuxio.spec` using `rpmbuild`.
- [ ] Make both formats consume the shared package-root target instead of
      maintaining two file-copy implementations.
- [ ] Build the DEB in the oldest supported Debian baseline.
- [ ] Build the RPM in the oldest supported Fedora baseline; do not package
      Ubuntu-built CGO/PAM binaries for Fedora.
- [ ] Let native tooling derive shared-library requirements.
- [ ] Declare PackageKit, PAM, polkit, systemd, and other runtime dependencies
      with the correct distribution-specific package names.
- [ ] Mark Debian configuration as conffiles and RPM configuration as
      `%config(noreplace)`.
- [ ] On first install, create runtime directories, reload systemd, enable and
      start `linuxio.target`, and enable the indexer timer.
- [ ] On upgrade, reload systemd and restart `linuxio.target` if it is
      running: `deb-systemd-invoke try-restart` in the DEB's post-install
      script and `%systemd_postun_with_restart` in the RPM, as Cockpit's
      packages do for `cockpit.service`. A stopped LinuxIO stays stopped.
      - The job hosts are not part of `linuxio.target`, so running jobs go
        on, among them the `packages.update` job that started the upgrade
        ([Jobs](../jobs.md#updates)).
      - `linuxio-auth@.service` is `PartOf=linuxio.target`, so every session
        ends and its user signs in again.
      - The restart comes when LinuxIO's own package is done, which in a
        larger upgrade is before the transaction ends.
- [ ] On removal, stop and disable services while preserving application data.
- [ ] Add `make package-deb` and `make package-rpm`.
- [ ] Replace the current `make localinstall` implementation with the
      package-based development workflow above.

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

- [ ] Generate `Packages` and `Release` metadata with `apt-ftparchive`.
- [ ] Publish SHA256 by-hash metadata paths.
- [ ] Sign `Release` as both `InRelease` and `Release.gpg`.
- [ ] Publish `linuxio.sources` with an embedded `Signed-By` public key.
- [ ] Do not use `trusted=yes` or otherwise permit unsigned repositories.

### RPM

Publish:

```text
rpm/
  linuxio.repo
  RPM-GPG-KEY-linuxio
  stable/x86_64/linuxio-<version>-1.x86_64.rpm
  stable/x86_64/repodata/
```

- [ ] Sign every RPM with `rpmsign`.
- [ ] Generate repository metadata with `createrepo_c`.
- [ ] Sign `repodata/repomd.xml`.
- [ ] Set `gpgcheck=1` and `repo_gpgcheck=1` in `linuxio.repo`.

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

- [ ] Keep all privileged package operations in `linuxio-bridge`.
- [ ] Delete `control.app_update`, its operation
      (`backend/bridge/handlers/appupdate`), the update dialog
      (`AppUpdateDialog.tsx`) and the code only they use; arbitrary download
      URLs and version selection go with them.
- [ ] Reduce `AppUpdateBanner.tsx` to one line that names the new version and
      has a button to the Updates page.
- [ ] Export a narrow `FindUpdateByName` from the existing packages handler
      for the banner. No `UpdateByName`: the update itself is the Updates
      page's own `packages.update`.
- [ ] Replace GitHub release checks in both the bridge app-update handler and
      the webserver update-info implementation with that PackageKit lookup.
- [ ] Have the authenticated frontend query `control.version` through the
      bridge for update availability.
- [ ] Keep `/api/version` only for detecting that the restarted webserver is
      running.
- [ ] Delete the `/run/linuxio/update-status.json` record
      (`backend/common/updatestatus`) and its reader in the webserver: only
      the app-update operation wrote it. The job's own record carries the
      result.
- [ ] The bridge restarts nothing after an update. The package does it (see
      "Build native DEB and RPM packages"), and a failed or cancelled
      transaction leaves the package as it was, so no restart follows.
- [ ] Keep `JobsUpdateBanner.tsx`: it tells signed-in users that an update
      waits for running jobs, whichever way the update arrived.
- [ ] Regenerate API contracts with `make generate`.

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
- [ ] Delete `packaging/scripts/localinstall.sh`.
- [ ] Delete `packaging/scripts/uninstall.sh`.
- [ ] Delete or replace their installer fixture tests.
- [ ] Keep `packaging/scripts/tests/systemd-indexer-smoke.sh`: it tests the
      indexer under a real systemd (socket activation, idle exit, the index
      timer), not the installer. Replace its `localinstall.sh` and
      `uninstall.sh` calls with a package install and a package removal, and
      drop its last check, that removal leaves no indexer state, or move it
      to a purge: removal keeps `/var/lib/linuxio`.
- [ ] Delete the app updater with its GitHub download, checksum, and
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
DEB native build --\
                   +-> package tests -> signed repository assembly
RPM Fedora build --/                         |
                                              v
                                      Pages deployment
                                              |
                                              v
                                      GitHub release publish
```

- [ ] Make the workflow call repository Make targets instead of invoking Go,
      Node, or their linters/test runners directly.
- [ ] Add `make test-packages-quiet`, `make repo-apt`, and `make repo-rpm`.
- [ ] Build DEB and RPM in their native baseline environments.
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
- [ ] Update A to B from a shell (`apt`, `dnf`) and through the
      distribution's unattended upgrade, with a job running: LinuxIO runs B
      afterwards with no `linuxio restart`, the job goes on, and a LinuxIO
      that was stopped stays stopped.
- [ ] Verify failed and cancelled updates do not restart LinuxIO.
- [ ] Pass the two login banner checks in "Login banner".
- [ ] Verify APT and DNF reject tampered metadata and packages.
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
