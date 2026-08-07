import { useEffect, useEffectEvent } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";

interface UseFileBrowserClipboardShortcutsParams {
  editingPath: string | null;
  onCopy: () => void;
  onCut: () => void;
  onPaste: () => void;
  renamingPath: string | null;
}

export const useFileBrowserClipboardShortcuts = ({
  editingPath,
  onCopy,
  onCut,
  onPaste,
  renamingPath,
}: UseFileBrowserClipboardShortcutsParams) => {
  const handleClipboardKeyDown = useEffectEvent((event: KeyboardEvent) => {
    const active = document.activeElement;
    const target = event.target;
    if (
      editingPath ||
      renamingPath ||
      document.querySelector(OVERLAY_ROOT_SELECTOR) ||
      active instanceof HTMLInputElement ||
      active instanceof HTMLTextAreaElement ||
      active instanceof HTMLSelectElement ||
      (active instanceof HTMLElement && active.isContentEditable) ||
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement ||
      target instanceof HTMLSelectElement ||
      (target instanceof HTMLElement && target.isContentEditable)
    ) {
      return;
    }

    // Key is compared lowercased so CapsLock cannot break the match; Shift is
    // excluded to keep combos like Ctrl+Shift+C (browser devtools) inert.
    const isCtrlOrCmd = (event.ctrlKey || event.metaKey) && !event.shiftKey;
    const key = event.key.toLowerCase();
    if (isCtrlOrCmd && key === "c") {
      event.preventDefault();
      onCopy();
    } else if (isCtrlOrCmd && key === "x") {
      event.preventDefault();
      onCut();
    } else if (isCtrlOrCmd && key === "v") {
      event.preventDefault();
      onPaste();
    }
  });

  useEffect(() => {
    document.addEventListener("keydown", handleClipboardKeyDown);
    return () =>
      document.removeEventListener("keydown", handleClipboardKeyDown);
  }, []);
};
