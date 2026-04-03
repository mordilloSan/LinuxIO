import { motion, AnimatePresence } from "framer-motion";
import { useState, useEffect, useMemo, useRef, useCallback } from "react";
import { useSearchParams } from "react-router-dom";

import NetworkTrafficGraph from "./NetworkTrafficGraph";

import { linuxio, type NetworkInterface } from "@/api";
import NetworkInterfaceCard from "@/components/cards/NetworkInterfaceCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppGrid from "@/components/ui/AppGrid";
import { useAppTheme } from "@/theme";
import AppTypography from "@/components/ui/AppTypography";

export type { NetworkInterface };


const NetworkInterfaceList = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const expanded = searchParams.get("iface");
  const [editForm, setEditForm] = useState<Record<string, any>>({});

  const { data: rawInterfaces = [], isPending: isLoading } =
    linuxio.dbus.get_network_info.useQuery({
      refetchInterval: 1000,
    });

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
        setSearchParams((prev) => {
          prev.delete("iface");
          return prev;
        });
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [setSearchParams]);

  const handleToggle = (iface: NetworkInterface) => {
    if (expanded === iface.name) {
      setSearchParams((prev) => {
        prev.delete("iface");
        return prev;
      });
    } else {
      setEditForm({
        ipv4: Array.isArray(iface.ipv4) ? iface.ipv4.join(", ") : "",
        ipv6: Array.isArray(iface.ipv6) ? iface.ipv6.join(", ") : "",
        dns: iface.dns ? iface.dns : "",
        gateway: iface.gateway ? iface.gateway : "",
        mtu: iface.mtu.toString(),
      });
      setSearchParams((prev) => {
        prev.set("iface", iface.name);
        return prev;
      });
    }
  };

  const handleSave = (iface: NetworkInterface) => {
    console.log("Save", iface.name, editForm);
    setSearchParams((prev) => {
      prev.delete("iface");
      return prev;
    });
  };
  const theme = useAppTheme();
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
    (e: React.MouseEvent) => {
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

  if (isLoading) {
    return <ComponentLoader />;
  }
  const selectedIface = interfaces.find((i) => i.name === expanded);

  return (
    <div>
      <AppGrid container spacing={4}>
        <AnimatePresence>
          {interfaces.map((iface) =>
            expanded && expanded !== iface.name ? null : (
              <AppGrid
                key={iface.name}
                size={
                  expanded === iface.name
                    ? { xs: 12, md: 4, lg: 3 }
                    : { xs: 12, sm: 6, md: 4, lg: 2 }
                }
                component={motion.div}
                layout
                initial={{ opacity: 0, scale: 0.95 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.9 }}
                transition={{ duration: 0.2 }}
              >
                <NetworkInterfaceCard
                  iface={iface}
                  expanded={expanded === iface.name}
                  editForm={editForm}
                  setEditForm={setEditForm}
                  onToggle={() => handleToggle(iface)}
                  onClose={() =>
                    setSearchParams((prev) => {
                      prev.delete("iface");
                      return prev;
                    })
                  }
                  onSave={handleSave}
                />
              </AppGrid>
            ),
          )}

          {/* Traffic graphs — appear on the right when a NIC is selected */}
          {selectedIface && (
            <AppGrid
              key="traffic-graphs"
              size={{ xs: 12, md: 8, lg: 9 }}
              component={motion.div}
              initial={{ opacity: 0, x: 40 }}
              animate={{ opacity: 1, x: 0 }}
              exit={{ opacity: 0, x: 40 }}
              transition={{ duration: 0.3, delay: 0.05 }}
            >
              <div
                onMouseMove={handleGraphMouseMove}
                onMouseLeave={handleGraphMouseLeave}
                style={{ display: "flex", flexDirection: "column", gap: 8 }}
              >
                <div>
                  <div style={{ height: 120, width: "100%", minWidth: 0 }}>
                    <NetworkTrafficGraph
                      ref={rxCanvasRef}
                      key={`rx-${selectedIface.name}`}
                      value={selectedIface.rx_speed}
                      color={theme.chart.rx}
                      label="RX"
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
                    <AppTypography variant="caption" style={{ opacity: 0.7 }}>
                      RX: {(selectedIface.rx_speed / 1024).toFixed(1)} kB/s
                    </AppTypography>
                  </div>
                </div>
                <div>
                  <div style={{ height: 120, width: "100%", minWidth: 0 }}>
                    <NetworkTrafficGraph
                      ref={txCanvasRef}
                      key={`tx-${selectedIface.name}`}
                      value={selectedIface.tx_speed}
                      color={theme.chart.tx}
                      label="TX"
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
                    <AppTypography variant="caption" style={{ opacity: 0.7 }}>
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
