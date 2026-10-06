# WireGuard interface editing

Date: 2026-10-05. Status: implemented (plan skipped at the user's request).

## Goal

Let an existing LinuxIO-created WireGuard interface be edited after creation:
endpoint host override, client DNS, server MTU, listen port, and the four
wg-quick hooks. Saving rewrites the exported client configs and restarts the
interface only when the server-side config changed. The Create Interface
dialog gains the host and MTU fields it was missing.

The target is a brand-new server: no compatibility or migration code for
earlier LinuxIO layouts.

Out of scope: renaming an interface, changing its address or subnet (belongs
with the IPv6 work), an explicit restart button, a "reset hooks to NAT
defaults" helper, interface-level traffic totals.

## Files and data model

`/etc/wireguard/<iface>.conf` remains the server config and stays parsed,
edited and written in place. LinuxIO metadata moves into comment lines above
`[Interface]`, carried by the INI section comment exactly as the peer files
do:

```
# linuxio-host: vpn.example.org
# linuxio-dns: 1.1.1.1, 9.9.9.9
[Interface]
Address = 10.10.20.1/24
ListenPort = 51820
PrivateKey = ...
MTU = 1420
PreUp = ...
PostUp = sysctl -w net.ipv4.ip_forward=1
PostUp = iptables ...
PreDown = ...
PostDown = iptables ... || true

# Alice's phone
[Peer]
...
```

- `linuxio-host` is the endpoint host written into client configs. Absent
  means detect the public IP at export, today's behaviour.
- `linuxio-dns` is the DNS written into client configs that have no DNS of
  their own. Absent means the host's default IPv4 gateway (at creation, the
  gateway of the chosen egress NIC, as today), else no DNS line.
- The `.dns` sidecar is removed: `metadata.go` and its callers go, and tests
  that seeded a sidecar seed the header instead. Nothing reads `.dns`.
- The server config parser stops reading a `DNS` key from `[Interface]`. That
  key would make wg-quick change the host's own resolver and LinuxIO never
  writes it; client DNS is metadata.
- `WireGuardConfig` gains `Host`, `PreUp` and `PreDown`. The parser reads
  all four hook kinds with shadows, one command per line, and the writer
  emits them in the order PreUp, PostUp, PreDown, PostDown.
- On every write the header is replaced by the lines LinuxIO owns; other
  comment text in that position is not preserved, as with peer files.

## API contract

Types in `backend/bridge/apischema`, TypeScript via `make generate`.

`WireGuardInterface` gains:

| field | type | note |
| --- | --- | --- |
| `host` | string | override, empty when auto-detected |
| `dns` | string[] | client DNS default, may be empty |
| `mtu` | int | server MTU, 0 when unset |
| `preUp`, `postUp`, `preDown`, `postDown` | string[] | one command per entry, may be empty |

`WireGuardAddInterfaceRequest` gains `host *string` (optional, same
validation as below). `mtu` already exists and the dialog starts sending it.

New `wireguard.update_interface`, void response, request
`WireGuardUpdateInterfaceRequest`:

| field | type | absent | present |
| --- | --- | --- | --- |
| `name` | string | required | |
| `host` | string pointer | unchanged | set, empty clears the override |
| `dns` | string[] | unchanged | set, empty clears so the gateway applies |
| `mtu` | int pointer | unchanged | set, 0 removes the line |
| `listenPort` | int pointer | unchanged | set |
| `preUp`, `postUp`, `preDown`, `postDown` | string[] | unchanged | set, empty removes that hook kind |

Registered as `Call[...]("wireguard.update_interface").HandleVoid(...)`, not
retry-safe. `operation-query-invalidations.ts` gains it, invalidating
`wireguard.list_interfaces` and `wireguard.list_peers`.

## Backend flow

All in `backend/bridge/handlers/wireguard`.

`list_interfaces` fills the new fields from the parsed config and header.

`update_interface`:

1. Validate the name, parse the config and header, and apply the request to
   an in-memory copy. Validate every field (below) before any write.
2. Classify the change. Port, MTU and hooks are server-side; host and DNS are
   export-side.
3. If a server-side field changed and the interface is up: run
   `wg-quick down` first, while the old config is still on disk, so the old
   PostDown hooks tear down what the old PostUp set up. Then write the new
   config. Then `wg-quick up`. If `up` fails, return its error with a message
   saying the interface is now down; the new config stays on disk.
4. Otherwise write the config without touching the running interface.
5. Rewrite exported peers:
   - When host or port changed, every peer file's `Endpoint` becomes the
     resolved host (override, else detected public IP) joined with the port
     using `net.JoinHostPort`. If neither a host nor a public IP is available
     the host part of the peer's existing endpoint is kept and only the port
     is applied; a peer with no endpoint at all is left alone with a warning.
   - When DNS changed, peers whose DNS equals the old interface default get
     the new default; peers with any other DNS keep theirs. Old and new
     defaults are the header DNS, else the default gateway, else none.
   - Peer files are rewritten through the peer-file writer, so metadata is
     preserved. The server `[Peer]` sections are unchanged by this step.
6. Boot persistence is unaffected: wg-quick reads the same file at boot.

`add_interface` accepts `host`, writes the header, and uses the override
when exporting the initial peers. `add_peer` and peer regeneration resolve
the endpoint host the same way: override first, then public IP lookup.

`remove_interface` no longer removes a sidecar.

## UI

Files under `frontend/src/routes/_authenticated/wireguard/-components` and
`frontend/src/components/cards/WireguardInterfaceCard.tsx`.

- Interface card: an Edit action next to Add Peer. Pending state uses the
  existing per-interface pending map; the dialog owns its own mutation.
- `EditInterfaceDialog.tsx`: host, DNS, MTU, port, and four multiline hook
  fields, one command per line. An info notice: changing port, MTU or hooks
  restarts the interface and reconnects peers; changing host or DNS requires
  peers to re-import their config. Sends only changed fields. Save is
  disabled with nothing changed or an invalid value. Server errors show in an
  alert as in the create dialog.
- `CreateInterfaceDialog.tsx`: Host (optional, helper "Leave empty to use the
  detected public IP") and MTU (optional) fields; the button wiring sends
  both.
- All styling through `components/ui` and `--app-*` variables.

## Validation

Server side, plain error messages:

- host: trimmed, no whitespace, no `/`, at most 253 characters; if it
  contains `:` it must parse as an IP address.
- port: 1 to 65535 and not the listen port of any other `*.conf` under
  `/etc/wireguard`.
- MTU: 0, or 1280 to 65535.
- DNS: entries without whitespace, comma or newline.
- hooks: lines trimmed and empty lines dropped; no other restriction. The
  route is privileged-only and hooks are root commands by design, the same
  trust as editing the file.
- name: existing `validateInterfaceName`.

## Testing

Go, under `newWireGuardTestEnv`; the wg-quick mock records the listen port it
finds on disk at call time so ordering is observable:

- header round-trip for host and DNS; config with no header reads as empty.
- PreUp and PreDown parse and write with shadows.
- port change on an up interface: calls are `down` then `up`, and the `down`
  call saw the old port while `up` saw the new one.
- hook change restarts; host-only and DNS-only changes do not restart.
- host change rewrites every peer endpoint; port change too; neither host nor
  public IP available keeps each peer's endpoint host and applies the port.
- DNS change rewrites only peers that had the old default.
- down interface: write only, no wg-quick calls.
- validation: bad host, port used by another interface, MTU out of range.
- `add_interface` with a host uses it in the exported peers;
  `add_peer` uses the override.
- `remove_interface` leaves no files behind.

Frontend, Vitest:

- `EditInterfaceDialog` sends only changed fields, shows the restart notice,
  blocks an invalid port and MTU.
- `CreateInterfaceDialog` sends host and MTU.

Finish with `make generate` and `make test-quiet`.

## Documentation

- `docs/dialog-styling.md`: add the edit-interface dialog and the new create
  fields to the WireGuard row.
