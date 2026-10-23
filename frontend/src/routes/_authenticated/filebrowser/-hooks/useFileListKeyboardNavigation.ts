import { useEffect, useEffectEvent, useRef, type RefObject } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/overlay";

import type { FileItem } from "../-types";

interface UseFileListKeyboardNavigationProps {
  allItems: FileItem[];
  /** Index selection ranges extend from; the last position reached without Shift. */
  anchorIndexRef: RefObject<number>;
  /** Items per visual row; read at key time, 1 (list view) when omitted. */
  columnCountRef?: RefObject<number>;
  containerRef: RefObject<HTMLDivElement>;
  focusedIndex: number;
  global?: boolean; // Listen to document events instead of container events
  onDelete?: () => void;
  onDeletePermanently?: () => void;
  onDownloadFile?: (item: FileItem) => void;
  onFocusChange: (index: number) => void;
  onNavigateUp?: () => void;
  onOpenDirectory?: (path: string) => void;
  onRename?: () => void;
  onSelectionChange: (paths: Set<string>) => void;
  selectedPaths: Set<string>;
}

const TYPEAHEAD_RESET_MS = 1000;

// Index a movement key leads to, or null when the key is not one (or does
// nothing in this layout). `from` is -1 while nothing is focused.
const getMoveTarget = (
  key: string,
  from: number,
  count: number,
  columns: number,
): number | null => {
  const last = count - 1;
  switch (key) {
    case "Home":
      return 0;
    case "End":
      return last;
    case "ArrowDown":
      return from < 0 ? 0 : Math.min(from + columns, last);
    case "ArrowUp":
      return from < 0 ? last : Math.max(from - columns, 0);
    case "ArrowRight":
      return columns > 1 ? Math.min(from + 1, last) : null;
    case "ArrowLeft":
      return columns > 1 ? (from < 0 ? last : Math.max(from - 1, 0)) : null;
    default:
      return null;
  }
};

export const useFileListKeyboardNavigation = ({
  containerRef,
  allItems,
  anchorIndexRef,
  columnCountRef,
  focusedIndex,
  onFocusChange,
  onSelectionChange,
  onDelete,
  onDeletePermanently,
  onDownloadFile,
  onNavigateUp,
  onOpenDirectory,
  onRename,
  selectedPaths,
  global = false,
}: UseFileListKeyboardNavigationProps) => {
  const typeaheadRef = useRef({ prefix: "", time: 0 });

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

    // Delete moves the selection to the trash; Shift+Delete removes it for good
    if (e.key === "Delete") {
      e.preventDefault();
      if (e.shiftKey) {
        onDeletePermanently?.();
      } else {
        onDelete?.();
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

    // Alt+Up and Backspace go to the parent folder
    if (
      (e.key === "ArrowUp" && e.altKey && !e.ctrlKey && !e.metaKey) ||
      (e.key === "Backspace" && !e.altKey && !e.ctrlKey && !e.metaKey)
    ) {
      e.preventDefault();
      onNavigateUp?.();
      return;
    }

    if (e.ctrlKey || e.altKey || e.metaKey) {
      return;
    }

    // The listing always has a focused index, but with nothing selected there
    // is no visible cursor: movement and typeahead start from the edge.
    const current = selectedPaths.size > 0 ? focusedIndex : -1;

    const moveTarget =
      allItems.length > 0
        ? getMoveTarget(
            e.key,
            current,
            allItems.length,
            columnCountRef?.current ?? 1,
          )
        : null;
    if (moveTarget !== null) {
      e.preventDefault();
      const anchor =
        e.shiftKey && current >= 0
          ? Math.min(Math.max(anchorIndexRef.current, 0), allItems.length - 1)
          : moveTarget;
      if (!e.shiftKey || current < 0) {
        anchorIndexRef.current = moveTarget;
      }
      const from = Math.min(anchor, moveTarget);
      const to = Math.max(anchor, moveTarget);
      onFocusChange(moveTarget);
      onSelectionChange(
        new Set(allItems.slice(from, to + 1).map((item) => item.path)),
      );
      return;
    }

    // Enter opens the focused folder, or does what double-click does on a file
    if (
      e.key === "Enter" &&
      !e.shiftKey &&
      // A focused control owns Enter (a breadcrumb or toolbar button).
      !target?.closest?.("button, a[href], [role='button'], [role='menuitem']")
    ) {
      const item = allItems[current];
      if (!item) return;
      e.preventDefault();
      if (item.type === "directory") {
        onOpenDirectory?.(item.path);
      } else {
        onDownloadFile?.(item);
      }
      return;
    }

    // Typeahead: characters typed within a second form a prefix to jump to
    if (/^[a-z0-9]$/i.test(e.key)) {
      e.preventDefault();
      const now = Date.now();
      const typeahead = typeaheadRef.current;
      const prefix =
        (now - typeahead.time < TYPEAHEAD_RESET_MS ? typeahead.prefix : "") +
        e.key.toLowerCase();
      typeaheadRef.current = { prefix, time: now };

      // A lone character steps past the focused item so repeating it cycles
      // through matches; a longer prefix may keep the focused one.
      const start = prefix.length === 1 ? current + 1 : Math.max(current, 0);
      for (let offset = 0; offset < allItems.length; offset++) {
        const index = (start + offset) % allItems.length;
        if (allItems[index].name.toLowerCase().startsWith(prefix)) {
          anchorIndexRef.current = index;
          onFocusChange(index);
          onSelectionChange(new Set([allItems[index].path]));
          return;
        }
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

  // Revealing the focused item is the virtualizer's task (VirtualDirectoryItems
  // scrolls by row index). Doing it here by indexing the rendered cards would
  // resolve to the wrong element, since only the virtual window is in the DOM.
};
