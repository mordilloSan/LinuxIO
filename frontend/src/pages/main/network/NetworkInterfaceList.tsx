import { Icon } from "@iconify/react";
import { Typography, Grid, Tooltip, Fade } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { motion, AnimatePresence } from "framer-motion";
import { useState, useEffect, useMemo, useRef, useCallback } from "react";

import NetworkInterfaceEditor from "./NetworkInterfaceEditor";
import NetworkTrafficGraph from "./NetworkTrafficGraph";

import { linuxio, type NetworkInterface } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";

export type { NetworkInterface };

// NetworkManager device states
// 10=unmanaged, 20=unavailable, 30=disconnected, 40-90=connecting stages, 100=activated, 110=deactivating, 120=failed
const getStatusTooltip = (state: number) => {
  if (state === 100) return "Connected";
  if (state === 110) return "Deactivating";
  if (state >= 40 && state <= 90) return "Connecting";
  if (state === 30) return "Disconnected";
  if (state === 20) return "Unavailable";
  if (state === 120) return "Failed";
  if (state === 10) return "Unmanaged";
  return "Unknown";
};

const getInterfaceIcon = (type?: string) => {
  if (type === "wifi") return "mdi:wifi";
  if (type === "ethernet") return "mdi:ethernet";
  if (type === "loopback") return "mdi:lan-connect";
  return "mdi:network";
};

const formatBps = (bps?: number) =>
  typeof bps === "number" ? `${(bps / 1024).toFixed(1)} kB/s` : "N/A";

const NetworkInterfaceList = () => {
  const [expanded, setExpanded] = useState<string | null>(null);
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
      if (e.key === "Escape") setExpanded(null);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  const handleToggle = (iface: NetworkInterface) => {
    if (expanded === iface.name) {
      setExpanded(null);
    } else {
      setEditForm({
        ipv4: Array.isArray(iface.ipv4) ? iface.ipv4.join(", ") : "",
        ipv6: Array.isArray(iface.ipv6) ? iface.ipv6.join(", ") : "",
        dns: iface.dns ? iface.dns : "",
        gateway: iface.gateway ? iface.gateway : "",
        mtu: iface.mtu.toString(),
      });
      setExpanded(iface.name);
    }
  };

  const handleSave = (iface: NetworkInterface) => {
    console.log("Save", iface.name, editForm);
    setExpanded(null);
  };
  const theme = useTheme();
  const primaryColor = theme.palette.primary.main;
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
      <Grid container spacing={4}>
        <AnimatePresence>
          {interfaces.map((iface) =>
            expanded && expanded !== iface.name ? null : (
              <Grid
                key={iface.name}
                size={
                  expanded === iface.name
                    ? { xs: 12, md: 4, lg: 3 }
                    : { xs: 12, sm: 4, md: 4, lg: 3, xl: 2 }
                }
                component={motion.div}
                layout
                initial={{ opacity: 0, scale: 0.95 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.9 }}
                transition={{ duration: 0.2 }}
              >
                <FrostedCard
                  hoverLift={expanded !== iface.name}
                  style={{
                    padding: 8,
                    position: "relative",
                    cursor: "pointer",
                  }}
                >
                  <Tooltip
                    title={getStatusTooltip(iface.state)}
                    placement="top"
                    arrow
                    slots={{ transition: Fade }}
                    slotProps={{ transition: { timeout: 300 } }}
                  >
                    <span
                      style={{
                        position: "absolute",
                        top: 16,
                        right: 8,
                        width: 10,
                        height: 10,
                        borderRadius: "50%",
                        display: "inline-block",
                        backgroundColor:
                          iface.state === 100
                            ? theme.palette.success.main
                            : iface.state >= 40 && iface.state <= 90
                              ? theme.palette.warning.main
                              : iface.state === 30 || iface.state === 120
                                ? theme.palette.error.main
                                : theme.palette.grey[500],
                      }}
                    />
                  </Tooltip>

                  <div
                    style={{ display: "flex", alignItems: "flex-start" }}
                    onClick={() => handleToggle(iface)}
                  >
                    <div
                      style={{
                        width: 44,
                        height: 44,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        marginRight: 6,
                      }}
                    >
                      <Icon
                        icon={getInterfaceIcon(iface.type)}
                        width={36}
                        height={36}
                        color={primaryColor}
                      />
                    </div>
                    <div style={{ flexGrow: 1 }}>
                      <Typography variant="subtitle1" fontWeight={600} noWrap>
                        {iface.name}
                      </Typography>
                      <Typography variant="body2" color="text.secondary" noWrap>
                        IPv4:{" "}
                        {Array.isArray(iface.ipv4)
                          ? iface.ipv4.join(", ")
                          : "N/A"}
                      </Typography>
                      <Typography variant="body2" color="text.secondary" noWrap>
                        MAC: {iface.mac}
                      </Typography>

                      <Typography variant="body2" color="text.secondary" noWrap>
                        {iface.speed === "unknown" ||
                        iface.speed.startsWith("-1")
                          ? "No Carrier"
                          : `Link Speed: ${iface.speed}${iface.duplex !== "unknown" ? ` (${iface.duplex})` : ""}`}
                      </Typography>

                      <Typography variant="body2" color="text.secondary" noWrap>
                        RX/s: {formatBps(iface.rx_speed)} | TX/s:{" "}
                        {formatBps(iface.tx_speed)}
                      </Typography>
                    </div>
                  </div>
                  <NetworkInterfaceEditor
                    iface={iface}
                    expanded={expanded === iface.name}
                    editForm={editForm}
                    setEditForm={setEditForm}
                    onClose={() => setExpanded(null)}
                    onSave={handleSave}
                  />
                </FrostedCard>
              </Grid>
            ),
          )}

          {/* Traffic graphs — appear on the right when a NIC is selected */}
          {selectedIface && (
            <Grid
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
                    <Typography variant="caption" sx={{ opacity: 0.7 }}>
                      RX: {(selectedIface.rx_speed / 1024).toFixed(1)} kB/s
                    </Typography>
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
                    <Typography variant="caption" sx={{ opacity: 0.7 }}>
                      TX: {(selectedIface.tx_speed / 1024).toFixed(1)} kB/s
                    </Typography>
                  </div>
                </div>
              </div>
            </Grid>
          )}
        </AnimatePresence>
      </Grid>
    </div>
  );
};

export default NetworkInterfaceList;
