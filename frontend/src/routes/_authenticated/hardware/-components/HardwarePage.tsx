import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import {
  Suspense,
  useCallback,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { linuxio, type SensorGroup } from "@/api";
import HardwareTableCard from "@/components/cards/HardwareTableCard";
import SensorGroupCard from "@/components/cards/SensorGroupCard";
import { isPrimarySensorReading } from "@/components/cards/SensorGroupCard";
import { SensorEmptyCard } from "@/components/cards/SensorSummaryCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import WidgetLoader from "@/components/loaders/WidgetLoader";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import { cardHeight, TRANSITION_SLOW_CSS } from "@/theme/constants";
import "@/theme/section.css";

import {
  BIOSInfoCard,
  CPUDetailsCard,
  CPUHistoryCard,
  DiskIOHistoryCard,
  GPUInfoCard,
  type HardwareHistoryRangeId,
  MemoryHistoryCard,
  MotherboardInfoCard,
  NetworkHistoryCard,
} from "./HardwareHistoryCards";
import {
  hardwareSensorQueryOptions,
  hardwareStableQueryOptions,
} from "./hardwareQueryOptions";
import {
  defaultHardwareSections,
  resolvedHardwareSections,
} from "./hardwareSections";

// ─── section header ──────────────────────────────────────────────────────────

const SectionHeader = ({
  title,
  expanded,
  onClick,
  extras,
}: {
  title: string;
  expanded: boolean;
  onClick: () => void;
  extras?: ReactNode;
}) => (
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

function SensorReadings() {
  const { data } = useSuspenseQuery(
    linuxio.system.get_sensor_info.queryOptions({
      ...hardwareSensorQueryOptions,
      refetchInterval: 5_000,
    }),
  );
  const sensorGroups = data as SensorGroup[];
  const visibleSensorGroups = useMemo(
    () =>
      sensorGroups
        .map((group) => ({
          ...group,
          readings: group.readings.filter(isPrimarySensorReading),
        }))
        .filter((group) => group.readings.length > 0),
    [sensorGroups],
  );
  const sensorSummary = useMemo(
    () => ({
      adapters: visibleSensorGroups.length,
      readings: visibleSensorGroups.reduce(
        (sum, group) => sum + group.readings.length,
        0,
      ),
    }),
    [visibleSensorGroups],
  );

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
        {visibleSensorGroups.map((group, index) => (
          <AppGrid
            key={`${group.adapter}-${index}`}
            size={{ xs: 12, sm: 6, lg: 4, xl: 3 }}
          >
            <SensorGroupCard group={group} />
          </AppGrid>
        ))}
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
  const [historyHoverTime, setHistoryHoverTime] = useState<number | null>(null);

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
        expanded={sections.systemInfo}
        onClick={() => toggleSection("systemInfo")}
        title="System Information"
      />
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

      {/* ── Hardware Cards ──────────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.hardware}
        onClick={() => toggleSection("hardware")}
        title="Hardware"
      />
      <AppCollapse in={sections.hardware} unmountOnExit>
        <AppGrid
          alignItems="stretch"
          container
          spacing={4}
          style={{ marginBottom: 16 }}
        >
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <CPUHistoryCard
                hoverTime={historyHoverTime}
                onHoverTimeChange={setHistoryHoverTime}
                onRangeChange={setHistoryRange}
                rangeId={historyRange}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <MemoryHistoryCard
                hoverTime={historyHoverTime}
                onHoverTimeChange={setHistoryHoverTime}
                onRangeChange={setHistoryRange}
                rangeId={historyRange}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <DiskIOHistoryCard
                hoverTime={historyHoverTime}
                onHoverTimeChange={setHistoryHoverTime}
                onRangeChange={setHistoryRange}
                rangeId={historyRange}
              />
            </ErrorBoundary>
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6, lg: 4, xl: 3 }}>
            <ErrorBoundary>
              <NetworkHistoryCard
                hoverTime={historyHoverTime}
                onHoverTimeChange={setHistoryHoverTime}
                onRangeChange={setHistoryRange}
                rangeId={historyRange}
              />
            </ErrorBoundary>
          </AppGrid>
        </AppGrid>
      </AppCollapse>

      {/* ── Sensor Readings ────────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.sensors}
        onClick={() => toggleSection("sensors")}
        title="Sensors"
      />
      <AppCollapse in={sections.sensors} unmountOnExit>
        <ErrorBoundary>
          <Suspense fallback={<WidgetLoader minHeight={180} />}>
            <SensorReadings />
          </Suspense>
        </ErrorBoundary>
      </AppCollapse>

      {/* ── Memory Modules ───────────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.memoryModules}
        onClick={() => toggleSection("memoryModules")}
        title="Memory"
      />
      <AppCollapse in={sections.memoryModules} unmountOnExit>
        <ErrorBoundary>
          <Suspense fallback={<WidgetLoader minHeight={280} />}>
            <MemoryModulesTable />
          </Suspense>
        </ErrorBoundary>
      </AppCollapse>

      {/* ── PCI Devices ──────────────────────────────────────────────────── */}
      <SectionHeader
        expanded={sections.pciDevices}
        onClick={() => toggleSection("pciDevices")}
        title="PCI Devices"
      />
      <AppCollapse in={sections.pciDevices} unmountOnExit>
        <ErrorBoundary>
          <Suspense fallback={<WidgetLoader minHeight={420} />}>
            <PciDevicesTable />
          </Suspense>
        </ErrorBoundary>
      </AppCollapse>
    </div>
  );
};

export default HardwarePage;
