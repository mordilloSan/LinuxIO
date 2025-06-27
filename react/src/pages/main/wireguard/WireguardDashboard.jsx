import { Grid, Typography, CircularProgress, Box } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import { motion, AnimatePresence } from "framer-motion";
import React, { useState, useRef, useEffect } from "react";

import InterfaceDetails from "./InterfaceDetails";

import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import axios from "@/utils/axios";

const WireGuardDashboard = () => {
  const [selectedInterface, setSelectedInterface] = useState(null);
  const selectedCardRef = useRef(null);
  const interfaceDetailsRef = useRef(null);

  // Fetch the WireGuard interfaces -- fix here!
  const {
    data: interfaceData,
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: ["wireguardInterfaces"],
    queryFn: async () => {
      const res = await axios.get("/wireguard/interfaces");
      return res.data;
    },
    refetchInterval: 50000,
  });

  const WGinterfaces = interfaceData?.interfaces || [];

  useEffect(() => {
    function handleClickOutside(event) {
      if (event.type === "mousedown") {
        if (
          selectedCardRef.current &&
          !selectedCardRef.current.contains(event.target) &&
          interfaceDetailsRef.current &&
          !interfaceDetailsRef.current.contains(event.target)
        ) {
          setSelectedInterface(null);
        }
      } else if (event.type === "keydown") {
        if (event.key === "Escape" || event.key === "Esc") {
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

  const handleDelete = async (interfaceName) => {
    try {
      await axios.delete(`/wireguard/interface/${interfaceName}`);
      refetch();
      setSelectedInterface(null);
    } catch (error) {
      console.error("Failed to delete WireGuard interface:", error);
    }
  };

  const handleAddPeer = async (interfaceName, peerData) => {
    try {
      await axios.post(`/wireguard/interface/${interfaceName}/peer`, peerData);
      refetch();
    } catch (error) {
      console.error("Failed to add peer:", error);
    }
  };

  const handleToggleInterface = async (interfaceName, status) => {
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

  const handleSelectInterface = (iface) => {
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
                <Grid item xs={12} md={6} lg={4} key={iface.name}>
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
              <Grid item xs={12}>
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
