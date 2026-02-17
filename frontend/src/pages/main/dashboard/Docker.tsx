import { Box, Typography, useTheme } from "@mui/material";
import React, { useMemo } from "react";

import { linuxio } from "@/api";
import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";

const stateColor: Record<string, string> = {
  running: "success.main",
  exited: "error.main",
  paused: "warning.main",
  restarting: "info.main",
};

const cleanName = (name: string) => name.replace(/^\//, "");

const DockerInfo: React.FC = () => {
  const theme = useTheme();

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
        display: "flex",
        flexDirection: "column",
        gap: 0.5,
        width: "100%",
        maxHeight: 110,
        overflowX: "hidden",
        overflowY: "hidden",
        pr: 0.5,
        "&:hover": {
          overflowY: "auto",
        },
      }}
    >
      {sorted.map((c) => (
        <Box
          key={c.Id}
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 1,
            px: 1,
            py: 0.3,
            borderRadius: 1,
            bgcolor: theme.palette.action.hover,
            flexShrink: 0,
          }}
        >
          <Box
            sx={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              bgcolor: stateColor[c.State] ?? "grey.500",
              flexShrink: 0,
            }}
          />
          <Typography
            variant="body2"
            sx={{
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              flex: 1,
            }}
          >
            {cleanName(c.Names[0] ?? c.Id.slice(0, 12))}
          </Typography>
        </Box>
      ))}
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
