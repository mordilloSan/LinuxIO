# Storage space as one graph, with live flow

Status: draft for review (2026-10-26)

## Goal

Storage → Disks → **Storage space** draws the host's storage once, as one
graph: every physical drive exactly once on the left, what the drives build
in the middle (btrfs pools, md arrays, LVM and dm-crypt devices, the EFI
boot set, network servers), and every mounted volume on the right. Lines
between them carry a visible, continuous flow, UniFi style, that speeds up
and brightens with throughput. The same edge component gives the Docker
topology the same look.

Today each pool, array and plain disk is its own stacked grid, so a drive
used three ways is drawn three times, md members are collapsed to whole
drives, and the activity grains only exist while a rate is above zero and
are stretched by a non-uniform SVG, which makes them invisible on an idle
host.

### Decisions taken

| Question | Decision |
|---|---|
| Layout | Three columns: Drives, Boot/pools/arrays, Volumes. No partitions column; a drive links straight to what each partition feeds, the edge is labelled with the partition suffix ("p2") |
| Drives with nothing on them | In the Drives column with the caption "Nothing on it"; the separate "Disks without volumes" list goes |
| Partitions nothing uses | One muted caption line each on the drive card: "p1 · FAT · 1.07 GB · not mounted" |
| EFI boot | A middle node "EFI boot" when the host has two or more EFI System Partitions; a single ESP draws as a plain partition → volume |
| Edge shape | Bus routing with soft corners: every target node owns one vertical lane in the gap before it; each incoming edge runs horizontally from its source into that lane, turns with a 40px quarter-curve (halved when the vertical run is shorter than 80px), travels the lane, and curves out into the node. Edges with the same target share the lane, so a four-drive array reads as one trunk, not four crossing curves |
| Flow | Every live edge flows, even at 0 B/s (slow, faint). Four tiers from the source drive's rates set speed and opacity. Reads run towards the volumes in the chart rx colour, writes back in the tx colour |
| Docker topology | Same edge component and look. Its edge SVG keeps its stretched viewBox: the streams use `vector-effect: non-scaling-stroke`, which strokes and dashes in screen pixels, so no pixel-space rewrite is needed |
| Positions | Computed from fixed card heights, never measured from the DOM; only the container width is observed |
| Narrow screens | Existing stacked fallback: columns stack, edges hidden |

### Out of scope

Dragging or rearranging nodes; a zoomable canvas; hover highlighting of
upstream and downstream paths; LVM volume-group nodes (each mapped device is
its own node as today); verifying that ESP copies hold the same files;
mounting spare ESPs; Docker's own layout rules beyond the edge style.

## User experience

### Columns and nodes

**Drives.** One card per physical drive from `storage.get_drive_info`, in
that list's order: icon, health dot, name, model, live "Read · Write" line,
size and temperature, as today, and the drive opens its details on click.
Below the rates, one muted line per partition nothing mounts or builds on.
A drive with no partitions and no use says "Nothing on it". Members of an
unmounted btrfs pool are plain drives here, linked to the offline pool node.

**Boot, pools and arrays.** One card per:

- btrfs filesystem (icon, name, profile chip, allocated and free), opening
  the pool details below the graph, as today; an unmounted pool keeps its
  "Not mounted" or "Degraded" chip and its Mount and Destroy buttons, and a
  missing member is a "N missing disks" card in the Drives column;
- md array (icon, `/dev/mdN`, level chip, "n of m" chip, size), opening the
  array details, as today; "Degraded" chip when degraded;
- other mapped device a volume sits on (LVM logical volume, dm-crypt
  mapping): name, type chip, size; when the device sits on another mapped
  device the chain collapses to the mounted one and the caption lists the
  layers, e.g. "LVM on Encrypted";
- NFS or SMB server: `server:/export`, "Network share" caption, no
  incoming edge;
- EFI boot: see below.

**Volumes.** One card per mounted filesystem the Disks page already lists:
mount point, the device it sits on, filesystem chip, free space; opens the
filesystem card as today.

### Edges

- Drive → middle node, one per member partition, labelled with the
  partition suffix after the drive name ("p2", or "1" for `sda1`). A whole
  disk used as a member has no label.
- Drive → volume, for a volume on a plain partition, labelled the same way.
- Middle node → volume, for each of the node's volumes.
- Server → volume, for network shares.

Edges leaving the same card spread vertically, 14px apart, ordered by their
target's position. Each target node in a column owns a vertical lane in the
gap before it, lanes 30px apart in the target's column order, the first
lane a third of the way across the gap. An edge runs horizontally from its
source exit to its target's lane, turns with a quadratic quarter-curve of
radius 40px (or half the vertical distance when that is less than 80px, so
the two turns meet in one S-curve), runs along the lane to the target's
centre line, turns again and enters the node; a source already level with
its target runs straight. Edges into the
same node share the lane, so they read as one trunk with branches, and a
stub only ever crosses other lanes at right angles. The flow animation runs
along each edge's own path; overlapping dashes on a shared lane line up
because every edge of a node uses the same period and phase. One label per
node sits on the short horizontal between its lane and the node, 11px
SVG text with a background-coloured halo, when all of the node's incoming
edges carry the same partition suffix; otherwise each edge is labelled at
its source exit.

### EFI boot node

Shown when two or more partitions carry the EFI System Partition type GUID
`c12a7328-f81f-11d2-ba4b-00a0c93ec93b`. Every ESP's drive links to it. Its
content:

- name "EFI boot";
- chip "N boot entries": firmware Boot#### entries whose hard-drive device
  path names one of these ESPs; then a status chip: success "All bootable"
  when every ESP has an entry, warning "M without entry" otherwise, and
  grey "No firmware entries" when the host has no efivars (BIOS boot, or a
  container);
- caption "M copies · nvme2n1p1 mounted", or "M copies · none mounted".

The node links to the mounted ESP's volume (`/boot/efi`). Its edges from the
drives whose copy is not mounted are plain, unanimated lines; the mounted
copy's edge flows like any live edge. The node makes no claim that the
copies are identical: that would need mounting them.

### Flow

The shared `AppTopologyEdge` draws a base line and, when the edge is live,
one dashed read stream and one dashed write stream:

| Tier | Rate (max of read and write) | Period | Opacity |
|---|---|---|---|
| idle | 0 | 2.8 s | 0.45, writes hidden |
| light | < 10 MB/s | 1.5 s | 0.75 |
| medium | < 100 MB/s | 1.0 s | 0.9 |
| busy | ≥ 100 MB/s | 0.65 s | 1 |

Reads: 2.2px round-capped dashes, 7 on 11 off, chart rx colour, moving
towards the volume. Writes: 1.6px dashes, 3 on 15 off, chart tx colour,
moving back, drawn 3px below the line. Both animate `stroke-dashoffset` by
one dash period, so the loop is seamless. An edge is live when its target
is mounted or active: a volume, a pool or array with a mounted volume, the
mounted ESP. Spare boot copies, unused partitions and offline pools get the
base line only. The rate behind a drive's edges is that drive's; a pool or
array's edge to its volume carries the sum of its members' rates; a plain
partition's edge carries its drive's. Rates come from `monitoring.get_live`
as today and disappear when a poll fails, which drops the edge to idle, not
to still.

**Pause animation** freezes every stream in place; `prefers-reduced-motion`
removes the animation and keeps the lines, chips and numbers. Both are
unchanged in behaviour and keep their browser tests.

The Docker topology passes its container's rx and tx bytes per second and
`live` for running containers, and gets the same streams; its twelve grain
tracks go.

### Selection and narrow layouts

Clicking a pool or array toggles it in the URL as today (`pool`, `raid`
search parameters) and shows its details under the graph; the selected
node's edges turn primary-coloured through the existing `highlighted` prop.
Escape still clears the selection. Below the 780px container width the
columns stack (drives, then middle nodes, then volumes), the SVG is hidden,
and every card keeps its text and actions.

## Layout model

A pure function in `storageGraph.ts` (replacing `storagePools.ts`'s
`buildStorageLayout`) takes drives, filesystems, btrfs filesystems, offline
pools, NFS mounts, the block-device inventory and md arrays, and returns:

```ts
interface StorageGraph {
  nodes: GraphNode[];   // kind: drive | pool | offlinePool | array | device | server | boot | volume | missing
  edges: GraphEdge[];   // from, to (node ids), label, live, rates
  height: number;       // px, the tallest column
}
interface GraphNode { id; kind; column: 0 | 1 | 2; top: number; height: number; … }
```

Rules:

1. Drive nodes in list order, column 0, heights 120 plus 16 per unused
   partition line, 24px apart.
2. Volume nodes, column 2, 56 tall. Middle nodes 128 tall. Offline pool
   nodes are taller to hold their buttons.
3. Every volume's device is walked up the inventory's `parents` until a
   drive is reached: the first hop below the drive gives the edge label;
   the top-most non-drive device, or the btrfs pool, is the middle node.
   btrfs membership comes from `btrfs_list` and the offline pools, not from
   mount points, as today.
4. Middle nodes are ordered by the mean `top` of their source drives, then
   placed at that mean and pushed down until they clear the node above by
   24px. Volume nodes are placed at their source's centre the same way,
   ordered by source position. Column 1 and 2 start at the first drive's
   top, so a host with one drive and one volume draws a straight line.
5. `height` is the bottom of the lowest node plus 24.

Horizontal positions come from the container: three equal columns with a
96px gap, measured by a small `useContainerWidth` hook (ResizeObserver,
like `LiveChartHover`); node `left`/`width` and the SVG `viewBox` use the
same numbers, so stroke widths and dash lengths are in pixels. Nodes are
absolutely positioned HTML (buttons where clickable, as today); the SVG
overlay has `pointer-events: none`.

## Backend: partition type and firmware boot entries

`storage.get_topology` stays a read-only, retry-safe, unprivileged call. Two
additions:

- `StorageBlockDevice.partType` (string, omitzero): the GPT partition type
  GUID from udev's `ID_PART_ENTRY_TYPE`, lowercased, read beside the
  `ID_PART_ENTRY_UUID` the inventory already takes. MBR partitions report
  their two-digit type the same way.
- `StorageTopologyInventory.bootEntries` (`[]EFIBootEntry`, omitzero):
  `{ id: "0000", label: "Ubuntu", partUUID, active, current, order }`.

The bridge reads `/sys/firmware/efi/efivars/` directly, no `efibootmgr`:

- `BootOrder-8be4df61-…` and `BootCurrent-8be4df61-…` are little-endian
  `uint16` lists after the 4-byte efivarfs attribute prefix.
- Each `Boot####-8be4df61-93ca-11d2-aa0d-00e098032b8c` is an
  `EFI_LOAD_OPTION`: `uint32` attributes (bit 0 = active), `uint16`
  file-path-list length, a NUL-terminated UTF-16LE description, then the
  device-path list. Nodes are `type, subtype, uint16 length`; a Hard Drive
  node is type 4 subtype 1 with `uint32` partition number, `uint64` start
  and size, 16-byte signature, `uint8` format (2 = GPT) and `uint8`
  signature type (2 = GUID). The signature is the partition's GUID in the
  mixed-endian layout, which maps to `PARTUUID`.
- A missing `efivars` directory, an unreadable or malformed entry, or an
  entry whose path is not a hard drive (network boot, a shell) is skipped;
  nothing fails the inventory. efivarfs files are world-readable, so no
  privilege is involved.

The pasted `efibootmgr -v` output of the reference host provides the parser's
test bytes: `04 01 2a 00 01 00 00 00 00 08 00 00 00 00 00 00 00 98 21 00 00
00 00 00 29 c3 05 3c cd 5c 35 46 93 00 b1 ae db 19 af aa 02 02` decodes to
partition 1 of GPT GUID `3c05c329-5ccd-4635-9300-b1aedb19afaa`.

No new capability flag: an empty `bootEntries` with ESPs present renders
"No firmware entries".

## Frontend layout of the change

- `components/ui/AppTopologyEdge.tsx` and `app-topology-edge.css`: base
  line, read and write streams, `live` prop, tiers by rate; the grain
  tracks and their CSS are removed.
- `hooks/useContainerWidth.ts`: ResizeObserver width of a ref.
- `storage/-components/disk-overview/storageGraph.ts` (+ test): the model
  above; `fsTypeLabel`, `deviceTypeLabel`, `profileLabel`, `describePool`
  move here from `storagePools.ts`, which is deleted.
- `StoragePools.tsx` and `storage-pools.css`: render the graph; the pool,
  array, offline pool, dialogs and details sections keep their current
  markup and behaviour.
- `docker/-components/topology/`: the `AppTopologyEdge` call gains `live`
  (running container); paths are unchanged.
- Generated API files via `make generate`.

## Testing

- `storageGraph.test.ts`: the reference host (four drives, ESP ×4 with
  four boot entries, md RAID 1 on p2, btrfs RAID 10 on p3) gives one drive
  node each, three middle nodes, three volumes, twelve drive edges with the
  right labels and liveness, and the pushed-apart positions; a single ESP
  gives no boot node; a boot entry for a missing ESP is ignored; LVM on a
  partition, LVM on LUKS (collapsed caption), NFS share, md on whole disks
  (no labels), an idle drive, an offline pool with a missing disk, an
  unused partition line, a plain partition volume.
- `StoragePools.test.tsx`: existing selection, dialog, rename, destroy,
  mount-degraded, details and privilege tests kept; drawing assertions
  updated to nodes and edges; a test that the stacked fallback lists every
  card.
- Backend: `efi_boot_test.go` decodes the bytes above and a BootOrder;
  inventory tests cover `partType`; `TestNativeInventoryMatchesLsblk`
  compares `PARTTYPE` too.
- Browser (`storage-disks.spec.ts`, `topology-flow.spec.ts`): an edge with
  0 B/s still animates (idle tier), a busy edge has a shorter
  `animation-duration`, writes are hidden at idle, pause freezes
  `stroke-dashoffset`, reduced motion removes the animation, and the Docker
  view passes the same checks; the fixture gains a second ESP with boot
  entries. `topology-selection.spec.ts` unchanged.
- Targets: `make generate`, then `make test-quiet`, then
  `make test-frontend-browser-quiet`.

## Documentation

`docs/storage-disks.md`: the Storage space paragraph, the Live activity
section and the Inventory section (partType, bootEntries, efivars). A note
in the same file on the EFI boot node's limits (entries, not file
equality).

## Sources

- UEFI Specification 2.10, §3.1.3 "Load Options" and §10.3.5.1 "Hard Drive
  Media Device Path".
- Linux `Documentation/filesystems/efivarfs.rst`: the 4-byte attribute
  prefix on every variable file.
- systemd Discoverable Partitions Specification: ESP type GUID.
- udev(7) and `ID_PART_ENTRY_TYPE` in the udev database.
- efibootmgr source for the device-path formatting the host output used.
