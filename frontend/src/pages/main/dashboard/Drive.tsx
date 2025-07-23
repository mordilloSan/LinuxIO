import { Box, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useState, useEffect } from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";

interface SmartStatus {
  passed: boolean;
  [key: string]: any;
}
interface SmartInfo {
  smart_status?: SmartStatus;
  [key: string]: any;
}
interface DriveInfo {
  name: string;
  model: string;
  size: string;
  type: string;
  vendor?: string;
  serial?: string;
  ro?: boolean;
  smart?: SmartInfo;
  smartError?: string;
  power?: any;
  powerError?: string;
}

function getConnectionStatus(
  drive: DriveInfo | undefined,
): "online" | "warning" | "error" {
  if (!drive) return "warning";
  if (drive.smartError) return "error";
  if (
    !drive.smart ||
    Object.keys(drive.smart).length === 0 ||
    !drive.smart.smart_status
  )
    return "warning";
  if (drive.smart.smart_status.passed === true) return "online";
  if (drive.smart.smart_status.passed === false) return "error";
  return "warning";
}

const Drive: React.FC = () => {
  const { data: drives = [], isLoading } = useQuery<DriveInfo[]>({
    queryKey: ["systemDrives"],
    queryFn: async () => {
      const res = await axios.get("/system/disk");
      return res.data;
    },
  });
  const [selected, setSelected] = useState("");

  useEffect(() => {
    if (drives.length && !selected) {
      const online = drives.find((d) => d.size !== "0" && d.size !== "0B");
      setSelected(online?.name || drives[0].name);
    }
  }, [drives, selected]);

  const selectedDrive = drives.find((drive) => drive.name === selected);
  const isDisconnected =
    selectedDrive?.size === "0" || selectedDrive?.size === "0B";

  const content = selectedDrive ? (
    isLoading ? (
      <ComponentLoader />
    ) : (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Typography variant="body2">
          <strong>Model:</strong> {selectedDrive.model || "Unknown"}
        </Typography>
        <Typography variant="body2">
          <strong>Type:</strong> {selectedDrive.type || "Unknown"}
        </Typography>
        <Typography variant="body2">
          <strong>Size:</strong> {selectedDrive.size || "Unknown"}
        </Typography>
      </Box>
    )
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
      connectionStatus={
        isDisconnected ? "offline" : getConnectionStatus(selectedDrive)
      }
    />
  );
};

export default Drive;
