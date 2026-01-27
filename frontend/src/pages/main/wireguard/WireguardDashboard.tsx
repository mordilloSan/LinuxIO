import { Grid, Typography, Box } from "@mui/material";
import { motion, AnimatePresence } from "framer-motion";
import React, { useState, useRef, useEffect, useEffectEvent } from "react";
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
  } = linuxio.wireguard.list_interfaces.useQuery({
    refetchInterval: 10000,
  });

  // Mutations
  const removeInterfaceMutation =
    linuxio.wireguard.remove_interface.useMutation();
  const addPeerMutation = linuxio.wireguard.add_peer.useMutation();
  const upInterfaceMutation = linuxio.wireguard.up_interface.useMutation();
  const downInterfaceMutation = linuxio.wireguard.down_interface.useMutation();
  const enableInterfaceMutation =
    linuxio.wireguard.enable_interface.useMutation();
  const disableInterfaceMutation =
    linuxio.wireguard.disable_interface.useMutation();

  const WGinterfaces = Array.isArray(interfaceData) ? interfaceData : [];

  const handleClickOutside = useEffectEvent(
    (event: MouseEvent | KeyboardEvent) => {
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
    },
  );

  const hasSelectedInterface = Boolean(selectedInterface);

  useEffect(() => {
    if (hasSelectedInterface) {
      document.addEventListener("mousedown", handleClickOutside);
      document.addEventListener("keydown", handleClickOutside);
    }
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("keydown", handleClickOutside);
    };
  }, [hasSelectedInterface]);

  const handleDelete = (interfaceName: string) => {
    removeInterfaceMutation.mutate([interfaceName], {
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
    addPeerMutation.mutate([interfaceName], {
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

    mutation.mutate([interfaceName], {
      onSuccess: () => {
        toast.success(
          `WireGuard interface "${interfaceName}" turned ${status === "up" ? "on" : "off"}.`,
          wireguardToastMeta,
        );
        refetch();
      },
    });
  };

  const handleToggleBootPersistence = (
    interfaceName: string,
    isEnabled: boolean,
  ) => {
    const mutation = isEnabled
      ? disableInterfaceMutation
      : enableInterfaceMutation;

    mutation.mutate([interfaceName], {
      onSuccess: () => {
        toast.success(
          `WireGuard interface "${interfaceName}" ${isEnabled ? "disabled" : "enabled"} for boot persistence.`,
          wireguardToastMeta,
        );
        refetch();
      },
      onError: (error: Error) => {
        toast.error(
          `Failed to ${isEnabled ? "disable" : "enable"} boot persistence: ${error.message}`,
          wireguardToastMeta,
        );
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
                    handleToggleBootPersistence={handleToggleBootPersistence}
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
