import { useSuspenseQuery } from "@tanstack/react-query";
import { Suspense, useCallback, useState } from "react";

import { linuxio, type SensorGroup } from "@/api";
import HardwareTableCard from "@/components/cards/HardwareTableCard";
import SensorGroupCard from "@/components/cards/SensorGroupCard";
import { isPrimarySensorReading } from "@/components/cards/sensorGroupHelpers";
import { SensorEmptyCard } from "@/components/cards/SensorSummaryCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import WidgetLoader from "@/components/loaders/WidgetLoader";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppGrid from "@/components/ui/AppGrid";
import SectionHeader from "@/components/ui/SectionHeader";
import { useConfigValue } from "@/hooks/useConfig";
import { cardHeight } from "@/theme/constants";

import {
  BIOSInfoCard,
  CPUDetailsCard,
  CPUHistoryCard,
  DiskIOHistoryCard,
  GPUInfoCard,
  HistoryHoverProvider,
  MemoryHistoryCard,
  MotherboardInfoCard,
  NetworkHistoryCard,
} from "./HardwareHistoryCards";
import type { HardwareHistoryRangeId } from "./hardwareHistoryRanges";
import {
  hardwareSensorQueryOptions,
  hardwareStableQueryOptions,
} from "./hardwareQueryOptions";
import {
  defaultHardwareSections,
  resolvedHardwareSections,
} from "./hardwareSections";

export const selectVisibleSensorGroupIdentities = (groups: SensorGroup[]) =>
  groups.flatMap((group, sourceIndex) => {
    const visibleReadingCount = group.readings.filter(
      isPrimarySensorReading,
    ).length;

    // Retain the raw index so filtering an empty adapter cannot make a live
    // card observe a different source group.
    return visibleReadingCount > 0
      ? [{ adapter: group.adapter, sourceIndex, visibleReadingCount }]
      : [];
  });

function SensorReadings() {
  const { data: visibleSensorGroups } = useSuspenseQuery(
    linuxio.system.get_sensor_info.queryOptions({
      ...hardwareSensorQueryOptions,
      refetchInterval: 5_000,
      select: selectVisibleSensorGroupIdentities,
    }),
  );
  const sensorSummary = {
    adapters: visibleSensorGroups.length,
    readings: visibleSensorGroups.reduce(
      (sum, group) => sum + group.visibleReadingCount,
      0,
    ),
  };

  if (visibleSensorGroups.length === 0) return <SensorEmptyCard />;

  return (
    <>
      <div style={{ display: "flex", gap: 8, marginBottom: 8 }}>
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
      </div>
      <AppGrid
        alignItems="stretch"
        container
        spacing={2}
        style={{ marginBottom: 16 }}
      >
        {visibleSensorGroups.map(
          ({ adapter, sourceIndex, visibleReadingCount }) => (
            <AppGrid
              key={`${adapter}-${sourceIndex}`}
              size={{ xs: 12, sm: 6, lg: 4, xl: 3 }}
            >
              <SensorGroupCard
                adapter={adapter}
                sourceIndex={sourceIndex}
                visibleReadingCount={visibleReadingCount}
              />
            </AppGrid>
          ),
        )}
      </AppGrid>
    </>
  );
}

function MemoryModulesTable() {
  const { data: memoryModules } = useSuspenseQuery(
    linuxio.system.get_memory_modules.queryOptions(hardwareStableQueryOptions),
  );
  const memoryColumns: AppDataTableColumnDef<(typeof memoryModules)[number]>[] =
    [
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

  return (
    <HardwareTableCard>
      <AppDataTable
        ariaLabel="Memory modules"
        columns={memoryColumns}
        data={memoryModules}
        emptyMessage="No memory module data available. Ensure dmidecode is installed."
        fillAvailable={false}
        getRowId={(module, index) => `${module.id}-${index}`}
        maxHeight={280}
        style={{ boxShadow: "none" }}
      />
    </HardwareTableCard>
  );
}

function PciDevicesTable() {
  const { data: pciDevices } = useSuspenseQuery(
    linuxio.system.get_pci_devices.queryOptions(hardwareStableQueryOptions),
  );
  const pciColumns: AppDataTableColumnDef<(typeof pciDevices)[number]>[] = [
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
    <HardwareTableCard>
      <AppDataTable
        ariaLabel="PCI devices"
        columns={pciColumns}
        data={pciDevices}
        emptyMessage="No PCI devices found"
        fillAvailable={false}
        getRowId={(device, index) => `${device.slot}-${index}`}
        maxHeight={420}
        style={{ boxShadow: "none" }}
      />
    </HardwareTableCard>
  );
}

// ─── main component ──────────────────────────────────────────────────────────

const HardwarePage = () => {
  // ── history range & synchronized crosshair ──
  const [historyRange, setHistoryRange] =
    useState<HardwareHistoryRangeId>("1h");

  // ── section collapse state ──
  const [hwSections, setHwSections] = useConfigValue("hardwareSections");
  const sections = resolvedHardwareSections(hwSections);
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
        const cur = { ...defaultHardwareSections, ...(prev ?? {}) };
        return { ...cur, [key]: !cur[key] };
      }),
    [setHwSections],
  );

  return (
    <div>
      {/* ── System Information ──────────────────────────────────────────── */}
      <SectionHeader
        controlsId="hardware-system-info-panel"
        expanded={sections.systemInfo}
        onToggle={() => toggleSection("systemInfo")}
        title="System Information"
      />
      <div id="hardware-system-info-panel">
        <AppCollapse in={sections.systemInfo} unmountOnExit>
          <AppGrid
            alignItems="stretch"
            container
            spacing={4}
            style={{ marginBottom: 16 }}
          >
            {[
              { id: "motherboard", component: MotherboardInfoCard },
              { id: "cpu-details", component: CPUDetailsCard },
              { id: "bios", component: BIOSInfoCard },
              { id: "gpu-details", component: GPUInfoCard },
            ].map(({ id, component: CardComponent }) => (
              <AppGrid key={id} size={{ xs: 12, md: 6, xl: 3 }}>
                <ErrorBoundary>
                  <Suspense fallback={<WidgetLoader minHeight={cardHeight} />}>
                    <CardComponent />
                  </Suspense>
                </ErrorBoundary>
              </AppGrid>
            ))}
          </AppGrid>
        </AppCollapse>
      </div>

      {/* ── Hardware Cards ──────────────────────────────────────────────── */}
      <SectionHeader
        controlsId="hardware-hardware-panel"
        expanded={sections.hardware}
        onToggle={() => toggleSection("hardware")}
        title="Hardware"
      />
      <div id="hardware-hardware-panel">
        <AppCollapse in={sections.hardware} unmountOnExit>
          <HistoryHoverProvider>
            <AppGrid
              alignItems="stretch"
              container
              spacing={4}
              style={{ marginBottom: 16 }}
            >
              <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
                <ErrorBoundary>
                  <CPUHistoryCard
                    onRangeChange={setHistoryRange}
                    rangeId={historyRange}
                  />
                </ErrorBoundary>
              </AppGrid>
              <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
                <ErrorBoundary>
                  <MemoryHistoryCard
                    onRangeChange={setHistoryRange}
                    rangeId={historyRange}
                  />
                </ErrorBoundary>
              </AppGrid>
              <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
                <ErrorBoundary>
                  <DiskIOHistoryCard
                    onRangeChange={setHistoryRange}
                    rangeId={historyRange}
                  />
                </ErrorBoundary>
              </AppGrid>
              <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
                <ErrorBoundary>
                  <NetworkHistoryCard
                    onRangeChange={setHistoryRange}
                    rangeId={historyRange}
                  />
                </ErrorBoundary>
              </AppGrid>
            </AppGrid>
          </HistoryHoverProvider>
        </AppCollapse>
      </div>

      {/* ── Sensor Readings ────────────────────────────────────────────── */}
      <SectionHeader
        controlsId="hardware-sensors-panel"
        expanded={sections.sensors}
        onToggle={() => toggleSection("sensors")}
        title="Sensors"
      />
      <div id="hardware-sensors-panel">
        <AppCollapse in={sections.sensors} unmountOnExit>
          <ErrorBoundary>
            <Suspense fallback={<WidgetLoader minHeight={180} />}>
              <SensorReadings />
            </Suspense>
          </ErrorBoundary>
        </AppCollapse>
      </div>

      {/* ── Memory Modules ───────────────────────────────────────────────── */}
      <SectionHeader
        controlsId="hardware-memory-panel"
        expanded={sections.memoryModules}
        onToggle={() => toggleSection("memoryModules")}
        title="Memory"
      />
      <div id="hardware-memory-panel">
        <AppCollapse in={sections.memoryModules} unmountOnExit>
          <ErrorBoundary>
            <Suspense fallback={<WidgetLoader minHeight={280} />}>
              <MemoryModulesTable />
            </Suspense>
          </ErrorBoundary>
        </AppCollapse>
      </div>

      {/* ── PCI Devices ──────────────────────────────────────────────────── */}
      <SectionHeader
        controlsId="hardware-pci-panel"
        expanded={sections.pciDevices}
        onToggle={() => toggleSection("pciDevices")}
        title="PCI Devices"
      />
      <div id="hardware-pci-panel">
        <AppCollapse in={sections.pciDevices} unmountOnExit>
          <ErrorBoundary>
            <Suspense fallback={<WidgetLoader minHeight={420} />}>
              <PciDevicesTable />
            </Suspense>
          </ErrorBoundary>
        </AppCollapse>
      </div>
    </div>
  );
};

export default HardwarePage;
