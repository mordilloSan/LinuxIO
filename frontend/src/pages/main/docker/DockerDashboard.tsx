import { Icon } from "@iconify/react";
import {
  Button,
  Chip,
  Collapse,
  Divider,
  Grid,
  IconButton,
  MenuItem,
  Select,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useMemo, useState, useCallback } from "react";
import { useSearchParams } from "react-router-dom";

import "@/theme/section.css";
import "./docker-dashboard.css";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import { useConfigValue } from "@/hooks/useConfig";
import { formatFileSize } from "@/utils/formaters";

// ─── small helpers ────────────────────────────────────────────────────────────

const stateChipSx = (color: string) => ({
  bgcolor: `${color}22`,
  color,
  borderColor: `${color}55`,
  border: "1px solid",
  fontWeight: 600,
});

const StateChip: React.FC<{ state: string; status: string }> = ({
  state,
  status,
}) => {
  const theme = useTheme();
  if (status.toLowerCase().includes("unhealthy"))
    return (
      <Chip
        size="small"
        label="Unhealthy"
        sx={stateChipSx(theme.palette.warning.main)}
      />
    );
  if (status.toLowerCase().includes("healthy"))
    return (
      <Chip
        size="small"
        label="Healthy"
        sx={stateChipSx(theme.palette.success.main)}
      />
    );
  if (state === "running")
    return (
      <Chip
        size="small"
        label="Running"
        sx={stateChipSx(theme.palette.success.main)}
      />
    );
  if (state === "exited" || state === "dead")
    return (
      <Chip
        size="small"
        label="Stopped"
        sx={stateChipSx(theme.palette.error.main)}
      />
    );
  return <Chip size="small" label={state} />;
};

const InfoRow: React.FC<{ label: string; value: React.ReactNode }> = ({
  label,
  value,
}) => {
  const theme = useTheme();
  return (
    <div
      className="dd-info-row"
      style={{ "--dd-divider": theme.palette.divider } as React.CSSProperties}
    >
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          fontSize: "0.62rem",
          flexShrink: 0,
        }}
      >
        {label}
      </Typography>
      <Typography
        variant="body2"
        fontWeight={500}
        noWrap
        sx={{ textAlign: "right", fontVariantNumeric: "tabular-nums" }}
      >
        {value || "—"}
      </Typography>
    </div>
  );
};

const DaemonSection: React.FC<{
  title: string;
  subtitle: string;
  icon: React.ReactNode;
  children: React.ReactNode;
}> = ({ title, subtitle, icon, children }) => (
  <div>
    <div
      style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 6 }}
    >
      <div
        style={{
          width: 40,
          height: 40,
          borderRadius: 8,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        {icon}
      </div>
      <div>
        <Typography variant="subtitle1" fontWeight={700} lineHeight={1.2}>
          {title}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {subtitle}
        </Typography>
      </div>
    </div>
    <div>{children}</div>
  </div>
);

const ResourceCardHeader: React.FC<{
  icon: React.ReactNode;
  title: string;
  subtitle: React.ReactNode;
  onViewAll: () => void;
}> = ({ icon, title, subtitle, onViewAll }) => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      padding: 8,
      paddingBottom: 6,
    }}
  >
    <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
      <div
        style={{
          width: 40,
          height: 40,
          borderRadius: 8,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        {icon}
      </div>
      <div>
        <Typography variant="subtitle1" fontWeight={700} lineHeight={1.2}>
          {title}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {subtitle}
        </Typography>
      </div>
    </div>
    <Button
      size="small"
      endIcon={<Icon icon="mdi:chevron-right" width={20} height={20} />}
      onClick={onViewAll}
      sx={{ flexShrink: 0 }}
    >
      View All
    </Button>
  </div>
);

// ─── main component ───────────────────────────────────────────────────────────

const DockerDashboard: React.FC = () => {
  const theme = useTheme();
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
        const cur = prev ?? { overview: true, daemon: true, resources: true };
        return { ...cur, [key]: !cur[key] };
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
        <Typography variant="subtitle1" fontWeight={700}>
          Overview
        </Typography>
        <IconButton
          size="small"
          className="section-toggle"
          component="span"
          sx={{
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
        </IconButton>
      </div>
      <Collapse in={sections.overview}>
        <Grid container spacing={2} sx={{ mb: 2 }}>
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
            ] as { label: string; tab: string; value: string; detail: string }[]
          ).map(({ label, tab, value, detail }) => (
            <Grid key={label} size={{ xs: 6, md: 3 }}>
              <FrostedCard
                onClick={() => navigateToTab(tab)}
                className="fc-opacity-hover"
                style={{
                  paddingInline: 10,
                  paddingBlock: 8,
                  cursor: "pointer",
                  transition: "opacity 0.15s",
                }}
              >
                <Typography
                  variant="overline"
                  color="text.secondary"
                  sx={{ lineHeight: 1.6 }}
                >
                  {label}
                </Typography>
                <div
                  style={{
                    display: "flex",
                    alignItems: "baseline",
                    justifyContent: "space-between",
                    marginTop: 1,
                  }}
                >
                  <Typography
                    variant="h6"
                    fontWeight={700}
                    sx={{ lineHeight: 1.2 }}
                  >
                    {value}
                  </Typography>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    noWrap
                    sx={{ textAlign: "right" }}
                  >
                    {detail}
                  </Typography>
                </div>
              </FrostedCard>
            </Grid>
          ))}
        </Grid>
      </Collapse>

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
        <Typography variant="subtitle1" fontWeight={700}>
          Docker Daemon
        </Typography>
        <IconButton
          size="small"
          className="section-toggle"
          component="span"
          sx={{
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
        </IconButton>
      </div>
      {/* ── Docker Daemon ───────────────────────────────────────────────────── */}
      <Collapse in={sections.daemon}>
        <Grid container spacing={2} sx={{ mb: 2 }}>
          {dockerInfo && (
            <>
              {/* ── Resource Usage ────────────────────────────────────────────── */}
              {runningContainers.length > 0 && (
                <>
                  <Grid size={{ xs: 12, sm: 4 }}>
                    <FrostedCard style={{ padding: 8 }}>
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 6,
                          marginBottom: 6,
                        }}
                      >
                        <div
                          style={{
                            width: 40,
                            height: 40,
                            borderRadius: 8,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            flexShrink: 0,
                          }}
                        >
                          <Icon
                            icon="ph:cpu"
                            width={28}
                            height={28}
                            color={theme.palette.primary.main}
                          />
                        </div>
                        <div>
                          <Typography
                            variant="subtitle1"
                            fontWeight={700}
                            lineHeight={1.2}
                          >
                            CPU
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            Processor utilization
                          </Typography>
                        </div>
                      </div>
                      <MetricBar
                        label="CPU"
                        percent={Math.min(totalCpu, 100)}
                        color={theme.palette.primary.main}
                        tooltip={`Total CPU across ${runningContainers.length} running containers`}
                        rightLabel={`${totalCpu.toFixed(1)}%`}
                      />
                    </FrostedCard>
                  </Grid>
                  <Grid size={{ xs: 12, sm: 4 }}>
                    <FrostedCard style={{ padding: 8 }}>
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 6,
                          marginBottom: 6,
                        }}
                      >
                        <div
                          style={{
                            width: 40,
                            height: 40,
                            borderRadius: 8,
                            display: "flex",
                            alignItems: "center",
                            justifyContent: "center",
                            flexShrink: 0,
                          }}
                        >
                          <Icon
                            icon="la:memory"
                            width={28}
                            height={28}
                            color={theme.palette.primary.main}
                          />
                        </div>
                        <div>
                          <Typography
                            variant="subtitle1"
                            fontWeight={700}
                            lineHeight={1.2}
                          >
                            Memory
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            RAM utilization
                          </Typography>
                        </div>
                      </div>
                      <MetricBar
                        label="Memory"
                        percent={totalMemPercent}
                        color={theme.palette.primary.main}
                        tooltip={`${formatFileSize(totalMemUsage)} / ${formatFileSize(systemMemTotal)}`}
                        rightLabel={formatFileSize(totalMemUsage)}
                      />
                    </FrostedCard>
                  </Grid>
                  {dockerInfo.disk_total > 0 && (
                    <Grid size={{ xs: 12, sm: 4 }}>
                      <FrostedCard style={{ padding: 8 }}>
                        <div
                          style={{
                            display: "flex",
                            alignItems: "center",
                            gap: 6,
                            marginBottom: 6,
                          }}
                        >
                          <div
                            style={{
                              width: 40,
                              height: 40,
                              borderRadius: 8,
                              display: "flex",
                              alignItems: "center",
                              justifyContent: "center",
                              flexShrink: 0,
                            }}
                          >
                            <Icon
                              icon="mdi:harddisk"
                              width={28}
                              height={28}
                              color={theme.palette.primary.main}
                            />
                          </div>
                          <div>
                            <Typography
                              variant="subtitle1"
                              fontWeight={700}
                              lineHeight={1.2}
                            >
                              Disk Usage
                            </Typography>
                            <Typography
                              variant="caption"
                              color="text.secondary"
                            >
                              Storage utilization
                            </Typography>
                          </div>
                        </div>
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
                      </FrostedCard>
                    </Grid>
                  )}
                </>
              )}
              <Grid size={{ xs: 12, sm: 4 }}>
                <FrostedCard style={{ padding: 8, height: "100%" }}>
                  <DaemonSection
                    title="Version"
                    subtitle="Engine & runtime versions"
                    icon={
                      <Icon
                        icon="mdi:tag"
                        width={28}
                        height={28}
                        color={theme.palette.primary.main}
                      />
                    }
                  >
                    <InfoRow label="Server" value={dockerInfo.server_version} />
                    <InfoRow label="API" value={dockerInfo.api_version} />
                    <InfoRow label="Go" value={dockerInfo.go_version} />
                    <InfoRow label="Git Commit" value={dockerInfo.git_commit} />
                  </DaemonSection>
                </FrostedCard>
              </Grid>
              <Grid size={{ xs: 12, sm: 4 }}>
                <FrostedCard style={{ padding: 8, height: "100%" }}>
                  <DaemonSection
                    title="System"
                    subtitle="Host machine information"
                    icon={
                      <Icon
                        icon="mdi:monitor"
                        width={28}
                        height={28}
                        color={theme.palette.primary.main}
                      />
                    }
                  >
                    <InfoRow label="Hostname" value={dockerInfo.name} />
                    <InfoRow label="OS" value={dockerInfo.operating_system} />
                    <InfoRow
                      label="Architecture"
                      value={dockerInfo.architecture}
                    />
                    <InfoRow
                      label="Root Dir"
                      value={dockerInfo.docker_root_dir}
                    />
                  </DaemonSection>
                </FrostedCard>
              </Grid>
              <Grid size={{ xs: 12, sm: 4 }}>
                <FrostedCard style={{ padding: 8, height: "100%" }}>
                  <DaemonSection
                    title="Configuration"
                    subtitle="Storage & runtime settings"
                    icon={
                      <Icon
                        icon="mdi:wrench"
                        width={28}
                        height={28}
                        color={theme.palette.primary.main}
                      />
                    }
                  >
                    <InfoRow
                      label="Storage Driver"
                      value={dockerInfo.storage_driver}
                    />
                    <InfoRow
                      label="Cgroup Driver"
                      value={dockerInfo.cgroup_driver}
                    />
                    <InfoRow
                      label="Cgroup Version"
                      value={dockerInfo.cgroup_version}
                    />
                    <InfoRow
                      label="Default Runtime"
                      value={dockerInfo.default_runtime}
                    />
                  </DaemonSection>
                </FrostedCard>
              </Grid>
            </>
          )}
        </Grid>
      </Collapse>

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
        <Typography variant="subtitle1" fontWeight={700}>
          Resources
        </Typography>
        <IconButton
          size="small"
          className="section-toggle"
          component="span"
          sx={{
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
        </IconButton>
      </div>
      <Collapse in={sections.resources}>
        <Grid container spacing={2}>
          {/* Containers table */}
          <Grid size={{ xs: 12, lg: 6 }}>
            <FrostedCard>
              <ResourceCardHeader
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
                  <Select
                    variant="standard"
                    disableUnderline
                    value={containerSort}
                    onChange={(e) =>
                      setContainerSort(e.target.value as typeof containerSort)
                    }
                    sx={{
                      fontSize: "0.75rem",
                      color: "text.secondary",
                      lineHeight: 1.4,
                      "& .MuiSelect-select": { p: 0, pr: "18px !important" },
                      "& .MuiSvgIcon-root": {
                        fontSize: "0.9rem",
                        color: "text.secondary",
                      },
                    }}
                  >
                    <MenuItem value="recent">Recent containers</MenuItem>
                    <MenuItem value="name">Sort by name</MenuItem>
                    <MenuItem value="state">Sort by state</MenuItem>
                  </Select>
                }
                onViewAll={() => navigateToTab("containers")}
              />

              <div
                className="dd-containers-grid"
                style={{ paddingInline: 8, paddingBlock: 3 }}
              >
                {(
                  [
                    { label: "Name" },
                    { label: "Image", hiddenXs: true },
                    { label: "State" },
                    { label: "Status", hiddenXs: true },
                  ] as { label: string; hiddenXs?: boolean }[]
                ).map(({ label, hiddenXs }) => (
                  <Typography
                    key={label}
                    variant="overline"
                    color="text.secondary"
                    sx={{
                      fontSize: "0.65rem",
                      ...(hiddenXs && { display: { xs: "none", sm: "block" } }),
                    }}
                  >
                    {label}
                  </Typography>
                ))}
              </div>
              <Divider />

              <div
                className="custom-scrollbar"
                style={{ maxHeight: SCROLL_HEIGHT, overflowY: "auto" }}
              >
                {previewContainers.length === 0 ? (
                  <div
                    style={{
                      paddingInline: 8,
                      paddingBlock: 12,
                      textAlign: "center",
                    }}
                  >
                    <Typography variant="body2" color="text.secondary">
                      No containers found
                    </Typography>
                  </div>
                ) : (
                  previewContainers.map((container, i) => {
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
                            <Typography variant="body2" fontWeight={500} noWrap>
                              {name}
                            </Typography>
                          </div>
                          <Typography
                            variant="caption"
                            color="text.secondary"
                            noWrap
                            sx={{ display: { xs: "none", sm: "block" } }}
                          >
                            {container.Image}
                          </Typography>
                          <div>
                            <StateChip
                              state={container.State}
                              status={container.Status}
                            />
                          </div>
                          <Typography
                            variant="caption"
                            color="text.secondary"
                            noWrap
                            sx={{ display: { xs: "none", sm: "block" } }}
                          >
                            {container.Status.replace(/\s*\(.*?\)\s*$/, "")}
                          </Typography>
                        </div>
                        {i < previewContainers.length - 1 && <Divider />}
                      </React.Fragment>
                    );
                  })
                )}
              </div>

              <Divider />
              <div style={{ paddingInline: 8, paddingBlock: 4 }}>
                <Typography variant="caption" color="text.secondary">
                  {containers.length} containers
                </Typography>
              </div>
            </FrostedCard>
          </Grid>

          {/* Images table */}
          <Grid size={{ xs: 12, lg: 6 }}>
            <FrostedCard>
              <ResourceCardHeader
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
                  <Select
                    variant="standard"
                    disableUnderline
                    value={imageSort}
                    onChange={(e) =>
                      setImageSort(e.target.value as typeof imageSort)
                    }
                    sx={{
                      fontSize: "0.75rem",
                      color: "text.secondary",
                      lineHeight: 1.4,
                      "& .MuiSelect-select": { p: 0, pr: "18px !important" },
                      "& .MuiSvgIcon-root": {
                        fontSize: "0.9rem",
                        color: "text.secondary",
                      },
                    }}
                  >
                    <MenuItem value="largest">Largest images</MenuItem>
                    <MenuItem value="recent">Most recent</MenuItem>
                    <MenuItem value="name">Sort by name</MenuItem>
                    <MenuItem value="usage">Most used</MenuItem>
                  </Select>
                }
                onViewAll={() => navigateToTab("images")}
              />

              <div
                className="dd-images-grid"
                style={{ paddingInline: 8, paddingBlock: 3 }}
              >
                {(
                  [
                    { label: "Repository" },
                    { label: "Tag", hiddenXs: true },
                    { label: "Status" },
                    { label: "Size", hiddenXs: true },
                  ] as { label: string; hiddenXs?: boolean }[]
                ).map(({ label, hiddenXs }) => (
                  <Typography
                    key={label}
                    variant="overline"
                    color="text.secondary"
                    sx={{
                      fontSize: "0.65rem",
                      ...(hiddenXs && { display: { xs: "none", sm: "block" } }),
                    }}
                  >
                    {label}
                  </Typography>
                ))}
              </div>
              <Divider />

              <div
                className="custom-scrollbar"
                style={{ maxHeight: SCROLL_HEIGHT, overflowY: "auto" }}
              >
                {previewImages.length === 0 ? (
                  <div
                    style={{
                      paddingInline: 8,
                      paddingBlock: 12,
                      textAlign: "center",
                    }}
                  >
                    <Typography variant="body2" color="text.secondary">
                      No images found
                    </Typography>
                  </div>
                ) : (
                  previewImages.map((image, i) => {
                    const fullTag = image.RepoTags?.[0] ?? "<none>:<none>";
                    const colonIdx = fullTag.lastIndexOf(":");
                    const repo =
                      colonIdx >= 0 ? fullTag.slice(0, colonIdx) : fullTag;
                    const tag =
                      colonIdx >= 0 ? fullTag.slice(colonIdx + 1) : "";
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
                          <Typography variant="body2" fontWeight={500} noWrap>
                            {repo}
                          </Typography>
                          <Typography
                            variant="caption"
                            color="text.secondary"
                            noWrap
                            sx={{ display: { xs: "none", sm: "block" } }}
                          >
                            {tag}
                          </Typography>
                          <div>
                            {inUse && (
                              <Chip
                                size="small"
                                label="In Use"
                                sx={stateChipSx(theme.palette.success.main)}
                              />
                            )}
                          </div>
                          <Typography
                            variant="caption"
                            color="text.secondary"
                            noWrap
                            sx={{ display: { xs: "none", sm: "block" } }}
                          >
                            {formatFileSize(image.Size)}
                          </Typography>
                        </div>
                        {i < previewImages.length - 1 && <Divider />}
                      </React.Fragment>
                    );
                  })
                )}
              </div>

              <Divider />
              <div style={{ paddingInline: 8, paddingBlock: 4 }}>
                <Typography variant="caption" color="text.secondary">
                  {images.length} images
                </Typography>
              </div>
            </FrostedCard>
          </Grid>
        </Grid>
      </Collapse>
    </div>
  );
};

export default DockerDashboard;
