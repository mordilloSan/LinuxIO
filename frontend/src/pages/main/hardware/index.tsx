import { Icon } from "@iconify/react";
import React, { useCallback, useMemo, useState } from "react";

import { linuxio } from "@/api";
import type { MonitoringRange } from "@/api";
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
import {
  BIOSInfoCard,
  CPUDetailsCard,
  CPUHistoryCard,
  DiskIOHistoryCard,
  GPUInfoCard,
  GPUHistoryCard,
  MemoryHistoryCard,
  NetworkHistoryCard,
  MotherboardInfoCard,
} from "@/pages/main/hardware/HardwareHistoryCards";
import "@/theme/section.css";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

// ─── types ───────────────────────────────────────────────────────────────────

interface SensorReading {
  label: string;
  value: number | boolean;
  kind: "number" | "boolean";
  unit: string;
}

interface SensorGroup {
  adapter: string;
  readings: SensorReading[];
}

type NumericSensorReading = SensorReading & {
  kind: "number";
  value: number;
};

type BooleanSensorReading = SensorReading & {
  kind: "boolean";
  value: boolean;
};

// ─── helpers ─────────────────────────────────────────────────────────────────

const getTempColor = (
  value: number,
  palette: { success: string; warning: string; error: string },
): string => {
  if (value < 50) return palette.success;
  if (value < 75) return palette.warning;
  return palette.error;
};

const isNumericSensorReading = (
  reading: SensorReading,
): reading is NumericSensorReading =>
  reading.kind === "number" && typeof reading.value === "number";

const isBooleanSensorReading = (
  reading: SensorReading,
): reading is BooleanSensorReading =>
  reading.kind === "boolean" && typeof reading.value === "boolean";

const isTemperatureReading = (
  reading: SensorReading,
): reading is NumericSensorReading => {
  if (!isNumericSensorReading(reading)) return false;
  const unit = reading.unit.toLowerCase();
  return unit === "c" || unit === "°c";
};

const isFanReading = (
  reading: SensorReading,
): reading is NumericSensorReading =>
  isNumericSensorReading(reading) && reading.unit.toLowerCase() === "rpm";

const isVoltageReading = (
  reading: SensorReading,
): reading is NumericSensorReading =>
  isNumericSensorReading(reading) && reading.unit.toLowerCase() === "v";

const formatNumericSensorValue = (value: number, unit: string): string => {
  const normalizedUnit = unit.toLowerCase();
  if (normalizedUnit === "rpm")
    return value > 0 ? `${Math.round(value)} RPM` : "Off";

  let digits = 2;
  if (normalizedUnit === "c" || normalizedUnit === "°c") digits = 1;
  if (normalizedUnit === "%") digits = 1;
  if (Number.isInteger(value)) digits = 0;

  const formatted = value.toFixed(digits);
  return unit ? `${formatted} ${unit}` : formatted;
};

const formatSensorValue = (reading: SensorReading): string => {
  if (isBooleanSensorReading(reading)) return reading.value ? "True" : "False";
  if (isNumericSensorReading(reading))
    return formatNumericSensorValue(reading.value, reading.unit);
  return String(reading.value);
};

const getSensorLabelMeta = (label: string) => {
  const match = label.match(/^(.*)\(([^()]*)\)\s*$/);
  if (!match) {
    return { baseLabel: label, suffix: null as string | null, context: "" };
  }

  const baseLabel = match[1].trimEnd();
  const parts = match[2]
    .split("/")
    .map((part) => part.trim())
    .filter(Boolean);
  const suffix = parts.length > 0 ? parts[parts.length - 1].toLowerCase() : null;
  const context = parts.slice(0, -1).join(" / ");
  return { baseLabel, suffix, context };
};

const isPrimarySensorReading = (reading: SensorReading): boolean => {
  const { suffix } = getSensorLabelMeta(reading.label);
  return suffix === null || suffix === "input";
};

const getSensorDisplayLabel = (reading: SensorReading): string => {
  const { baseLabel, suffix, context } = getSensorLabelMeta(reading.label);
  if (suffix !== "input") return reading.label;
  if (!context) return baseLabel;
  return `${baseLabel} (${context})`;
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

const sensorChipColor = (
  reading: SensorReading,
): "success" | "warning" | "info" | "default" | "error" => {
  if (isBooleanSensorReading(reading)) {
    if (reading.label.toLowerCase().includes("alarm"))
      return reading.value ? "error" : "success";
    return reading.value ? "warning" : "default";
  }
  return unitChipColor(reading.unit);
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
  const visibleReadings = group.readings.filter(isPrimarySensorReading);
  const temps = visibleReadings.filter(isTemperatureReading);
  const fans = visibleReadings.filter(isFanReading);
  const voltages = visibleReadings.filter(isVoltageReading);
  const other = visibleReadings.filter((r) => {
    if (!isNumericSensorReading(r)) return true;
    const unit = r.unit.toLowerCase();
    return unit !== "c" && unit !== "°c" && unit !== "rpm" && unit !== "v";
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
            {visibleReadings.length} reading
            {visibleReadings.length !== 1 ? "s" : ""}
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
              label={getSensorDisplayLabel(r)}
              percent={Math.min((r.value / 105) * 100, 100)}
              color={getTempColor(r.value, {
                success: theme.palette.success.main,
                warning: theme.palette.warning.main,
                error: theme.palette.error.main,
              })}
              tooltip={`${getSensorDisplayLabel(r)}: ${formatNumericSensorValue(r.value, r.unit)}`}
              rightLabel={formatNumericSensorValue(r.value, r.unit)}
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
                <AppTypography variant="caption">
                  {getSensorDisplayLabel(r)}
                </AppTypography>
              </div>
              <AppTypography
                variant="caption"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {formatNumericSensorValue(r.value, r.unit)}
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
                <AppTypography variant="caption">
                  {getSensorDisplayLabel(r)}
                </AppTypography>
              </div>
              <AppTypography
                variant="caption"
                style={{ fontVariantNumeric: "tabular-nums" }}
              >
                {formatNumericSensorValue(r.value, r.unit)}
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
            <AppTypography variant="caption">
              {getSensorDisplayLabel(r)}
            </AppTypography>
            <Chip
              size="small"
              label={formatSensorValue(r)}
              color={sensorChipColor(r)}
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

const HardwarePage: React.FC = () => {
  // ── data ──
  const { data: sensorGroups } = linuxio.system.get_sensor_info.useQuery({
    refetchInterval: 5000,
  }) as { data: SensorGroup[] | undefined };
  const { data: pciDevices } = linuxio.system.get_pci_devices.useQuery({
    staleTime: 300000,
  });
  const { data: memoryModules } = linuxio.system.get_memory_modules.useQuery({
    staleTime: 300000,
  });

  const visibleSensorGroups = useMemo(
    () =>
      (sensorGroups ?? [])
        .map((group) => ({
          ...group,
          readings: group.readings.filter(isPrimarySensorReading),
        }))
        .filter((group) => group.readings.length > 0),
    [sensorGroups],
  );

  // ── shared history range + hover ──
  const [historyRange, setHistoryRange] = useState<MonitoringRange>("1m");
  const [historyHoverRatio, setHistoryHoverRatio] = useState<number | null>(
    null,
  );

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
    if (visibleSensorGroups.length === 0)
      return { adapters: 0, readings: 0, maxTemp: null as number | null };
    const readings = visibleSensorGroups.reduce(
      (sum, group) => sum + group.readings.length,
      0,
    );
    let maxTemp: number | null = null;
    for (const g of visibleSensorGroups) {
      for (const r of g.readings) {
        if (
          isTemperatureReading(r) &&
          (maxTemp === null || r.value > maxTemp)
        ) {
          maxTemp = r.value;
        }
      }
    }
    return { adapters: visibleSensorGroups.length, readings, maxTemp };
  }, [visibleSensorGroups]);

  return (
    <div>
      {/* ── System Information ──────────────────────────────────────────── */}
      <SectionHeader
        title="System Information"
        expanded={sections.systemInfo}
        onClick={() => toggleSection("systemInfo")}
      />
      <AppCollapse in={sections.systemInfo}>
        <AppGrid container spacing={4} style={{ marginBottom: 16 }}>
          {[
            { id: "motherboard", component: MotherboardInfoCard },
            { id: "cpu-details", component: CPUDetailsCard },
            { id: "bios", component: BIOSInfoCard },
            { id: "gpu-details", component: GPUInfoCard },
          ].map(({ id, component: CardComponent }) => (
            <AppGrid key={id} size={{ xs: 12, md: 6, xl: 3 }}>
              <ErrorBoundary>
                <CardComponent />
              </ErrorBoundary>
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
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 2 }}>
            <ErrorBoundary>
              <CPUHistoryCard
                range={historyRange}
                onRangeChange={setHistoryRange}
                hoverRatio={historyHoverRatio}
                onHoverChange={setHistoryHoverRatio}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 2 }}>
            <ErrorBoundary>
              <MemoryHistoryCard
                range={historyRange}
                onRangeChange={setHistoryRange}
                hoverRatio={historyHoverRatio}
                onHoverChange={setHistoryHoverRatio}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 2 }}>
            <ErrorBoundary>
              <GPUHistoryCard
                range={historyRange}
                onRangeChange={setHistoryRange}
                hoverRatio={historyHoverRatio}
                onHoverChange={setHistoryHoverRatio}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <DiskIOHistoryCard
                range={historyRange}
                onRangeChange={setHistoryRange}
                hoverRatio={historyHoverRatio}
                onHoverChange={setHistoryHoverRatio}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <NetworkHistoryCard
                range={historyRange}
                onRangeChange={setHistoryRange}
                hoverRatio={historyHoverRatio}
                onHoverChange={setHistoryHoverRatio}
              />
            </ErrorBoundary>
          </AppGrid>
        </AppGrid>
      </AppCollapse>

      {/* ── Sensor Readings ────────────────────────────────────────────── */}
      <SectionHeader
        title="Sensors"
        expanded={sections.sensors}
        onClick={() => toggleSection("sensors")}
      />
      <AppCollapse in={sections.sensors}>
        {visibleSensorGroups.length === 0 ? (
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
              {visibleSensorGroups.map((group, idx) => (
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
