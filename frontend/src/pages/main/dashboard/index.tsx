import {
  closestCenter,
  DndContext,
  DragEndEvent,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  rectSortingStrategy,
  SortableContext,
} from "@dnd-kit/sortable";
import { DragIndicator, Settings } from "@mui/icons-material";
import {
  Box,
  Checkbox,
  FormControlLabel,
  Grid,
  IconButton,
  Popover,
  Tooltip,
} from "@mui/material";
import React, { useCallback, useMemo, useState } from "react";

import DockerInfo from "./Docker";
import DriveInfo from "./Drive";
import FileSystem from "./FileSystem";
import GpuInfo from "./Gpu";
import Memory from "./Memory";
import MotherBoardInfo from "./MotherBoard";
import Network from "./Network";
import Processor from "./Processor";
import SystemHealth from "./System";

import SortableCard from "@/components/cards/SortableCard";
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
  { id: "system", label: "System Health", component: MemoSystemHealth },
  { id: "cpu", label: "Processor", component: MemoProcessor },
  { id: "memory", label: "Memory", component: MemoMemory },
  { id: "docker", label: "Docker", component: MemoDockerInfo },
  { id: "nic", label: "Network", component: MemoNetwork },
  { id: "fs", label: "File System", component: MemoFileSystem },
  { id: "mb", label: "Motherboard", component: MemoMotherBoardInfo },
  { id: "gpu", label: "GPU", component: MemoGpuInfo },
  { id: "drive", label: "Drive", component: MemoDriveInfo },
];

const Dashboard: React.FC = () => {
  const { dockerAvailable } = useAuth();
  const [editMode, setEditMode] = useState(false);
  const [dashboardOrder, setDashboardOrder] = useConfigValue("dashboardOrder");
  const [hiddenCards, setHiddenCards] = useConfigValue("hiddenCards");
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 2000, tolerance: 5 },
    }),
  );

  // Cards available (docker filtered by availability)
  const availableCards = useMemo(
    () => allCards.filter((card) => card.id !== "docker" || dockerAvailable),
    [dockerAvailable],
  );

  // Cards sorted by order and filtered by visibility
  const cards = useMemo(() => {
    const hiddenSet = new Set(hiddenCards ?? []);
    const visible = availableCards.filter((c) => !hiddenSet.has(c.id));

    if (!dashboardOrder?.length) return visible;

    const cardMap = new Map(visible.map((c) => [c.id, c]));
    const ordered: typeof visible = [];

    for (const id of dashboardOrder) {
      const card = cardMap.get(id);
      if (card) {
        ordered.push(card);
        cardMap.delete(id);
      }
    }

    for (const card of cardMap.values()) {
      ordered.push(card);
    }

    return ordered;
  }, [dashboardOrder, hiddenCards, availableCards]);

  const cardIds = useMemo(() => cards.map((c) => c.id), [cards]);

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = cardIds.indexOf(active.id as string);
    const newIndex = cardIds.indexOf(over.id as string);
    const newOrder = arrayMove(cardIds, oldIndex, newIndex);
    setDashboardOrder(newOrder);
  };

  const toggleCard = useCallback(
    (id: string) => {
      const current = hiddenCards ?? [];
      const next = current.includes(id)
        ? current.filter((c) => c !== id)
        : [...current, id];
      setHiddenCards(next);
    },
    [hiddenCards, setHiddenCards],
  );

  return (
    <Box>
      <Box sx={{ display: "flex", justifyContent: "flex-end", mb: 1 }}>
        <Tooltip title="Card visibility">
          <IconButton
            onClick={(e) => setAnchorEl(e.currentTarget)}
            size="small"
          >
            <Settings />
          </IconButton>
        </Tooltip>
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

      <Popover
        open={Boolean(anchorEl)}
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <Box sx={{ p: 2, display: "flex", flexDirection: "column" }}>
          {availableCards.map((card) => (
            <FormControlLabel
              key={card.id}
              control={
                <Checkbox
                  checked={!hiddenCards?.includes(card.id)}
                  onChange={() => toggleCard(card.id)}
                  size="small"
                />
              }
              label={card.label}
            />
          ))}
        </Box>
      </Popover>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={cardIds} strategy={rectSortingStrategy}>
          <Grid container spacing={4}>
            {cards.map(({ id, component: CardComponent }) => (
              <Grid key={id} size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}>
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
