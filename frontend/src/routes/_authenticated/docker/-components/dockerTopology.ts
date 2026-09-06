import type { ContainerInfo, DockerNetwork } from "@/api";
import { getContainerName } from "@/utils/dockerContainer";

import { groupContainersByStack } from "./containerStacks";

export interface TopologySelection {
  container?: string;
  network?: string;
}

interface HostPortBinding {
  container: ContainerInfo;
  containerPort: number;
  hostAddress: string;
  hostPort: number;
  protocol: string;
}

export const getHostPortBindings = (
  containers: ContainerInfo[],
): HostPortBinding[] => {
  const seen = new Set<string>();
  const bindings: HostPortBinding[] = [];
  for (const container of containers) {
    for (const port of container.Ports ?? []) {
      if (port.PublicPort === undefined) continue;
      const hostAddress =
        !port.IP || port.IP === "0.0.0.0" || port.IP === "::" ? "*" : port.IP;
      const key = `${container.Id}:${hostAddress}:${port.PublicPort}:${port.PrivatePort}:${port.Type}`;
      if (seen.has(key)) continue;
      seen.add(key);
      bindings.push({
        container,
        containerPort: port.PrivatePort,
        hostAddress,
        hostPort: port.PublicPort,
        protocol: port.Type,
      });
    }
  }
  return bindings.sort(
    (left, right) =>
      left.hostPort - right.hostPort ||
      left.protocol.localeCompare(right.protocol) ||
      left.hostAddress.localeCompare(right.hostAddress) ||
      getContainerName(left.container).localeCompare(
        getContainerName(right.container),
      ),
  );
};

export const formatHostEndpoint = (binding: HostPortBinding) => {
  const address =
    binding.hostAddress === "*"
      ? "*"
      : binding.hostAddress.includes(":")
        ? `[${binding.hostAddress}]`
        : binding.hostAddress;
  return `${address}:${binding.hostPort}/${binding.protocol}`;
};

// ponytail: two columns; add graph routing if dense installations need it.
// Fixed positions keep polling from moving nodes. The SVG spans
// only the gap; HTML buttons keep names, focus and narrow-screen reflow native.
export const buildTopology = (
  containers: ContainerInfo[],
  networks: DockerNetwork[],
  collapsed: ReadonlySet<string>,
) => {
  const inventory = new Map(
    containers.map((container) => [container.Id, container]),
  );
  for (const network of networks) {
    for (const [id, endpoint] of Object.entries(network.Containers ?? {})) {
      if (!inventory.has(id)) {
        // Network and container inventory refresh independently. Preserve the
        // attachment while its container is absent from the list response.
        inventory.set(id, {
          Id: id,
          Names: [endpoint.Name || id.slice(0, 12)],
          Created: 0,
          Image: "",
          State: "Unknown",
          Status: "",
        });
      }
    }
  }
  const sorted = [...inventory.values()].sort(
    (a, b) =>
      getContainerName(a).localeCompare(getContainerName(b)) ||
      a.Id.localeCompare(b.Id),
  );
  let cursor = 76;
  const anchors = new Map<string, number>();
  const groups = groupContainersByStack(sorted).map((entry) => {
    const project = entry.type === "stack" ? entry.project : undefined;
    const members =
      entry.type === "stack" ? entry.containers : [entry.container];
    const folded = project !== undefined && collapsed.has(project);
    const top = cursor;
    if (project) cursor += 40;
    const nodes = folded
      ? []
      : members.map((container) => {
          const y = cursor;
          anchors.set(container.Id, y + 48);
          cursor += 112;
          return { container, y };
        });
    if (folded) {
      for (const container of members) anchors.set(container.Id, top + 20);
      cursor += 16;
    }
    return { project, members, folded, top, nodes, height: cursor - top };
  });
  const height = Math.max(560, cursor + 24, networks.length * 128 + 100);
  const networkNodes = [...networks]
    .sort((a, b) => a.Name.localeCompare(b.Name) || a.Id.localeCompare(b.Id))
    .map((network, index) => ({
      network,
      y: 76 + index * ((height - 200) / Math.max(1, networks.length - 1)),
    }));
  const edges = networkNodes.flatMap(({ network, y }) =>
    Object.keys(network.Containers ?? {}).flatMap((id) => {
      const target = anchors.get(id);
      return target === undefined
        ? []
        : [
            {
              network: network.Id,
              container: id,
              path: `M 0 ${y + 48} C 48 ${y + 48}, 52 ${target}, 100 ${target}`,
            },
          ];
    }),
  );
  return { groups, networkNodes, edges, height, inventory };
};
