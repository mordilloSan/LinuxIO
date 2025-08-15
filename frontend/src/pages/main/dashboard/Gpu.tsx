import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Typography, Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import GeneralCard from "@/components/cards/GeneralCard";
import axios from "@/utils/axios";

interface GPU {
  vendor: string;
  model: string;
  driver: string;
  address: string;
}

interface GpuResponse {
  gpus: GPU[];
}

const GpuInfo: React.FC = () => {
  const { data: gpuInfo } = useQuery<GpuResponse>({
    queryKey: ["Gpuinfo"],
    queryFn: async () => {
      const res = await axios.get("/system/gpu");
      return res.data;
    },
    refetchInterval: 50000,
  });

  const gpu = gpuInfo?.gpus?.[0];

  const visibleDetails = gpu ? (
    <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
      <Typography variant="body1">{`${gpu.vendor} - ${gpu.model}`}</Typography>
      <Typography variant="body1">{`Driver: ${gpu.driver}`}</Typography>
      <Typography variant="body1">{`Address: ${gpu.address}`}</Typography>
    </Box>
  ) : (
    <Typography variant="body2">No GPU information available.</Typography>
  );

  return (
    <GeneralCard
      title="GPU"
      stats={visibleDetails}
      icon={TemperatureIcon}
      iconProps={{ sx: { color: "grey" } }}
      avatarIcon="bi:gpu-card"
    />
  );
};

export default GpuInfo;
