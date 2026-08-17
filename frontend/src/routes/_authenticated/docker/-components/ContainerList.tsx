import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { motion } from "motion/react";
import {
  memo,
  Suspense,
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useState,
} from "react";

import { linuxio, openChannel, type ContainerInfo } from "@/api";
import ContainerCard from "@/components/cards/ContainerCard";
import SortableCard from "@/components/cards/SortableCard";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppGrid, { type GridSize } from "@/components/ui/AppGrid";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import {
  CARD_GRID_SIZE_DENSE,
  DASHBOARD_CARD_SPACING,
  DETAIL_PANEL_GAP,
  EASING_STANDARD,
  TRANSITION_DURATION_SLOW_MS,
} from "@/theme/constants";

import ContainerDetailsPanel from "./ContainerDetailsPanel";
import {
  ContainerStackBand,
  ContainerStackSummaryCard,
} from "./ContainerStackGroup";
import {
  COMPOSE_PROJECT_LABEL,
  getComposeProject,
  getStackDragId,
  groupContainersByStack,
  resolveStackDrag,
  type ContainerTableRow,
} from "./containerStacks";
import ContainerTable from "./ContainerTable";

// Card mode only needs identity/display fields in this parent.  Keeping the
// volatile metrics out of the selected result means a metrics-only poll can
// update the per-card observers without rebuilding the grid shells.
const selectContainerSearchStatus = (container: ContainerInfo): string => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "unhealthy";
  if (status.includes("healthy")) return "healthy";
  return container.State;
};

// Of the labels, only the compose project survives into card mode: stack
// grouping needs it, and it is as stable as the identity fields around it.
const selectComposeProjectLabel = (labels?: Record<string, string>) => {
  const project = labels?.[COMPOSE_PROJECT_LABEL];
  return project ? { [COMPOSE_PROJECT_LABEL]: project } : undefined;
};

const selectCardContainers = (
  containers: readonly ContainerInfo[],
): ContainerInfo[] =>
  containers.map((container) => ({
    Created: container.Created,
    Id: container.Id,
    Image: container.Image,
    Labels: selectComposeProjectLabel(container.Labels),
    Names: container.Names,
    State: container.State,
    Status: selectContainerSearchStatus(container),
  }));

const selectAllContainers = (containers: ContainerInfo[]) => containers;

interface ContainerListProps {
  checkingUpdates?: boolean;
  stoppingContainerIds?: ReadonlySet<string>;
  viewMode?: "card" | "table";
}

const EMPTY_STOPPING_CONTAINER_IDS = new Set<string>();
const dockerRouteApi = getRouteApi("/_authenticated/docker/containers");
const getContainerId = (container: { Id: string }) => container.Id;

// Both derive from CARD_GRID_SIZE_DENSE (spans 12/6/4/2, so 1/2/3/6 cards per
// row): an expanded stack band spans exactly the columns its member cards
// would take loose, capped at the full row, instead of always stretching to
// the end — and its inner grid deals the members out at that same width.
const getStackBandSize = (memberCount: number): GridSize => ({
  xs: 12,
  sm: Math.min(memberCount * 6, 12),
  md: Math.min(memberCount * 4, 12),
  lg: Math.min(memberCount * 2, 12),
});

const getStackBandColumns = (memberCount: number): GridSize => ({
  xs: 1,
  sm: Math.min(memberCount, 2),
  md: Math.min(memberCount, 3),
  lg: Math.min(memberCount, 6),
});

const ContainerList = ({
  checkingUpdates = false,
  stoppingContainerIds = EMPTY_STOPPING_CONTAINER_IDS,
  viewMode = "card",
}: ContainerListProps) => {
  const theme = useAppTheme();
  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const isCompactLayout = useAppMediaQuery(theme.breakpoints.down("md"));
  const navigate = dockerRouteApi.useNavigate();
  const searchParams = dockerRouteApi.useSearch();
  const selectedContainerId =
    typeof searchParams.container === "string"
      ? searchParams.container
      : undefined;
  // The selected layout and table need the complete row. The collapsed card
  // grid needs only stable identity/display fields; each card reads its live
  // values from the same cache below the query-free shell.
  const selectContainers =
    viewMode === "card" && !selectedContainerId
      ? selectCardContainers
      : selectAllContainers;
  const { data: rawContainers } = useSuspenseQuery({
    ...linuxio.docker.list_containers,
    select: selectContainers,
    refetchInterval: 5000,
  });
  const containers = rawContainers;
  const [search, setSearch] = useState("");
  // Stacks folded to one row/card. Owned here so the fold survives the
  // card/table toggle; a fresh session starts fully expanded.
  const [collapsedStacks, setCollapsedStacks] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const toggleStack = useCallback((project: string) => {
    setCollapsedStacks((previous) => {
      const next = new Set(previous);
      if (!next.delete(project)) next.add(project);
      return next;
    });
  }, []);

  const updateSelectedContainer = useCallback(
    (containerId: string | null) => {
      void navigate({
        to: "/docker/containers",
        search: (previous) => ({
          ...previous,
          container: containerId ?? undefined,
        }),
      });
    },
    [navigate],
  );

  // The saved order is flat container ids; a drag that involves a stack band
  // moves the whole member block through it. The resolver needs the id→project
  // mapping to rebuild the blocks from the flat order.
  const projectById = useMemo(() => {
    const map = new Map<string, string>();
    for (const container of containers) {
      const project = getComposeProject(container);
      if (project) map.set(container.Id, project);
    }
    return map;
  }, [containers]);
  const resolveStackDragEnd = useCallback(
    (ids: readonly string[], activeId: string, overId: string) =>
      resolveStackDrag(ids, activeId, overId, projectById),
    [projectById],
  );
  const surface = useReorderableSurface({
    getId: getContainerId,
    items: containers,
    resolveDragEnd: resolveStackDragEnd,
    surface: "docker.containers",
  });
  const { editMode, items: orderedContainers, pendingId } = surface;
  const tableDnd = useReorderableTableDnd<
    ContainerTableRow,
    (typeof containers)[number]
  >({
    handleAriaLabel: "Reorder container",
    handleColumnWidth: 28,
    surface,
  });

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
        container.Id.toLowerCase().includes(searchText) ||
        (getComposeProject(container)?.toLowerCase().includes(searchText) ??
          false)
      );
    });
  }, [orderedContainers, search]);
  // Grouping runs after the search filter, so a query that leaves a stack one
  // member simply shows that member loose. Layout mode flattens the groups —
  // the saved order being rearranged is the flat list.
  const stackEntries = useMemo(
    () => groupContainersByStack(filteredContainers),
    [filteredContainers],
  );
  const hasStacks = stackEntries.some((entry) => entry.type === "stack");
  // SortableContext items in display order: the bands' drag ids sort alongside
  // the container ids they stand for.
  const cardSortableIds = useMemo(() => {
    const ids: string[] = [];
    for (const entry of stackEntries) {
      if (entry.type === "container") {
        ids.push(entry.container.Id);
        continue;
      }
      ids.push(getStackDragId(entry.project));
      for (const container of entry.containers) ids.push(container.Id);
    }
    return ids;
  }, [stackEntries]);
  const selectedContainer = useMemo(
    () =>
      orderedContainers.find(
        (container) => container.Id === selectedContainerId,
      ) ?? null,
    [orderedContainers, selectedContainerId],
  );

  const clearSelectedContainer = useEffectEvent(() => {
    updateSelectedContainer(null);
  });

  useEffect(() => {
    if (selectedContainerId && !selectedContainer) {
      clearSelectedContainer();
    }
  }, [selectedContainer, selectedContainerId]);

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

  // Selecting a container replaces whichever list you came from with the same
  // detail layout, so the table gets the card view's detail for free.
  if (selectedContainer) {
    return (
      <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
        <motion.div
          layout="position"
          transition={{
            duration: slowTransitionDurationSeconds,
            ease: EASING_STANDARD,
          }}
        >
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
                gap: DETAIL_PANEL_GAP,
              }}
              transition={{
                duration: slowTransitionDurationSeconds,
                delay: 0.04,
                ease: EASING_STANDARD,
              }}
            >
              <motion.div
                animate={{ opacity: 1, y: 0 }}
                initial={{ opacity: 0, y: 12 }}
                style={{
                  flex: isCompactLayout ? "0 0 auto" : 1,
                  width: isCompactLayout ? "100%" : undefined,
                  minWidth: 0,
                  display: "flex",
                }}
                transition={{
                  duration: slowTransitionDurationSeconds,
                  delay: 0.04,
                  ease: EASING_STANDARD,
                }}
              >
                <ContainerCard
                  actionPending={stoppingContainerIds.has(selectedContainer.Id)}
                  containerId={selectedContainer.Id}
                  onSelect={() => handleSelectContainer(selectedContainer.Id)}
                  selected
                />
              </motion.div>
              <motion.div
                animate={{ opacity: 1, y: 0 }}
                initial={{ opacity: 0, y: 16 }}
                style={{
                  flex: isCompactLayout ? "0 0 auto" : 1,
                  width: isCompactLayout ? "100%" : undefined,
                  minWidth: 0,
                  display: "flex",
                }}
                transition={{
                  duration: slowTransitionDurationSeconds,
                  delay: 0.08,
                  ease: EASING_STANDARD,
                }}
              >
                <ContainerDetailsPanel
                  container={selectedContainer}
                  sections={["ports", "volumes"]}
                  showStatus={false}
                  subtitle="ports and volumes"
                  title="Container"
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
                  flex: isCompactLayout ? "0 0 auto" : 1,
                  width: isCompactLayout ? "100%" : undefined,
                  minWidth: 0,
                  display: "flex",
                }}
                transition={{
                  duration: slowTransitionDurationSeconds,
                  delay: 0.12,
                  ease: EASING_STANDARD,
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
                duration: slowTransitionDurationSeconds,
                delay: 0.16,
                ease: EASING_STANDARD,
              }}
            >
              <UnitLogsCard
                key={selectedContainer.Id}
                createStream={(tail) =>
                  openChannel("docker.logs.follow", {
                    containerId: selectedContainer.Id,
                    tail,
                  })
                }
                title="Container Logs"
              />
            </motion.div>
          </div>
        </motion.div>
      </Suspense>
    );
  }

  if (viewMode === "table") {
    const table = (
      <ContainerTable
        checkingUpdates={checkingUpdates}
        collapsedStackIds={collapsedStacks}
        containers={orderedContainers}
        dnd={tableDnd}
        onSelectContainer={updateSelectedContainer}
        onToggleStack={toggleStack}
        stoppingContainerIds={stoppingContainerIds}
      />
    );

    return (
      <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
        {table}
      </Suspense>
    );
  }

  return (
    <Suspense fallback={<AppTypography>Loading containers...</AppTypography>}>
      <RoutedTabSearch active={search !== ""}>
        <AppHeaderSearch
          clearOnDocumentEscape
          onChange={setSearch}
          placeholder="Search containers…"
          value={search}
        />
      </RoutedTabSearch>

      {/* The grid scrolls itself, so this wrapper has to pass the panel's
          height down rather than size to the cards. */}
      <motion.div
        layout="position"
        style={{
          display: "flex",
          flexDirection: "column",
          flex: "1 1 auto",
          minHeight: 0,
        }}
        transition={{
          duration: slowTransitionDurationSeconds,
          ease: EASING_STANDARD,
        }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            flex: "1 1 auto",
            minHeight: 0,
          }}
        >
          {filteredContainers.length === 0 ? (
            <div style={{ textAlign: "center", padding: "32px 0" }}>
              <AppTypography color="text.secondary" variant="body2">
                No containers found.
              </AppTypography>
            </div>
          ) : (
            <ReorderableCardGrid
              fillAvailable
              getId={getContainerId}
              items={filteredContainers}
              renderBody={
                !hasStacks
                  ? undefined
                  : (renderCard) => (
                      // Entries render in the saved order — dragging a band is
                      // what places a stack — and dense flow backfills the slot
                      // a wide band can't use with the cards that follow, so
                      // the lines stay full either way. The grouped layout
                      // stays up in layout mode too: flattening there would
                      // remount the very card the hold just picked up.
                      <AppGrid
                        container
                        spacing={DASHBOARD_CARD_SPACING}
                        style={{ gridAutoFlow: "row dense" }}
                      >
                        {stackEntries.map((entry, index) => {
                          if (entry.type === "container") {
                            return (
                              <AppGrid
                                key={entry.container.Id}
                                size={CARD_GRID_SIZE_DENSE}
                              >
                                {renderCard(entry.container, index)}
                              </AppGrid>
                            );
                          }
                          const dragId = getStackDragId(entry.project);
                          if (collapsedStacks.has(entry.project)) {
                            return (
                              <AppGrid
                                key={`stack:${entry.project}`}
                                size={CARD_GRID_SIZE_DENSE}
                              >
                                <SortableCard
                                  editMode={editMode}
                                  id={dragId}
                                  pending={pendingId === dragId}
                                >
                                  <ContainerStackSummaryCard
                                    containers={entry.containers}
                                    onExpand={() => toggleStack(entry.project)}
                                    project={entry.project}
                                  />
                                </SortableCard>
                              </AppGrid>
                            );
                          }
                          return (
                            <AppGrid
                              key={`stack:${entry.project}`}
                              size={getStackBandSize(entry.containers.length)}
                            >
                              <ContainerStackBand
                                containers={entry.containers}
                                editMode={editMode}
                                onToggle={() => toggleStack(entry.project)}
                                pending={pendingId === dragId}
                                project={entry.project}
                              >
                                <AppGrid
                                  columns={getStackBandColumns(
                                    entry.containers.length,
                                  )}
                                  container
                                  spacing={DASHBOARD_CARD_SPACING}
                                >
                                  {entry.containers.map((container) => (
                                    <AppGrid key={container.Id} size={1}>
                                      {renderCard(container, index)}
                                    </AppGrid>
                                  ))}
                                </AppGrid>
                              </ContainerStackBand>
                            </AppGrid>
                          );
                        })}
                      </AppGrid>
                    )
              }
              sortableIds={hasStacks ? cardSortableIds : undefined}
              renderItem={(container) => (
                <ContainerCard
                  actionPending={stoppingContainerIds.has(container.Id)}
                  containerId={container.Id}
                  onSelect={
                    editMode
                      ? undefined
                      : () => handleSelectContainer(container.Id)
                  }
                />
              )}
              size={CARD_GRID_SIZE_DENSE}
              surface={surface}
            />
          )}
        </div>
      </motion.div>
    </Suspense>
  );
};

export default memo(ContainerList);
