# Capabilities

This is the canonical guide for LinuxIO's **capability system** — how the app
detects optional host tooling (Docker, NFS, rsync, WireGuard, lm-sensors, …), gates
features on it, and installs it from the UI.

## Summary

- A capability is a named, optional host dependency (a binary, package, or
  service) that some feature needs in order to work.
- Each capability is declared **once per side** in a manifest:
  - Backend: the `capabilityRegistry` in `backend/bridge/handlers/capabilities/registry.go`.
  - Frontend: the `CAPABILITIES` array in `frontend/src/api/capabilities.ts`.
- `system.get_capabilities` reports, for every capability, an `*_available`
  boolean and an optional `*_error` string. It is a post-login UI hint, not
  part of the login JSON or an authorization decision: once the authenticated
  session mux opens, `AuthProvider` invokes the RPC asynchronously once for the
  frontend authentication lifecycle and persists valid results for route/action
  gating.
- Installable capabilities expose an `Install` spec; `system.install_capability`
  installs the package and/or enables the service, then re-detects.
- An anti-drift test keeps the backend registry and wire struct in lock-step.

## Wire Shape

`system.get_capabilities` returns one pair of fields per capability:

```json
{
  "docker_available": true,
  "wireguard_available": false,
  "wireguard_error": "wg-quick not found (missing wireguard-tools dependency)"
}
```

- `<wire>_available` — `true` only when the dependency is present and usable.
- `<wire>_error` — human-readable reason, present only when unavailable.

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
| `CapabilitySpec` | `handlers/capabilities/registry.go` | `Name` (wire prefix), `LogName`, `Detect`, optional `Install`. |
| `InstallSpec` | `handlers/capabilities/registry.go` | Package/service to install per distro family. |
| `CapabilitiesAvailable` / `CapabilitiesError` | `common/session/session.go` | Shared wire/session capability fields. |
| `setCapabilityField` | `handlers/capabilities/response.go` | Maps a wire name to its struct fields. |
| `buildCapabilitiesResponse` | `handlers/capabilities/response.go` | Iterates the registry and fills the struct. |
| `CapabilitySpecByName` | `handlers/capabilities/registry.go` | Lookup used by the install runner. |
| `CapabilitiesResponse` | `apischema/capabilities.go` | Exported API contract type (pointer errors), reflected into TypeScript. |
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

### Anti-drift test

`TestCapabilityRegistryCoversWireFields` (`registry_test.go`) asserts that
every `<prefix>_available` field on `session.CapabilitiesAvailable` has a
matching registry entry and vice versa. `TestSetCapabilityFieldRoundTrips`
checks that `setCapabilityField` writes only the intended capability's fields.
Forgetting any of the three touch points (struct field, registry entry, switch
case) fails the build.

> The exported `apischema.CapabilitiesResponse` embeds the shared session
> structs. `make generate` + `make tsc-only` verify the field appears in the
> generated frontend contract.

## Install Flow

Installable capabilities set an `InstallSpec`. The runner
`system.install_capability` (privileged, job mode, in the `packages` package)
does the work and streams per-stage progress:

```text
resolve -> [install_package] -> [post_install] -> [enable_service] -> [start_service] -> wait_service_active -> detect
```

- `detectDistroFamily()` reads `/etc/os-release` and classifies the host as
  `debian` or `rhel`; `pickByFamily` chooses the matching package/service name.
- The monitoring daemon ships with LinuxIO and is not installed through
  Capability Manager; its capability reports daemon health. The filesystem
  indexer ships with LinuxIO the same way.
- Package installs go through PackageKit (`InstallByName`), so installable
  capabilities that have a package step require PackageKit to be available.
- A distro-specific optional package may report a warning without preventing
  later service and detection stages. Avahi uses this on RHEL-family hosts for
  `nss-mdns`, which may require EPEL; the responder is still enabled, started,
  and re-detected if that optional package is unavailable.
- Service steps use `systemd.EnableUnit` / `StartUnit`, then `waitUnitActive`.
- Finally `detectWithRetry` re-runs `Detect` for a few seconds to cover the gap
  between a service going `active` and its surface (D-Bus name, socket) being
  reachable. The job result is the freshly re-detected
  `{available, error, warning}`. `error` describes detection or availability;
  `warning` describes non-fatal optional setup.

`InstallSpec` fields:

| Field | Meaning |
|-------|---------|
| `PackageDebian` / `PackageRHEL` | Package name per family (empty = no package step). |
| `OptionalPackageRHEL` | Best-effort RHEL-family package whose failure does not stop later stages. |
| `OptionalPackageRHELFailureWarning` | Concise capability-specific consequence returned to the UI; raw package-manager errors remain in task output. |
| `ServiceDebian` / `ServiceRHEL` | systemd unit to start after install (empty = none). |
| `EnableService` | Also `systemctl enable` the unit, not just start it. |
| `OptionalComponent` | Names a LinuxIO-managed non-package installer, run before any package step. `"docker"` downloads Docker's convenience script from `https://get.docker.com`, reports its sha256, and streams its output (skipped when a `docker` binary is already on `PATH`). `"node"` downloads the pinned nvm install script and runs it, then `nvm install --lts`, as the session user (see [Node.js](#nodejs-assistant)). `handlers/packages` rejects any other value. |
| `RequiresDocker` | Optional-component prerequisite checked before install. |

Omit `Install` entirely for capabilities with no UI install path (the
PackageKit capability itself).

## Frontend Pieces

| Symbol | File | Role |
|--------|------|------|
| `CAPABILITIES` | `api/capabilities.ts` | Source of truth: one `CapabilityDef` per capability. |
| `CapabilitiesResponse`, `CapabilityKey`, … | `api/capabilities.ts` | Types derived from `CAPABILITIES`. |
| `capabilityStateFromWire` | `api/capabilities.ts` | Maps the wire response into tri-state auth state. |
| `useCapability` | `hooks/useCapabilities.ts` | Per-capability `{ status, isEnabled, reason }`. |
| `hasAccessPolicy` / `useAccessContext` | `hooks/useCapabilities.ts` | Evaluate a route's access policy. |
| `AuthProvider` | `contexts/AuthProvider.tsx` | Seeds cached state on reload; refreshes `get_capabilities` asynchronously after mux open, persists valid results, and exposes state. |
| `CapabilityManagerSection` | `routes/_authenticated/settings/-components/CapabilityManagerSection.tsx` | Lists capabilities, shows status, offers Install. |

A `CapabilityDef` (see the interface in `api/capabilities.ts` for the full set):

```ts
{
  wire: "wireguard",          // snake_case wire prefix -> wireguard_available / wireguard_error
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

Adding the entry automatically: derives the wire/state types, adds the
`<state>` field to auth state, and registers the row (with an Install button if
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
install path. It gates the **Storage → Snapshots** tab (`/storage/snapshots`),
which manages subvolumes, snapshots and replication, and the pools on the
Disks overview, which manage devices, health and settings; see
[Btrfs management](./btrfs.md).

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
It gates the Assistant route and its settings tab, whose ACP agent adapters
run through `npx`.

`DetectAsUser(ctx, user)` is the variant for capabilities that depend on the
session user's own environment; every other capability keeps `Detect(ctx)`.
Exactly one of the two is set, which the registry test enforces.

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
2. **Backend struct** — add `XAvailable bool` and `XError *string` to
   `session.CapabilitiesAvailable` / `session.CapabilitiesError`.
3. **Backend registry** — add a `CapabilitySpec` (with `Install` if applicable)
   to `capabilityRegistry`.
4. **Backend switch** — add a `case "<wire>":` to `setCapabilityField`.
5. **API contract** — run generation so the embedded
   `apischema.CapabilitiesResponse` fields appear in TypeScript.
6. **Frontend manifest** — add a `CapabilityDef` to `CAPABILITIES` in
   `api/capabilities.ts`.
7. **Gate** — add `requiredCapabilities: ["xAvailable"]` to the route, and/or
   gate in-page with `useCapability`.
8. **Generate** — run `make generate`.

## Verification

```bash
make generate                              # regenerates the TS contract
make test-go-quiet GO_TEST_PKGS=./bridge/handlers/capabilities # registry + detection tests
make tsc-only                              # derived capability types compile
```

Manual end-to-end: with the dependency absent, the feature is gated and the
Capability Manager shows it unavailable (with an Install button if installable);
installing it and refreshing capabilities re-enables the feature.
