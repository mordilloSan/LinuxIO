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
      <Box
        sx={{
          display: "flex",
          flexDirection: "column",
          alignSelf: "flex-start",
          mt: 4,
          width: "fit-content",
        }}
      >
        {[
          {
            label: "Total",
            value: formatFileSize(memoryData?.system?.total ?? 0, 2),
          },
          {
            label: "Used",
            value: formatFileSize(memoryData?.system?.active ?? 0, 2),
          },
          {
            label: "Docker",
            value: formatFileSize(memoryData?.docker?.used ?? 0, 2),
          },
          {
            label: "Swap",
            value: `${formatFileSize((memoryData?.system?.swapTotal ?? 0) - (memoryData?.system?.swapFree ?? 0), 2)}/${formatFileSize(memoryData?.system?.swapTotal ?? 0, 2)}`,
          },
        ].map(({ label, value }) => (
          <Box
            key={label}
            sx={{
              display: "flex",
              justifyContent: "flex-start",
              alignItems: "baseline",
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
          </Box>
        ))}
      </Box>
    ),
    avatarIcon: "la:memory",
  };

  return <GeneralCard {...data} />;
};

export default MemoryUsage;
