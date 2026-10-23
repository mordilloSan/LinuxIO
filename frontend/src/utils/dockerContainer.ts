import type { ContainerInfo, ContainerPort } from "@/api";

/** First Docker name without its leading slash, else the short container ID. */
export const getContainerName = (container: ContainerInfo) =>
  container.Names?.[0]?.replace(/^\//, "") || container.Id.slice(0, 12);

/** Health-aware display state understood by `getContainerStatusColor`. */
export const getContainerDisplayState = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "Unhealthy";
  if (status.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return container.State || "Unknown";
};

/** Ports with duplicate host/IP bindings collapsed, sorted by private port. */
export const getDedupedPorts = (container: ContainerInfo): ContainerPort[] => {
  const seen = new Set<string>();
  return (container.Ports ?? [])
    .filter((port) => {
      const key = port.PublicPort
        ? `${port.PrivatePort}/${port.Type}:${port.PublicPort}`
        : `${port.PrivatePort}/${port.Type}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .sort(
      (a, b) => a.PrivatePort - b.PrivatePort || a.Type.localeCompare(b.Type),
    );
};

/** Bind and named-volume mounts; tmpfs and other mount types are hidden. */
export const getContainerMounts = (container: ContainerInfo) =>
  (container.Mounts ?? []).filter(
    (mount) => mount.Type === "bind" || mount.Type === "volume",
  );

/** `host:container/proto`, or `container/proto` when the port is unpublished. */
export const formatContainerPort = (port: ContainerPort) =>
  port.PublicPort
    ? `${port.PublicPort}:${port.PrivatePort}/${port.Type}`
    : `${port.PrivatePort}/${port.Type}`;

/** Compose service name from its label, `-` for containers outside Compose. */
export const getContainerServiceName = (container: ContainerInfo) =>
  container.Labels?.["com.docker.compose.service"] || "-";

/** Compact age such as `45s`, `12m`, `3h 5m` or `2d 4h`; `-` for future times. */
export const formatContainerUptime = (createdUnix: number) => {
  const secs = Math.floor(Date.now() / 1000) - createdUnix;
  if (secs < 0) return "-";
  if (secs < 60) return `${secs}s`;
  const minutes = Math.floor(secs / 60) % 60;
  const hours = Math.floor(secs / 3600) % 24;
  const days = Math.floor(secs / 86400);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
};
