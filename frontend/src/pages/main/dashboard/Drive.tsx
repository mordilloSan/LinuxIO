import { Box, Typography } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { useState, useEffect } from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";
import { formatBytes } from "@/utils/formatBytes";

interface DriveInfo {
  name: string;
  model: string;
  sizeBytes: number;
  transport: string;
  vendor?: string;
  serial?: string;
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
      const online = drives.find((d) => d.sizeBytes !== 0);
      setSelected(online?.name || drives[0].name);
    }
  }, [drives, selected]);

  const selectedDrive = drives.find((drive) => drive.name === selected);
  const content = selectedDrive ? (
    isLoading ? (
      <ComponentLoader />
    ) : (
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
    />
  );
};

export default Drive;
