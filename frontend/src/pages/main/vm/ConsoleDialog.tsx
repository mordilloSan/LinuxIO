import { Icon } from "@iconify/react";
import RFB from "@novnc/novnc";
import { useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";

import type { ConsoleSession } from "./vmShared";

import { createStreamMessageChannel, type ResultFrame } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { type AppTheme, useAppMediaQuery, useAppTheme } from "@/theme";

const consolePaperStyle = (isMobile: boolean): CSSProperties => ({
  height: isMobile ? "calc(100vh - 24px)" : "min(82vh, 820px)",
  maxWidth: isMobile ? "calc(100vw - 16px)" : "min(1200px, calc(100vw - 32px))",
  width: isMobile ? "calc(100vw - 16px)" : "min(1200px, calc(100vw - 32px))",
});

const consoleHeaderStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  borderBottom: `1px solid ${theme.palette.divider}`,
  display: "flex",
  gap: theme.spacing(4),
  justifyContent: "space-between",
  padding: theme.spacing(3.5, 4),
});

const consoleErrorStyle = (theme: AppTheme): CSSProperties => ({
  margin: theme.spacing(3, 4, 0),
});

const consoleViewportStyle = (theme: AppTheme): CSSProperties => ({
  background: theme.palette.common.black,
  height: "calc(100% - 70px)",
  minHeight: 360,
  outline: "none",
  overflow: "hidden",
});

export default function ConsoleDialog({
  onClose,
  open,
  session,
}: {
  onClose: () => void;
  open: boolean;
  session: ConsoleSession;
}) {
  const theme = useAppTheme();
  const isMobile = useAppMediaQuery(theme.breakpoints.down("sm"));
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const { stream, vm } = session;
  const [status, setStatus] = useState(stream ? "Connecting" : "Unavailable");
  const [error, setError] = useState<string | null>(
    stream ? null : "Console stream is unavailable.",
  );

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

    const channel = createStreamMessageChannel(stream, {
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
      channel.close();
      viewport.innerHTML = "";
    };
  }, [open, stream]);

  return (
    <GeneralDialog
      fullWidth
      maxWidth={false}
      onClose={onClose}
      open={open}
      paperStyle={consolePaperStyle(isMobile)}
    >
      <div style={consoleHeaderStyle(theme)}>
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
        <AppAlert severity="error" style={consoleErrorStyle(theme)}>
          {error}
        </AppAlert>
      )}
      <div ref={viewportRef} style={consoleViewportStyle(theme)} />
    </GeneralDialog>
  );
}
