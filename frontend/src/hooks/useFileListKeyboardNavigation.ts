import { useEffect, useCallback } from "react";
import { FileItem } from "@/types/filebrowser";

interface UseFileListKeyboardNavigationProps {
  containerRef: React.RefObject<HTMLDivElement>;
  allItems: FileItem[];
  focusedIndex: number;
  selectedPaths: Set<string>;
  onFocusChange: (index: number) => void;
  onSelectionChange: (paths: Set<string>) => void;
  onDelete?: () => void;
  global?: boolean; // Listen to document events instead of container events
}

export const useFileListKeyboardNavigation = ({
  containerRef,
  allItems,
  focusedIndex,
  onFocusChange,
  onSelectionChange,
  onDelete,
  global = false,
}: UseFileListKeyboardNavigationProps) => {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // Don't intercept keyboard events when user is typing in an input/textarea
      const target = e.target as HTMLElement;
      if (
        target?.tagName === "INPUT" ||
        target?.tagName === "TEXTAREA" ||
        target?.isContentEditable
      ) {
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

      // CTRL+A to select all
      if (e.ctrlKey && e.key === "a") {
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
    },
    [allItems, focusedIndex, onFocusChange, onSelectionChange, onDelete],
  );

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
  }, [handleKeyDown, global]);

  // Scroll focused item into view
  useEffect(() => {
    if (focusedIndex >= 0 && focusedIndex < allItems.length) {
      const container = containerRef.current;
      if (!container) return;

      // Get all file cards in order
      const fileCards = container.querySelectorAll('[data-file-card="true"]');
      const focusedCard = fileCards[focusedIndex] as HTMLElement;

      if (focusedCard) {
        focusedCard.scrollIntoView({
          behavior: "smooth",
          block: "nearest",
          inline: "nearest",
        });
      }
    }
  }, [focusedIndex, allItems.length, containerRef]);
};
