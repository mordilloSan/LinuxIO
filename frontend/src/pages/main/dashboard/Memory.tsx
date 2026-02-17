import { Typography, Box } from "@mui/material";

import { linuxio } from "@/api";
import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";

// Utility functions

const calculatePercentage = (used: number, total: number) =>
  ((used / total) * 100).toFixed(2);

const MemoryUsage = () => {
  const {
    data: memoryData,
    isPending,
    isError,
  } = linuxio.system.get_memory_info.useQuery({
    refetchInterval: 2000,
  });

  const ramUsagePercentage = memoryData?.system?.active
    ? parseFloat(
        calculatePercentage(memoryData.system.active, memoryData.system.total),
      )
    : 0;

  const data = {
    title: "Memory Usage",
    stats2: isError ? (
      <ErrorMessage />
    ) : isPending ? (
      <ComponentLoader />
    ) : (
      <GradientCircularGauge
        value={ramUsagePercentage}
        gradientColors={["#82ca9d", "#eab308", "#ef4444"]}
        size={108}
        thickness={9.8}
        showPercentage={true}
      />
    ),
    stats: (
      <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
        <Typography variant="body1">
          <strong>Total Memory:</strong>{" "}
          {formatFileSize(memoryData?.system?.total ?? 0, 2)}
        </Typography>
        <Typography variant="body1">
          <strong>Used Memory:</strong>{" "}
          {formatFileSize(memoryData?.system?.active ?? 0, 2)}
        </Typography>
        <Typography variant="body1">
          <strong>Docker:</strong>{" "}
          {formatFileSize(memoryData?.docker?.used ?? 0, 2)}
        </Typography>
        <Typography variant="body1">
          <strong>Swap:</strong>{" "}
          {formatFileSize(
            (memoryData?.system?.swapTotal ?? 0) -
              (memoryData?.system?.swapFree ?? 0),
            2,
          )}
          /{formatFileSize(memoryData?.system?.swapTotal ?? 0, 2)}
        </Typography>
      </Box>
    ),
    avatarIcon: "la:memory",
  };

  return <GeneralCard {...data} />;
};

export default MemoryUsage;
