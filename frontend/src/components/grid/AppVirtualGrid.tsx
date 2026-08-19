import { useVirtualizer } from "@tanstack/react-virtual";
import {
  memo,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  type CSSProperties,
  type Key,
  type MouseEventHandler,
  type ReactNode,
  type RefObject,
} from "react";

import AppTypography from "@/components/ui/AppTypography";
import { useGridColumnCount } from "@/hooks/useGridColumnCount";
import { HOVER_LIFT_HEADROOM } from "@/theme/constants";

export interface AppVirtualGridProps<TItem> {
  ariaLabel?: string;
  className?: string;
  emptyMessage?: string;
  estimateItemHeight?: number;
  fillAvailable?: boolean;
  gap?: number;
  getItemKey: (item: TItem, index: number) => Key;
  height?: CSSProperties["height"];
  items: TItem[];
  maxHeight?: CSSProperties["maxHeight"];
  minItemWidth?: number;
  onMouseDown?: MouseEventHandler<HTMLDivElement>;
  onMouseDownCapture?: MouseEventHandler<HTMLDivElement>;
  overscan?: number;
  overlay?: ReactNode;
  padding?: number;
  renderItem: (item: TItem, index: number) => ReactNode;
  scrollElementRef?: RefObject<HTMLDivElement | null>;
  scrollToIndex?: number | null;
  style?: CSSProperties;
}

interface AppVirtualGridItemProps<TItem> {
  index: number;
  item: TItem;
  renderItem: (item: TItem, index: number) => ReactNode;
}

function AppVirtualGridItem<TItem>({
  index,
  item,
  renderItem,
}: AppVirtualGridItemProps<TItem>) {
  return <div role="gridcell">{renderItem(item, index)}</div>;
}

// TanStack Virtual updates the grid for scroll and measurement changes. Keep
// those updates at the row-positioning layer when an item's inputs are stable.
const MemoizedAppVirtualGridItem = memo(
  AppVirtualGridItem,
) as typeof AppVirtualGridItem;

// React Compiler skips this component: TanStack Virtual's API returns
// unstable functions it cannot memoize. Manual memoization stays load-bearing.
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
  const columnCount = useGridColumnCount(scrollRef, {
    gap,
    minItemWidth,
    padding,
  });

  const rowCount = Math.ceil(items.length / columnCount);
  const estimateRowSize = useCallback(
    () => estimateItemHeight + gap,
    [estimateItemHeight, gap],
  );
  const virtualMeasurementInputs = useMemo(
    () => ({ columnCount, estimateRowSize, getItemKey, items }),
    [columnCount, estimateRowSize, getItemKey, items],
  );
  const getVirtualRowKey = useCallback(
    (rowIndex: number) => {
      const firstItemIndex = rowIndex * virtualMeasurementInputs.columnCount;
      const item = virtualMeasurementInputs.items[firstItemIndex];
      return item
        ? virtualMeasurementInputs.getItemKey(item, firstItemIndex)
        : rowIndex;
    },
    // TanStack does not track estimateSize as a measurement dependency. A new
    // key callback shares the estimate callback's input snapshot, recalculating
    // unmeasured offsets while keeping actual keyed row measurements.
    [virtualMeasurementInputs],
  );
  // TanStack Virtual exposes dynamic helper functions that React Compiler cannot memoize safely.
  // eslint-disable-next-line react-hooks/incompatible-library
  // oxlint-disable-next-line react/incompatible-library
  const virtualizer = useVirtualizer({
    count: rowCount,
    // The virtualizer owns the row wrappers' transform and the rowgroup
    // height — scroll and remeasure updates are written straight to the DOM
    // instead of re-rendering. The outer padding rides along as
    // paddingStart/paddingEnd so row starts already include it.
    directDomUpdates: true,
    estimateSize: estimateRowSize,
    getItemKey: getVirtualRowKey,
    getScrollElement: () => scrollRef.current,
    overscan,
    paddingEnd: padding,
    // This scrollport is `overflow: auto`, so it clips a hover-lifted card in
    // the first row against its own top edge. Reserve the lift headroom inside
    // the scroll area, where the card has somewhere to rise into; the negative
    // margin below gives the space back, so nothing moves on screen.
    paddingStart: padding + HOVER_LIFT_HEADROOM,
    useAnimationFrameWithResizeObserver: true,
  });
  const virtualRows = virtualizer.getVirtualItems();

  useLayoutEffect(() => {
    // Count/key changes already rebuild offsets while preserving keyed sizes.
    // Column membership, the inter-row gap, and outer padding change row
    // geometry, so those layout changes still require clearing measured heights.
    virtualizer.measure();
  }, [columnCount, gap, padding, virtualizer]);

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
        // Cancels the paddingStart reserved above, so the first row lands where
        // it always did and the grid only gains headroom the lift can use. The
        // space pulled into is the gap the tab strip already leaves below
        // itself (--tab-strip-headroom); both are HOVER_LIFT_HEADROOM.
        marginTop: -HOVER_LIFT_HEADROOM,
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
          ref={virtualizer.containerRef}
          role="rowgroup"
          style={{
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
                }}
              >
                {Array.from({ length: columnCount }, (_, columnIndex) => {
                  const itemIndex = rowStartIndex + columnIndex;
                  const item = items[itemIndex];
                  if (!item) return <div key={`empty-${itemIndex}`} />;

                  return (
                    <MemoizedAppVirtualGridItem
                      index={itemIndex}
                      item={item}
                      key={getItemKey(item, itemIndex)}
                      renderItem={renderItem}
                    />
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
