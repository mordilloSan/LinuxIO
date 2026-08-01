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
import { Icon } from "@iconify/react";
import {
  ComponentType,
  memo,
  Suspense,
  useCallback,
  useMemo,
  useState,
} from "react";

import DashboardCardSkeleton, {
  type DashboardCardSkeletonLayout,
} from "@/components/cards/DashboardCardSkeleton";
import SortableCard from "@/components/cards/SortableCard";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTooltip from "@/components/ui/AppTooltip";
import useAuth from "@/hooks/useAuth";
import { useConfigValue } from "@/hooks/useConfig";
import { useAppTheme } from "@/theme";

import DockerInfo from "./Docker";
import DriveInfo from "./Drive";
import FileSystem from "./FileSystem";
import GpuInfo from "./Gpu";
import Memory from "./Memory";
import MotherBoardInfo from "./MotherBoard";
import Network from "./Network";
import Processor from "./Processor";
import SystemHealth from "./SystemHealth";
import SystemOverview from "./SystemOverview";

const MemoSystemOverview = memo(SystemOverview);
const MemoSystemHealth = memo(SystemHealth);
const MemoProcessor = memo(Processor);
const MemoMemory = memo(Memory);
const MemoFileSystem = memo(FileSystem);
const MemoNetwork = memo(Network);
const MemoMotherBoardInfo = memo(MotherBoardInfo);
const MemoGpuInfo = memo(GpuInfo);
const MemoDriveInfo = memo(DriveInfo);
const MemoDockerInfo = memo(DockerInfo);

interface DashboardCardDefinition {
  component: ComponentType;
  id: string;
  label: string;
  skeletonLayout: DashboardCardSkeletonLayout;
}

const allCards: DashboardCardDefinition[] = [
  {
    id: "overview",
    label: "System Overview",
    component: MemoSystemOverview,
    skeletonLayout: "stats",
  },
  {
    id: "system",
    label: "System Health",
    component: MemoSystemHealth,
    skeletonLayout: "split",
  },
  {
    id: "cpu",
    label: "Processor",
    component: MemoProcessor,
    skeletonLayout: "split",
  },
  {
    id: "memory",
    label: "Memory",
    component: MemoMemory,
    skeletonLayout: "split",
  },
  {
    id: "docker",
    label: "Docker",
    component: MemoDockerInfo,
    skeletonLayout: "split",
  },
  {
    id: "nic",
    label: "Network",
    component: MemoNetwork,
    skeletonLayout: "split",
  },
  {
    id: "fs",
    label: "File System",
    component: MemoFileSystem,
    skeletonLayout: "stats",
  },
  {
    id: "mb",
    label: "Motherboard",
    component: MemoMotherBoardInfo,
    skeletonLayout: "stats",
  },
  {
    id: "gpu",
    label: "GPU",
    component: MemoGpuInfo,
    skeletonLayout: "stats",
  },
  {
    id: "drive",
    label: "Drive",
    component: MemoDriveInfo,
    skeletonLayout: "split",
  },
];

const DashboardPage = () => {
  const theme = useAppTheme();
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

  const availableCards = useMemo(
    () => allCards.filter((card) => card.id !== "docker" || dockerAvailable),
    [dockerAvailable],
  );

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
    <div>
      <div
        style={{
          display: "flex",
          justifyContent: "flex-end",
          marginBottom: theme.spacing(1),
        }}
      >
        <AppTooltip title="Card visibility">
          <AppIconButton
            aria-label="Card visibility"
            onClick={(e) => setAnchorEl(e.currentTarget)}
            size="small"
          >
            <Icon height={20} icon="mdi:view-grid-outline" width={20} />
          </AppIconButton>
        </AppTooltip>
        <AppTooltip title={editMode ? "Lock layout" : "Edit layout"}>
          <AppIconButton
            aria-label={editMode ? "Lock layout" : "Edit layout"}
            color={editMode ? "primary" : "default"}
            onClick={() => setEditMode((prev) => !prev)}
            size="small"
          >
            <Icon height={20} icon="mdi:drag" width={20} />
          </AppIconButton>
        </AppTooltip>
      </div>

      <AppPopover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        onClose={() => setAnchorEl(null)}
        open={Boolean(anchorEl)}
        paperStyle={{
          padding: theme.spacing(2),
        }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <div
          style={{
            display: "flex",
            flexDirection: "column",
          }}
        >
          {availableCards.map((card) => (
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={!hiddenCards?.includes(card.id)}
                  onChange={() => toggleCard(card.id)}
                  size="small"
                />
              }
              key={card.id}
              label={card.label}
            />
          ))}
        </div>
      </AppPopover>

      <DndContext
        collisionDetection={closestCenter}
        onDragEnd={handleDragEnd}
        sensors={sensors}
      >
        <SortableContext items={cardIds} strategy={rectSortingStrategy}>
          <AppGrid container spacing={4}>
            {cards.map(
              ({ id, label, component: CardComponent, skeletonLayout }) => (
                <AppGrid key={id} size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}>
                  <SortableCard editMode={editMode} id={id}>
                    <ErrorBoundary>
                      <Suspense
                        fallback={
                          <DashboardCardSkeleton
                            layout={skeletonLayout}
                            title={label}
                          />
                        }
                      >
                        <CardComponent />
                      </Suspense>
                    </ErrorBoundary>
                  </SortableCard>
                </AppGrid>
              ),
            )}
          </AppGrid>
        </SortableContext>
      </DndContext>
    </div>
  );
};

export default DashboardPage;
