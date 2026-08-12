import {
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { AnimatePresence, motion } from "motion/react";
import { useCallback, useEffect, useEffectEvent } from "react";

import { CACHE_TTL_MS, linuxio, type NetworkInterface } from "@/api";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import SortableCard from "@/components/cards/SortableCard";
import { appendLiveSample } from "@/components/charts/liveSeriesStore";
import {
  type LiveSeriesPoint,
  useLiveSeries,
} from "@/components/charts/useLiveSeries";
import ReorderableArea from "@/components/reorder/ReorderableArea";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useAppTheme } from "@/theme";
import {
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";
import { formatThroughput } from "@/utils/formaters";

import NetworkTrafficGraph from "./NetworkTrafficGraph";

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

const selectNetworkInterface =
  (name: string) => (interfaces: NetworkInterface[]) =>
    interfaces.find((iface) => iface.name === name);

/** Live sampling cadence, matching the dashboard network chart. */
const SAMPLE_INTERVAL_MS = 1000;

const NetworkInterfaceTrafficGraphs = ({ name }: { name: string }) => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const { data: iface } = useQuery({
    ...linuxio.network.get_network_info,
    refetchOnMount: false,
    select: selectNetworkInterface(name),
  });

  // Same series ids as the dashboard network chart, so the buffers carry over
  // between the two pages instead of each starting from an empty canvas.
  const rxId = `network:rx:${name}`;
  const txId = `network:tx:${name}`;
  // History arrives in bytes/s; the store keeps kB/s like the dashboard chart.
  const [rxSeries, txSeries] = useLiveSeries([rxId, txId], async (request) => {
    // One-shot backfill: the request carries a rolling from_ms, so caching
    // the entry would only pollute the cache.
    const points = await queryClient.fetchQuery({
      ...linuxio.monitoring.get_network_history(request),
      staleTime: CACHE_TTL_MS.NONE,
      gcTime: CACHE_TTL_MS.NONE,
    });
    const rxPoints: LiveSeriesPoint[] = [];
    const txPoints: LiveSeriesPoint[] = [];
    for (const point of points) {
      const rates = point.interfaces?.[name];
      if (!rates) continue;
      rxPoints.push({
        t: point.captured_at_ms,
        v: rates.recv_bytes_per_sec / 1024,
      });
      txPoints.push({
        t: point.captured_at_ms,
        v: rates.sent_bytes_per_sec / 1024,
      });
    }
    return { [rxId]: rxPoints, [txId]: txPoints };
  });

  const appendLatestTraffic = useEffectEvent(() => {
    if (!iface) return;
    appendLiveSample(rxId, iface.rx_speed / 1024);
    appendLiveSample(txId, iface.tx_speed / 1024);
  });

  // Append on a fixed interval, decoupled from React's render cycle.
  useEffect(() => {
    const intervalId = setInterval(() => {
      appendLatestTraffic();
    }, SAMPLE_INTERVAL_MS);
    return () => clearInterval(intervalId);
  }, [rxId, txId]);

  if (!iface) return null;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div>
        <div style={{ height: 120, width: "100%", minWidth: 0 }}>
          <NetworkTrafficGraph
            color={theme.chart.rx}
            key={rxId}
            label="RX"
            series={rxSeries}
          />
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            marginLeft: 4,
            marginTop: 2,
          }}
        >
          <span
            style={{
              width: 7,
              height: 7,
              backgroundColor: theme.chart.rx,
              borderRadius: "50%",
              display: "inline-block",
            }}
          />
          <AppTypography style={{ opacity: 0.7 }} variant="caption">
            RX: {formatThroughput(iface.rx_speed)}
          </AppTypography>
        </div>
      </div>
      <div>
        <div style={{ height: 120, width: "100%", minWidth: 0 }}>
          <NetworkTrafficGraph
            color={theme.chart.tx}
            key={txId}
            label="TX"
            series={txSeries}
          />
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            marginLeft: 4,
            marginTop: 2,
          }}
        >
          <span
            style={{
              width: 7,
              height: 7,
              backgroundColor: theme.chart.tx,
              borderRadius: "50%",
              display: "inline-block",
            }}
          />
          <AppTypography style={{ opacity: 0.7 }} variant="caption">
            TX: {formatThroughput(iface.tx_speed)}
          </AppTypography>
        </div>
      </div>
    </div>
  );
};

// A press in layout mode belongs to the drag, not to expanding the interface.
const noopToggle = () => {};
const getNetworkInterfaceId = (iface: { name: string }) => iface.name;

const NetworkInterfaceList = () => {
  const search = networkRouteApi.useSearch();
  const navigate = networkRouteApi.useNavigate();
  const expanded = typeof search.iface === "string" ? search.iface : undefined;

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

  return (
    <div>
      <ReorderableArea surface={surface}>
        <AppGrid container spacing={4}>
          <AnimatePresence>
            {surface.items.map((iface) =>
              expanded && expanded !== iface.name ? null : (
                <AppGrid
                  animate={{ opacity: 1, scale: 1 }}
                  component={motion.div}
                  exit={{ opacity: 0, scale: 0.9 }}
                  initial={{ opacity: 0, scale: 0.95 }}
                  key={iface.name}
                  layout
                  size={
                    expanded === iface.name
                      ? { xs: 12, md: 4, lg: 3 }
                      : { xs: 12, sm: 6, md: 4, lg: 2 }
                  }
                  transition={{
                    duration: slowTransitionDurationSeconds,
                    ease: EASING_STANDARD,
                  }}
                >
                  <SortableCard
                    editMode={surface.editMode}
                    id={iface.name}
                    pending={surface.pendingId === iface.name}
                  >
                    <NetworkInterfaceCard
                      expanded={expanded === iface.name}
                      name={iface.name}
                      onClose={handleClose}
                      onToggle={surface.editMode ? noopToggle : handleToggle}
                      type={iface.type}
                    />
                  </SortableCard>
                </AppGrid>
              ),
            )}

            {/* Traffic graphs — appear on the right when a NIC is selected */}
            {selectedIface && (
              <AppGrid
                animate={{ opacity: 1, x: 0 }}
                component={motion.div}
                exit={{ opacity: 0, x: 40 }}
                initial={{ opacity: 0, x: 40 }}
                key="traffic-graphs"
                size={{ xs: 12, md: 8, lg: 9 }}
                transition={{
                  duration: slowTransitionDurationSeconds,
                  delay: 0.05,
                  ease: EASING_STANDARD,
                }}
              >
                <NetworkInterfaceTrafficGraphs name={selectedIface.name} />
              </AppGrid>
            )}
          </AnimatePresence>
        </AppGrid>
      </ReorderableArea>
    </div>
  );
};

export default NetworkInterfaceList;
