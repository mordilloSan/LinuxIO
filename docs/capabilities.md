# Capabilities

This is the canonical guide for LinuxIO's **capability system** — how the app
detects optional host tooling (Docker, NFS, rsync, WireGuard, lm-sensors, …), gates
features on it, and installs it from the UI.

## Summary

- A capability is a named, optional host dependency (a binary, package, or
  service) that some feature needs in order to work.
- Each capability is named **once**, in the backend `capabilityRegistry` in
  `backend/bridge/handlers/capabilities/registry.go`. The registry's names are
  the generated `CapabilityName` union, and the frontend `CAPABILITIES` array
  in `frontend/src/api/capabilities.ts` gives each of those names its label,
  icon and auth-state key; typecheck fails when the two differ.
- `system.get_capabilities` reports, for every capability, its status under its
  name: an `available` boolean and an optional `error` string. It is a
  post-login UI hint, not part of the login JSON or an authorization decision:
  once the authenticated session mux opens, `AuthProvider` invokes the RPC
  asynchronously once for the frontend authentication lifecycle and persists
  valid results for route/action gating.
- Installable capabilities expose an `Install` spec; `system.install_capability`
  installs the package and/or enables the service, then re-detects.

## Wire Shape

`system.get_capabilities` returns an object keyed by capability name:

```json
{
  "docker": { "available": true },
  "wireguard": {
    "available": false,
    "error": "wg-quick not found (missing wireguard-tools dependency)"
  }
}
```

- `available` — `true` only when the dependency is present and usable.
- `error` — the reason detection gave, present only when it gave one.

In Go the result is `apischema.CapabilitiesResponse`, a
`map[CapabilityName]CapabilityStatus`; the generator renders it as
`Partial<Record<CapabilityName, CapabilityStatus>>`. `apischema` cannot import
the handler package that owns the registry, so
`handlers/capabilities` adds the registry's names to `apischema.StringEnums`
as `CapabilityName` when it is loaded; the generator loads it through the
handler registry like every route family.

The frontend collapses this into a tri-state per capability: `true` (available),
`false` (unavailable), or `null` (unknown / not yet checked). On an authenticated
reload, cached values may seed the UI while the session refresh runs. A new
sign-in clears the prior capability cache so results cannot leak between users;
the asynchronous refresh then replaces the unknown values. These booleans only
control UI availability; backend handlers continue to enforce authorization.

### Session and bootstrap lifecycle

`StartBridge` creates and attaches the authenticated bridge without waiting for
`system.get_capabilities`, and the login response does not include capability
fields. `AuthProvider` starts the authenticated mux eagerly, then performs one
best-effort capability refresh per authenticated frontend bootstrap after the
mux opens. Completion is ignored when the user/authentication generation is
stale, and only valid results are persisted. Callers may explicitly refresh
after an install; that is separate from the initial bootstrap refresh.

## Backend Pieces

| Symbol | File | Role |
|--------|------|------|
| `capabilityRegistry` | `handlers/capabilities/registry.go` | Source of truth: one `CapabilitySpec` per capability. |
| `CapabilitySpec` | `handlers/capabilities/registry.go` | `Name` (wire key), `LogName`, `Detect`, optional `Install`. |
| `InstallSpec` | `handlers/capabilities/registry.go` | Package/service to install per distro family. |
| `init` | `handlers/capabilities/registry.go` | Publishes the registry's names as the `CapabilityName` enum. |
| `buildCapabilitiesResponse` | `handlers/capabilities/response.go` | Runs every detector and keys each status by its name. |
| `CapabilitySpecByName` | `handlers/capabilities/registry.go` | Lookup used by the install runner. |
| `CapabilitiesResponse`, `CapabilityName`, `CapabilityStatus` | `apischema/capabilities.go` | Exported API contract types, reflected into TypeScript. |
| `runInstallCapabilityJob` | `handlers/packages/install_capability.go` | The `system.install_capability` runner. |

### Detection

`CapabilitySpec.Detect` returns `(ok bool, errMsg string)`. Use the helpers so
the error/unavailable shaping stays consistent:

- `checkedCapability(ok, err)` — wraps a `(bool, error)` result.
- `checkedCapabilityErr(ok, err, unavailable)` — same, with a sentinel error to
  report when `ok` is false but there is no concrete error.
- `checkDependencyCommand(command, dependencyName)` — the common case: a binary
  on `$PATH`. Returns a `"<command> not found (missing <dependencyName> dependency)"`
  error when absent.

Detection lives next to the feature where possible (e.g.
`CheckDockerAvailability` in `handlers/docker`, `CheckNFSClientAvailability` in `handlers/storage`; the registry calls them), or is
inlined for a plain binary check:

```go
{
    Name:    "wireguard",
    LogName: "WireGuard tools",
    Detect: func(_ context.Context) (bool, string) {
        return checkedCapability(checkDependencyCommand("wg-quick", "wireguard-tools"))
    },
    Install: &InstallSpec{PackageDebian: "wireguard-tools", PackageRHEL: "wireguard-tools"},
},
```

### Tests

`TestCapabilityRegistryEntriesAreValid` (`registry_test.go`) asserts that every
registry name is non-empty and unique and that each entry sets exactly one
detector; `TestCapabilityNamesArePublished` asserts that the `CapabilityName`
enum is the registry's names. `TestBuildCapabilitiesResponseReportsEveryCapability`
(`response_test.go`) runs every registry name through a stub detector and
checks that each one keys its own status, with its error, in the JSON
response. On the frontend, `capabilities.test.ts` asserts at typecheck that the
`CAPABILITIES` names equal the generated `CapabilityName` union, so a
capability added to the registry without a frontend entry, or the reverse,
fails `make tsc-only` after `make generate`.

## Install Flow

Installable capabilities set an `InstallSpec`. The runner
`system.install_capability` (privileged, job mode, in the `packages` package)
does the work and streams per-stage progress:

```text
resolve -> [install_package] -> [post_install] -> [enable_service] -> [start_service] -> wait_service_active -> detect
```

`post_install` is the step after the package of the spec's package hook,
where the package installed is `PackageDebian` (the hook also prepares just
before `install_package`), and then its `PostInstall` command. The hook's
step after the package runs whatever the package step's outcome, a
failure or a cancel included, on a context the cancel does not reach,
bounded at 30 seconds. An error of that step fails the job: beside the
package step's error, on one line, or as "`<package>` was installed: …".
A job whose context was cancelled reports only that it was cancelled, so
then the step's error is also logged as a Warn line,
`package hook failed after a cancelled install`; either way the install
does not go on and the job does not end completed.

- `detectDistroFamily()` reads `/etc/os-release` and classifies the host as
  `debian` or `rhel`; `pickByFamily` chooses the matching package/service name.
- The monitoring daemon ships with LinuxIO and is not installed through
  Capability Manager; its capability reports daemon health. The filesystem
  indexer ships with LinuxIO the same way.
- Package installs go through PackageKit (`InstallByNameWithProgress`), so
  installable capabilities that have a package step require PackageKit to be
  available.
- PackageKit keeps its own package cache, apart from apt's or dnf's, and on a
  host where nothing has refreshed it it knows no package to install: on
  Debian and Ubuntu while apt's lists are missing, and on Fedora after a dnf
  transaction made outside PackageKit before it first ran. A package name it
  does not find, whether it answers with no match or with its
  package-not-found error, is looked up again after one refresh of that
  cache, the same refresh as the Updates page's button and bounded at two
  minutes; the capability row shows
  `Installing zram-generator (Refreshing the package lists)` meanwhile. Only
  then does the job fail with not found (404), and a refresh that fails is
  reported beside it. A name that resolves installs without a refresh, so a
  warm cache costs nothing. In the test VM
  (`make test-vm-backend SUITES=packagekit`, on 2026-11-18) both backends
  answered with no match, and the install refreshed once and installed the
  package, on Ubuntu 26.04 (PackageKit 1.3.4, apt) and Fedora 45
  (PackageKit 1.4.0, dnf).
- A distro-specific optional package may report a warning without preventing
  later service and detection stages. Avahi uses this on RHEL-family hosts for
  `nss-mdns`, which may require EPEL; the responder is still enabled, started,
  and re-detected if that optional package is unavailable.
- Service steps use `systemd.EnableUnit` / `StartUnit`, then `waitUnitActive`.
- Finally `detectWithRetry` re-runs `Detect` for a few seconds to cover the gap
  between a service going `active` and its surface (D-Bus name, socket) being
  reachable. The job result is the freshly re-detected
  `{available, error, warning}`. `error` describes detection or availability;
  `warning` describes non-fatal optional setup, or what a package hook left
  as the package made it.
- On the frontend every install job that ends, completed, failed or
  cancelled, has the capabilities read again (`tasks/terminalTaskFeedback.ts`):
  a job that failed or was cancelled after its package step still put the
  package in, and the Capabilities row would otherwise keep reading
  unavailable.

`InstallSpec` fields:

| Field | Meaning |
|-------|---------|
| `PackageDebian` / `PackageRHEL` | Package name per family (empty = no package step). |
| `OptionalPackageRHEL` | Best-effort RHEL-family package whose failure does not stop later stages. |
| `OptionalPackageRHELFailureWarning` | Concise capability-specific consequence returned to the UI; raw package-manager errors remain in task output. |
| `ServiceDebian` / `ServiceRHEL` | systemd unit to start after install (empty = none). |
| `EnableService` | Also `systemctl enable` the unit, not just start it. |
| `PackageHookDebian` | When `PackageDebian` is the package installed, a `PackageHook` the feature package provides: it prepares the host before the package step and returns a step the installer always runs after it, reported as `post_install`, whose warning goes into the result. For a package whose own scripts change the host as it installs; zram's keeps `zram-tools` from starting zram (see [zram](#zram)). A step that fails after the package went in fails the job, saying the package was installed; after a failed package step its error joins the package's. |
| `OptionalComponent` | Names a LinuxIO-managed non-package installer, run before any package step. `"docker"` downloads Docker's convenience script from `https://get.docker.com`, reports its sha256, and streams its output (skipped when a `docker` binary is already on `PATH`). `"node"` downloads the pinned nvm install script and runs it, then `nvm install --lts`, as the session user (see [Node.js](#nodejs-assistant)). `"claude_code"` adds Anthropic's package repository after checking its signing key, and `"assistant_<agent>"` installs that agent's pinned npm package for the session user (see [Claude Code and the Assistant agents](#claude-code-and-the-assistant-agents)). `handlers/packages` rejects any other value. |
| `RequiresDocker` | Optional-component prerequisite checked before install. |

Omit `Install` entirely for capabilities with no UI install path (the
PackageKit capability itself).

## Frontend Pieces

| Symbol | File | Role |
|--------|------|------|
| `CAPABILITIES` | `api/capabilities.ts` | One `CapabilityDef` per `CapabilityName`: label, icon, auth-state key, manager text. |
| `CapabilityKey`, `CapabilityState`, … | `api/capabilities.ts` | Types derived from `CAPABILITIES`. |
| `capabilityStateFromWire` | `api/capabilities.ts` | Maps the wire response into tri-state auth state. |
| `useCapability` | `hooks/useCapabilities.ts` | Per-capability `{ status, isEnabled, reason }`. |
| `hasAccessPolicy` / `useAccessContext` | `hooks/useCapabilities.ts` | Evaluate a route's access policy. |
| `AuthProvider` | `contexts/AuthProvider.tsx` | Seeds cached state on reload; refreshes `get_capabilities` asynchronously after mux open, persists valid results, and exposes state. |
| `CapabilityManagerSection` | `routes/_authenticated/settings/-components/CapabilityManagerSection.tsx` | Lists capabilities, shows status, offers Install, and links an available capability to its page (`route`, which its install toast opens too), unless the route needs sudo the session lacks (`routeRequiresPrivileged`). |

A `CapabilityDef` (see the interface in `api/capabilities.ts` for the full set):

```ts
{
  wire: "wireguard",          // CapabilityName: the registry name keying its status
  state: "wireguardAvailable",// camelCase key in auth state
  label: "WireGuard",
  description: "Create and manage WireGuard VPN interfaces",
  readyText: "wg-quick command is available.",
  dependency: "wg-quick",
  icon: "simple-icons:wireguard",
  reasonUnknown: "WireGuard tools availability is still being checked.",
  reasonUnavailable: "WireGuard tools are unavailable.",
  installable: { requiresPackageKit: true }, // omit if not installable
}
```

Adding the entry automatically: derives the state types, adds the `<state>`
field to auth state, and registers the row (with an Install button if
`installable`) in the Capability Manager.

## rsync backups

The `rsync` capability checks for the `rsync` executable after sign-in and
installs the distro's `rsync` package through the existing privileged
PackageKit workflow. Installation does not expose a folder. Use **Storage →
rsync** to configure the read-only module on port 873 and start its daemon,
or use the existing SSH listener on port 9222 with Linux account credentials. See
[TerraMaster backups](rsync-backups.md) for the UI workflow and service details.

## Btrfs maintenance

The `btrfs_maintenance` capability checks for the `btrfsmaintenance` scripts
(`/usr/share/btrfsmaintenance/btrfs-scrub.sh`) and installs the distro's
`btrfsmaintenance` package. Debian installs it with every timer disabled, so
installation alone changes nothing on the host. Expanding a btrfs filesystem
card in **Storage** then shows one Off/Daily/Weekly/Monthly select per task
(scrub, balance, trim, defrag). `storage.set_btrfs_maintenance` rewrites only
that task's `BTRFS_*_PERIOD` and `BTRFS_*_MOUNTPOINTS`/`_PATHS` lines in
`/etc/default/btrfsmaintenance` (or `/etc/sysconfig/btrfsmaintenance`) and
starts the package's `btrfsmaintenance-refresh.service`, which enables or
disables the matching `btrfs-<task>.timer`. The period is shared by every
mountpoint a task targets; turning a task off for the last mountpoint sets its
period to `none`, and a task whose period is `none` reports no mountpoints so
the package's default of `/` does not leak into a schedule the user enables.

## Btrfs tools

The `btrfs` capability checks for the `btrfs` executable (`btrfs-progs`) and
installs the distro's `btrfs-progs` package through the existing privileged
install path. It gates the **Storage → Subvolumes** tab (`/storage/subvolumes`),
listed once a btrfs filesystem is mounted, which manages subvolumes,
snapshots and replication, and the pools on the
Disks overview, which manage devices, health and settings; see
[Btrfs management](./btrfs.md).

## zram

The `zram` capability is available when either tool that sets zram up is
installed (`memory.CheckZramAvailability`): the `zramswap` command of
`zram-tools`, also looked for in `/usr/sbin` for a bridge whose `PATH` omits
it, or systemd's `zram-generator` at
`/usr/lib/systemd/system-generators/zram-generator`. Install uses the Debian
package `zram-tools` or the RHEL package `zram-generator`. There is no
service step, and on a host without either tool an install that runs to
its end starts no zram and sets none to start at boot: nothing about the
host's swap changes until the admin chooses a mode in the dashboard's
**Memory overflow** dialog, which starts zram when compressed memory only
is chosen. The exceptions are below and under
[Known limits](#known-limits).

On Fedora `zram-generator` comes without `zram-generator-defaults`, and its
package starts nothing (Fedora 45 test VM, 2026-11-18), so the RHEL family
has no hook. On Debian and Ubuntu the `zram-tools` package's `postinst`, as
`dh_installsystemd` writes it, enables `zramswap.service` and starts it, so
zram would swap at zram-tools' defaults (half the memory, 1955 MiB in the
4 GiB Ubuntu 26.04 test VM) before the admin chose anything. The
capability's package hook (`PackageHookDebian`, provided by
`memory.ZramToolsInstallHook`) keeps that start from happening, unless
zram-tools was installed already, whose package then starts nothing new:

1. Before the package step it takes the lock every memory change takes and
   holds it until the step after the package ends
   ([One change at a time](./memory.md#one-change-at-a-time)), so no mode
   change runs while the package installs. It reads whether
   `zramswap.service` starts at boot (a unit no unit file defines does
   not), then writes a runtime drop-in,
   `/run/systemd/system/zramswap.service.d/90-linuxio-memory.conf`, with
   `ConditionPathExists=!/`, and has systemd reload.
2. The package installs. Its `deb-systemd-helper enable` enables the unit
   and records that it did, and its start is skipped: the journal reads
   `zramswap.service - Linux zramswap setup skipped, unmet condition check
   ConditionPathExists=!/`. No zram swaps while the hold is in place; a
   cancel can let the package's scripts run after it is gone (see
   [Known limits](#known-limits)).
3. After the package step, whatever its outcome, the hook disables
   `zramswap.service` when it is enabled now and was not before the
   install, removes the drop-in and its folder, has systemd reload, and
   releases the lock. It logs one Info line, `zram service kept off during
   install`, with `disabled`.

The hold is a condition drop-in and not a mask. Under a runtime mask the
package's start is skipped too, but its `enable` is as well, so
`deb-systemd-helper` records no state for the unit, and the next upgrade of
the package, seen as a reinstall in the test VM (Ubuntu 26.04,
init-system-helpers 1.69), enabled and started it, with zram swapping. With the drop-in a reinstall leaves the unit `disabled` and
`inactive`, as it does after any `systemctl disable`. `zramswap.service`
is wanted by `multi-user.target` only (`RequiredBy` is empty), so holding
it holds nothing else back; the rule that a swap unit is held off with a
condition and never a mask, since `swap.target` requires it
([Memory overflow](./memory.md#holding-a-swap-file-off-at-boot)), is not
in play, and a condition is what is used anyway. An admin's own
`zramswap.service` that is enabled and running, on a host where zram-tools
was never installed, stays so (seen in the test VM): the hold does not stop
a running unit, and its start at boot was the admin's before the install,
so it is not disabled. One exception follows from the package's scripts and
was not seen on a host: with zram-tools removed but not purged (dpkg's
`config-files` state), the `postinst` runs as an upgrade and restarts the
unit; the stop runs and the start is skipped under the hold, so such a unit
stays stopped until it is started again or the next boot.

The hook changes nothing, and the install's `warning`, which the install
dialog shows, says why, when another memory change is running (the package
then turns zram on now and at every boot) or when a file LinuxIO did not
write is at the drop-in's path (the package may then turn zram on, since
that file may hold the unit itself). A drop-in a killed job host left there
is LinuxIO's by its content: the next install takes it over and removes
it, and so does the zram-tools start step of the next switch to compressed
memory ([`start_zram`](./memory.md#order-of-steps)), before it starts
`zramswap.service`. A step after the package that fails says what the host is left
with: a disable that failed means `zramswap.service` starts zram at the
next boot, a removal or reload that failed that it cannot start until
then.

The dialog
itself does not wait for the capability; its zram row links to Capabilities
while the package is missing. The capability's `route` links back: once
the capability is available its row in Settings, and the toast that reports
the install from any page but the dashboard, open the dialog by its URL
(`/?dialog=memory-overflow`, "Open Memory overflow"). The dialog opens only
for a session with sudo, so the route is flagged `routeRequiresPrivileged`,
as the VMs, WireGuard and rsync routes are, and its link on the row is
hidden for any other session.
The two tools differ in configuration file and units; see
[Memory overflow](./memory.md#the-zram-provider).

`frontend/src/test/vm/capability-install.spec.ts` makes that round in the
test VM: it takes the distribution's zram tool out with its package manager,
follows the dialog's link to Capabilities, installs, waits for the job, and
follows the row's link back to the dialog, which must name the tool as the
provider. On Ubuntu it also checks that after the install no zram swaps,
`zramswap.service` reads `disabled` and `inactive`, and its journal for
this boot since Install was clicked shows the skipped start and no start. On 2026-11-18 it passed twice on Ubuntu
26.04 (`zram-tools` in 5 s, with no refresh), before any hook, with zram
left swapping. With the hold it passed the same day, the install taking
6 s: the unit's journal held only
`zramswap.service - Linux zramswap setup skipped, unmet condition check ConditionPathExists=!/`,
the job host logged `zram service kept off during install` 3 s later, and
no zram swapped. It passed again with the spec asserting that skipped
start itself, the install again taking 6 s. On Fedora 45, with SELinux enforcing and LinuxIO as
its installer puts it ([SELinux](./process-systemd-architecture.md#selinux)),
it passed after a fresh install and again after a reboot, installing
`zram-generator` in 3 s each time; after the reboot the install showed
"Refreshing the package lists" first.

## Filesystem tools

The `e2fsprogs` and `xfsprogs` capabilities check for `mkfs.ext4` and
`mkfs.xfs` and install the distro package of the same name on Debian and RHEL
families. Formatting a partition in **Storage → Disks** needs the tool of the
chosen type; the `btrfs` capability above covers `mkfs.btrfs`.

## Node.js (Assistant)

The `node` capability resolves `npx` on the PATH the session user's
interactive login shell reports, through `DetectAsUser` rather than `Detect`,
because version managers such as nvm install under the home directory, add
themselves in `~/.bashrc`, and never appear on the bridge's `PATH`. Installing
it from the UI runs the pinned nvm install script and then
`nvm install --lts && nvm alias default 'lts/*'` as the session user, never
as root, so Node.js lands in that user's `~/.nvm` and nvm adds itself to their
`~/.bashrc`; verification then re-runs the same login-shell probe as that
user. A host-wide Node.js from the package manager also satisfies the probe.
It gates the Assistant route and its settings tab; the agents themselves are
installed with its npm and run on its `node`.

`DetectAsUser(ctx, user)` is the variant for capabilities that depend on the
session user's own environment; every other capability keeps `Detect(ctx)`.
Exactly one of the two is set, which the registry test enforces.

## Claude Code and the Assistant agents

Four capabilities cover what the [Assistant](./AI-assistant.md) runs. None of
them is installed or updated by opening a chat.

`claude_code` resolves `claude` on the session user's login-shell PATH with
`DetectAsUser`, the PATH the Claude agent runs it from, so a copy under the
home directory counts as much as the system package. Installing it from the UI
makes Anthropic's own repository known to the package manager and then
installs `claude-code` through PackageKit like any other package:

- The signing key is downloaded from
  `https://downloads.claude.ai/keys/claude-code.asc` and refused unless it
  holds exactly one primary key whose fingerprint is
  `31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`, the one Anthropic publishes with
  its install instructions. A package manager trusts every key in the file, so
  a second key is refused as well, and so is anything another parser might
  read differently: text around the one armored block, lines that are not
  armor headers, data after the checksum, and any packet that is not part of
  a public key (a secret key above all). Nothing is written for a refused key.
- Debian family: the key goes to `/etc/apt/keyrings/claude-code.asc` and
  `/etc/apt/sources.list.d/claude-code.list` gets
  `deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/latest latest main`.
- RHEL family: the key goes to `/etc/pki/rpm-gpg/RPM-GPG-KEY-claude-code`, is
  imported with `rpm --import` so no fingerprint prompt appears later, and
  `/etc/yum.repos.d/claude-code.repo` points at
  `https://downloads.claude.ai/claude-code/rpm/latest` with `gpgcheck=1`.
- The channel is `latest`, which offers every release as it ships. A
  repository file that already exists is left unchanged, so an administrator
  who chose `stable` keeps it.
- The package lists are refreshed before the package step so `claude-code`
  resolves.

Installed this way Claude Code does not update itself: new versions appear on
the Updates page with the rest of the system's packages.

`assistant_claude`, `assistant_gemini` and `assistant_codex` each report, with
`DetectAsUser`, whether the version of that agent pinned in
`backend/bridge/internal/acpagents` is installed under
`~/.local/share/linuxio/agents/<id>` for the session user. Install runs
`npm install` of that exact version as the session user, never as root, into a
staging directory that replaces the old one only on success. Detection and
launch read that directory through an `os.Root`, so no link in it leads the
bridge elsewhere, and accept only a regular manifest of ordinary size. A
different installed version reports
`<package> <installed> is installed, LinuxIO needs <pinned>`, which is how a
LinuxIO update that moves a pin asks for the agent to be installed again.

## Consuming A Capability

There are two established patterns. Pick based on whether the *whole feature* or
just *some actions* depend on the tool.

### 1. Whole-route gating (Docker, Hardware, VMs, WireGuard)

Define one typed policy beside the file route, use it in `beforeLoad`, and
expose the same policy through `staticData`. The guard denies direct
navigation; the sidebar reads `staticData` and hides inaccessible routes:

```tsx
const access = {
  requiredCapabilities: ["libvirtAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/vm")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.virt.list,
      linuxio.virt.preflight({}),
    ]),
  component: VMPage,
  staticData: {
    access,
    navigation: {
      icon: VirtualMachineIcon,
      position: 55,
      title: "VMs",
    },
  },
});
```

Users discover and install the missing tool from the Capability Manager in the
navbar.

Settings also hides the Docker and Power tabs until `dockerAvailable` and
`tunedAvailable`, respectively, are true. Power still requires a privileged
session. Direct links to unavailable tabs display General without mounting or
fetching the hidden section; a refreshed capability scan updates the tabs.
TuneD detection includes an installed, stopped systemd unit, so Power remains
available to start it. Installation guidance stays in Capabilities instead of
an unavailable-service banner inside Power.

### 2. In-page gating (NFS within Storage)

Keep the page visible, but warn and disable the dependent actions. Use
`useCapability`:

```tsx
const { reason, status } = useCapability("nfsClientAvailable");
const unavailable = status === "unavailable";

{unavailable && <AppAlert severity="warning">{reason}</AppAlert>}
// ...disable the dependent buttons, using `reason` as the tooltip.
```

This is right when the page still has value without the tool (e.g. viewing
existing entries) while specific mutations must be blocked.

## Adding A Capability — Checklist

Worked example: the `wireguard` capability.

1. **Detect** — reuse a `CheckXAvailability` in the feature package, or inline
   `checkDependencyCommand` for a plain binary.
2. **Backend registry** — add a `CapabilitySpec` (with `Install` if applicable)
   to `capabilityRegistry`. Its `Name` is the capability's name everywhere:
   the response key, the `CapabilityName` value and the install request.
3. **Generate** — run `make generate`, which adds the name to the
   `CapabilityName` union.
4. **Frontend manifest** — add a `CapabilityDef` with that name as `wire` to
   `CAPABILITIES` in `api/capabilities.ts`; typecheck fails until it exists.
5. **Gate** — add `requiredCapabilities: ["xAvailable"]` to the route, and/or
   gate in-page with `useCapability`.

## Verification

```bash
make generate                              # regenerates the TS contract
make test-go-quiet GO_TEST_PKGS=./bridge/handlers/capabilities # registry + detection tests
make tsc-only                              # derived capability types compile
```

Manual end-to-end: with the dependency absent, the feature is gated and the
Capability Manager shows it unavailable (with an Install button if installable);
installing it and refreshing capabilities re-enables the feature. For zram the
test VM does this in a browser:
`make test-vm-browser VMS=1 PLAYWRIGHT_ARGS=capability-install.spec.ts`
(see [zram](#zram)).

## Known limits

- A package that no enabled repository has costs one cache refresh before the
  install reports it, including an optional package such as `nss-mdns` on a
  RHEL-family host without EPEL, whose warning comes after that refresh.
- Only an install from Capabilities keeps zram-tools from starting zram:
  one made with apt leaves zram swapping at zram-tools' defaults and
  `zramswap.service` enabled, as the package sets them up, until the admin
  chooses a mode in Memory overflow.
- A job host killed during a zram-tools install, between the hold and its
  removal, leaves the hold in `/run` until the next boot or the next switch
  to compressed memory, which removes it before it starts zram. When the
  kill came after the package went in and before the disable, the service
  is enabled and starts zram at the next boot.
- A cancel while PackageKit's transaction still runs can let the package's
  scripts run after the hold is gone, leaving zram swapping and the service
  enabled until a mode is chosen: on a cancel the install does not wait for
  the transaction, and the step after the package removes the hold and
  frees the lock at once.
