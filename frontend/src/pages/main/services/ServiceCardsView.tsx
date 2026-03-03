import BlockIcon from "@mui/icons-material/Block";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import RefreshIcon from "@mui/icons-material/Refresh";
import RestartAltIcon from "@mui/icons-material/RestartAlt";
import StopCircleIcon from "@mui/icons-material/StopCircle";
import TerminalIcon from "@mui/icons-material/Terminal";
import VisibilityIcon from "@mui/icons-material/Visibility";
import VisibilityOffIcon from "@mui/icons-material/VisibilityOff";
import {
  Button,
  FormControlLabel,
  Switch,
  Tooltip,
  useTheme,
} from "@mui/material";
import { alpha } from "@mui/material/styles";
import React from "react";

import {
  AutoStartRow,
  DetailRow,
  UnitCardsView,
  formatBytes,
  formatTimestamp,
} from "./UnitViews";

import type { Service, UnitInfo } from "@/api";
import { linuxio, openServiceLogsStream } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { getServiceStatusColor } from "@/constants/statusColors";
import { useLogStream } from "@/hooks/useLogStream";

interface ServiceCardsViewProps {
  services: Service[];
  expanded: string | null;
  onExpand: (name: string | null) => void;
  renderDetailPanel: (service: Service) => React.ReactNode;
}

const ServiceStatusRows = React.memo<{ service: Service }>(({ service }) => {
  const statusColor = getServiceStatusColor(service.active_state);
  const isActive = service.active_state === "active";
  const ts = isActive
    ? formatTimestamp(service.active_enter_timestamp)
    : formatTimestamp(service.inactive_enter_timestamp);

  return (
    <>
      <DetailRow label="Status" noBorder>
        <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
          <span
            style={{
              fontSize: "0.85rem",
              fontWeight: 600,
              color: statusColor,
            }}
          >
            {isActive ? "Running" : service.active_state}
            {service.sub_state &&
              service.sub_state !== service.active_state && (
                <span
                  style={{
                    color: "var(--mui-palette-text-secondary)",
                    marginLeft: 8,
                    fontWeight: 400,
                  }}
                >
                  ({service.sub_state})
                </span>
              )}
          </span>
          <span
            style={{
              fontSize: "0.7rem",
              color: "var(--mui-palette-text-secondary)",
            }}
          >
            {isActive ? "Active" : "Inactive"} since {ts}
          </span>
        </div>
      </DetailRow>
      <AutoStartRow unitFileState={service.unit_file_state} />
    </>
  );
});
ServiceStatusRows.displayName = "ServiceStatusRows";

const ServiceLogsCard: React.FC<{ service: Service }> = ({ service }) => {
  const theme = useTheme();
  const { logs, isLoading, error, liveMode, setLiveMode, logsBoxRef } =
    useLogStream({
      open: true,
      createStream: (tail) => openServiceLogsStream(service.name, tail),
    });

  return (
    <FrostedCard sx={{ p: 3 }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 12,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <TerminalIcon
            fontSize="small"
            style={{ color: "var(--mui-palette-text-secondary)" }}
          />
          <span style={{ fontSize: "0.875rem", fontWeight: 600 }}>
            Service Logs
          </span>
        </div>
        <Tooltip title={liveMode ? "Live streaming ON" : "Live streaming OFF"}>
          <FormControlLabel
            control={
              <Switch
                checked={liveMode}
                onChange={(_, v) => setLiveMode(v)}
                size="small"
              />
            }
            label="Live"
          />
        </Tooltip>
      </div>
      <div
        style={{
          position: "relative",
          backgroundColor: theme.codeBlock.background,
          color: theme.codeBlock.color,
          borderRadius: 4,
          overflow: "hidden",
        }}
      >
        {isLoading && (
          <div
            style={{
              position: "absolute",
              inset: 0,
              background: alpha(theme.codeBlock.background, 0.85),
              zIndex: 10,
            }}
          >
            <ComponentLoader />
          </div>
        )}
        {error && (
          <div style={{ color: "var(--mui-palette-error-main)", padding: 16 }}>
            {error}
          </div>
        )}
        <div
          ref={logsBoxRef}
          className="custom-scrollbar"
          style={{
            padding: 16,
            overflow: "auto",
            fontFamily: "Fira Mono, monospace",
            fontSize: "0.8rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-all",
            minHeight: 120,
            maxHeight: 340,
          }}
        >
          {!isLoading &&
            !error &&
            (logs || (
              <span
                style={{
                  color: "var(--mui-palette-text-secondary)",
                  fontSize: "0.75rem",
                }}
              >
                No logs available.
              </span>
            ))}
        </div>
      </div>
    </FrostedCard>
  );
};

const ServiceInfoRows: React.FC<{ service: Service }> = ({ service }) => {
  const { data: info } = linuxio.dbus.get_unit_info.useQuery(service.name, {
    refetchInterval: 2000,
  });
  const mainPid = Number(info?.MainPID ?? 0);
  const memory = formatBytes(info?.MemoryCurrent);
  const statusColor = getServiceStatusColor(service.active_state);

  return (
    <>
      <DetailRow label="Active">
        <span
          style={{
            fontSize: "0.75rem",
            fontWeight: 500,
            color: statusColor,
          }}
        >
          {service.active_state}
        </span>
      </DetailRow>
      <DetailRow label="Load">
        <span
          style={{
            fontSize: "0.75rem",
            fontWeight: 500,
            color:
              service.load_state === "loaded"
                ? "var(--mui-palette-text-primary)"
                : "var(--mui-palette-text-secondary)",
          }}
        >
          {service.load_state}
        </span>
      </DetailRow>
      {mainPid > 0 && (
        <DetailRow label="PID">
          <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
            {mainPid}
          </span>
        </DetailRow>
      )}
      {memory !== "—" && (
        <DetailRow label="Memory">
          <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>{memory}</span>
        </DetailRow>
      )}
      <ServiceCardActions service={service} info={info} />
    </>
  );
};

const ServiceCardActions: React.FC<{
  service: Service;
  info: UnitInfo | undefined;
}> = ({ service, info }) => {
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

  const isActive = service.active_state === "active";
  const unitFileState = String(
    info?.UnitFileState ?? service.unit_file_state ?? "",
  );
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
    <div
      style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 12 }}
      onClick={(e) => e.stopPropagation()}
    >
      {isActive ? (
        <Tooltip title="Stop the service">
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
        </Tooltip>
      ) : (
        <Tooltip title="Start the service">
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
        </Tooltip>
      )}
      <Tooltip title="Restart the service (stop then start)">
        <span>
          <Button
            size="small"
            variant="outlined"
            startIcon={<RestartAltIcon fontSize="small" />}
            onClick={() => restartService([service.name])}
            disabled={!isActive || anyPending}
          >
            Restart
          </Button>
        </span>
      </Tooltip>
      <Tooltip title="Reload configuration without restarting (if supported)">
        <span>
          <Button
            size="small"
            variant="outlined"
            startIcon={<RefreshIcon fontSize="small" />}
            onClick={() => reloadService([service.name])}
            disabled={!isActive || anyPending}
          >
            Reload
          </Button>
        </span>
      </Tooltip>
      {isEnabled ? (
        <Tooltip title="Disable autostart at boot">
          <span>
            <Button
              size="small"
              variant="outlined"
              startIcon={<BlockIcon fontSize="small" />}
              onClick={() => disableService([service.name])}
              disabled={isMasked || anyPending}
            >
              Disable
            </Button>
          </span>
        </Tooltip>
      ) : (
        <Tooltip title="Enable autostart at boot">
          <span>
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
          </span>
        </Tooltip>
      )}
      {isMasked ? (
        <Tooltip title="Unmask to allow the service to be started">
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
        </Tooltip>
      ) : (
        <Tooltip title="Mask to completely prevent the service from starting">
          <Button
            size="small"
            variant="outlined"
            startIcon={<VisibilityOffIcon fontSize="small" />}
            onClick={() => maskService([service.name])}
            disabled={anyPending}
          >
            Mask
          </Button>
        </Tooltip>
      )}
    </div>
  );
};

const ServiceCardsView: React.FC<ServiceCardsViewProps> = ({
  services,
  expanded,
  onExpand,
  renderDetailPanel,
}) => (
  <UnitCardsView
    items={services}
    expanded={expanded}
    onExpand={onExpand}
    emptyMessage="No services found."
    renderSummaryRows={(service) => <ServiceStatusRows service={service} />}
    renderSelectedRows={(service) => <ServiceInfoRows service={service} />}
    renderDetailPanel={renderDetailPanel}
    renderBottomPanel={(service) => <ServiceLogsCard service={service} />}
  />
);

export default ServiceCardsView;
