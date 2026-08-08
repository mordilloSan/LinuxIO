import { Icon } from "@iconify/react";
import { useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useState } from "react";

import { linuxio } from "@/api";
import DockerResourceListCard from "@/components/cards/DockerResourceListCard";
import DockerSectionCard from "@/components/cards/DockerSectionCard";
import DockerStatCard from "@/components/cards/DockerStatCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppGrid from "@/components/ui/AppGrid";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import SectionHeader from "@/components/ui/SectionHeader";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

// ─── small helpers ────────────────────────────────────────────────────────────
const StateChip = ({
  state,
  status,
  stopping = false,
}: {
  state: string;
  status: string;
  stopping?: boolean;
}) => {
  if (stopping) {
    return (
      <Chip
        color="warning"
        label={
          <span
            style={{
              alignItems: "center",
              display: "inline-flex",
              gap: 4,
            }}
          >
            <AppCircularProgress color="inherit" size={12} />
            Stopping
          </span>
        }
        size="small"
        variant="soft"
      />
    );
  }
  if (status.toLowerCase().includes("unhealthy"))
    return (
      <Chip color="warning" label="Unhealthy" size="small" variant="soft" />
    );
  if (status.toLowerCase().includes("healthy"))
    return <Chip color="success" label="Healthy" size="small" variant="soft" />;
  if (state === "running")
    return <Chip color="success" label="Running" size="small" variant="soft" />;
  if (state === "exited" || state === "dead")
    return <Chip color="error" label="Stopped" size="small" variant="soft" />;
  return <Chip label={state} size="small" variant="soft" />;
};

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const RESOURCE_TABLE_MAX_HEIGHT = 201;
const RESOURCE_TABLE_STYLE = { boxShadow: "none" } as const;
const EMPTY_STOPPING_CONTAINER_IDS = new Set<string>();
const dockerRouteApi = getRouteApi("/_authenticated/docker/");

const getDockerResourceId = (resource: { Id: string }) => resource.Id;

const getContainerDisplayName = (names?: string[]) =>
  names?.[0]?.replace(/^\//, "") || "Unnamed";

const getImageTagParts = (repoTags?: string[]) => {
  const fullTag = repoTags?.[0] ?? "<none>:<none>";
  const colonIdx = fullTag.lastIndexOf(":");
  return {
    repo: colonIdx >= 0 ? fullTag.slice(0, colonIdx) : fullTag,
    tag: colonIdx >= 0 ? fullTag.slice(colonIdx + 1) : "",
  };
};

const stripStatusDetail = (status: string) =>
  status.replace(/\s*\(.*?\)\s*$/, "");

// ─── main component ───────────────────────────────────────────────────────────

interface DockerDashboardProps {
  stoppingContainerIds?: ReadonlySet<string>;
}

const DockerDashboard = ({
  stoppingContainerIds = EMPTY_STOPPING_CONTAINER_IDS,
}: DockerDashboardProps) => {
  const theme = useAppTheme();
  const navigate = dockerRouteApi.useNavigate();
  const [
    { data: rawContainers },
    { data: rawImages },
    { data: rawNetworks },
    { data: rawVolumes },
    { data: dockerInfo },
  ] = useSuspenseQueries({
    queries: [
      linuxio.docker.list_containers.queryOptions({
        refetchInterval: 5000,
      }),
      linuxio.docker.list_images.queryOptions({
        refetchInterval: 30000,
      }),
      linuxio.docker.list_networks.queryOptions({
        refetchInterval: 30000,
      }),
      linuxio.docker.list_volumes.queryOptions({
        refetchInterval: 30000,
      }),
      linuxio.docker.get_docker_info.queryOptions({
        refetchInterval: 60000,
      }),
    ],
  });
  const containers = rawContainers;
  const images = rawImages;
  const networks = rawNetworks;
  const volumes = rawVolumes;
  const navigateToTab = (
    to:
      | "/docker/containers"
      | "/docker/images"
      | "/docker/networks"
      | "/docker/volumes",
  ) => {
    navigate({ to });
  };
  const [dockerDashboardSections, setDockerDashboardSections] = useConfigValue(
    "dockerDashboardSections",
  );
  const sections = dockerDashboardSections ?? {
    overview: true,
    daemon: true,
    resources: true,
  };
  const setSection = useCallback(
    (key: "overview" | "daemon" | "resources") =>
      setDockerDashboardSections((prev) => {
        const cur = prev ?? {
          overview: true,
          daemon: true,
          resources: true,
        };
        return {
          ...cur,
          [key]: !cur[key],
        };
      }),
    [setDockerDashboardSections],
  );
  const runningContainers = useMemo(
    () => containers.filter((c) => c.State === "running"),
    [containers],
  );
  const stoppedContainers = useMemo(
    () => containers.filter((c) => c.State === "exited" || c.State === "dead"),
    [containers],
  );
  const unhealthyContainers = useMemo(
    () =>
      containers.filter((c) => c.Status.toLowerCase().includes("unhealthy")),
    [containers],
  );
  const healthyContainers = useMemo(
    () =>
      containers.filter(
        (c) =>
          c.Status.toLowerCase().includes("healthy") &&
          !c.Status.toLowerCase().includes("unhealthy"),
      ),
    [containers],
  );
  const totalCpu = useMemo(
    () =>
      runningContainers.reduce(
        (sum, c) => sum + (c.metrics?.cpu_percent ?? 0),
        0,
      ),
    [runningContainers],
  );
  const totalMemUsage = useMemo(
    () =>
      runningContainers.reduce(
        (sum, c) => sum + (c.metrics?.mem_usage ?? 0),
        0,
      ),
    [runningContainers],
  );
  // Use system total RAM as the denominator. Per-container mem_limit equals
  // the host's total RAM when no limit is set, so summing them multiplies it
  // by the container count and produces a wildly inflated number.
  const systemMemTotal = dockerInfo?.mem_total ?? 0;
  const totalMemPercent =
    systemMemTotal > 0
      ? Math.min((totalMemUsage / systemMemTotal) * 100, 100)
      : 0;
  const totalImageSize = useMemo(
    () => images.reduce((sum, img) => sum + img.Size, 0),
    [images],
  );
  const [containerSort, setContainerSort] = useState<
    "recent" | "name" | "state"
  >("recent");
  const previewContainers = useMemo(() => {
    const list = [...containers];
    if (containerSort === "recent")
      return list.sort((a, b) => b.Created - a.Created);
    if (containerSort === "name")
      return list.sort((a, b) =>
        (a.Names?.[0] ?? "").localeCompare(b.Names?.[0] ?? ""),
      );
    if (containerSort === "state")
      return list.sort(
        (a, b) =>
          (a.State === "running" ? -1 : 1) - (b.State === "running" ? -1 : 1),
      );
    return list;
  }, [containers, containerSort]);
  const [imageSort, setImageSort] = useState<
    "largest" | "recent" | "name" | "usage"
  >("largest");
  const previewImages = useMemo(() => {
    const list = [...images];
    if (imageSort === "largest") return list.sort((a, b) => b.Size - a.Size);
    if (imageSort === "recent")
      return list.sort((a, b) => b.Created - a.Created);
    if (imageSort === "name")
      return list.sort((a, b) =>
        (a.RepoTags?.[0] ?? "").localeCompare(b.RepoTags?.[0] ?? ""),
      );
    if (imageSort === "usage")
      return list.sort((a, b) => (b.Containers ?? 0) - (a.Containers ?? 0));
    return list;
  }, [images, imageSort]);

  const containerColumns = useMemo<
    AppDataTableColumnDef<(typeof previewContainers)[number]>[]
  >(
    () => [
      {
        id: "name",
        header: "NAME",
        cell: ({ row }) => {
          const container = row.original;
          const name = getContainerDisplayName(container.Names);
          return (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 4,
                minWidth: 0,
              }}
            >
              <DockerIcon alt={name} identifier={container.icon} size={22} />
              <AppTypography
                fontWeight={500}
                noWrap
                title={name}
                toastMeta={DOCKER_TOAST_META}
                variant="body2"
              >
                {name}
              </AppTypography>
            </div>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = row as (typeof previewContainers)[number];
            return [container.Id, container.Names?.[0], container.icon];
          },
        },
      },
      {
        accessorKey: "Image",
        header: "IMAGE",
        cell: ({ row }) => (
          <AppTypography
            color="text.secondary"
            noWrap
            title={row.original.Image}
            toastMeta={DOCKER_TOAST_META}
            variant="caption"
          >
            {row.original.Image}
          </AppTypography>
        ),
        meta: {
          getCellRenderKey: (row) => {
            const container = row as (typeof previewContainers)[number];
            return [container.Id, container.Image];
          },
          hideBelow: "md",
          width: "220px",
        },
      },
      {
        id: "state",
        header: "STATE",
        cell: ({ row }) => (
          <StateChip
            state={row.original.State}
            status={row.original.Status}
            stopping={stoppingContainerIds.has(row.original.Id)}
          />
        ),
        meta: {
          getCellRenderKey: (row) => {
            const container = row as (typeof previewContainers)[number];
            return [
              container.Id,
              container.State,
              container.Status,
              stoppingContainerIds.has(container.Id),
            ];
          },
          width: "96px",
        },
      },
      {
        id: "status",
        header: "STATUS",
        cell: ({ row }) => {
          const status = stripStatusDetail(row.original.Status);
          return (
            <AppTypography
              color="text.secondary"
              copyText={row.original.Status}
              noWrap
              title={row.original.Status}
              toastMeta={DOCKER_TOAST_META}
              variant="caption"
            >
              {status}
            </AppTypography>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = row as (typeof previewContainers)[number];
            return [container.Id, container.Status];
          },
          hideBelow: "md",
          width: "100px",
        },
      },
    ],
    [stoppingContainerIds],
  );
  const imageColumns = useMemo<
    AppDataTableColumnDef<(typeof previewImages)[number]>[]
  >(
    () => [
      {
        id: "repository",
        header: "REPOSITORY",
        accessorFn: (image) => getImageTagParts(image.RepoTags).repo,
        cell: ({ row }) => {
          const { repo } = getImageTagParts(row.original.RepoTags);
          return (
            <AppTypography
              fontWeight={500}
              noWrap
              title={repo}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {repo}
            </AppTypography>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const image = row as (typeof previewImages)[number];
            return [image.Id, image.RepoTags?.join("|")];
          },
        },
      },
      {
        id: "tag",
        header: "TAG",
        accessorFn: (image) => getImageTagParts(image.RepoTags).tag,
        cell: ({ row }) => {
          const { tag } = getImageTagParts(row.original.RepoTags);
          return (
            <AppTypography
              color="text.secondary"
              noWrap
              title={tag}
              toastMeta={DOCKER_TOAST_META}
              variant="caption"
            >
              {tag}
            </AppTypography>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const image = row as (typeof previewImages)[number];
            return [image.Id, image.RepoTags?.join("|")];
          },
          hideBelow: "md",
          width: "90px",
        },
      },
      {
        id: "status",
        header: "STATUS",
        cell: ({ row }) =>
          (row.original.Containers ?? 0) > 0 ? (
            <Chip color="success" label="In Use" size="small" variant="soft" />
          ) : null,
        meta: {
          getCellRenderKey: (row) => {
            const image = row as (typeof previewImages)[number];
            return [image.Id, image.Containers ?? 0];
          },
          width: "96px",
        },
      },
      {
        id: "size",
        header: "SIZE",
        accessorFn: (image) => image.Size,
        cell: ({ row }) => (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {formatFileSize(row.original.Size)}
          </AppTypography>
        ),
        meta: {
          getCellRenderKey: (row) => {
            const image = row as (typeof previewImages)[number];
            return [image.Id, image.Size];
          },
          hideBelow: "md",
          width: "90px",
        },
      },
    ],
    [],
  );
  return (
    <div>
      {/* ── Stat Cards ─────────────────────────────────────────────────────── */}
      <SectionHeader
        controlsId="docker-overview-panel"
        expanded={sections.overview}
        onToggle={() => setSection("overview")}
        title="Overview"
      />
      <div id="docker-overview-panel">
        <AppCollapse in={sections.overview}>
          <AppGrid container spacing={2} style={{ marginBottom: 8 }}>
            {(
              [
                {
                  label: "Containers",
                  to: "/docker/containers",
                  value: `${containers.length}`,
                  detail: [
                    `${runningContainers.length} running`,
                    stoppedContainers.length > 0
                      ? `${stoppedContainers.length} stopped`
                      : null,
                    unhealthyContainers.length > 0
                      ? `${unhealthyContainers.length} unhealthy`
                      : healthyContainers.length > 0
                        ? `${healthyContainers.length} healthy`
                        : null,
                  ]
                    .filter(Boolean)
                    .join(" · "),
                },
                {
                  label: "Images",
                  to: "/docker/images",
                  value: `${images.length}`,
                  detail: `${formatFileSize(totalImageSize)} on disk`,
                },
                {
                  label: "Networks",
                  to: "/docker/networks",
                  value: `${networks.length}`,
                  detail: `${networks.filter((n) => !n.Internal).length} external`,
                },
                {
                  label: "Volumes",
                  to: "/docker/volumes",
                  value: `${volumes.length}`,
                  detail: `${volumes.filter((v) => v.Driver === "local").length} local`,
                },
              ] as {
                label: string;
                to:
                  | "/docker/containers"
                  | "/docker/images"
                  | "/docker/networks"
                  | "/docker/volumes";
                value: string;
                detail: string;
              }[]
            ).map(({ label, to, value, detail }) => (
              <AppGrid
                key={label}
                size={{
                  xs: 6,
                  md: 3,
                }}
              >
                <DockerStatCard
                  detail={detail}
                  label={label}
                  onClick={() => navigateToTab(to)}
                  value={value}
                />
              </AppGrid>
            ))}
          </AppGrid>
        </AppCollapse>
      </div>

      <SectionHeader
        controlsId="docker-daemon-panel"
        expanded={sections.daemon}
        onToggle={() => setSection("daemon")}
        title="Docker Daemon"
      />
      {/* ── Docker Daemon ───────────────────────────────────────────────────── */}
      <div id="docker-daemon-panel">
        <AppCollapse in={sections.daemon}>
          <AppGrid container spacing={2} style={{ marginBottom: 8 }}>
            {dockerInfo && (
              <>
                {/* ── Resource Usage ────────────────────────────────────────────── */}
                {runningContainers.length > 0 && (
                  <>
                    <AppGrid
                      size={{
                        xs: 12,
                        sm: 4,
                      }}
                    >
                      <DockerSectionCard
                        icon={
                          <Icon
                            color={theme.palette.primary.main}
                            height={28}
                            icon="ph:cpu"
                            width={28}
                          />
                        }
                        subtitle="Processor utilization"
                        title="CPU"
                      >
                        <MetricBar
                          color={theme.palette.primary.main}
                          label="CPU"
                          percent={Math.min(totalCpu, 100)}
                          rightLabel={`${totalCpu.toFixed(1)}%`}
                          tooltip={`Total CPU across ${runningContainers.length} running containers`}
                        />
                      </DockerSectionCard>
                    </AppGrid>
                    <AppGrid
                      size={{
                        xs: 12,
                        sm: 4,
                      }}
                    >
                      <DockerSectionCard
                        icon={
                          <Icon
                            color={theme.palette.primary.main}
                            height={28}
                            icon="la:memory"
                            width={28}
                          />
                        }
                        subtitle="RAM utilization"
                        title="Memory"
                      >
                        <MetricBar
                          color={theme.palette.primary.main}
                          label="Memory"
                          percent={totalMemPercent}
                          rightLabel={formatFileSize(totalMemUsage)}
                          tooltip={`${formatFileSize(totalMemUsage)} / ${formatFileSize(systemMemTotal)}`}
                        />
                      </DockerSectionCard>
                    </AppGrid>
                    {dockerInfo.disk_total > 0 && (
                      <AppGrid
                        size={{
                          xs: 12,
                          sm: 4,
                        }}
                      >
                        <DockerSectionCard
                          icon={
                            <Icon
                              color={theme.palette.primary.main}
                              height={28}
                              icon="mdi:harddisk"
                              width={28}
                            />
                          }
                          subtitle="Storage utilization"
                          title="Disk Usage"
                        >
                          <MetricBar
                            color={theme.palette.primary.main}
                            label="Disk (Docker)"
                            percent={Math.min(
                              (dockerInfo.disk_used / dockerInfo.disk_total) *
                                100,
                              100,
                            )}
                            rightLabel={formatFileSize(dockerInfo.disk_used)}
                            tooltip={`Docker disk usage: ${formatFileSize(dockerInfo.disk_used)} / ${formatFileSize(dockerInfo.disk_total)}`}
                          />
                        </DockerSectionCard>
                      </AppGrid>
                    )}
                  </>
                )}
                <AppGrid
                  size={{
                    xs: 12,
                    sm: 4,
                  }}
                >
                  <DockerSectionCard
                    fullHeight
                    icon={
                      <Icon
                        color={theme.palette.primary.main}
                        height={28}
                        icon="mdi:tag"
                        width={28}
                      />
                    }
                    subtitle="Engine & runtime versions"
                    title="Version"
                  >
                    <InfoRow label="Server">
                      {dockerInfo.server_version || "—"}
                    </InfoRow>
                    <InfoRow label="API">
                      {dockerInfo.api_version || "—"}
                    </InfoRow>
                    <InfoRow label="Go">{dockerInfo.go_version || "—"}</InfoRow>
                    <InfoRow label="Git Commit">
                      {dockerInfo.git_commit || "—"}
                    </InfoRow>
                  </DockerSectionCard>
                </AppGrid>
                <AppGrid
                  size={{
                    xs: 12,
                    sm: 4,
                  }}
                >
                  <DockerSectionCard
                    fullHeight
                    icon={
                      <Icon
                        color={theme.palette.primary.main}
                        height={28}
                        icon="mdi:monitor"
                        width={28}
                      />
                    }
                    subtitle="Host machine information"
                    title="System"
                  >
                    <InfoRow label="Hostname">{dockerInfo.name || "—"}</InfoRow>
                    <InfoRow label="OS">
                      {dockerInfo.operating_system || "—"}
                    </InfoRow>
                    <InfoRow label="Architecture">
                      {dockerInfo.architecture || "—"}
                    </InfoRow>
                    <InfoRow label="Root Dir">
                      {dockerInfo.docker_root_dir || "—"}
                    </InfoRow>
                  </DockerSectionCard>
                </AppGrid>
                <AppGrid
                  size={{
                    xs: 12,
                    sm: 4,
                  }}
                >
                  <DockerSectionCard
                    fullHeight
                    icon={
                      <Icon
                        color={theme.palette.primary.main}
                        height={28}
                        icon="mdi:wrench"
                        width={28}
                      />
                    }
                    subtitle="Storage & runtime settings"
                    title="Configuration"
                  >
                    <InfoRow label="Storage Driver">
                      {dockerInfo.storage_driver || "—"}
                    </InfoRow>
                    <InfoRow label="Cgroup Driver">
                      {dockerInfo.cgroup_driver || "—"}
                    </InfoRow>
                    <InfoRow label="Cgroup Version">
                      {dockerInfo.cgroup_version || "—"}
                    </InfoRow>
                    <InfoRow label="Default Runtime">
                      {dockerInfo.default_runtime || "—"}
                    </InfoRow>
                  </DockerSectionCard>
                </AppGrid>
              </>
            )}
          </AppGrid>
        </AppCollapse>
      </div>

      {/* ── Resources ──────────────────────────────────────────────────────── */}
      <SectionHeader
        controlsId="docker-resources-panel"
        expanded={sections.resources}
        onToggle={() => setSection("resources")}
        title="Resources"
      />
      <div id="docker-resources-panel">
        <AppCollapse in={sections.resources}>
          <AppGrid container spacing={2}>
            {/* Containers table */}
            <AppGrid
              size={{
                xs: 12,
                lg: 6,
              }}
            >
              <DockerResourceListCard
                footerText={`${containers.length} containers`}
                icon={
                  <Icon
                    color={theme.palette.primary.main}
                    height={28}
                    icon="mdi:cube-outline"
                    width={28}
                  />
                }
                onViewAll={() => navigateToTab("/docker/containers")}
                subtitle={
                  <AppSelect
                    disableUnderline
                    onChange={(e) =>
                      setContainerSort(e.target.value as typeof containerSort)
                    }
                    style={{
                      fontSize: "0.75rem",
                      color: "var(--app-palette-text-secondary)",
                      lineHeight: 1.4,
                    }}
                    value={containerSort}
                    variant="standard"
                  >
                    <option value="recent">Recent containers</option>
                    <option value="name">Sort by name</option>
                    <option value="state">Sort by state</option>
                  </AppSelect>
                }
                title="Containers"
              >
                <AppDataTable
                  ariaLabel="Docker dashboard containers"
                  columns={containerColumns}
                  data={previewContainers}
                  emptyMessage="No containers found"
                  fillAvailable={false}
                  getRowId={getDockerResourceId}
                  maxHeight={RESOURCE_TABLE_MAX_HEIGHT}
                  style={RESOURCE_TABLE_STYLE}
                />
              </DockerResourceListCard>
            </AppGrid>

            {/* Images table */}
            <AppGrid
              size={{
                xs: 12,
                lg: 6,
              }}
            >
              <DockerResourceListCard
                footerText={`${images.length} images`}
                icon={
                  <Icon
                    color={theme.palette.primary.main}
                    height={28}
                    icon="mdi:layers"
                    width={28}
                  />
                }
                onViewAll={() => navigateToTab("/docker/images")}
                subtitle={
                  <AppSelect
                    disableUnderline
                    onChange={(e) =>
                      setImageSort(e.target.value as typeof imageSort)
                    }
                    style={{
                      fontSize: "0.75rem",
                      color: "var(--app-palette-text-secondary)",
                      lineHeight: 1.4,
                    }}
                    value={imageSort}
                    variant="standard"
                  >
                    <option value="largest">Largest images</option>
                    <option value="recent">Most recent</option>
                    <option value="name">Sort by name</option>
                    <option value="usage">Most used</option>
                  </AppSelect>
                }
                title="Images"
              >
                <AppDataTable
                  ariaLabel="Docker dashboard images"
                  columns={imageColumns}
                  data={previewImages}
                  emptyMessage="No images found"
                  fillAvailable={false}
                  getRowId={getDockerResourceId}
                  maxHeight={RESOURCE_TABLE_MAX_HEIGHT}
                  style={RESOURCE_TABLE_STYLE}
                />
              </DockerResourceListCard>
            </AppGrid>
          </AppGrid>
        </AppCollapse>
      </div>
    </div>
  );
};
export default DockerDashboard;
