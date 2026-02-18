import {
  ChevronRight as ChevronRightIcon,
  Inventory2 as ContainersIcon,
  Layers as ImagesIcon,
} from "@mui/icons-material";
import { Box, Button, Chip, Divider, Grid, Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useMemo } from "react";
import { useSearchParams } from "react-router-dom";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import DockerIcon from "@/components/docker/DockerIcon";
import MetricBar from "@/components/gauge/MetricBar";
import { formatFileSize } from "@/utils/formaters";

// ─── small helpers ────────────────────────────────────────────────────────────

const StateChip: React.FC<{ state: string; status: string }> = ({
  state,
  status,
}) => {
  if (status.toLowerCase().includes("unhealthy"))
    return <Chip size="small" label="Unhealthy" color="warning" />;
  if (state === "running")
    return <Chip size="small" label="Running" color="success" />;
  if (state === "exited" || state === "dead")
    return <Chip size="small" label="Stopped" color="error" />;
  return <Chip size="small" label={state} />;
};

const InfoRow: React.FC<{ label: string; value: React.ReactNode }> = ({
  label,
  value,
}) => (
  <Box
    sx={{
      display: "flex",
      justifyContent: "space-between",
      alignItems: "baseline",
      py: 0.6,
      borderBottom: "1px solid",
      borderColor: "divider",
      "&:last-child": { borderBottom: "none" },
      gap: 1,
    }}
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
  </Box>
);

const DaemonSection: React.FC<{
  title: string;
  children: React.ReactNode;
}> = ({ title, children }) => (
  <Box >
    <Typography variant="overline" color="text.secondary">
      {title}
    </Typography>
    <Box sx={{ mt: 0.75 }}>{children}</Box>
  </Box>
);

const ResourceCardHeader: React.FC<{
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  onViewAll: () => void;
}> = ({ icon, title, subtitle, onViewAll }) => (
  <Box
    sx={{
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      p: 2,
      pb: 1.5,
    }}
  >
    <Box sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
      <Box
        sx={{
          width: 40,
          height: 40,
          borderRadius: 2,
          background: "linear-gradient(135deg, #7c3aed 0%, #4f46e5 100%)",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        {icon}
      </Box>
      <Box>
        <Typography variant="subtitle1" fontWeight={700} lineHeight={1.2}>
          {title}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {subtitle}
        </Typography>
      </Box>
    </Box>
    <Button
      size="small"
      endIcon={<ChevronRightIcon />}
      onClick={onViewAll}
      sx={{ flexShrink: 0 }}
    >
      View All
    </Button>
  </Box>
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
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("dockerTab", tab);
        return next;
      },
      { replace: true },
    );
  };

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

  const previewContainers = containers;
  const previewImages = useMemo(
    () => [...images].sort((a, b) => b.Size - a.Size),
    [images],
  );

  const SCROLL_HEIGHT = 165;

  return (
    <Box>
      {/* ── Stat Cards ─────────────────────────────────────────────────────── */}
      <Grid container spacing={2} sx={{ mb: 2 }}>
        {(
          [
            {
              label: "Containers",
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
              value: `${images.length}`,
              detail: `${formatFileSize(totalImageSize)} on disk`,
            },
            {
              label: "Networks",
              value: `${networks.length}`,
              detail: `${networks.filter((n) => !n.Internal).length} external`,
            },
            {
              label: "Volumes",
              value: `${volumes.length}`,
              detail: `${volumes.filter((v) => v.Driver === "local").length} local`,
            },
          ] as { label: string; value: string; detail: string }[]
        ).map(({ label, value, detail }) => (
          <Grid key={label} size={{ xs: 6, md: 3 }}>
            <FrostedCard sx={{ px: 2.5, py: 2 }}>
              <Typography
                variant="overline"
                color="text.secondary"
                sx={{ lineHeight: 1.6 }}
              >
                {label}
              </Typography>
              <Box sx={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", mt: 0.25 }}>
                <Typography variant="h6" fontWeight={700} sx={{ lineHeight: 1.2 }}>
                  {value}
                </Typography>
                <Typography variant="caption" color="text.secondary" noWrap sx={{ textAlign: "right" }}>
                  {detail}
                </Typography>
              </Box>
            </FrostedCard>
          </Grid>
        ))}
      </Grid>

      {/* ── Resource Usage ─────────────────────────────────────────────────── */}
      {runningContainers.length > 0 && (
        <FrostedCard sx={{ p: 2, mb: 2 }}>
          <Typography variant="overline" color="text.secondary">
            Resource Usage
          </Typography>
          <Grid container spacing={4} sx={{ mt: 0.5 }}>
            <Grid size={{ xs: 12, sm: 4 }}>
              <MetricBar
                label="CPU"
                percent={Math.min(totalCpu, 100)}
                color={theme.palette.primary.main}
                tooltip={`Total CPU across ${runningContainers.length} running containers`}
                rightLabel={`${totalCpu.toFixed(1)}%`}
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <MetricBar
                label="Memory"
                percent={totalMemPercent}
                color={theme.palette.primary.main}
                tooltip={`${formatFileSize(totalMemUsage)} / ${formatFileSize(systemMemTotal)}`}
                rightLabel={formatFileSize(totalMemUsage)}
              />
            </Grid>
            {dockerInfo && dockerInfo.disk_total > 0 && (
              <Grid size={{ xs: 12, sm: 4 }}>
                <MetricBar
                  label="Disk (Docker)"
                  percent={Math.min(
                    (dockerInfo.disk_used / dockerInfo.disk_total) * 100,
                    100,
                  )}
                  color={theme.palette.primary.main}
                  tooltip={`Docker disk usage: ${formatFileSize(dockerInfo.disk_used)} / ${formatFileSize(dockerInfo.disk_total)}`}
                  rightLabel={formatFileSize(dockerInfo.disk_used)}
                />
              </Grid>
            )}
          </Grid>
        </FrostedCard>
      )}
      <Typography variant="subtitle1" fontWeight={700} sx={{ mb: 1.5 }}>
        Docker Daemon
      </Typography>
      {/* ── Docker Daemon ───────────────────────────────────────────────────── */}
      <Grid container spacing={2} sx={{ mb: 2 }}>
        {dockerInfo && (
          <Grid size={{ xs: 12 }}>
            <FrostedCard sx={{ p: 2, height: "100%" }}>
              <Grid container spacing={4} sx={{ mt: 1 }}>
                <Grid size={{ xs: 12, sm: 4 }}>
                  <DaemonSection title="Version">
                    <InfoRow label="Server" value={dockerInfo.server_version} />
                    <InfoRow label="API" value={dockerInfo.api_version} />
                    <InfoRow label="Go" value={dockerInfo.go_version} />
                    <InfoRow label="Git Commit" value={dockerInfo.git_commit} />
                  </DaemonSection>
                </Grid>
                <Grid size={{ xs: 12, sm: 4 }}>
                  <DaemonSection title="System">
                    <InfoRow label="Hostname" value={dockerInfo.name} />
                    <InfoRow label="OS" value={dockerInfo.operating_system} />
                    <InfoRow label="Architecture" value={dockerInfo.architecture} />
                    <InfoRow label="Root Dir" value={dockerInfo.docker_root_dir} />
                  </DaemonSection>
                </Grid>
                <Grid size={{ xs: 12, sm: 4 }}>
                  <DaemonSection title="Configuration">
                    <InfoRow label="Storage Driver" value={dockerInfo.storage_driver} />
                    <InfoRow label="Cgroup Driver" value={dockerInfo.cgroup_driver} />
                    <InfoRow label="Cgroup Version" value={dockerInfo.cgroup_version} />
                    <InfoRow label="Default Runtime" value={dockerInfo.default_runtime} />
                  </DaemonSection>
                </Grid>
              </Grid>


            </FrostedCard>
          </Grid>
        )}
      </Grid>

      {/* ── Resources ──────────────────────────────────────────────────────── */}
      <Typography variant="subtitle1" fontWeight={700} sx={{ mb: 1.5 }}>
        Resources
      </Typography>
      <Grid container spacing={2}>
        {/* Containers table */}
        <Grid size={{ xs: 12, lg: 6 }}>
          <FrostedCard>
            <ResourceCardHeader
              icon={<ContainersIcon sx={{ color: "#fff", fontSize: 20 }} />}
              title="Containers"
              subtitle="Recent containers"
              onViewAll={() => navigateToTab("containers")}
            />

            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: "1fr 1fr 90px 1fr",
                px: 2,
                py: 0.75,
              }}
            >
              {["Name", "Image", "State", "Status"].map((col) => (
                <Typography
                  key={col}
                  variant="overline"
                  color="text.secondary"
                  sx={{ fontSize: "0.65rem" }}
                >
                  {col}
                </Typography>
              ))}
            </Box>
            <Divider />

            <Box className="custom-scrollbar" sx={{ maxHeight: SCROLL_HEIGHT, overflowY: "auto" }}>
              {previewContainers.length === 0 ? (
                <Box sx={{ px: 2, py: 3, textAlign: "center" }}>
                  <Typography variant="body2" color="text.secondary">
                    No containers found
                  </Typography>
                </Box>
              ) : (
                previewContainers.map((container, i) => {
                  const name =
                    container.Names?.[0]?.replace("/", "") || "Unnamed";
                  return (
                    <React.Fragment key={container.Id}>
                      <Box
                        sx={{
                          display: "grid",
                          gridTemplateColumns: "1fr 1fr 90px 1fr",
                          alignItems: "center",
                          px: 2,
                          py: 1.25,
                        }}
                      >
                        <Box
                          sx={{
                            display: "flex",
                            alignItems: "center",
                            gap: 1,
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
                        </Box>
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          noWrap
                        >
                          {container.Image}
                        </Typography>
                        <Box>
                          <StateChip
                            state={container.State}
                            status={container.Status}
                          />
                        </Box>
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          noWrap
                        >
                          {container.Status}
                        </Typography>
                      </Box>
                      {i < previewContainers.length - 1 && <Divider />}
                    </React.Fragment>
                  );
                })
              )}
            </Box>

            <Divider />
            <Box sx={{ px: 2, py: 1 }}>
              <Typography variant="caption" color="text.secondary">
                {containers.length} containers
              </Typography>
            </Box>
          </FrostedCard>
        </Grid>

        {/* Images table */}
        <Grid size={{ xs: 12, lg: 6 }}>
          <FrostedCard>
            <ResourceCardHeader
              icon={<ImagesIcon sx={{ color: "#fff", fontSize: 20 }} />}
              title="Images"
              subtitle="Largest images"
              onViewAll={() => navigateToTab("images")}
            />

            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: "1fr 80px 80px 80px",
                px: 2,
                py: 0.75,
              }}
            >
              {["Repository", "Status", "Tag", "Size"].map((col) => (
                <Typography
                  key={col}
                  variant="overline"
                  color="text.secondary"
                  sx={{ fontSize: "0.65rem" }}
                >
                  {col}
                </Typography>
              ))}
            </Box>
            <Divider />

            <Box className="custom-scrollbar" sx={{ maxHeight: SCROLL_HEIGHT, overflowY: "auto" }}>
              {previewImages.length === 0 ? (
                <Box sx={{ px: 2, py: 3, textAlign: "center" }}>
                  <Typography variant="body2" color="text.secondary">
                    No images found
                  </Typography>
                </Box>
              ) : (
                previewImages.map((image, i) => {
                  const fullTag = image.RepoTags?.[0] ?? "<none>:<none>";
                  const colonIdx = fullTag.lastIndexOf(":");
                  const repo =
                    colonIdx >= 0 ? fullTag.slice(0, colonIdx) : fullTag;
                  const tag = colonIdx >= 0 ? fullTag.slice(colonIdx + 1) : "";
                  const inUse = (image.Containers ?? 0) > 0;

                  return (
                    <React.Fragment key={image.Id}>
                      <Box
                        sx={{
                          display: "grid",
                          gridTemplateColumns: "1fr 80px 80px 80px",
                          alignItems: "center",
                          px: 2,
                          py: 1.25,
                        }}
                      >
                        <Typography variant="body2" fontWeight={500} noWrap>
                          {repo}
                        </Typography>
                        <Box>
                          {inUse && (
                            <Chip size="small" label="In Use" color="success" />
                          )}
                        </Box>
                        <Typography
                          variant="caption"
                          color="text.secondary"
                          noWrap
                        >
                          {tag}
                        </Typography>
                        <Typography variant="body2" noWrap>
                          {formatFileSize(image.Size)}
                        </Typography>
                      </Box>
                      {i < previewImages.length - 1 && <Divider />}
                    </React.Fragment>
                  );
                })
              )}
            </Box>

            <Divider />
            <Box sx={{ px: 2, py: 1 }}>
              <Typography variant="caption" color="text.secondary">
                {images.length} images
              </Typography>
            </Box>
          </FrostedCard>
        </Grid>
      </Grid>
    </Box>
  );
};

export default DockerDashboard;
