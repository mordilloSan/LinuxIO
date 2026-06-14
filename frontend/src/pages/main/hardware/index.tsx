import { Icon } from "@iconify/react";
import React, { useCallback, useMemo } from "react";

import { linuxio } from "@/api";
import HardwareTableCard from "@/components/cards/HardwareTableCard";
import type { SensorGroup } from "@/components/cards/SensorGroupCard";
import SensorGroupCard from "@/components/cards/SensorGroupCard";
import { isPrimarySensorReading } from "@/components/cards/SensorGroupCard";
import { SensorEmptyCard } from "@/components/cards/SensorSummaryCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import {
  BIOSInfoCard,
  CPUDetailsCard,
  CPUHistoryCard,
  DiskIOHistoryCard,
  GPUInfoCard,
  MemoryHistoryCard,
  MotherboardInfoCard,
  NetworkHistoryCard,
} from "@/pages/main/hardware/HardwareHistoryCards";
import { TRANSITION_SLOW_CSS } from "@/theme/constants";
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
      <AppTypography fontWeight={700} variant="subtitle1">
        {title}
      </AppTypography>
      {extras}
    </div>
    <AppIconButton
      className="section-toggle"
      size="small"
      style={{
        opacity: 0,
        transition: "opacity 0.15s",
        pointerEvents: "none",
      }}
    >
      <Icon
        height={24}
        icon="mdi:chevron-down"
        style={{
          transition: `transform ${TRANSITION_SLOW_CSS}`,
          transform: expanded ? "rotate(0deg)" : "rotate(-90deg)",
        }}
        width={24}
      />
    </AppIconButton>
  </div>
);

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
  const memoryRows = memoryModules ?? [];
  const pciRows = pciDevices ?? [];
  const memoryColumns: AppVirtualDataTableColumnDef<
    (typeof memoryRows)[number]
  >[] = [
    {
      accessorKey: "id",
      header: "ID",
      cell: ({ row }) => row.original.id || "—",
    },
    {
      accessorKey: "technology",
      header: "Technology",
      cell: ({ row }) => row.original.technology,
    },
    {
      accessorKey: "type",
      header: "Type",
      cell: ({ row }) => row.original.type,
    },
    {
      accessorKey: "size",
      header: "Size",
      cell: ({ row }) => row.original.size,
    },
    {
      accessorKey: "state",
      header: "State",
      cell: ({ row }) => (
        <Chip
          color={row.original.state === "Present" ? "success" : "default"}
          label={row.original.state}
          size="small"
          style={{ height: 22, fontSize: "0.75rem" }}
          variant="soft"
        />
      ),
    },
    {
      accessorKey: "rank",
      header: "Rank",
      cell: ({ row }) => row.original.rank,
    },
    {
      accessorKey: "speed",
      header: "Speed",
      cell: ({ row }) => row.original.speed,
    },
  ];
  const pciColumns: AppVirtualDataTableColumnDef<(typeof pciRows)[number]>[] = [
    {
      accessorKey: "class",
      header: "Class",
      cell: ({ row }) => row.original.class || "—",
    },
    {
      accessorKey: "model",
      header: "Model",
      cell: ({ row }) => row.original.model || "—",
    },
    {
      accessorKey: "vendor",
      header: "Vendor",
      cell: ({ row }) => row.original.vendor || "—",
    },
    {
      accessorKey: "slot",
      header: "Slot",
      cell: ({ row }) => row.original.slot || "—",
      meta: {
        cellStyle: {
          fontFamily: "monospace",
          fontSize: "0.8rem",
        },
      },
    },
  ];

  return (
    <div>
      {/* ── System Information ──────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.systemInfo}
        onClick={() => toggleSection("systemInfo")}
        title="System Information"
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
        expanded={sections.hardware}
        onClick={() => toggleSection("hardware")}
        title="Hardware"
      />
      <AppCollapse in={sections.hardware}>
        <AppGrid container spacing={4} style={{ marginBottom: 16 }}>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <CPUHistoryCard />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <MemoryHistoryCard />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <DiskIOHistoryCard />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <NetworkHistoryCard />
            </ErrorBoundary>
          </AppGrid>
        </AppGrid>
      </AppCollapse>

      {/* ── Sensor Readings ────────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.sensors}
        extras={
          visibleSensorGroups.length > 0 ? (
            <>
              <Chip
                color="primary"
                label={`${sensorSummary.adapters} Adapter${sensorSummary.adapters !== 1 ? "s" : ""}`}
                size="small"
                variant="soft"
              />
              <Chip
                color="default"
                label={`${sensorSummary.readings} Reading${sensorSummary.readings !== 1 ? "s" : ""}`}
                size="small"
                variant="soft"
              />
            </>
          ) : null
        }
        onClick={() => toggleSection("sensors")}
        title="Sensors"
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
        expanded={sections.memoryModules}
        onClick={() => toggleSection("memoryModules")}
        title="Memory"
      />
      <AppCollapse in={sections.memoryModules}>
        <HardwareTableCard>
          <AppVirtualDataTable
            ariaLabel="Memory modules"
            columns={memoryColumns}
            data={memoryRows}
            emptyMessage="No memory module data available. Ensure dmidecode is installed."
            fillAvailable={false}
            getRowId={(mod, idx) => `${mod.id}-${idx}`}
            maxHeight={280}
            style={{ boxShadow: "none" }}
          />
        </HardwareTableCard>
      </AppCollapse>

      {/* ── PCI Devices ──────────────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.pciDevices}
        onClick={() => toggleSection("pciDevices")}
        title="PCI Devices"
      />
      <AppCollapse in={sections.pciDevices}>
        <HardwareTableCard>
          <AppVirtualDataTable
            ariaLabel="PCI devices"
            columns={pciColumns}
            data={pciRows}
            emptyMessage="No PCI devices found"
            fillAvailable={false}
            getRowId={(dev, idx) => `${dev.slot}-${idx}`}
            maxHeight={420}
            style={{ boxShadow: "none" }}
          />
        </HardwareTableCard>
      </AppCollapse>
    </div>
  );
};

export default HardwarePage;
