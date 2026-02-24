import DragIndicatorIcon from "@mui/icons-material/DragIndicator";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  Box,
  Chip,
  Collapse,
  IconButton,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useQueryClient } from "@tanstack/react-query";
import { motion } from "framer-motion";
import React, { Suspense, useMemo, useState } from "react";
import { toast } from "sonner";

import ActionButton from "./ActionButton";

import { linuxio } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import { ContainerInfo } from "@/types/container";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

const LogsDialog = React.lazy(() => import("@/pages/main/docker/LogsDialog"));
const TerminalDialog = React.lazy(
  () => import("@/pages/main/docker/TerminalDialog"),
);

// ── Helpers ───────────────────────────────────────────────────────────────────

const getDisplayState = (container: ContainerInfo) => {
  const s = container.Status.toLowerCase();
  if (s.includes("unhealthy")) return "Unhealthy";
  if (s.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return container.State;
};

const getStatusDotColor = (state: string) => {
  if (state === "Healthy" || state === "Running") return "#00e676";
  if (state === "Unhealthy") return "#ffc107";
  if (state === "Stopped" || state === "Dead") return "#f44336";
  return "#ffc107";
};

const getImageVersion = (image: string) => {
  const noDigest = image.split("@")[0];
  const parts = noDigest.split(":");
  if (parts.length < 2) return "—";
  const tag = parts[parts.length - 1];
  return tag || "—";
};

const formatUptime = (createdUnix: number) => {
  const secs = Math.floor(Date.now() / 1000) - createdUnix;
  if (secs < 0) return "—";
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60) % 60;
  const h = Math.floor(secs / 3600) % 24;
  const d = Math.floor(secs / 86400);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
};

// ── Per-row component ─────────────────────────────────────────────────────────

interface ContainerRowProps {
  container: ContainerInfo;
  index: number;
  editMode?: boolean;
}

const ContainerRow: React.FC<ContainerRowProps> = ({ container, index, editMode }) => {
  const theme = useTheme();
  const queryClient = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: container.Id });
  const [logDialogOpen, setLogDialogOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [hasLoadedLogs, setHasLoadedLogs] = useState(false);
  const [hasLoadedTerminal, setHasLoadedTerminal] = useState(false);

  const name = useMemo(
    () => container.Names?.[0]?.replace("/", "") || "Unnamed",
    [container.Names],
  );

  const { mutate: startContainer } = linuxio.docker.start_container.useMutation(
    {
      onSuccess: () => {
        toast.success(`Container ${name} started`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to start ${name}`)),
    },
  );
  const { mutate: stopContainer } = linuxio.docker.stop_container.useMutation({
    onSuccess: () => {
      toast.success(`Container ${name} stopped`);
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.list_containers.queryKey(),
      });
    },
    onError: (err: Error) =>
      toast.error(getMutationErrorMessage(err, `Failed to stop ${name}`)),
  });
  const { mutate: restartContainer } =
    linuxio.docker.restart_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} restarted`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to restart ${name}`)),
    });
  const { mutate: removeContainer } =
    linuxio.docker.remove_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${name} removed`);
        queryClient.invalidateQueries({
          queryKey: linuxio.docker.list_containers.queryKey(),
        });
      },
      onError: (err: Error) =>
        toast.error(getMutationErrorMessage(err, `Failed to remove ${name}`)),
    });

  // ── derived ─────────────────────────────────────────────────────────────────
  const cpuPercent = container.metrics?.cpu_percent ?? 0;
  const memUsage = container.metrics?.mem_usage ?? 0;
  const displayState = getDisplayState(container);
  const version = getImageVersion(container.Image);
  const uptime = formatUptime(container.Created);

  // Deduped ports
  const ports = useMemo(() => {
    const seen = new Set<string>();
    return (container.Ports ?? [])
      .filter((p) => {
        // Dedupe by private+public port only — collapses IPv4/IPv6 duplicate entries
        const key = p.PublicPort
          ? `${p.PrivatePort}/${p.Type}:${p.PublicPort}`
          : `${p.PrivatePort}/${p.Type}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      })
      .sort(
        (a, b) => a.PrivatePort - b.PrivatePort || a.Type.localeCompare(b.Type),
      );
  }, [container.Ports]);

  // Networks
  const networks = useMemo(
    () => Object.entries(container.NetworkSettings?.Networks ?? {}),
    [container.NetworkSettings],
  );

  // Volumes
  const mounts = useMemo(
    () =>
      (container.Mounts ?? []).filter(
        (m) => m.Type === "bind" || m.Type === "volume",
      ),
    [container.Mounts],
  );

  const isWatchtower =
    container.Labels?.["com.docker.compose.project"] === "linuxio-watchtower";

  // ---- auto-update ----
  const { data: autoUpdateContainers = [] } =
    linuxio.docker.list_auto_update_containers.useQuery({
      enabled: !isWatchtower,
    });
  const autoUpdate = autoUpdateContainers.includes(name);
  const [autoUpdateLoading, setAutoUpdateLoading] = useState(false);
  const autoUpdateChecked = isWatchtower ? true : autoUpdate;
  const autoUpdateDisabled = autoUpdateLoading || isWatchtower;
  const autoUpdateTooltip = isWatchtower
    ? "Auto Update: Managed by LinuxIO"
    : autoUpdate
      ? "Auto Update: On"
      : "Auto Update: Off";

  const handleAutoUpdateToggle = async (enabled: boolean) => {
    if (isWatchtower) return;
    setAutoUpdateLoading(true);
    try {
      await linuxio.docker.set_auto_update.call(
        JSON.stringify({ container: name, enabled }),
      );
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.list_auto_update_containers.queryKey(),
      });
      toast.success(`Auto-update ${enabled ? "enabled" : "disabled"} for ${name}`);
    } catch {
      toast.error(`Failed to update auto-update setting for ${name}`);
    } finally {
      setAutoUpdateLoading(false);
    }
  };

  const rowBg =
    index % 2 === 0
      ? "transparent"
      : theme.palette.mode === "dark"
        ? "rgba(255,255,255,0.04)"
        : "rgba(0,0,0,0.05)";

  return (
    <React.Fragment>
      <TableRow
        ref={setNodeRef}
        style={{
          transform: CSS.Transform.toString(transform),
          transition,
          opacity: isDragging ? 0.4 : 1,
          backgroundColor: rowBg,
        }}
        sx={{
          "& .MuiTableCell-root": { borderBottom: "none" },
          "@media (max-width: 600px)": {
            "& .MuiTableCell-root": { fontSize: "0.75rem", padding: "8px 4px" },
          },
        }}
      >
        {/* Drag handle */}
        {editMode && (
          <TableCell width="28px" sx={{ p: "0 4px" }}>
            <Box
              component="span"
              {...attributes}
              {...listeners}
              sx={{ display: "flex", alignItems: "center", color: "text.disabled", cursor: "grab", "&:active": { cursor: "grabbing" } }}
            >
              <DragIndicatorIcon fontSize="small" />
            </Box>
          </TableCell>
        )}
        {/* Name (with status dot) */}
        <TableCell>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            <Tooltip title={displayState}>
              <Box
                sx={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  bgcolor: getStatusDotColor(displayState),
                  flexShrink: 0,
                }}
              />
            </Tooltip>
            <DockerIcon identifier={container.icon} size={24} alt={name} />
            <Typography variant="body2" fontWeight="bold" noWrap>
              {name}
            </Typography>
          </Box>
        </TableCell>

        {/* Version */}
        <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
          <Typography
            variant="body2"
            sx={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              color: "text.secondary",
            }}
          >
            {version}
          </Typography>
        </TableCell>

        {/* Uptime */}
        <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
          <Typography
            variant="body2"
            sx={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              color: "text.secondary",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {uptime}
          </Typography>
        </TableCell>

        {/* Network */}
        <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
          {networks.length > 0 ? (
            <Tooltip
              title={networks.map(([n]) => n).join(", ")}
              disableHoverListener={networks.length <= 1}
            >
              <Typography
                variant="body2"
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.78rem",
                  color: "text.secondary",
                }}
                noWrap
              >
                {networks[0][0]}
                {networks.length > 1 && (
                  <Box
                    component="span"
                    sx={{ ml: 0.5, color: "text.disabled" }}
                  >
                    +{networks.length - 1}
                  </Box>
                )}
              </Typography>
            </Tooltip>
          ) : (
            <Typography variant="body2" color="text.disabled">
              —
            </Typography>
          )}
        </TableCell>

        {/* Container IP */}
        <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
          {networks.length > 0 && networks[0][1].IPAddress ? (
            <Tooltip
              title={networks
                .map(([n, ep]) => `${n}: ${ep.IPAddress}`)
                .join("\n")}
              disableHoverListener={networks.length <= 1}
            >
              <Typography
                variant="body2"
                sx={{ fontFamily: "monospace", fontSize: "0.78rem" }}
              >
                {networks[0][1].IPAddress}
              </Typography>
            </Tooltip>
          ) : (
            <Typography variant="body2" color="text.disabled">
              —
            </Typography>
          )}
        </TableCell>

        {/* Ports (Container → Host) */}
        <TableCell sx={{ display: { xs: "none", xl: "table-cell" } }}>
          {ports.length > 0 ? (
            <Box sx={{ display: "flex", flexDirection: "column", gap: 0.25 }}>
              {ports.slice(0, 2).map((p, i) => (
                <Typography
                  key={i}
                  variant="body2"
                  noWrap
                  sx={{ fontFamily: "monospace", fontSize: "0.75rem" }}
                >
                  <Box component="span" color="text.primary">
                    {p.PrivatePort}/{p.Type}
                  </Box>
                  <Box component="span" color="text.disabled" mx={0.5}>
                    →
                  </Box>
                  <Box component="span" color="text.secondary">
                    {p.PublicPort ?? "—"}
                  </Box>
                </Typography>
              ))}
              {ports.length > 2 && (
                <Typography variant="caption" color="text.disabled">
                  +{ports.length - 2} more
                </Typography>
              )}
            </Box>
          ) : (
            <Typography variant="body2" color="text.disabled">
              —
            </Typography>
          )}
        </TableCell>

        {/* Volumes (App → Host) */}
        <TableCell
          sx={{ display: { xs: "none", xl: "table-cell" }, maxWidth: 280 }}
        >
          {mounts.length > 0 ? (
            <Box sx={{ display: "flex", flexDirection: "column", gap: 0.25 }}>
              {mounts.slice(0, 2).map((m, i) => (
                <Tooltip key={i} title={`${m.Destination} → ${m.Source}`}>
                  <Typography
                    variant="body2"
                    noWrap
                    sx={{ fontFamily: "monospace", fontSize: "0.75rem" }}
                  >
                    <Box component="span" color="text.primary">
                      {m.Destination}
                    </Box>
                    <Box component="span" color="text.disabled" mx={0.5}>
                      →
                    </Box>
                    <Box component="span" color="text.secondary">
                      {m.Source}
                    </Box>
                  </Typography>
                </Tooltip>
              ))}
              {mounts.length > 2 && (
                <Typography variant="caption" color="text.disabled">
                  +{mounts.length - 2} more
                </Typography>
              )}
            </Box>
          ) : (
            <Typography variant="body2" color="text.disabled">
              —
            </Typography>
          )}
        </TableCell>

        {/* CPU / Memory (stacked) */}
        <TableCell
          align="center"
          width="80px"
          sx={{ display: { xs: "none", xl: "table-cell" } }}
        >
          <Typography
            variant="body2"
            sx={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              color: "text.secondary",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {cpuPercent.toFixed(1)}%
          </Typography>
          <Typography
            variant="body2"
            sx={{
              fontFamily: "monospace",
              fontSize: "0.78rem",
              color: "text.secondary",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {formatFileSize(memUsage)}
          </Typography>
        </TableCell>

        {/* Actions + expand */}
        <TableCell align="right">
          <Box
            sx={{
              display: "flex",
              justifyContent: "flex-end",
              alignItems: "center",
              gap: { xs: 0, sm: 0.5 },
            }}
          >
            {isWatchtower ? (
              <Tooltip title="View Logs">
                <Chip
                  label="Managed by LinuxIO"
                  size="small"
                  variant="outlined"
                  onClick={() => {
                    setHasLoadedLogs(true);
                    setLogDialogOpen(true);
                  }}
                  sx={{ fontSize: "0.68rem", opacity: 0.7, cursor: "pointer" }}
                />
              </Tooltip>
            ) : (
              <>
                {container.State !== "running" && (
                  <Tooltip title="Start">
                    <span>
                      <ActionButton
                        icon="mdi:play"
                        onClick={() => startContainer([container.Id])}
                      />
                    </span>
                  </Tooltip>
                )}
                {container.State === "running" && (
                  <Tooltip title="Stop">
                    <span>
                      <ActionButton
                        icon="mdi:stop"
                        onClick={() => stopContainer([container.Id])}
                      />
                    </span>
                  </Tooltip>
                )}
                <Tooltip title="Restart">
                  <span>
                    <ActionButton
                      icon="mdi:restart"
                      onClick={() => restartContainer([container.Id])}
                    />
                  </span>
                </Tooltip>
                <Tooltip title="Remove">
                  <span>
                    <ActionButton
                      icon="mdi:delete"
                      onClick={() => removeContainer([container.Id])}
                    />
                  </span>
                </Tooltip>
                <Tooltip title="Logs">
                  <span>
                    <ActionButton
                      icon="mdi:file-document-outline"
                      onClick={() => {
                        setHasLoadedLogs(true);
                        setLogDialogOpen(true);
                      }}
                    />
                  </span>
                </Tooltip>
              </>
            )}
            {!isWatchtower && (
              <Tooltip title="Terminal">
                <span>
                  <ActionButton
                    icon="mdi:console"
                    onClick={() => {
                      setHasLoadedTerminal(true);
                      setTerminalOpen(true);
                    }}
                  />
                </span>
              </Tooltip>
            )}
            <Tooltip title={autoUpdateTooltip}>
              <Box component="span" sx={{ display: "inline-flex" }}>
                <Switch
                  size="small"
                  checked={autoUpdateChecked}
                  onChange={(e) => handleAutoUpdateToggle(e.target.checked)}
                  disabled={autoUpdateDisabled}
                  sx={
                    isWatchtower
                      ? {
                          "& .MuiSwitch-switchBase.Mui-checked.Mui-disabled": {
                            color: "action.disabled",
                          },
                          "& .MuiSwitch-switchBase.Mui-disabled + .MuiSwitch-track":
                            {
                              opacity: 1,
                              backgroundColor: "action.disabledBackground",
                            },
                        }
                      : undefined
                  }
                />
              </Box>
            </Tooltip>
            <IconButton
              size="small"
              onClick={() => setExpanded((v) => !v)}
              sx={{
                ml: 0.5,
                visibility:
                  ports.length > 2 || mounts.length > 2 ? "visible" : "hidden",
              }}
            >
              <ExpandMoreIcon
                fontSize="small"
                style={{
                  transform: expanded ? "rotate(180deg)" : "rotate(0deg)",
                  transition: "0.2s",
                }}
              />
            </IconButton>
          </Box>
        </TableCell>
      </TableRow>

      {/* Expanded row — full ports + volumes */}
      {(ports.length > 2 || mounts.length > 2) && (
        <TableRow
          sx={{
            "& .MuiTableCell-root": { borderBottom: "none" },
            backgroundColor: "transparent",
          }}
        >
          <TableCell style={{ paddingBottom: 0, paddingTop: 0 }} colSpan={editMode ? 10 : 9}>
            <Collapse in={expanded} timeout="auto" unmountOnExit>
              <Box
                component={motion.div}
                initial={{ opacity: 0, y: -8 }}
                animate={{ opacity: 1, y: 0 }}
                sx={{
                  mx: 2,
                  mb: 1,
                  borderRadius: 2,
                  p: 1.5,
                  display: "flex",
                  gap: 4,
                  flexWrap: "wrap",
                  bgcolor: (t) =>
                    t.palette.mode === "dark"
                      ? "rgba(255,255,255,0.04)"
                      : "rgba(0,0,0,0.03)",
                }}
              >
                {ports.length > 2 && (
                  <Box>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      fontWeight={600}
                      display="block"
                      mb={0.75}
                    >
                      ALL PORTS (Container → Host)
                    </Typography>
                    <Box
                      sx={{
                        display: "flex",
                        flexDirection: "column",
                        gap: 0.5,
                      }}
                    >
                      {ports.map((p, i) => (
                        <Typography
                          key={i}
                          variant="body2"
                          sx={{ fontFamily: "monospace", fontSize: "0.75rem" }}
                        >
                          <Box component="span" color="text.primary">
                            {p.PrivatePort}/{p.Type}
                          </Box>
                          <Box component="span" color="text.disabled" mx={0.75}>
                            →
                          </Box>
                          <Box component="span" color="text.secondary">
                            {p.PublicPort
                              ? `${p.IP && p.IP !== "0.0.0.0" ? p.IP + ":" : ""}${p.PublicPort}`
                              : "—"}
                          </Box>
                        </Typography>
                      ))}
                    </Box>
                  </Box>
                )}
                {mounts.length > 2 && (
                  <Box>
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      fontWeight={600}
                      display="block"
                      mb={0.75}
                    >
                      ALL VOLUMES (App → Host)
                    </Typography>
                    <Box
                      sx={{
                        display: "flex",
                        flexDirection: "column",
                        gap: 0.5,
                      }}
                    >
                      {mounts.map((m, i) => (
                        <Typography
                          key={i}
                          variant="body2"
                          sx={{ fontFamily: "monospace", fontSize: "0.75rem" }}
                        >
                          <Box component="span" color="text.primary">
                            {m.Destination}
                          </Box>
                          <Box component="span" color="text.disabled" mx={0.75}>
                            →
                          </Box>
                          <Box component="span" color="text.secondary">
                            {m.Source}
                          </Box>
                        </Typography>
                      ))}
                    </Box>
                  </Box>
                )}
              </Box>
            </Collapse>
          </TableCell>
        </TableRow>
      )}

      <Suspense fallback={null}>
        {hasLoadedLogs && (
          <LogsDialog
            open={logDialogOpen}
            onClose={() => setLogDialogOpen(false)}
            containerName={name}
            containerId={container.Id}
          />
        )}
        {hasLoadedTerminal && (
          <TerminalDialog
            open={terminalOpen}
            onClose={() => setTerminalOpen(false)}
            containerId={container.Id}
            containerName={name}
          />
        )}
      </Suspense>
    </React.Fragment>
  );
};

// ── Main export ───────────────────────────────────────────────────────────────

interface ContainerTableProps {
  containers: ContainerInfo[];
  editMode?: boolean;
}

const ContainerTable: React.FC<ContainerTableProps> = ({ containers, editMode = false }) => {
  return (
    <Box>
      <TableContainer className="custom-scrollbar" sx={{ overflowX: "auto" }}>
        <Table size="small" sx={{ borderRadius: 3, boxShadow: 2 }}>
          <TableHead>
            <TableRow
              sx={(t) => ({
                "& .MuiTableCell-root": { borderBottom: "none" },
                backgroundColor:
                  t.palette.mode === "dark"
                    ? "rgba(255,255,255,0.08)"
                    : "rgba(0,0,0,0.08)",
              })}
            >
              {editMode && <TableCell width="28px" />}
              <TableCell>Name</TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                Version
              </TableCell>
              <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
                Uptime
              </TableCell>
              <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
                Network
              </TableCell>
              <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
                Container IP
              </TableCell>
              <TableCell
                width="160px"
                sx={{ display: { xs: "none", xl: "table-cell" } }}
              >
                Ports (Container→Host)
              </TableCell>
              <TableCell sx={{ display: { xs: "none", xl: "table-cell" } }}>
                Volumes (App→Host)
              </TableCell>
              <TableCell
                align="center"
                width="100px"
                sx={{ display: { xs: "none", xl: "table-cell" } }}
              >
                CPU / Mem
              </TableCell>
              <TableCell align="center" width="180px">
                Actions
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {containers.map((container, index) => (
              <ContainerRow
                key={container.Id}
                container={container}
                index={index}
                editMode={editMode}
              />
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      {containers.length === 0 && (
        <Box textAlign="center" py={4}>
          <Typography variant="body2" color="text.secondary">
            No containers found.
          </Typography>
        </Box>
      )}
    </Box>
  );
};

export default ContainerTable;
