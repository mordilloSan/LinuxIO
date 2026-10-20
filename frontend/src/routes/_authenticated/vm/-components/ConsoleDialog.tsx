import { Icon } from "@iconify/react";
import RFB from "@novnc/novnc";
import { useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";

import {
  openVMConsoleStream,
  type ResultFrame,
  useStreamMux,
  type VirtualMachine,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import { AppDialogTitle } from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { useStreamMessageChannel } from "@/hooks/useStreamMessageChannel";
import { useAppMediaQuery } from "@/theme";
import { down } from "@/theme/breakpoints";

const consolePaperStyle = (isMobile: boolean): CSSProperties => ({
  height: isMobile ? "calc(100vh - 24px)" : "min(82vh, 820px)",
  maxWidth: isMobile ? "calc(100vw - 16px)" : "min(1200px, calc(100vw - 32px))",
  width: isMobile ? "calc(100vw - 16px)" : "min(1200px, calc(100vw - 32px))",
});

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
  vm,
}: {
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
  vm: VirtualMachine;
}) {
  const isMobile = useAppMediaQuery(down("sm"));
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const { isOpen: muxOpen } = useStreamMux();
  const [status, setStatus] = useState("Connecting");
  const [error, setError] = useState<string | null>(null);
  const { openChannel, closeChannel } = useStreamMessageChannel();

  useEffect(() => {
    if (!open || !muxOpen || !viewportRef.current) return;

    // The effect owns the stream so each run (StrictMode replays it in dev)
    // opens and closes its own instead of reattaching to a closed one.
    const stream = openVMConsoleStream(vm.name);
    if (!stream) return;
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
  }, [open, muxOpen, vm.name, openChannel, closeChannel]);

  const shownError = muxOpen ? error : "Console stream is unavailable.";

  return (
    <GeneralDialog
      fullWidth
      maxWidth={false}
      onClose={onClose}
      open={open}
      paperStyle={consolePaperStyle(isMobile)}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle style={{ justifyContent: "space-between" }}>
        <div>
          {vm.name}
          <AppTypography color="text.secondary" variant="caption">
            {muxOpen ? status : "Unavailable"}
          </AppTypography>
        </div>
        <AppIconButton aria-label="Close console" onClick={onClose}>
          <Icon height={22} icon="mdi:close" width={22} />
        </AppIconButton>
      </AppDialogTitle>
      {shownError && (
        <AppAlert severity="error" style={consoleErrorStyle}>
          {shownError}
        </AppAlert>
      )}
      <div ref={viewportRef} style={consoleViewportStyle} />
    </GeneralDialog>
  );
}
