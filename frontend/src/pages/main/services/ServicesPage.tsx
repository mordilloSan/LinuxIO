import { Box, Alert } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useState, useCallback } from "react";
import { toast } from "sonner";

import ServiceLogsDrawer from "./ServiceLogsDrawer";
import ServiceTable, { Service } from "./ServiceTable";

import { linuxio } from "@/api";
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
  } = linuxio.dbus.list_services.useQuery({
    refetchInterval: 2000,
  });

  const invalidateServices = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.dbus.list_services.queryKey(),
    });
  }, [queryClient]);

  const pastTense = (action: string) => {
    const map: Record<string, string> = {
      start: "started",
      stop: "stopped",
      restart: "restarted",
      reload: "reloaded",
      enable: "enabled",
      disable: "disabled",
      mask: "masked",
      unmask: "unmasked",
    };
    return map[action] ?? `${action}ed`;
  };

  const startMutation = linuxio.dbus.start_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(
        `Service ${serviceName} ${pastTense("start")} successfully`,
      );
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(
          error,
          `Failed to start service ${serviceName}`,
        ),
      );
    },
  });

  const stopMutation = linuxio.dbus.stop_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(`Service ${serviceName} ${pastTense("stop")} successfully`);
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(error, `Failed to stop service ${serviceName}`),
      );
    },
  });

  const restartMutation = linuxio.dbus.restart_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(
        `Service ${serviceName} ${pastTense("restart")} successfully`,
      );
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(
          error,
          `Failed to restart service ${serviceName}`,
        ),
      );
    },
  });

  const reloadMutation = linuxio.dbus.reload_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(
        `Service ${serviceName} ${pastTense("reload")} successfully`,
      );
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(
          error,
          `Failed to reload service ${serviceName}`,
        ),
      );
    },
  });

  const enableMutation = linuxio.dbus.enable_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(
        `Service ${serviceName} ${pastTense("enable")} successfully`,
      );
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(
          error,
          `Failed to enable service ${serviceName}`,
        ),
      );
    },
  });

  const disableMutation = linuxio.dbus.disable_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(
        `Service ${serviceName} ${pastTense("disable")} successfully`,
      );
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(
          error,
          `Failed to disable service ${serviceName}`,
        ),
      );
    },
  });

  const maskMutation = linuxio.dbus.mask_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(`Service ${serviceName} ${pastTense("mask")} successfully`);
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(error, `Failed to mask service ${serviceName}`),
      );
    },
  });

  const unmaskMutation = linuxio.dbus.unmask_service.useMutation({
    onSuccess: (_, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.success(
        `Service ${serviceName} ${pastTense("unmask")} successfully`,
      );
      invalidateServices();
    },
    onError: (error, args) => {
      const serviceName = String(args?.[0] ?? "");
      toast.error(
        getMutationErrorMessage(
          error,
          `Failed to unmask service ${serviceName}`,
        ),
      );
    },
  });

  const isActionPending =
    startMutation.isPending ||
    stopMutation.isPending ||
    restartMutation.isPending ||
    reloadMutation.isPending ||
    enableMutation.isPending ||
    disableMutation.isPending ||
    maskMutation.isPending ||
    unmaskMutation.isPending;

  const performServiceAction = useCallback(
    (serviceName: string, action: string) => {
      switch (action) {
        case "start":
          startMutation.mutate([serviceName]);
          return;
        case "stop":
          stopMutation.mutate([serviceName]);
          return;
        case "restart":
          restartMutation.mutate([serviceName]);
          return;
        case "reload":
          reloadMutation.mutate([serviceName]);
          return;
        case "enable":
          enableMutation.mutate([serviceName]);
          return;
        case "disable":
          disableMutation.mutate([serviceName]);
          return;
        case "mask":
          maskMutation.mutate([serviceName]);
          return;
        case "unmask":
          unmaskMutation.mutate([serviceName]);
          return;
        default:
          toast.error(`Unknown service action: ${action}`);
      }
    },
    [
      disableMutation,
      enableMutation,
      maskMutation,
      reloadMutation,
      restartMutation,
      startMutation,
      stopMutation,
      unmaskMutation,
    ],
  );

  // Service action handlers
  const handleRestart = useCallback(
    (service: Service) => performServiceAction(service.name, "restart"),
    [performServiceAction],
  );

  const handleStop = useCallback(
    (service: Service) => performServiceAction(service.name, "stop"),
    [performServiceAction],
  );

  const handleStart = useCallback(
    (service: Service) => performServiceAction(service.name, "start"),
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
