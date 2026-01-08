import { Box, Typography } from "@mui/material";
import React, { useState, useMemo } from "react";

import linuxio from "@/api/react-query";
import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";

// --- API shape from /system/disk ---
interface ApiDisk {
  model: string;
  name: string;
  ro: boolean;
  serial?: string;
  size: string; // e.g. "0B", "953.9G"
  type?: string; // e.g. "nvme", "usb", "sata", etc.
  vendor?: string;
  power?: unknown; // present for nvme
  smart?: unknown; // present for nvme
}

// --- Component's normalized shape ---
interface DriveInfo {
  name: string;
  model: string;
  sizeBytes: number;
  transport: string;
  vendor?: string;
  serial?: string;
}

// Parse "953.9G", "0B", "465.8G", "1024M", "1.8T" to bytes
function parseSizeToBytes(input: string | undefined | null): number {
  if (!input) return 0;
  const s = String(input).trim().toUpperCase();

  // Match number + optional unit (B/K/M/G/T/P), with optional trailing 'B'
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

  // Use binary multiples (KiB, MiB, …) which most tools report
  return Math.floor(value * Math.pow(1024, pow));
}

const Drive: React.FC = () => {
  const {
    data: rawDrives = [],
    isPending: isLoading,
    isError,
  } = linuxio.useCall<ApiDisk[]>("system", "get_drive_info");

  // Normalize API → component shape
  const drives = useMemo<DriveInfo[]>(
    () =>
      rawDrives.map((d) => ({
        name: d.name,
        model: d.model,
        sizeBytes: parseSizeToBytes(d.size),
        transport: d.type ?? "unknown",
        vendor: d.vendor,
        serial: d.serial,
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
      <GeneralCard
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
      <GeneralCard
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
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="body2">
        <strong>Model:</strong> {selectedDrive.model || "Unknown"}
      </Typography>
      <Typography variant="body2">
        <strong>Type:</strong> {selectedDrive.transport || "Unknown"}
      </Typography>
      <Typography variant="body2">
        <strong>Size:</strong>{" "}
        {formatFileSize(selectedDrive.sizeBytes) || "Unknown"}
      </Typography>
      {selectedDrive.vendor && (
        <Typography variant="body2">
          <strong>Vendor:</strong> {selectedDrive.vendor}
        </Typography>
      )}
      {selectedDrive.serial && (
        <Typography variant="body2">
          <strong>Serial:</strong> {selectedDrive.serial}
        </Typography>
      )}
    </Box>
  ) : (
    <Typography variant="body2">No drive selected.</Typography>
  );

  const options = drives.map((drive) => ({
    value: drive.name,
    label: drive.name,
  }));

  return (
    <GeneralCard
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
