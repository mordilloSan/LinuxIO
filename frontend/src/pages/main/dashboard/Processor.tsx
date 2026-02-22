import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Box, Typography } from "@mui/material";
import React from "react";

import ProcessorGraph from "./ProcessorGraph";

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
        <ProcessorGraph usage={averageCpuUsage} />
      </Box>
    ),
    stats: (
      <Box
        sx={{
          display: "flex",
          gap: 1,
          flexDirection: "column",
          alignSelf: "flex-start",
          mt: 4,
        }}
      >
        {/* Variant B: theme-colored label + white value */}
        <Box sx={{ display: "flex", gap: 0.5, alignItems: "baseline" }}>
          <Typography
            variant="body2"
            color="primary.main"
            fontWeight={600}
            sx={{ flexShrink: 0 }}
          >
            CPU:
          </Typography>
          <Typography variant="body2" fontWeight={500} noWrap>
            {CPUInfo?.modelName}
          </Typography>
        </Box>
        <Box sx={{ display: "flex", gap: 0.5, alignItems: "baseline" }}>
          <Typography
            variant="body2"
            color="primary.main"
            fontWeight={600}
            sx={{ flexShrink: 0 }}
          >
            Cores:
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {CPUInfo?.cores} Threads
          </Typography>
        </Box>
        <Box sx={{ display: "flex", gap: 0.5, alignItems: "baseline" }}>
          <Typography
            variant="body2"
            color="primary.main"
            fontWeight={600}
            sx={{ flexShrink: 0 }}
          >
            Max Usage:
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {Math.max(...(CPUInfo?.perCoreUsage || [0])).toFixed(0)}%
          </Typography>
        </Box>
      </Box>
    ),
    icon_text: IconText,
    icon: TemperatureIcon,
    iconProps: { sx: { color: "grey" } },
  };

  return <GeneralCard {...data} />;
};

export default Processor;
