import { Grid, Typography, Box } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import { motion, AnimatePresence } from "framer-motion";
import React, { useState, useRef, useEffect } from "react";
import { toast } from "sonner";

import InterfaceDetails from "./InterfaceClients";

import { linuxio } from "@/api/linuxio";
import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { WireGuardInterface } from "@/types/wireguard";

const wireguardToastMeta = {
  meta: { href: "/wireguard", label: "Open WireGuard" },
};

const WireGuardDashboard: React.FC = () => {
  const [selectedInterface, setSelectedInterface] = useState<string | null>(
    null,
  );
  const selectedCardRef = useRef<HTMLDivElement>(null!);
  const interfaceDetailsRef = useRef<HTMLDivElement | null>(null);
  const queryClient = useQueryClient();

  const {
    data: interfaceData,
    isPending: isLoading,
    isError,
    refetch,
  } = linuxio.useCall<WireGuardInterface[]>(
    "wireguard",
    "list_interfaces",
    [],
    {
      refetchInterval: 10000,
    },
  );

  const WGinterfaces = Array.isArray(interfaceData) ? interfaceData : [];

  useEffect(() => {
    function handleClickOutside(event: MouseEvent | KeyboardEvent) {
      if (event.type === "mousedown") {
        const mouseEvent = event as MouseEvent;
        if (
          selectedCardRef.current &&
          !selectedCardRef.current.contains(mouseEvent.target as Node) &&
          interfaceDetailsRef.current &&
          !interfaceDetailsRef.current.contains(mouseEvent.target as Node)
        ) {
          setSelectedInterface(null);
        }
      } else if (event.type === "keydown") {
        const keyboardEvent = event as KeyboardEvent;
        if (keyboardEvent.key === "Escape" || keyboardEvent.key === "Esc") {
          setSelectedInterface(null);
        }
      }
    }

    if (selectedInterface) {
      document.addEventListener("mousedown", handleClickOutside);
      document.addEventListener("keydown", handleClickOutside);
    }
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("keydown", handleClickOutside);
    };
  }, [selectedInterface]);

  const handleDelete = async (interfaceName: string) => {
    try {
      await linuxio.request("wireguard", "remove_interface", [interfaceName]);
      toast.success(
        `WireGuard interface '${interfaceName}' deleted`,
        wireguardToastMeta,
      );
      refetch();
      setSelectedInterface(null);
    } catch (error) {
      toast.error(
        `Failed to delete interface '${interfaceName}'`,
        wireguardToastMeta,
      );
      console.error("Failed to delete WireGuard interface:", error);
    }
  };

  const handleAddPeer = async (interfaceName: string) => {
    try {
      await linuxio.request("wireguard", "add_peer", [interfaceName]);
      toast.success(`Peer added to '${interfaceName}'`, wireguardToastMeta);
      refetch();
      queryClient.invalidateQueries({
        queryKey: ["stream", "wireguard", "list_peers", interfaceName],
      });
    } catch (error) {
      toast.error(
        `Failed to add peer to '${interfaceName}'`,
        wireguardToastMeta,
      );
      console.error("Failed to add peer:", error);
    }
  };

  const handleToggleInterface = async (
    interfaceName: string,
    status: "up" | "down",
  ) => {
    try {
      if (status !== "up" && status !== "down") {
        throw new Error('Action must be either "up" or "down".');
      }
      const command = status === "up" ? "up_interface" : "down_interface";
      await linuxio.request("wireguard", command, [interfaceName]);
      toast.success(
        `WireGuard interface "${interfaceName}" turned ${status === "up" ? "on" : "off"}.`,
        wireguardToastMeta,
      );
      refetch();
    } catch (error: any) {
      toast.error(
        `Failed to turn ${status} WireGuard interface "${interfaceName}": ${error?.message || "Unknown error"}`,
        wireguardToastMeta,
      );
      console.error(`Failed to ${status} WireGuard interface:`, error);
    }
  };

  const handleSelectInterface = (iface: WireGuardInterface) => {
    setSelectedInterface(iface.name === selectedInterface ? null : iface.name);
  };

  return (
    <>
      {isLoading ? (
        <ComponentLoader />
      ) : isError ? (
        <Typography color="error">Failed to fetch interfaces</Typography>
      ) : WGinterfaces.length > 0 ? (
        <>
          <AnimatePresence>
            <Grid container spacing={3}>
              {WGinterfaces.map((iface) => (
                <Grid
                  size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 3 }}
                  key={iface.name}
                >
                  <WireguardInterfaceCard
                    iface={iface}
                    selectedInterface={selectedInterface}
                    selectedCardRef={
                      iface.name === selectedInterface ? selectedCardRef : null
                    }
                    handleSelectInterface={handleSelectInterface}
                    handleToggleInterface={handleToggleInterface}
                    handleDelete={handleDelete}
                    handleAddPeer={handleAddPeer}
                  />
                </Grid>
              ))}
            </Grid>
          </AnimatePresence>
          {selectedInterface && (
            <Grid container spacing={3}>
              <Grid size={{ xs: 12 }}>
                <motion.div
                  initial={{ opacity: 0, x: -20 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: 20 }}
                  transition={{ duration: 0.5 }}
                  layout
                >
                  <Box mt={4} mb={2}>
                    <Typography variant="h5" gutterBottom>
                      Clients for {selectedInterface}
                    </Typography>
                  </Box>
                  <div ref={interfaceDetailsRef}>
                    <InterfaceDetails params={{ id: selectedInterface }} />
                  </div>
                </motion.div>
              </Grid>
            </Grid>
          )}
        </>
      ) : (
        <Typography color="textSecondary">No interfaces found</Typography>
      )}
    </>
  );
};

export default WireGuardDashboard;
