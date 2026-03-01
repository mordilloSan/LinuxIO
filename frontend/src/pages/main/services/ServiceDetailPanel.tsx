import BlockIcon from "@mui/icons-material/Block";
import CloseIcon from "@mui/icons-material/Close";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RefreshIcon from "@mui/icons-material/Refresh";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Box,
  Button,
  Divider,
  IconButton,
  Skeleton,
  Typography,
} from "@mui/material";
import React from "react";

import { linuxio } from "@/api";
import type { Service } from "@/api";
import FrostedCard from "@/components/cards/RootCard";

interface ServiceDetailPanelProps {
  service: Service;
  onClose: () => void;
}

const labelSx = {
  textTransform: "uppercase" as const,
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "text.secondary",
  flexShrink: 0,
  width: 90,
  pt: 0.3,
};

const sectionLabelSx = {
  textTransform: "uppercase" as const,
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "text.secondary",
  display: "block",
  mb: 0.75,
};

const Row: React.FC<{ label: string; children: React.ReactNode }> = ({
  label,
  children,
}) => (
  <Box
    sx={{
      display: "flex",
      gap: 2,
      py: 0.75,
      borderBottom: "1px solid",
      borderColor: "divider",
      "&:last-child": { borderBottom: "none" },
      alignItems: "flex-start",
    }}
  >
    <Typography variant="caption" sx={labelSx}>
      {label}
    </Typography>
    <Box sx={{ flex: 1, minWidth: 0 }}>{children}</Box>
  </Box>
);

const formatBytes = (val: unknown): string => {
  const b = Number(val ?? 0);
  if (!b || b > 1e18) return "—";
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} kB`;
  if (b < 1024 * 1024 * 1024) return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  return `${(b / (1024 * 1024 * 1024)).toFixed(2)} GB`;
};

const toStringArray = (val: unknown): string[] => {
  if (!Array.isArray(val)) return [];
  return val.filter((v): v is string => typeof v === "string" && v.length > 0);
};

const DEP_FIELDS: Array<{ label: string; key: string }> = [
  { label: "Requires", key: "Requires" },
  { label: "Wants", key: "Wants" },
  { label: "Wanted by", key: "WantedBy" },
  { label: "Triggered by", key: "TriggeredBy" },
  { label: "Part of", key: "PartOf" },
  { label: "Conflicts", key: "Conflicts" },
  { label: "Before", key: "Before" },
  { label: "After", key: "After" },
];

const ServiceDetailPanel: React.FC<ServiceDetailPanelProps> = ({
  service,
  onClose,
}) => {
  const { data: info, isPending } = linuxio.dbus.get_service_info.useQuery(
    service.name,
    { refetchInterval: 2000 },
  );

  const { mutate: startService, isPending: isStarting } =
    linuxio.dbus.start_service.useMutation();
  const { mutate: stopService, isPending: isStopping } =
    linuxio.dbus.stop_service.useMutation();
  const { mutate: restartService, isPending: isRestarting } =
    linuxio.dbus.restart_service.useMutation();
  const { mutate: reloadService, isPending: isReloading } =
    linuxio.dbus.reload_service.useMutation();
  const { mutate: enableService, isPending: isEnabling } =
    linuxio.dbus.enable_service.useMutation();
  const { mutate: disableService, isPending: isDisabling } =
    linuxio.dbus.disable_service.useMutation();
  const { mutate: maskService, isPending: isMasking } =
    linuxio.dbus.mask_service.useMutation();
  const { mutate: unmaskService, isPending: isUnmasking } =
    linuxio.dbus.unmask_service.useMutation();

  const fragmentPath = String(info?.FragmentPath ?? "");
  const mainPid = Number(info?.MainPID ?? 0);
  const memory = formatBytes(info?.MemoryCurrent);

  const isActive = service.active_state === "active";
  const unitFileState = String(info?.UnitFileState ?? "");
  const isEnabled =
    unitFileState === "enabled" || unitFileState === "enabled-runtime";
  const isMasked = unitFileState === "masked";

  const anyPending =
    isStarting ||
    isStopping ||
    isRestarting ||
    isReloading ||
    isEnabling ||
    isDisabling ||
    isMasking ||
    isUnmasking;

  return (
    <FrostedCard sx={{ p: 3 }}>
      {/* Header*/}
      <Box
        sx={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          mb: 1.5,
          gap: 1,
        }}
      >
        <Box sx={{ flex: 1, minWidth: 0 }}>
                                  <Typography
                                    variant="body2"
                                    fontWeight="bold"
                                    noWrap
                                    sx={{ minWidth: 0 }}
                                  >
            Relationships
          </Typography>
          <Typography
            variant="caption"
            color="text.secondary"
            noWrap
            sx={{ display: "block" }}
          >
            teste
          </Typography>
        </Box>

        <IconButton onClick={onClose} size="small">
          <CloseIcon fontSize="small" />
        </IconButton>
      </Box>

      {/* Info rows */}
      <Box>
        {mainPid > 0 && (
          <Row label="PID">
            <Typography variant="body2" fontWeight={500}>
              {mainPid}
            </Typography>
          </Row>
        )}

        {memory !== "—" && (
          <Row label="Memory">
            <Typography variant="body2" fontWeight={500}>
              {memory}
            </Typography>
          </Row>
        )}

        <Row label="Path">
          {isPending ? (
            <Skeleton width="80%" height={20} />
          ) : (
            <Typography
              variant="body2"
              fontWeight={500}
              sx={{ wordBreak: "break-all", fontSize: "0.8rem" }}
            >
              {fragmentPath || "—"}
            </Typography>
          )}
        </Row>

        {/* Dependency rows */}
        {!isPending &&
          DEP_FIELDS.map(({ label, key }) => {
            const items = toStringArray(info?.[key]);
            if (!items.length) return null;
            return (
              <Row key={key} label={label}>
                <Typography
                  variant="body2"
                  fontWeight={500}
                  sx={{ wordBreak: "break-word" }}
                >
                  {items.join(", ")}
                </Typography>
              </Row>
            );
          })}
      </Box>

      <Divider sx={{ my: 2 }} />

      {/* Actions */}

      <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap", mb: 2 }}>
        {isActive ? (
          <Button
            size="small"
            variant="outlined"
            color="error"
            startIcon={<StopCircleIcon fontSize="small" />}
            onClick={() => stopService([service.name])}
            disabled={anyPending}
          >
            Stop
          </Button>
        ) : (
          <Button
            size="small"
            variant="outlined"
            color="success"
            startIcon={<PlayArrowIcon fontSize="small" />}
            onClick={() => startService([service.name])}
            disabled={anyPending}
          >
            Start
          </Button>
        )}
        <Button
          size="small"
          variant="outlined"
          startIcon={<RestartAltIcon fontSize="small" />}
          onClick={() => restartService([service.name])}
          disabled={!isActive || anyPending}
        >
          Restart
        </Button>
        <Button
          size="small"
          variant="outlined"
          startIcon={<RefreshIcon fontSize="small" />}
          onClick={() => reloadService([service.name])}
          disabled={!isActive || anyPending}
        >
          Reload
        </Button>
      <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
        {isEnabled ? (
          <Button
            size="small"
            variant="outlined"
            startIcon={<BlockIcon fontSize="small" />}
            onClick={() => disableService([service.name])}
            disabled={isMasked || anyPending}
          >
            Disable
          </Button>
        ) : (
          <Button
            size="small"
            variant="outlined"
            color="success"
            startIcon={<PlayArrowIcon fontSize="small" />}
            onClick={() => enableService([service.name])}
            disabled={isMasked || anyPending}
          >
            Enable
          </Button>
        )}
        {isMasked ? (
          <Button
            size="small"
            variant="outlined"
            color="warning"
            startIcon={<VisibilityIcon fontSize="small" />}
            onClick={() => unmaskService([service.name])}
            disabled={anyPending}
          >
            Unmask
          </Button>
        ) : (
          <Button
            size="small"
            variant="outlined"
            startIcon={<VisibilityOffIcon fontSize="small" />}
            onClick={() => maskService([service.name])}
            disabled={anyPending}
          >
            Mask
          </Button>
        )}
      </Box>
      </Box>


    </FrostedCard>
  );
};

export default ServiceDetailPanel;
