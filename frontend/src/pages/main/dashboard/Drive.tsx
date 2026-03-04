import { Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useMemo, useState } from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";

interface DriveInfo {
  name: string;
  model: string;
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
  const theme = useTheme();
  const {
    data: rawDrives = [],
    isPending: isLoading,
    isError,
  } = linuxio.storage.get_drive_info.useQuery();

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
        title="Drives"
        avatarIcon="mdi:harddisk"
        stats={<ComponentLoader />}
        selectOptions={[]}
        selectedOption={selectedDriveName}
        selectedOptionLabel={selectedDriveName}
        onSelect={() => {}}
      />
    );
  }

  if (isError || drives.length === 0) {
    return (
      <DashboardCard
        title="Drives"
        avatarIcon="mdi:harddisk"
        stats={<Typography variant="body2">No drives found.</Typography>}
        selectOptions={[]}
        selectedOption=""
        selectedOptionLabel=""
        onSelect={() => {}}
      />
    );
  }

  const selectedDrive = drives.find(
    (drive) => drive.name === selectedDriveName,
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
                : "1px solid var(--mui-palette-divider)",
            gap: theme.spacing(1),
          }}
        >
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </Typography>
          <Typography variant="body2" fontWeight={500} noWrap>
            {value}
          </Typography>
        </div>
      ))}
    </div>
  ) : (
    <Typography variant="body2">No drive selected.</Typography>
  );

  const options = drives.map((drive) => ({
    value: drive.name,
    label: drive.name,
  }));

  return (
    <DashboardCard
      title="Drives"
      avatarIcon="mdi:harddisk"
      stats={content}
      selectOptions={options}
      selectedOption={selectedDriveName}
      selectedOptionLabel={selectedDriveName}
      onSelect={(val: string) => setSelected(val)}
    />
  );
};

export default Drive;
