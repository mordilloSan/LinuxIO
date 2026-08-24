import {
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { motion } from "motion/react";
import { useCallback, useEffect, useEffectEvent, useMemo } from "react";

import { CACHE_TTL_MS, linuxio, type NetworkInterface } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import { appendLiveSample } from "@/components/charts/liveSeriesStore";
import {
  type LiveSeriesPoint,
  useLiveSeries,
} from "@/components/charts/useLiveSeries";
import NetworkInterfaceEditor from "@/components/network/NetworkInterfaceEditor";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useAppTheme } from "@/theme";
import {
  CARD_PADDING_LG,
  DETAIL_PANEL_GAP,
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";
import { formatThroughput } from "@/utils/formaters";

import NetworkInterfaceLogsCard from "./NetworkInterfaceLogsCard";
import NetworkInterfaceStatsCard from "./NetworkInterfaceStatsCard";
import { selectNetworkInterface } from "./networkSelectors";
import NetworkTrafficGraph from "./NetworkTrafficGraph";
import NetworkTrafficHistoryCard from "./NetworkTrafficHistoryCard";

export type { NetworkInterface };
const networkRouteApi = getRouteApi("/_authenticated/network");

interface NetworkInterfaceIdentity {
  name: string;
  type: string;
}

const getNetworkInterfaceType = (name: string): string => {
  if (name.startsWith("wl")) return "wifi";
  if (name.startsWith("lo")) return "loopback";
  return "ethernet";
};

export const selectNetworkInterfaceIdentities = (
  interfaces: NetworkInterface[],
): NetworkInterfaceIdentity[] =>
  interfaces
    .filter((iface) => !iface.name.startsWith("veth"))
    .map((iface) => ({
      name: iface.name,
      type: getNetworkInterfaceType(iface.name),
    }));

/** Live sampling cadence, matching the dashboard network chart. */
const SAMPLE_INTERVAL_MS = 1000;

/* The detail view's upper row of summary cards. A floor rather than a fixed
   height: the statistics card is the tallest of the three at its natural size
   and comes in just under this, so the row buys the editor and the live chart
   a little room without capping a card whose content runs longer — the manual
   IPv4 form, or a stat value that wraps in a narrow column. */
const DETAIL_SUMMARY_ROW_MIN_HEIGHT = 300;

/* The detail view's lower row: traffic history beside the interface journal,
   half the width each. Both cards are pinned to this one row height rather
   than sized by their content, so the chart and the log viewport keep a shared
   baseline as log lines arrive and as the history range changes. Taller than
   `cardHeight` because a multi-hour chart and a log tail both need the room
   the upper row of summary cards does not. */
const DETAIL_HISTORY_ROW_HEIGHT = 320;

const NetworkInterfaceTrafficGraphs = ({ name }: { name: string }) => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const { data: iface } = useQuery({
    ...linuxio.network.get_network_info,
    refetchOnMount: false,
    select: selectNetworkInterface(name),
  });

  // TX shares the dashboard buffer. RX has a separate signed buffer because
  // the dashboard renders it above zero, while this focused chart renders
  // received traffic below its zero line.
  const rxInboundId = `network:rx:inbound:${name}`;
  const txId = `network:tx:${name}`;
  // History arrives in bytes/s; the store keeps kB/s like the dashboard chart.
  const [rxInboundSeries, txSeries] = useLiveSeries(
    [rxInboundId, txId],
    async (request) => {
      // One-shot backfill: the request carries a rolling from_ms, so caching
      // the entry would only pollute the cache.
      const points = await queryClient.query({
        ...linuxio.monitoring.get_network_history(request),
        staleTime: CACHE_TTL_MS.NONE,
        gcTime: CACHE_TTL_MS.NONE,
      });
      const rxInboundPoints: LiveSeriesPoint[] = [];
      const txPoints: LiveSeriesPoint[] = [];
      for (const point of points) {
        const rates = point.interfaces?.[name];
        if (!rates) continue;
        rxInboundPoints.push({
          t: point.captured_at_ms,
          v: -rates.recv_bytes_per_sec / 1024,
        });
        txPoints.push({
          t: point.captured_at_ms,
          v: rates.sent_bytes_per_sec / 1024,
        });
      }
      return { [rxInboundId]: rxInboundPoints, [txId]: txPoints };
    },
  );

  const appendLatestTraffic = useEffectEvent(() => {
    if (!iface) return;
    appendLiveSample(rxInboundId, -iface.rx_speed / 1024);
    appendLiveSample(txId, iface.tx_speed / 1024);
  });

  // Append on a fixed interval, decoupled from React's render cycle.
  useEffect(() => {
    const intervalId = setInterval(() => {
      appendLatestTraffic();
    }, SAMPLE_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, []);

  const trafficSeries = useMemo(
    () => [
      { color: theme.chart.tx, label: "Sent", series: txSeries },
      { color: theme.chart.rx, label: "Received", series: rxInboundSeries },
    ],
    [rxInboundSeries, theme.chart.rx, theme.chart.tx, txSeries],
  );

  if (!iface) return null;

  return (
    <AppGrid size={{ xs: 12, sm: 6, md: 4 }}>
      <FrostedCard
        style={{
          display: "flex",
          flexDirection: "column",
          height: "100%",
          padding: CARD_PADDING_LG,
        }}
      >
        <AppTypography fontWeight={600} variant="subtitle1">
          Traffic
        </AppTypography>
        {/* The chart absorbs whatever height the row's tallest card leaves
            over, and keeps a floor of its own on a short row. */}
        <div style={{ flex: 1, minHeight: 150, minWidth: 0, width: "100%" }}>
          <NetworkTrafficGraph series={trafficSeries} />
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 12 }}>
          <TrafficLegend
            color={theme.chart.tx}
            label="Sent"
            sign="+"
            value={formatThroughput(iface.tx_speed)}
          />
          <TrafficLegend
            color={theme.chart.rx}
            label="Received"
            sign="−"
            value={formatThroughput(iface.rx_speed)}
          />
        </div>
      </FrostedCard>
    </AppGrid>
  );
};

const TrafficLegend = ({
  color,
  label,
  sign,
  value,
}: {
  color: string;
  label: string;
  sign: "+" | "−";
  value: string;
}) => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      gap: 2,
    }}
  >
    <span
      style={{
        width: 7,
        height: 7,
        backgroundColor: color,
        borderRadius: "50%",
        display: "inline-block",
      }}
    />
    <AppTypography style={{ opacity: 0.7 }} variant="caption">
      {label}: {sign}
      {value}
    </AppTypography>
  </div>
);

const NetworkInterfaceConfigurationCards = ({
  name,
  onClose,
  type,
}: {
  name: string;
  onClose: () => void;
  type: string;
}) => {
  const { data: rawInterface } = useQuery({
    ...linuxio.network.get_network_info,
    refetchOnMount: false,
    select: selectNetworkInterface(name),
  });

  if (!rawInterface) return null;

  return (
    <NetworkInterfaceEditor
      expanded
      iface={{ ...rawInterface, type }}
      onClose={onClose}
    />
  );
};

// A press in layout mode belongs to the drag, not to expanding the interface.
const noopToggle = () => {};
const getNetworkInterfaceId = (iface: { name: string }) => iface.name;

const NetworkInterfaceList = () => {
  const expanded = networkRouteApi.useSearch({
    select: (search) =>
      typeof search.iface === "string" ? search.iface : undefined,
  });
  const navigate = networkRouteApi.useNavigate();

  const { data: interfaces } = useSuspenseQuery({
    ...linuxio.network.get_network_info,
    refetchInterval: 1000,
    select: selectNetworkInterfaceIdentities,
  });

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        void navigate({
          to: ".",
          search: (previous) => ({
            ...previous,
            iface: undefined,
          }),
        });
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [navigate]);

  const handleClose = useCallback(() => {
    void navigate({
      to: ".",
      search: (previous) => ({
        ...previous,
        iface: undefined,
      }),
    });
  }, [navigate]);

  const handleToggle = useCallback(
    (name: string) => {
      void navigate({
        to: ".",
        search: (previous) => ({
          ...previous,
          iface: expanded === name ? undefined : name,
        }),
      });
    },
    [expanded, navigate],
  );

  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const selectedIface = interfaces.find((iface) => iface.name === expanded);
  const surface = useReorderableSurface({
    getId: getNetworkInterfaceId,
    items: interfaces,
    surface: "network.interfaces",
  });

  if (selectedIface) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: DETAIL_PANEL_GAP,
        }}
      >
        <AppGrid
          animate={{ opacity: 1, y: 0 }}
          component={motion.div}
          container
          initial={{ opacity: 0, y: 14 }}
          // The tab panel stretches its only child, and a grid's default
          // `align-content: stretch` would hand that spare height to the single
          // auto row — inflating every `height: 100%` card to the full page.
          // Packing the row at the start keeps the cards content-tall.
          style={{
            alignContent: "start",
            gap: DETAIL_PANEL_GAP,
            gridAutoRows: `minmax(${DETAIL_SUMMARY_ROW_MIN_HEIGHT}px, auto)`,
          }}
          transition={{
            duration: slowTransitionDurationSeconds,
            delay: 0.04,
            ease: EASING_STANDARD,
          }}
        >
          <NetworkInterfaceConfigurationCards
            name={selectedIface.name}
            onClose={handleClose}
            type={selectedIface.type}
          />
          <AppGrid size={{ xs: 12, sm: 6, md: 4 }}>
            <NetworkInterfaceStatsCard name={selectedIface.name} />
          </AppGrid>
          <NetworkInterfaceTrafficGraphs name={selectedIface.name} />
        </AppGrid>
        <AppGrid
          animate={{ opacity: 1, y: 0 }}
          component={motion.div}
          container
          initial={{ opacity: 0, y: 18 }}
          style={{
            alignContent: "start",
            gap: DETAIL_PANEL_GAP,
            gridAutoRows: DETAIL_HISTORY_ROW_HEIGHT,
          }}
          transition={{
            duration: slowTransitionDurationSeconds,
            delay: 0.12,
            ease: EASING_STANDARD,
          }}
        >
          <AppGrid size={{ xs: 12, md: 6 }}>
            <NetworkTrafficHistoryCard name={selectedIface.name} />
          </AppGrid>
          <AppGrid size={{ xs: 12, md: 6 }}>
            <NetworkInterfaceLogsCard name={selectedIface.name} />
          </AppGrid>
        </AppGrid>
      </div>
    );
  }

  return (
    <ReorderableCardGrid
      fillAvailable
      getId={getNetworkInterfaceId}
      renderItem={(iface) => (
        <NetworkInterfaceCard
          name={iface.name}
          onToggle={surface.editMode ? noopToggle : handleToggle}
          type={iface.type}
        />
      )}
      size={{ xs: 12, sm: 6, md: 4, lg: 2 }}
      surface={surface}
    />
  );
};

export default NetworkInterfaceList;
