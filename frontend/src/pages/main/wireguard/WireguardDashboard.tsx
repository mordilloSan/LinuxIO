import { Grid, Typography, Box } from "@mui/material";
import { motion, AnimatePresence } from "framer-motion";
import React, { useState, useRef, useEffect } from "react";
import { toast } from "sonner";

import InterfaceDetails from "./InterfaceClients";

import linuxio from "@/api/react-query";
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

  const {
    data: interfaceData,
    isPending: isLoading,
    isError,
    error,
    refetch,
  } = linuxio.useCall<WireGuardInterface[]>(
    "wireguard",
    "list_interfaces",
    [],
    {
      refetchInterval: 10000,
    },
  );

  // Mutations
  const removeInterfaceMutation = linuxio.useMutate<unknown, string>(
    "wireguard",
    "remove_interface",
  );

  const addPeerMutation = linuxio.useMutate<unknown, string>(
    "wireguard",
    "add_peer",
  );

  const upInterfaceMutation = linuxio.useMutate<unknown, string>(
    "wireguard",
    "up_interface",
  );

  const downInterfaceMutation = linuxio.useMutate<unknown, string>(
    "wireguard",
    "down_interface",
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

  const handleDelete = (interfaceName: string) => {
    removeInterfaceMutation.mutate(interfaceName, {
      onSuccess: () => {
        toast.success(
          `WireGuard interface '${interfaceName}' deleted`,
          wireguardToastMeta,
        );
        refetch();
        setSelectedInterface(null);
      },
    });
  };

  const handleAddPeer = (interfaceName: string) => {
    addPeerMutation.mutate(interfaceName, {
      onSuccess: () => {
        toast.success(`Peer added to '${interfaceName}'`, wireguardToastMeta);
        refetch();
      },
    });
  };

  const handleToggleInterface = (
    interfaceName: string,
    status: "up" | "down",
  ) => {
    const mutation =
      status === "up" ? upInterfaceMutation : downInterfaceMutation;

    mutation.mutate(interfaceName, {
      onSuccess: () => {
        toast.success(
          `WireGuard interface "${interfaceName}" turned ${status === "up" ? "on" : "off"}.`,
          wireguardToastMeta,
        );
        refetch();
      },
    });
  };

  const handleSelectInterface = (iface: WireGuardInterface) => {
    setSelectedInterface(iface.name === selectedInterface ? null : iface.name);
  };

  return (
    <>
      {isLoading ? (
        <ComponentLoader />
      ) : isError ? (
        <Typography color="error">
          {error?.message || "Failed to fetch interfaces"}
        </Typography>
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
