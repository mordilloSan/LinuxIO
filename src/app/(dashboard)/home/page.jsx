import React from "react";
import { Grid } from "@mui/material";
import Memory from "./Memory";
import SystemHealth from "./SystemHealth";
import FileSystem from "./FileSystem";
import Processor from "./Processor";

const Dashboard = () => {
  return (
    <Grid container spacing={4}>
      <Grid item xs={12} md={6} lg={3}>
        <SystemHealth />
      </Grid>
      <Grid item xs={12} md={6} lg={3}>
        <Processor />
      </Grid>
      <Grid item xs={12} md={6} lg={3}>
        <Memory />
      </Grid>
      <Grid item xs={12} md={6} lg={3}>
        <FileSystem />
      </Grid>
    </Grid>
  );
};

export default Dashboard;
