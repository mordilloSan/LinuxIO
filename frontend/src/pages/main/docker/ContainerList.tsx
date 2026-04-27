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
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import React, { Suspense, useMemo } from "react";

import ContainerTable from "./ContainerTable";
import ContainerCard from "../../../components/cards/ContainerCard";

import { linuxio } from "@/api";
import SortableCard from "@/components/cards/SortableCard";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";

interface ContainerListProps {
  editMode: boolean;
  viewMode?: "card" | "table";
}

const ContainerList: React.FC<ContainerListProps> = ({
  editMode,
  viewMode = "card",
}) => {
  const { data: rawContainers } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });
  const containers = useMemo(() => rawContainers ?? [], [rawContainers]);

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

  if (viewMode === "table") {
    return (
      <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={containerIds}
            strategy={verticalListSortingStrategy}
          >
            <ContainerTable
              containers={orderedContainers}
              editMode={editMode}
            />
          </SortableContext>
        </DndContext>
      </Suspense>
    );
  }

  return (
    <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
      <div>
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
        >
          <SortableContext items={containerIds} strategy={rectSortingStrategy}>
            <AppGrid container spacing={2}>
              {orderedContainers.map((container) => (
                <AppGrid
                  key={container.Id}
                  size={{ xs: 12, sm: 6, md: 4, lg: 2 }}
                >
                  <SortableCard id={container.Id} editMode={editMode}>
                    <ContainerCard container={container} />
                  </SortableCard>
                </AppGrid>
              ))}
            </AppGrid>
          </SortableContext>
        </DndContext>
      </div>
    </Suspense>
  );
};

export default ContainerList;
