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

1. `$HOME/.config/linuxio` for the authenticated user.
2. `/var/lib/linuxio/users/<uid>`.
3. An in-memory store for the bridge session.

The persistent artifacts are:

| Artifact | Purpose |
|----------|---------|
| `config.yaml` | Per-user functional settings used by the bridge. |
| `ui.yaml` | UI preferences produced by the frontend. |
| `config.yaml.lock` | Core-config sidecar lock. |
| `ui.yaml.lock` | UI-config sidecar lock. |

Legacy `.linuxio-config.yaml`, `.linuxio-ui.yaml`, and their sidecar locks
directly under `$HOME` are left untouched and are not used as configuration
inputs.

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
sign-out. Memory-mode config updates also skip the Docker startup drop-ins
described below.

## Docker startup settings

Two Docker settings in the core document end in a systemd drop-in for
`docker.service`, which `config.set` writes as root after the config is saved
(`SyncDaemonMountOrdering` and `SyncDaemonHostIPWait` in
`backend/bridge/handlers/docker`). Both are off by default, and turning one off
removes its file. A host where neither was turned on has no drop-in from
LinuxIO.

| Setting | Fields | Drop-in in `/etc/systemd/system/docker.service.d` |
|---------|--------|---------------------------------------------------|
| Wait for folder mounts | `docker.requireMountsForFolders`, read with `docker.folders` | `linuxio-require-mounts.conf`: `RequiresMountsFor=` for each folder, after `network-online.target` and `remote-fs.target` |
| Wait for published host IPs | `docker.waitForHostIPs`, `docker.hostIPs` | `linuxio-wait-host-ips.conf`: one `ExecStartPre=` line for each address |

A drop-in is rewritten when its switch is in the update, or when its list is in
the update while the switch is on. `docker.requireMountsForFolders` always
needs a privileged session and `docker.folders` needs one while that switch is
on. `docker.waitForHostIPs` and `docker.hostIPs` always need one, because the
address list has no use but the drop-in ([Privilege pattern](./privilege_pattern.md)).

### Wait for published host IPs

A container that publishes a port on one host address (`192.168.1.66:53:53/udp`)
fails with "cannot assign requested address" when Docker starts before that
address is assigned, and stays down. `network-online.target` does not prevent
it: on a Netplan host the generator can run before the network card exists, so
wait-online never waits for that card. With the setting on, Docker waits for
each listed address:

```ini
# Managed by LinuxIO. Do not edit by hand.
[Service]
ExecStartPre=-/usr/bin/timeout 30 /usr/bin/sh -c 'until /usr/sbin/ip -o addr show to 192.168.1.66 -tentative | grep -q .; do sleep 0.5; done'
```

- The line waits for the address and names no network card, so it works with
  networkd and NetworkManager and stays valid when a
  [bridge handoff](./vm-bridge-networking.md) moves the address to a bridge.
- The 30 s limit is inside the command. A timeout of `ExecStartPre` itself
  counts against `TimeoutStartSec` and fails the unit even with the `-` prefix,
  and Docker must start when an address never appears.
- `-tentative` leaves out an IPv6 address still in duplicate address
  detection, which cannot be bound yet.
- The line is a shell command and not a LinuxIO command, so it keeps working
  if LinuxIO is removed. `timeout`, `sh` and `ip` are written with the paths
  they have in the system folders (`hostcmd.SearchDirs`) when the file is
  rendered. They are not looked up through `PATH`, which starts with
  `/usr/local`: a group other than root can write there on some distributions,
  and the path is one root runs from at every boot. A missing tool refuses the
  save, before the config is stored, with an error that names it. `grep` and
  `sleep` come from systemd's default `PATH`.
- The only request value in the line is the text of a parsed address.
  `ParseDockerHostIPs` (`backend/bridge/internal/userconfig/rules.go`) accepts
  an address through `net/netip` only and stores its canonical text, with an
  IPv4-mapped IPv6 address as its IPv4 form. It refuses an empty value, a
  zone, the unspecified address, a loopback address and a duplicate. The
  drop-in is rendered from a second parse of the stored list.

The switch on with an empty list is refused with 400 before the config is
saved, and a document on disk in that state fails validation. Switching off
keeps the stored list. An empty list is left out of `config.yaml`.

`docker.list_host_ips` gives the settings page two lists in the stored form:
the host addresses that containers publish ports on, read from
`HostConfig.PortBindings` of an inspect of every container (the container list
has no ports for a container that failed to start, which is the case the
setting is for), and the addresses the host holds now. The page pre-fills an
empty list with the published addresses the host holds, offers the others to
add, and marks every listed or detected address that no interface holds. Save
accepts an unassigned address, since a card can be down or a VPN not up yet;
the mark is the only signal. With Docker unreachable the published list is
empty. The gateway of a Docker bridge network (`172.17.0.1` on `docker0`) is
in neither list: Docker assigns it itself after it starts, so waiting for it
would take the whole 30 s at every boot. It is never suggested, and one typed
into the list is marked.

The config is stored before the drop-ins are written. When a write or the
daemon-reload fails, the save returns the error with the setting stored, and
saving again writes the file.

Known limits:

- Addresses are waited for one after another, so N missing addresses delay
  every start of Docker by up to N × 30 s. LinuxIO waits three minutes for a
  Docker restart it asked for (a storage change), so with six or more missing
  addresses it reports that restart as failed although Docker starts later.
- The settings are per user and the drop-ins are host-wide, so the last save
  wins. This holds for both settings.
- An address that is gone for good (a dynamic IPv6 address that a
  NetworkManager bridge handoff replaced) is marked on the settings page and
  not rewritten. The list and the container's own port binding are edited by
  hand.
- A drop-in written by hand that does the same job is not touched by LinuxIO.

## Startup and recovery

Each disk tier opens the core and UI documents while holding both sidecar locks.
Missing documents are created independently, so one missing file never resets
the other.

Core configuration uses strict YAML decoding and normal semantic validation. A
document that fails decoding or validation at startup is renamed to:

```text
config.yaml.broken-<UTC timestamp>
```

If that name exists, the bridge uses `(2)`, `(3)`, and so on. It then writes
defaults and continues from the same storage tier. This preserves the invalid
document for manual recovery without locking the user out.

Symlink, file-type, ownership, lock, stat, and write failures are not repaired.
They make startup try the next storage tier without replacing the failed config
artifact. A core update that encounters a document corrupted after startup also
fails without quarantining or rewriting it; quarantine is a boot-only recovery
operation.

`ui.yaml` holds only what differs from the backend UI defaults: a save writes
a field, and a theme colour key, only when its value is not the default (an
all-default save writes `{}`), and a read lays the file over the defaults. A
default the backend changes later therefore reaches every user who has not
chosen a value of their own. A file written before this rule holds every field
and keeps holding them, so those users keep the defaults of the day they
saved until their next save, which prunes the file. An invalid UI document is
replaced with `{}` and expanded over backend UI defaults when read; a theme
colour key that LinuxIO does not have makes the document invalid. Core and UI
updates remain independent: they have separate locks, snapshots, validation,
and atomic whole-file replacement.

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
