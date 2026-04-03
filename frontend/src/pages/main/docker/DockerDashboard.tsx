import { Icon } from "@iconify/react";
import React, { useMemo, useState, useCallback } from "react";
import { useSearchParams } from "react-router-dom";

import "@/theme/section.css";
import "./docker-dashboard.css";
import { linuxio } from "@/api";
import DockerResourceListCard from "@/components/cards/DockerResourceListCard";
import DockerSectionCard from "@/components/cards/DockerSectionCard";
import DockerStatCard from "@/components/cards/DockerStatCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import InfoRow from "@/components/ui/InfoRow";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

// ─── small helpers ────────────────────────────────────────────────────────────
const StateChip: React.FC<{
  state: string;
  status: string;
}> = ({ state, status }) => {
  if (status.toLowerCase().includes("unhealthy"))
    return (
      <Chip size="small" label="Unhealthy" color="warning" variant="soft" />
    );
  if (status.toLowerCase().includes("healthy"))
    return <Chip size="small" label="Healthy" color="success" variant="soft" />;
  if (state === "running")
    return <Chip size="small" label="Running" color="success" variant="soft" />;
  if (state === "exited" || state === "dead")
    return <Chip size="small" label="Stopped" color="error" variant="soft" />;
  return <Chip size="small" label={state} variant="soft" />;
};
// ─── main component ───────────────────────────────────────────────────────────

const DockerDashboard: React.FC = () => {
  const theme = useAppTheme();
  const [, setSearchParams] = useSearchParams();
  const { data: containers = [] } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });
  const { data: images = [] } = linuxio.docker.list_images.useQuery({
    refetchInterval: 30000,
  });
  const { data: networks = [] } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 30000,
  });
  const { data: volumes = [] } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 30000,
  });
  const { data: dockerInfo } = linuxio.docker.get_docker_info.useQuery({
    refetchInterval: 60000,
  });
  const navigateToTab = (tab: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set("dockerTab", tab);
      return next;
    });
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
  const SCROLL_HEIGHT = 165;
  return (
    <div>
      {/* ── Stat Cards ─────────────────────────────────────────────────────── */}
      <div
        className="dd-section-header"
        onClick={() => setSection("overview")}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 6,
          cursor: "pointer",
          userSelect: "none",
        }}
      >
        <AppTypography variant="subtitle1" fontWeight={700}>
          Overview
        </AppTypography>
        <AppIconButton
          size="small"
          className="section-toggle"
          style={{
            opacity: 0,
            transition: "opacity 0.15s",
            pointerEvents: "none",
          }}
        >
          <Icon
            icon="mdi:chevron-down"
            width={24}
            height={24}
            style={{
              transition: "transform 0.2s",
              transform: sections.overview ? "rotate(0deg)" : "rotate(-90deg)",
            }}
          />
        </AppIconButton>
      </div>
      <AppCollapse in={sections.overview}>
        <AppGrid container spacing={2} style={{ marginBottom: 8 }}>
          {(
            [
              {
                label: "Containers",
                tab: "containers",
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
                tab: "images",
                value: `${images.length}`,
                detail: `${formatFileSize(totalImageSize)} on disk`,
              },
              {
                label: "Networks",
                tab: "networks",
                value: `${networks.length}`,
                detail: `${networks.filter((n) => !n.Internal).length} external`,
              },
              {
                label: "Volumes",
                tab: "volumes",
                value: `${volumes.length}`,
                detail: `${volumes.filter((v) => v.Driver === "local").length} local`,
              },
            ] as {
              label: string;
              tab: string;
              value: string;
              detail: string;
            }[]
          ).map(({ label, tab, value, detail }) => (
            <AppGrid
              key={label}
              size={{
                xs: 6,
                md: 3,
              }}
            >
              <DockerStatCard
                label={label}
                value={value}
                detail={detail}
                onClick={() => navigateToTab(tab)}
              />
            </AppGrid>
          ))}
        </AppGrid>
      </AppCollapse>

      <div
        className="dd-section-header"
        onClick={() => setSection("daemon")}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 6,
          cursor: "pointer",
          userSelect: "none",
        }}
      >
        <AppTypography variant="subtitle1" fontWeight={700}>
          Docker Daemon
        </AppTypography>
        <AppIconButton
          size="small"
          className="section-toggle"
          style={{
            opacity: 0,
            transition: "opacity 0.15s",
            pointerEvents: "none",
          }}
        >
          <Icon
            icon="mdi:chevron-down"
            width={24}
            height={24}
            style={{
              transition: "transform 0.2s",
              transform: sections.daemon ? "rotate(0deg)" : "rotate(-90deg)",
            }}
          />
        </AppIconButton>
      </div>
      {/* ── Docker Daemon ───────────────────────────────────────────────────── */}
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
                          icon="ph:cpu"
                          width={28}
                          height={28}
                          color={theme.palette.primary.main}
                        />
                      }
                      title="CPU"
                      subtitle="Processor utilization"
                    >
                      <MetricBar
                        label="CPU"
                        percent={Math.min(totalCpu, 100)}
                        color={theme.palette.primary.main}
                        tooltip={`Total CPU across ${runningContainers.length} running containers`}
                        rightLabel={`${totalCpu.toFixed(1)}%`}
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
                          icon="la:memory"
                          width={28}
                          height={28}
                          color={theme.palette.primary.main}
                        />
                      }
                      title="Memory"
                      subtitle="RAM utilization"
                    >
                      <MetricBar
                        label="Memory"
                        percent={totalMemPercent}
                        color={theme.palette.primary.main}
                        tooltip={`${formatFileSize(totalMemUsage)} / ${formatFileSize(systemMemTotal)}`}
                        rightLabel={formatFileSize(totalMemUsage)}
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
                            icon="mdi:harddisk"
                            width={28}
                            height={28}
                            color={theme.palette.primary.main}
                          />
                        }
                        title="Disk Usage"
                        subtitle="Storage utilization"
                      >
                        <MetricBar
                          label="Disk (Docker)"
                          percent={Math.min(
                            (dockerInfo.disk_used / dockerInfo.disk_total) *
                              100,
                            100,
                          )}
                          color={theme.palette.primary.main}
                          tooltip={`Docker disk usage: ${formatFileSize(dockerInfo.disk_used)} / ${formatFileSize(dockerInfo.disk_total)}`}
                          rightLabel={formatFileSize(dockerInfo.disk_used)}
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
                  icon={
                    <Icon
                      icon="mdi:tag"
                      width={28}
                      height={28}
                      color={theme.palette.primary.main}
                    />
                  }
                  title="Version"
                  subtitle="Engine & runtime versions"
                  fullHeight
                >
                  <InfoRow label="Server">
                    {dockerInfo.server_version || "—"}
                  </InfoRow>
                  <InfoRow label="API">{dockerInfo.api_version || "—"}</InfoRow>
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
                  icon={
                    <Icon
                      icon="mdi:monitor"
                      width={28}
                      height={28}
                      color={theme.palette.primary.main}
                    />
                  }
                  title="System"
                  subtitle="Host machine information"
                  fullHeight
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
                  icon={
                    <Icon
                      icon="mdi:wrench"
                      width={28}
                      height={28}
                      color={theme.palette.primary.main}
                    />
                  }
                  title="Configuration"
                  subtitle="Storage & runtime settings"
                  fullHeight
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

      {/* ── Resources ──────────────────────────────────────────────────────── */}
      <div
        className="dd-section-header"
        onClick={() => setSection("resources")}
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 6,
          cursor: "pointer",
          userSelect: "none",
        }}
      >
        <AppTypography variant="subtitle1" fontWeight={700}>
          Resources
        </AppTypography>
        <AppIconButton
          size="small"
          className="section-toggle"
          style={{
            opacity: 0,
            transition: "opacity 0.15s",
            pointerEvents: "none",
          }}
        >
          <Icon
            icon="mdi:chevron-down"
            width={24}
            height={24}
            style={{
              transition: "transform 0.2s",
              transform: sections.resources ? "rotate(0deg)" : "rotate(-90deg)",
            }}
          />
        </AppIconButton>
      </div>
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
              icon={
                <Icon
                  icon="mdi:cube-outline"
                  width={28}
                  height={28}
                  color={theme.palette.primary.main}
                />
              }
              title="Containers"
              subtitle={
                <AppSelect
                  variant="standard"
                  disableUnderline
                  value={containerSort}
                  onChange={(e) =>
                    setContainerSort(e.target.value as typeof containerSort)
                  }
                  style={{
                    fontSize: "0.75rem",
                    color: "var(--mui-palette-text-secondary)",
                    lineHeight: 1.4,
                  }}
                >
                  <option value="recent">Recent containers</option>
                  <option value="name">Sort by name</option>
                  <option value="state">Sort by state</option>
                </AppSelect>
              }
              onViewAll={() => navigateToTab("containers")}
              columnHeaders={[
                { label: "Name" },
                { label: "Image", hiddenXs: true },
                { label: "State" },
                { label: "Status", hiddenXs: true },
              ]}
              gridClassName="dd-containers-grid"
              isEmpty={previewContainers.length === 0}
              emptyText="No containers found"
              footerText={`${containers.length} containers`}
              scrollHeight={SCROLL_HEIGHT}
            >
              {previewContainers.map((container, i) => {
                const name =
                  container.Names?.[0]?.replace("/", "") || "Unnamed";
                return (
                  <React.Fragment key={container.Id}>
                    <div
                      className="dd-containers-grid"
                      style={{
                        alignItems: "center",
                        paddingInline: 8,
                        paddingBlock: 5,
                      }}
                    >
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 4,
                          minWidth: 0,
                        }}
                      >
                        <DockerIcon
                          identifier={container.icon}
                          size={22}
                          alt={name}
                        />
                        <AppTypography variant="body2" fontWeight={500} noWrap>
                          {name}
                        </AppTypography>
                      </div>
                      <AppTypography
                        variant="caption"
                        color="text.secondary"
                        noWrap
                        className="dd-hidden-xs"
                      >
                        {container.Image}
                      </AppTypography>
                      <div>
                        <StateChip
                          state={container.State}
                          status={container.Status}
                        />
                      </div>
                      <AppTypography
                        variant="caption"
                        color="text.secondary"
                        noWrap
                        className="dd-hidden-xs"
                      >
                        {container.Status.replace(/\s*\(.*?\)\s*$/, "")}
                      </AppTypography>
                    </div>
                    {i < previewContainers.length - 1 && <AppDivider />}
                  </React.Fragment>
                );
              })}
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
              icon={
                <Icon
                  icon="mdi:layers"
                  width={28}
                  height={28}
                  color={theme.palette.primary.main}
                />
              }
              title="Images"
              subtitle={
                <AppSelect
                  variant="standard"
                  disableUnderline
                  value={imageSort}
                  onChange={(e) =>
                    setImageSort(e.target.value as typeof imageSort)
                  }
                  style={{
                    fontSize: "0.75rem",
                    color: "var(--mui-palette-text-secondary)",
                    lineHeight: 1.4,
                  }}
                >
                  <option value="largest">Largest images</option>
                  <option value="recent">Most recent</option>
                  <option value="name">Sort by name</option>
                  <option value="usage">Most used</option>
                </AppSelect>
              }
              onViewAll={() => navigateToTab("images")}
              columnHeaders={[
                { label: "Repository" },
                { label: "Tag", hiddenXs: true },
                { label: "Status" },
                { label: "Size", hiddenXs: true },
              ]}
              gridClassName="dd-images-grid"
              isEmpty={previewImages.length === 0}
              emptyText="No images found"
              footerText={`${images.length} images`}
              scrollHeight={SCROLL_HEIGHT}
            >
              {previewImages.map((image, i) => {
                const fullTag = image.RepoTags?.[0] ?? "<none>:<none>";
                const colonIdx = fullTag.lastIndexOf(":");
                const repo =
                  colonIdx >= 0 ? fullTag.slice(0, colonIdx) : fullTag;
                const tag = colonIdx >= 0 ? fullTag.slice(colonIdx + 1) : "";
                const inUse = (image.Containers ?? 0) > 0;
                return (
                  <React.Fragment key={image.Id}>
                    <div
                      className="dd-images-grid"
                      style={{
                        alignItems: "center",
                        paddingInline: 8,
                        paddingBlock: 5,
                      }}
                    >
                      <AppTypography variant="body2" fontWeight={500} noWrap>
                        {repo}
                      </AppTypography>
                      <AppTypography
                        variant="caption"
                        color="text.secondary"
                        noWrap
                        className="dd-hidden-xs"
                      >
                        {tag}
                      </AppTypography>
                      <div>
                        {inUse && (
                          <Chip
                            size="small"
                            label="In Use"
                            color="success"
                            variant="soft"
                          />
                        )}
                      </div>
                      <AppTypography
                        variant="caption"
                        color="text.secondary"
                        noWrap
                        className="dd-hidden-xs"
                      >
                        {formatFileSize(image.Size)}
                      </AppTypography>
                    </div>
                    {i < previewImages.length - 1 && <AppDivider />}
                  </React.Fragment>
                );
              })}
            </DockerResourceListCard>
          </AppGrid>
        </AppGrid>
      </AppCollapse>
    </div>
  );
};
export default DockerDashboard;
