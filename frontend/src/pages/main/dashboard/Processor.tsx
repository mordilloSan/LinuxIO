import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Box, Typography } from "@mui/material";
import React from "react";

import linuxio from "@/api/react-query";
import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import ComponentLoader from "@/components/loaders/ComponentLoader";

interface CPUInfoResponse {
  vendorId: string;
  modelName: string;
  family: string;
  model: string;
  mhz: number;
  cores: number;
  loadAverage: {
    load1: number;
    load5: number;
    load15: number;
  };
  perCoreUsage: number[];
  temperature: { [core: string]: number };
}

const Processor: React.FC = () => {
  const {
    data: CPUInfo,
    isPending,
    isError,
  } = linuxio.useCall<CPUInfoResponse>("system", "get_cpu_info", [], {
    refetchInterval: 2000,
  });

  const averageCpuUsage = CPUInfo?.perCoreUsage?.length
    ? CPUInfo.perCoreUsage.reduce((sum, cpu) => sum + cpu, 0) /
      CPUInfo.perCoreUsage.length
    : 0;

  const temperatures = CPUInfo?.temperature
    ? Object.values(CPUInfo.temperature)
    : [];

  const avgTemp = temperatures.length
    ? (
        temperatures.reduce((sum, t) => sum + t, 0) / temperatures.length
      ).toFixed(1)
    : "--";

  const IconText = `${avgTemp}°C`;

  const data = {
    title: "Processor",
    avatarIcon: "ph:cpu",
    stats2: isError ? (
      <ErrorMessage />
    ) : isPending ? (
      <ComponentLoader />
    ) : (
      <GradientCircularGauge
        value={averageCpuUsage}
        gradientColors={["#82ca9d", "#eab308", "#ef4444"]}
        size={108}
        thickness={9.8}
        showPercentage={true}
      />
    ),
    stats: (
      <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
        <Typography variant="body1">
          <strong>CPU:</strong> {CPUInfo?.modelName}
        </Typography>
        <Typography variant="body1">
          <strong>Cores:</strong> {CPUInfo?.cores} Threads
        </Typography>
        <Typography variant="body1">
          <strong>Max Usage:</strong>{" "}
          {Math.max(...(CPUInfo?.perCoreUsage || [0])).toFixed(0)}%
        </Typography>
      </Box>
    ),
    icon_text: IconText,
    icon: TemperatureIcon,
    iconProps: { sx: { color: "grey" } },
  };

  return <GeneralCard {...data} />;
};

export default Processor;
