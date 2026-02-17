import {
  closestCenter,
  DndContext,
  DragEndEvent,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  rectSortingStrategy,
  SortableContext,
} from "@dnd-kit/sortable";
import { DragIndicator } from "@mui/icons-material";
import { Box, Grid, IconButton, Tooltip } from "@mui/material";
import React, { useMemo, useState } from "react";

import DockerInfo from "./Docker";
import DriveInfo from "./Drive";
import FileSystem from "./FileSystem";
import GpuInfo from "./Gpu";
import Memory from "./Memory";
import MotherBoardInfo from "./MotherBoard";
import Network from "./Network";
import Processor from "./Processor";
import SortableCard from "../../../components/cards/SortableCard";
import SystemHealth from "./System";

import ErrorBoundary from "@/components/errors/ErrorBoundary";
import useAuth from "@/hooks/useAuth";
import { useConfigValue } from "@/hooks/useConfig";

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
  const [editMode, setEditMode] = useState(false);
  const [dashboardOrder, setDashboardOrder] =
    useConfigValue("dashboardOrder");

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  );

  const cards = useMemo(() => {
    const filtered = allCards.filter(
      (card) => card.id !== "docker" || dockerAvailable,
    );

    if (!dashboardOrder?.length) return filtered;

    const cardMap = new Map(filtered.map((c) => [c.id, c]));
    const ordered: typeof filtered = [];

    for (const id of dashboardOrder) {
      const card = cardMap.get(id);
      if (card) {
        ordered.push(card);
        cardMap.delete(id);
      }
    }

    // Append any cards not in saved order (new cards)
    for (const card of cardMap.values()) {
      ordered.push(card);
    }

    return ordered;
  }, [dashboardOrder, dockerAvailable]);

  const cardIds = useMemo(() => cards.map((c) => c.id), [cards]);

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = cardIds.indexOf(active.id as string);
    const newIndex = cardIds.indexOf(over.id as string);
    const newOrder = arrayMove(cardIds, oldIndex, newIndex);
    setDashboardOrder(newOrder);
  };

  return (
    <Box>
      <Box sx={{ display: "flex", justifyContent: "flex-end", mb: 1 }}>
        <Tooltip title={editMode ? "Lock layout" : "Edit layout"}>
          <IconButton
            onClick={() => setEditMode((prev) => !prev)}
            color={editMode ? "primary" : "default"}
            size="small"
          >
            <DragIndicator />
          </IconButton>
        </Tooltip>
      </Box>
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={cardIds} strategy={rectSortingStrategy}>
          <Grid container spacing={4}>
            {cards.map(({ id, component: CardComponent }) => (
              <Grid
                key={id}
                size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
              >
                <SortableCard id={id} editMode={editMode}>
                  <ErrorBoundary>
                    <CardComponent />
                  </ErrorBoundary>
                </SortableCard>
              </Grid>
            ))}
          </Grid>
        </SortableContext>
      </DndContext>
    </Box>
  );
};

export default Dashboard;
