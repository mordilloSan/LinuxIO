import { Box } from "@mui/material";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import React, { Suspense } from "react";
import { toast } from "sonner";

import ComposeList from "./ComposeList";

import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const ComposeStacksPage: React.FC = () => {
  const queryClient = useQueryClient();

  const { data: projects = [], isPending } =
    linuxio.docker.list_compose_projects.useQuery({
      refetchInterval: 5000,
    });

  const { mutate: startProject, isPending: isStarting } = useMutation({
    mutationFn: async (projectName: string) => {
      return linuxio.call("docker", "compose_up", [projectName]);
    },
    onSuccess: () => {
      toast.success("Stack started successfully");
      queryClient.invalidateQueries({
        queryKey: ["docker", "list_compose_projects"],
      });
    },
    onError: (error: Error) => {
      toast.error(`Failed to start stack: ${error.message}`);
    },
  });

  const { mutate: stopProject, isPending: isStopping } = useMutation({
    mutationFn: async (projectName: string) => {
      return linuxio.call("docker", "compose_stop", [projectName]);
    },
    onSuccess: () => {
      toast.success("Stack stopped successfully");
      queryClient.invalidateQueries({
        queryKey: ["docker", "list_compose_projects"],
      });
    },
    onError: (error: Error) => {
      toast.error(`Failed to stop stack: ${error.message}`);
    },
  });

  const { mutate: restartProject, isPending: isRestarting } = useMutation({
    mutationFn: async (projectName: string) => {
      return linuxio.call("docker", "compose_restart", [projectName]);
    },
    onSuccess: () => {
      toast.success("Stack restarted successfully");
      queryClient.invalidateQueries({
        queryKey: ["docker", "list_compose_projects"],
      });
    },
    onError: (error: Error) => {
      toast.error(`Failed to restart stack: ${error.message}`);
    },
  });

  const { mutate: downProject, isPending: isDowning } = useMutation({
    mutationFn: async (projectName: string) => {
      return linuxio.call("docker", "compose_down", [projectName]);
    },
    onSuccess: () => {
      toast.success("Stack removed successfully");
      queryClient.invalidateQueries({
        queryKey: ["docker", "list_compose_projects"],
      });
    },
    onError: (error: Error) => {
      toast.error(`Failed to remove stack: ${error.message}`);
    },
  });

  const isLoading = isStarting || isStopping || isRestarting || isDowning;

  return (
    <Suspense fallback={<ComponentLoader />}>
      <Box>
        {isPending ? (
          <ComponentLoader />
        ) : (
          <ComposeList
            projects={projects}
            onStart={startProject}
            onStop={stopProject}
            onRestart={restartProject}
            onDown={downProject}
            isLoading={isLoading}
          />
        )}
      </Box>
    </Suspense>
  );
};

export default ComposeStacksPage;
