import { Typography, Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";

import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import CircularProgressWithLabel from "@/components/gauge/CircularProgress";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";

// Utility functions
const formatBytesToGB = (bytes: number) => (bytes / 1000 ** 3).toFixed(2);
const calculatePercentage = (used: number, total: number) =>
  ((used / total) * 100).toFixed(2);

const MemoryUsage = () => {
  const {
    data: memoryData,
    isPending,
    isError,
  } = useQuery({
    queryKey: ["memoryInfo"],
    queryFn: async () => {
      const response = await axios.get("/system/mem");
      return response.data;
    },
    refetchInterval: 2000,
  });

  const ramUsagePercentage = memoryData?.system.active
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
      <CircularProgressWithLabel
        value={ramUsagePercentage}
        size={120}
        thickness={4}
      />
    ),
    stats: (
      <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
        <Typography variant="body1">
          <strong>Total Memory:</strong>{" "}
          {formatBytesToGB(memoryData?.system.total || 0)} GB
        </Typography>
        <Typography variant="body1">
          <strong>Used Memory:</strong>{" "}
          {formatBytesToGB(memoryData?.system.active || 0)} GB
        </Typography>
        <Typography variant="body1">
          <strong>Swap:</strong>{" "}
          {formatBytesToGB(
            memoryData?.system.swapTotal - memoryData?.system.swapFree || 0,
          )}{" "}
          of {formatBytesToGB(memoryData?.system.swapTotal || 0)} GB
        </Typography>
      </Box>
    ),
    avatarIcon: "la:memory",
  };

  return <GeneralCard {...data} />;
};

export default MemoryUsage;
