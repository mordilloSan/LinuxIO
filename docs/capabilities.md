# Capabilities

This is the canonical guide for LinuxIO's **capability system** — how the app
detects optional host tooling (Docker, NFS, WireGuard, lm-sensors, …), gates
features on it, and installs it from the UI.

## Summary

- A capability is a named, optional host dependency (a binary, package, or
  service) that some feature needs in order to work.
- Each capability is declared **once per side** in a manifest:
  - Backend: the `capabilityRegistry` in `backend/bridge/handlers/system/capabilities.go`.
  - Frontend: the `CAPABILITIES` array in `frontend/src/api/capabilities.ts`.
- `system.get_capabilities` reports, for every capability, an `*_available`
  boolean and an optional `*_error` string. It is a post-login UI hint, not
  part of the login JSON or an authorization decision: once the authenticated
  session mux opens, `AuthContext` invokes the RPC asynchronously once for the
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
fields. `AuthContext` starts the authenticated mux eagerly, then performs one
best-effort capability refresh per authenticated frontend bootstrap after the
mux opens. Completion is ignored when the user/authentication generation is
stale, and only valid results are persisted. Callers may explicitly refresh
after an install; that is separate from the initial bootstrap refresh.

## Backend Pieces

| Symbol | File | Role |
|--------|------|------|
| `capabilityRegistry` | `handlers/system/capabilities.go` | Source of truth: one `CapabilitySpec` per capability. |
| `CapabilitySpec` | `handlers/system/capabilities.go` | `Name` (wire prefix), `LogName`, `Detect`, optional `Install`. |
| `InstallSpec` | `handlers/system/capabilities.go` | Package/service to install per distro family. |
| `CapabilitiesAvailable` / `CapabilitiesError` | `common/session/session.go` | Shared wire/session capability fields. |
| `setCapabilityField` | `handlers/system/capabilities.go` | Maps a wire name to its struct fields. |
| `buildCapabilitiesResponse` | `handlers/system/capabilities.go` | Iterates the registry and fills the struct. |
| `CapabilitySpecByName` | `handlers/system/capabilities.go` | Lookup used by the install runner. |
| `CapabilitiesResponse` | `apischema/models.go` | Exported API contract type (pointer errors), reflected into TypeScript. |
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
`docker.CheckDockerAvailability`, `storage.CheckNFSClientAvailability`), or is
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

`TestCapabilityRegistryCoversWireFields` (`capabilities_test.go`) asserts that
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
resolve -> [install_asset] -> [install_package] -> [enable_service] -> [start_service] -> wait_service_active -> detect
```

- `detectDistroFamily()` reads `/etc/os-release` and classifies the host as
  `debian` or `rhel`; `pickByFamily` chooses the matching package/service name.
- Optional LinuxIO-managed components (for example the indexer or
  go-monitoring) install through a component-specific asset/script step instead
  of PackageKit.
- Package installs go through PackageKit (`InstallByName`), so installable
  capabilities that have a package step require PackageKit to be available.
- Service steps use `systemd.EnableUnit` / `StartUnit`, then `waitUnitActive`.
- Finally `detectWithRetry` re-runs `Detect` for a few seconds to cover the gap
  between a service going `active` and its surface (D-Bus name, socket) being
  reachable. The job result is the freshly re-detected `{available, error}`.

`InstallSpec` fields:

| Field | Meaning |
|-------|---------|
| `PackageDebian` / `PackageRHEL` | Package name per family (empty = no package step). |
| `ServiceDebian` / `ServiceRHEL` | systemd unit to start after install (empty = none). |
| `EnableService` | Also `systemctl enable` the unit, not just start it. |
| `OptionalComponent` | LinuxIO-managed non-package installer handled in `handlers/packages`. |
| `RequiresDocker` | Optional-component prerequisite checked before install. |

Omit `Install` entirely for capabilities with no UI install path (Docker, the
PackageKit capability itself).

## Frontend Pieces

| Symbol | File | Role |
|--------|------|------|
| `CAPABILITIES` | `api/capabilities.ts` | Source of truth: one `CapabilityDef` per capability. |
| `CapabilitiesResponse`, `CapabilityKey`, … | `api/capabilities.ts` | Types derived from `CAPABILITIES`. |
| `capabilityStateFromWire` | `api/capabilities.ts` | Maps the wire response into tri-state auth state. |
| `useCapability` | `hooks/useCapabilities.ts` | Per-capability `{ status, isEnabled, reason }`. |
| `hasAccessPolicy` / `useAccessContext` | `hooks/useCapabilities.ts` | Evaluate a route's access policy. |
| `AuthContext` | `contexts/AuthContext.tsx` | Seeds cached state on reload; refreshes `get_capabilities` asynchronously after mux open, persists valid results, and exposes state. |
| `CapabilityManagerSection` | `components/navbar/CapabilityManagerSection.tsx` | Lists capabilities, shows status, offers Install. |

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

## Consuming A Capability

There are two established patterns. Pick based on whether the *whole feature* or
just *some actions* depend on the tool.

### 1. Whole-route gating (Docker, Hardware, VMs, WireGuard)

Define one typed policy beside the file route, use it in `beforeLoad`, and
expose the same policy through `staticData`. The guard denies direct
navigation; the sidebar reads `staticData` and hides inaccessible routes:

```tsx
const access = {
  requiredCapabilities: ["wireguardAvailable"],
  requiresPrivileged: true,
} satisfies AccessPolicy;

export const Route = createFileRoute("/_authenticated/wireguard")({
  beforeLoad: ({ context }) => requireAccess(access, context),
  loader: (loaderArgs) =>
    loadRouteQueries(loaderArgs, [
      linuxio.wireguard.list_interfaces,
    ]),
  component: WireguardPage,
  staticData: {
    access,
    navigation: {
      icon: WireguardIcon,
      position: 80,
      title: "Wireguard",
    },
  },
});
```

Users discover and install the missing tool from the Capability Manager in the
navbar.

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
cd backend && go test ./bridge/handlers/system/   # anti-drift + round-trip tests
make tsc-only                              # derived capability types compile
```

Manual end-to-end: with the dependency absent, the feature is gated and the
Capability Manager shows it unavailable (with an Install button if installable);
installing it and refreshing capabilities re-enables the feature.
