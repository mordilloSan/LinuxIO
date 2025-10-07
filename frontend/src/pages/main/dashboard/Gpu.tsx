import ThermostatIcon from "@mui/icons-material/Thermostat";
import { Typography, Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import axios from "@/utils/axios";

interface GpuDevice {
  address: string;
  device_id: string;
  driver: string;
  model: string;
  revision: string;
  subsystem: string;
  subsystem_id: string;
  vendor: string;
  vendor_id: string;
}

const GpuInfo: React.FC = () => {
  const {
    data: gpus,
    isLoading,
    isError,
  } = useQuery<GpuDevice[]>({
    queryKey: ["Gpuinfo"],
    queryFn: async () => {
      const res = await axios.get<GpuDevice[]>("/system/gpu"); // API returns an array
      return res.data;
    },
    refetchInterval: 50_000,
  });

  let content: React.ReactNode = null;

  if (isLoading) {
    content = <Typography variant="body2">Loading…</Typography>;
  } else if (isError || !gpus || gpus.length === 0) {
    content = (
      <Typography variant="body2">No GPU information available.</Typography>
    );
  } else {
    content = (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        {gpus.map((gpu, idx) => (
          <Box
            key={`${gpu.vendor_id}-${gpu.device_id}-${idx}`}
            sx={{ display: "flex", flexDirection: "column" }}
          >
            <Typography variant="body1">{`${gpu.vendor} — ${gpu.model}`}</Typography>
            <Typography variant="body2">{`Driver: ${gpu.driver}`}</Typography>
            <Typography variant="body2">{`Address: ${gpu.address}`}</Typography>
          </Box>
        ))}
      </Box>
    );
  }

  return (
    <GeneralCard
      title="GPU"
      stats={content}
      icon={ThermostatIcon}
      iconProps={{ sx: { color: "grey" } }}
      avatarIcon="bi:gpu-card"
    />
  );
};

export default GpuInfo;
