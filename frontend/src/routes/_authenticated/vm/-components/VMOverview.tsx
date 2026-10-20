import { Icon } from "@iconify/react";
import { useState, type CSSProperties, type ReactNode } from "react";

import type {
  MonitoringLive,
  NetworkInterface,
  VMPreflight,
  VMStoragePool,
  VirtualMachine,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppAlert, { AppAlertTitle } from "@/components/ui/AppAlert";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { filterInterfaces } from "@/routes/_authenticated/-dashboard/Network";
import { DASHBOARD_CARD_GAP } from "@/theme/constants";
import { formatFileSize } from "@/utils/formaters";

import PreflightSummary from "./PreflightSummary";
import { PillToggle, STATUS_COLOR, Throughput, Tile } from "./vmCards";
import { normalizeState } from "./vmShared";

const FILTERS = [
  { label: "All", value: "all" },
  { label: "Host", value: "host" },
  { label: "Virtual machines", value: "vm" },
  { label: "Storage", value: "storage" },
] as const;
type Filter = (typeof FILTERS)[number]["value"];

const pageStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-16)",
  minHeight: 0,
};

const bannerStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  flexWrap: "wrap",
  gap: "var(--app-space-16)",
  padding: "var(--app-space-16)",
};

const tileGridStyle: CSSProperties = {
  display: "grid",
  gap: DASHBOARD_CARD_GAP,
  gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))",
};

const rowStyle: CSSProperties = {
  alignItems: "center",
  display: "grid",
  gap: "var(--app-space-16)",
  gridTemplateColumns: "minmax(160px, 1fr) repeat(3, minmax(140px, 1fr))",
  padding: "var(--app-space-12) var(--app-space-16)",
};

const rowIdentityStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  gap: "var(--app-space-12)",
  minWidth: 0,
};

const metricStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 4,
  minWidth: 0,
};

const metricHeadStyle: CSSProperties = {
  display: "flex",
  justifyContent: "space-between",
  gap: "var(--app-space-8)",
};

const messageListStyle: CSSProperties = { margin: 0, paddingLeft: 18 };

function healthOf(preflight?: VMPreflight) {
  if (!preflight)
    return {
      color: STATUS_COLOR.muted,
      icon: "mdi:shield-outline",
      text: "Checking the virtualization environment.",
      title: "Checking",
    };
  if ((preflight.errors ?? []).length > 0)
    return {
      color: STATUS_COLOR.error,
      icon: "mdi:shield-alert",
      text: "Your virtualization environment has errors that block creating VMs.",
      title: "Error",
    };
  if ((preflight.warnings ?? []).length > 0)
    return {
      color: STATUS_COLOR.warning,
      icon: "mdi:shield-alert-outline",
      text: "Your virtualization environment works, with warnings.",
      title: "Warning",
    };
  return {
    color: STATUS_COLOR.ok,
    icon: "mdi:shield-check",
    text: "Your virtualization environment is working normally.",
    title: "Normal",
  };
}

/** A label, a value and a bar; the bar is hidden when there is no live data. */
function Metric({
  label,
  muted,
  percent,
  value,
}: {
  label: string;
  muted?: boolean;
  percent?: number;
  value: ReactNode;
}) {
  return (
    <div style={{ ...metricStyle, opacity: muted ? 0.6 : 1 }}>
      <div style={metricHeadStyle}>
        <AppTypography color="text.secondary" noWrap variant="body2">
          {label}
        </AppTypography>
        <AppTypography noWrap variant="body2">
          {value}
        </AppTypography>
      </div>
      {percent !== undefined && (
        <AppLinearProgress
          aria-label={label}
          value={Math.min(percent, 100)}
          variant="determinate"
        />
      )}
    </div>
  );
}

function Row({
  children,
  icon,
  muted,
  name,
  status,
  statusColor,
}: {
  children: ReactNode;
  icon: string;
  muted?: boolean;
  name: string;
  status: string;
  statusColor: string;
}) {
  return (
    <FrostedCard
      aria-label={name}
      role="listitem"
      style={{ ...rowStyle, opacity: muted ? 0.7 : 1 }}
    >
      <div style={rowIdentityStyle}>
        <Icon
          color={muted ? STATUS_COLOR.muted : "var(--app-palette-primary-main)"}
          height={28}
          icon={icon}
          width={28}
        />
        <div style={{ minWidth: 0 }}>
          <AppTypography fontWeight={600} noWrap variant="body2">
            {name}
          </AppTypography>
          <AppTypography
            noWrap
            style={{ color: statusColor }}
            variant="caption"
          >
            {status}
          </AppTypography>
        </div>
      </div>
      {children}
    </FrostedCard>
  );
}

const percentText = (value: number | undefined) =>
  value === undefined ? "-" : `${value.toFixed(2)}%`;

export default function VMOverview({
  hostname,
  interfaces,
  live,
  pools,
  preflight,
  vms,
}: {
  hostname: string;
  interfaces: NetworkInterface[];
  live: MonitoringLive;
  pools: VMStoragePool[];
  preflight?: VMPreflight;
  vms: VirtualMachine[];
}) {
  const [filter, setFilter] = useState<Filter>("all");
  const health = healthOf(preflight);
  const running = vms.filter((vm) => vm.state === "running").length;
  const activePools = pools.filter((pool) => pool.active).length;
  const uplink =
    filterInterfaces(interfaces).find((iface) => iface.operstate === "up") ??
    filterInterfaces(interfaces)[0];
  const uplinkRates = uplink ? live.interfaces?.[uplink.name] : undefined;
  const ramPercent =
    live.memory.total_bytes > 0
      ? (live.memory.used_bytes / live.memory.total_bytes) * 100
      : 0;
  const show = (kind: Filter) => filter === "all" || filter === kind;

  return (
    <div style={pageStyle}>
      <FrostedCard style={bannerStyle}>
        <Icon color={health.color} height={56} icon={health.icon} width={56} />
        <div style={{ flex: "1 1 240px", minWidth: 0 }}>
          <AppTypography
            component="h2"
            fontWeight={600}
            style={{ color: health.color }}
            variant="h5"
          >
            {health.title}
          </AppTypography>
          <AppTypography variant="body2">{health.text}</AppTypography>
        </div>
        <PreflightSummary preflight={preflight} />
      </FrostedCard>
      {preflight && (preflight.errors ?? []).length > 0 && (
        <AppAlert severity="error">
          <AppAlertTitle>Preflight Errors</AppAlertTitle>
          <ul style={messageListStyle}>
            {(preflight.errors ?? []).map((error) => (
              <li key={error}>{error}</li>
            ))}
          </ul>
        </AppAlert>
      )}
      {preflight && (preflight.warnings ?? []).length > 0 && (
        <AppAlert severity="warning">
          <AppAlertTitle>Preflight Warnings</AppAlertTitle>
          <ul style={messageListStyle}>
            {(preflight.warnings ?? []).map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        </AppAlert>
      )}

      <div style={tileGridStyle}>
        <Tile
          count={1}
          icon="mdi:server"
          label="Host"
          status="Normal"
          statusColor={STATUS_COLOR.ok}
        />
        <Tile
          count={running}
          icon="mdi:monitor-multiple"
          label="Virtual machines"
          status={`Running of ${vms.length}`}
          statusColor={STATUS_COLOR.ok}
        />
        <Tile
          count={pools.length}
          icon="mdi:database"
          label="Storage"
          status={
            activePools === pools.length
              ? "Normal"
              : `${pools.length - activePools} inactive`
          }
          statusColor={
            activePools === pools.length
              ? STATUS_COLOR.ok
              : STATUS_COLOR.warning
          }
        />
      </div>

      <PillToggle
        ariaLabel="Show"
        onChange={setFilter}
        options={FILTERS}
        value={filter}
      />

      <div
        aria-label="Virtualization overview"
        role="list"
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-8)",
        }}
      >
        {show("host") && (
          <Row
            icon="mdi:server"
            name={hostname}
            status="Running"
            statusColor={STATUS_COLOR.ok}
          >
            <Metric
              label="CPU"
              percent={live.cpu.percent}
              value={percentText(live.cpu.percent)}
            />
            <Metric
              label="RAM"
              percent={ramPercent}
              value={percentText(ramPercent)}
            />
            <Metric
              label={uplink?.name ?? "Network"}
              value={
                <Throughput
                  rx={uplinkRates?.rx_bytes_per_sec}
                  tx={uplinkRates?.tx_bytes_per_sec}
                />
              }
            />
          </Row>
        )}
        {show("vm") &&
          vms.map((vm) => {
            const off = vm.state !== "running";
            const memoryPercent =
              vm.metrics && vm.memoryMB
                ? (vm.metrics.memoryUsedMB / vm.memoryMB) * 100
                : undefined;
            return (
              <Row
                icon="mdi:monitor"
                key={vm.name}
                muted={off}
                name={vm.name}
                status={normalizeState(vm.state)}
                statusColor={off ? STATUS_COLOR.muted : STATUS_COLOR.ok}
              >
                <Metric
                  label="CPU"
                  muted={off}
                  percent={vm.metrics?.cpuPercent ?? 0}
                  value={percentText(vm.metrics?.cpuPercent)}
                />
                <Metric
                  label="RAM"
                  muted={off}
                  percent={memoryPercent ?? 0}
                  value={percentText(memoryPercent)}
                />
                <Metric
                  label="Network"
                  muted={off}
                  value={
                    <Throughput
                      rx={vm.metrics?.networkReceiveBytesPerSecond}
                      tx={vm.metrics?.networkSendBytesPerSecond}
                    />
                  }
                />
              </Row>
            );
          })}
        {show("storage") &&
          pools.map((pool) => {
            const used =
              pool.capacityBytes > 0
                ? (pool.allocationBytes / pool.capacityBytes) * 100
                : 0;
            return (
              <Row
                icon="mdi:database"
                key={pool.name}
                muted={!pool.active}
                name={`${hostname} ${pool.name}`}
                status={pool.active ? "Running" : "Inactive"}
                statusColor={pool.active ? STATUS_COLOR.ok : STATUS_COLOR.muted}
              >
                <div style={{ gridColumn: "2 / -1" }}>
                  <Metric
                    label={`${formatFileSize(pool.allocationBytes, 2)} / ${formatFileSize(pool.capacityBytes, 2)}`}
                    muted={!pool.active}
                    percent={used}
                    value={percentText(used)}
                  />
                </div>
              </Row>
            );
          })}
      </div>
    </div>
  );
}
