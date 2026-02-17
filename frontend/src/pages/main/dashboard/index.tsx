import { Grid } from "@mui/material";
import React from "react";

import DockerInfo from "./Docker";
import DriveInfo from "./Drive";
import FileSystem from "./FileSystem";
import GpuInfo from "./Gpu";
import Memory from "./Memory";
import MotherBoardInfo from "./MotherBoard";
import Network from "./Network";
import Processor from "./Processor";
import SystemHealth from "./System";

import ErrorBoundary from "@/components/errors/ErrorBoundary";
import useAuth from "@/hooks/useAuth";

const MemoSystemHealth = React.memo(SystemHealth);
const MemoProcessor = React.memo(Processor);
const MemoMemory = React.memo(Memory);
const MemoFileSystem = React.memo(FileSystem);
const MemoNetwork = React.memo(Network);
const MemoMotherBoardInfo = React.memo(MotherBoardInfo);
const MemoGpuInfo = React.memo(GpuInfo);
const MemoDriveInfo = React.memo(DriveInfo);
const MemoDockerInfo = React.memo(DockerInfo);

const allCards = [
  { id: "system", component: MemoSystemHealth },
  { id: "cpu", component: MemoProcessor },
  { id: "memory", component: MemoMemory },
  { id: "docker", component: MemoDockerInfo },
  { id: "nic", component: MemoNetwork },
  { id: "fs", component: MemoFileSystem },
  { id: "mb", component: MemoMotherBoardInfo },
  { id: "gpu", component: MemoGpuInfo },
  { id: "drive", component: MemoDriveInfo },
];

const Dashboard: React.FC = () => {
  const { dockerAvailable } = useAuth();

  const cards = allCards.filter(
    (card) => card.id !== "docker" || dockerAvailable,
  );

  return (
    <Grid container spacing={4}>
      {cards.map(({ id, component: CardComponent }) => (
        <Grid key={id} size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}>
          <ErrorBoundary>
            <CardComponent />
          </ErrorBoundary>
        </Grid>
      ))}
    </Grid>
  );
};

export default Dashboard;
