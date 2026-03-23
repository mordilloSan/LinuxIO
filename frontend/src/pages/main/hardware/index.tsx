import { Icon } from "@iconify/react";
import React, { useCallback, useMemo } from "react";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import MetricBar from "@/components/gauge/MetricBar";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import "@/theme/section.css";
import GpuInfo from "@/pages/main/dashboard/Gpu";
import MemoryUsage from "@/pages/main/dashboard/Memory";
import Processor from "@/pages/main/dashboard/Processor";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

// ─── types ───────────────────────────────────────────────────────────────────

interface SensorReading {
  label: string;
  value: number;
  unit: string;
}

interface SensorGroup {
  adapter: string;
  readings: SensorReading[];
}

// ─── helpers ─────────────────────────────────────────────────────────────────

const formatUptime = (seconds: number): string => {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const parts: string[] = [];
  if (days > 0) parts.push(`${days}d`);
  if (hours > 0) parts.push(`${hours}h`);
  if (minutes > 0) parts.push(`${minutes}m`);
  return parts.join(" ") || "0m";
};

const getTempColor = (
  value: number,
  palette: { success: string; warning: string; error: string },
): string => {
  if (value < 50) return palette.success;
  if (value < 75) return palette.warning;
  return palette.error;
};

const unitChipColor = (
  unit: string,
): "success" | "warning" | "info" | "default" => {
  const u = unit.toLowerCase();
  if (u === "c" || u === "°c") return "warning";
  if (u === "rpm") return "info";
  if (u === "v") return "success";
  return "default";
};

const SectionHeader: React.FC<{
  title: string;
  expanded: boolean;
  onClick: () => void;
}> = ({ title, expanded, onClick }) => (
  <div
    className="dd-section-header"
    onClick={onClick}
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
      {title}
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
          transform: expanded ? "rotate(0deg)" : "rotate(-90deg)",
        }}
      />
    </AppIconButton>
  </div>
);

// ─── sensor card ─────────────────────────────────────────────────────────────

const SensorGroupCard: React.FC<{ group: SensorGroup }> = ({ group }) => {
  const theme = useAppTheme();
  const temps = group.readings.filter((r) => {
    const u = r.unit.toLowerCase();
    return u === "c" || u === "°c";
  });
  const fans = group.readings.filter((r) => r.unit.toLowerCase() === "rpm");
  const voltages = group.readings.filter((r) => r.unit.toLowerCase() === "v");
  const other = group.readings.filter((r) => {
    const u = r.unit.toLowerCase();
    return u !== "c" && u !== "°c" && u !== "rpm" && u !== "v";
  });

  return (
    <FrostedCard style={{ padding: 10, height: "100%" }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          marginBottom: 8,
        }}
      >
        <div
          style={{
            width: 36,
            height: 36,
            borderRadius: 8,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            flexShrink: 0,
          }}
        >
          <Icon
            icon="mdi:chip"
            width={24}
            height={24}
            color={theme.palette.primary.main}
          />
        </div>
        <div style={{ minWidth: 0 }}>
          <AppTypography
            variant="subtitle2"
            fontWeight={700}
            style={{ lineHeight: 1.2 }}
            noWrap
          >
            {group.adapter}
          </AppTypography>
          <AppTypography variant="caption" color="text.secondary">
            {group.readings.length} reading
            {group.readings.length !== 1 ? "s" : ""}
          </AppTypography>
        </div>
      </div>

      {/* Temperatures */}
      {temps.length > 0 && (
        <div
          style={{
            marginBottom:
              temps.length > 0 && (fans.length > 0 || voltages.length > 0)
                ? 8
                : 0,
          }}
        >
          {temps.map((r, i) => (
            <MetricBar
              key={`temp-${i}`}
              label={r.label}
              percent={Math.min((r.value / 105) * 100, 100)}
              color={getTempColor(r.value, {
                success: theme.palette.success.main,
                warning: theme.palette.warning.main,
                error: theme.palette.error.main,
              })}
              tooltip={`${r.label}: ${r.value}°C`}
              rightLabel={`${r.value}°C`}
            />
          ))}
        </div>
      )}

      {/* Fan speeds */}
      {fans.length > 0 && (
        <div
          style={{
            marginBottom: voltages.length > 0 || other.length > 0 ? 8 : 0,
          }}
        >
          {fans.map((r, i) => (
            <div
              key={`fan-${i}`}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                paddingBlock: 2,
                paddingInline: 2,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                <Icon
                  icon="mdi:fan"
                  width={14}
                  height={14}
                  color={
                    r.value > 0
                      ? theme.palette.info.main
                      : alpha(theme.palette.text.secondary, 0.4)
                  }
                />
                <AppTypography variant="caption">{r.label}</AppTypography>
              </div>
              <AppTypography
                variant="caption"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {r.value > 0 ? `${r.value} RPM` : "Off"}
              </AppTypography>
            </div>
          ))}
        </div>
      )}

      {/* Voltages */}
      {voltages.length > 0 && (
        <div style={{ marginBottom: other.length > 0 ? 8 : 0 }}>
          {voltages.map((r, i) => (
            <div
              key={`volt-${i}`}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                paddingBlock: 2,
                paddingInline: 2,
              }}
            >
              <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
                <Icon
                  icon="mdi:flash"
                  width={14}
                  height={14}
                  color={theme.palette.success.main}
                />
                <AppTypography variant="caption">{r.label}</AppTypography>
              </div>
              <AppTypography
                variant="caption"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {r.value.toFixed(2)} V
              </AppTypography>
            </div>
          ))}
        </div>
      )}

      {/* Other readings */}
      {other.length > 0 &&
        other.map((r, i) => (
          <div
            key={`other-${i}`}
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              paddingBlock: 2,
              paddingInline: 2,
            }}
          >
            <AppTypography variant="caption">{r.label}</AppTypography>
            <Chip
              size="small"
              label={`${r.value} ${r.unit}`}
              color={unitChipColor(r.unit)}
              variant="soft"
              style={{ height: 20, fontSize: "0.65rem" }}
            />
          </div>
        ))}
    </FrostedCard>
  );
};

// ─── constants ──────────────────────────────────────────────────────────────

const defaultHwSections = {
  overview: true,
  hardware: true,
  sensors: true,
  systemInfo: true,
  gpu: true,
  pciDevices: true,
  memoryModules: true,
};

const memoryColumns: UnifiedTableColumn[] = [
  { field: "id", headerName: "ID" },
  { field: "technology", headerName: "Technology" },
  { field: "type", headerName: "Type" },
  { field: "size", headerName: "Size" },
  { field: "state", headerName: "State" },
  { field: "rank", headerName: "Rank" },
  { field: "speed", headerName: "Speed" },
];

const pciColumns: UnifiedTableColumn[] = [
  { field: "class", headerName: "Class" },
  { field: "model", headerName: "Model" },
  { field: "vendor", headerName: "Vendor" },
  { field: "slot", headerName: "Slot" },
];

// ─── main component ──────────────────────────────────────────────────────────

const MemoProcessor = React.memo(Processor);
const MemoMemory = React.memo(MemoryUsage);
const MemoGpuInfo = React.memo(GpuInfo);

const HardwarePage: React.FC = () => {
  const theme = useAppTheme();

  // ── data ──
  const { data: hostInfo } = linuxio.system.get_host_info.useQuery({
    refetchInterval: 60000,
  });
  const { data: uptime } = linuxio.system.get_uptime.useQuery({
    refetchInterval: 10000,
  });
  const { data: sensorGroups } = linuxio.system.get_sensor_info.useQuery({
    refetchInterval: 5000,
  }) as { data: SensorGroup[] | undefined };
  const { data: systemInfo } = linuxio.system.get_system_info.useQuery({
    staleTime: 300000,
  });
  const { data: pciDevices } = linuxio.system.get_pci_devices.useQuery({
    staleTime: 300000,
  });
  const { data: memoryModules } = linuxio.system.get_memory_modules.useQuery({
    staleTime: 300000,
  });

  // ── section collapse state ──
  const [hwSections, setHwSections] = useConfigValue("hardwareSections");
  const sections = { ...defaultHwSections, ...(hwSections ?? {}) };
  const toggleSection = useCallback(
    (
      key:
        | "overview"
        | "hardware"
        | "gpu"
        | "sensors"
        | "systemInfo"
        | "pciDevices"
        | "memoryModules",
    ) =>
      setHwSections((prev) => {
        const cur = { ...defaultHwSections, ...(prev ?? {}) };
        return { ...cur, [key]: !cur[key] };
      }),
    [setHwSections],
  );

  // ── sensor summary ──
  const sensorSummary = useMemo(() => {
    if (!sensorGroups)
      return { adapters: 0, readings: 0, maxTemp: null as number | null };
    const readings = sensorGroups.reduce((s, g) => s + g.readings.length, 0);
    let maxTemp: number | null = null;
    for (const g of sensorGroups) {
      for (const r of g.readings) {
        const u = r.unit.toLowerCase();
        if (
          (u === "c" || u === "°c") &&
          (maxTemp === null || r.value > maxTemp)
        ) {
          maxTemp = r.value;
        }
      }
    }
    return { adapters: sensorGroups.length, readings, maxTemp };
  }, [sensorGroups]);

  return (
    <div style={{ padding: theme.spacing(1) }}>
      {/* ── System Overview ─────────────────────────────────────────────── */}
      <SectionHeader
        title="System Overview"
        expanded={sections.overview}
        onClick={() => toggleSection("overview")}
      />
      <AppCollapse in={sections.overview}>
        <AppGrid container spacing={2} style={{ marginBottom: 16 }}>
          {(
            [
              {
                label: "Hostname",
                value: hostInfo?.hostname ?? "—",
                detail: hostInfo?.os ?? "",
                icon: "mdi:server",
              },
              {
                label: "Platform",
                value: hostInfo?.platform ?? "—",
                detail: hostInfo?.platformVersion ?? "",
                icon: "mdi:linux",
              },
              {
                label: "Kernel",
                value: hostInfo?.kernelVersion ?? "—",
                detail: hostInfo?.kernelArch ?? "",
                icon: "mdi:cog",
              },
              {
                label: "Uptime",
                value: uptime != null ? formatUptime(uptime) : "—",
                detail:
                  sensorSummary.maxTemp != null
                    ? `Peak: ${sensorSummary.maxTemp}°C`
                    : `${sensorSummary.adapters} sensor adapters`,
                icon: "mdi:clock-outline",
              },
            ] as {
              label: string;
              value: string;
              detail: string;
              icon: string;
            }[]
          ).map(({ label, value, detail, icon }) => (
            <AppGrid key={label} size={{ xs: 6, md: 3 }}>
              <FrostedCard
                style={{
                  paddingInline: 10,
                  paddingBlock: 8,
                }}
              >
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                    marginBottom: 4,
                  }}
                >
                  <Icon
                    icon={icon}
                    width={18}
                    height={18}
                    color={theme.palette.primary.main}
                  />
                  <AppTypography
                    variant="overline"
                    color="text.secondary"
                    style={{ lineHeight: 1.6 }}
                  >
                    {label}
                  </AppTypography>
                </div>
                <div
                  style={{
                    display: "flex",
                    alignItems: "baseline",
                    justifyContent: "space-between",
                    marginTop: 1,
                  }}
                >
                  <AppTypography
                    variant="h6"
                    fontWeight={700}
                    noWrap
                    style={{ lineHeight: 1.2 }}
                    fontSize="0.95rem"
                  >
                    {value}
                  </AppTypography>
                  <AppTypography
                    variant="caption"
                    color="text.secondary"
                    noWrap
                    align="right"
                  >
                    {detail}
                  </AppTypography>
                </div>
              </FrostedCard>
            </AppGrid>
          ))}
        </AppGrid>
      </AppCollapse>

      {/* ── System Information ──────────────────────────────────────────── */}
      <SectionHeader
        title="System Information"
        expanded={sections.systemInfo}
        onClick={() => toggleSection("systemInfo")}
      />
      <AppCollapse in={sections.systemInfo}>
        <AppGrid container spacing={2} style={{ marginBottom: 16 }}>
          {(
            [
              { label: "Type", value: systemInfo?.chassisType },
              { label: "Name", value: systemInfo?.productName },
              { label: "Version", value: systemInfo?.productVersion },
              { label: "Vendor", value: systemInfo?.productVendor },
              { label: "BIOS", value: systemInfo?.biosVendor },
              { label: "BIOS Version", value: systemInfo?.biosVersion },
              { label: "BIOS Date", value: systemInfo?.biosDate },
              { label: "CPU", value: systemInfo?.cpuSummary },
            ] as { label: string; value: string | undefined }[]
          ).map(({ label, value }) => (
            <AppGrid key={label} size={{ xs: 6, md: 3 }}>
              <FrostedCard style={{ paddingInline: 10, paddingBlock: 8 }}>
                <AppTypography
                  variant="overline"
                  color="text.secondary"
                  style={{ lineHeight: 1.6 }}
                >
                  {label}
                </AppTypography>
                <AppTypography
                  variant="body2"
                  fontWeight={600}
                  noWrap
                  style={{ lineHeight: 1.3 }}
                >
                  {value || "—"}
                </AppTypography>
              </FrostedCard>
            </AppGrid>
          ))}
        </AppGrid>
      </AppCollapse>

      {/* ── Memory Modules ───────────────────────────────────────────────── */}
      <SectionHeader
        title="Memory"
        expanded={sections.memoryModules}
        onClick={() => toggleSection("memoryModules")}
      />
      <AppCollapse in={sections.memoryModules}>
        <FrostedCard
          style={{ padding: 0, marginBottom: 16, overflow: "hidden" }}
        >
          <UnifiedCollapsibleTable
            data={memoryModules ?? []}
            columns={memoryColumns}
            getRowKey={(mod, idx) => `${mod.id}-${idx}`}
            renderMainRow={(mod) => (
              <>
                <AppTableCell>{mod.id || "—"}</AppTableCell>
                <AppTableCell>{mod.technology}</AppTableCell>
                <AppTableCell>{mod.type}</AppTableCell>
                <AppTableCell>{mod.size}</AppTableCell>
                <AppTableCell>
                  <Chip
                    size="small"
                    label={mod.state}
                    color={mod.state === "Present" ? "success" : "default"}
                    variant="soft"
                    style={{ height: 22, fontSize: "0.75rem" }}
                  />
                </AppTableCell>
                <AppTableCell>{mod.rank}</AppTableCell>
                <AppTableCell>{mod.speed}</AppTableCell>
              </>
            )}
            emptyMessage="No memory module data available. Ensure dmidecode is installed."
          />
        </FrostedCard>
      </AppCollapse>

      {/* ── Hardware Cards ──────────────────────────────────────────────── */}
      <SectionHeader
        title="Hardware"
        expanded={sections.hardware}
        onClick={() => toggleSection("hardware")}
      />
      <AppCollapse in={sections.hardware}>
        <AppGrid container spacing={4} style={{ marginBottom: 16 }}>
          {[
            { id: "cpu", component: MemoProcessor },
            { id: "memory", component: MemoMemory },
            { id: "gpu", component: MemoGpuInfo },
          ].map(({ id, component: CardComponent }) => (
            <AppGrid key={id} size={{ xs: 12, lg: 4 }}>
              <ErrorBoundary>
                <CardComponent />
              </ErrorBoundary>
            </AppGrid>
          ))}
        </AppGrid>
      </AppCollapse>

      {/* ── Sensor Readings ────────────────────────────────────────────── */}
      <SectionHeader
        title="Sensors"
        expanded={sections.sensors}
        onClick={() => toggleSection("sensors")}
      />
      <AppCollapse in={sections.sensors}>
        {!sensorGroups || sensorGroups.length === 0 ? (
          <FrostedCard style={{ padding: 16, textAlign: "center" }}>
            <AppTypography variant="body2" color="text.secondary">
              No sensor data available. Ensure <code>lm-sensors</code> is
              installed and configured.
            </AppTypography>
          </FrostedCard>
        ) : (
          <>
            {/* Summary bar */}
            <AppGrid container spacing={2} style={{ marginBottom: 16 }}>
              <AppGrid size={{ xs: 12 }}>
                <FrostedCard
                  style={{
                    paddingInline: 12,
                    paddingBlock: 8,
                    display: "flex",
                    alignItems: "center",
                    gap: 12,
                    flexWrap: "wrap",
                  }}
                >
                  <Chip
                    size="small"
                    label={`${sensorSummary.adapters} Adapter${sensorSummary.adapters !== 1 ? "s" : ""}`}
                    color="primary"
                    variant="soft"
                  />
                  <Chip
                    size="small"
                    label={`${sensorSummary.readings} Reading${sensorSummary.readings !== 1 ? "s" : ""}`}
                    color="default"
                    variant="soft"
                  />
                  {sensorSummary.maxTemp != null && (
                    <Chip
                      size="small"
                      label={`Peak Temp: ${sensorSummary.maxTemp}°C`}
                      color={
                        sensorSummary.maxTemp >= 75
                          ? "error"
                          : sensorSummary.maxTemp >= 50
                            ? "warning"
                            : "success"
                      }
                      variant="soft"
                    />
                  )}
                </FrostedCard>
              </AppGrid>
            </AppGrid>

            {/* Sensor group cards */}
            <AppGrid container spacing={2} style={{ marginBottom: 16 }}>
              {sensorGroups.map((group, idx) => (
                <AppGrid
                  key={`${group.adapter}-${idx}`}
                  size={{ xs: 12, sm: 6, lg: 4 }}
                >
                  <SensorGroupCard group={group} />
                </AppGrid>
              ))}
            </AppGrid>
          </>
        )}
      </AppCollapse>

      {/* ── PCI Devices ──────────────────────────────────────────────────── */}
      <SectionHeader
        title="PCI Devices"
        expanded={sections.pciDevices}
        onClick={() => toggleSection("pciDevices")}
      />
      <AppCollapse in={sections.pciDevices}>
        <FrostedCard
          style={{ padding: 0, marginBottom: 16, overflow: "hidden" }}
        >
          <UnifiedCollapsibleTable
            data={pciDevices ?? []}
            columns={pciColumns}
            getRowKey={(dev, idx) => `${dev.slot}-${idx}`}
            renderMainRow={(dev) => (
              <>
                <AppTableCell>{dev.class || "—"}</AppTableCell>
                <AppTableCell>{dev.model || "—"}</AppTableCell>
                <AppTableCell>{dev.vendor || "—"}</AppTableCell>
                <AppTableCell
                  style={{ fontFamily: "monospace", fontSize: "0.8rem" }}
                >
                  {dev.slot || "—"}
                </AppTableCell>
              </>
            )}
            emptyMessage="No PCI devices found"
          />
        </FrostedCard>
      </AppCollapse>
    </div>
  );
};

export default HardwarePage;
