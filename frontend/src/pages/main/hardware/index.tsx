import { Icon } from "@iconify/react";
import React, { useCallback, useMemo, useState } from "react";

import { linuxio } from "@/api";
import type { MonitoringRange } from "@/api";
import HardwareTableCard from "@/components/cards/HardwareTableCard";
import type { SensorGroup } from "@/components/cards/SensorGroupCard";
import SensorGroupCard from "@/components/cards/SensorGroupCard";
import { isPrimarySensorReading } from "@/components/cards/SensorGroupCard";
import { SensorEmptyCard } from "@/components/cards/SensorSummaryCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
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
  MemoryHistoryCard,
  NetworkHistoryCard,
  MotherboardInfoCard,
} from "@/pages/main/hardware/HardwareHistoryCards";
import { useAppTheme } from "@/theme";
import "@/theme/section.css";

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

// ─── section header ──────────────────────────────────────────────────────────

const SectionHeader: React.FC<{
  title: string;
  expanded: boolean;
  onClick: () => void;
  extras?: React.ReactNode;
}> = ({ title, expanded, onClick, extras }) => (
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
    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
      <AppTypography variant="subtitle1" fontWeight={700}>
        {title}
      </AppTypography>
      {extras}
    </div>
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

// ─── main component ──────────────────────────────────────────────────────────

const HardwarePage: React.FC = () => {
  const theme = useAppTheme();

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
    const readings = visibleSensorGroups.reduce(
      (sum, group) => sum + group.readings.length,
      0,
    );
    return { adapters: visibleSensorGroups.length, readings };
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

      {/* ── Hardware Cards ──────────────────────────────────────────────── */}
      <SectionHeader
        title="Hardware"
        expanded={sections.hardware}
        onClick={() => toggleSection("hardware")}
      />
      <AppCollapse in={sections.hardware}>
        <div
          style={{
            marginBottom: 12,
            padding: "10px 12px",
            borderRadius: 12,
            border: `1px solid ${theme.palette.divider}`,
            background: theme.palette.background.paper,
          }}
        >
          <AppTypography
            variant="caption"
            style={{ color: theme.palette.text.secondary, lineHeight: 1.55 }}
          >
            Historical charts use PCP directly via libpcp. If these stay empty
            after install, check that the{" "}
            <span
              style={{
                fontFamily: "monospace",
                color: theme.palette.text.primary,
              }}
            >
              pmcd
            </span>{" "}
            and{" "}
            <span
              style={{
                fontFamily: "monospace",
                color: theme.palette.text.primary,
              }}
            >
              pmlogger
            </span>{" "}
            services are running.
          </AppTypography>
        </div>
        <AppGrid container spacing={4} style={{ marginBottom: 16 }}>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <CPUHistoryCard
                range={historyRange}
                onRangeChange={setHistoryRange}
                hoverRatio={historyHoverRatio}
                onHoverChange={setHistoryHoverRatio}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <MemoryHistoryCard
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
        extras={
          visibleSensorGroups.length > 0 ? (
            <>
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
            </>
          ) : null
        }
      />
      <AppCollapse in={sections.sensors}>
        {visibleSensorGroups.length === 0 ? (
          <SensorEmptyCard />
        ) : (
          <>
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

      {/* ── Memory Modules ───────────────────────────────────────────────── */}
      <SectionHeader
        title="Memory"
        expanded={sections.memoryModules}
        onClick={() => toggleSection("memoryModules")}
      />
      <AppCollapse in={sections.memoryModules}>
        <HardwareTableCard>
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
        </HardwareTableCard>
      </AppCollapse>

      {/* ── PCI Devices ──────────────────────────────────────────────────── */}
      <SectionHeader
        title="PCI Devices"
        expanded={sections.pciDevices}
        onClick={() => toggleSection("pciDevices")}
      />
      <AppCollapse in={sections.pciDevices}>
        <HardwareTableCard>
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
        </HardwareTableCard>
      </AppCollapse>
    </div>
  );
};

export default HardwarePage;
