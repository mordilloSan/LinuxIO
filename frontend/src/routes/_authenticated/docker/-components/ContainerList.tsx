import {
  closestCenter,
  pointerWithin,
  type CollisionDetection,
  type DragOverEvent,
} from "@dnd-kit/core";
import { arrayMove } from "@dnd-kit/sortable";
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
  useRef,
  useState,
} from "react";

import { linuxio, openChannel, type ContainerInfo } from "@/api";
import ContainerCard from "@/components/cards/ContainerCard";
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppGrid, { type GridSize } from "@/components/ui/AppGrid";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import {
  useReorderableSurface,
  type ReorderableSurface,
  type ReorderableSurfaceDndProps,
} from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import {
  CARD_GRID_SIZE_DENSE,
  DASHBOARD_CARD_SPACING,
  DETAIL_PANEL_GAP,
  EASING_STANDARD,
  TRANSITION_DURATION_SLOW_MS,
  TRANSITION_DURATION_STANDARD_MS,
} from "@/theme/constants";

import ContainerDetailsPanel from "./ContainerDetailsPanel";
import { ContainerHistoryCards } from "./ContainerHistoryCards";
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
  type ContainerStackEntry,
  type ContainerTableRow,
} from "./containerStacks";
import ContainerTable from "./ContainerTable";

// The name go-monitoring records for a container, so history samples can be
// matched to the Docker inventory row they belong to.
const getContainerDisplayName = (container: ContainerInfo): string =>
  container.Names?.[0]?.replace("/", "") ?? "";

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

// The card grid's items are entries — a loose container or a whole stack — so
// a stack is just one wide card to the grid and every polished card gesture
// (hold ring, layout-mode overlay, drag) applies to it unchanged.
const getStackEntryId = (entry: ContainerStackEntry): string =>
  entry.type === "container"
    ? entry.container.Id
    : getStackDragId(entry.project);

// The collapse toggle swaps a stack's band for its summary card while the
// neighbours slide (the grid's layout animation); the incoming form fades in
// on the same beat.
const STACK_TOGGLE_TRANSITION = {
  duration: TRANSITION_DURATION_STANDARD_MS / 1000,
  ease: EASING_STANDARD,
};

const areSameOrder = (left: readonly string[], right: readonly string[]) =>
  left.length === right.length &&
  left.every((id, index) => id === right[index]);

type DroppableRects = Parameters<CollisionDetection>[0]["droppableRects"];

// Mirrors the surface's own saved-order merge: preview ids reorder the live
// list, unknown ids drop out, unlisted items keep their place at the end.
const applyPreviewOrder = (
  containers: ContainerInfo[],
  order: readonly string[],
): ContainerInfo[] => {
  const remaining = new Map(
    containers.map((container) => [container.Id, container]),
  );
  const ordered: ContainerInfo[] = [];
  for (const id of order) {
    const container = remaining.get(id);
    if (container) {
      ordered.push(container);
      remaining.delete(id);
    }
  }
  return [...ordered, ...remaining.values()];
};

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
  const { editMode, items: orderedContainers } = surface;
  const tableDnd = useReorderableTableDnd<
    ContainerTableRow,
    (typeof containers)[number]
  >({
    handleAriaLabel: "Reorder container",
    handleColumnWidth: 28,
    surface,
  });

  // While a card-view drag is live, the grid renders this provisional order —
  // recomputed from the saved order on every over-change, through the same
  // resolver the drop persists with, so the reflow the user watches is exactly
  // what releasing would save. Reflowing for real is what keeps every width
  // honest: strategy transforms can't preview a grid of mixed item sizes.
  const [dragPreviewIds, setDragPreviewIds] = useState<string[] | null>(null);
  // Collision geometry is frozen for the whole drag. SortableContext
  // re-measures droppables whenever its items change mid-drag, so collisions
  // computed against the moved layout would feed straight back into the
  // reorder that moved it — an update loop React kills as "maximum update
  // depth exceeded". Against the drag-start rects, `over` can only change
  // when the pointer moves: hovering a slot always means "insert at that
  // slot's saved position", and the reflow shows the outcome.
  const dragStartRectsRef = useRef<DroppableRects | null>(null);
  const entryCollisionDetection = useCallback<CollisionDetection>((args) => {
    dragStartRectsRef.current ??= new Map(args.droppableRects);
    const frozen = { ...args, droppableRects: dragStartRectsRef.current };
    // The pointer is what the user aims with: the slot under it reacts the
    // moment the cursor crosses in. Judging by the dragged rect's own center
    // (closestCenter alone) makes a wide band lag half its width behind the
    // cursor. Keyboard drags have no pointer, and a cursor in the gutters
    // matches nothing — both fall back to nearest-center.
    const underPointer = pointerWithin(frozen);
    return underPointer.length > 0 ? underPointer : closestCenter(frozen);
  }, []);
  const handleEntryDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over) return;
      // Back over its own slot: previewing "no change" is what dropping does.
      if (active.id === over.id) {
        setDragPreviewIds(null);
        return;
      }
      const activeId = String(active.id);
      const overId = String(over.id);
      const ids = surface.ids;
      const next =
        resolveStackDrag(ids, activeId, overId, projectById) ??
        (() => {
          const oldIndex = ids.indexOf(activeId);
          const newIndex = ids.indexOf(overId);
          if (oldIndex < 0 || newIndex < 0) return null;
          return arrayMove(ids, oldIndex, newIndex);
        })();
      if (!next) return;
      setDragPreviewIds((previous) =>
        previous && areSameOrder(previous, next) ? previous : next,
      );
    },
    [projectById, surface.ids],
  );
  const entryDndContextProps = useMemo<ReorderableSurfaceDndProps>(() => {
    const base = surface.dndContextProps;
    const settleDrag = () => {
      dragStartRectsRef.current = null;
      setDragPreviewIds(null);
    };
    return {
      ...base,
      collisionDetection: entryCollisionDetection,
      onDragAbort: () => {
        settleDrag();
        base.onDragAbort();
      },
      onDragCancel: () => {
        settleDrag();
        base.onDragCancel();
      },
      onDragEnd: (event) => {
        settleDrag();
        base.onDragEnd(event);
      },
      onDragOver: handleEntryDragOver,
      onDragStart: (event) => {
        dragStartRectsRef.current = null;
        base.onDragStart(event);
      },
    };
  }, [entryCollisionDetection, handleEntryDragOver, surface.dndContextProps]);
  const displayContainers = useMemo(
    () =>
      dragPreviewIds
        ? applyPreviewOrder(orderedContainers, dragPreviewIds)
        : orderedContainers,
    [dragPreviewIds, orderedContainers],
  );

  const filteredContainers = useMemo(() => {
    const searchText = search.trim().toLowerCase();
    if (!searchText) return displayContainers;

    return displayContainers.filter((container) => {
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
  }, [displayContainers, search]);
  // Grouping runs after the search filter, so a query that leaves a stack one
  // member simply shows that member loose.
  const stackEntries = useMemo(
    () => groupContainersByStack(filteredContainers),
    [filteredContainers],
  );
  // The grid sorts entries while the surface keeps persisting flat container
  // ids: an entry drag runs through `resolveStackDrag` on its way to the saved
  // order, which moves a stack's members as one block.
  const entryIds = useMemo(
    () => stackEntries.map(getStackEntryId),
    [stackEntries],
  );
  const entrySurface = useMemo<ReorderableSurface<ContainerStackEntry>>(
    () => ({
      dndContextProps: entryDndContextProps,
      editMode: surface.editMode,
      exitEditMode: surface.exitEditMode,
      ids: entryIds,
      items: stackEntries,
      pendingId: surface.pendingId,
    }),
    [entryDndContextProps, entryIds, stackEntries, surface],
  );
  const getEntrySize = useCallback(
    (entry: ContainerStackEntry): GridSize =>
      entry.type === "stack" && !collapsedStacks.has(entry.project)
        ? getStackBandSize(entry.containers.length)
        : CARD_GRID_SIZE_DENSE,
    [collapsedStacks],
  );
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

  // One renderer for both homes of an entry: the sortable grid cell, and the
  // DragOverlay copy that follows the pointer while the grid reflows.
  const renderEntry = (entry: ContainerStackEntry) => {
    if (entry.type === "container") {
      return (
        <ContainerCard
          actionPending={stoppingContainerIds.has(entry.container.Id)}
          containerId={entry.container.Id}
          onSelect={
            editMode
              ? undefined
              : () => handleSelectContainer(entry.container.Id)
          }
        />
      );
    }
    if (collapsedStacks.has(entry.project)) {
      return (
        <motion.div
          animate={{ opacity: 1 }}
          initial={{ opacity: 0 }}
          key="summary"
          style={{ height: "100%" }}
          transition={STACK_TOGGLE_TRANSITION}
        >
          <ContainerStackSummaryCard
            containers={entry.containers}
            onExpand={() => toggleStack(entry.project)}
            project={entry.project}
          />
        </motion.div>
      );
    }
    return (
      <motion.div
        animate={{ opacity: 1 }}
        initial={{ opacity: 0 }}
        key="band"
        style={{ height: "100%" }}
        transition={STACK_TOGGLE_TRANSITION}
      >
        <ContainerStackBand
          containers={entry.containers}
          onToggle={() => toggleStack(entry.project)}
          project={entry.project}
        >
          <AppGrid
            columns={getStackBandColumns(entry.containers.length)}
            container
            spacing={DASHBOARD_CARD_SPACING}
          >
            {entry.containers.map((container) => (
              <AppGrid key={container.Id} size={1}>
                <ContainerCard
                  actionPending={stoppingContainerIds.has(container.Id)}
                  // The band's header and padding must not make this grid row
                  // taller than a row of loose cards; the members give that
                  // height back by folding their metric bars into one line.
                  compactMetrics
                  containerId={container.Id}
                  onSelect={
                    editMode
                      ? undefined
                      : () => handleSelectContainer(container.Id)
                  }
                />
              </AppGrid>
            ))}
          </AppGrid>
        </ContainerStackBand>
      </motion.div>
    );
  };

  const renderEntryDragOverlay = (activeId: string) => {
    const entry = stackEntries.find(
      (candidate) => getStackEntryId(candidate) === activeId,
    );
    return entry ? renderEntry(entry) : null;
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
                display: "grid",
                gridTemplateColumns: isCompactLayout
                  ? "minmax(0, 1fr)"
                  : "minmax(max-content, min(25vw, 480px)) minmax(0, 1fr) minmax(0, 1fr)",
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
              initial={{ opacity: 0, y: 18 }}
              transition={{
                duration: slowTransitionDurationSeconds,
                delay: 0.16,
                ease: EASING_STANDARD,
              }}
            >
              <ContainerHistoryCards
                containerId={selectedContainer.Id}
                key={selectedContainer.Id}
                name={getContainerDisplayName(selectedContainer)}
              />
            </motion.div>
            <motion.div
              animate={{ opacity: 1, y: 0 }}
              initial={{ opacity: 0, y: 20 }}
              transition={{
                duration: slowTransitionDurationSeconds,
                delay: 0.2,
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
              animateLayout
              // Backfills the slot a band can't use with the cards that
              // follow, so the lines stay full without giving up the saved
              // entry order.
              dense
              fillAvailable
              getId={getStackEntryId}
              getItemSize={getEntrySize}
              renderDragOverlay={renderEntryDragOverlay}
              renderItem={renderEntry}
              size={CARD_GRID_SIZE_DENSE}
              // Drag reflows snap like the table's; only the collapse/expand
              // toggle animates positions.
              suspendLayoutAnimation={editMode}
              surface={entrySurface}
            />
          )}
        </div>
      </motion.div>
    </Suspense>
  );
};

export default memo(ContainerList);
