import React, { useCallback, useMemo, useState } from "react";
import "@xterm/xterm/css/xterm.css";

import { linuxio, openContainerStream, useStreamMux } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import TerminalContextMenu from "@/components/terminal/TerminalContextMenu";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import { useLiveStream } from "@/hooks/useLiveStream";
import { useTerminalContextMenu } from "@/hooks/useTerminalContextMenu";
import { useXtermStreamTerminal } from "@/hooks/useXtermStreamTerminal";
import { useAppTheme } from "@/theme";

interface Props {
  containerId: string;
  containerName?: string;
  onClose: () => void;
  open: boolean;
}
const TerminalDialog: React.FC<Props> = ({
  open,
  onClose,
  containerId,
  containerName,
}) => {
  const { streamRef, openStream, closeStream } = useLiveStream();
  const handleClose = useCallback(() => {
    onClose();
  }, [onClose]);
  const [selectedShell, setSelectedShell] = useState<string | null>(null);
  const { isOpen } = useStreamMux();
  const theme = useAppTheme();

  // Fetch available shells when dialog opens
  const {
    data: shells,
    isLoading: loadingShells,
    isFetched: hasFetchedShells,
  } = linuxio.terminal.list_shells.useQuery(containerId, {
    enabled: open && !!containerId,
  });
  const availableShells = useMemo(() => {
    if (!shells) return [];
    return shells.filter((s) => s && typeof s === "string" && s.trim() !== "");
  }, [shells]);
  const activeShell = useMemo(() => {
    if (selectedShell && availableShells.includes(selectedShell)) {
      return selectedShell;
    }
    return availableShells[0] ?? "";
  }, [selectedShell, availableShells]);
  const terminalOptions = useMemo(
    () => ({
      fontFamily: "monospace",
      fontSize: 15,
    }),
    [],
  );
  const handleDialogEntered = useCallback(() => {
    setSelectedShell(null);
  }, []);
  const handleDialogExited = useCallback(() => {
    closeStream();
    setSelectedShell(null);
  }, [closeStream]);

  const {
    containerRef: termRef,
    terminalRef,
    writeData,
  } = useXtermStreamTerminal({
    background: theme.palette.background.default,
    enabled: open && availableShells.length > 0 && !!activeShell && isOpen,
    focusDelayMs: 200,
    foreground: theme.palette.text.primary,
    onKeyDown: (event) => {
      if (event.key === "Escape") {
        handleClose();
        return false;
      }
    },
    onReady: (terminal) => {
      const opened = openStream({
        open: () =>
          openContainerStream(
            containerId,
            activeShell,
            terminal.cols,
            terminal.rows,
          ),
        onData: writeData,
      });
      if (opened && streamRef.current) {
        streamRef.current.resize(terminal.cols, terminal.rows);
      }

      return () => {
        closeStream();
      };
    },
    readyMode: "timeout",
    sessionKey: `${containerId}:${activeShell}`,
    streamRef,
    terminalOptions,
  });

  const {
    contextMenu,
    handleCloseContextMenu,
    handleContextMenu,
    handleCopy,
    handlePaste,
  } = useTerminalContextMenu({
    streamRef,
    terminalRef,
  });

  // Shell picker handler
  const handleShellChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newShell = e.target.value;
    closeStream();
    setSelectedShell(newShell);
  };

  // Dialog close handler
  const handleDialogClose = () => {
    closeStream();
    onClose();
  };
  return (
    <GeneralDialog
      fullWidth
      maxWidth="md"
      onClose={handleDialogClose}
      open={open}
      slotProps={{
        transition: {
          onEntered: handleDialogEntered,
          onExited: handleDialogExited,
        },
      }}
    >
      <AppDialogTitle>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: theme.spacing(2),
          }}
        >
          <span>
            {containerName ? `Shell for ${containerName}` : "Container Shell"}
          </span>
          <AppSelect
            disabled={!isOpen || availableShells.length === 0}
            onChange={handleShellChange}
            size="small"
            style={{ minWidth: 80 }}
            value={activeShell}
            variant="standard"
          >
            {availableShells.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </AppSelect>
        </div>
      </AppDialogTitle>
      <AppDialogContent
        style={{
          minHeight: 350,
          maxHeight: 600,
          fontFamily: "Fira Mono, monospace",
          padding: 0,
          background: theme.palette.background.default,
          borderTop: `1px solid ${theme.palette.divider}`,
        }}
      >
        {loadingShells ? (
          <div
            style={{
              padding: theme.spacing(3),
              textAlign: "center",
            }}
          >
            <ComponentLoader />
          </div>
        ) : hasFetchedShells && availableShells.length === 0 ? (
          <div
            style={{
              padding: theme.spacing(3),
              color: theme.palette.error.main,
              textAlign: "center",
            }}
          >
            No shell available in this container.
            <br />
            (Try installing <b>bash</b> or <b>sh</b> in your container.)
          </div>
        ) : availableShells.length > 0 ? (
          <div
            onContextMenu={handleContextMenu}
            ref={termRef}
            style={{
              width: "100%",
              minHeight: 350,
              height: 420,
              background: theme.palette.background.default,
            }}
            tabIndex={0}
          />
        ) : null}
        <TerminalContextMenu
          contextMenu={contextMenu}
          onClose={handleCloseContextMenu}
          onCopy={handleCopy}
          onPaste={handlePaste}
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleDialogClose}>Close</AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
export default TerminalDialog;
