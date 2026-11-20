import { useLayoutEffect, useState, type RefObject } from "react";

interface GridColumnCountOptions {
  /** Horizontal gap between columns, in px. */
  gap: number;
  /** Minimum width one column may shrink to, in px. */
  minItemWidth: number;
  /** Horizontal padding inside the container, applied on both sides, in px. */
  padding: number;
}

/**
 * How many `minItemWidth`-wide columns fit a container `width` wide, CSS
 * `auto-fill` style; at least one.
 */
export const gridColumnCount = (
  width: number,
  { gap, minItemWidth, padding }: GridColumnCountOptions,
) => {
  const availableWidth = Math.max(0, width - padding * 2);
  return Math.max(1, Math.floor((availableWidth + gap) / (minItemWidth + gap)));
};

/**
 * Tracks a scroll container's client width and derives how many
 * `minItemWidth`-wide columns fit (`gridColumnCount`). Shared by the
 * virtualized card grids (AppVirtualGrid, VirtualDirectoryItems), which lay
 * items out in virtual rows of `columnCount` cells.
 */
export function useGridColumnCount(
  containerRef: RefObject<HTMLElement | null>,
  { gap, minItemWidth, padding }: GridColumnCountOptions,
) {
  // The element does not exist during render, so zero means "not measured".
  // The layout effect replaces it with clientWidth before the browser paints.
  const [viewportWidth, setViewportWidth] = useState(0);

  useLayoutEffect(() => {
    const node = containerRef.current;
    if (!node) return;

    const measure = () => {
      setViewportWidth(node.clientWidth);
    };

    measure();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [containerRef]);

  return gridColumnCount(viewportWidth, { gap, minItemWidth, padding });
}
