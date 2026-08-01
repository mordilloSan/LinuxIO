import { Icon } from "@iconify/react";
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useState } from "react";

import { DevtoolsModal } from "@/components/dev-tools/DevtoolsModal";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface DevToolsPanelProps {
  isOpen: boolean;
  isWebVitalsVisible: boolean;
  onClose: () => void;
  onToggleWebVitals: () => void;
}

/**
 * Dev-only tool panel for testing and debugging.
 * Only rendered when import.meta.env.DEV is true.
 */
export const DevToolsPanel = ({
  isOpen,
  isWebVitalsVisible,
  onClose,
  onToggleWebVitals,
}: DevToolsPanelProps) => {
  const theme = useAppTheme();
  // Check if update notification is currently shown
  const shown = !!sessionStorage.getItem("dev_update_forced");
  const [isDevtoolsOpen, setIsDevtoolsOpen] = useState(false);
  const [isRouterDevtoolsOpen, setIsRouterDevtoolsOpen] = useState(false);

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
          <AppIconButton
            aria-label="Close developer tools"
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
            <Icon height={18} icon="mdi:close" width={18} />
          </AppIconButton>
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
        <AppButton
          color="primary"
          fullWidth
          onClick={onToggleWebVitals}
          size="small"
          variant="contained"
        >
          {isWebVitalsVisible ? "Hide" : "Show"} Web Vitals in Footer
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
