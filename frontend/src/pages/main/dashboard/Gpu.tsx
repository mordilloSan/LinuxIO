import ThermostatIcon from "@mui/icons-material/Thermostat";
import { Typography, Box } from "@mui/material";
import React from "react";

import linuxio from "@/api/react-query";
import GeneralCard from "@/components/cards/GeneralCard";

const GpuInfo: React.FC = () => {
  const {
    data: gpus,
    isPending: isLoading,
    isError,
  } = linuxio.system.get_gpu_info.useQuery({
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
