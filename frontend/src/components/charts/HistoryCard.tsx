import { Icon } from "@iconify/react";
import {
  createContext,
  useContext,
  useMemo,
  useState,
  useSyncExternalStore,
  type ComponentProps,
  type ReactNode,
} from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import HistoryAreaChart from "@/components/charts/HistoryAreaChart";
import {
  formatChartClock,
  formatChartDay,
} from "@/components/charts/timeFormat";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { cardHeight } from "@/theme/constants";

import {
  HISTORY_RANGES,
  type HistoryRange,
  type HistoryRangeId,
} from "./historyRanges";

import "./history-card.css";

type HistoryHoverListener = () => void;

/** Scoped external store for a group of history charts' shared crosshair. */
export class HistoryHoverStore {
  private hoverTime: number | null = null;
  private readonly listeners = new Set<HistoryHoverListener>();

  getSnapshot = (): number | null => this.hoverTime;

  setHoverTime = (hoverTime: number | null): void => {
    if (this.hoverTime === hoverTime) return;
    this.hoverTime = hoverTime;
    for (const listener of this.listeners) listener();
  };

  subscribe = (listener: HistoryHoverListener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
}

const HistoryHoverContext = createContext<HistoryHoverStore | null>(null);

export const HistoryHoverProvider = ({ children }: { children: ReactNode }) => {
  const [store] = useState(() => new HistoryHoverStore());
  return (
    <HistoryHoverContext.Provider value={store}>
      {children}
    </HistoryHoverContext.Provider>
  );
};

const useHistoryHover = (): [number | null, (time: number | null) => void] => {
  const store = useContext(HistoryHoverContext);
  if (!store) {
    throw new Error(
      "History charts must be rendered inside HistoryHoverProvider",
    );
  }
  const hoverTime = useSyncExternalStore(
    store.subscribe,
    store.getSnapshot,
    store.getSnapshot,
  );
  return [hoverTime, store.setHoverTime];
};

type SynchronizedHistoryAreaChartProps = Omit<
  ComponentProps<typeof HistoryAreaChart>,
  "hoverTime" | "onHoverTimeChange"
>;

/** Only this chart leaf subscribes to crosshair movement. */
export const SynchronizedHistoryAreaChart = (
  props: SynchronizedHistoryAreaChartProps,
) => {
  const [hoverTime, onHoverTimeChange] = useHistoryHover();
  return (
    <HistoryAreaChart
      {...props}
      hoverTime={hoverTime}
      onHoverTimeChange={onHoverTimeChange}
    />
  );
};

// Query-time downsampling uses the selected range as an explicit time window.
// Keep the request bounded independently of the agent's retention policy.
const HISTORY_REQUEST_LIMIT = 400;

export const rangeById = (id: HistoryRangeId): HistoryRange =>
  HISTORY_RANGES.find((range) => range.id === id) ?? HISTORY_RANGES[0];

export const historyRequest = (range: HistoryRange) => ({
  resolution: range.resolution,
  window_ms: range.windowMs,
  limit: HISTORY_REQUEST_LIMIT,
});

export const useHistoryTimestampFormatter = (range: HistoryRange) =>
  useMemo(
    () =>
      range.id === "7d" || range.id === "30d"
        ? formatChartDay
        : formatChartClock,
    [range.id],
  );

export const RangeSelect = ({
  value,
  onChange,
  ariaLabel,
}: {
  value: HistoryRangeId;
  onChange: (id: HistoryRangeId) => void;
  ariaLabel?: string;
}) => {
  const theme = useAppTheme();

  return (
    <AppSelect
      aria-label={ariaLabel}
      className="history-range-select"
      disableUnderline
      onChange={(event) => onChange(event.target.value as HistoryRangeId)}
      renderValue={(selected) => selected}
      size="small"
      style={{
        ["--app-select-input-font-size" as string]: "0.72rem",
        width: 46,
        opacity: 0.65,
        color: theme.palette.text.secondary,
        fontSize: "0.78rem",
        lineHeight: 1.43, // app-typo--body2
      }}
      value={value}
      variant="standard"
    >
      {HISTORY_RANGES.map((range) => (
        <option key={range.id} value={range.id}>
          {range.label}
        </option>
      ))}
    </AppSelect>
  );
};

export const HistoryCardShell = ({
  title,
  avatarIcon,
  headerRight,
  children,
}: {
  title: string;
  avatarIcon: string;
  headerRight?: ReactNode;
  children: ReactNode;
}) => {
  const theme = useAppTheme();

  return (
    <FrostedCard
      style={{
        minHeight: cardHeight,
        boxSizing: "border-box",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        padding: 6,
      }}
    >
      <CardIconHeader
        icon={
          <Icon
            color={theme.palette.primary.main}
            height={28}
            icon={avatarIcon}
            width={28}
          />
        }
        right={headerRight}
        style={{ marginBottom: 8 }}
        title={title}
      />
      {children}
    </FrostedCard>
  );
};

export const HistoryCardBody = ({
  children,
  message,
}: {
  children: ReactNode;
  message: string | null;
}) => {
  const theme = useAppTheme();

  return message ? (
    <div
      style={{
        flex: 1,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: theme.palette.text.secondary,
        padding: 16,
      }}
    >
      <AppTypography align="center" variant="body2">
        {message}
      </AppTypography>
    </div>
  ) : (
    <div style={{ flex: 1, minHeight: 0, padding: "0 4px 2px" }}>
      {children}
    </div>
  );
};

export interface HistoryCardProps {
  /** Shared time range so a group of cards stays synchronized. */
  rangeId: HistoryRangeId;
  onRangeChange: (id: HistoryRangeId) => void;
}

export type HistoryLiveProps = Pick<HistoryCardProps, "rangeId">;

export const historyCardMessage = (
  points: readonly unknown[] | undefined,
  isLoading: boolean,
  error: { message: string } | null,
  monitoringEnabled: boolean,
  monitoringReason: string | undefined,
): string | null => {
  if (!monitoringEnabled) {
    return monitoringReason ?? "Monitoring agent is unavailable.";
  }
  if (error) {
    return `Historical data not available. ${error.message}`;
  }
  if (isLoading) {
    return "Loading history…";
  }
  if (!points || points.length === 0) {
    return "Historical data not available.";
  }
  return null;
};
