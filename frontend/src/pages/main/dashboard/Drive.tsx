import { Box, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useState, useEffect } from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";
import { formatBytes } from "@/utils/formatBytes";

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
    data: drives = [],
    isLoading,
    isError,
  } = useQuery<DriveInfo[]>({
    queryKey: ["systemDrives"],
    queryFn: async () => {
      const res = await axios.get<ApiDisk[]>("/system/disk");
      const items = res.data ?? [];
      // Normalize API → component shape
      const normalized: DriveInfo[] = items.map((d) => ({
        name: d.name,
        model: d.model,
        sizeBytes: parseSizeToBytes(d.size),
        transport: d.type ?? "unknown",
        vendor: d.vendor,
        serial: d.serial,
      }));
      return normalized;
    },
  });

  const [selected, setSelected] = useState("");

  useEffect(() => {
    if (drives.length && !selected) {
      const online = drives.find((d) => d.sizeBytes > 0);
      setSelected(online?.name || drives[0].name);
    }
  }, [drives, selected]);

  if (isLoading) {
    return (
      <GeneralCard
        title="Drives"
        avatarIcon="mdi:harddisk"
        stats={<ComponentLoader />}
        selectOptions={[]}
        selectedOption={selected}
        selectedOptionLabel={selected}
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

  const selectedDrive = drives.find((drive) => drive.name === selected);
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
        {formatBytes(selectedDrive.sizeBytes) || "Unknown"}
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
      selectedOption={selected}
      selectedOptionLabel={selected}
      onSelect={(val: string) => setSelected(val)}
    />
  );
};

export default Drive;
