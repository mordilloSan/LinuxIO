import { Box, Typography } from "@mui/material";
import React from "react";

import { linuxio } from "@/api";
import GeneralCard from "@/components/cards/GeneralCard";
import ErrorMessage from "@/components/errors/Error";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import ComponentLoader from "@/components/loaders/ComponentLoader";

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

  const runningCount = containers.filter(
    (c) => c.State === "running",
  ).length;
  const totalCount = containers.length;
  const runningPercent = totalCount > 0 ? (runningCount / totalCount) * 100 : 0;

  const data = {
    title: "Docker",
    avatarIcon: "mdi:docker",
    connectionStatus: isContainersError
      ? ("offline" as const)
      : ("online" as const),
    stats2: isContainersError ? (
      <ErrorMessage />
    ) : isContainersLoading ? (
      <ComponentLoader />
    ) : (
      <GradientCircularGauge
        value={runningPercent}
        gradientColors={["#ef4444", "#eab308", "#82ca9d"]}
        size={108}
        thickness={9.8}
        showPercentage={true}
      />
    ),
    stats: (
      <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
        <Typography variant="body1">
          <strong>Containers:</strong> {runningCount} running / {totalCount}{" "}
          total
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
    ),
  };

  return <GeneralCard {...data} />;
};

export default DockerInfo;
