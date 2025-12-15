import { useState, useCallback, useRef, useEffect } from "react";
import { FileItem } from "@/types/filebrowser";

interface MarqueeBox {
  startX: number;
  startY: number;
  currentX: number;
  currentY: number;
}

interface MarqueeSelectionResult {
  isSelecting: boolean;
  selectionBox: {
    left: number;
    top: number;
    width: number;
    height: number;
  } | null;
  handleMouseDown: (event: React.MouseEvent) => void;
}

/**
 * Custom hook for marquee selection (AutoCAD-style selection box)
 * Allows click-and-drag selection of items
 */
export const useMarqueeSelection = (
  containerRef: React.RefObject<HTMLElement | null>,
  allItems: FileItem[],
  onSelectionChange: (paths: Set<string>) => void,
): MarqueeSelectionResult => {
  const [marqueeBox, setMarqueeBox] = useState<MarqueeBox | null>(null);
  const isSelectingRef = useRef(false);

  const handleMouseDown = useCallback(
    (event: React.MouseEvent) => {
      // Only start marquee selection on left mouse button
      if (event.button !== 0) return;

      // Don't start marquee selection if clicking on a file/folder card
      const target = event.target as HTMLElement;
      if (target.closest("[data-file-card='true']")) {
        return;
      }

      // Get container bounds for coordinate calculation
      const container = containerRef.current;
      if (!container) return;

      const rect = container.getBoundingClientRect();

      // Clamp starting coordinates to container boundaries
      const scrollAwareX = event.clientX - rect.left + container.scrollLeft;
      const scrollAwareY = event.clientY - rect.top + container.scrollTop;

      const maxX = container.scrollWidth;
      const maxY = container.scrollHeight;

      const startX = Math.max(0, Math.min(scrollAwareX, maxX));
      const startY = Math.max(0, Math.min(scrollAwareY, maxY));

      isSelectingRef.current = true;
      setMarqueeBox({
        startX,
        startY,
        currentX: startX,
        currentY: startY,
      });

      // Prevent text selection during drag
      event.preventDefault();
    },
    [containerRef],
  );

  // Helper function to calculate intersecting items
  const calculateSelectedItems = useCallback(
    (box: MarqueeBox) => {
      const container = containerRef.current;
      if (!container) return new Set<string>();

      const left = Math.min(box.startX, box.currentX);
      const right = Math.max(box.startX, box.currentX);
      const top = Math.min(box.startY, box.currentY);
      const bottom = Math.max(box.startY, box.currentY);

      const selectedPaths = new Set<string>();
      const cardElements = container.querySelectorAll(
        "[data-file-card='true']",
      );

      cardElements.forEach((element) => {
        const cardRect = element.getBoundingClientRect();
        const containerRect = container.getBoundingClientRect();

        // Convert card position to container-relative coordinates
        const cardLeft =
          cardRect.left - containerRect.left + container.scrollLeft;
        const cardRight =
          cardRect.right - containerRect.left + container.scrollLeft;
        const cardTop = cardRect.top - containerRect.top + container.scrollTop;
        const cardBottom =
          cardRect.bottom - containerRect.top + container.scrollTop;

        // Check if the card intersects with the selection box
        const intersects =
          cardLeft < right &&
          cardRight > left &&
          cardTop < bottom &&
          cardBottom > top;

        if (intersects) {
          const pathAttr = element.getAttribute("data-file-path");
          if (pathAttr) {
            selectedPaths.add(pathAttr);
          }
        }
      });

      return selectedPaths;
    },
    [containerRef],
  );

  useEffect(() => {
    if (!marqueeBox) return;

    const container = containerRef.current;
    if (!container) return;

    const handleMouseMove = (event: MouseEvent) => {
      if (!isSelectingRef.current || !marqueeBox) return;

      const rect = container.getBoundingClientRect();

      // Clamp coordinates to container boundaries
      const scrollAwareX = event.clientX - rect.left + container.scrollLeft;
      const scrollAwareY = event.clientY - rect.top + container.scrollTop;

      const maxX = container.scrollWidth;
      const maxY = container.scrollHeight;

      const currentX = Math.max(0, Math.min(scrollAwareX, maxX));
      const currentY = Math.max(0, Math.min(scrollAwareY, maxY));

      const newBox = {
        ...marqueeBox,
        currentX,
        currentY,
      };

      setMarqueeBox(newBox);

      // Update selection dynamically during drag
      const selectedPaths = calculateSelectedItems(newBox);
      onSelectionChange(selectedPaths);
    };

    const handleMouseUp = () => {
      if (!isSelectingRef.current) return;

      // Selection has already been updated dynamically during drag
      // Just reset the marquee state
      isSelectingRef.current = false;
      setMarqueeBox(null);
    };

    // Add event listeners to document for global tracking
    document.addEventListener("mousemove", handleMouseMove);
    document.addEventListener("mouseup", handleMouseUp);

    return () => {
      document.removeEventListener("mousemove", handleMouseMove);
      document.removeEventListener("mouseup", handleMouseUp);
    };
  }, [marqueeBox, containerRef, calculateSelectedItems, onSelectionChange]);

  // Calculate visual selection box dimensions
  const selectionBox = marqueeBox
    ? {
        left: Math.min(marqueeBox.startX, marqueeBox.currentX),
        top: Math.min(marqueeBox.startY, marqueeBox.currentY),
        width: Math.abs(marqueeBox.currentX - marqueeBox.startX),
        height: Math.abs(marqueeBox.currentY - marqueeBox.startY),
      }
    : null;

  return {
    isSelecting: isSelectingRef.current && marqueeBox !== null,
    selectionBox,
    handleMouseDown,
  };
};
