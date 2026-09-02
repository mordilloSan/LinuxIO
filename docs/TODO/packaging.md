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
- [ ] Host both repositories below `https://packages.linuxio.io/`, initially
      as a static GitHub Pages deployment.
- [ ] Use PackageKit for update discovery and installation; never download or
      execute an installer script from the application.
- [ ] Do not add legacy migration, compatibility symlinks, source packages,
      extra release channels, or additional architectures in this work.

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
- [ ] Install package-owned units under `/usr/lib/systemd/system`.
- [ ] Continue using `/etc/systemd/system` only for administrator or
      runtime-generated drop-ins.
- [ ] Treat `/etc/linuxio`, `/etc/pam.d/linuxio`, and other administrator
      configuration as preserved package configuration.
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
- [ ] On upgrade, install files and reload systemd without restarting LinuxIO
      inside the package transaction.
- [ ] On removal, stop and disable services while preserving application data.
- [ ] Add `make package-deb` and `make package-rpm`.
- [ ] Remove `make localinstall`, `make reinstall`, and the custom uninstall
      target. Development installs use `apt install ./dist/*.deb` or
      `dnf install ./dist/*.rpm`.

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
- [ ] Configure `packages.linuxio.io` so hosting can move away from GitHub
      Pages later without changing installed clients.
- [ ] Provide documented commands that install the static `.sources` or
      `.repo` file and then invoke `apt install linuxio` or
      `dnf install linuxio`.
- [ ] Do not create an executable bootstrap installer or a repository bootstrap
      package.

## 4. Replace the application updater with PackageKit

The successful update path must be:

```text
Browser
  -> control.app_update
  -> linuxio-bridge asks PackageKit for updates
  -> bridge selects the exact package name "linuxio"
  -> PackageKit invokes apt or dnf
  -> the package manager verifies the signed repository and package
  -> the task and durable status are marked successful
  -> LinuxIO restarts after the task completes
  -> the browser reconnects
```

- [ ] Keep all privileged package operations in `linuxio-bridge`.
- [ ] Export narrow `FindUpdateByName` and `UpdateByName` functions from the
      existing packages handler.
- [ ] Reuse the existing PackageKit operation gate, transactions, progress,
      cancellation, and error handling.
- [ ] Make the server select package `linuxio`; remove arbitrary download URLs
      and version selection from the update request.
- [ ] Replace GitHub release checks in both the bridge app-update handler and
      the webserver update-info implementation.
- [ ] Have the authenticated frontend query `control.version` through the
      bridge for update availability.
- [ ] Keep `/api/version` only for detecting that the restarted webserver is
      running.
- [ ] Preserve the durable `/run/linuxio/update-status.json` projection.
- [ ] After PackageKit reports success, complete the task before restarting
      `linuxio.target`.
- [ ] If the generic package-update operation includes package `linuxio`, use
      the same post-task restart path.
- [ ] Do not restart LinuxIO after a failed or cancelled PackageKit operation.
- [ ] Regenerate API contracts with `make generate` if the update request or
      response changes.

## 5. Delete obsolete installation and update machinery

- [ ] Delete `packaging/scripts/install-linuxio-binaries.sh`.
- [ ] Delete `packaging/scripts/install-dependencies.sh`.
- [ ] Delete `packaging/scripts/localinstall.sh`.
- [ ] Delete `packaging/scripts/uninstall.sh`.
- [ ] Delete or replace their installer fixture tests.
- [ ] Delete GitHub download, checksum, and transient installer-unit code from
      the app updater.
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
- [ ] No package-owned files exist under `/usr/local`.
- [ ] No package-owned base units exist under `/etc/systemd/system`.
- [ ] Install version A from each local signed repository, publish version B,
      and update A to B through PackageKit.
- [ ] Verify a logged-in browser observes progress, survives the expected
      service restart, reconnects, and reports success on DEB and RPM.
- [ ] Verify failed and cancelled updates do not restart LinuxIO.
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
