import type { ContainerInfo, DockerNetwork, MonitoringLive } from "@/api";

// Docker inventory uses full IDs; live monitoring reports their first 12 characters.
export const topologyNextcloudId = "0123456789ab".padEnd(64, "c");

export const topologyContainers: ContainerInfo[] = [
  {
    Id: topologyNextcloudId,
    Names: ["/nextcloud"],
    Image: "nextcloud:31",
    icon: "nextcloud",
    Created: 0,
    State: "running",
    Status: "Up 2 hours (healthy)",
    Labels: { "com.docker.compose.project": "cloud" },
    Ports: [
      { IP: "0.0.0.0", PublicPort: 8080, PrivatePort: 80, Type: "tcp" },
      { IP: "::", PublicPort: 8080, PrivatePort: 80, Type: "tcp" },
    ],
  },
  {
    Id: "postgres-id",
    Names: ["/postgres"],
    Image: "postgres:17",
    icon: "postgres",
    Created: 0,
    State: "running",
    Status: "Up 2 hours",
    Labels: { "com.docker.compose.project": "cloud" },
  },
  {
    Id: "redis-id",
    Names: ["/redis"],
    Image: "redis:8",
    icon: "redis",
    Created: 0,
    State: "running",
    Status: "Up 2 hours",
    Labels: { "com.docker.compose.project": "cloud" },
  },
  {
    Id: "jellyfin-id",
    Names: ["/jellyfin"],
    Image: "jellyfin/jellyfin:latest",
    icon: "jellyfin",
    Created: 0,
    State: "running",
    Status: "Up 1 day (healthy)",
    Ports: [
      { IP: "192.168.1.20", PublicPort: 8096, PrivatePort: 8096, Type: "tcp" },
    ],
  },
  {
    Id: "backup-id",
    Names: ["/backup"],
    Image: "restic/restic:latest",
    icon: "backup",
    Created: 0,
    State: "exited",
    Status: "Exited (0)",
  },
];

export const topologyNetworks: DockerNetwork[] = [
  {
    Id: "proxy-id",
    Name: "proxy",
    Driver: "bridge",
    Scope: "local",
    Attachable: false,
    ConfigOnly: false,
    Ingress: false,
    IPAM: { Config: [{ Subnet: "172.20.0.0/16", Gateway: "172.20.0.1" }] },
    Containers: {
      [topologyNextcloudId]: {
        Name: "nextcloud",
        IPv4Address: "172.20.0.2/16",
      },
      "jellyfin-id": { Name: "jellyfin", IPv4Address: "172.20.0.3/16" },
    },
  },
  {
    Id: "cloud-id",
    Name: "cloud_internal",
    Driver: "bridge",
    Scope: "local",
    Internal: true,
    Attachable: false,
    ConfigOnly: false,
    Ingress: false,
    IPAM: { Config: [{ Subnet: "172.21.0.0/16", Gateway: "172.21.0.1" }] },
    Containers: {
      [topologyNextcloudId]: {
        Name: "nextcloud",
        IPv4Address: "172.21.0.2/16",
      },
      "postgres-id": { Name: "postgres", IPv4Address: "172.21.0.3/16" },
      "redis-id": { Name: "redis", IPv4Address: "172.21.0.4/16" },
    },
  },
  {
    Id: "bridge-id",
    Name: "bridge",
    Driver: "bridge",
    Scope: "local",
    Attachable: false,
    ConfigOnly: false,
    Ingress: false,
    Containers: {},
    IPAM: { Config: [{ Subnet: "172.17.0.0/16", Gateway: "172.17.0.1" }] },
  },
];

export const topologyLive = (timestamp = Date.now()): MonitoringLive => ({
  captured_at_ms: timestamp,
  uptime_seconds: 86400,
  cpu: {
    percent: 0,
    per_core_percent: [],
    breakdown: { user: 0, system: 0, iowait: 0, steal: 0, idle: 100 },
    load_average: [],
    frequencies_mhz: [],
    temperatures: {},
  },
  memory: {
    total_bytes: 0,
    used_bytes: 0,
    available_bytes: 0,
    free_bytes: 0,
    cached_bytes: 0,
    buffers_bytes: 0,
    shared_bytes: 0,
    swap_total_bytes: 0,
    swap_free_bytes: 0,
    zfs_arc_bytes: 0,
    docker_used_bytes: 0,
  },
  disks: {},
  disk_io: {
    read_bytes_per_sec: 0,
    write_bytes_per_sec: 0,
    read_ops_per_sec: 0,
    write_ops_per_sec: 0,
  },
  interfaces: {},
  filesystems: [],
  sensors: [],
  gpus: {},
  smart: {},
  containers: {
    captured_at_ms: timestamp,
    items: topologyContainers
      .filter((container) => container.State === "running")
      .map((container, index) => ({
        id: container.Id.slice(0, 12),
        name: container.Names[0],
        cpu_percent: 1.2 + index * 2.3,
        memory_bytes: (128 + index * 64) * 1024 * 1024,
        rx_bytes_per_sec: (index + 1) * 18500,
        tx_bytes_per_sec: (index + 1) * 82400,
      })),
  },
});
