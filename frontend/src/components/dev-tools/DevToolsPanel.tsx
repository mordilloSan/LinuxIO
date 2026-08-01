import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useState, useSyncExternalStore } from "react";

import { DevtoolsModal } from "@/components/dev-tools/DevtoolsModal";
import AppButton from "@/components/ui/AppButton";
import {
  getWebVitalsSnapshot,
  subscribeToWebVitals,
  WEB_VITAL_NAMES,
  type WebVitalName,
} from "@/performance/webVitalsStore";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface DevToolsPanelProps {
  isOpen: boolean;
  onClose: () => void;
}

function formatWebVital(name: WebVitalName, value: number) {
  return name === "CLS" ? value.toFixed(3) : `${Math.round(value)} ms`;
}

/**
 * Dev-only tool panel for testing and debugging.
 * Only rendered when import.meta.env.DEV is true.
 */
export const DevToolsPanel = ({ isOpen, onClose }: DevToolsPanelProps) => {
  const theme = useAppTheme();
  // Check if update notification is currently shown
  const shown = !!sessionStorage.getItem("dev_update_forced");
  const [isDevtoolsOpen, setIsDevtoolsOpen] = useState(false);
  const [isRouterDevtoolsOpen, setIsRouterDevtoolsOpen] = useState(false);
  const webVitals = useSyncExternalStore(
    subscribeToWebVitals,
    getWebVitalsSnapshot,
    getWebVitalsSnapshot,
  );

  const forceUpdateNotification = () => {
    const fakeUpdateInfo = {
      available: true,
      current_version: "dev-v0.6.12",
      latest_version: "dev-v0.6.12",
      release_url: "https://github.com/mordilloSan/LinuxIO/releases",
    };

    sessionStorage.setItem("update_info", JSON.stringify(fakeUpdateInfo));
    sessionStorage.setItem("dev_update_forced", "true");
    window.location.reload();
  };

  const clearUpdateNotification = () => {
    sessionStorage.removeItem("update_info");
    sessionStorage.removeItem("dev_update_forced");
    window.location.reload();
  };

  if (!import.meta.env.DEV || !isOpen) {
    return null;
  }

  return (
    <>
      {/* Dev Tools Panel */}
      <div
        style={{
          position: "fixed",
          bottom: 60,
          right: 20,
          zIndex: 9999,
          color: theme.palette.common.white,
          padding: "12px 16px",
          borderRadius: 8,
          boxShadow: `0 4px 6px ${alpha(theme.palette.common.black, 0.3)}`,
          backgroundColor: alpha(theme.palette.background.paper, 0.92),
          fontSize: 14,
          display: "flex",
          flexDirection: "column",
          gap: 8,
          minWidth: 200,
        }}
      >
        <div
          style={{
            fontWeight: "bold",
            marginBottom: 4,
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <span> Dev Tools</span>
          <button
            onClick={onClose}
            style={{
              background: "transparent",
              border: "none",
              color: theme.palette.common.white,
              cursor: "pointer",
              fontSize: 18,
              padding: 0,
              marginLeft: 8,
            }}
          >
            ×
          </button>
        </div>
        <AppButton
          color="primary"
          fullWidth
          onClick={() => {
            setIsDevtoolsOpen(!isDevtoolsOpen);
            setIsRouterDevtoolsOpen(false);
          }}
          size="small"
          variant="contained"
        >
          {isDevtoolsOpen ? "Close" : "Open"} Tanstack Query Devtools
        </AppButton>
        <AppButton
          color="primary"
          fullWidth
          onClick={() => {
            setIsRouterDevtoolsOpen(!isRouterDevtoolsOpen);
            setIsDevtoolsOpen(false);
          }}
          size="small"
          variant="contained"
        >
          {isRouterDevtoolsOpen ? "Close" : "Open"} TanStack Router Devtools
        </AppButton>
        {!shown ? (
          <AppButton
            color="warning"
            fullWidth
            onClick={forceUpdateNotification}
            size="small"
            variant="contained"
          >
            Show Update Notification
          </AppButton>
        ) : (
          <AppButton
            color="secondary"
            fullWidth
            onClick={clearUpdateNotification}
            size="small"
            variant="contained"
          >
            Hide Update Notification
          </AppButton>
        )}
        <div
          aria-label="Core Web Vitals"
          style={{
            border: `1px solid ${theme.palette.divider}`,
            borderRadius: 6,
            display: "grid",
            gap: 4,
            padding: 8,
          }}
          title="Local page-load metrics. INP needs an interaction; final values may update when the tab is hidden."
        >
          <span style={{ fontSize: 12, fontWeight: 600 }}>Web Vitals</span>
          {WEB_VITAL_NAMES.map((name) => {
            const metric = webVitals.metrics[name];
            const color = metric
              ? metric.rating === "good"
                ? theme.palette.success.main
                : metric.rating === "needs-improvement"
                  ? theme.palette.warning.main
                  : theme.palette.error.main
              : theme.palette.text.secondary;

            return (
              <div
                key={name}
                style={{
                  alignItems: "center",
                  display: "flex",
                  justifyContent: "space-between",
                }}
              >
                <span>{name}</span>
                <span style={{ color }}>
                  {metric ? formatWebVital(name, metric.value) : "Pending"}
                </span>
              </div>
            );
          })}
        </div>
        
      </div>

      {isDevtoolsOpen && (
        <DevtoolsModal onClose={() => setIsDevtoolsOpen(false)}>
          <ReactQueryDevtoolsPanel
            onClose={() => setIsDevtoolsOpen(false)}
            style={{ height: "100%", width: "100%" }}
          />
        </DevtoolsModal>
      )}

      {isRouterDevtoolsOpen && (
        <DevtoolsModal onClose={() => setIsRouterDevtoolsOpen(false)}>
          <TanStackRouterDevtoolsPanel
            isOpen={isRouterDevtoolsOpen}
            setIsOpen={setIsRouterDevtoolsOpen}
            style={{ height: "100%", width: "100%" }}
          />
        </DevtoolsModal>
      )}
    </>
  );
};
