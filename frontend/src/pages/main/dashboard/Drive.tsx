import React, { useMemo, useState } from "react";

import DriveGraph from "./DriveGraph";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

interface DriveInfo {
  model: string;
  name: string;
  sizeBytes: number;
  transport: string;
  vendor?: string;
}

function parseSizeToBytes(input: string | undefined | null): number {
  if (!input) return 0;
  const s = String(input).trim().toUpperCase();

  const m = s.match(/^([\d.]+)\s*([KMGTPE]?)(B)?$/);
  if (!m) return 0;

  const value = parseFloat(m[1]);
  if (!isFinite(value) || value < 0) return 0;

  const unit = m[2] || "B";
  const pow =
    unit === "B"
      ? 0
      : unit === "K"
        ? 1
        : unit === "M"
          ? 2
          : unit === "G"
            ? 3
            : unit === "T"
              ? 4
              : unit === "P"
                ? 5
                : 0;

  return Math.floor(value * Math.pow(1024, pow));
}

const Drive: React.FC = () => {
  const theme = useAppTheme();
  const {
    data: rawDrives = [],
    isPending: isLoading,
    isError,
  } = linuxio.storage.get_drive_info.useQuery();
  const { data: diskThroughput, isPending: isThroughputLoading } =
    linuxio.system.get_disk_throughput.useQuery({
      refetchInterval: 1000,
    });

  const drives = useMemo<DriveInfo[]>(
    () =>
      rawDrives.map((d) => ({
        name: d.name,
        model: d.model,
        sizeBytes: parseSizeToBytes(d.size),
        transport: d.type ?? "unknown",
        vendor: d.vendor,
      })),
    [rawDrives],
  );

  const [selected, setSelected] = useState("");
  const fallbackSelected = useMemo(() => {
    if (!drives.length) return "";
    const online = drives.find((d) => d.sizeBytes > 0);
    return online?.name || drives[0].name;
  }, [drives]);
  const selectedDriveName =
    selected && drives.some((drive) => drive.name === selected)
      ? selected
      : fallbackSelected;

  if (isLoading) {
    return (
      <DashboardCard
        avatarIcon="mdi:harddisk"
        onSelect={() => {}}
        selectedOption={selectedDriveName}
        selectedOptionLabel={selectedDriveName}
        selectOptions={[]}
        stats={<ComponentLoader />}
        title="Drives"
      />
    );
  }

  if (isError || drives.length === 0) {
    return (
      <DashboardCard
        avatarIcon="mdi:harddisk"
        onSelect={() => {}}
        selectedOption=""
        selectedOptionLabel=""
        selectOptions={[]}
        stats={<AppTypography variant="body2">No drives found.</AppTypography>}
        title="Drives"
      />
    );
  }

  const selectedDrive = drives.find(
    (drive) => drive.name === selectedDriveName,
  );
  const selectedDriveThroughput = diskThroughput?.devices.find(
    (device) => device.name === selectedDriveName,
  );
  const content = selectedDrive ? (
    <div
      style={{ display: "flex", flexDirection: "column", width: "fit-content" }}
    >
      {[
        { label: "Model", value: selectedDrive.model || "Unknown" },
        { label: "Type", value: selectedDrive.transport || "Unknown" },
        {
          label: "Size",
          value: formatFileSize(selectedDrive.sizeBytes) || "Unknown",
        },
        ...(selectedDrive.vendor
          ? [{ label: "Vendor", value: selectedDrive.vendor }]
          : []),
      ].map(({ label, value }, index, rows) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
            gap: theme.spacing(1),
          }}
        >
          <AppTypography
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
            variant="caption"
          >
            {label}
          </AppTypography>
          <AppTypography
            copyText={value}
            fontWeight={500}
            noWrap
            style={{ minWidth: 0 }}
            variant="body2"
          >
            {value}
          </AppTypography>
        </div>
      ))}
    </div>
  ) : (
    <AppTypography variant="body2">No drive selected.</AppTypography>
  );
  const content2 = selectedDrive ? (
    isThroughputLoading ? (
      <ComponentLoader />
    ) : (
      <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
        <DriveGraph
          key={selectedDriveName}
          readBytesPerSec={selectedDriveThroughput?.readBytesPerSec ?? 0}
          writeBytesPerSec={selectedDriveThroughput?.writeBytesPerSec ?? 0}
        />
      </div>
    )
  ) : (
    <AppTypography variant="body2">No I/O data.</AppTypography>
  );

  const options = drives.map((drive) => ({
    value: drive.name,
    label: drive.name,
  }));

  return (
    <DashboardCard
      avatarIcon="mdi:harddisk"
      onSelect={(val: string) => setSelected(val)}
      selectedOption={selectedDriveName}
      selectedOptionLabel={selectedDriveName}
      selectOptions={options}
      stats={content}
      stats2={content2}
      title="Drives"
    />
  );
};

export default Drive;
