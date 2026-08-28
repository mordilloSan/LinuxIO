import { Icon } from "@iconify/react";
import RFB from "@novnc/novnc";
import { useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";

import type { ResultFrame } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useStreamMessageChannel } from "@/hooks/useStreamMessageChannel";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";

import type { ConsoleSession } from "./vmShared";

const consolePaperStyle = (isMobile: boolean): CSSProperties => ({
  height: isMobile ? "calc(100vh - 24px)" : "min(82vh, 820px)",
  maxWidth: isMobile ? "calc(100vw - 16px)" : "min(1200px, calc(100vw - 32px))",
  width: isMobile ? "calc(100vw - 16px)" : "min(1200px, calc(100vw - 32px))",
});

const consoleHeaderStyle: CSSProperties = {
  alignItems: "center",
  borderBottom: "1px solid var(--app-palette-divider)",
  display: "flex",
  gap: "var(--app-space-16)",
  justifyContent: "space-between",
  padding: "var(--app-space-16) var(--app-space-16)",
};

const consoleErrorStyle: CSSProperties = {
  margin: "var(--app-space-12) var(--app-space-16) 0",
};

const consoleViewportStyle: CSSProperties = {
  background: "black",
  height: "calc(100% - 70px)",
  minHeight: 360,
  outline: "none",
  overflow: "hidden",
};

export default function ConsoleDialog({
  onClose,
  onExited,
  open,
  session,
}: {
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
  session: ConsoleSession;
}) {
  const isMobile = useAppMediaQuery(down("sm"));
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const { stream, vm } = session;
  const [status, setStatus] = useState(stream ? "Connecting" : "Unavailable");
  const [error, setError] = useState<string | null>(
    stream ? null : "Console stream is unavailable.",
  );
  const { openChannel, closeChannel } = useStreamMessageChannel();

  useEffect(() => {
    if (!open || !stream || !viewportRef.current) return;

    const viewport = viewportRef.current;
    viewport.innerHTML = "";
    const handleStreamResult = (result: ResultFrame) => {
      if (result.status !== "error") {
        return;
      }
      setStatus("Unavailable");
      setError(result.error || "Console failed to open.");
    };

    const channel = openChannel(stream, {
      onResult: handleStreamResult,
    });
    const rfb = new RFB(viewport, channel, {
      focusOnClick: true,
      shared: true,
    });

    rfb.scaleViewport = true;
    rfb.resizeSession = true;
    rfb.focusOnClick = true;

    const handleConnect = () => {
      setStatus("Connected");
      setError(null);
    };
    const handleDisconnect = () =>
      setStatus((current) =>
        current === "Unavailable" ? current : "Disconnected",
      );
    const handleCredentials = () => {
      setStatus("Authentication required");
      setError("The VNC server requested credentials.");
    };
    const handleSecurityFailure = () => {
      setStatus("Security failure");
      setError("VNC security negotiation failed.");
    };

    rfb.addEventListener("connect", handleConnect);
    rfb.addEventListener("disconnect", handleDisconnect);
    rfb.addEventListener("credentialsrequired", handleCredentials);
    rfb.addEventListener("securityfailure", handleSecurityFailure);

    return () => {
      rfb.removeEventListener("connect", handleConnect);
      rfb.removeEventListener("disconnect", handleDisconnect);
      rfb.removeEventListener("credentialsrequired", handleCredentials);
      rfb.removeEventListener("securityfailure", handleSecurityFailure);
      rfb.disconnect();
      closeChannel();
      viewport.innerHTML = "";
    };
  }, [open, stream, openChannel, closeChannel]);

  return (
    <GeneralDialog
      fullWidth
      maxWidth={false}
      onClose={onClose}
      open={open}
      paperStyle={consolePaperStyle(isMobile)}
      slotProps={{ transition: { onExited } }}
    >
      <div style={consoleHeaderStyle}>
        <div>
          <AppTypography component="h2" variant="h6">
            {vm.name}
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            {status}
          </AppTypography>
        </div>
        <AppIconButton aria-label="Close console" onClick={onClose}>
          <Icon height={22} icon="mdi:close" width={22} />
        </AppIconButton>
      </div>
      {error && (
        <AppAlert severity="error" style={consoleErrorStyle}>
          {error}
        </AppAlert>
      )}
      <div ref={viewportRef} style={consoleViewportStyle} />
    </GeneralDialog>
  );
}
