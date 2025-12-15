import { Box, Typography, Grid } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React, { Suspense } from "react";

import ContainerCard from "../../../components/cards/ContainerCard";

import { ContainerInfo } from "@/types/container";
import axios from "@/utils/axios";

const ContainerList: React.FC = () => {
  const { data: containers = [] } = useQuery<ContainerInfo[]>({
    queryKey: ["containers"],
    queryFn: async () => {
      const res = await axios.get("/docker/containers");
      return res.data ?? [];
    },
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
