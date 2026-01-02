import { Icon } from "@iconify/react";
import { Box, Typography, Grid, Tooltip, Fade } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { motion, AnimatePresence } from "framer-motion";
import { useState, useEffect, useMemo } from "react";

import NetworkInterfaceEditor from "./NetworkInterfaceEditor";

import { linuxio } from "@/api/linuxio";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";

export interface NetworkInterface {
  name: string;
  type: string;
  mac: string;
  mtu: number;
  speed: string;
  duplex: string;
  state: number;
  ipv4: string[];
  ipv6: string[];
  rx_speed: number;
  tx_speed: number;
  dns: string[];
  gateway: string;
  ipv4_method?: "auto" | "manual" | "disabled" | "unknown";
}

const getStatusColor = (state: number) => {
  if (state === 100) return "success.main";
  if (state === 30) return "warning.main";
  if (state === 20) return "error.main";
  return "grey.500";
};

const getStatusTooltip = (state: number) => {
  if (state === 100) return "Connected";
  if (state === 30) return "Connecting";
  if (state === 20) return "Disconnected";
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

  const { data: rawInterfaces = [], isPending: isLoading } = linuxio.useCall<
    NetworkInterface[]
  >("dbus", "GetNetworkInfo", [], { refetchInterval: 1000 });

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

  if (isLoading) {
    return <ComponentLoader />;
  }
  return (
    <Box>
      <Grid container spacing={4}>
        <AnimatePresence>
          {interfaces.map((iface) =>
            expanded && expanded !== iface.name ? null : (
              <Grid
                key={iface.name}
                size={{ xs: 12, sm: 4, md: 4, lg: 3, xl: 2 }}
                component={motion.div}
                layout
                initial={{ opacity: 0, scale: 0.95 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.9 }}
                transition={{ duration: 0.2 }}
              >
                <FrostedCard
                  sx={{
                    p: 2,
                    position: "relative",
                    transition: "transform 0.2s, box-shadow 0.2s",
                    cursor: "pointer",
                    ...(expanded !== iface.name && {
                      "&:hover": {
                        transform: "translateY(-4px)",
                        boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
                      },
                    }),
                  }}
                >
                  <Tooltip
                    title={getStatusTooltip(iface.state)}
                    placement="top"
                    arrow
                    slots={{ transition: Fade }}
                    slotProps={{ transition: { timeout: 300 } }}
                  >
                    <Box
                      sx={{
                        position: "absolute",
                        top: 16,
                        right: 8,
                        width: 10,
                        height: 10,
                        borderRadius: "50%",
                        backgroundColor: getStatusColor(iface.state),
                      }}
                    />
                  </Tooltip>

                  <Box
                    display="flex"
                    alignItems="flex-start"
                    onClick={() => handleToggle(iface)}
                  >
                    <Box
                      sx={{
                        width: 44,
                        height: 44,
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        mr: 1.5,
                      }}
                    >
                      <Icon
                        icon={getInterfaceIcon(iface.type)}
                        width={36}
                        height={36}
                        color={primaryColor}
                      />
                    </Box>
                    <Box flexGrow={1}>
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
                    </Box>
                  </Box>
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
        </AnimatePresence>
      </Grid>
    </Box>
  );
};

export default NetworkInterfaceList;
