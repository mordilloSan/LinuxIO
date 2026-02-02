import { Box, Alert } from "@mui/material";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import React, { useState, useCallback } from "react";
import { toast } from "sonner";

import ServiceLogsDrawer from "./ServiceLogsDrawer";
import ServiceTable, { Service } from "./ServiceTable";

import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { getMutationErrorMessage } from "@/utils/mutations";

const ServicesList: React.FC = () => {
  const [logsDrawerOpen, setLogsDrawerOpen] = useState(false);
  const [selectedService, setSelectedService] = useState<string>("");
  const queryClient = useQueryClient();

  const {
    data,
    isPending: isLoading,
    isError,
    error,
  } = linuxio.dbus.ListServices.useQuery({
    refetchInterval: 2000,
  });

  // Service action mutation with dynamic command mapping
  const { mutate: performServiceAction, isPending: isActionPending } =
    useMutation({
      mutationFn: async (variables: {
        serviceName: string;
        action: string;
      }) => {
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
        return linuxio.call("dbus", command, [serviceName]);
      },
      onSuccess: (_, variables) => {
        const { serviceName, action } = variables;
        toast.success(`Service ${serviceName} ${action}ed successfully`);
        queryClient.invalidateQueries({
          queryKey: ["linuxio", "dbus", "ListServices"],
        });
      },
      onError: (error: Error, variables) => {
        const { serviceName, action } = variables;
        toast.error(
          getMutationErrorMessage(
            error,
            `Failed to ${action} service ${serviceName}`,
          ),
        );
      },
    });

  // Service action handlers
  const handleRestart = useCallback(
    (service: Service) =>
      performServiceAction({ serviceName: service.name, action: "restart" }),
    [performServiceAction],
  );

  const handleStop = useCallback(
    (service: Service) =>
      performServiceAction({ serviceName: service.name, action: "stop" }),
    [performServiceAction],
  );

  const handleStart = useCallback(
    (service: Service) =>
      performServiceAction({ serviceName: service.name, action: "start" }),
    [performServiceAction],
  );

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
          isLoading={isActionPending}
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
