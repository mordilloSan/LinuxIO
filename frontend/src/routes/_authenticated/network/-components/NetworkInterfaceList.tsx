import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { AnimatePresence, motion } from "motion/react";
import { useCallback, useEffect, useRef, type MouseEvent } from "react";

import { linuxio, type NetworkInterface } from "@/api";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import SortableCard from "@/components/cards/SortableCard";
import ReorderableArea from "@/components/reorder/ReorderableArea";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useAppTheme } from "@/theme";
import {
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";

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

const NetworkInterfaceTrafficGraphs = ({ name }: { name: string }) => {
  const theme = useAppTheme();
  const rxCanvasRef = useRef<HTMLCanvasElement>(null);
  const txCanvasRef = useRef<HTMLCanvasElement>(null);
  const { data: iface } = useQuery(
    linuxio.network.get_network_info.queryOptions({
      refetchOnMount: false,
      select: selectNetworkInterface(name),
    }),
  );

  const dispatchToCanvas = useCallback(
    (
      canvas: HTMLCanvasElement | null,
      type: string,
      clientX: number,
      clientY: number,
    ) => {
      if (!canvas) return;
      canvas.dispatchEvent(
        new MouseEvent(type, { clientX, clientY, bubbles: false }),
      );
    },
    [],
  );

  const handleGraphMouseMove = useCallback(
    (e: MouseEvent) => {
      const containerRect = (
        e.currentTarget as HTMLElement
      ).getBoundingClientRect();
      const relX = (e.clientX - containerRect.left) / containerRect.width;

      for (const canvas of [rxCanvasRef.current, txCanvasRef.current]) {
        if (!canvas) continue;
        const rect = canvas.getBoundingClientRect();
        dispatchToCanvas(
          canvas,
          "mousemove",
          rect.left + relX * rect.width,
          rect.top,
        );
      }
    },
    [dispatchToCanvas],
  );

  const handleGraphMouseLeave = useCallback(() => {
    for (const canvas of [rxCanvasRef.current, txCanvasRef.current]) {
      if (!canvas) continue;
      canvas.dispatchEvent(new MouseEvent("mouseout", { bubbles: false }));
    }
  }, []);

  if (!iface) return null;

  return (
    <div
      onMouseLeave={handleGraphMouseLeave}
      onMouseMove={handleGraphMouseMove}
      style={{ display: "flex", flexDirection: "column", gap: 8 }}
    >
      <div>
        <div style={{ height: 120, width: "100%", minWidth: 0 }}>
          <NetworkTrafficGraph
            color={theme.chart.rx}
            key={`rx-${name}`}
            label="RX"
            ref={rxCanvasRef}
            value={iface.rx_speed}
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
            RX: {(iface.rx_speed / 1024).toFixed(1)} kB/s
          </AppTypography>
        </div>
      </div>
      <div>
        <div style={{ height: 120, width: "100%", minWidth: 0 }}>
          <NetworkTrafficGraph
            color={theme.chart.tx}
            key={`tx-${name}`}
            label="TX"
            ref={txCanvasRef}
            value={iface.tx_speed}
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
            TX: {(iface.tx_speed / 1024).toFixed(1)} kB/s
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

  const { data: interfaces } = useSuspenseQuery(
    linuxio.network.get_network_info.queryOptions({
      refetchInterval: 1000,
      select: selectNetworkInterfaceIdentities,
    }),
  );

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        navigate({
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
    navigate({
      to: ".",
      search: (previous) => ({
        ...previous,
        iface: undefined,
      }),
    });
  }, [navigate]);

  const handleToggle = useCallback(
    (name: string) => {
      navigate({
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
