import { Grid, Typography, CircularProgress, Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import { motion, AnimatePresence } from "framer-motion";
import React, { useState, useRef, useEffect } from "react";

import InterfaceDetails from "./InterfaceDetails";
import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import axios from "@/utils/axios";
import { WireGuardInterface } from "@/types/wireguard";

const WireGuardDashboard: React.FC = () => {
  const [selectedInterface, setSelectedInterface] = useState<string | null>(
    null
  );
  const selectedCardRef = useRef<HTMLDivElement>(null!);
  const interfaceDetailsRef = useRef<HTMLDivElement | null>(null);

  const {
    data: interfaceData,
    isLoading,
    isError,
    refetch,
  } = useQuery<{ interfaces: WireGuardInterface[] }>({
    queryKey: ["wireguardInterfaces"],
    queryFn: async () => {
      const res = await axios.get<{ interfaces: WireGuardInterface[] }>(
        "/wireguard/interfaces"
      );
      return res.data;
    },
    refetchInterval: 50000,
  });

  const WGinterfaces = interfaceData?.interfaces || [];

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
      await axios.delete(`/wireguard/interface/${interfaceName}`);
      refetch();
      setSelectedInterface(null);
    } catch (error) {
      console.error("Failed to delete WireGuard interface:", error);
    }
  };

  const handleAddPeer = async (interfaceName: string, peerData: any) => {
    try {
      await axios.post(`/wireguard/interface/${interfaceName}/peer`, peerData);
      refetch();
    } catch (error) {
      console.error("Failed to add peer:", error);
    }
  };

  const handleToggleInterface = async (
    interfaceName: string,
    status: "up" | "down"
  ) => {
    try {
      if (status !== "up" && status !== "down") {
        throw new Error('Action must be either "up" or "down".');
      }
      // Placeholder for toggle logic
      refetch();
    } catch (error) {
      console.error(`Failed to ${status} WireGuard interface:`, error);
    }
  };

  const handleSelectInterface = (iface: WireGuardInterface) => {
    setSelectedInterface(iface.name === selectedInterface ? null : iface.name);
  };

  return (
    <>
      {isLoading ? (
        <CircularProgress />
      ) : isError ? (
        <Typography color="error">Failed to fetch interfaces</Typography>
      ) : WGinterfaces.length > 0 ? (
        <>
          <AnimatePresence>
            <Grid container spacing={3}>
              {WGinterfaces.map((iface) => (
                <Grid
                  size={{ xs: 12, sm: 6, md: 6, lg: 4, xl: 4 }}
                  key={iface.name}>
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
                  layout>
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
