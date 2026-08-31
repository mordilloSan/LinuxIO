# Bridge Configuration Storage

This document covers configuration owned by each authenticated bridge. The
indexer's separate configuration and state are recorded in the
[configuration and storage layout](./configuration-storage-layout.md).

LinuxIO keeps each authenticated user's functional settings and UI preferences
in separate bridge-owned stores. Configuration failures degrade through two
persistent locations and finally to memory so an unavailable settings file does
not prevent the bridge from starting.

## Storage locations

At startup the bridge tries these stores in order:

1. The authenticated user's home directory.
2. `/var/lib/linuxio/users/<uid>`.
3. An in-memory store for the bridge session.

The persistent artifacts are:

| Artifact | Purpose |
|----------|---------|
| `.linuxio-config.yaml` | Per-user functional settings used by the bridge. |
| `.linuxio-ui.yaml` | UI preferences produced by the frontend. |
| `.linuxio-config.yaml.lock` | Core-config sidecar lock. |
| `.linuxio-ui.yaml.lock` | UI-config sidecar lock. |

The packaged tmpfiles policy creates `/var/lib/linuxio/users` as a root-owned
`0711` directory. A fallback directory is named by authenticated numeric UID,
must be owned by that UID, and is restricted to `0700`. If it cannot be safely
created or verified, the bridge uses memory instead.

If the launcher cannot enter the authenticated user's home, it uses `/` as the
bridge working directory. Authentication, UID/GID validation, privilege
dropping, and config ownership validation are unchanged. Failure to validate
the authenticated file owner remains fatal; storage fallback does not bypass
that security boundary.

## Storage mode contract

`config.get` includes a `storageMode` field:

| Value | Meaning |
|-------|---------|
| `home` | Both config documents are persisted in the user's home. |
| `fallback` | Home storage failed and both documents are persisted under `/var/lib/linuxio/users/<uid>`. |
| `memory` | Both persistent stores failed; changes live only in the current bridge process. |

The authenticated layout shows a warning for `fallback` and `memory`. The
memory warning states that changes are temporary and will be lost on refresh or
sign-out. Memory-mode config updates also skip the persistent Docker systemd
mount-ordering side effect.

## Startup and recovery

Each disk tier opens the core and UI documents while holding both sidecar locks.
Missing documents are created independently, so one missing file never resets
the other.

Core configuration uses strict YAML decoding and normal semantic validation. A
document that fails decoding or validation at startup is renamed to:

```text
.linuxio-config.yaml.broken-<UTC timestamp>
```

If that name exists, the bridge uses `(2)`, `(3)`, and so on. It then writes
defaults and continues from the same storage tier. This preserves the invalid
document for manual recovery without locking the user out.

Symlink, file-type, ownership, lock, stat, and write failures are not repaired.
They make startup try the next storage tier without replacing the failed config
artifact. A core update that encounters a document corrupted after startup also
fails without quarantining or rewriting it; quarantine is a boot-only recovery
operation.

An invalid UI document is replaced with `{}` and expanded over backend UI
defaults when read. Core and UI updates remain independent: they have separate
locks, snapshots, validation, and atomic whole-file replacement.

## Defaults

When home resolution succeeds but the home store cannot be opened, fallback
storage retains the user's home as the base for path defaults such as the Docker
folder. If home resolution itself fails, defaults use the UID fallback directory
as their base. Memory mode uses the same resolved default base selected before
the persistent attempts failed.

## Logging and diagnostics

The bridge logs:

- default-file creation at `INFO` with the artifact path;
- invalid core quarantine at `WARN` with the active and quarantined paths;
- invalid UI reset at `WARN` with the path and parse error;
- home-to-fallback degradation at `WARN` with the home error and fallback path;
- persistent-to-memory degradation at `WARN` with both disk errors; and
- the ready storage mode plus core and UI paths at `INFO`.

Core and UI paths are empty in memory mode. The public config update result
therefore also returns an empty path for memory-only updates.

## Ownership and concurrency

Production stores enforce the authenticated UID/GID even when the bridge runs
privileged. Config files and lock files reject symlinks and non-regular files.
Disk writes use atomic replacement, and sidecar locks serialize separate bridge
processes for the same user. In-process mutexes protect snapshots served to
handlers.

The core and UI documents deliberately remain flat YAML files. There is no
database, combined compatibility document, local-storage theme copy, or
filesystem-based repair of Docker folder settings.

See [API Contract](./api-contract.md) for the route and generated-client rules
that expose these settings.
