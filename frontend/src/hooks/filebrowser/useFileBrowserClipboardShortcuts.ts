import { useEffect, useEffectEvent } from "react";

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
      document.querySelector(".app-dialog-root") ||
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

    const isCtrlOrCmd = event.ctrlKey || event.metaKey;
    if (isCtrlOrCmd && event.key === "c") {
      event.preventDefault();
      onCopy();
    } else if (isCtrlOrCmd && event.key === "x") {
      event.preventDefault();
      onCut();
    } else if (isCtrlOrCmd && event.key === "v") {
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
