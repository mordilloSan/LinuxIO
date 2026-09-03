import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";

import { type ContainerInfo, linuxio } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { getContainerStatusColor } from "@/constants/statusColors";
import {
  getContainerDisplayState,
  getContainerName,
} from "@/utils/dockerContainer";

import "./docker-topology.css";

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

const formatHostEndpoint = (binding: HostPortBinding) => {
  const address =
    binding.hostAddress === "*"
      ? "*"
      : binding.hostAddress.includes(":")
        ? `[${binding.hostAddress}]`
        : binding.hostAddress;
  return `${address}:${binding.hostPort}/${binding.protocol}`;
};

const ContainerNode = ({
  addresses = [],
  container,
  name,
}: {
  addresses?: string[];
  container?: ContainerInfo;
  name: string;
}) => {
  const state = container ? getContainerDisplayState(container) : "Unknown";
  return (
    <div className="docker-topology__container-node">
      <Icon aria-hidden="true" height={20} icon="mdi:cube-outline" width={20} />
      <div className="docker-topology__container-copy">
        <AppTypography fontWeight={600} variant="body2">
          {name}
        </AppTypography>
        {addresses.map((address) => (
          <AppTypography
            className="docker-topology__mono"
            color="text.secondary"
            component="bdi"
            key={address}
            variant="caption"
          >
            {address}
          </AppTypography>
        ))}
      </div>
      <Chip
        color={container ? getContainerStatusColor(state) : "default"}
        label={state}
        size="xsmall"
        variant="soft"
      />
    </div>
  );
};

const VisualizationHeader = ({
  count,
  description,
  icon,
  id,
  title,
}: {
  count: string;
  description: string;
  icon: string;
  id: string;
  title: string;
}) => (
  <header className="docker-topology__header">
    <div className="docker-topology__heading">
      <Icon aria-hidden="true" height={24} icon={icon} width={24} />
      <div>
        <AppTypography component="h2" fontWeight={600} id={id} variant="h6">
          {title}
        </AppTypography>
        <AppTypography color="text.secondary" variant="body2">
          {description}
        </AppTypography>
      </div>
    </div>
    <Chip label={count} size="small" variant="soft" />
  </header>
);

const DockerTopologyPage = () => {
  const { data: containers } = useSuspenseQuery({
    ...linuxio.docker.list_containers,
    refetchInterval: 10000,
  });
  const { data: networks } = useSuspenseQuery({
    ...linuxio.docker.list_networks,
    refetchInterval: 10000,
  });
  const containersById = new Map(
    containers.map((container) => [container.Id, container]),
  );
  const attachedContainerIds = new Set(
    networks.flatMap((network) => Object.keys(network.Containers ?? {})),
  );
  const unattachedContainers = containers.filter(
    (container) => !attachedContainerIds.has(container.Id),
  );
  const portBindings = getHostPortBindings(containers);

  return (
    <div className="docker-topology-page">
      <FrostedCard
        aria-labelledby="docker-network-topology-title"
        className="docker-topology__card"
        role="region"
      >
        <VisualizationHeader
          count={`${networks.length} network${networks.length === 1 ? "" : "s"}`}
          description="Docker networks and their attached containers."
          icon="mdi:lan"
          id="docker-network-topology-title"
          title="Network topology"
        />
        <div className="docker-topology__body">
          {networks.length > 0 ? (
            <ul
              aria-label="Docker network topology"
              className="docker-topology__network-list"
            >
              {networks.map((network) => {
                const connected = Object.entries(network.Containers ?? {}).sort(
                  ([, left], [, right]) => left.Name.localeCompare(right.Name),
                );
                const subnets = (network.IPAM?.Config ?? [])
                  .map((config) => config.Subnet)
                  .filter(Boolean);
                return (
                  <li className="docker-topology__network-row" key={network.Id}>
                    <div className="docker-topology__network-node">
                      <div className="docker-topology__node-title">
                        <Icon
                          aria-hidden="true"
                          height={20}
                          icon="mdi:lan"
                          width={20}
                        />
                        <AppTypography fontWeight={600} variant="body2">
                          {network.Name}
                        </AppTypography>
                      </div>
                      <div className="docker-topology__chips">
                        <Chip
                          label={network.Driver}
                          size="xsmall"
                          variant="soft"
                        />
                        {network.Internal && (
                          <Chip label="Internal" size="xsmall" variant="soft" />
                        )}
                      </div>
                      {subnets.map((subnet) => (
                        <AppTypography
                          className="docker-topology__mono"
                          color="text.secondary"
                          component="bdi"
                          key={subnet}
                          variant="caption"
                        >
                          {subnet}
                        </AppTypography>
                      ))}
                    </div>
                    <Icon
                      aria-hidden="true"
                      className="docker-topology__connector"
                      height={24}
                      icon="mdi:lan-connect"
                      width={24}
                    />
                    <div className="docker-topology__containers">
                      {connected.length > 0 ? (
                        connected.map(([containerId, endpoint]) => (
                          <ContainerNode
                            addresses={[
                              endpoint.IPv4Address,
                              endpoint.IPv6Address,
                            ].filter((address): address is string =>
                              Boolean(address),
                            )}
                            container={containersById.get(containerId)}
                            key={containerId}
                            name={endpoint.Name || containerId.slice(0, 12)}
                          />
                        ))
                      ) : (
                        <AppTypography color="text.secondary" variant="body2">
                          No attached containers.
                        </AppTypography>
                      )}
                    </div>
                  </li>
                );
              })}
              {unattachedContainers.length > 0 && (
                <li className="docker-topology__network-row">
                  <div className="docker-topology__network-node">
                    <div className="docker-topology__node-title">
                      <Icon
                        aria-hidden="true"
                        height={20}
                        icon="mdi:lan-disconnect"
                        width={20}
                      />
                      <AppTypography fontWeight={600} variant="body2">
                        No listed network
                      </AppTypography>
                    </div>
                  </div>
                  <Icon
                    aria-hidden="true"
                    className="docker-topology__connector"
                    height={24}
                    icon="mdi:lan-disconnect"
                    width={24}
                  />
                  <div className="docker-topology__containers">
                    {unattachedContainers.map((container) => (
                      <ContainerNode
                        container={container}
                        key={container.Id}
                        name={getContainerName(container)}
                      />
                    ))}
                  </div>
                </li>
              )}
            </ul>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No Docker networks are available.
            </AppTypography>
          )}
        </div>
      </FrostedCard>

      <FrostedCard
        aria-labelledby="docker-host-ports-title"
        className="docker-topology__card"
        role="region"
      >
        <VisualizationHeader
          count={`${portBindings.length} binding${portBindings.length === 1 ? "" : "s"}`}
          description="Published Docker ports across running and stopped containers."
          icon="mdi:server-network"
          id="docker-host-ports-title"
          title="Host port bindings"
        />
        <div className="docker-topology__body">
          {portBindings.length > 0 ? (
            <ul
              aria-label="Host port bindings"
              className="docker-topology__port-list"
            >
              {portBindings.map((binding) => {
                const name = getContainerName(binding.container);
                return (
                  <li
                    className="docker-topology__port-row"
                    key={`${binding.container.Id}:${binding.hostAddress}:${binding.hostPort}:${binding.containerPort}:${binding.protocol}`}
                  >
                    <div className="docker-topology__endpoint">
                      <AppTypography color="text.secondary" variant="caption">
                        {binding.hostAddress === "*"
                          ? "Host · All interfaces"
                          : "Host"}
                      </AppTypography>
                      <AppTypography
                        className="docker-topology__mono"
                        component="bdi"
                        fontWeight={600}
                        variant="body2"
                      >
                        {formatHostEndpoint(binding)}
                      </AppTypography>
                    </div>
                    <Icon
                      aria-hidden="true"
                      className="docker-topology__flow-arrow"
                      height={20}
                      icon="mdi:chevron-right"
                      width={20}
                    />
                    <ContainerNode container={binding.container} name={name} />
                    <Icon
                      aria-hidden="true"
                      className="docker-topology__flow-arrow"
                      height={20}
                      icon="mdi:chevron-right"
                      width={20}
                    />
                    <div className="docker-topology__endpoint">
                      <AppTypography color="text.secondary" variant="caption">
                        Container port
                      </AppTypography>
                      <AppTypography
                        className="docker-topology__mono"
                        component="bdi"
                        fontWeight={600}
                        variant="body2"
                      >
                        {binding.containerPort}/{binding.protocol}
                      </AppTypography>
                    </div>
                  </li>
                );
              })}
            </ul>
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No published Docker ports. Publish a container port to see it
              here.
            </AppTypography>
          )}
        </div>
      </FrostedCard>
    </div>
  );
};

export default DockerTopologyPage;
