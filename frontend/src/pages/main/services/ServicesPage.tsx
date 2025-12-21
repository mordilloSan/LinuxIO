import { Box, Alert } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState, useCallback } from "react";

import ServiceLogsDrawer from "./ServiceLogsDrawer";
import ServiceTable, { Service } from "./ServiceTable";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useStreamQuery } from "@/hooks/useStreamApi";
import { streamApi } from "@/utils/streamApi";

const ServicesList: React.FC = () => {
  const queryClient = useQueryClient();
  const [logsDrawerOpen, setLogsDrawerOpen] = useState(false);
  const [selectedService, setSelectedService] = useState<string>("");
  const [actionPending, setActionPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const { data, isPending: isLoading, isError, error } = useStreamQuery<Service[]>({
    handlerType: "dbus",
    command: "ListServices",
    refetchInterval: 2000,
  });

  // Service action handler with dynamic command
  const performServiceAction = useCallback(async (serviceName: string, action: string) => {
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
    if (!command) return;

    setActionPending(true);
    setActionError(null);
    try {
      await streamApi.get("dbus", command, [serviceName]);
      queryClient.invalidateQueries({ queryKey: ["stream", "dbus", "ListServices"] });
    } catch (err: any) {
      console.error("Service action failed:", err);
      setActionError(err.message || "Action failed");
    } finally {
      setActionPending(false);
    }
  }, [queryClient]);

  const handleRestart = (service: Service) => performServiceAction(service.name, "restart");
  const handleStop = (service: Service) => performServiceAction(service.name, "stop");
  const handleStart = (service: Service) => performServiceAction(service.name, "start");

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
      {actionError && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setActionError(null)}>
          {actionError}
        </Alert>
      )}
      {data && (
        <ServiceTable
          serviceList={data}
          onRestart={handleRestart}
          onStop={handleStop}
          onStart={handleStart}
          onViewLogs={handleViewLogs}
          isLoading={actionPending}
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
