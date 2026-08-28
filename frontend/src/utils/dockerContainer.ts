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
