import { Icon } from "@iconify/react";
import type { ComponentType } from "react";
import { memo, Suspense, useCallback, useMemo, useState } from "react";

import DashboardCardSkeleton, {
  type DashboardCardSkeletonLayout,
} from "@/components/cards/DashboardCardSkeleton";
import ErrorBoundary from "@/components/errors/ErrorBoundary";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import AppTooltip from "@/components/ui/AppTooltip";
import useAuth from "@/hooks/useAuth";
import { useConfigValue } from "@/hooks/useConfig";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
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

const getCardId = (card: DashboardCardDefinition) => card.id;

const DashboardPage = () => {
  const theme = useAppTheme();
  const { dockerAvailable } = useAuth();
  const [hiddenCards, setHiddenCards] = useConfigValue("hiddenCards");
  const [anchorEl, setAnchorEl] = useState<HTMLButtonElement | null>(null);

  const availableCards = useMemo(
    () => allCards.filter((card) => card.id !== "docker" || dockerAvailable),
    [dockerAvailable],
  );

  const visibleCards = useMemo(() => {
    const hiddenSet = new Set(hiddenCards);
    return availableCards.filter((card) => !hiddenSet.has(card.id));
  }, [availableCards, hiddenCards]);

  const surface = useReorderableSurface({
    getId: getCardId,
    items: visibleCards,
    surface: "dashboard",
  });

  const toggleCard = useCallback(
    (id: string) => {
      const next = hiddenCards.includes(id)
        ? hiddenCards.filter((card) => card !== id)
        : [...hiddenCards, id];
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

      <ReorderableCardGrid
        fillAvailable={false}
        getId={getCardId}
        renderItem={({ label, component: CardComponent, skeletonLayout }) => (
          <ErrorBoundary>
            <Suspense
              fallback={
                <DashboardCardSkeleton layout={skeletonLayout} title={label} />
              }
            >
              <CardComponent />
            </Suspense>
          </ErrorBoundary>
        )}
        size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
        surface={surface}
      />
    </div>
  );
};

export default DashboardPage;
