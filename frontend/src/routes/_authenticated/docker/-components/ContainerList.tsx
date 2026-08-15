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
import UnitLogsCard from "@/components/cards/UnitLogsCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import {
  EASING_EMPHASIZED,
  TRANSITION_DURATION_SLOW_MS,
} from "@/theme/constants";

import ContainerDetailsPanel from "./ContainerDetailsPanel";
import ContainerTable from "./ContainerTable";
import type { ContainerAutoUpdateController } from "./useContainerAutoUpdateState";

// Card mode only needs identity/display fields in this parent.  Keeping the
// volatile metrics out of the selected result means a metrics-only poll can
// update the per-card observers without rebuilding the grid shells.
const selectContainerSearchStatus = (container: ContainerInfo): string => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "unhealthy";
  if (status.includes("healthy")) return "healthy";
  return container.State;
};

const selectCardContainers = (
  containers: readonly ContainerInfo[],
): ContainerInfo[] =>
  containers.map((container) => ({
    Created: container.Created,
    Id: container.Id,
    Image: container.Image,
    Names: container.Names,
    State: container.State,
    Status: selectContainerSearchStatus(container),
  }));

const selectAllContainers = (containers: ContainerInfo[]) => containers;

interface ContainerListProps {
  checkingUpdates?: boolean;
  /**
   * Page-level auto-update controller, shared with the settings dialog so
   * both surfaces write through one save queue.
   */
  containerAutoUpdate: ContainerAutoUpdateController;
  stoppingContainerIds?: ReadonlySet<string>;
  viewMode?: "card" | "table";
}

const EMPTY_STOPPING_CONTAINER_IDS = new Set<string>();
const dockerRouteApi = getRouteApi("/_authenticated/docker/containers");
const getContainerId = (container: { Id: string }) => container.Id;

const ContainerList = ({
  checkingUpdates = false,
  containerAutoUpdate,
  stoppingContainerIds = EMPTY_STOPPING_CONTAINER_IDS,
  viewMode = "card",
}: ContainerListProps) => {
  const theme = useAppTheme();
  const detailTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
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

  const autoUpdateBlockedReasons = useMemo<ReadonlyMap<string, string>>(() => {
    if (containerAutoUpdate.state?.options.mode !== "update") return new Map();
    const blocked = new Map<string, string>();
    for (const [name, eligibility] of containerAutoUpdate.targetEligibility) {
      if (!eligibility.mutationAllowed && eligibility.mutationReason) {
        blocked.set(name, eligibility.mutationReason);
      }
    }
    return blocked;
  }, [
    containerAutoUpdate.state?.options.mode,
    containerAutoUpdate.targetEligibility,
  ]);

  const autoUpdatePresentation = useCallback(
    (container: (typeof containers)[number]) => {
      const name = container.Names?.[0]?.replace("/", "") ?? "";
      const selected = containerAutoUpdate.selectedNames.has(name);
      const blockedReason = autoUpdateBlockedReasons.get(name);
      return {
        disabled:
          containerAutoUpdate.disabled ||
          (!selected && blockedReason !== undefined),
        reason: blockedReason ?? containerAutoUpdate.reason,
        selected,
      };
    },
    [
      autoUpdateBlockedReasons,
      containerAutoUpdate.disabled,
      containerAutoUpdate.reason,
      containerAutoUpdate.selectedNames,
    ],
  );

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

  const surface = useReorderableSurface({
    getId: getContainerId,
    items: containers,
    surface: "docker.containers",
  });
  const { editMode, items: orderedContainers } = surface;
  const tableDnd = useReorderableTableDnd<
    ContainerInfo,
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
            duration: detailTransitionDurationSeconds,
            ease: EASING_EMPHASIZED,
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
                gap: theme.spacing(2.5),
              }}
              transition={{
                duration: detailTransitionDurationSeconds,
                delay: 0.04,
                ease: EASING_EMPHASIZED,
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
                  duration: detailTransitionDurationSeconds,
                  delay: 0.04,
                  ease: EASING_EMPHASIZED,
                }}
              >
                <ContainerCard
                  actionPending={stoppingContainerIds.has(selectedContainer.Id)}
                  autoUpdateDisabled={
                    autoUpdatePresentation(selectedContainer).disabled
                  }
                  autoUpdatePending={containerAutoUpdate.pendingNames.has(
                    selectedContainer.Names?.[0]?.replace("/", "") ?? "",
                  )}
                  autoUpdateReason={
                    autoUpdatePresentation(selectedContainer).reason
                  }
                  autoUpdateSelected={
                    autoUpdatePresentation(selectedContainer).selected
                  }
                  containerId={selectedContainer.Id}
                  onSelect={() => handleSelectContainer(selectedContainer.Id)}
                  onToggleAutoUpdate={containerAutoUpdate.toggleContainer}
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
                  duration: detailTransitionDurationSeconds,
                  delay: 0.08,
                  ease: EASING_EMPHASIZED,
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
                  duration: detailTransitionDurationSeconds,
                  delay: 0.12,
                  ease: EASING_EMPHASIZED,
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
                duration: detailTransitionDurationSeconds,
                delay: 0.16,
                ease: EASING_EMPHASIZED,
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
        autoUpdateBlockedReasons={autoUpdateBlockedReasons}
        autoUpdateDisabled={containerAutoUpdate.disabled}
        autoUpdatePendingNames={containerAutoUpdate.pendingNames}
        autoUpdateReason={containerAutoUpdate.reason}
        autoUpdateSelectedNames={containerAutoUpdate.selectedNames}
        checkingUpdates={checkingUpdates}
        containers={orderedContainers}
        dnd={tableDnd}
        onSelectContainer={updateSelectedContainer}
        stoppingContainerIds={stoppingContainerIds}
        onToggleAutoUpdate={containerAutoUpdate.toggleContainer}
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
      <RoutedTabSearch>
        <AppHeaderSearch
          onChange={setSearch}
          placeholder="Search containers…"
          value={search}
        />
      </RoutedTabSearch>

      <motion.div
        layout="position"
        transition={{
          duration: detailTransitionDurationSeconds,
          ease: EASING_EMPHASIZED,
        }}
      >
        <div>
          {filteredContainers.length === 0 ? (
            <div style={{ textAlign: "center", padding: "32px 0" }}>
              <AppTypography color="text.secondary" variant="body2">
                No containers found.
              </AppTypography>
            </div>
          ) : (
            <ReorderableCardGrid
              getId={getContainerId}
              items={filteredContainers}
              renderItem={(container) => (
                <ContainerCard
                  actionPending={stoppingContainerIds.has(container.Id)}
                  autoUpdateDisabled={
                    autoUpdatePresentation(container).disabled
                  }
                  autoUpdatePending={containerAutoUpdate.pendingNames.has(
                    container.Names?.[0]?.replace("/", "") ?? "",
                  )}
                  autoUpdateReason={autoUpdatePresentation(container).reason}
                  autoUpdateSelected={
                    autoUpdatePresentation(container).selected
                  }
                  containerId={container.Id}
                  onSelect={
                    editMode
                      ? undefined
                      : () => handleSelectContainer(container.Id)
                  }
                  onToggleAutoUpdate={containerAutoUpdate.toggleContainer}
                />
              )}
              size={{ xs: 12, sm: 6, md: 4, lg: 2 }}
              surface={surface}
            />
          )}
        </div>
      </motion.div>
    </Suspense>
  );
};

export default memo(ContainerList);
