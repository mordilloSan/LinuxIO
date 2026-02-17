import { Box, Tooltip, Typography } from "@mui/material";
import React, { useMemo } from "react";

import { linuxio } from "@/api";
import GeneralCard from "@/components/cards/GeneralCard";
import DockerIcon from "@/components/docker/DockerIcon";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const stateColor: Record<string, string> = {
  running: "success.main",
  healthy: "success.main",
  exited: "error.main",
  unhealthy: "error.main",
  paused: "warning.main",
  restarting: "info.main",
};

const cleanName = (name: string) => name.replace(/^\//, "");

const getStatusLabel = (status: string, state: string): string => {
  const health = status.match(/\((\w+)\)/)?.[1];
  if (health === "healthy" || health === "unhealthy") return health;
  return state;
};

const DockerInfo: React.FC = () => {
  const {
    data: containers = [],
    isPending: isContainersLoading,
    isError: isContainersError,
  } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });

  const { data: images = [] } = linuxio.docker.list_images.useQuery({
    refetchInterval: 30_000,
  });

  const { data: networks = [] } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 30_000,
  });

  const { data: volumes = [] } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 30_000,
  });

  const runningCount = useMemo(
    () => containers.filter((c) => c.State === "running").length,
    [containers],
  );

  const sorted = useMemo(
    () =>
      [...containers].sort((a, b) => {
        if (a.State === "running" && b.State !== "running") return -1;
        if (a.State !== "running" && b.State === "running") return 1;
        return 0;
      }),
    [containers],
  );

  const stats = (
    <Box
      sx={{
        display: "flex",
        gap: 0.5,
        flexDirection: "column",
        alignSelf: "flex-start",
        mt: 4,
      }}
    >
      <Typography variant="body1">
        <strong>Containers:</strong> {runningCount}/{containers.length}
      </Typography>
      <Typography variant="body1">
        <strong>Images:</strong> {images.length}
      </Typography>
      <Typography variant="body1">
        <strong>Networks:</strong> {networks.length}
      </Typography>
      <Typography variant="body1">
        <strong>Volumes:</strong> {volumes.length}
      </Typography>
    </Box>
  );

  const stats2 = isContainersError ? (
    <ErrorMessage />
  ) : isContainersLoading ? (
    <ComponentLoader />
  ) : (
    <Box
      className="custom-scrollbar"
      sx={{
        display: "grid",
        gridTemplateColumns: {
          xs: "repeat(3, 36px)",
          sm: "repeat(4, 36px)",
        },
        gap: 2.5,
        justifyContent: "center",
        width: "100%",
        maxHeight: 90,
        overflowX: "hidden",
        overflowY: "auto",
        pr: 0.5,
        "&::-webkit-scrollbar-thumb": {
          backgroundColor: "transparent !important",
        },
        "&:hover::-webkit-scrollbar-thumb": {
          backgroundColor: "rgba(100, 100, 100, 0.2) !important",
        },
      }}
    >
      {sorted.map((c) => {
        const name = cleanName(c.Names[0] ?? c.Id.slice(0, 12));
        return (
          <Tooltip
            key={c.Id}
            title={
              <>
                {name}
                <br />
                <Box
                  component="span"
                  sx={{ color: stateColor[getStatusLabel(c.Status, c.State)] ?? "grey.500" }}
                >
                  {getStatusLabel(c.Status, c.State)}
                </Box>
              </>
            }
            arrow
            placement="top"
          >
            <Box
              sx={{
                position: "relative",
                width: 36,
                height: 36,
              }}
            >
              <DockerIcon identifier={c.icon} size={36} alt={name} />
              <Box
                sx={{
                  position: "absolute",
                  bottom: 0,
                  right: 0,
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  bgcolor: stateColor[c.State] ?? "grey.500",
                  border: "1.5px solid",
                  borderColor: "background.paper",
                }}
              />
            </Box>
          </Tooltip>
        );
      })}
    </Box>
  );

  return (
    <GeneralCard
      title="Docker"
      avatarIcon="mdi:docker"
      stats={stats}
      stats2={stats2}
      connectionStatus={isContainersError ? "offline" : "online"}
    />
  );
};

export default DockerInfo;
