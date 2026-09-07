import { Icon } from "@iconify/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type CSSProperties } from "react";

import { linuxio, type LiveDiskRates } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppPaper from "@/components/ui/AppPaper";
import AppRouterLinkButton from "@/components/ui/AppRouterLinkButton";
import AppTopologyEdge from "@/components/ui/AppTopologyEdge";
import AppTypography from "@/components/ui/AppTypography";
import { useCapabilityState } from "@/hooks/useCapabilities";
import { formatFileSize, formatThroughput } from "@/utils/formaters";

import {
  buildStorageTopology,
  relatedStorageNodes,
  type StorageNode,
  type StorageSelection,
} from "./storageTopology";
import "./storage-topology.css";

const labels = ["Devices", "Volumes & mounts", "Applications"];
const kinds = {
  drive: "Device",
  volume: "Volume",
  mount: "Mount point",
  container: "Container",
  vm: "Virtual machine",
};

function NodeIcon({ node }: { node: StorageNode }) {
  if (node.container?.icon)
    return <DockerIcon identifier={node.container.icon} alt="" size={28} />;
  const icons = {
    drive: "mdi:harddisk",
    volume: "mdi:database-outline",
    mount: "mdi:folder-outline",
    container: "mdi:cube-outline",
    vm: "mdi:monitor",
  };
  return <Icon aria-hidden="true" icon={icons[node.kind]} width={26} />;
}

function DiskActivity({ rates }: { rates?: LiveDiskRates }) {
  return (
    <AppTypography
      component="span"
      variant="caption"
      className="storage-topology__activity"
    >
      {(["read", "write"] as const).map((direction) => {
        const value = rates?.[`${direction}_bytes_per_sec`];
        return (
          <span
            key={direction}
            className="storage-topology__rate"
            data-active={value !== undefined && value > 0}
            data-direction={direction}
          >
            {direction === "read" ? "Read" : "Write"}{" "}
            {value === undefined ? "—" : formatThroughput(value)}
          </span>
        );
      })}
    </AppTypography>
  );
}

function Usage({ node, fresh }: { node: StorageNode; fresh: boolean }) {
  const fs = fresh ? node.filesystem : undefined;
  return (
    <span className="storage-topology__usage">
      {fs ? (
        <>
          <AppTypography component="span" variant="caption">
            {formatFileSize(fs.used)} / {formatFileSize(fs.total)} used
          </AppTypography>
          <AppLinearProgress
            aria-label={`${node.label} capacity used`}
            variant="determinate"
            value={Math.max(0, Math.min(100, fs.usedPercent))}
          />
        </>
      ) : (
        <AppTypography
          component="span"
          color="text.secondary"
          variant="caption"
        >
          {node.mountpoint
            ? "Mounted · usage unavailable"
            : node.block?.fsType === "swap"
              ? "Swap"
              : "Unmounted"}
          {node.block ? ` · ${formatFileSize(node.block.sizeBytes)}` : ""}
        </AppTypography>
      )}
    </span>
  );
}

interface Props {
  selection: StorageSelection;
  onSelectionChange: (selection: StorageSelection) => void;
}

export default function StorageTopologyPage({
  selection,
  onSelectionChange,
}: Props) {
  const { data: inventory, isError: inventoryError } = useSuspenseQuery({
    ...linuxio.storage.get_topology,
    refetchInterval: 30000,
  });
  const capabilities = useCapabilityState();
  const live = useQuery({
    ...linuxio.monitoring.get_live,
    enabled: capabilities.monitoringAvailable === true,
    refetchInterval: 2000,
  });
  const groups = useQuery({
    ...linuxio.storage.list_vgs,
    refetchInterval: 30000,
  });
  const containers = useQuery({
    ...linuxio.docker.list_containers,
    enabled: capabilities.dockerAvailable === true,
    refetchInterval: 10000,
  });
  const vms = useQuery({
    ...linuxio.virt.list,
    enabled: capabilities.libvirtAvailable === true,
    refetchInterval: 10000,
  });
  const [expiredSample, setExpiredSample] = useState<number>();
  const [paused, setPaused] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (selection.node) heading.current?.focus();
  }, [selection.node]);
  useEffect(() => {
    if (live.data?.captured_at_ms === undefined) return;
    const timestamp = live.data.captured_at_ms;
    const timer = window.setTimeout(() => setExpiredSample(timestamp), 15000);
    return () => window.clearTimeout(timer);
  }, [live.data?.captured_at_ms]);
  const fresh =
    capabilities.monitoringAvailable === true &&
    !live.isError &&
    live.data !== undefined &&
    live.data.captured_at_ms !== expiredSample;
  const topology = buildStorageTopology(
    inventory,
    capabilities.monitoringAvailable ? (live.data?.filesystems ?? []) : [],
    groups.data ?? [],
    capabilities.dockerAvailable ? (containers.data ?? []) : [],
    capabilities.libvirtAvailable ? (vms.data ?? []) : [],
  );
  const selected = selection.node
    ? topology.inventory.get(selection.node)
    : undefined;
  const related = relatedStorageNodes(selected?.id, topology.edges);
  const relatedNodes = topology.nodes.filter(
    (node) => node.id !== selected?.id && related.has(node.id),
  );
  const ratesFor = (node: StorageNode) =>
    fresh && node.kind === "drive" && node.block
      ? live.data?.disks[node.block.kernelName]
      : undefined;
  const containerSample = live.data?.containers;
  const containerMetrics = new Map(
    (fresh &&
    containerSample &&
    live.data.captured_at_ms - containerSample.captured_at_ms < 15000
      ? containerSample.items
      : []
    ).map((item) => [item.id, item]),
  );
  const smartFor = (node: StorageNode) =>
    fresh && node.block
      ? live.data?.smart[node.block.kernelName]?.data
      : undefined;
  const select = (node: StorageNode) => onSelectionChange({ node: node.id });
  const selectedSmart = selected ? smartFor(selected) : undefined;
  const selectedBlocks =
    selected?.kind === "drive"
      ? [
          ...new Map(
            [
              ...selected.backing,
              ...relatedNodes.flatMap((node) => node.backing),
            ].map((block) => [block.path, block]),
          ).values(),
        ]
      : (selected?.backing ?? []);

  return (
    <div className="storage-topology-page" data-paused={paused}>
      <header className="storage-topology__toolbar">
        <div>
          <AppTypography component="h1" variant="h5" fontWeight={600}>
            Storage topology
          </AppTypography>
          <AppTypography color="text.secondary" variant="body2">
            Follow your data from disk to application.
          </AppTypography>
        </div>
        <div className="storage-topology__tools">
          <Chip
            size="small"
            variant="soft"
            label={fresh ? "Live activity" : "Metrics unavailable"}
          />
          <AppButton
            size="small"
            variant="outlined"
            color="inherit"
            aria-pressed={paused}
            onClick={() => setPaused(!paused)}
          >
            {paused ? "Resume animation" : "Pause animation"}
          </AppButton>
        </div>
      </header>
      <div className="storage-topology__workspace">
        <AppPaper className="storage-topology__map">
          <div className="storage-topology__map-header">
            <AppTypography
              component="span"
              variant="body2"
              className="storage-topology__counts"
            >
              <span>
                <strong>{topology.columns[0].length}</strong> devices
              </span>
              <span>
                <strong>{topology.columns[1].length}</strong> volumes & mounts
              </span>
              <span>
                <strong>{topology.columns[2].length}</strong> applications
              </span>
            </AppTypography>
            {selection.node && (
              <AppButton size="small" onClick={() => onSelectionChange({})}>
                Clear selection
              </AppButton>
            )}
          </div>
          {topology.nodes.length === 0 ? (
            <div className="storage-topology__empty">
              <Icon aria-hidden="true" icon="mdi:harddisk" width={40} />
              <AppTypography component="h2" variant="h6">
                No storage devices found
              </AppTypography>
              <AppTypography color="text.secondary" variant="body2">
                Devices and mount points will appear here when available.
              </AppTypography>
            </div>
          ) : (
            <div
              className="storage-topology__canvas"
              style={
                { "--storage-height": `${topology.height}px` } as CSSProperties
              }
            >
              <svg
                aria-hidden="true"
                className="storage-topology__edges"
                viewBox={`0 0 1000 ${topology.height}`}
                preserveAspectRatio="none"
              >
                {topology.edges.map((edge) => {
                  const source = topology.inventory.get(edge.from)!;
                  const target = topology.inventory.get(edge.to)!;
                  const disk = ratesFor(source);
                  const container = target.container;
                  const activity =
                    container?.State === "running"
                      ? (containerMetrics.get(container.Id) ??
                        containerMetrics.get(container.Id.slice(0, 12)))
                      : undefined;
                  const writable =
                    !source.mount?.readOnly &&
                    !target.mount?.readOnly &&
                    (!container ||
                      target.uses?.some(
                        (use) => use.mountId === edge.from && !use.readOnly,
                      ));
                  return (
                    <AppTopologyEdge
                      className="storage-topology__edge"
                      key={`${edge.from}:${edge.to}`}
                      d={edge.path}
                      highlighted={Boolean(
                        selected &&
                        related.has(edge.from) &&
                        related.has(edge.to),
                      )}
                      forward={
                        disk?.read_bytes_per_sec ??
                        activity?.block_read_bytes_per_sec
                      }
                      reverse={
                        writable
                          ? (disk?.write_bytes_per_sec ??
                            activity?.block_write_bytes_per_sec)
                          : undefined
                      }
                      paused={paused}
                    />
                  );
                })}
              </svg>
              {topology.columns.map((column, index) => (
                <section
                  key={labels[index]}
                  className="storage-topology__column"
                  aria-label={labels[index]}
                  style={
                    { "--storage-left": `${3 + index * 35}%` } as CSSProperties
                  }
                >
                  <AppTypography
                    component="h2"
                    variant="overline"
                    className="storage-topology__column-label"
                  >
                    {labels[index]}
                  </AppTypography>
                  {column.length === 0 && (
                    <AppTypography
                      className="storage-topology__column-empty"
                      color="text.secondary"
                      variant="body2"
                    >
                      {index === 2
                        ? containers.isLoading || vms.isLoading
                          ? "Loading application storage…"
                          : "No application storage paths reported."
                        : "No volumes or mounts reported."}
                    </AppTypography>
                  )}
                  <ul className="storage-topology__nodes">
                    {column.map((node) => (
                      <li
                        key={node.id}
                        style={
                          { "--storage-top": `${node.top}px` } as CSSProperties
                        }
                      >
                        <AppButton
                          className="storage-topology__node"
                          color="inherit"
                          onClick={() => select(node)}
                          aria-label={`Inspect ${kinds[node.kind].toLowerCase()} ${node.label}`}
                          aria-pressed={selected?.id === node.id}
                          data-related={!selected || related.has(node.id)}
                        >
                          <span className="storage-topology__identity">
                            <span className="storage-topology__icon">
                              <NodeIcon node={node} />
                            </span>
                            <span className="storage-topology__copy">
                              <AppTypography
                                component="span"
                                variant="body2"
                                fontWeight={600}
                                noWrap
                                title={node.label}
                              >
                                {node.label}
                              </AppTypography>
                              <AppTypography
                                component="span"
                                color="text.secondary"
                                variant="caption"
                                noWrap
                                title={node.description}
                              >
                                {node.description}
                              </AppTypography>
                            </span>
                          </span>
                          {node.kind === "drive" ? (
                            <>
                              <AppTypography
                                component="span"
                                color="text.secondary"
                                variant="caption"
                                className="storage-topology__drive-meta"
                              >
                                <span>
                                  {formatFileSize(node.block!.sizeBytes)}
                                </span>
                                <span>
                                  {smartFor(node)?.smart_status ??
                                    node.block?.transport.toUpperCase()}
                                </span>
                              </AppTypography>
                              <DiskActivity rates={ratesFor(node)} />
                            </>
                          ) : node.column === 1 ? (
                            <Usage node={node} fresh={fresh} />
                          ) : (
                            <AppTypography
                              component="span"
                              color="text.secondary"
                              variant="caption"
                            >
                              {kinds[node.kind]} · {node.uses?.length} storage{" "}
                              {node.uses?.length === 1 ? "path" : "paths"}
                            </AppTypography>
                          )}
                        </AppButton>
                      </li>
                    ))}
                  </ul>
                </section>
              ))}
            </div>
          )}
          <div className="storage-topology__legend">
            <AppTypography variant="caption" color="text.secondary">
              Lines show backing devices and application host paths. Grains show
              device or container read/write totals, not I/O attributed to each
              path.
            </AppTypography>
          </div>
        </AppPaper>
        <AppPaper
          role="complementary"
          className="storage-topology__inspector"
          aria-label="Storage details"
        >
          <AppTypography variant="overline" color="text.secondary">
            {selected ? kinds[selected.kind] : "Storage overview"}
          </AppTypography>
          <AppTypography
            ref={heading}
            component="h2"
            variant="h6"
            fontWeight={600}
            tabIndex={-1}
            className="storage-topology__detail-title"
          >
            {selected?.label ??
              (selection.node
                ? "Item no longer available"
                : "Your storage, connected")}
          </AppTypography>
          {!selected ? (
            <>
              <AppTypography color="text.secondary" variant="body2">
                {selection.node
                  ? "This item may have been removed or unmounted. Select another item to continue."
                  : "Select a device, mount point, or application to trace its storage connections."}
              </AppTypography>
              <div className="storage-topology__summary">
                <Icon aria-hidden="true" icon="mdi:harddisk" width={36} />
                <AppTypography component="p" variant="h5">
                  {formatFileSize(
                    topology.columns[0].reduce(
                      (total, node) => total + (node.block?.sizeBytes ?? 0),
                      0,
                    ),
                  )}
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary">
                  Reported device capacity
                </AppTypography>
              </div>
            </>
          ) : (
            <>
              <AppTypography color="text.secondary" variant="body2">
                {selected.description}
              </AppTypography>
              {selected.column === 1 && <Usage node={selected} fresh={fresh} />}
              {selected.kind === "drive" && (
                <DiskActivity rates={ratesFor(selected)} />
              )}
              <dl className="storage-topology__facts">
                {selected.block && (
                  <>
                    <dt>Device</dt>
                    <dd className="storage-topology__path">
                      {selected.block.path}
                    </dd>
                    <dt>Size</dt>
                    <dd>{formatFileSize(selected.block.sizeBytes)}</dd>
                    {!selected.mount && (
                      <>
                        <dt>Access</dt>
                        <dd>
                          {selected.block.readOnly ? "Read only" : "Read/write"}
                        </dd>
                      </>
                    )}
                  </>
                )}
                {selected.mount && (
                  <>
                    {selected.mount.device !== selected.block?.path && (
                      <>
                        <dt>Filesystem source</dt>
                        <dd className="storage-topology__path">
                          {selected.mount.device}
                        </dd>
                      </>
                    )}
                    <dt>Mount access</dt>
                    <dd>
                      {selected.mount.readOnly ? "Read only" : "Read/write"}
                    </dd>
                  </>
                )}
                {selected.group && (
                  <>
                    <dt>Volume group</dt>
                    <dd>{selected.group.name}</dd>
                    <dt>Group free</dt>
                    <dd>{formatFileSize(selected.group.free)}</dd>
                  </>
                )}
                {selected.block?.serial && (
                  <>
                    <dt>Serial</dt>
                    <dd>{selected.block.serial}</dd>
                  </>
                )}
                {selected.kind === "drive" && (
                  <>
                    <dt>SMART health</dt>
                    <dd>{selectedSmart?.smart_status ?? "Unavailable"}</dd>
                  </>
                )}
                {selectedSmart?.temperature_celsius !== undefined && (
                  <>
                    <dt>Temperature</dt>
                    <dd>{selectedSmart.temperature_celsius} °C</dd>
                  </>
                )}
              </dl>
              {selectedBlocks.length > 0 && (
                <div className="storage-topology__detail-section">
                  <AppTypography component="h3" variant="subtitle2">
                    {selected.kind === "drive"
                      ? "Block devices"
                      : "Backing devices"}
                  </AppTypography>
                  <ul className="storage-topology__backing">
                    {selectedBlocks.map((block) => (
                      <li key={block.path}>
                        <AppTypography component="span" variant="body2">
                          {block.path}
                        </AppTypography>
                        <AppTypography
                          component="span"
                          variant="caption"
                          color="text.secondary"
                        >
                          {block.type} · {formatFileSize(block.sizeBytes)}
                          {block.parents.length
                            ? ` · from ${block.parents.join(", ")}`
                            : ""}
                        </AppTypography>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {selected.uses && (
                <div className="storage-topology__detail-section">
                  <AppTypography component="h3" variant="subtitle2">
                    Storage paths
                  </AppTypography>
                  <ul className="storage-topology__backing">
                    {selected.uses.map((use, index) => (
                      <li key={`${use.source}:${index}`}>
                        <AppTypography component="span" variant="body2">
                          {use.source}
                        </AppTypography>
                        <AppTypography
                          component="span"
                          variant="caption"
                          color="text.secondary"
                        >
                          → {use.destination}
                          {use.readOnly === undefined
                            ? ""
                            : use.readOnly
                              ? " · Read only"
                              : " · Read/write"}
                        </AppTypography>
                        {!use.mountId && (
                          <AppTypography
                            component="span"
                            variant="caption"
                            color="text.secondary"
                          >
                            No matching mount reported
                          </AppTypography>
                        )}
                      </li>
                    ))}
                  </ul>
                  <AppTypography variant="caption" color="text.secondary">
                    Matched by host path. Symlinks and filesystems inside a
                    guest are not resolved.
                  </AppTypography>
                </div>
              )}
              <div className="storage-topology__detail-section">
                <AppTypography component="h3" variant="subtitle2">
                  Connected items
                </AppTypography>
                {relatedNodes.length ? (
                  <ul className="storage-topology__connections">
                    {relatedNodes.map((node) => (
                      <li key={node.id}>
                        <AppButton color="inherit" onClick={() => select(node)}>
                          <NodeIcon node={node} />
                          <span>{node.label}</span>
                        </AppButton>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    No connections reported.
                  </AppTypography>
                )}
              </div>
              <div className="storage-topology__detail-section">
                {selected.kind === "drive" &&
                  selected.block?.type === "disk" && (
                    <AppRouterLinkButton
                      to="/storage"
                      search={{ drive: selected.block.kernelName }}
                      variant="outlined"
                      fullWidth
                    >
                      Open disk details
                    </AppRouterLinkButton>
                  )}
                {selected.mountpoint && (
                  <AppRouterLinkButton
                    to="/storage"
                    search={{ fs: selected.mountpoint }}
                    variant="outlined"
                    fullWidth
                  >
                    Open filesystem details
                  </AppRouterLinkButton>
                )}
                {selected.group && (
                  <AppRouterLinkButton
                    to="/storage/lvm"
                    variant="outlined"
                    fullWidth
                  >
                    Open LVM
                  </AppRouterLinkButton>
                )}
                {selected.container && (
                  <AppRouterLinkButton
                    to="/docker/containers"
                    search={{ container: selected.container.Id }}
                    variant="outlined"
                    fullWidth
                  >
                    Open container details
                  </AppRouterLinkButton>
                )}
                {selected.vm && (
                  <AppRouterLinkButton
                    to="/vm/machines/$name"
                    params={{ name: selected.vm.name }}
                    variant="outlined"
                    fullWidth
                  >
                    Open virtual machine details
                  </AppRouterLinkButton>
                )}
              </div>
            </>
          )}
          {(inventoryError ||
            groups.isError ||
            containers.isError ||
            vms.isError) && (
            <AppAlert severity="info">
              Some inventory could not be refreshed. Connections may be
              incomplete.
            </AppAlert>
          )}
          {capabilities.dockerAvailable !== true && (
            <AppTypography variant="caption" color="text.secondary">
              Docker inventory unavailable.
            </AppTypography>
          )}
          {capabilities.libvirtAvailable !== true && (
            <AppTypography variant="caption" color="text.secondary">
              Virtual machine inventory unavailable.
            </AppTypography>
          )}
        </AppPaper>
      </div>
    </div>
  );
}
