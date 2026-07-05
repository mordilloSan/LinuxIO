import React, { useEffect, useRef, useState, useSyncExternalStore } from "react";

import {
  getLiveHoverRightPx,
  LIVE_MILLIS_PER_PIXEL,
  setLiveHoverRightPx,
  subscribeLiveHover,
} from "@/components/charts/liveSeriesStore";
import {
  formatLiveTooltipTime,
  type LiveTooltipRow,
} from "@/components/charts/liveTooltip";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

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
const LiveChartHover: React.FC<LiveChartHoverProps> = ({ delayMs, rowsAt }) => {
  const theme = useAppTheme();
  const overlayRef = useRef<HTMLDivElement>(null);
  const hoverRightPx = useSyncExternalStore(
    subscribeLiveHover,
    getLiveHoverRightPx,
  );

  // While hovered, refresh once per second so the tooltip values track the
  // samples scrolling underneath the fixed crosshair.
  const [, setTick] = useState(0);
  const hovering = hoverRightPx !== null;
  useEffect(() => {
    if (!hovering) return;
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, [hovering]);

  const width = overlayRef.current?.getBoundingClientRect().width ?? 0;
  const visible = hoverRightPx !== null && width > 0 && hoverRightPx <= width;
  const hoverTime =
    visible && hoverRightPx !== null
      ? Date.now() - delayMs - hoverRightPx * LIVE_MILLIS_PER_PIXEL
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
              className="live-tooltip-box"
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
              <div className="live-tooltip-time">
                {formatLiveTooltipTime(hoverTime)}
              </div>
              {rows.map((row) => (
                <div className="live-tooltip-row" key={row.label ?? row.color}>
                  <span
                    className="live-tooltip-chip"
                    style={{ background: row.color }}
                  />
                  <span className="live-tooltip-value">{row.value}</span>
                  {row.label ? (
                    <span className="live-tooltip-label">{row.label}</span>
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
