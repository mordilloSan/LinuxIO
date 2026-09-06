# Storage topology

Storage → Topology (`/storage/topology`) connects devices, volumes and mount
points, and applications. The validated `node` search parameter preserves
selection through reloads, links, and browser navigation. Selecting an item
highlights its upstream and downstream dependencies and focuses the inspector.
Narrow layouts use selectable lists with the same details and actions.

## Inventory and ownership

`storage.get_topology` is a read-only, retry-safe bridge Call returning
`StorageTopologyInventory`. It combines a block-device inventory from
`lsblk --json --bytes --tree` with the current mount table. It does not mount,
probe SMART health, or change storage configuration. The subprocess honors
request cancellation and has a ten-second timeout.

Devices have stable paths, kernel names for joining monitoring data, byte
capacity, filesystem metadata, mount points, and parent paths. Repeated nodes
in the kernel-reported tree merge into a single device while retaining all
parents, including RAID or LVM relationships. Loop devices are omitted.
The complete mount table retains memory and kernel filesystem boundaries;
these mounts appear in the map only when an application references them.

The page refreshes inventory every 30 seconds. `storage.list_vgs` supplies
optional LVM group metadata. Existing volume and mount mutations invalidate
the topology query through the central operation invalidation map. Generated
query options and the route loader retain normal cancellation and cache
ownership. Initial inventory errors use the route error boundary.

Docker and libvirt queries run only when their capabilities are available.
Docker bind/volume sources and VM disk files match the longest enclosing host
mount path, respecting path-segment boundaries. Direct VM block devices match
reported device paths or kernel names. Unmatched sources stay visible in the
inspector. Missing monitoring, Docker, libvirt, or LVM metadata does not prevent
the block and mount inventory from rendering.

## Measurements and interpretation

`monitoring.get_live` supplies per-device read/write rates, SMART records,
and filesystem usage. It is polled every two seconds when monitoring is
available. Activity stops on a query error or when the sample timestamp does
not advance for 15 seconds. Animation can be paused and respects reduced
motion; numbers and selection remain available without animation.

Layered grain streams flow in opposite read and write directions on measured
connections. Irregular spacing, staggered timing, and fainter outer tracks
give the streams width and soft edges using SVG and CSS animation.
Device-to-volume links reflect device totals; application links use optional
container block read/write totals with fresh container samples and running
containers. Unmeasured application links (including VMs) remain static.
Read-only mounts and container binds have no write grains.
Grain speed is decorative, not a throughput scale. Grains stop for zero,
missing, or expired measurements, and freeze when animation is paused.

- Lines represent backing relationships and reported host paths. They do not
  measure I/O on a connection or attribute device traffic to an application.
  Container activity is not attributed to individual mounts or paths.
- The map compresses intermediate partitions and device-mapper layers; the
  inspector lists their reported parent relationships and LVM group metadata.
- Application path matching does not resolve symlinks, VM image backing chains,
  or filesystems inside a guest. Container image layers and sources without a
  reported local path are outside this view.
- Remote, memory, and unmatched filesystems have no invented physical-disk
  connection. Storage layouts only expose the relationships reported by the
  host inventory; this is not a complete ZFS or Btrfs pool inspector.
- Device capacity is raw reported capacity, not usable pooled space. Filesystem
  usage comes from monitoring, and shared mounts must not be summed as distinct
  physical allocations.

The automated browser fixture uses sample storage and application data. It
covers dark/light rendering, keyboard selection, URL navigation, reload,
reduced motion, pausing, and narrow layouts without mutating host storage.
