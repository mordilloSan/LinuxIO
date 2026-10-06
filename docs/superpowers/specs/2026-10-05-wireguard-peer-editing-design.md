# WireGuard peer editing

Date: 2026-10-05. Status: implemented (plan skipped at the user's request).

## Goal

Bring the WireGuard page to wg-easy parity for per-peer management on
interfaces that LinuxIO created: rename a peer, enable or disable it without
deleting it, choose what the client routes through the tunnel, declare
networks behind the peer, set per-peer DNS, MTU and keepalive, add or drop a
preshared key, and regenerate keys. New peers get a name at creation and a
preshared key by default.

Out of scope, each a later spec: interface editing (host override, DNS, MTU,
port, hooks), IPv6 and subnets larger than /24, client expiry, one-time
links, per-client firewall, bring-your-own public keys.

## Files and data model

Peers remain plain files under `/etc/wireguard`, no database and no new
sidecar. The exported client config `/etc/wireguard/<iface>/<id>.conf` is the
single source of truth for a peer. Its file stem (`Peer2`) is the stable
peer id and never changes.

LinuxIO metadata lives in comment lines above `[Interface]` in the client
config. `gopkg.in/ini.v1` attaches them to the `[Interface]` section comment
on load and writes them back on save (verified), and wg-quick and the
official client apps ignore comment lines:

```
# linuxio-name: Alice's phone
# linuxio-enabled: false
# linuxio-server-allowed-ips: 192.168.50.0/24, 10.9.0.0/16
[Interface]
PrivateKey = <client private key>
Address = 10.10.20.2/32
DNS = 1.1.1.1
MTU = 1420

[Peer]
PublicKey = <server public key>
PresharedKey = <psk>
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 203.0.113.10:51820
PersistentKeepalive = 25
```

- Keys are `linuxio-name`, `linuxio-enabled`, `linuxio-server-allowed-ips`.
  A line matches `^#\s*linuxio-([a-z-]+):\s*(.*)$`. On every write the whole
  header is replaced by these three lines; other comment text in that
  position is not preserved.
- A client config with no header is a legacy peer: name equals id, enabled
  is true, no extra networks.
- The peer's public key is derived from the client `PrivateKey`. A file
  whose private key does not parse is skipped with a warning, as unparsable
  files are today.
- The server config `/etc/wireguard/<iface>.conf` keeps its current role and
  is still parsed, edited and written back so hand-made changes elsewhere in
  the file survive. The `[Peer]` section for a peer is derived from the
  client config: `PublicKey`, `PresharedKey` if any, `AllowedIPs` equal to
  the `/32` address followed by the extra networks, `PersistentKeepalive` if
  non-zero. The section carries a `# <name>` comment above it for admins.
  The section is present when the peer is enabled and absent when disabled.
  The server config parser stops reading a `Name` key from `[Peer]`
  sections: it was never written, and `wg setconf` rejects unknown keys.
- Matching between the server config and client configs is by public key
  everywhere (list, update, remove). Today removal matches by allowed IP.
- Download and QR code serve the client config with `# linuxio-` lines
  removed.
- The interface DNS default lives in the server config header since the
  interface-editing spec of the same date; the `.dns` sidecar is gone.

Defaults written into new client configs: client routes `0.0.0.0/0, ::/0`
(replacing the historical `0.0.0.0/1, 128.0.0.0/1, ::/0` split; existing
files are untouched until edited), DNS from the interface or the egress
gateway as today, no MTU line, keepalive 25, a generated preshared key.

## API contract

Types live in `backend/bridge/apischema`; TypeScript is regenerated with
`make generate`.

`Peer` response:

| field | type | note |
| --- | --- | --- |
| `id` | string | file stem, stable |
| `name` | string | display name, defaults to id |
| `enabled` | bool | metadata flag |
| `address` | string | tunnel address with `/32` |
| `client_allowed_ips` | string[] | client-side `AllowedIPs` |
| `server_allowed_ips` | string[] | networks behind the peer, may be empty |
| `dns` | string[] | the `DNS` line in the client config, may be empty |
| `mtu` | int | 0 when unset |
| `public_key` | string | derived |
| `preshared_key` | string, optional | unchanged from today |
| `persistent_keepalive` | int, optional | unchanged |
| `endpoint` | string, optional | unchanged |
| `last_handshake`, `last_handshake_unix`, `rx_bytes`, `tx_bytes`, `rx_bps`, `tx_bps` | unchanged | runtime stats |

`allowed_ips` is removed.

Requests:

- `WireGuardPeerRequest { interfaceName, peerId }` replaces
  `InterfaceNamePeerNameRequest` for `wireguard.remove_peer`,
  `wireguard.peer_qrcode` and `wireguard.peer_config_download`. If the old
  type turns out to have callers outside WireGuard it stays and only the
  WireGuard routes move.
- `wireguard.add_peer` takes `WireGuardAddPeerRequest { interfaceName,
  name? }`. An absent or blank name means the id.
- New `wireguard.update_peer`, void response, request
  `WireGuardUpdatePeerRequest`:

| field | type | absent | present |
| --- | --- | --- | --- |
| `interfaceName`, `peerId` | string | required | |
| `name` | string | unchanged | set, blank resets to id |
| `enabled` | bool | unchanged | set |
| `clientAllowedIPs` | string[] | unchanged | set, empty resets to full tunnel |
| `serverAllowedIPs` | string[] | unchanged | set, empty clears |
| `dns` | string[] | unchanged | set, empty resets to the interface default (the `# linuxio-dns:` header, else the default gateway, else no line) |
| `mtu` | int | unchanged | set, 0 removes the line |
| `persistentKeepalive` | int | unchanged | set, 0 removes the line in both files |
| `presharedKey` | `"generate"` or `"remove"` | unchanged | act |
| `regenerateKeys` | bool | | true issues a new client private key |

Route registration follows `handlers.go`: `Call[...]("wireguard.update_peer").HandleVoid(...)`, not retry-safe.
`frontend/src/api/operation-query-invalidations.ts` gains
`wireguard.update_peer` invalidating `wireguard.list_interfaces` and
`wireguard.list_peers`, matching `add_peer`.

## Backend flow

All of this stays in `backend/bridge/handlers/wireguard`.

`update_peer`:

1. Validate names, load the client config and its metadata, load the server
   config and its header.
2. Apply the request. Key regeneration replaces the client private key;
   `presharedKey: generate` creates a key with wgctrl, `remove` drops it.
3. Validate the result (see below).
4. Write the client config with the new header.
5. Rewrite the server config: drop any `[Peer]` section whose public key
   matches the peer's previous public key, append the derived section when
   enabled, write the file.
6. If the interface is up, run the existing strip-and-syncconf path. A
   syncconf failure is returned as the error; the files are already
   consistent and the next update or interface restart converges, which is
   the behaviour `add_peer` has today.

Disable therefore removes the peer from the kernel immediately and keeps it
out across reboots because wg-quick reads the same server config.

`add_peer` keeps its allocation logic, writes the header with the given
name, enabled true and no extras, and generates a preshared key.

`remove_peer` matches the server section by public key and deletes the
client file.

`list_peers` reads every client config, derives public keys, applies
metadata, and merges runtime stats by public key as today. Legacy
`mergeConfiguredPeerPublicKeys` is removed.

`peer_qrcode` and `peer_config_download` strip `# linuxio-` lines. The
download filename is the name made filesystem-safe, falling back to the id.

## UI

Files under `frontend/src/routes/_authenticated/wireguard/-components` plus
`frontend/src/components/cards/WireguardPeerCard.tsx`.

- Peer card: an inline `AppSwitch` for enabled that calls `update_peer`
  with only `enabled`; an Edit action next to Download, QR and Delete; a
  "Disabled" chip in place of the online chip when disabled; the address
  line shows `address`, and the extra networks are listed when present.
  Pending state reuses the existing per-peer pending map with new actions
  `toggle` and `edit`.
  Per-peer DNS, routes and MTU shown in the dialog are the effective values
  from the client config; there is no separate inherit flag.
- `EditPeerDialog.tsx`: name, client routes as a comma-separated text field
  with a "Full tunnel" button that fills `0.0.0.0/0, ::/0`, networks behind
  the peer, DNS, MTU, keepalive, a preshared-key switch whose on state from
  off sends `generate` and off state from on sends `remove`, a "Regenerate
  preshared key" button, and a "Regenerate keys" button. Both regenerate
  buttons go through `components/filebrowser/ConfirmDialog` marked
  destructive, since the client must re-import its config. The dialog sends
  only fields that changed. Validation mirrors the server rules and shows
  inline helper text; server errors show in an `AppAlert` as in the create
  dialog.
- `AddPeerDialog.tsx`: one name field, prefilled with the next id, submit
  calls `add_peer`. The card's Add Peer action opens it.
- Download uses the filename returned by the backend instead of building
  one from the peer name.
- All styling through `components/ui` and `--app-*` variables.

## Validation

Server side, returned as plain error messages:

- name: at most 64 characters, no `\n` or `\r`, trimmed.
- client routes and extra networks: each entry must parse with
  `net.ParseCIDR`; IPv4 and IPv6 both accepted; duplicates removed.
- DNS: each entry non-empty with no whitespace, comma or newline.
- MTU: 0, or 1280 to 65535.
- keepalive: 0 to 65535.
- `presharedKey`: only `generate` or `remove`.
- peer and interface names: existing `validateInterfaceName`.

## Testing

Go, under the existing `newWireGuardTestEnv` temp-dir harness:

- metadata header round-trip, including a name with spaces and an
  apostrophe, and a legacy file without a header.
- each `update_peer` field alone: name, client routes, extras, DNS, MTU,
  keepalive; the client file and server section reflect it and a sync ran.
- disable removes the server section and syncs; enable restores it with
  the `/32` plus extras; the client file is intact throughout.
- preshared key generate and remove; key regeneration changes the derived
  public key in both files.
- remove by public key when two peers share a prefix.
- download and QR output contains no `# linuxio-` line.
- `add_peer` with a name and with none; new peers have a PSK.

Frontend, Vitest:

- `EditPeerDialog` sends only changed fields, blocks invalid CIDR, MTU and
  keepalive, and routes both regenerate buttons through the confirm dialog.
- `WireguardPeerCard` shows the Disabled chip, the inline switch calls
  `update_peer` with only `enabled`, and shows extras when present.
- `AddPeerDialog` prefills and submits the name.

Finish with `make generate` and `make test-quiet`, since the contract spans
both sides.

## Documentation

- `docs/dialog-styling.md`: add the edit-peer and add-peer dialogs to the
  WireGuard row.
- No API contract doc currently lists WireGuard routes; nothing else to
  reconcile.
