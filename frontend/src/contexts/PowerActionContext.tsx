import { Backdrop, Box, Typography } from "@mui/material";
import React, {
  createContext,
  useState,
  useCallback,
  useMemo,
  useEffect,
} from "react";
import { useNavigate } from "react-router-dom";

import ComponentLoader from "@/components/loaders/ComponentLoader";

type PowerActionState = "rebooting" | "poweringOff" | null;

export interface PowerActionContextType {
  triggerReboot: () => void;
  triggerPowerOff: () => void;
}

export const PowerActionContext = createContext<
  PowerActionContextType | undefined
>(undefined);

export const PowerActionProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const navigate = useNavigate();
  const [powerAction, setPowerAction] = useState<PowerActionState>(null);

  // Stable action functions - never cause re-renders in consumers
  const triggerReboot = useCallback(() => setPowerAction("rebooting"), []);
  const triggerPowerOff = useCallback(() => setPowerAction("poweringOff"), []);

  // Poll server until it's back up after reboot
  useEffect(() => {
    if (powerAction !== "rebooting") return;

    let cancelled = false;
    const pollInterval = 3000;

    const poll = async () => {
      try {
        const response = await fetch("/api/version", {
          method: "GET",
          cache: "no-store",
        });
        if (response.ok && !cancelled) {
          window.location.href = "/sign-in";
        } else if (!cancelled) {
          setTimeout(poll, pollInterval);
        }
      } catch {
        if (!cancelled) {
          setTimeout(poll, pollInterval);
        }
      }
    };

    const initialDelay = setTimeout(poll, 5000);

    return () => {
      cancelled = true;
      clearTimeout(initialDelay);
    };
  }, [powerAction, navigate]);

  const value = useMemo(
    () => ({ triggerReboot, triggerPowerOff }),
    [triggerReboot, triggerPowerOff],
  );

  return (
    <PowerActionContext.Provider value={value}>
      {children}
      <Backdrop
        sx={{
          color: "#fff",
          zIndex: (theme) => theme.zIndex.modal + 1,
          flexDirection: "column",
          gap: 3,
          backdropFilter: "blur(16px)",
        }}
        open={powerAction !== null}
      >
        <Box textAlign="center" px={3}>
          <Typography variant="h5" component="div" gutterBottom>
            {powerAction === "rebooting" ? "Rebooting..." : "Shutting Down..."}
          </Typography>
          <Typography variant="body1" sx={{ opacity: 0.8 }}>
            {powerAction === "rebooting"
              ? "Please wait while the system restarts. You will be redirected once the server is back online."
              : "The system is shutting down. You may close this window."}
          </Typography>
          {powerAction === "rebooting" && <ComponentLoader />}
        </Box>
      </Backdrop>
    </PowerActionContext.Provider>
  );
};
