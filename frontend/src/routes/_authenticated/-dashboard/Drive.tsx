import { useSuspenseQueries, useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";

import { type ApiDisk, linuxio } from "@/api";
import DashboardCard, {
  CardHeaderSelect,
} from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

import DashboardStatRows from "./DashboardStatRows";
import DriveGraph from "./DriveGraph";

function parseSizeToBytes(input: string | undefined | null): number {
  if (!input) return 0;
  const s = input.trim().toUpperCase();

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

const resolveDriveName = (drives: ApiDisk[], selected: string): string => {
  if (selected && drives.some((drive) => drive.name === selected)) {
    return selected;
  }
  if (!drives.length) return "";
  const online = drives.find((drive) => parseSizeToBytes(drive.size) > 0);
  return online?.name || drives[0].name;
};

const hasAnyDrive = (drives: ApiDisk[]): boolean => drives.length > 0;

interface DriveSelectionProps {
  selected: string;
}

const DriveSelect = ({
  onSelect,
  selected,
}: DriveSelectionProps & { onSelect: (name: string) => void }) => {
  const selectHeader = useCallback(
    (drives: ApiDisk[]) => ({
      names: drives.map((drive) => drive.name),
      selectedName: resolveDriveName(drives, selected),
    }),
    [selected],
  );

  const { data: header } = useSuspenseQuery({
    ...linuxio.storage.get_drive_info,
    select: selectHeader,
  });

  return (
    <CardHeaderSelect
      onChange={onSelect}
      options={header.names.map((name) => ({ label: name, value: name }))}
      value={header.selectedName}
    />
  );
};

const DriveStats = ({ selected }: DriveSelectionProps) => {
  const selectDrive = useCallback(
    (drives: ApiDisk[]) => {
      const name = resolveDriveName(drives, selected);
      const raw = drives.find((drive) => drive.name === name);

      return raw
        ? {
            model: raw.model,
            sizeBytes: parseSizeToBytes(raw.size),
            transport: raw.type ?? "unknown",
            vendor: raw.vendor,
          }
        : null;
    },
    [selected],
  );

  const { data: drive } = useSuspenseQuery({
    ...linuxio.storage.get_drive_info,
    select: selectDrive,
  });

  if (!drive) {
    return <AppTypography variant="body2">No drives found.</AppTypography>;
  }

  return (
    <DashboardStatRows
      containerStyle={{ alignSelf: "auto" }}
      rows={[
        { label: "Model", value: drive.model || "Unknown" },
        { label: "Type", value: drive.transport || "Unknown" },
        {
          label: "Size",
          value: formatFileSize(drive.sizeBytes) || "Unknown",
        },
        ...(drive.vendor ? [{ label: "Vendor", value: drive.vendor }] : []),
      ].map((row) => ({ ...row, valueStyle: { minWidth: 0 } }))}
    />
  );
};

const DriveGraphPane = ({ selected }: DriveSelectionProps) => {
  const selectDriveName = useCallback(
    (drives: ApiDisk[]) => resolveDriveName(drives, selected),
    [selected],
  );

  const [{ data: driveName }, { data: diskThroughput }] = useSuspenseQueries({
    queries: [
      { ...linuxio.storage.get_drive_info, select: selectDriveName },
      { ...linuxio.system.get_disk_throughput, refetchInterval: 1000 },
    ],
  });

  const device = diskThroughput?.devices.find((d) => d.name === driveName);

  return (
    <div style={{ height: "90px", width: "100%", minWidth: 0 }}>
      <DriveGraph
        key={driveName}
        readBytesPerSec={device?.readBytesPerSec ?? 0}
        writeBytesPerSec={device?.writeBytesPerSec ?? 0}
      />
    </div>
  );
};

const Drive = () => {
  const [selected, setSelected] = useState("");
  const { data: hasDrives } = useSuspenseQuery({
    ...linuxio.storage.get_drive_info,
    select: hasAnyDrive,
  });

  return (
    <DashboardCard
      avatarIcon="mdi:harddisk"
      headerExtras={<DriveSelect onSelect={setSelected} selected={selected} />}
      stats={<DriveStats selected={selected} />}
      stats2={hasDrives ? <DriveGraphPane selected={selected} /> : undefined}
      title="Drives"
    />
  );
};

export default Drive;
