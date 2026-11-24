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

Do not invoke `go test`, Vitest, TypeScript, linters, or formatters directly;
use the corresponding Make target.

The browser tests serve the built frontend on `127.0.0.1:4174`. A failed run can
leave its `vite preview --config config/browser.vite.config.ts` alive, and the
next run then fails with "already used"; several sessions working in the same
checkout meet the same port. Before `make test-frontend-browser-quiet`, look
with `ss -ltn | rg 4174` and `pgrep -af 'vite preview'`, and stop only a
leftover preview:

```sh
pkill -f '^node .*vite preview --config config/browser.vite.config.ts'
```

The `^node` anchor matters: without it `pkill -f` also matches the shell that
runs it. Two runs at once slow each other down, so a timing assertion that
fails while a second browser log in `.cache/test-logs` starts within seconds of
yours is rerun when the machine is quiet;
`PLAYWRIGHT_ARGS="<spec> --repeat-each 2"` confirms the spec is stable.

## Prerequisites

- Go 1.21 or newer on PATH (Go fetches the exact version `go.mod` pins)
- Node.js 26 or newer
- GNU Make

For Debian/Ubuntu, install the development libraries and optional analysis/test
tools with:

```sh
sudo apt install -y build-essential libpam0g-dev libsystemd-dev debhelper \
  cppcheck clang-tools clang-tidy bear libpam-wrapper libnss-wrapper \
  libuid-wrapper
```

For Fedora/RHEL/CentOS, use:

```sh
sudo dnf install -y gcc pam-devel systemd-devel rpm-build systemd-rpm-macros \
  selinux-policy-devel cppcheck clang-tools-extra clang-tidy bear \
  pam_wrapper nss_wrapper uid_wrapper
```

`debhelper` assembles the DEB (`make package-deb`, and `make localinstall` on
the Debian family); `rpm-build` with the systemd and SELinux macro packages
assembles the RPM (`make package-rpm`, and `make localinstall` on the RHEL
family). The static-analysis tools are only needed by `analyze-auth`. The
wrapper libraries are needed by `test-auth-pam`; that target skips when they
are not installed.

The package itself needs only PAM, polkit, PackageKit and systemd
([Packages](./packaging.md#installing)). Docker and the other optional host
capabilities are installed after sign-in from LinuxIO Capability Manager;
Docker's entry runs Docker's convenience script and streams its output.

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
`8090`. Go/backend or bridge changes are rebuilt and installed with
`make localinstall`.

Built binaries land in `build/` (`BIN_DIR`). `make build-go-binaries` builds
every Go binary (embedding the bridge SHA-256 in the webserver),
`make build-binaries` adds the C authentication helper and writes
`build/THIRD_PARTY_NOTICES.md`, the notices the packages install, from the
programs and the frontend bundle (`make third-party-notices`;
[Packages](./packaging.md#third-party-notices)), and
`make print-binaries` lists the shipped-binary names that the release workflow
and the package tests consume. Only `common/version.Version` is stamped into
binaries at link time.

`make localinstall` installs this checkout as a package, the way a release
installs: it runs `make fastbuild`, picks the DEB or the RPM from
`/etc/os-release` (the Debian or the RHEL family), assembles it as you from
those programs (below) and installs it with `apt-get install --reinstall` or
`dnf install` (`dnf reinstall` for the version already installed). Only the
package manager and a closing `systemctl start linuxio.target` run through
`SUDO` (`sudo` unless overridden). The package restarts LinuxIO when it was
running and leaves a stopped one stopped; the start brings that one up. The
package refuses to install over a LinuxIO that the install script of v0.54 or
earlier put in ([Refusing a script install](./packaging.md#refusing-a-script-install)).

`make package-root DESTDIR=<empty folder>` stages the tree both packages
install, and `make package-deb` and `make package-rpm` assemble the DEB and
the RPM from the programs in `build/` into `build/packages/`
(`PACKAGES_DIR`), at `PACKAGE_VERSION` (from git; `PACKAGE_VERSION=...` on the
command line gives another, such as a higher one to try an upgrade with).
None of them builds a program; each runs as you and fails with the name of
the package to install when its tool is missing (debhelper, rpm-build), so a
Debian-family host assembles its RPM in a Fedora test VM. `make
test-packages` (`test-packages-quiet`) checks every LinuxIO package in
`PACKAGES_DIR` against `build/`, and `make test-packaging-scripts`, part of
`make test`, runs the host-independent fixtures of the packaging scripts and
files. What the scripts lay out, what the packages do when they are
installed, upgraded and removed, and what each check holds them to is in
[Packages, installation and updates](./packaging.md#how-the-packages-are-built).

`make repo-apt` and `make repo-rpm` add the packages in `PACKAGES_DIR` to the
signed repositories a release publishes, `build/repo/apt` and
`build/repo/rpm` (`REPO_DIR`): `packaging/scripts/repo-apt.sh` (the pool,
`apt-ftparchive` metadata with SHA-256 sums only and by-hash copies, each
package's changelog under `changelogs/` and the `Changelogs` field that
names it, `InRelease` and `Release.gpg`, and `linuxio.sources` with the
public key in `Signed-By`) and `packaging/scripts/repo-rpm.sh` (each RPM
signed with `rpmsign`, `createrepo_c` metadata with an `updateinfo` advisory
per package, `repomd.xml.asc`, `RPM-GPG-KEY-linuxio` and `linuxio.repo` with
`gpgcheck=1` and `repo_gpgcheck=1`); the changelogs are the packages' own
([Packages](./packaging.md#changelog)). A package
already there stays, the metadata is made again over all of them, and a
version already published with other contents is refused. They sign with
`REPO_SIGNING_KEY`, a key the caller's GnuPG keyring already holds, and never
create, import or print one; `REPO_BASE_URL` is the address `REPO_DIR` is
published at, without the `apt/` or `rpm/` each adds. The test VMs publish
with the same scripts and a throwaway key (`linuxio-vm.sh repo-publish`,
below). `packaging/scripts/tests/repo-fixtures.sh`, part of
`make test-packaging-scripts`, checks their arguments everywhere and, where
the tools are installed, a round trip with a key of its own.

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
(`vdb`–`vde`) for storage tests. Each data disk carries a serial,
`linuxio-vdb` to `linuxio-vde`, which udev reports as `ID_SERIAL`, as a real
disk's: the btrfs hot spare and the automatic rebuild recognise a disk by it.
A VM created before the disks had serials gets them at its next
`make vm-reset`, which every test target without `VM` runs, since the revert
to `clean` brings back the definition the snapshot kept; `make vm-update`
takes the snapshot again with them.

`make test-vm-browser` is the whole browser run in one command. It builds
LinuxIO and the update build the update checks publish (`vm-update-build`,
[Jobs](./jobs.md#testing)), picks free VMs (below), creating the ones it
needs, reverts each to `clean`, installs the build in each and runs the specs
on all of them at once. Then it reverts the first
VM to `clean` again and runs `jobs-update.spec.ts` alone on it, the
Playwright project `release-update`, since that file installs LinuxIO itself
from the VM's signed repository and leaves the VM changed; a run narrowed
with `PLAYWRIGHT_ARGS` skips that stage, and one narrowed to
`--project=release-update` reverts its VMs, installs nothing and runs that
file alone. At the end it shuts the VMs down. `make test-vm-backend` is
the same for the root suites, without the install. A VM needs 4 GiB of free
memory (`MemAvailable`, after 1.5 GiB kept for the host and its browsers) and
counts as two of the host's threads (`nproc`, hyperthreads and not cores),
though it has 4 vCPUs: the VMs share threads, since they mostly wait (below).
So a host with 12 threads runs six with 26 GiB free and three with 14 GiB;
nine is the most, the numbers a release has, and `test-vm-backend` takes no
more than it has suites. A test VM that already runs, or that another target
holds, takes one from the count, so runs side by side divide the threads.
`VMS=2` asks for two, and `VMS` is taken as given, with only the memory
checked. Both work from a host with no VM yet; the first run waits for each
VM's first boot.

The longest spec file sets the floor of a browser run, about a third of the
time of all the files when this was measured, so VMs past three or four
shorten the full browser run little; they serve the root suites and runs
side by side.

The VMs are numbered from 1: `linuxio-e2e`, then `linuxio-e2e-2` with its
`linuxio-e2e-2-*` volumes and `~/.cache/linuxio-vm-2`, up to 9. They share
only the image cache. `VM=2` on a target names one of them. `vm-create`,
`vm-destroy` and `vm-ssh` act on one VM, the first unless `VM` names another;
`vm-install`, `vm-reset`, `vm-update`, `vm-stop` and `test-vm-browser-rerun`
act on every VM that exists, side by side (any of 1 to 9, so a gap left by
`vm-destroy` does not hide the VMs after it), on VMs 1 to `VMS` when `VMS`
is given, or on those `VM` names, one or a list (`VM='2 3'`). The test targets pick their VMs (below) unless `VM` names
them. A run prints the VMs it used, as the `VM` that names them again:
`test-vm-browser-rerun` and `vm-ssh` after a `test-vm-browser` take that
`VM`, since the other VMs do not have the build.

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

Each VM has a lock, `~/.cache/linuxio-vm/locks/DOMAIN`. A target takes the
locks of the VMs it acts on, without waiting, as its recipe begins: after
its prerequisites (`build-nocheck`, `vm-update-build`, `ensure-go`,
`setup-frontend`) and after make has asked the script how many VMs exist,
but before it touches a VM, so a refused target may have built something but
has reverted, booted or stopped nothing. The recipe's shell opens the lock
files and the script takes the locks on those descriptors, so they stay with
the shell for the whole recipe, and every step of the recipe (prepare,
install, the specs, the release-update stage on its first VM, the trap that
stops the VMs, each VM's row and log) acts on the VMs it locked. Targets on
different VMs, of any distribution or release, run side by side, as many as
the host has memory for; a target that needs a VM another target holds is
refused with the name of that VM, since it would revert, reboot or stop the
VM under the first. A target that needs several VMs takes all of them or
none, under a short lock (`locks/taking`) so that no other target finds part
of them held: refused for one, it stops nothing and leaves the others free.
A lock file it cannot open refuses it too. `make vm-ssh VM_KEEP=1` on a
running VM takes no lock, since it stops nothing, so it can look into a VM
another target holds; on a VM that is off it takes the VM's lock to start
it, like any target, and should the VM stop while it runs without the lock,
it refuses to start it again.

A test target given no `VM` (`test-vm-backend`, `test-vm-browser` and the
`vm-test-*` suites) picks its VMs. It first tries the lock of each of VMs 1
to 9, so it sees every VM of its release other targets hold, then takes, in order, the free
ones that exist and are off and hold no installed build, then those that
hold one (`install` leaves `installed` in the VM's state folder; a revert, a
create and a destroy remove it), then numbers that do not exist yet, which it
creates (a first boot takes a few minutes), until it has what it wants: one
for a `vm-test-*` suite, and for the others as many as the host's threads
allow, two for each VM, less the test VMs that run or that other targets hold
(`test-vm-backend` no more than it has suites), or `VMS`. It takes a VM only
when the memory has room for it: 4 GiB for each of the run's VMs and 1.5 GiB
kept for the host, where other test VMs count as spent (what the running
ones have not used yet, and 4 GiB for each one another target holds that is
off or still being created, since its target will start it). A VM that runs with no target holding
it was left up to look at (`VM_KEEP=1`, or a target killed before its trap),
so it is left alone. Picking and locking are one step under `locks/taking`,
so two targets started at once never take the same VM. Its first line says
what it took, why no more and the memory it counted, and the run goes on
with what it got: `linuxio-vm: VM 2 for this run; in use: linuxio-e2e;
memory: 14.4 GiB available, less 4.0 GiB for other test VMs and 1.5 GiB
kept for the host; VM=2 names it again`, or `VM 2 for this run, 1 of 3
wanted (another VM needs 4.0 GiB and 3.5 GiB is left); ...`. With none it
fails, naming the busy VMs and what the memory check found. A first line
that takes a VM holding a build says so (`VM 1 held an installed build,
which the revert removes`). A picked VM's state is unknown, so a `vm-test-*`
suite reverts it to `clean` first (and creates it when it is
missing), as the full runs do. With `VM=N` a suite runs on that VM as it is,
and is refused when another target holds it; `VM` with a list is for the
targets that act on several. `make vm-create VM=4` creates a VM ahead of
the run that would want it.

A checkout or worktree whose `mk/vm.mk` is older than these locks takes
`~/.cache/linuxio-vm/lock` alone for any VM target, and one older than
commit `0042b2a6` (2026-11-17) takes no lock at all, so nothing keeps its
targets away from any other. Targets with the per-VM locks take the old lock
shared, so a target of a checkout with the old lock is refused while one of
them runs, and the other way round, while they run beside each other. The
shared lock can go once no checkout with the old lock is left.

Before the script starts a VM that is not running (a first boot in
`vm-create`, a target that starts a stopped VM, a spec's `power-cut` or
`off-for`, or its boot after it powered the VM off), it checks the host's
memory, since targets side by side could otherwise start more VMs than the
host holds. A first start needs the VM's 4 GiB and the 1.5 GiB kept for the
host free, where what running test VMs of any distribution or release were
given and have not used yet (their memory less their RSS) counts as spent,
and a running one whose numbers libvirt does not give counts whole. A
restart of a VM that the same target started before or found running needs
only its 4 GiB: the reserve is for the run's browsers, which have it by then.
Make gives each target an ID, `LINUXIO_VM_RUN`, which the specs' rig calls
inherit, and the VM's state folder keeps, in `run`, the ID of the target that
last started it or found it running. When the VM does not fit, the script
fails before starting it, saying what it needs, what is free and which test
VMs are running. Starts take a short lock (`locks/start`) around the check,
so two at once never count the same memory, and `vm-create` also checks
before its download. `test-vm-browser` shuts the other VMs of the run down
before the release-update spec boots the first one again (not with
`VM_KEEP=1`).
`LINUXIO_VM_MEMINFO=FILE` makes the check read `FILE` in place of
`/proc/meminfo`, to see a refusal without filling the host.

Known limits:

- A run counts the memory when it picks, and each start checks again: a
  start can still be refused, failing the run, when memory went elsewhere
  meanwhile, or at the edge of what the host has, since a running VM holds a
  little more than its 4 GiB. A busy VM that is off, or still being
  created, counts as 4 GiB to come even when its target only stops or
  reverts it, so a run beside `vm-reset` may take fewer VMs than the host
  holds. Only a run that plans its own number is
  held to the threads: one given `VMS` or `VM`, and a `vm-test-*` suite's one
  VM, start whatever else runs, with the memory checked.
- Picking counts the VMs of its own release only: a VM of another release
  or distribution that another target holds and has not started yet is not
  counted, so one of the two can be refused at its start.
- Only `install` (`make vm-install`, the browser runs) marks a VM as holding
  a build. The indexer suite installs LinuxIO itself and
  leave no mark, so such a VM is picked like a clean one; it is reverted
  first either way.
- A VM a test target creates stays, stopped, and joins the VMs that the
  targets without `VM` act on (`vm-reset`, `vm-install` and so on);
  `make vm-destroy VM=N` removes it.
- A VM left running with no target holding it is not picked until
  `make vm-stop VM=N` shuts it down, and what it has not used yet counts as
  spent meanwhile.
- A restart can still be refused when other targets started VMs while this
  one was off (during an `off-for`, or after a spec powered it off); the VM
  then stays off and the specs after it on that VM fail. A restart counts on
  no reserve, so it can also take what another target's first start counted
  on, and a VM that a target created counts as started by it, so its next
  boot in that target is a restart.
- Every program a target starts inherits the lock descriptors, so a target
  killed with SIGKILL whose child lives on (the seed server of a first boot,
  a Playwright browser) leaves its VMs locked until that child ends.
  `lslocks` names it, by the lock file `~/.cache/linuxio-vm/locks/DOMAIN`.
- Targets started from one checkout share its `.cache` (the suite binaries
  and their queue, the logs, the Playwright results), so targets that run
  side by side are started from separate checkouts or worktrees, as
  `AGENTS.md` asks of any two Make runs.

The full runs are long: the browser specs take 15 minutes on three VMs and
42 on one, the root suites about 6 on three, and either holds every VM it
runs on, up to nine, meanwhile. An agent therefore runs only the suite
(`SUITES=...`) or the spec file (`PLAYWRIGHT_ARGS=...`) its change needs and
leaves the full runs to the owner (`AGENTS.md`).

`VM_RELEASE=noble` on a `vm-*` target selects a second VM on Ubuntu 24.04
(kernel 6.8): the domain `linuxio-e2e-noble`, its `linuxio-e2e-noble-*`
volumes and `~/.cache/linuxio-vm-noble`, beside the first one. It is there for
`make vm-test-btrfs-integration VM_RELEASE=noble`, the root btrfs tests on a
kernel before 6.19, where a signal does not abort a scrub or a replace. The
test program is built without cgo, so it runs on the older glibc. The browser
specs run there too (`make vm-install VM_RELEASE=noble`, then
`make test-vm-browser-rerun VM_RELEASE=noble PLAYWRIGHT_ARGS=...`): sign-in,
the scheduled-script specs and `package-repository.spec.ts` were run on it,
and the DEB's removal, purge and install again were checked there by hand
([Packages](./packaging.md#removal)).

`DISTRO=fedora` on a `vm-*` or `test-vm-*` target selects the Fedora VMs
instead of Ubuntu's: the domain `linuxio-e2e-fedora` (`linuxio-e2e-fedora-2`
and so on), its `linuxio-e2e-fedora-*` volumes and
`~/.cache/linuxio-vm-fedora`, beside the Ubuntu VMs, which keep their names
when `DISTRO` is not given. Each has a lock of its own, so a target on a
Fedora VM runs beside one on an Ubuntu VM. What differs between
distributions is a profile at the top of the script: the cloud image, where its checksum is
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
  `docker-compose` and `docker-buildx`, `btrfs-progs`, the RPM tools
  above, `pam`, `polkit` and `PackageKit` (what the RPM requires), every
  package upgraded, and
  `dnf-makecache.timer` off. SELinux stays enforcing. Fedora's root
  filesystem is btrfs.
- `dnf` where Ubuntu has `apt`, in `vm-update` and in the suites.

Only part of the tests is written for Fedora, and `mk/vm.mk` holds the two
lists: `vm_fedora_suites` (the root suites `memory`, `indexer`, `packagekit`,
`relay`, `userconfig` and `filebrowser`; `docs/TODO/e2e-testing.md`,
"Distributions", has their results) and `vm_fedora_specs` (`login.spec.ts`,
`capability-install.spec.ts`, `package-repository.spec.ts`,
`indexer.spec.ts` and `wireguard.spec.ts`). With
`DISTRO=fedora`, `test-vm-backend` given no `SUITES` runs the first list, and
`test-vm-browser` given no `PLAYWRIGHT_ARGS` runs the second and then the
release-update spec, which is written for both distributions. The other
suites and specs do things with `apt` packages or were never run there; one
that passes on Fedora is added to its list. `capability-install.spec.ts`
skips its cancel test on Fedora. `make test-vm-all` runs the four in turn:
Ubuntu's root suites and browser specs, then Fedora's. It ends at the first
failure and takes no `SUITES`, `PLAYWRIGHT_ARGS` or `DISTRO`.

```sh
make test-vm-browser        # everything below that a browser run needs, in one go
make test-vm-backend        # the same for the root suites, shared out between the VMs
make test-vm-all            # both of the above on Ubuntu, then on Fedora what is written for it
make vm-create              # download and check the image, first boot, snapshot "clean"
make vm-create VM=2         # one more VM
make vm-install             # make build-nocheck, then package this build in the VMs and install it
make vm-ssh CMD='systemctl is-active linuxio.target'
make test-vm-browser-rerun  # the Playwright specs in frontend/src/test/vm, on the VMs as they are
make vm-test-btrfs-integration
make vm-stop                # shut the VMs down; what is installed stays
make vm-reset               # revert every disk to "clean" and stop the VMs
make vm-update              # upgrade the packages in "clean" and take the snapshot again
make vm-update-build        # this tree with jobs.Protocol one higher and a version of its own, for the update checks
make vm-destroy             # delete the first VM and its volumes; the cache stays
```

- `vm-install` (and the install of `test-vm-browser`) copies the build (the
  programs and their `THIRD_PARTY_NOTICES.md`), `packaging/` and `LICENSE`
  into the VM, which
  assembles the profile's package from them, as a release's image does: the
  DEB with `package-deb.sh` on Ubuntu, the RPM with `package-rpm.sh` on
  Fedora, at `PACKAGE_VERSION`, its changelog the fixture release notes
  `packaging/scripts/tests/release-notes.md`, which the update spec reads on
  the Updates page. It runs `package-contents.sh` on it, fails
  there when the check does, and installs it with `apt-get install
  --reinstall` or `dnf install` (`dnf reinstall` for the version installed),
  over a LinuxIO package of the same or an older version; then it starts
  `linuxio.target`, so LinuxIO runs. The tools that assemble a package
  (`debhelper`; `rpm-build`, `systemd-rpm-macros`, `selinux-policy-devel` and
  `systemd-devel`) come with the seed, `vm-update` adds them to `clean`, and
  the install adds them to a VM that lacks them.
- `linuxio-vm.sh repo-publish BUILD_DIR VERSION [NOTE]`, which the update
  specs run, publishes a build into the VM's signed package repository as a
  release does: the VM assembles the package as `vm-install` does, at
  VERSION, and the release's own `repo-apt.sh` or `repo-rpm.sh` adds it to
  the packages already in `/srv/linuxio-repo/apt` (or `rpm`) and signs the
  metadata (and the RPM). The signing key is a throwaway one the first
  publish makes in the VM (`gpg --batch`, no passphrase) under
  `~/.linuxio-repository-gnupg` of the VM's user; it never leaves the VM and
  goes with `vm-reset`. apt, dnf and PackageKit read the repository in place
  through `file://`, PackageKit's apt and dnf backends included (seen on
  Ubuntu 26.04 and Fedora 45), so no server runs; the apt changelogs alone
  are named `copy://`, since apt's `file:` method leaves a fetched changelog
  where `apt changelog` and PackageKit do not look. The publish copies the
  repository's `linuxio.sources` or `linuxio.repo`, which carries the public
  key and turns every signature check on, to `/etc/apt/sources.list.d` or
  `/etc/yum.repos.d`; it installs nothing. NOTE is appended as a comment to
  the copy of `packaging/selinux/linuxio.cil` that package is assembled
  from, so it carries a changed SELinux module. The tools (`apt-utils` and
  `gnupg`; `createrepo_c`, `rpm-sign` and `gnupg2`) are in the profile's
  list with the assembling tools.
- `vm-create` downloads the cloud image (checked against the release's
  `SHA256SUMS`, Fedora's `CHECKSUM`), and boots it once with
  `packaging/vm/user-data.yaml.in` (`user-data-fedora.yaml.in` on Fedora).
  cloud-init reads its seed over HTTP from a server on
  `192.168.122.1:8399` (8400 for VM 2, and so on; from 8409 for
  `VM_RELEASE=noble` and 8419 for Fedora, so that first boots of any VMs can
  run at once) that runs only during that boot. It creates the user
  `linuxio-e2e` with passwordless sudo and installs `docker.io` with its
  Compose and buildx plugins (`docker-compose-v2`, `docker-buildx`),
  `btrfs-progs`, the tools that assemble and publish the package, and what
  the DEB depends on (`libpam0g`, `packagekit`, `polkitd`), so an install of
  the package needs no download, and upgrades every package. The
  VM is then shut down and snapshotted as `clean`, without LinuxIO. A change
  to the seed's packages reaches a VM only when it is created again
  (`vm-destroy`, then `vm-create`); `vm-update` upgrades what `clean` already
  has.
- Every target that needs the VM starts it if it is stopped and shuts it
  down when it ends, passed or failed (`vm-install`, `vm-ssh`, the
  `test-vm-*` and the `vm-test-*` ones), so no VM runs unused. What was
  installed and the journal stay on its disks only until a target reverts
  it: a stopped VM may be reverted by any test target that picks it (one
  that holds a build after the others). To keep one, leave it running with
  `VM_KEEP=1`, which a pick never takes, or name it with `VM=N` in your own
  next target before another run takes it. `VM_KEEP=1` on a target leaves
  the VM up, to look at a failure while its processes still run, or to run
  several `vm-ssh` commands against one boot; `make vm-stop` then shuts it
  down. `vm-reset` leaves it stopped, without
  LinuxIO; run `vm-install` again after a reset.
- A change to a systemd unit or socket, a PAM file or SELinux policy counts
  as working only when it is seen as the package installs it and again after a
  reboot, so that every socket and service starts with it. Every target
  shuts the VM down when it ends (not with `VM_KEEP=1`), so the next one
  (`make vm-ssh CMD=...`) boots it afresh under its lock. The script's own `reboot`
  (`packaging/vm/linuxio-vm.sh reboot`) takes no lock: it is for a spec, or
  for a VM a target of yours holds. A drop-in or an edit made on a running VM shows
  less: `SELinuxContext=` on `linuxio-auth@.service` let sign-in pass that
  way, because its socket was already listening, and as installed systemd
  could not create the socket at all
  (`docs/process-systemd-architecture.md`, "SELinux").
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
  that long, then a boot), each of which waits for the next boot. `reboot`
  first waits until the boot is 40 s old, and `power-cut` and `off-for` keep
  the domain off for what is missing: libvirt's DHCP server offers an address without
  its 3 s ping when it pinged it in the last 30 s, which is fast enough for
  the initrd's DHCP client to take the VM's address, and the system then
  gets another, where no spec looks for it. `jobs-phase2.spec.ts`
  installs Docker's Compose plugin, creates a btrfs pool on `vdb` and `vdc`
  and reboots the VM, so run `vm-reset` and `vm-install` before reusing it
  for other checks. A disk that drops out needs no rig command:
  `jobs-phase3.spec.ts` takes a data disk off the guest's PCI bus
  (`/sys/bus/pci/devices/<function>/remove`) and brings it back under its
  name with `/sys/bus/pci/rescan`, while the domain keeps it.
  `ui-actions.spec.ts` is not a jobs spec: a session without sudo, the
  "Open the real folder" action, a toast's page link on Docker, and the
  navbar's Reboot and Power Down. It creates and removes a user without sudo
  (`e2e-ui`), a folder `/srv/e2e-link` and two busybox containers, reboots the
  VM and powers it off (the rig's `ssh` starts a domain that is shut off).
  `docker-host-ip-wait.spec.ts` is the boot test of Docker's wait for
  published host IPs
  ([Bridge Configuration Storage](./config-storage.md#wait-for-published-host-ips)).
  Two units of its own stand in for the hosts the setting is for: `e2e-early`
  puts an address on a bridge before the network is up, and `e2e-late` adds
  one on a dummy interface 10 s after everything Docker's unit is ordered
  after. A busybox container publishes a port on each, and the VM reboots
  twice, without the setting and with it. The spec removes its units, links
  and container and leaves the setting off; a run that stops between its
  save and its last test leaves it on, with addresses the VM no longer has,
  and the next run switches it off first.
  `capability-install.spec.ts` installs zram from Capabilities
  ([Capabilities](./capabilities.md#zram)), and on Ubuntu cancels a second
  install while apt holds dpkg back for 30 s, through an apt drop-in it
  always removes, to check that the job waits for PackageKit's transaction.
  It reads the distribution from
  `LINUXIO_VM_DISTRO` (`vmDistro` in `vm.ts`), takes the zram tool out with
  that distribution's package manager first, and leaves zram and its tool as
  the clean image has them: on Ubuntu no zram tool and no swap, with the apt
  timers stopped until the next boot; on Fedora `zram-generator` with its
  default `zram0` swapping.
  `swap-files.spec.ts` creates, resizes and removes LinuxIO's swap file
  from the Memory overflow dialog and then switches to compressed memory; it
  leaves the VM with no swap and no zram tool, and skips itself on Fedora.
  `indexer.spec.ts` presses Index filesystem and follows the scan unit
  (`linuxio-indexer-index.service`) to its result, which is the one path from
  the page through the bridge and systemd to the scanner and the daemon
  ([Filesystem Indexer](./indexer.md#full-index)). It then makes a file
  outside LinuxIO, opens its folder and finds the file by search, and renames
  it through LinuxIO. It scans the whole VM, and creates and removes
  `/srv/e2e-indexer`. It also checks that a file made and one deleted in the
  VM appear and go in an open folder without a reload
  ([File Browser Live Listing](./filebrowser-live-listing.md)).
  `wireguard.spec.ts` creates an interface `wge2e` through the page's create
  dialog, setting its port, CIDR and NIC under Advanced, checks that the
  first device's QR code opens with `iface` and `qr` in the URL, then calls
  the WireGuard routes for it and checks the link, `wg-quick@wge2e.service` and the kernel log for
  AppArmor refusals ([WireGuard Interfaces](./wireguard.md#testing)). It
  installs `wireguard-tools`, runs `wg-quick` by hand for the cases where the
  unit and the interface disagree, reboots the VM once, and removes the
  interface; the package stays installed. It turns IPv4 forwarding off before
  the create, before one up and under the running interface, and
  `conf/default`'s for one up, makes a network namespace `wge2e-peer` for
  the peer that pings through the tunnel, runs the connection check through
  the route and from the card's dialog, with forwarding on and off, and
  checks that each check leaves no device peer, link (`linuxio.check`),
  namespace or listed peer behind, and disables Docker's units for
  the reboot, enabling them again after it (their names are kept in
  `/var/tmp/wge2e-docker-disabled` meanwhile, for the clean-up of the next
  run after one that was cut short); forwarding is on when it ends.
- The `vm-test-*` targets are the root suites. Each builds its test binaries
  on the host into `.cache/vm`, static so they also run on the older glibc
  of `VM_RELEASE=noble`, and the script's `test` command copies them into
  the VM, prints its kernel version and runs them as root, with the test
  patterns of the matching host target. Without `VM` a suite picks a free
  VM and reverts it to `clean` first; with `VM=N` it runs on that VM as it
  is.
- `test-vm-backend` runs every root suite: the four above, and five that
  have no target of their own. It picks as many free VMs as the CPUs and
  the memory allow, one for each suite at most, creates the ones it needs
  and reverts each to `clean` so the run starts from a known state; it installs no build,
  since the suites bring their own test binaries. The suites wait in one queue, longest first, and each VM takes the
  next when it is free: memory (about 9 minutes, with four reboots), btrfs (3), indexer (just
  under 2, most of it the indexer's idle timeout), docker (1½), packagekit
  (under ½, most of it the first boot), then relay,
  userconfig, filebrowser and network, seconds each. On three VMs
  the run takes about as long as memory, plus the LinuxIO build the indexer
  suite needs. `SUITES='docker relay'` runs only those. With one VM
  (`VMS=1`) they run one after another on it, the later ones on what the
  earlier left. The first failure ends that VM's suites; the other VMs take
  what is left of the queue. With more than one VM each one's output is in
  `.cache/test-logs/vm-N-test.log`. The five without a target:
  - `indexer` is the disposable-host smoke test
    ([Indexer](./indexer.md#disposable-host-smoke-test)). It needs the
    LinuxIO build, so the target runs `build-nocheck` first; the suite
    copies the checkout's packaging and the build in and assembles the
    package there as `vm-install` does, and the script installs it with apt
    or dnf, checks socket activation, idle exit and the index timer, and
    removes it again.
  - `docker` is `test-docker-update-integration`: real Compose replacements,
    which pull alpine images from Docker Hub. Ubuntu's `docker.io` and
    Fedora's `moby-engine` come without the Compose and buildx plugins, so
    the seed installs them (`docker-compose-v2`, `docker-compose` on Fedora,
    and `docker-buildx`), and the suite installs them in a VM created before
    the seed did. With buildx Compose builds through Bake, as it does on a
    host that has Docker's own packages; a host without buildx is covered by
    the unit tests' recorded build output only.
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
  on the host and runs the memory suite (`suite_memory` in
  `packaging/vm/linuxio-vm.sh`) as root in the VM: the swap files LinuxIO
  creates, resizes, removes and moves, the modes and the drain, through the
  routes' own functions on the real kernel and systemd (see
  [Memory overflow](./memory.md#tests) for what each test checks). Each test
  the suite names must pass: the rig fails a run whose `-test.run` pattern
  matches no test (go test passes one) and, through `memory_test`, a named
  test that skipped, so a renamed or deleted test never passes silently. With
  `ZRAM_PROVIDER=zram-generator` it installs `systemd-zram-generator`
  instead and refuses a VM that has zram-tools, which LinuxIO prefers: run
  `make vm-reset` first. The suite needs a zram tool and a swap file in
  `/etc/fstab`, so the first run installs `zram-tools` with apt and disables
  the `zramswap.service` its package starts, and, when `/etc/fstab` has no
  `/swap.img` (the clean Ubuntu image has no swap at all), creates a 1 GiB
  `/swap.img`, adds it to fstab and starts it (`swap_file`); it makes it
  again after each step that removes it. On Fedora, which has no zram-tools,
  `zram-generator` is the default and comes with the image, and so does a
  `zram0` that swaps from boot (`zram-generator-defaults`); Ubuntu's
  `systemd-zram-generator` brings such a default too. Where that vendor file
  (`/usr/lib/systemd/zram-generator.conf`) is there, the suite first puts
  the default back as a fresh image has it: no `/swap.img`, no
  `/etc/systemd/zram-generator.conf`, and `zram0` started again from the
  vendor file. It runs `TestMemoryDefaultZramIntegration` there, and again
  once `/swap.img` swaps beside it. The other tests start from `/swap.img`
  alone, so the suite then switches the default off the way Fedora
  documents, which is also where LinuxIO's disk mode leaves it: an empty
  `/etc/systemd/zram-generator.conf`, a reload, then a stop of
  `dev-zram0.swap` and its setup service. A stop alone does not hold: while
  a configuration names the device, systemd starts it again as soon as it is
  reset. Fedora's root is btrfs with `/var` a subvolume of its own, so
  `/swap.img` is made with `btrfs filesystem mkswapfile` (a file without
  copy-on-write) in the subvolume of `/`, which, unlike `mkswap`, leaves the
  file with its folder's SELinux type: the suite labels it `swapfile_t`.
  LinuxIO is not installed in the VM, so the tests make
  `/var/lib/linuxio` (0755, as its tmpfiles entry does) where they need it
  and remove it after. In order the suite runs `TestSwapFileBlocksIntegration`
  (`LINUXIO_RUN_SWAPFILE_BLOCKS_INTEGRATION=1`), `TestMemoryIntegration`,
  the resize, leftover, missing-file and move tests (the move only on a
  btrfs root; elsewhere it may skip), then the status read without sudo: it
  puts a 0600 file of root's without a swap header in fstab, has
  `TestMemoryStatusAsRootIntegration` write root's status to
  `/var/tmp/linuxio-memory-root-status.json`, installs TuneD (`tuned`) and
  starts it, and runs `TestMemoryStatusWithoutRootIntegration` and
  `TestReadFolderFSWithoutRootIntegration` as a system user without sudo it
  creates, `linuxio-e2e-nosudo`, from a copy of the test binary in
  `/var/tmp`; TuneD is then stopped and disabled, and the user, the file, its
  line and the copies removed. It then makes the data disk `vdb` a swap
  partition with a `noauto` fstab entry at priority 10, started only for
  `TestSwapPartitionIntegration` (`LINUXIO_MEMORY_PARTITION=/dev/vdb`) and
  stopped when it ends, passed, failed or interrupted; a `vdb` that holds
  anything but swap, as after a storage check, is refused: run
  `make vm-reset` first. `TestMemoryDrainIntegration` then runs twice on
  `/swap.img`: once with 768 MiB parked on it, and once with 320 MiB and the
  test's reads from the disk capped at 2 MiB a second, where the drain has to
  take more than 90 seconds. The drain, resize and partition tests run in a
  cgroup of their own, `/sys/fs/cgroup/linuxio-drain-test`, which they
  remove; the read cap goes on the whole disk under the swap file
  (`io.max` takes no partition). Last come the reboots (the script's
  `reboot`, orderly): `TestSwapFilesRebootIntegration` in the phases
  `create`, `resized` and `removed`, and `TestSwapModesRebootIntegration` in
  `zram`, `zswap` and `disk` (`LINUXIO_MEMORY_REBOOT_PHASE`), with a reboot
  between two phases. At the start the suite removes what a cut run left
  (`memory_leftovers`): the tests' files and their fstab lines, LinuxIO's
  own swap file and `vdb` swapping. The whole target takes about nine
  minutes on Ubuntu and seven on Fedora. These changes stay in the VM until
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
| `PACKAGES_DIR` | `$(BIN_DIR)/packages` | Where `package-deb`, `package-rpm` and `localinstall` put the packages, and where `test-packages`, `repo-apt` and `repo-rpm` read them; `make clean` removes the LinuxIO packages in it. |
| `RELEASE_NOTES` | none | The release notes (Markdown) `package-deb` and `package-rpm` write into the packages' changelog; without them it names the version alone. |
| `REPO_DIR` | `$(BIN_DIR)/repo` | The signed repositories `repo-apt` and `repo-rpm` add to, `apt/` and `rpm/`. |
| `REPO_SIGNING_KEY` | none | The key in the GnuPG keyring that `repo-apt` and `repo-rpm` sign with; required by both. |
| `REPO_BASE_URL` | none | Where `REPO_DIR` is published; the repository files point apt and dnf at it. Required by `repo-apt` and `repo-rpm`. |
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
| `NODE_VERSION` | the pin in `packaging/build/pins.env` | Node version installed by `ensure-node`, a full `x.y.z` release. |
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
| `GOLANGCI_LINT_VERSION` | the pin in `packaging/build/pins.env` | golangci-lint version installed by Make. |
| `MODERNIZE` | `$(GO_TOOLS_DIR)/bin/modernize` | modernize executable. |
| `MODERNIZE_VERSION` | the pin in `packaging/build/pins.env` | modernize version installed by Make. |
| `DEADCODE` | `$(GO_TOOLS_DIR)/bin/deadcode` | deadcode executable. |
| `DEADCODE_VERSION` | the pin in `packaging/build/pins.env` | deadcode version installed by Make. |
| `GOVULNCHECK` | `$(GO_TOOLS_DIR)/bin/govulncheck` | govulncheck executable. |
| `GOVULNCHECK_VERSION` | the pin in `packaging/build/pins.env` | govulncheck version installed by Make. |
| `GOLANGCI_LINT_OPTS` | `--modules-download-mode=mod` | Additional golangci-lint options. |

Go is not installed by Make: any Go 1.21 or newer on PATH works (linux-toolbox
installs it to `/usr/local/go`), and `GOTOOLCHAIN` makes Go download the exact
version `go.mod` pins. `SKIP_ENSURE_GO=1` is accepted for compatibility.
`CODEQL_ACTION_GO_BINARY` is honored automatically when present.

### Test, analysis, and build behavior

| Variable | Default | Purpose |
| --- | --- | --- |
| `GO_TEST_PKGS` | `./...` | Go packages (relative to `backend/`) for `test-go`, for example `./bridge/handlers/filebrowser/...`. |
| `GO_BENCH`, `GO_BENCH_TIME` | `.`, `1x` | The benchmarks `bench-go` runs in `GO_TEST_PKGS` and for how long. It runs without the race detector `test-go` uses, which slows code several times over and says nothing about speed. |
| `GO_TEST_FLAGS` | empty | Extra flags for backend tests; `-count=1` disables Go test caching. `test-go` builds with the `sqlite_fts5` and `sqlite_omit_load_extension` tags `build-indexer` uses and, on amd64, the `glibc` tag `build-monitoring` uses, so the indexer's FTS5 search and the NVML collector are tested; `golint-only` adds `glibc` too. A `-tags` given here replaces both. |
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
| `Ctrl+Shift+3` | `linuxioDevStatus.reinstall` | Asks for the sudo password in a masked prompt, then runs `make localinstall` in a visible terminal. |

`make localinstall` calls `sudo` from the Makefile, and `sudo` caches credentials
per terminal, so priming it first would not help. The reinstall command instead
runs `make localinstall SUDO='sudo -A'` (`SUDO` defaults to `sudo` in
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
- `PACKAGE_VERSION` is the version `package-deb`, `package-rpm`,
  `localinstall` and the test VMs' install give the package; from git unless
  given.
- `LINUXIO_CWRAP_LIBDIR` points `test-auth-pam` at wrapper libraries installed
  outside standard system library directories.
- `CFLAGS`, `LDFLAGS`, `GOOS`, and `GOARCH` remain standard toolchain
  overrides and are passed through where applicable.

## Releases

A release is always made from a `dev/vX.Y.Z` branch: merging its pull request
into `main` starts the release workflow (`.github/workflows/release.yml`),
which tags the merge commit, builds the packages and publishes them. See
[releases.md](releases.md).

## Dependency updates

Dependabot (`.github/dependabot.yml`) opens the routine updates as pull
requests into `main`. Each week, it opens at most one grouped pull request for
GitHub Actions (`ci(deps): …`) and one each for the Go modules in `backend/`
and the npm packages in `frontend/` (`build(deps): …`). The grouped pull
requests hold minor and patch versions; a new major version arrives in a pull
request of its own. A version must be at least seven days old before
Dependabot proposes it; security updates skip that wait. The targets below
are for updating by hand.

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

Run `make update-build-pins` to move what a release is built from: the two
build images' digests, Node, the Go check tools and the commit each GitHub
Action is pinned to (`packaging/build/pins.env`, `.github/workflows`). It
shows what changed and prints a commit message, and changes nothing when all
are current ([Releases](./releases.md)).

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
  `update-deps`, `update-go-deps`, `update-build-pins`
- frontend checks: `lint`, `lint-only`, `tsc`, `tsc-only`, `test-frontend`,
  `test-frontend-only`, `lint-ci`, `tsc-ci`, `test-frontend-ci`,
  `check-frontend`, `setup-frontend-browser`, `test-frontend-browser`,
  `update-frontend-screenshots`
- backend checks: `golint`, `golint-only`, `test-backend`, `test-go`, `deadcode`,
  `deadcode-only`, `check-backend`, `test-auth`, `test-auth-protocol`,
  `test-auth-pam`, `test-docker-update-integration`,
  `test-assistant-agent-integration`,
  `test-filebrowser-root`, `check-c-build-deps`
- analysis: `bundle-metrics`, `compiler-coverage`, `analyze`, `analyze-auth`
- builds and development: `test`, `build`, `build-nocheck`, `fastbuild`,
  `build-vite`, `build-backend`, `build-bridge`, `build-auth`, `build-cli`,
  `build-docker-update`, `build-leak-profile`, `third-party-notices`,
  `generate`, `dev-prep`, `dev`, `dev-stop`, `dev-status-install`,
  `dev-status-uninstall`, `clean`, `cloc`
- installation: `localinstall`, `package-root`, `package-deb`,
  `package-rpm`, `test-packages`, `test-packaging-scripts`, `repo-apt`,
  `repo-rpm`
- test VM: `test-vm-browser`, `vm-create`, `vm-install`, `vm-ssh`, `vm-stop`, `vm-reset`, `vm-update`, `vm-update-build`, `vm-destroy`,
  `vm-test-btrfs-integration`, `vm-test-memory-integration`,
  `vm-test-filebrowser-root`, `vm-test-relay-root`, `test-vm-backend`,
  `test-vm-browser-rerun`, `test-vm-all`
- documentation: `help-overrides`, `check-docs` (the docs index in
  `docs/README.md`, and the paths and links docs name)

`_build-binaries` is an internal implementation target. It remains callable
for diagnostics but is not part of the normal user workflow.
