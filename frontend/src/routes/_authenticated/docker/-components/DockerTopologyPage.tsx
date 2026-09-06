import { Icon } from "@iconify/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type CSSProperties } from "react";

import { linuxio, type ContainerInfo, type LiveContainer } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppPaper from "@/components/ui/AppPaper";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppTypography from "@/components/ui/AppTypography";
import { getContainerStatusColor } from "@/constants/statusColors";
import { useCapability } from "@/hooks/useCapabilities";
import {
  getContainerDisplayState,
  getContainerName,
} from "@/utils/dockerContainer";
import { formatFileSize, formatThroughput } from "@/utils/formaters";

import {
  buildTopology,
  formatHostEndpoint,
  getHostPortBindings,
  type TopologySelection,
} from "./dockerTopology";
import "./docker-topology.css";

const ContainerIdentity = ({ container }: { container: ContainerInfo }) => (
  <span className="docker-topology__identity">
    <span className="docker-topology__app-icon">
      {container.icon ? (
        <DockerIcon alt="" identifier={container.icon} size={32} />
      ) : (
        <Icon aria-hidden="true" icon="mdi:cube-outline" width={28} />
      )}
    </span>
    <span className="docker-topology__copy">
      <AppTypography
        component="span"
        fontWeight={600}
        noWrap
        title={getContainerName(container)}
        variant="body2"
      >
        {getContainerName(container)}
      </AppTypography>
      <AppTypography component="span" color="text.secondary" variant="caption">
        {getContainerDisplayState(container)}
      </AppTypography>
    </span>
    <span
      aria-hidden="true"
      className="docker-topology__state"
      style={{
        background: getContainerStatusColor(
          getContainerDisplayState(container),
        ),
      }}
    />
  </span>
);

const Activity = ({ metrics }: { metrics?: LiveContainer }) => (
  <AppTypography
    component="span"
    className="docker-topology__activity"
    variant="caption"
  >
    {(["rx", "tx"] as const).map((direction) => {
      const value = metrics?.[`${direction}_bytes_per_sec`];
      return (
        <span
          className={`docker-topology__rate docker-topology__rate--${direction}`}
          data-active={value !== undefined && value > 0}
          key={direction}
        >
          <span aria-hidden="true">{direction === "rx" ? "↓" : "↑"}</span>
          <span className="docker-topology__sr-only">
            {direction === "rx" ? "Received" : "Sent"}:{" "}
          </span>
          {value === undefined ? "Unavailable" : formatThroughput(value)}
        </span>
      );
    })}
  </AppTypography>
);

interface DockerTopologyPageProps {
  selection: TopologySelection;
  onSelectionChange: (selection: TopologySelection) => void;
}

const DockerTopologyPage = ({
  selection,
  onSelectionChange,
}: DockerTopologyPageProps) => {
  const { data: containers } = useSuspenseQuery({
    ...linuxio.docker.list_containers,
    refetchInterval: 10000,
  });
  const { data: networks } = useSuspenseQuery({
    ...linuxio.docker.list_networks,
    refetchInterval: 10000,
  });
  const { isEnabled: monitoringEnabled } = useCapability("monitoringAvailable");
  const live = useQuery({
    ...linuxio.monitoring.get_live,
    enabled: monitoringEnabled,
    refetchInterval: 2000,
  });
  const sample = live.data?.containers;
  const [expiredSample, setExpiredSample] = useState<number>();
  const [paused, setPaused] = useState(false);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const detailHeading = useRef<HTMLElement>(null);

  // Bring the inspector into view after selection, including when it sits
  // below the map on a narrow screen. Keyboard users can continue into it.
  useEffect(() => {
    if (selection.container || selection.network)
      detailHeading.current?.focus();
  }, [selection.container, selection.network]);

  // Expire unchanged samples even if the next request stalls or the service
  // keeps returning the same cached payload. Compare server times to server
  // times below, so a browser clock offset cannot make old metrics look live.
  useEffect(() => {
    if (sample?.captured_at_ms === undefined) return;
    const timestamp = sample.captured_at_ms;
    const timer = window.setTimeout(() => setExpiredSample(timestamp), 15000);
    return () => window.clearTimeout(timer);
  }, [sample?.captured_at_ms]);
  const fresh =
    monitoringEnabled &&
    !live.isError &&
    sample !== undefined &&
    sample.captured_at_ms !== expiredSample &&
    (live.data?.captured_at_ms ?? 0) - sample.captured_at_ms < 15000;
  const metrics = new Map(
    (fresh ? sample.items : []).map((item) => [item.id, item]),
  );
  // Monitoring uses 12-character IDs; Docker inventory retains full IDs.
  // Match the regular container page's exact-ID-first lookup.
  const metricsFor = (container: ContainerInfo) =>
    container.State === "running"
      ? (metrics.get(container.Id) ?? metrics.get(container.Id.slice(0, 12)))
      : undefined;
  const topology = buildTopology(containers, networks, collapsed);
  const selectedContainer = selection.container
    ? topology.inventory.get(selection.container)
    : undefined;
  const selectedNetwork = selectedContainer
    ? undefined
    : networks.find((network) => network.Id === selection.network);
  const hasSelection = Boolean(selectedContainer || selectedNetwork);
  const attachedNetworks = selectedContainer
    ? networks.filter(
        (network) => selectedContainer.Id in (network.Containers ?? {}),
      )
    : [];
  const selectedMetrics = selectedContainer
    ? metricsFor(selectedContainer)
    : undefined;
  const bindings = getHostPortBindings(
    selectedContainer
      ? [selectedContainer]
      : selectedNetwork
        ? containers.filter(
            (container) => container.Id in (selectedNetwork.Containers ?? {}),
          )
        : containers,
  );
  const selectionMissing = Boolean(
    (selection.container || selection.network) && !hasSelection,
  );
  const related = (container: ContainerInfo) =>
    !hasSelection ||
    selectedContainer?.Id === container.Id ||
    Boolean(
      selectedNetwork && container.Id in (selectedNetwork.Containers ?? {}),
    );
  const toggleStack = (project: string) =>
    setCollapsed((previous) => {
      const next = new Set(previous);
      if (next.has(project)) next.delete(project);
      else next.add(project);
      return next;
    });

  return (
    <div className="docker-topology-page" data-paused={paused}>
      <header className="docker-topology__toolbar">
        <div>
          <AppTypography component="h1" variant="h5" fontWeight={600}>
            Docker topology
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            Your applications and their connections.
          </AppTypography>
        </div>
        <div className="docker-topology__tools">
          <Chip
            label={
              fresh
                ? "Live activity"
                : live.isFetching
                  ? "Connecting to metrics"
                  : "Metrics unavailable"
            }
            size="small"
            variant="soft"
          />
          <AppButton
            aria-pressed={paused}
            color="inherit"
            onClick={() => setPaused(!paused)}
            size="small"
            variant="outlined"
          >
            {paused ? "Resume animation" : "Pause animation"}
          </AppButton>
        </div>
      </header>
      <div className="docker-topology__workspace">
        <AppPaper className="docker-topology__map" variant="outlined">
          <div className="docker-topology__map-header">
            <div className="docker-topology__counts">
              <AppTypography variant="body2">
                <strong>{containers.length}</strong> containers
              </AppTypography>
              <AppTypography variant="body2">
                <strong>{networks.length}</strong> networks
              </AppTypography>
              <AppTypography variant="body2">
                <strong>
                  {
                    containers.filter(
                      (container) => container.State === "running",
                    ).length
                  }
                </strong>{" "}
                running
              </AppTypography>
            </div>
            {hasSelection && (
              <AppButton
                color="inherit"
                onClick={() => onSelectionChange({})}
                size="small"
              >
                Clear selection
              </AppButton>
            )}
          </div>
          <div
            className="docker-topology__canvas"
            style={
              { "--topology-height": `${topology.height}px` } as CSSProperties
            }
          >
            <AppTypography
              className="docker-topology__column-label docker-topology__column-label--networks"
              component="h2"
              variant="overline"
            >
              Networks
            </AppTypography>
            <AppTypography
              className="docker-topology__column-label docker-topology__column-label--apps"
              component="h2"
              variant="overline"
            >
              Applications
            </AppTypography>
            <svg
              aria-hidden="true"
              className="docker-topology__edges"
              preserveAspectRatio="none"
              viewBox={`0 0 100 ${topology.height}`}
            >
              {topology.edges.map((edge) => (
                <path
                  className="docker-topology__edge"
                  d={edge.path}
                  data-highlighted={
                    selectedContainer?.Id === edge.container ||
                    selectedNetwork?.Id === edge.network
                  }
                  key={`${edge.network}:${edge.container}`}
                  vectorEffect="non-scaling-stroke"
                />
              ))}
            </svg>
            <ul
              aria-label="Docker networks"
              className="docker-topology__networks"
            >
              {topology.networkNodes.map(({ network, y }) => (
                <li
                  key={network.Id}
                  style={{ "--node-top": `${y}px` } as CSSProperties}
                >
                  <AppButton
                    aria-label={`Inspect network ${network.Name}`}
                    aria-pressed={selectedNetwork?.Id === network.Id}
                    className="docker-topology__node docker-topology__network-node"
                    color="inherit"
                    data-related={
                      !hasSelection ||
                      selectedNetwork?.Id === network.Id ||
                      Boolean(
                        selectedContainer &&
                        selectedContainer.Id in (network.Containers ?? {}),
                      )
                    }
                    onClick={() => onSelectionChange({ network: network.Id })}
                  >
                    <span className="docker-topology__identity">
                      <span className="docker-topology__network-icon">
                        <Icon aria-hidden="true" icon="mdi:lan" width={24} />
                      </span>
                      <span className="docker-topology__copy">
                        <AppTypography
                          component="span"
                          fontWeight={600}
                          noWrap
                          title={network.Name}
                          variant="body2"
                        >
                          {network.Name}
                        </AppTypography>
                        <AppTypography
                          component="span"
                          color="text.secondary"
                          variant="caption"
                        >
                          {network.Driver}
                          {network.Internal ? " · Internal" : ""}
                        </AppTypography>
                      </span>
                    </span>
                    <AppTypography
                      component="span"
                      color="text.secondary"
                      variant="caption"
                    >
                      {Object.keys(network.Containers ?? {}).length} attached
                    </AppTypography>
                  </AppButton>
                </li>
              ))}
              {networks.length === 0 && (
                <li className="docker-topology__empty">
                  <AppTypography color="text.secondary" variant="body2">
                    No Docker networks are available.
                  </AppTypography>
                </li>
              )}
            </ul>
            <ul
              aria-label="Docker applications"
              className="docker-topology__applications"
            >
              {topology.groups.map((group) => (
                <li
                  className="docker-topology__group"
                  data-stack={Boolean(group.project)}
                  key={
                    group.project
                      ? `stack:${group.project}`
                      : group.members[0].Id
                  }
                  style={
                    {
                      "--node-top": `${group.top}px`,
                      "--group-height": `${group.height}px`,
                    } as CSSProperties
                  }
                >
                  {group.project && (
                    <AppButton
                      aria-expanded={!group.folded}
                      aria-label={`${group.folded ? "Expand" : "Collapse"} stack ${group.project}`}
                      className="docker-topology__stack-header"
                      color="inherit"
                      onClick={() => toggleStack(group.project!)}
                    >
                      <Icon
                        aria-hidden="true"
                        icon={
                          group.folded
                            ? "mdi:chevron-right"
                            : "mdi:chevron-down"
                        }
                        width={18}
                      />
                      <AppTypography
                        component="span"
                        noWrap
                        title={group.project}
                        variant="caption"
                        fontWeight={600}
                      >
                        {group.project}
                      </AppTypography>
                      <AppTypography
                        component="span"
                        color="text.secondary"
                        variant="caption"
                      >
                        {group.members.length}
                      </AppTypography>
                    </AppButton>
                  )}
                  {group.nodes.map(({ container }) => (
                    <AppButton
                      aria-label={`Inspect container ${getContainerName(container)}`}
                      aria-pressed={selectedContainer?.Id === container.Id}
                      className="docker-topology__node docker-topology__container-node"
                      color="inherit"
                      data-related={related(container)}
                      key={container.Id}
                      onClick={() =>
                        onSelectionChange({ container: container.Id })
                      }
                    >
                      <ContainerIdentity container={container} />
                      <Activity metrics={metricsFor(container)} />
                    </AppButton>
                  ))}
                </li>
              ))}
              {topology.inventory.size === 0 && (
                <li className="docker-topology__empty">
                  <AppTypography color="text.secondary" variant="body2">
                    No containers yet. Create a container to see its connections
                    here.
                  </AppTypography>
                  <AppRouterLinkButton to="/docker/containers">
                    Open containers
                  </AppRouterLinkButton>
                </li>
              )}
            </ul>
          </div>
          <footer className="docker-topology__legend">
            <AppTypography color="text.secondary" variant="caption">
              <span aria-hidden="true" className="docker-topology__line-key" />
              Network attachment
            </AppTypography>
            <AppTypography color="text.secondary" variant="caption">
              ↓ Received · ↑ Sent · Activity is per container
            </AppTypography>
          </footer>
        </AppPaper>
        <AppPaper
          aria-label="Topology details"
          className="docker-topology__inspector"
          role="region"
          variant="outlined"
        >
          <AppTypography color="text.secondary" variant="overline">
            {selectedContainer
              ? "Container"
              : selectedNetwork
                ? "Network"
                : "Overview"}
          </AppTypography>
          <AppTypography
            className="docker-topology__detail-title"
            component="h2"
            fontWeight={600}
            ref={detailHeading}
            tabIndex={-1}
            variant="h5"
          >
            {selectedContainer
              ? getContainerName(selectedContainer)
              : (selectedNetwork?.Name ?? "Explore your Docker host")}
          </AppTypography>
          {!hasSelection && (
            <AppTypography color="text.secondary" variant="body2">
              {selectionMissing
                ? "This resource is no longer listed. Select another application or network."
                : "Select an application or network to trace its connections and inspect its details."}
            </AppTypography>
          )}
          {selectionMissing && (
            <AppButton onClick={() => onSelectionChange({})}>
              Clear selection
            </AppButton>
          )}
          {selectedContainer && (
            <>
              <Chip
                label={getContainerDisplayState(selectedContainer)}
                size="small"
                variant="soft"
              />
              <AppTypography
                className="docker-topology__detail-value"
                color="text.secondary"
                variant="body2"
              >
                {selectedContainer.Image ||
                  "Container details are unavailable."}
              </AppTypography>
              <div className="docker-topology__metrics">
                <div>
                  <AppTypography color="text.secondary" variant="caption">
                    CPU
                  </AppTypography>
                  <AppTypography component="p" fontWeight={600} variant="h6">
                    {selectedMetrics
                      ? `${selectedMetrics.cpu_percent.toFixed(1)}%`
                      : "—"}
                  </AppTypography>
                </div>
                <div>
                  <AppTypography color="text.secondary" variant="caption">
                    Memory
                  </AppTypography>
                  <AppTypography component="p" fontWeight={600} variant="h6">
                    {selectedMetrics
                      ? formatFileSize(selectedMetrics.memory_bytes)
                      : "—"}
                  </AppTypography>
                </div>
              </div>
              <Activity metrics={selectedMetrics} />
              <AppTypography
                className="docker-topology__section-title"
                component="h3"
                fontWeight={600}
                variant="body2"
              >
                Network attachments
              </AppTypography>
              {attachedNetworks.length === 0 && (
                <AppTypography color="text.secondary" variant="body2">
                  No listed network
                </AppTypography>
              )}
              {attachedNetworks.map((network) => (
                <div className="docker-topology__detail-item" key={network.Id}>
                  <AppButton
                    color="inherit"
                    onClick={() => onSelectionChange({ network: network.Id })}
                  >
                    {network.Name}
                  </AppButton>
                  {[
                    network.Containers?.[selectedContainer.Id]?.IPv4Address,
                    network.Containers?.[selectedContainer.Id]?.IPv6Address,
                  ]
                    .filter(Boolean)
                    .map((address) => (
                      <AppTypography
                        className="docker-topology__mono"
                        component="bdi"
                        color="text.secondary"
                        key={address}
                        variant="caption"
                      >
                        {address}
                      </AppTypography>
                    ))}
                </div>
              ))}
              {containers.some(
                (container) => container.Id === selectedContainer.Id,
              ) && (
                <AppRouterLinkButton
                  className="docker-topology__open-resource"
                  search={{ container: selectedContainer.Id }}
                  to="/docker/containers"
                  variant="outlined"
                >
                  Open container details
                </AppRouterLinkButton>
              )}
            </>
          )}
          {selectedNetwork && (
            <>
              <AppTypography color="text.secondary" variant="body2">
                {selectedNetwork.Driver} · {selectedNetwork.Scope}
                {selectedNetwork.Internal ? " · Internal network" : ""}
              </AppTypography>
              {(selectedNetwork.IPAM?.Config ?? []).map((config, index) => (
                <div className="docker-topology__detail-item" key={index}>
                  {config.Subnet && (
                    <AppTypography
                      className="docker-topology__mono"
                      component="bdi"
                      variant="body2"
                    >
                      {config.Subnet}
                    </AppTypography>
                  )}
                  {config.Gateway && (
                    <AppTypography
                      className="docker-topology__mono"
                      component="bdi"
                      color="text.secondary"
                      variant="caption"
                    >
                      Gateway {config.Gateway}
                    </AppTypography>
                  )}
                </div>
              ))}
              <AppTypography
                className="docker-topology__section-title"
                component="h3"
                fontWeight={600}
                variant="body2"
              >
                Attached containers
              </AppTypography>
              {Object.keys(selectedNetwork.Containers ?? {}).length === 0 && (
                <AppTypography color="text.secondary" variant="body2">
                  No attached containers.
                </AppTypography>
              )}
              {Object.entries(selectedNetwork.Containers ?? {}).map(
                ([id, endpoint]) => (
                  <div className="docker-topology__detail-item" key={id}>
                    <AppButton
                      color="inherit"
                      onClick={() => onSelectionChange({ container: id })}
                    >
                      {endpoint.Name || id.slice(0, 12)}
                    </AppButton>
                    {[endpoint.IPv4Address, endpoint.IPv6Address]
                      .filter(Boolean)
                      .map((address) => (
                        <AppTypography
                          className="docker-topology__mono"
                          component="bdi"
                          color="text.secondary"
                          key={address}
                          variant="caption"
                        >
                          {address}
                        </AppTypography>
                      ))}
                  </div>
                ),
              )}
            </>
          )}
          <AppTypography
            className="docker-topology__section-title"
            component="h3"
            fontWeight={600}
            variant="body2"
          >
            Published ports
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Host endpoint → container port
          </AppTypography>
          <ul
            aria-label="Host port bindings"
            className="docker-topology__ports"
          >
            {bindings.map((binding) => (
              <li
                key={`${binding.container.Id}:${binding.hostAddress}:${binding.hostPort}:${binding.containerPort}:${binding.protocol}`}
              >
                <AppButton
                  color="inherit"
                  onClick={() =>
                    onSelectionChange({ container: binding.container.Id })
                  }
                >
                  {getContainerName(binding.container)}
                </AppButton>
                <AppTypography
                  className="docker-topology__mono"
                  component="bdi"
                  variant="caption"
                >
                  {formatHostEndpoint(binding)} → {binding.containerPort}/
                  {binding.protocol}
                </AppTypography>
              </li>
            ))}
          </ul>
          {bindings.length === 0 && (
            <AppTypography color="text.secondary" variant="body2">
              No published ports. Publish a container port to see it here.
            </AppTypography>
          )}
          <AppTypography
            className="docker-topology__note"
            color="text.secondary"
            variant="caption"
          >
            Attachments show network membership. Activity shows total container
            traffic, not traffic on individual connections.
          </AppTypography>
        </AppPaper>
      </div>
    </div>
  );
};

export default DockerTopologyPage;
