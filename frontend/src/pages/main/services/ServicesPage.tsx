import { Box, CircularProgress, Alert } from "@mui/material";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import React, { useState } from "react";

import ServiceLogsDrawer from "./ServiceLogsDrawer";
import ServiceTable, { Service } from "./ServiceTable";

import axios from "@/utils/axios";

const ServicesList: React.FC = () => {
  const queryClient = useQueryClient();
  const [logsDrawerOpen, setLogsDrawerOpen] = useState(false);
  const [selectedService, setSelectedService] = useState<string>("");

  const { data, isLoading, isError, error } = useQuery<Service[]>({
    queryKey: ["services"],
    queryFn: async () => {
      const res = await axios.get("/services/status");
      return res.data;
    },
    refetchInterval: 2000,
  });

  // Mutation for service actions
  const serviceActionMutation = useMutation({
    mutationFn: async ({
      serviceName,
      action,
    }: {
      serviceName: string;
      action:
        | "start"
        | "stop"
        | "restart"
        | "reload"
        | "enable"
        | "disable"
        | "mask"
        | "unmask";
    }) => {
      const res = await axios.post(`/services/${serviceName}/${action}`);
      return res.data;
    },
    onSuccess: () => {
      // Refetch services after successful action
      queryClient.invalidateQueries({ queryKey: ["services"] });
    },
    onError: (error: any) => {
      console.error("Service action failed:", error);
      // You might want to show a toast notification here
      alert(`Action failed: ${error.response?.data?.message || error.message}`);
    },
  });

  const handleRestart = (service: Service) => {
    serviceActionMutation.mutate({
      serviceName: service.name,
      action: "restart",
    });
  };

  const handleStop = (service: Service) => {
    serviceActionMutation.mutate({
      serviceName: service.name,
      action: "stop",
    });
  };

  const handleStart = (service: Service) => {
    serviceActionMutation.mutate({
      serviceName: service.name,
      action: "start",
    });
  };

  const handleViewLogs = (service: Service) => {
    setSelectedService(service.name);
    setLogsDrawerOpen(true);
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
      {serviceActionMutation.isError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          Action failed. Please try again.
        </Alert>
      )}
      {data && (
        <ServiceTable
          serviceList={data}
          onRestart={handleRestart}
          onStop={handleStop}
          onStart={handleStart}
          onViewLogs={handleViewLogs}
          isLoading={serviceActionMutation.isPending}
        />
      )}
      <ServiceLogsDrawer
        open={logsDrawerOpen}
        onClose={() => setLogsDrawerOpen(false)}
        serviceName={selectedService}
      />
    </Box>
  );
};

export default ServicesList;
