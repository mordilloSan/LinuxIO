import { useVirtualizer } from "@tanstack/react-virtual";
import React, { useLayoutEffect, useMemo, useRef, useState } from "react";

import AppTypography from "@/components/ui/AppTypography";

export interface AppVirtualGridProps<TItem> {
  ariaLabel?: string;
  className?: string;
  emptyMessage?: string;
  estimateItemHeight?: number;
  fillAvailable?: boolean;
  gap?: number;
  getItemKey: (item: TItem, index: number) => React.Key;
  height?: React.CSSProperties["height"];
  items: TItem[];
  maxHeight?: React.CSSProperties["maxHeight"];
  minItemWidth?: number;
  onMouseDown?: React.MouseEventHandler<HTMLDivElement>;
  onMouseDownCapture?: React.MouseEventHandler<HTMLDivElement>;
  overscan?: number;
  overlay?: React.ReactNode;
  padding?: number;
  renderItem: (item: TItem, index: number) => React.ReactNode;
  scrollElementRef?: React.RefObject<HTMLDivElement | null>;
  scrollToIndex?: number | null;
  style?: React.CSSProperties;
}

function AppVirtualGrid<TItem>({
  ariaLabel = "Grid",
  className,
  emptyMessage = "No items available.",
  estimateItemHeight = 88,
  fillAvailable = true,
  gap = 12,
  getItemKey,
  height,
  items,
  maxHeight,
  minItemWidth = 260,
  onMouseDown,
  onMouseDownCapture,
  overscan = 6,
  overlay,
  padding = 4,
  renderItem,
  scrollElementRef,
  scrollToIndex,
  style,
}: AppVirtualGridProps<TItem>) {
  "use no memo";

  const internalScrollRef = useRef<HTMLDivElement>(null);
  const scrollRef = scrollElementRef ?? internalScrollRef;
  const [viewportWidth, setViewportWidth] = useState(0);

  useLayoutEffect(() => {
    const node = scrollRef.current;
    if (!node) return;

    const measure = () => {
      setViewportWidth(node.clientWidth);
    };

    measure();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [scrollRef]);

  const columnCount = useMemo(() => {
    const availableWidth = Math.max(0, viewportWidth - padding * 2);
    return Math.max(
      1,
      Math.floor((availableWidth + gap) / (minItemWidth + gap)),
    );
  }, [gap, minItemWidth, padding, viewportWidth]);

  const rowCount = Math.ceil(items.length / columnCount);
  // TanStack Virtual exposes dynamic helper functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: rowCount,
    estimateSize: () => estimateItemHeight + gap,
    getItemKey: (rowIndex) => {
      const firstItemIndex = rowIndex * columnCount;
      const item = items[firstItemIndex];
      return item ? getItemKey(item, firstItemIndex) : rowIndex;
    },
    getScrollElement: () => scrollRef.current,
    overscan,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualRows = virtualizer.getVirtualItems();

  useLayoutEffect(() => {
    virtualizer.measure();
  }, [columnCount, items.length, virtualizer]);

  useLayoutEffect(() => {
    if (scrollToIndex === null || scrollToIndex === undefined) return;
    if (scrollToIndex < 0 || scrollToIndex >= items.length) return;
    virtualizer.scrollToIndex(Math.floor(scrollToIndex / columnCount), {
      align: "auto",
    });
  }, [columnCount, items.length, scrollToIndex, virtualizer]);

  return (
    <div
      aria-label={ariaLabel}
      className={["custom-scrollbar", className].filter(Boolean).join(" ")}
      onMouseDown={onMouseDown}
      onMouseDownCapture={onMouseDownCapture}
      ref={scrollRef}
      role="grid"
      style={{
        flex: fillAvailable ? "1 1 0" : undefined,
        height: height ?? (fillAvailable ? "100%" : undefined),
        maxHeight,
        minHeight: fillAvailable ? 0 : undefined,
        minWidth: 0,
        overflow: "auto",
        position: "relative",
        ...style,
      }}
    >
      {items.length === 0 ? (
        <div style={{ paddingBlock: 32, textAlign: "center" }}>
          <AppTypography color="text.secondary" variant="body2">
            {emptyMessage}
          </AppTypography>
        </div>
      ) : (
        <div
          role="rowgroup"
          style={{
            height: virtualizer.getTotalSize() + padding * 2,
            minWidth: 0,
            position: "relative",
          }}
        >
          {virtualRows.map((virtualRow) => {
            const rowStartIndex = virtualRow.index * columnCount;
            return (
              <div
                data-index={virtualRow.index}
                key={virtualRow.key}
                ref={virtualizer.measureElement}
                role="row"
                style={{
                  boxSizing: "border-box",
                  display: "grid",
                  gap,
                  gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
                  left: padding,
                  paddingBottom: gap,
                  position: "absolute",
                  right: padding,
                  top: 0,
                  transform: `translateY(${virtualRow.start + padding}px)`,
                }}
              >
                {Array.from({ length: columnCount }, (_, columnIndex) => {
                  const itemIndex = rowStartIndex + columnIndex;
                  const item = items[itemIndex];
                  if (!item) return <div key={`empty-${itemIndex}`} />;

                  return (
                    <div key={getItemKey(item, itemIndex)} role="gridcell">
                      {renderItem(item, itemIndex)}
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      )}
      {overlay}
    </div>
  );
}

export default AppVirtualGrid;
