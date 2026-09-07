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

## Required changes

### 1. Keep the CLI name

- [ ] Keep the current build output in `Makefile:21`:

  ```make
  cli_binary := $(bin_dir)/linuxio
  ```

- [ ] Install that file as `/usr/bin/linuxio` from the package-root target.
- [ ] Do not build `linuxio-cli` or add a compatibility symlink.

### 2. Separate public and private paths

- [ ] Replace `BinDir` in `backend/common/version/version.go:5` with the
      private executable location:

  ```go
  const LibexecDir = "/usr/libexec/linuxio"
  ```

- [ ] Do not add a `CLIPath` constant unless code genuinely needs to invoke
      the CLI.
- [ ] Use `LibexecDir` when the webserver spawns the bridge in
      `backend/webserver/bridge/bridge.go:57`.
- [ ] Update the Docker update worker path in
      `backend/bridge/handlers/docker/auto_update_native.go:27`.
- [ ] Update the authentication bridge path in
      `backend/auth/linuxio-auth.c:64`.
- [ ] Update every package-owned systemd `ExecStart` entry.
- [ ] Update the verbose systemd drop-ins written by
      `backend/cli/main.go:653`.

### 3. Simplify `linuxio version`

Native packaging guarantees that every file in the package has one version.

- [ ] Remove `versionExecCommand` and the four private-binary subprocess
      probes from `backend/cli/main.go:98`.
- [ ] Remove `version --self`.
- [ ] Make `linuxio version` print the package/build version.
- [ ] Support the conventional `linuxio --version` with the same result.
- [ ] Remove component-version probing from
      `backend/webserver/auth/version.go:138`; it must not depend on all
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

- [x] Add `packaging/man/linuxio.8` in plain roff. Do not add a documentation
      generator.
- [x] Document the synopsis, commands, required privileges, files, systemd
      units, and examples.
- [ ] Install it in the DEB through `debian/linuxio.manpages`.
- [ ] Include `%{_mandir}/man8/linuxio.8*` in the RPM. Let debhelper and RPM
      packaging handle compression.

### 6. Update tests and documentation

- [ ] Remove component-probing and `--self` tests.
- [ ] Test both `linuxio version` and `linuxio --version`.
- [ ] Update systemd path assertions.
- [ ] Update `docs/process-systemd-architecture.md:31`.
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
- [ ] Delete `packaging/scripts/localinstall.sh` after the package-based target
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
