import { Box, CircularProgress, Alert } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";

import ServiceTable, { Service } from "./ServiceTable";

import axios from "@/utils/axios";

const ServicesList: React.FC = () => {
  const { data, isLoading, isError, error } = useQuery<Service[]>({
    queryKey: ["services"],
    queryFn: async () => {
      const res = await axios.get("/services/status");
      return res.data;
    },
    refetchInterval: 2000,
  });

  // Handlers - you would implement actual API calls here!
  const handleRestart = (service: Service) => {
    // Example: await axios.post(`/services/restart`, { name: service.name })
    alert(`Restarting service: ${service.displayName || service.name}`);
    // After API: refetch();
  };

  const handleStop = (service: Service) => {
    alert(`Stopping service: ${service.displayName || service.name}`);
    // After API: refetch();
  };

  const handleViewLogs = (service: Service) => {
    alert(`Show logs for: ${service.displayName || service.name}`);
    // You might open a drawer/modal with logs, etc.
  };

  return (
    <Box>
      {isLoading && (
        <Box textAlign="center" my={5}>
          <CircularProgress />
        </Box>
      )}
      {isError && (
        <Alert severity="error">
          {error instanceof Error ? error.message : "Failed to load services"}
        </Alert>
      )}
      {data && (
        <ServiceTable
          serviceList={data}
          onRestart={handleRestart}
          onStop={handleStop}
          onViewLogs={handleViewLogs}
        />
      )}
    </Box>
  );
};

export default ServicesList;
