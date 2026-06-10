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
import { motion } from "framer-motion";
import React, {
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useSearchParams } from "react-router-dom";

import ContainerDetailsPanel from "./ContainerDetailsPanel";
import ContainerTable from "./ContainerTable";
import ContainerCard from "../../../components/cards/ContainerCard";

import { linuxio, openDockerLogsStream } from "@/api";
import SortableCard from "@/components/cards/SortableCard";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppMediaQuery, useAppTheme } from "@/theme";

interface ContainerListProps {
  editMode: boolean;
  viewMode?: "card" | "table";
}

const ContainerList: React.FC<ContainerListProps> = ({
  editMode,
  viewMode = "card",
}) => {
  const theme = useAppTheme();
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const [searchParams, setSearchParams] = useSearchParams();
  const { data: rawContainers } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });
  const hasLoadedContainers = rawContainers !== undefined;
  const containers = useMemo(() => rawContainers ?? [], [rawContainers]);
  const selectedContainerId = searchParams.get("container");
  const [search, setSearch] = useState("");

  const [containerOrder, setContainerOrder] = useConfigValue("containerOrder");

  const updateSelectedContainer = useCallback(
    (containerId: string | null) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        if (containerId) {
          next.set("container", containerId);
        } else {
          next.delete("container");
        }
        return next;
      });
    },
    [setSearchParams],
  );

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
  const filteredContainers = useMemo(() => {
    const searchText = search.trim().toLowerCase();
    if (!searchText) return orderedContainers;

    return orderedContainers.filter((container) => {
      const name = container.Names?.[0]?.replace("/", "") ?? "";
      return (
        name.toLowerCase().includes(searchText) ||
        container.Image.toLowerCase().includes(searchText) ||
        container.State.toLowerCase().includes(searchText) ||
        container.Status.toLowerCase().includes(searchText) ||
        container.Id.toLowerCase().includes(searchText)
      );
    });
  }, [orderedContainers, search]);
  const selectedContainer = useMemo(
    () =>
      orderedContainers.find(
        (container) => container.Id === selectedContainerId,
      ) ?? null,
    [orderedContainers, selectedContainerId],
  );

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;

    const oldIndex = containerIds.indexOf(active.id as string);
    const newIndex = containerIds.indexOf(over.id as string);
    setContainerOrder(arrayMove(containerIds, oldIndex, newIndex));
  };

  useEffect(() => {
    if (editMode) {
      updateSelectedContainer(null);
    }
  }, [editMode, updateSelectedContainer]);

  useEffect(() => {
    if (hasLoadedContainers && selectedContainerId && !selectedContainer) {
      updateSelectedContainer(null);
    }
  }, [
    hasLoadedContainers,
    selectedContainer,
    selectedContainerId,
    updateSelectedContainer,
  ]);

  useEffect(() => {
    if (!selectedContainer) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" || event.key === "Esc") {
        updateSelectedContainer(null);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [selectedContainer, updateSelectedContainer]);

  const handleSelectContainer = (containerId: string) => {
    updateSelectedContainer(
      selectedContainerId === containerId ? null : containerId,
    );
  };

  if (viewMode === "table") {
    return (
      <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
        <DndContext
          collisionDetection={closestCenter}
          onDragEnd={handleDragEnd}
          sensors={sensors}
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

  if (editMode) {
    return (
      <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
        <div>
          <DndContext
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
            sensors={sensors}
          >
            <SortableContext
              items={containerIds}
              strategy={rectSortingStrategy}
            >
              <AppGrid container spacing={2}>
                {orderedContainers.map((container) => (
                  <AppGrid
                    key={container.Id}
                    size={{ xs: 12, sm: 6, md: 4, lg: 2 }}
                  >
                    <SortableCard editMode id={container.Id}>
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
  }

  return (
    <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
      {!selectedContainer && (
        <div
          style={{
            marginBottom: theme.spacing(2),
            display: "flex",
            alignItems: "center",
            gap: theme.spacing(2),
          }}
        >
          <AppSearchField
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search containers…"
            style={{ width: 320 }}
            value={search}
          />
          <AppTypography fontWeight={700}>
            {filteredContainers.length} shown
          </AppTypography>
        </div>
      )}

      <motion.div
        layout="position"
        transition={{ duration: 0.65, ease: [0.22, 1, 0.36, 1] }}
      >
        {selectedContainer ? (
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: theme.spacing(3),
            }}
          >
            <motion.div
              animate={{ opacity: 1, y: 0 }}
              initial={{ opacity: 0, y: 14 }}
              style={{
                display: "flex",
                flexDirection: isCompactLayout ? "column" : "row",
                alignItems: "stretch",
                gap: theme.spacing(2.5),
              }}
              transition={{
                duration: 0.65,
                delay: 0.04,
                ease: [0.22, 1, 0.36, 1],
              }}
            >
              <motion.div
                animate={{ opacity: 1, y: 0 }}
                initial={{ opacity: 0, y: 12 }}
                style={{
                  flex: isCompactLayout ? "0 0 auto" : 1,
                  width: isCompactLayout ? "100%" : undefined,
                  display: "flex",
                }}
                transition={{
                  duration: 0.65,
                  delay: 0.04,
                  ease: [0.22, 1, 0.36, 1],
                }}
              >
                <ContainerCard
                  container={selectedContainer}
                  onSelect={() => handleSelectContainer(selectedContainer.Id)}
                  selected
                />
              </motion.div>
              <motion.div
                animate={{ opacity: 1, x: 0, y: 0 }}
                initial={{
                  opacity: 0,
                  x: isCompactLayout ? 0 : 40,
                  y: isCompactLayout ? 20 : 0,
                }}
                style={{
                  width: isCompactLayout ? "100%" : "33.33%",
                  flexShrink: 0,
                  display: "flex",
                }}
                transition={{
                  duration: 0.65,
                  delay: 0.08,
                  ease: [0.22, 1, 0.36, 1],
                }}
              >
                <ContainerDetailsPanel
                  container={selectedContainer}
                  onClose={() => updateSelectedContainer(null)}
                />
              </motion.div>
            </motion.div>
            <motion.div
              animate={{ opacity: 1, y: 0 }}
              initial={{ opacity: 0, y: 20 }}
              transition={{
                duration: 0.65,
                delay: 0.16,
                ease: [0.22, 1, 0.36, 1],
              }}
            >
              <UnitLogsCard
                key={selectedContainer.Id}
                createStream={(tail) =>
                  openDockerLogsStream(selectedContainer.Id, tail)
                }
                title="Container Logs"
              />
            </motion.div>
          </div>
        ) : (
          <div>
            {filteredContainers.length === 0 ? (
              <div style={{ textAlign: "center", padding: "32px 0" }}>
                <AppTypography color="text.secondary" variant="body2">
                  No containers found.
                </AppTypography>
              </div>
            ) : (
              <AppGrid container spacing={2}>
                {filteredContainers.map((container) => (
                  <AppGrid
                    key={container.Id}
                    size={{ xs: 12, sm: 6, md: 4, lg: 2 }}
                  >
                    <ContainerCard
                      container={container}
                      onSelect={() => handleSelectContainer(container.Id)}
                    />
                  </AppGrid>
                ))}
              </AppGrid>
            )}
          </div>
        )}
      </motion.div>
    </Suspense>
  );
};

export default ContainerList;
