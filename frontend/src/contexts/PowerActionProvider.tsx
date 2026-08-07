import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";
import { redirectToSignIn } from "@/utils/navigation";

import { PowerActionContext } from "./PowerActionContext";

type PowerActionState = "rebooting" | "poweringOff" | null;

export const PowerActionProvider = ({ children }: { children: ReactNode }) => {
  const [powerAction, setPowerAction] = useState<PowerActionState>(null);

  // Stable action functions - never cause re-renders in consumers
  const triggerReboot = useCallback(() => setPowerAction("rebooting"), []);
  const triggerPowerOff = useCallback(() => setPowerAction("poweringOff"), []);

  // Poll server until it's back up after reboot
  useEffect(() => {
    if (powerAction !== "rebooting") return;

    let cancelled = false;
    let pollTimeout: ReturnType<typeof setTimeout> | null = null;
    const pollInterval = 3000;
    let poll: () => Promise<void>;

    const schedulePoll = () => {
      pollTimeout = setTimeout(poll, pollInterval);
    };

    poll = async () => {
      try {
        const response = await fetch("/api/version", {
          method: "GET",
          cache: "no-store",
        });
        if (response.ok && !cancelled) {
          redirectToSignIn();
        } else if (!cancelled) {
          schedulePoll();
        }
      } catch {
        if (!cancelled) {
          schedulePoll();
        }
      }
    };

    const initialDelay = setTimeout(poll, 5000);

    return () => {
      cancelled = true;
      clearTimeout(initialDelay);
      if (pollTimeout !== null) clearTimeout(pollTimeout);
    };
  }, [powerAction]);

  const value = useMemo(
    () => ({ triggerReboot, triggerPowerOff }),
    [triggerReboot, triggerPowerOff],
  );

  return (
    <PowerActionContext.Provider value={value}>
      {children}
      {powerAction !== null ? (
        <div
          style={{
            position: "fixed",
            inset: 0,
            zIndex: 1301,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            padding: "24px",
            color: "#fff",
            background: "rgba(0, 0, 0, 0.52)",
            backdropFilter: "blur(16px)",
          }}
        >
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              gap: 12,
              textAlign: "center",
              paddingLeft: "12px",
              paddingRight: "12px",
            }}
          >
            <AppTypography component="div" gutterBottom variant="h5">
              {powerAction === "rebooting"
                ? "Rebooting..."
                : "Shutting Down..."}
            </AppTypography>
            <AppTypography style={{ opacity: 0.8 }} variant="body1">
              {powerAction === "rebooting"
                ? "Please wait while the system restarts. You will be redirected once the server is back online."
                : "The system is shutting down. You may close this window."}
            </AppTypography>
            {powerAction === "rebooting" && <ComponentLoader />}
          </div>
        </div>
      ) : null}
    </PowerActionContext.Provider>
  );
};
