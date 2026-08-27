import { AnimatePresence, motion } from "motion/react";
import { useEffect, useEffectEvent, useRef, useState } from "react";

import { linuxio, type WireGuardInterface, useCallMutation } from "@/api";
import WireguardInterfaceCard from "@/components/cards/WireguardInterfaceCard";
import type { WireguardInterfaceAction } from "@/components/cards/WireguardInterfaceCard";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useScopedToast } from "@/hooks/useScopedToast";
import {
  CARD_GRID_SIZE_STANDARD,
  EASING_STANDARD,
  TRANSITION_DURATION_SLOW_MS,
} from "@/theme/constants";

import InterfaceDetails from "./InterfaceClients";

const WIREGUARD_TOAST_META = {
  label: "Open WireGuard",
  to: "/wireguard",
} as const;

interface WireGuardDashboardProps {
  interfaces: WireGuardInterface[];
}

const getWireguardInterfaceId = (iface: WireGuardInterface) => iface.name;

const WireGuardDashboard = ({ interfaces }: WireGuardDashboardProps) => {
  const toast = useScopedToast(WIREGUARD_TOAST_META);
  const [selectedInterface, setSelectedInterface] = useState<string | null>(
    null,
  );
  const [pendingActions, setPendingActions] = useState<
    ReadonlyMap<string, WireguardInterfaceAction>
  >(() => new Map());
  const selectedCardRef = useRef<HTMLDivElement>(null!);
  const interfaceDetailsRef = useRef<HTMLDivElement | null>(null);

  const interfaceActionConfig = (verb: string, fallback: string) => ({
    success: (_result: void, variables: { name: string }) =>
      toast.success(`WireGuard interface "${variables.name}" ${verb}`),
    error: fallback,
    toast: WIREGUARD_TOAST_META,
  });

  // Mutations
  const removeInterface = useCallMutation(linuxio.wireguard.remove_interface, {
    success: (_result, variables) => {
      toast.success(`WireGuard interface '${variables.name}' deleted`);
      setSelectedInterface(null);
    },
    error: "Failed to remove WireGuard interface",
    toast: WIREGUARD_TOAST_META,
  });

  const addPeer = useCallMutation(linuxio.wireguard.add_peer, {
    success: (_result, variables) =>
      toast.success(`Peer added to '${variables.interfaceName}'`),
    error: "Failed to add peer",
    toast: WIREGUARD_TOAST_META,
  });

  const upInterface = useCallMutation(
    linuxio.wireguard.up_interface,
    interfaceActionConfig("turned on.", "Failed to bring interface up"),
  );

  const downInterface = useCallMutation(
    linuxio.wireguard.down_interface,
    interfaceActionConfig("turned off.", "Failed to bring interface down"),
  );

  const enableInterface = useCallMutation(
    linuxio.wireguard.enable_interface,
    interfaceActionConfig(
      "enabled for boot persistence.",
      "Failed to enable boot persistence",
    ),
  );

  const disableInterface = useCallMutation(
    linuxio.wireguard.disable_interface,
    interfaceActionConfig(
      "disabled for boot persistence.",
      "Failed to disable boot persistence",
    ),
  );

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

  const runInterfaceAction = (
    interfaceName: string,
    action: WireguardInterfaceAction,
    run: () => Promise<unknown>,
  ) => {
    if (pendingActions.has(interfaceName)) return;

    setPendingActions((current) => new Map(current).set(interfaceName, action));
    void run()
      .catch(() => undefined)
      .finally(() => {
        setPendingActions((current) => {
          if (current.get(interfaceName) !== action) return current;
          const next = new Map(current);
          next.delete(interfaceName);
          return next;
        });
      });
  };

  const handleDelete = (interfaceName: string) => {
    runInterfaceAction(interfaceName, "delete", () =>
      removeInterface.mutateAsync({ name: interfaceName }),
    );
  };

  const handleAddPeer = (interfaceName: string) => {
    runInterfaceAction(interfaceName, "add-peer", () =>
      addPeer.mutateAsync({ interfaceName }),
    );
  };

  const handleToggleInterface = (
    interfaceName: string,
    status: "up" | "down",
  ) => {
    runInterfaceAction(interfaceName, status, () =>
      status === "up"
        ? upInterface.mutateAsync({ name: interfaceName })
        : downInterface.mutateAsync({ name: interfaceName }),
    );
  };

  const handleToggleBootPersistence = (
    interfaceName: string,
    isEnabled: boolean,
  ) => {
    const action = isEnabled ? "disable" : "enable";
    runInterfaceAction(interfaceName, action, () =>
      isEnabled
        ? disableInterface.mutateAsync({ name: interfaceName })
        : enableInterface.mutateAsync({ name: interfaceName }),
    );
  };

  const surface = useReorderableSurface({
    getId: getWireguardInterfaceId,
    items: interfaces,
    surface: "wireguard.interfaces",
  });

  const handleSelectInterface = (iface: WireGuardInterface) => {
    setSelectedInterface(iface.name === selectedInterface ? null : iface.name);
  };

  return (
    <>
      {interfaces.length > 0 ? (
        <>
          <ReorderableCardGrid
            fillAvailable={false}
            getId={getWireguardInterfaceId}
            renderItem={(iface) => (
              <WireguardInterfaceCard
                handleAddPeer={handleAddPeer}
                handleDelete={handleDelete}
                handleSelectInterface={handleSelectInterface}
                handleToggleBootPersistence={handleToggleBootPersistence}
                handleToggleInterface={handleToggleInterface}
                iface={iface}
                pendingAction={pendingActions.get(iface.name)}
                selectedCardRef={
                  iface.name === selectedInterface ? selectedCardRef : null
                }
                selectedInterface={selectedInterface}
              />
            )}
            size={CARD_GRID_SIZE_STANDARD}
            surface={surface}
          />
          <AnimatePresence>
            {selectedInterface && (
              <AppGrid container spacing={3}>
                <AppGrid size={{ xs: 12 }}>
                  <motion.div
                    animate={{ opacity: 1, x: 0 }}
                    exit={{ opacity: 0, x: 20 }}
                    initial={{ opacity: 0, x: -20 }}
                    layout
                    transition={{
                      duration: TRANSITION_DURATION_SLOW_MS / 1000,
                      ease: EASING_STANDARD,
                    }}
                  >
                    <div
                      style={{
                        marginTop: "var(--app-space-16)",
                        marginBottom: "var(--app-space-8)",
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
          </AnimatePresence>
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
