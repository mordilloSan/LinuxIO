import { Box, Typography, Grid } from "@mui/material";
import React, { Suspense } from "react";

import ContainerCard from "../../../components/cards/ContainerCard";

import { linuxio } from "@/api";

const ContainerList: React.FC = () => {
  const { data: containers = [] } = linuxio.docker.list_containers.useQuery({
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
