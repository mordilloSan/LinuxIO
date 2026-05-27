import { type Terminal } from "@xterm/xterm";
import {
  type MouseEvent,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { encodeString, type Stream } from "@/api";

export interface TerminalContextMenuPosition {
  mouseX: number;
  mouseY: number;
}

export interface UseTerminalContextMenuOptions {
  streamRef: RefObject<Stream | null>;
  terminalRef: RefObject<Terminal | null>;
}

export interface UseTerminalContextMenuResult {
  contextMenu: TerminalContextMenuPosition | null;
  handleCloseContextMenu: () => void;
  handleContextMenu: (event: MouseEvent) => void;
  handleCopy: () => void;
  handlePaste: () => Promise<void>;
}

export function useTerminalContextMenu({
  streamRef,
  terminalRef,
}: UseTerminalContextMenuOptions): UseTerminalContextMenuResult {
  const [contextMenu, setContextMenu] =
    useState<TerminalContextMenuPosition | null>(null);
  const openTimerRef = useRef<number | null>(null);

  const clearOpenTimer = useCallback(() => {
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
  }, []);

  const handleCloseContextMenu = useCallback(() => {
    clearOpenTimer();
    setContextMenu(null);
  }, [clearOpenTimer]);

  const handleContextMenu = useCallback(
    (event: MouseEvent) => {
      event.preventDefault();
      event.stopPropagation();

      const wasOpen = contextMenu !== null;
      const mouseX = event.clientX;
      const mouseY = event.clientY;

      clearOpenTimer();
      setContextMenu(null);

      if (!wasOpen) {
        openTimerRef.current = window.setTimeout(() => {
          setContextMenu({ mouseX, mouseY });
          openTimerRef.current = null;
        }, 0);
      }
    },
    [clearOpenTimer, contextMenu],
  );

  const handleCopy = useCallback(() => {
    const selection = terminalRef.current?.getSelection();
    if (selection) {
      void navigator.clipboard.writeText(selection);
    }
    handleCloseContextMenu();
  }, [handleCloseContextMenu, terminalRef]);

  const handlePaste = useCallback(async () => {
    try {
      const text = await navigator.clipboard.readText();
      streamRef.current?.write(encodeString(text));
    } catch {
      // Clipboard read denied or unavailable; ignore.
    }
    handleCloseContextMenu();
  }, [handleCloseContextMenu, streamRef]);

  useEffect(() => {
    const handleBlur = () => {
      handleCloseContextMenu();
    };
    window.addEventListener("blur", handleBlur);
    return () => {
      window.removeEventListener("blur", handleBlur);
    };
  }, [handleCloseContextMenu]);

  useEffect(() => clearOpenTimer, [clearOpenTimer]);

  return {
    contextMenu,
    handleCloseContextMenu,
    handleContextMenu,
    handleCopy,
    handlePaste,
  };
}
