import { Box, Typography, Grid } from "@mui/material";
import React, { Suspense } from "react";

import ContainerCard from "../../../components/cards/ContainerCard";

import { useStreamQuery } from "@/hooks/useStreamApi";
import { ContainerInfo } from "@/types/container";

const ContainerList: React.FC = () => {
  const { data: containers = [] } = useStreamQuery<ContainerInfo[]>({
    handlerType: "docker",
    command: "list_containers",
    refetchInterval: 5000,
  });

  return (
    <Suspense fallback={<Typography>Loading containers...</Typography>}>
      <Box>
        <Grid container spacing={2}>
          {containers.map((container) => (
            <ContainerCard key={container.Id} container={container} />
          ))}
        </Grid>
      </Box>
    </Suspense>
  );
};

export default ContainerList;
