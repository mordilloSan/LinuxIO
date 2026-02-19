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
import { Box, Grid, Typography } from "@mui/material";
import React, { Suspense, useMemo } from "react";

import ContainerCard from "../../../components/cards/ContainerCard";

import { linuxio } from "@/api";
import SortableCard from "@/components/cards/SortableCard";
import { useConfigValue } from "@/hooks/useConfig";

interface ContainerListProps {
  editMode: boolean;
}

const ContainerList: React.FC<ContainerListProps> = ({ editMode }) => {
  const { data: containers = [] } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });

  const [containerOrder, setContainerOrder] = useConfigValue("containerOrder");

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 2000, tolerance: 5 },
    }),
  );

  // Merge saved order with live containers:
  // - containers removed from Docker are dropped automatically
  // - new containers (not yet in the saved order) are appended at the end
  const orderedContainers = useMemo(() => {
    if (!containerOrder?.length) return containers;

    const containerMap = new Map(containers.map((c) => [c.Id, c]));
    const ordered: typeof containers = [];

    for (const id of containerOrder) {
      const c = containerMap.get(id);
      if (c) {
        ordered.push(c);
        containerMap.delete(id);
      }
    }

    for (const c of containerMap.values()) {
      ordered.push(c);
    }

    return ordered;
  }, [containers, containerOrder]);

  const containerIds = useMemo(
    () => orderedContainers.map((c) => c.Id),
    [orderedContainers],
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = containerIds.indexOf(active.id as string);
    const newIndex = containerIds.indexOf(over.id as string);
    setContainerOrder(arrayMove(containerIds, oldIndex, newIndex));
  };

  return (
    <Suspense fallback={<Typography>Loading containers...</Typography>}>
      <Box>
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext items={containerIds} strategy={rectSortingStrategy}>
            <Grid container spacing={2}>
              {orderedContainers.map((container) => (
                <Grid
                  key={container.Id}
                  size={{ xs: 12, sm: 4, md: 4, lg: 3, xl: 2 }}
                >
                  <SortableCard id={container.Id} editMode={editMode}>
                    <ContainerCard container={container} />
                  </SortableCard>
                </Grid>
              ))}
            </Grid>
          </SortableContext>
        </DndContext>
      </Box>
    </Suspense>
  );
};

export default ContainerList;
