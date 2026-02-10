import { Grid, Typography, Box } from "@mui/material";
import { motion, AnimatePresence } from "framer-motion";
import React, { useState, useRef, useEffect, useEffectEvent } from "react";
import { toast } from "sonner";

import InterfaceDetails from "./InterfaceClients";

import { linuxio } from "@/api";
import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { WireGuardInterface } from "@/types/wireguard";
import { getMutationErrorMessage } from "@/utils/mutations";

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
  const { mutate: removeInterface } =
    linuxio.wireguard.remove_interface.useMutation({
      onSuccess: (_, variables) => {
        const [interfaceName] = variables;
        toast.success(
          `WireGuard interface '${interfaceName}' deleted`,
          wireguardToastMeta,
        );
        setSelectedInterface(null);
        refetch();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(
            error,
            "Failed to remove WireGuard interface",
          ),
          wireguardToastMeta,
        );
      },
    });

  const { mutate: addPeer } = linuxio.wireguard.add_peer.useMutation({
    onSuccess: (_, variables) => {
      const [interfaceName] = variables;
      toast.success(`Peer added to '${interfaceName}'`, wireguardToastMeta);
      refetch();
    },
    onError: (error: Error) => {
      toast.error(
        getMutationErrorMessage(error, "Failed to add peer"),
        wireguardToastMeta,
      );
    },
  });

  const { mutate: upInterface } = linuxio.wireguard.up_interface.useMutation({
    onSuccess: (_, variables) => {
      const [interfaceName] = variables;
      toast.success(
        `WireGuard interface "${interfaceName}" turned on.`,
        wireguardToastMeta,
      );
      refetch();
    },
    onError: (error: Error) => {
      toast.error(
        getMutationErrorMessage(error, "Failed to bring interface up"),
        wireguardToastMeta,
      );
    },
  });

  const { mutate: downInterface } =
    linuxio.wireguard.down_interface.useMutation({
      onSuccess: (_, variables) => {
        const [interfaceName] = variables;
        toast.success(
          `WireGuard interface "${interfaceName}" turned off.`,
          wireguardToastMeta,
        );
        refetch();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to bring interface down"),
          wireguardToastMeta,
        );
      },
    });

  const { mutate: enableInterface } =
    linuxio.wireguard.enable_interface.useMutation({
      onSuccess: (_, variables) => {
        const [interfaceName] = variables;
        toast.success(
          `WireGuard interface "${interfaceName}" enabled for boot persistence.`,
          wireguardToastMeta,
        );
        refetch();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to enable boot persistence"),
          wireguardToastMeta,
        );
      },
    });

  const { mutate: disableInterface } =
    linuxio.wireguard.disable_interface.useMutation({
      onSuccess: (_, variables) => {
        const [interfaceName] = variables;
        toast.success(
          `WireGuard interface "${interfaceName}" disabled for boot persistence.`,
          wireguardToastMeta,
        );
        refetch();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to disable boot persistence"),
          wireguardToastMeta,
        );
      },
    });

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
    removeInterface([interfaceName]);
  };

  const handleAddPeer = (interfaceName: string) => {
    addPeer([interfaceName]);
  };

  const handleToggleInterface = (
    interfaceName: string,
    status: "up" | "down",
  ) => {
    const mutation = status === "up" ? upInterface : downInterface;
    mutation([interfaceName]);
  };

  const handleToggleBootPersistence = (
    interfaceName: string,
    isEnabled: boolean,
  ) => {
    const mutation = isEnabled ? disableInterface : enableInterface;
    mutation([interfaceName]);
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
