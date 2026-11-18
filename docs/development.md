# Development targets and overrides

The repository Makefile is the supported entry point for setup, checks, tests,
builds, and packaging. `Makefile` owns the public variables, validation and
`.PHONY`; targets live in `mk/{toolchain,go,frontend,auth,quality,install,vm}.mk`
and the longer shell programs behind them in `scripts/`. Prefer focused targets
when working on one area:

```sh
make check-frontend
make check-backend
make test-frontend-browser
```

Primary user-facing validation targets have a `-quiet` form. It prints a short
summary and saves the complete output under `quiet_log_dir` (default:
`.cache/test-logs`):

```sh
make check-backend-quiet
make test-quiet quiet_failure_lines=80
```

`test-updater-quiet` remains attached to the foreground terminal so its required
`sudo` invocation can prompt for a password; the test output is still captured
in the quiet log.

Do not invoke `go test`, Vitest, TypeScript, linters, or formatters directly;
use the corresponding Make target.

## Prerequisites

- Go 1.21 or newer on PATH (Go fetches the exact version `go.mod` pins)
- Node.js 26 or newer
- GNU Make

For Debian/Ubuntu, install the development libraries and optional analysis/test
tools with:

```sh
sudo apt install -y build-essential libpam0g-dev libsystemd-dev cppcheck \
  clang-tools clang-tidy bear libpam-wrapper libnss-wrapper libuid-wrapper
```

For Fedora/RHEL/CentOS, use:

```sh
sudo dnf install -y gcc pam-devel systemd-devel cppcheck clang-tools-extra \
  clang-tidy bear pam_wrapper nss_wrapper uid_wrapper
```

The static-analysis tools are only needed by `analyze-auth`. The wrapper
libraries are needed by `test-auth-pam`; that target skips when they are not
installed.

The runtime bootstrap script installs only PAM, PolicyKit, and PackageKit.
Docker and the other optional host capabilities are installed after sign-in
from LinuxIO Capability Manager, not from the bootstrap script; Docker's entry
runs Docker's convenience script and streams its output.

Release installation deliberately combines immutable release binaries with
current-`main` packaging assets (systemd units, PAM and application
configuration, MOTD, and helper files). This is the recovery path for fixing
installer or service-definition bugs in already-published releases. Keep those
assets compatible with supported historical binaries, or add a clear minimum
binary-version failure before making an incompatible packaging change.

## Initial setup

```sh
git clone https://github.com/mordilloSan/LinuxIO
cd LinuxIO
make setup
make build
make localinstall
```

`make setup` installs Node through nvm, checks that Go is on PATH and prints the
`go.mod` toolchain it resolves to (downloading it on first use), installs the
pinned Go tools (golangci-lint, deadcode, modernize, govulncheck) and the
frontend dependencies; `make test` and `make build` run it themselves.
`make setup-frontend` installs only the frontend dependencies. Two things stay
separate: `make setup-frontend-browser` downloads Playwright Chromium for the
browser tests, and `make check-c-build-deps` names the missing C compiler,
`libsystemd-dev` or `libpam-dev` packages without installing them.

The webserver embeds `backend/webserver/web/frontend/`, which is gitignored and
holds the Vite build. Without it no Go package that imports the webserver
compiles, so `make dev-prep` creates empty placeholder files there when they
are missing. The Go check targets (`golint`, `test-go`, `test-backend`,
`check-backend`, `deadcode`, `test`) and `make dev` run it themselves. The
build targets do not, so a release binary never embeds a placeholder beside a
real Vite build; run `make dev-prep` or `make build-vite` before
`make fastbuild` on a fresh checkout or after `make clean`.

For frontend hot reload, start the backend through systemd and run `make dev`.
Vite serves on port `3000` by default and proxies backend requests to port
`8090`. Go/backend or bridge changes can be rebuilt with `make fastbuild` and
installed with `make localinstall`.

Built binaries land in `build/` (`BIN_DIR`). `make build-go-binaries` builds
every Go binary (embedding the bridge SHA-256 in the webserver),
`make build-binaries` adds the C authentication helper, and
`make print-binaries` lists the shipped-binary names that the release workflow
and installer tests consume. `make localinstall` runs
`packaging/scripts/localinstall.sh`, a wrapper for
`install-linuxio-binaries.sh --local --build-dir build/`. Only
`common/version.Version` is stamped into binaries at link time.

## Test VM

`mk/vm.mk` drives a disposable Ubuntu 26.04 VM (or Fedora 45, with
`DISTRO=fedora`, below) for checks that need root, a
real kernel, systemd or Docker. `packaging/vm/linuxio-vm.sh` does the work on
`qemu:///system`, so it needs `virsh`, membership in the `libvirt` group, the
`default` NAT network (`192.168.122.1`), `curl`, `openssl` and `python3`, but no
`sudo`. It touches only the `linuxio-e2e*` domains and their `linuxio-e2e*`
volumes in the `default` pool. Its state (the cloud image cache, the SSH key
and the login) lives in `~/.cache/linuxio-vm`.

While it runs, a VM takes up to 4 GiB of RAM and 4 vCPU. Its disks are
sparse qcow2 files: a 25 GiB system disk and four 8 GiB data disks
(`vdb`–`vde`) for storage tests.

`make test-vm-browser` is the whole browser run in one command. It builds
LinuxIO and the update build the update checks install (`vm-update-build`,
[Jobs](./jobs.md#testing)), counts how many VMs the host has room for,
creates the ones that are missing, reverts each to `clean`, installs the build
in each and runs the specs on all of them at once. Then it reverts the first
VM to `clean` again and runs `jobs-update.spec.ts` alone on it, the
Playwright project `release-update`, since that file installs an older release
and leaves the VM changed; a run narrowed with `PLAYWRIGHT_ARGS` skips that
stage. At the end it shuts the VMs down. `make test-vm-backend` is
the same for the root suites, without the install. A VM needs 4 GiB of free
memory (`MemAvailable`, after 1.5 GiB kept for the host and its browsers) and
4 CPUs, so a host with 12 CPUs and 14 GiB free runs three; four is the most
either picks, since a fifth would not shorten the run, and `test-vm-backend`
picks no more than it has suites. `VMS=2` sets the number by hand. Both work
from a host with no VM yet; the first run waits for each VM's first boot.

The VMs are numbered from 1: `linuxio-e2e`, then `linuxio-e2e-2` with its
`linuxio-e2e-2-*` volumes and `~/.cache/linuxio-vm-2`, up to 9. They share
only the image cache. `VM=2` on a target names one of them. `vm-create`,
`vm-destroy`, `vm-ssh` and the `vm-test-*` targets act on one VM, the first
unless `VM` names another; `vm-install`, `vm-reset`, `vm-update`, `vm-stop`
and `test-vm-browser-rerun` act on every VM that exists, side by side, or on
the one `VM` names. After a `test-vm-browser` that used fewer VMs than exist,
pass the same `VMS` to `test-vm-browser-rerun`: the others do not have the
build.

The specs wait on reboots, throttled disks and timers and hardly use the CPU,
so more CPUs for a VM gain nothing and more VMs do. Playwright runs one worker
for each VM; a worker takes the next spec file and runs it against its own
VM, so a spec file starts from what `vm-install` left and never from what
another file did. The longest file sets the floor: on a host with 12 CPUs
the specs took 42 minutes on one VM and 15 on three.

What a run prints says which VM did what. A command run on several VMs
(`prepare`, `reset`, `install`, `stop` and so on) shows one row per VM with
its steps as they end, colour-coded `vm1`, `vm2`, `vm3`; each VM's own
output goes to `.cache/test-logs/vm-N-COMMAND.log`, and the end of it is
shown when that VM fails. On a terminal the rows go on from one command to
the next of the same target, so `test-vm-backend` keeps one row per VM from
`ready` to `stop`; a failure, or anything else printed, starts new rows
below. A row wider than the terminal drops its oldest steps for `…`, and the
cursor is hidden while the rows are redrawn. The specs' reporter
(`frontend/config/vm-reporter.ts`) puts the VM on every result, and prints
what a spec writes under its own test once that test ends, so lines of two
VMs never mix. On a terminal a board under the results shows each VM's
test, with the number its result line will carry, its file and how long it
has run; in a log or a pipe the results come alone. The
run ends with a table for each VM (tests, passed, failed, skipped, when it
finished), a timeline of which file ran on which VM, and each failure with
its error, its code and its trace. Every line, without colour, is also in
`.cache/test-logs/test-vm-browser.log`.

Only one VM target runs at a time. A second one is refused while the first
holds `~/.cache/linuxio-vm/lock`, since it would revert, reboot or stop a VM
under it. `make vm-ssh VM_KEEP=1` is not held back: it stops nothing.

The full runs are long: the browser specs take 15 minutes on three VMs and
42 on one, the root suites about 6 on three, and either holds the lock, so
no other VM target can run meanwhile. An agent therefore runs only the suite
(`SUITES=...`) or the spec file (`PLAYWRIGHT_ARGS=...`) its change needs and
leaves the full runs to the owner (`AGENTS.md`).

`VM_RELEASE=noble` on a `vm-*` target selects a second VM on Ubuntu 24.04
(kernel 6.8): the domain `linuxio-e2e-noble`, its `linuxio-e2e-noble-*`
volumes and `~/.cache/linuxio-vm-noble`, beside the first one. It is there for
`make vm-test-btrfs-integration VM_RELEASE=noble`, the root btrfs tests on a
kernel before 6.19, where a signal does not abort a scrub or a replace. The
test program is built without cgo, so it runs on the older glibc. The browser
specs run there too (`make vm-install VM_RELEASE=noble`, then
`make test-vm-browser-rerun VM_RELEASE=noble PLAYWRIGHT_ARGS=...`): sign-in
and the scheduled-script specs were run on it.

`DISTRO=fedora` on a `vm-*` or `test-vm-*` target selects the Fedora VMs
instead of Ubuntu's: the domain `linuxio-e2e-fedora` (`linuxio-e2e-fedora-2`
and so on), its `linuxio-e2e-fedora-*` volumes and
`~/.cache/linuxio-vm-fedora`, beside the Ubuntu VMs, which keep their names
when `DISTRO` is not given. The lock is the same one, so one VM target runs
at a time whatever the distribution. What differs between distributions is a
profile at the top of the script: the cloud image, where its checksum is
published, the first-boot seed, the package manager the suites install with,
and the packages they install. Fedora's is:

- the Fedora Cloud Base image (`Fedora-Cloud-Base-Generic-45-1.4.x86_64.qcow2`)
  from `download.fedoraproject.org`, which sends it from a mirror, checked
  against the SHA-256 in the release's `CHECKSUM` file read from
  `dl.fedoraproject.org`. The script does not check the signature of either
  distribution's checksum file. A Fedora release's images do not change once
  it ships, so a new release is a new version in the profile.
- `packaging/vm/user-data-fedora.yaml.in`: the user in `wheel` with
  passwordless sudo, `moby-engine` (Fedora's Docker) started and enabled,
  `btrfs-progs`, `install-dependencies.sh`, every package upgraded, and
  `dnf-makecache.timer` off. SELinux stays enforcing. Fedora's root
  filesystem is btrfs.
- `dnf` where Ubuntu has `apt`, in `vm-update` and in the suites.

On Fedora the root suites `relay`, `filebrowser`, `userconfig` and `memory`
have been run (`docs/TODO/e2e-testing.md`, "Distributions", has the
results); the browser specs have not, since their package fixtures build
`apt` packages.

```sh
make test-vm-browser        # everything below that a browser run needs, in one go
make test-vm-backend        # the same for the root suites, shared out between the VMs
make vm-create              # download and check the image, first boot, snapshot "clean"
make vm-create VM=2         # one more VM
make vm-install             # make build-nocheck, then install this build in the VMs
make vm-ssh CMD='systemctl is-active linuxio.target'
make test-vm-browser-rerun  # the Playwright specs in frontend/src/test/vm, on the VMs as they are
make vm-test-btrfs-integration
make vm-stop                # shut the VMs down; what is installed stays
make vm-reset               # revert every disk to "clean" and stop the VMs
make vm-update              # upgrade the packages in "clean" and take the snapshot again
make vm-update-build        # this tree with jobs.Protocol one higher, for the update checks
make vm-destroy             # delete the first VM and its volumes; the cache stays
```

- `vm-create` downloads the cloud image (checked against the release's
  `SHA256SUMS`, Fedora's `CHECKSUM`), and boots it once with
  `packaging/vm/user-data.yaml.in` (`user-data-fedora.yaml.in` on Fedora).
  cloud-init reads its seed over HTTP from a server on
  `192.168.122.1:8399` (8400 for VM 2, and so on) that runs only during that
  boot. It creates the user
  `linuxio-e2e` with passwordless sudo and installs `docker.io`,
  `btrfs-progs` and the LinuxIO dependencies, and upgrades every package. The
  VM is then shut down and snapshotted as `clean`, without LinuxIO.
- Every target that needs the VM starts it if it is stopped and shuts it
  down when it ends, passed or failed (`vm-install`, `vm-ssh`, the
  `test-vm-*` and the `vm-test-*` ones), so no VM runs unused; what was
  installed and the journal stay on its disks, and the next target boots it
  again. `VM_KEEP=1` on a target leaves it up, to look at a failure while its
  processes still run, or to run several `vm-ssh` commands against one boot;
  `make vm-stop` then shuts it down. `vm-reset` leaves it stopped, without
  LinuxIO; run `vm-install` again after a reset.
- The seed switches `apt-daily.timer` and `apt-daily-upgrade.timer` off, so
  the VM never upgrades itself: an unattended upgrade that a power-cut check
  interrupts leaves dpkg broken for the specs after it. `vm-update` is how
  its packages are brought up to date instead. It reverts to `clean`, runs
  `apt-get dist-upgrade`, switches the two timers off (a VM created before
  the seed did so still has them on) and takes `clean` again; run it now and
  then, since a long list of waiting updates also slows the Updates page.
  The kernel is upgraded with the rest, so the kernel tests then run on the
  new one. On Fedora it runs `dnf upgrade`; Fedora upgrades nothing by
  itself.
- `make vm-ssh` without `CMD` opens a shell. The user can `sudo` without a
  password. Make expands `$` in `CMD`, so write `$$`.
- `test-vm-browser-rerun` runs the specs against the VMs as they are, which
  is what a rerun of one file wants (`PLAYWRIGHT_ARGS=jobs-phase4`). It signs in
  at `https://<vm-ip>:8090` as `linuxio-e2e`, with the VM's password in its
  state directory (`~/.cache/linuxio-vm/password` for the first).
  `PLAYWRIGHT_ARGS` applies to `test-vm-browser` too. Both run the
  Playwright project `specs` unless `PLAYWRIGHT_ARGS` names a project
  (`--project=release-update` runs `jobs-update.spec.ts`).
  The specs leave their schedules behind, and the Schedules table renders
  only the rows near what is visible, in the order of the definition files
  (by ID, so a new schedule lands anywhere). The helpers in `vm.ts`
  (`createSchedule`, `runNow`, `clickInScheduleRow`) scroll the table to the
  row, so leftover schedules do not hide a spec's own; a spec that reads
  `scheduleRow` directly must do the same.
  The specs reach the VM through `linuxio-vm.sh`: `ssh`, and for host events
  `reboot` (an orderly ACPI reboot), `power-cut` (`virsh destroy`, then
  `start`) and `off-for SECONDS` (an orderly shutdown, the domain off for
  that long, then a boot), each of which waits for the next boot. `jobs-phase2.spec.ts`
  installs Docker's Compose plugin, creates a btrfs pool on `vdb` and `vdc`
  and reboots the VM, so run `vm-reset` and `vm-install` before reusing it
  for other checks.
  `ui-actions.spec.ts` is not a jobs spec: a session without sudo, the
  "Open the real folder" action, a toast's page link on Docker, and the
  navbar's Reboot and Power Down. It creates and removes a user without sudo
  (`e2e-ui`), a folder `/srv/e2e-link` and two busybox containers, reboots the
  VM and powers it off (the rig's `ssh` starts a domain that is shut off).
- The `vm-test-*` targets are the root suites. Each builds its test binaries
  on the host into `.cache/vm`, static so they also run on the older glibc
  of `VM_RELEASE=noble`, and the script's `test` command copies them into
  the VM, prints its kernel version and runs them as root, with the test
  patterns of the matching host target. They run on the VM as it is.
- `test-vm-backend` runs every root suite: the four above, and six that
  have no target of their own. It uses as many VMs as the host has room for,
  one for each suite at most, creates the ones that are missing and reverts
  each to `clean` so the run starts from a known state; it installs no build,
  since the suites bring their own test binaries. The suites wait in one queue, longest first, and each VM takes the
  next when it is free: memory (about 6 minutes, with two reboots), btrfs (3), indexer (just
  under 2, most of it the indexer's idle timeout), docker (1½), packagekit
  (under ½, most of it the first boot), then relay,
  updater, userconfig, filebrowser and network, seconds each. On three VMs
  the run takes about as long as memory, plus the LinuxIO build the indexer
  suite needs. `SUITES='docker updater'` runs only those. With one VM
  (`VMS=1`) they run one after another on it, the later ones on what the
  earlier left. The first failure ends that VM's suites; the other VMs take
  what is left of the queue. With more than one VM each one's output is in
  `.cache/test-logs/vm-N-test.log`. The six without a target:
  - `indexer` is the disposable-host smoke test
    ([Indexer](./indexer.md#disposable-host-smoke-test)). It needs the
    LinuxIO build, so the target runs `build-nocheck` first; the suite
    copies the checkout's packaging and the build in, and the script
    installs it, checks socket activation, idle exit and the index timer, and
    uninstalls it.
  - `docker` is `test-docker-update-integration`: real Compose replacements,
    which pull alpine images from Docker Hub. Ubuntu's `docker.io` comes
    without the Compose plugin, so the suite installs `docker-compose-v2`
    (`docker-compose` on Fedora, beside `moby-engine`).
  - `updater` is `test-updater`, the install script's `--dry-run` under the
    updater's systemd sandbox. The test reads the script from
    `LINUXIO_INSTALL_SCRIPT`, a copy, since the VM has no checkout.
  - `network` adds dummy and macvlan links and a network namespace as root
    (`TestVirtualChildrenByParentSeesOtherNamespaces`).
  - `userconfig` has a root bridge create config files for another user's
    IDs (`TestOwnedConfigArtifactsCanTargetDifferentIDsWhenPrivileged`).
  - `packagekit` is `TestInstallByNameRefreshesAColdCacheIntegration`
    (`bridge/handlers/packages`): an install by name through the real
    PackageKit while its cache is cold, which must refresh the package lists
    once and install the package, GNU `hello`, which the suite removes before
    and after. With PackageKit stopped, the suite removes apt's lists and
    binary caches on Ubuntu, and on Fedora removes PackageKit's own cache and
    makes a dnf transaction outside it (`hello` installed and removed),
    after which its dnf backend finds nothing until a refresh; without that
    transaction it fetches a cache itself. Both backends answer an empty
    result, not PackageKit's package-not-found error. Run it with
    `make test-vm-backend SUITES=packagekit VMS=1`, and `DISTRO=fedora`.
- `vm-test-btrfs-integration` builds the `common/btrfs` and
  `bridge/handlers/storage` test binaries on the host,
  prints the VM's kernel version and runs the same tests as
  `test-btrfs-integration`, as root in the VM.
- `vm-test-memory-integration` builds the `bridge/handlers/memory` test binary
  on the host and runs `TestMemoryIntegration` as root in the VM: a switch to
  zram and back through systemd, the kernel and `/etc` (see
  [Memory overflow](./memory.md#tests)). With `ZRAM_PROVIDER=zram-generator`
  it installs `systemd-zram-generator` instead and refuses a VM that has
  zram-tools, which LinuxIO prefers: run `make vm-reset` first. The test needs
  a zram tool and a swap file in `/etc/fstab`, so the first run installs
  `zram-tools` with apt and disables the `zramswap.service` its package
  starts, and, when `/etc/fstab`
  has no `/swap.img`, creates a 1 GiB `/swap.img`, adds it to fstab and starts
  it. On Fedora, which has no zram-tools, `zram-generator` is the default
  and comes with the image, and so does a `zram0` that swaps from boot
  (`zram-generator-defaults`). Where such a vendor file
  (`/usr/lib/systemd/zram-generator.conf`) is there, the suite first puts
  that default back as a fresh image has it, undoing an earlier run: no
  `/swap.img` (taken away and out of fstab, made again after), no
  `/etc/systemd/zram-generator.conf`, and `zram0` started again from the
  vendor file. It runs `TestMemoryDefaultZramIntegration` there, and again
  once `/swap.img` swaps beside it. `TestMemoryIntegration` starts from disk,
  so the suite then switches the default off the way Fedora documents, which
  is also where LinuxIO's disk mode leaves it: an empty
  `/etc/systemd/zram-generator.conf`, a reload, then a stop of
  `dev-zram0.swap` and its setup service. A stop alone does not hold: while a
  configuration names the device, systemd starts it again as soon as it is
  reset. Its root is btrfs, so `/swap.img` is made with
  `btrfs filesystem mkswapfile` (a file without copy-on-write), which, unlike
  `mkswap`, leaves the file with its folder's SELinux type: the suite labels
  it `swapfile_t`, the type `swapon` is allowed to use. After
  `TestMemoryIntegration` the suite installs TuneD (`tuned`) and starts it,
  and runs `TestMemoryStatusWithoutRootIntegration` as a system user without
  sudo it creates, `linuxio-e2e-nosudo`, from a copy of the test binary in
  `/var/tmp`; TuneD is then stopped and disabled, and the user and the copy
  removed. It then runs `TestMemoryDrainIntegration` twice on the data disk
  `vdb`, made a swap device with a `noauto` fstab entry and started only for
  the test: once with 2 GiB parked on it, and once with 320 MiB and the test's
  reads from the disk capped at 2 MiB a second, where the drain has to take
  more than 90 seconds. The test runs in a cgroup of its own,
  `/sys/fs/cgroup/linuxio-drain-test`, which it removes. fstab then names
  `vdb` by `UUID=` for `TestMemoryDrainAliasIntegration`, and by its path
  again after it, also at the next run after a cut one. The device is stopped
  when its tests end, passed, failed or interrupted. A `vdb` that holds
  anything but swap, as after a storage check, is refused: run
  `make vm-reset` first. Last, `TestMemoryRebootIntegration` runs twice
  around an orderly reboot of the VM (the script's `reboot`): with fstab
  naming `/swap.img`, and naming it through the link `/swap-link`, which the
  suite makes and removes, also at the next run after a cut one. The whole
  target takes about six minutes. These changes stay in the VM until
  `make vm-reset` reverts its disks.
- `vm-test-filebrowser-root` does the same for the file browser's root-only
  ownership tests (`test-filebrowser-root`).
- `vm-test-relay-root` builds the `bridge/cmd` test binary on the host and
  runs the bridge-to-job-host relay tests (`jobs_relay_test.go`) as root in the
  VM. The tests serve a host in-process and the relay dials it without
  starting its socket unit, which as root would call systemd, so they also run
  as the invoking user; as root the in-process host is the root host.

## Overrides

Override values on the command line (`make test GO_TEST_FLAGS=-count=1`) or in
the environment. The lowercase names used internally are implementation
details; the uppercase aliases below are kept for CI and existing scripts.

### Repository and artifact paths

| Variable | Default | Purpose |
| --- | --- | --- |
| `REPO_ROOT` | Makefile directory | Repository root. |
| `FRONTEND_DIR` | `$(REPO_ROOT)/frontend` | Frontend project directory. |
| `BACKEND_DIR` | Auto-detected `backend/` or repository root | Go module directory. |
| `CACHE_DIR` | `$(REPO_ROOT)/.cache` | Make caches, logs, and analysis artifacts. |
| `BIN_DIR` | `$(REPO_ROOT)/build` | Built binary output directory. |
| `GO_TOOLS_DIR` | `$(HOME)/.go` | Installed Go tool binaries. |
| `NVM_DIR` | `$(HOME)/.nvm` | Node Version Manager installation. |
| `VITE_DEV_LOG` | `$(FRONTEND_DIR)/.vite-dev.log` | Vite log file. |
| `VITE_DEV_PID` | `$(FRONTEND_DIR)/.vite-dev.pid` | Vite PID file. |
| `quiet_log_dir` | `$(CACHE_DIR)/test-logs` | Full logs created by `*-quiet` targets. |

Paths are quoted by the recipes. Keep overrides confined to the repository or
an intentional user-local tool directory.

### Toolchains and commands

| Variable | Default | Purpose |
| --- | --- | --- |
| `GO_VERSION` | `go.mod` version | Go version used for builds; Go fetches it when the installed one differs. |
| `NODE_VERSION` | `frontend/package.json` engine | Node version installed by `ensure-node`. |
| `NVM_VERSION` | `0.40.2` | NVM installer release. |
| `GOOS` | `linux` | Go target operating system; LinuxIO validates this as `linux`. |
| `GOARCH` | Host architecture | Go target architecture (`amd64` or `arm64`). |
| `GOAMD64` | `v3` | amd64 microarchitecture level. |
| `GO_TOOLCHAIN` | `go$(GO_VERSION)` | `GOTOOLCHAIN` passed to every Go command. |
| `GO_BIN` | `go` on PATH | Go executable; useful for CodeQL integration. |
| `CC` | `cc` | C compiler for the authentication helper. |
| `sha256_cmd` | `sha256sum` | SHA-256 command; use `shasum -a 256` where required by a host. |
| `setsid_cmd` | `setsid` | Session launcher used to terminate complete validation process groups. |
| `GOLANGCI_LINT` | `$(GO_TOOLS_DIR)/bin/golangci-lint` | golangci-lint executable. |
| `GOLANGCI_LINT_VERSION` | `latest` | golangci-lint version installed by Make. |
| `MODERNIZE` | `$(GO_TOOLS_DIR)/bin/modernize` | modernize executable. |
| `MODERNIZE_VERSION` | `latest` | modernize version installed by Make. |
| `DEADCODE` | `$(GO_TOOLS_DIR)/bin/deadcode` | deadcode executable. |
| `DEADCODE_VERSION` | `latest` | deadcode version installed by Make. |
| `GOVULNCHECK` | `$(GO_TOOLS_DIR)/bin/govulncheck` | govulncheck executable. |
| `GOVULNCHECK_VERSION` | `latest` | govulncheck version installed by Make. |
| `GOLANGCI_LINT_OPTS` | `--modules-download-mode=mod` | Additional golangci-lint options. |

Go is not installed by Make: any Go 1.21 or newer on PATH works (linux-toolbox
installs it to `/usr/local/go`), and `GOTOOLCHAIN` makes Go download the exact
version `go.mod` pins. `SKIP_ENSURE_GO=1` is accepted for compatibility.
`CODEQL_ACTION_GO_BINARY` is honored automatically when present.

### Test, analysis, and build behavior

| Variable | Default | Purpose |
| --- | --- | --- |
| `GO_TEST_PKGS` | `./...` | Go packages (relative to `backend/`) for `test-go`, for example `./bridge/handlers/filebrowser/...`. |
| `GO_TEST_FLAGS` | empty | Extra flags for backend tests; `-count=1` disables Go test caching. `test-go` builds with the `sqlite_fts5` tag `build-indexer` uses and, on amd64, the `glibc` tag `build-monitoring` uses, so the indexer's FTS5 search and the NVML collector are tested; `golint-only` adds `glibc` too. A `-tags` given here replaces both. |
| `PLAYWRIGHT_ARGS` | empty | Extra arguments for the browser suite, such as one spec path. |
| `VITEST_MAX_WORKERS` | `8` | Maximum Vitest workers; lower this on memory-constrained hosts. |
| `VITEST_FILE` | empty | Frontend-relative test/spec file for a focused Vitest run. |
| `VITEST_TEST_NAME` | empty | Vitest test-name regex, optionally combined with `VITEST_FILE`. |
| `GO_BUILD_EXTRA_ENV` | empty | Extra environment assignments for Go builds. |
| `GO_BUILD_TAGS` | empty | Additional Go build tags. |
| `DEADCODE_PARALLEL` | `auto` | Deadcode scheduling: `auto`, `0` (serial), or `1` (parallel). |
| `DEADCODE_CACHE` | `1` | Reuse content-addressed deadcode results; set `0` to rescan. |
| `LTO` | `1` | Enable link-time optimization for the C helper. |
| `STRIP` | `1` | Strip unneeded symbols from the C helper. |
| `WERROR` | `0` | Treat C compiler warnings as errors. |
| `VERBOSE` | `true` | Enable verbose frontend tooling output. |
| `quiet_failure_lines` | `40` | Failure-tail length for `*-quiet` targets; `0` suppresses the tail. |
| `LOC_INCLUDE_EXT` | `js,jsx,ts,tsx,css,scss,html,go` | Extensions counted by `make cloc`. |

### Development server inputs

| Variable | Default | Purpose |
| --- | --- | --- |
| `VITE_DEV_PORT` | `3000` | Vite development server port (`1`–`65535`). |
| `DEV_LOG_LINES` | `25` | Number of LinuxIO log lines shown by `make dev`. |
| `VSCODE_EXTENSIONS_DIR` | `~/.vscode-server/extensions` if present, else `~/.vscode/extensions` | Where `make dev-status-install` and `make dev-status-uninstall` manage the status bar extension symlink. |

`make dev` starts only the Vite development server and tails LinuxIO logs. The
backend still runs through systemd. `make dev-stop` stops Vite from any
terminal, and every running `make dev` exits once Vite is gone; interrupting
`make dev` stops Vite only when that run started it.

### VS Code integration

`.vscode/tasks.json` defines `make dev` and `make dev-stop` as tasks. `make dev`
runs in its own terminal that is never revealed automatically (`reveal:
"never"`), so the log tail stays available without taking over the panel, and
that closes when `make dev` exits, for example after `make dev-stop`.

`.vscode/dev-status/` is an optional workspace extension that adds a status bar
dot: green while the Vite pid in `frontend/.vite-dev.pid` is alive, grey
otherwise. Hovering it shows links to start or stop the dev server (by running
the two tasks) and to show the `make dev` terminal. It only activates in a
checkout that has `scripts/dev.sh`. Install it once on the machine that runs the
VS Code server, then run `Developer: Reload Window`:

```sh
make dev-status-install     # symlink the extension into the extensions directory
make dev-status-uninstall   # remove the symlink
```

Both targets only touch the `linuxio.linuxio-dev-status-*` symlink, so the
extension always runs from the checkout. Install is safe to repeat. The target
directory is `VSCODE_EXTENSIONS_DIR`, which defaults to
`~/.vscode-server/extensions` when it exists and `~/.vscode/extensions` otherwise.

The extension also ships the key bindings, because VS Code has no
workspace-level `keybindings.json` and a repo cannot carry bindings any other
way. They apply only while the extension is active in a LinuxIO checkout (the
`linuxioDevStatus.active` context), so installing it is all a contributor does;
a binding of the same key in the user's own `keybindings.json` wins over them.

| Key | Command | Does |
| --- | --- | --- |
| `Ctrl+Shift+1` | `linuxioDevStatus.start` | Runs the `make dev` task. |
| `Ctrl+Shift+2` | `linuxioDevStatus.stop` | Runs the `make dev-stop` task. |
| `Ctrl+Shift+3` | `linuxioDevStatus.reinstall` | Asks for the sudo password in a masked prompt, then runs `make reinstall` in a visible terminal. |

`make reinstall` calls `sudo` from the Makefile, and `sudo` caches credentials
per terminal, so priming it first would not help. The reinstall command instead
runs `make reinstall SUDO='sudo -A'` (`SUDO` defaults to `sudo` in
`mk/install.mk`) with `SUDO_ASKPASS` pointing at
`.vscode/dev-status/askpass.sh`, which prints `$LINUXIO_SUDO_PASSWORD`. That
variable is set only in the task's environment: the password is never written
to disk, put on a command line or shown in the terminal, but the build tools
that `make` starts inherit it. A wrong password fails the task, and you press
`Ctrl+Shift+3` again. Changing a binding or command needs the extension's
`version` bumped in `package.json`, then `make dev-status-install` and a window
reload, because the symlink name carries the version.

### Target-specific inputs

These are consumed by particular targets rather than global defaults:

- `BRIDGE_SHA256` supplies the bridge digest embedded by `build-backend`.
- `LINUXIO_CWRAP_LIBDIR` points `test-auth-pam` at wrapper libraries installed
  outside standard system library directories.
- `CFLAGS`, `LDFLAGS`, `GOOS`, and `GOARCH` remain standard toolchain
  overrides and are passed through where applicable.

## Release overrides

Private release targets are loaded from `release.mk` when that file is
available. Their supported inputs are:

| Variable | Purpose |
| --- | --- |
| `REPO` | Repository passed to GitHub CLI commands, for example `owner/name`. |
| `VERSION` | Release version consumed by `start-dev`/`open-pr`, such as `v1.2.3`. |
| `PR` | Optional pull-request number for `merge-release`. |
| `CONFIRM` | Set to `0` to skip the `merge-release` confirmation prompt. |

`merge-release` shows a merge summary and asks for confirmation before merging.
If the release PR is already merged — for example after an interrupted run —
re-running `make merge-release` resumes the release-workflow watch and branch
cleanup instead of failing.

GitHub CLI authentication is provided by its normal credential mechanism; no
credentials are stored in Make variables.

## Dependency updates

Run `make update-deps` in a terminal to choose frontend upgrades. Nothing is
preselected: use Space to select packages and Enter to apply. Select related
packages such as `vitest` and `@vitest/coverage-v8` together.
The picker offers the highest stable version (`--target greatest --pre 0`),
even when the registry’s `latest` tag points to an older release.
The picker only edits `package.json`; the target handles installation and
auditing once, without a second install prompt.

When the selection changes `package.json`, the target installs dependencies,
runs an audit, applies `npm audit fix` only if the audit fails, and reports
any remaining vulnerabilities. A clean audit skips fixes. Audit fixes
can also update dependencies outside the selection. Selecting nothing skips
installation and audit fixes; Go dependencies are not updated by this target.

After a successful update the target prints a suggested Conventional Commits
message built from the `package.json` changes: `chore(deps): update dependency
<name> to v<version>` for one package, or a `chore(deps): update N frontend
dependencies` subject with one line per package for several. It never creates
the commit.

Run `make update-go-deps` to update all Go dependencies, including test
dependencies, with `go get -u -t ./...`, followed by `go mod tidy`.
To update specific modules instead, pass explicit versions or `@latest`:

```sh
make update-go-deps GO_MODULES='golang.org/x/net@latest golang.org/x/sys@latest'
```

Required transitive dependencies may also change.

When `go.mod` changes, the target prints a suggested commit message in the
same shape as `make update-deps`: `chore(deps): update module <path> to
<version>` for one direct module, or a `chore(deps): update N Go dependencies`
subject with one line per direct module, Go version or toolchain change.
Indirect-only changes give `chore(deps): update indirect Go modules`.

## Distro and WireGuard icons

The existing icon generator bundles the literal IDs in
`frontend/src/icons/distro.ts`, including distro aliases and a Linux fallback.
It also picks up WireGuard's existing capability icon. The Simple Icons package
is a development dependency; only selected logos enter the generated bundle.
Keep distro IDs explicit so the scanner can discover them.

## Target index

`make help` prints the common targets only (the `help_common` list in the
`Makefile`); `make help-full` (or `make help FULL=1`) prints the full index.
`make help --full` is not possible, because Make parses dashed words itself.
The full index groups the supported targets as follows:

- setup: `setup`, `setup-frontend`, `ensure-node`, `ensure-go`,
  `ensure-golint`, `ensure-modernize`, `ensure-deadcode`, `ensure-govulncheck`,
  `update-deps`, `update-go-deps`
- frontend checks: `lint`, `lint-only`, `tsc`, `tsc-only`, `test-frontend`,
  `test-frontend-only`, `lint-ci`, `tsc-ci`, `test-frontend-ci`,
  `check-frontend`, `setup-frontend-browser`, `test-frontend-browser`,
  `update-frontend-screenshots`
- backend checks: `golint`, `golint-only`, `test-backend`, `test-go`, `deadcode`,
  `deadcode-only`, `check-backend`, `test-auth`, `test-auth-protocol`,
  `test-auth-pam`, `test-updater`, `test-docker-update-integration`,
  `test-assistant-agent-integration`,
  `test-filebrowser-root`, `check-c-build-deps`
- analysis: `bundle-metrics`, `compiler-coverage`, `analyze`, `analyze-auth`
- builds and development: `test`, `build`, `build-nocheck`, `fastbuild`,
  `build-vite`, `build-backend`, `build-bridge`, `build-auth`, `build-cli`,
  `build-docker-update`, `build-leak-profile`, `generate`, `dev-prep`, `dev`,
  `dev-stop`, `dev-status-install`, `dev-status-uninstall`, `clean`, `cloc`
- installation: `localinstall`, `reinstall`, `uninstall`
- test VM: `test-vm-browser`, `vm-create`, `vm-install`, `vm-ssh`, `vm-stop`, `vm-reset`, `vm-update`, `vm-update-build`, `vm-destroy`,
  `vm-test-btrfs-integration`, `vm-test-memory-integration`,
  `vm-test-filebrowser-root`, `vm-test-relay-root`, `test-vm-backend`,
  `test-vm-browser-rerun`
- documentation: `help-overrides`, `check-docs` (the docs index in
  `docs/README.md`, and the paths and links docs name)

For release-helper smoke coverage, run `make test-release-automation`.

`_build-binaries` is an internal implementation target. It remains callable
for diagnostics but is not part of the normal user workflow.
