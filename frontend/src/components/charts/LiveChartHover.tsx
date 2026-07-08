import React, {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

import {
  getLiveHoverNowMs,
  getLiveHoverRightPx,
  LIVE_MILLIS_PER_PIXEL,
  setLiveHoverRightPx,
  subscribeLiveHover,
} from "@/components/charts/liveSeriesStore";
import type { LiveTooltipRow } from "@/components/charts/liveTooltip";
import { formatChartClockWithSeconds } from "@/components/charts/timeFormat";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

import "@/components/charts/chart-tooltip.css";

interface LiveChartHoverProps {
  /** The chart's streamTo render delay; needed to map pixels back to time. */
  delayMs: number;
  /** Series values at the hovered moment; return [] to hide the tooltip. */
  rowsAt: (tMs: number) => LiveTooltipRow[];
}

/**
 * Crosshair + tooltip overlay for the smoothie live charts, replacing
 * smoothie's own tooltip. Stretch it over the canvas (parent must be
 * position:relative). All overlays share one hover position keyed on the
 * distance from the right edge — every live chart scrolls at
 * LIVE_MILLIS_PER_PIXEL, so hovering one chart shows the same moment on all
 * of them, like the synchronized hardware history cards.
 */
const LiveChartHover = ({ delayMs, rowsAt }: LiveChartHoverProps) => {
  const theme = useAppTheme();
  const overlayRef = useRef<HTMLDivElement>(null);
  const hoverRightPx = useSyncExternalStore(
    subscribeLiveHover,
    getLiveHoverRightPx,
  );

  // ResizeObserver delivers the initial size right after observe(), so no
  // synchronous measurement is needed.
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const overlay = overlayRef.current;
    if (!overlay) return;
    const observer = new ResizeObserver((entries) => {
      setWidth(entries[0]?.contentRect.width ?? 0);
    });
    observer.observe(overlay);
    return () => observer.disconnect();
  }, []);

  // Clock snapshot from the store: its shared ticker refreshes it once per
  // second while a hover is active, so the tooltip values track the samples
  // scrolling underneath the fixed crosshair without impure render reads.
  const hoverNowMs = useSyncExternalStore(
    subscribeLiveHover,
    getLiveHoverNowMs,
  );

  const visible = hoverRightPx !== null && width > 0 && hoverRightPx <= width;
  const hoverTime =
    visible && hoverRightPx !== null
      ? hoverNowMs - delayMs - hoverRightPx * LIVE_MILLIS_PER_PIXEL
      : null;
  const rows = hoverTime !== null ? rowsAt(hoverTime) : [];
  const tooltipOnLeft =
    hoverRightPx !== null && width - hoverRightPx > width / 2;

  return (
    <div
      onPointerLeave={() => setLiveHoverRightPx(null)}
      onPointerMove={(event) => {
        const rect = overlayRef.current?.getBoundingClientRect();
        if (!rect) return;
        setLiveHoverRightPx(rect.right - event.clientX);
      }}
      ref={overlayRef}
      style={{ position: "absolute", inset: 0 }}
    >
      {visible && hoverRightPx !== null && (
        <>
          <div
            style={{
              position: "absolute",
              top: 0,
              bottom: 0,
              right: hoverRightPx,
              width: 1,
              background: alpha(theme.chart.neutral, 0.4),
              pointerEvents: "none",
            }}
          />
          {hoverTime !== null && rows.length > 0 && (
            <div
              className="chart-tooltip-box"
              style={{
                position: "absolute",
                top: 4,
                ...(tooltipOnLeft
                  ? { right: hoverRightPx + 8 }
                  : { left: width - hoverRightPx + 8 }),
                pointerEvents: "none",
                zIndex: 2,
              }}
            >
              <div className="chart-tooltip-time">
                {formatChartClockWithSeconds(hoverTime)}
              </div>
              {rows.map((row) => (
                <div className="chart-tooltip-row" key={row.label ?? row.color}>
                  <span
                    className="chart-tooltip-chip"
                    style={{ background: row.color }}
                  />
                  <span className="chart-tooltip-value">{row.value}</span>
                  {row.label ? (
                    <span className="chart-tooltip-label">{row.label}</span>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default LiveChartHover;
