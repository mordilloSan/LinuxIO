import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Box, Typography } from "@mui/material";
import React from "react";

import CpuGraph from "./CpuGraph";

import { linuxio } from "@/api";
import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const Processor: React.FC = () => {
  const {
    data: CPUInfo,
    isPending,
    isError,
  } = linuxio.system.get_cpu_info.useQuery({
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
      <Box sx={{ height: "90px", width: "100%", minWidth: 0 }}>
        <CpuGraph usage={averageCpuUsage} />
      </Box>
    ),
    stats: (
      <Box sx={{ display: "flex", gap: 1, flexDirection: "column", alignSelf: "flex-start", mt: 4 }}>
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
