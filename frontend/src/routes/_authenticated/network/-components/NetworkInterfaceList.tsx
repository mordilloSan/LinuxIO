import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { AnimatePresence, motion } from "framer-motion";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  type MouseEvent,
} from "react";

import { linuxio, type NetworkInterface } from "@/api";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import {
  TRANSITION_DURATION_SLOW_MS,
  EASING_STANDARD,
} from "@/theme/constants";

import NetworkTrafficGraph from "./NetworkTrafficGraph";

export type { NetworkInterface };
const networkRouteApi = getRouteApi("/_authenticated/network");

const NetworkInterfaceList = () => {
  const search = networkRouteApi.useSearch();
  const navigate = networkRouteApi.useNavigate();
  const expanded = typeof search.iface === "string" ? search.iface : undefined;

  const { data: rawInterfaces } = useSuspenseQuery(
    linuxio.network.get_network_info.queryOptions({
      refetchInterval: 1000,
    }),
  );

  // Transform data - filter veths and add type field
  const interfaces = useMemo(
    () =>
      rawInterfaces
        .filter((iface) => !iface.name.startsWith("veth"))
        .map((iface) => ({
          ...iface,
          type: iface.name.startsWith("wl")
            ? "wifi"
            : iface.name.startsWith("lo")
              ? "loopback"
              : "ethernet",
        })),
    [rawInterfaces],
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

  const handleToggle = (iface: NetworkInterface) => {
    if (expanded === iface.name) {
      navigate({
        to: ".",
        search: (previous) => ({
          ...previous,
          iface: undefined,
        }),
      });
    } else {
      navigate({
        to: ".",
        search: (previous) => ({
          ...previous,
          iface: iface.name,
        }),
      });
    }
  };

  const theme = useAppTheme();
  const slowTransitionDurationSeconds = TRANSITION_DURATION_SLOW_MS / 1000;
  const rxCanvasRef = useRef<HTMLCanvasElement>(null);
  const txCanvasRef = useRef<HTMLCanvasElement>(null);

  // Dispatch a synthetic mouse event directly to a canvas (bubbles: false to avoid loops)
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

  const selectedIface = interfaces.find((i) => i.name === expanded);

  return (
    <div>
      <AppGrid container spacing={4}>
        <AnimatePresence>
          {interfaces.map((iface) =>
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
                <NetworkInterfaceCard
                  expanded={expanded === iface.name}
                  iface={iface}
                  onClose={() =>
                    navigate({
                      to: ".",
                      search: (previous) => ({
                        ...previous,
                        iface: undefined,
                      }),
                    })
                  }
                  onToggle={() => handleToggle(iface)}
                />
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
              <div
                onMouseLeave={handleGraphMouseLeave}
                onMouseMove={handleGraphMouseMove}
                style={{ display: "flex", flexDirection: "column", gap: 8 }}
              >
                <div>
                  <div style={{ height: 120, width: "100%", minWidth: 0 }}>
                    <NetworkTrafficGraph
                      color={theme.chart.rx}
                      key={`rx-${selectedIface.name}`}
                      label="RX"
                      ref={rxCanvasRef}
                      value={selectedIface.rx_speed}
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
                      RX: {(selectedIface.rx_speed / 1024).toFixed(1)} kB/s
                    </AppTypography>
                  </div>
                </div>
                <div>
                  <div style={{ height: 120, width: "100%", minWidth: 0 }}>
                    <NetworkTrafficGraph
                      color={theme.chart.tx}
                      key={`tx-${selectedIface.name}`}
                      label="TX"
                      ref={txCanvasRef}
                      value={selectedIface.tx_speed}
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
                      TX: {(selectedIface.tx_speed / 1024).toFixed(1)} kB/s
                    </AppTypography>
                  </div>
                </div>
              </div>
            </AppGrid>
          )}
        </AnimatePresence>
      </AppGrid>
    </div>
  );
};

export default NetworkInterfaceList;
