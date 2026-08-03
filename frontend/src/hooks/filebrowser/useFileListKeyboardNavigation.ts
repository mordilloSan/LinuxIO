import { useEffect, useEffectEvent, type RefObject } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";
import { FileItem } from "@/types/filebrowser";

interface UseFileListKeyboardNavigationProps {
  allItems: FileItem[];
  containerRef: RefObject<HTMLDivElement>;
  focusedIndex: number;
  global?: boolean; // Listen to document events instead of container events
  onDelete?: () => void;
  onFocusChange: (index: number) => void;
  onRename?: () => void;
  onSelectionChange: (paths: Set<string>) => void;
  selectedPaths: Set<string>;
}

export const useFileListKeyboardNavigation = ({
  containerRef,
  allItems,
  focusedIndex,
  onFocusChange,
  onSelectionChange,
  onDelete,
  onRename,
  global = false,
}: UseFileListKeyboardNavigationProps) => {
  const handleKeyDown = useEffectEvent((e: KeyboardEvent) => {
    // Don't intercept keyboard events when user is typing in an input/textarea
    const target = e.target as HTMLElement;
    if (
      target?.tagName === "INPUT" ||
      target?.tagName === "TEXTAREA" ||
      target?.isContentEditable
    ) {
      return;
    }

    // Dialogs autofocus their action button, which passes the guard above;
    // handling Escape here would preventDefault before the overlay's own
    // handler sees it, clearing the selection while the dialog stays open.
    if (document.querySelector(OVERLAY_ROOT_SELECTOR)) {
      return;
    }

    if (e.key === "Escape") {
      e.preventDefault();
      onSelectionChange(new Set());
      onFocusChange(-1);
      return;
    }

    // Delete key to delete selected items
    if (e.key === "Delete") {
      e.preventDefault();
      if (onDelete) {
        onDelete();
      }
      return;
    }

    // F2 key to rename selected item
    if (e.key === "F2") {
      e.preventDefault();
      if (onRename) {
        onRename();
      }
      return;
    }

    // CTRL+A to select all (key is compared lowercased so CapsLock cannot
    // break the match; Shift is excluded to keep Ctrl+Shift+A inert)
    if (e.ctrlKey && !e.shiftKey && e.key.toLowerCase() === "a") {
      e.preventDefault();
      const allPaths = new Set(allItems.map((item) => item.path));
      onSelectionChange(allPaths);
      return;
    }

    // Letter key navigation
    if (
      e.key.length === 1 &&
      e.key.match(/[a-z]/i) &&
      !e.ctrlKey &&
      !e.altKey &&
      !e.metaKey
    ) {
      e.preventDefault();
      const letter = e.key.toLowerCase();

      // Find next item starting with this letter
      const currentIndex = focusedIndex;
      let foundIndex = -1;

      // Search from current position forward
      for (let i = currentIndex + 1; i < allItems.length; i++) {
        if (allItems[i].name.toLowerCase().startsWith(letter)) {
          foundIndex = i;
          break;
        }
      }

      // If not found, wrap around and search from beginning
      if (foundIndex === -1) {
        for (let i = 0; i <= currentIndex; i++) {
          if (allItems[i].name.toLowerCase().startsWith(letter)) {
            foundIndex = i;
            break;
          }
        }
      }

      if (foundIndex !== -1) {
        onFocusChange(foundIndex);
        const item = allItems[foundIndex];
        onSelectionChange(new Set([item.path]));
      }
    }
  });

  useEffect(() => {
    if (global) {
      // Listen to document-level events for global keyboard navigation
      document.addEventListener("keydown", handleKeyDown);
      return () => {
        document.removeEventListener("keydown", handleKeyDown);
      };
    } else {
      // Listen to container-level events for local keyboard navigation
      const container = containerRef.current;
      if (container) {
        container.addEventListener("keydown", handleKeyDown);
        container.setAttribute("tabIndex", "0");
      }

      return () => {
        if (container) {
          container.removeEventListener("keydown", handleKeyDown);
        }
      };
    }
  }, [global, containerRef]);

  // Revealing the focused item is the virtualizer's job (VirtualDirectoryItems
  // scrolls by row index). Doing it here by indexing the rendered cards would
  // resolve to the wrong element, since only the virtual window is in the DOM.
};
