import { Box, Alert } from "@mui/material";
import React, { useState, useCallback } from "react";
import { toast } from "sonner";

import ServiceLogsDrawer from "./ServiceLogsDrawer";
import ServiceTable, { Service } from "./ServiceTable";

import { linuxio } from "@/api/linuxio";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const ServicesList: React.FC = () => {
  const [logsDrawerOpen, setLogsDrawerOpen] = useState(false);
  const [selectedService, setSelectedService] = useState<string>("");

  const {
    data,
    isPending: isLoading,
    isError,
    error,
    refetch,
  } = linuxio.useCall<Service[]>("dbus", "ListServices", [], {
    refetchInterval: 2000,
  });

  // Service action mutation with dynamic command mapping
  const serviceActionMutation = linuxio.useMutate<
    unknown,
    { serviceName: string; action: string }
  >("dbus", "StartService", {
    mutationFn: async (variables) => {
      const { serviceName, action } = variables;
      const commandMap: Record<string, string> = {
        start: "StartService",
        stop: "StopService",
        restart: "RestartService",
        reload: "ReloadService",
        enable: "EnableService",
        disable: "DisableService",
        mask: "MaskService",
        unmask: "UnmaskService",
      };
      const command = commandMap[action];
      if (!command) {
        throw new Error(`Unknown service action: ${action}`);
      }
      return linuxio.request("dbus", command, [serviceName]);
    },
    onSuccess: (_, variables) => {
      const { serviceName, action } = variables;
      toast.success(`Service ${serviceName} ${action}ed successfully`);
      refetch();
    },
  });

  // Service action handler
  const performServiceAction = useCallback(
    (serviceName: string, action: string) => {
      serviceActionMutation.mutate({ serviceName, action });
    },
    [serviceActionMutation],
  );

  const handleRestart = (service: Service) =>
    performServiceAction(service.name, "restart");
  const handleStop = (service: Service) =>
    performServiceAction(service.name, "stop");
  const handleStart = (service: Service) =>
    performServiceAction(service.name, "start");

  const handleViewLogs = (service: Service) => {
    setSelectedService(service.name);
    setLogsDrawerOpen(true);
  };

  return (
    <Box>
      {isLoading && <ComponentLoader />}
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
