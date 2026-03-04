import TemperatureIcon from "@mui/icons-material/Thermostat";
import { Stack, Typography } from "@mui/material";
import React from "react";

import ProcessorGraph from "./ProcessorGraph";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
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
      <Stack sx={{ height: "90px", width: "100%", minWidth: 0 }}>
        <ProcessorGraph usage={averageCpuUsage} />
      </Stack>
    ),
    stats: (
      <Stack
        sx={{
          display: "flex",
          flexDirection: "column",
          alignSelf: "flex-start",
          width: "fit-content",
        }}
      >
        {[
          { label: "CPU", value: CPUInfo?.modelName },
          {
            label: "Cores",
            value: CPUInfo ? `${CPUInfo.cores} Threads` : undefined,
          },
          {
            label: "Max Usage",
            value: `${Math.max(...(CPUInfo?.perCoreUsage || [0])).toFixed(0)}%`,
          },
        ].map(({ label, value }) => (
          <Stack
            key={label}
            direction="row"
            alignItems="baseline"
            sx={{
              justifyContent: "flex-start",
              py: 0.5,
              borderBottom: "1px solid",
              borderColor: "divider",
              "&:last-child": { borderBottom: "none" },
              gap: 1,
            }}
          >
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                fontSize: "0.62rem",
                flexShrink: 0,
              }}
            >
              {label}
            </Typography>
            <Typography variant="body2" fontWeight={500} noWrap>
              {value}
            </Typography>
          </Stack>
        ))}
      </Stack>
    ),
    icon_text: IconText,
    icon: TemperatureIcon,
    iconProps: { sx: { color: "text.secondary" } },
  };

  return <DashboardCard {...data} />;
};

export default Processor;
