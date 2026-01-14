import { Box } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
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

  const { mutate: startProjectMutation, isPending: isStarting } =
    linuxio.docker.compose_up.useMutation({
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

  const startProject = (projectName: string) =>
    startProjectMutation([projectName]);

  const { mutate: stopProjectMutation, isPending: isStopping } =
    linuxio.docker.compose_stop.useMutation({
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

  const stopProject = (projectName: string) =>
    stopProjectMutation([projectName]);

  const { mutate: restartProjectMutation, isPending: isRestarting } =
    linuxio.docker.compose_restart.useMutation({
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

  const restartProject = (projectName: string) =>
    restartProjectMutation([projectName]);

  const { mutate: downProjectMutation, isPending: isDowning } =
    linuxio.docker.compose_down.useMutation({
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

  const downProject = (projectName: string) =>
    downProjectMutation([projectName]);

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
