import { AnimatePresence, motion } from "framer-motion";
import React, { useEffect, useEffectEvent, useRef, useState } from "react";

import InterfaceDetails from "./InterfaceClients";

import { linuxio, type WireGuardInterface } from "@/api";
import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import PageLoader from "@/components/loaders/PageLoader";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";

const WIREGUARD_TOAST_META = { href: "/wireguard", label: "Open WireGuard" };

const WireGuardDashboard = () => {
  const theme = useAppTheme();
  const toast = useScopedToast(WIREGUARD_TOAST_META);
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
  } = linuxio.wireguard.list_interfaces.useQuery({
    refetchInterval: 10000,
  });

  const interfaceActionConfig = (verb: string, fallback: string) => ({
    success: (_result: void, variables: { name: string }) =>
      toast.success(`WireGuard interface "${variables.name}" ${verb}`),
    error: fallback,
    toast: WIREGUARD_TOAST_META,
  });

  // Mutations
  const { mutate: removeInterface } =
    linuxio.wireguard.remove_interface.useJobAction({
      success: (_result, variables) => {
        toast.success(`WireGuard interface '${variables.name}' deleted`);
        setSelectedInterface(null);
      },
      error: "Failed to remove WireGuard interface",
      toast: WIREGUARD_TOAST_META,
    });

  const { mutate: addPeer } = linuxio.wireguard.add_peer.useJobAction({
    success: (_result, variables) =>
      toast.success(`Peer added to '${variables.interfaceName}'`),
    error: "Failed to add peer",
    toast: WIREGUARD_TOAST_META,
  });

  const { mutate: upInterface } = linuxio.wireguard.up_interface.useJobAction(
    interfaceActionConfig("turned on.", "Failed to bring interface up"),
  );

  const { mutate: downInterface } =
    linuxio.wireguard.down_interface.useJobAction(
      interfaceActionConfig("turned off.", "Failed to bring interface down"),
    );

  const { mutate: enableInterface } =
    linuxio.wireguard.enable_interface.useJobAction(
      interfaceActionConfig(
        "enabled for boot persistence.",
        "Failed to enable boot persistence",
      ),
    );

  const { mutate: disableInterface } =
    linuxio.wireguard.disable_interface.useJobAction(
      interfaceActionConfig(
        "disabled for boot persistence.",
        "Failed to disable boot persistence",
      ),
    );

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
    removeInterface({ name: interfaceName });
  };

  const handleAddPeer = (interfaceName: string) => {
    addPeer({ interfaceName });
  };

  const handleToggleInterface = (
    interfaceName: string,
    status: "up" | "down",
  ) => {
    const mutation = status === "up" ? upInterface : downInterface;
    mutation({ name: interfaceName });
  };

  const handleToggleBootPersistence = (
    interfaceName: string,
    isEnabled: boolean,
  ) => {
    const mutation = isEnabled ? disableInterface : enableInterface;
    mutation({ name: interfaceName });
  };

  const handleSelectInterface = (iface: WireGuardInterface) => {
    setSelectedInterface(iface.name === selectedInterface ? null : iface.name);
  };

  return (
    <>
      {isLoading ? (
        <PageLoader />
      ) : isError ? (
        <AppTypography color="error">
          {error?.message || "Failed to fetch interfaces"}
        </AppTypography>
      ) : WGinterfaces.length > 0 ? (
        <>
          <AnimatePresence>
            <AppGrid container spacing={3}>
              {WGinterfaces.map((iface) => (
                <AppGrid
                  key={iface.name}
                  size={{ xs: 12, sm: 6, md: 4, lg: 3 }}
                >
                  <WireguardInterfaceCard
                    handleAddPeer={handleAddPeer}
                    handleDelete={handleDelete}
                    handleSelectInterface={handleSelectInterface}
                    handleToggleBootPersistence={handleToggleBootPersistence}
                    handleToggleInterface={handleToggleInterface}
                    iface={iface}
                    selectedCardRef={
                      iface.name === selectedInterface ? selectedCardRef : null
                    }
                    selectedInterface={selectedInterface}
                  />
                </AppGrid>
              ))}
            </AppGrid>
          </AnimatePresence>
          {selectedInterface && (
            <AppGrid container spacing={3}>
              <AppGrid size={{ xs: 12 }}>
                <motion.div
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: 20 }}
                  initial={{ opacity: 0, x: -20 }}
                  layout
                  transition={{ duration: 0.5 }}
                >
                  <div
                    style={{
                      marginTop: theme.spacing(4),
                      marginBottom: theme.spacing(2),
                    }}
                  >
                    <AppTypography gutterBottom variant="h5">
                      Clients for {selectedInterface}
                    </AppTypography>
                  </div>
                  <div ref={interfaceDetailsRef}>
                    <InterfaceDetails params={{ id: selectedInterface }} />
                  </div>
                </motion.div>
              </AppGrid>
            </AppGrid>
          )}
        </>
      ) : (
        <AppTypography color="text.secondary">
          No interfaces found
        </AppTypography>
      )}
    </>
  );
};

export default WireGuardDashboard;
