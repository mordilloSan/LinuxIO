import { useEffect, useMemo, useRef, useState } from "react";

import { useAppTheme } from "@/theme";
import { GAP_MD } from "@/theme/constants";
import { alpha } from "@/utils/color";

import "@/components/charts/chart-tooltip.css";

export interface HistoryChartPoint {
  /** Epoch milliseconds. */
  t: number;
  v: number;
  /** Optional extra text shown after the value in the tooltip. */
  detail?: string;
}

export interface HistoryChartSeries {
  label: string;
  color: string;
  points: HistoryChartPoint[];
  /**
   * Draw this series faintly and leave it out of the crosshair dots and
   * tooltip. Used to focus a filtered subset of a large stack without
   * removing the rest, so the top of the stack stays the true total.
   */
  dimmed?: boolean;
}

interface HistoryAreaChartProps {
  series: HistoryChartSeries[];
  /** Fixed y-axis maximum (e.g. 100 for percentages). Auto-scaled if omitted. */
  yMax?: number;
  formatValue: (value: number) => string;
  /** Axis tick formatter; defaults to formatValue. */
  formatTick?: (value: number) => string;
  formatTimestamp: (t: number) => string;
  /**
   * Hovered timestamp (epoch ms) shared across synchronized charts. When
   * provided together with onHoverTimeChange, the crosshair and tooltip are
   * controlled and render on every synchronized chart.
   */
  hoverTime?: number | null;
  onHoverTimeChange?: (t: number | null) => void;
  /**
   * Fixed x-axis span in milliseconds, anchored at the newest data point.
   * When the data covers less than the window the left side stays empty
   * instead of stretching the data to fill the plot.
   */
  windowMs?: number;
  /**
   * Draw the series as cumulative bands (first series at the bottom) instead
   * of overlapping areas. All series must share the same timestamps. The
   * tooltip still reports each series' own value, listed top band first.
   */
  stacked?: boolean;
}

const MARGIN = { top: 6, right: 44, bottom: 18, left: 4 };
const Y_DIVISIONS = 4;

/**
 * Average consecutive samples into at most `budget` buckets. At full sample
 * density a segment spans ~1px and no curve is visible; fewer, averaged
 * points are what make the line read as smooth (same aggregation the agent's
 * own rollups use).
 */
const downsamplePoints = (
  points: HistoryChartPoint[],
  budget: number,
): HistoryChartPoint[] => {
  if (points.length <= budget) return points;
  const bucketSize = Math.ceil(points.length / budget);
  const out: HistoryChartPoint[] = [];
  for (let i = 0; i < points.length; i += bucketSize) {
    const bucket = points.slice(i, i + bucketSize);
    let tSum = 0;
    let vSum = 0;
    for (const point of bucket) {
      tSum += point.t;
      vSum += point.v;
    }
    out.push({
      t: tSum / bucket.length,
      v: vSum / bucket.length,
      detail: bucket[Math.floor(bucket.length / 2)].detail,
    });
  }
  return out;
};

/**
 * Build a smooth cubic path through the points using monotone (Fritsch–
 * Butland) interpolation: curved like a spline but without overshooting
 * spikes, so the line never suggests values the data doesn't have.
 */
const smoothPath = (points: { x: number; y: number }[]): string => {
  if (points.length === 0) return "";
  if (points.length === 1) return `M${points[0].x},${points[0].y}`;

  const slopes: number[] = [];
  for (let i = 0; i < points.length - 1; i++) {
    const dx = points[i + 1].x - points[i].x;
    slopes.push(dx === 0 ? 0 : (points[i + 1].y - points[i].y) / dx);
  }
  const tangents = points.map((_, i) => {
    if (i === 0) return slopes[0];
    if (i === points.length - 1) return slopes[slopes.length - 1];
    const a = slopes[i - 1];
    const b = slopes[i];
    return a * b <= 0 ? 0 : (2 * a * b) / (a + b);
  });

  let path = `M${points[0].x.toFixed(1)},${points[0].y.toFixed(1)}`;
  for (let i = 0; i < points.length - 1; i++) {
    const dx = (points[i + 1].x - points[i].x) / 3;
    path +=
      `C${(points[i].x + dx).toFixed(1)},${(points[i].y + tangents[i] * dx).toFixed(1)}` +
      ` ${(points[i + 1].x - dx).toFixed(1)},${(points[i + 1].y - tangents[i + 1] * dx).toFixed(1)}` +
      ` ${points[i + 1].x.toFixed(1)},${points[i + 1].y.toFixed(1)}`;
  }
  return path;
};

/** Round up to a clean axis maximum in the value's 1024-based display unit. */
const niceMax = (value: number): number => {
  if (!Number.isFinite(value) || value <= 0) return 1;
  let unit = 1;
  while (value / unit >= 1024 && unit < 1024 ** 4) unit *= 1024;
  const inUnit = value / unit;
  const pow = 10 ** Math.floor(Math.log10(inUnit));
  for (const mantissa of [1, 2, 2.5, 5, 10]) {
    if (inUnit <= mantissa * pow) return mantissa * pow * unit;
  }
  return 10 * pow * unit;
};

const HistoryAreaChart = ({
  series,
  yMax,
  formatValue,
  formatTick,
  formatTimestamp,
  hoverTime,
  onHoverTimeChange,
  windowMs,
  stacked = false,
}: HistoryAreaChartProps) => {
  const theme = useAppTheme();
  const containerRef = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [localHoverTime, setLocalHoverTime] = useState<number | null>(null);

  const controlled = onHoverTimeChange !== undefined;
  const effectiveHoverTime = controlled ? (hoverTime ?? null) : localHoverTime;
  const setHoverTime = (t: number | null) => {
    if (controlled) {
      onHoverTimeChange(t);
    } else {
      setLocalHoverTime(t);
    }
  };

  useEffect(() => {
    const element = containerRef.current;
    if (!element) return;
    const observer = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect;
      if (rect) {
        setSize({ width: rect.width, height: rect.height });
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const plotLeft = MARGIN.left;
  const plotTop = MARGIN.top;
  const plotWidth = Math.max(0, size.width - MARGIN.left - MARGIN.right);
  const plotHeight = Math.max(0, size.height - MARGIN.top - MARGIN.bottom);

  // Everything derived from data + plot size, independent of the hover
  // position. Memoized so moving the crosshair (a re-render per pointer
  // event) doesn't rebuild the smooth paths of every band — with per-core
  // stacks that is dozens of series.
  const {
    visibleSeries,
    timestamps,
    pointCount,
    plotValues,
    axisMax,
    domainStart,
    t0,
    t1,
    xFor,
    yFor,
    paths,
  } = useMemo(() => {
    const lastDataT = series[0]?.points[series[0].points.length - 1]?.t;
    const domainStart =
      windowMs !== undefined && lastDataT !== undefined
        ? lastDataT - windowMs
        : undefined;
    const windowedSeries =
      domainStart === undefined
        ? series
        : series.map((s) => ({
            ...s,
            points: s.points.filter((point) => point.t >= domainStart),
          }));

    // ~1 point per 4px of plot; before the first measure fall back to a
    // conservative budget so the initial render is already smooth.
    const pointBudget = Math.max(40, Math.floor(plotWidth / 4));
    const visibleSeries = windowedSeries.map((s) => ({
      ...s,
      points: downsamplePoints(s.points, pointBudget),
    }));

    const timestamps = visibleSeries[0]?.points.map((point) => point.t) ?? [];
    const pointCount = timestamps.length;

    // Values actually plotted: raw per series, or running totals when stacked
    // so band i is drawn between the cumulative curves i-1 and i.
    const plotValues = visibleSeries.map((s) =>
      s.points.map((point) => point.v),
    );
    if (stacked) {
      for (let i = 1; i < plotValues.length; i++) {
        const below = plotValues[i - 1];
        for (let k = 0; k < plotValues[i].length; k++) {
          plotValues[i][k] += below[k] ?? 0;
        }
      }
    }

    let axisMax = yMax ?? 0;
    if (typeof yMax !== "number") {
      let max = 0;
      for (const values of plotValues) {
        for (const value of values) {
          if (value > max) max = value;
        }
      }
      axisMax = niceMax(max);
    }

    const t0 = domainStart ?? timestamps[0] ?? 0;
    const t1 = lastDataT ?? timestamps[pointCount - 1] ?? 0;
    const timeSpan = Math.max(1, t1 - t0);

    const xFor = (t: number): number =>
      pointCount < 2 && domainStart === undefined
        ? MARGIN.left + plotWidth / 2
        : MARGIN.left + ((t - t0) / timeSpan) * plotWidth;
    const yFor = (v: number): number =>
      MARGIN.top + plotHeight - (Math.min(v, axisMax) / axisMax) * plotHeight;

    const paths = visibleSeries.map((s, i) => {
      if (plotWidth <= 0 || plotHeight <= 0 || s.points.length === 0) {
        return { line: "", area: "" };
      }
      const coords = s.points.map((point, k) => ({
        x: xFor(point.t),
        y: yFor(plotValues[i][k]),
      }));
      const line = smoothPath(coords);
      if (stacked && i > 0) {
        // Close the band against the curve below instead of the baseline.
        const below = s.points
          .map((point, k) => ({
            x: xFor(point.t),
            y: yFor(plotValues[i - 1][k]),
          }))
          .reverse();
        return { line, area: `${line}L${smoothPath(below).slice(1)}Z` };
      }
      const baseline = (MARGIN.top + plotHeight).toFixed(1);
      const area =
        `${line}L${coords[coords.length - 1].x.toFixed(1)},${baseline}` +
        `L${coords[0].x.toFixed(1)},${baseline}Z`;
      return { line, area };
    });

    return {
      visibleSeries,
      timestamps,
      pointCount,
      plotValues,
      axisMax,
      domainStart,
      t0,
      t1,
      xFor,
      yFor,
      paths,
    };
  }, [series, yMax, windowMs, stacked, plotWidth, plotHeight]);

  const nearestIndexToTime = (t: number): number | null => {
    if (pointCount === 0) return null;
    let best = 0;
    let bestDistance = Infinity;
    for (let i = 0; i < pointCount; i++) {
      const distance = Math.abs(timestamps[i] - t);
      if (distance < bestDistance) {
        bestDistance = distance;
        best = i;
      }
    }
    return best;
  };

  const timeAtClientX = (clientX: number): number | null => {
    const element = containerRef.current;
    if (!element || pointCount === 0) return null;
    const rect = element.getBoundingClientRect();
    const x = clientX - rect.left;
    let best = 0;
    let bestDistance = Infinity;
    for (let i = 0; i < pointCount; i++) {
      const distance = Math.abs(xFor(timestamps[i]) - x);
      if (distance < bestDistance) {
        bestDistance = distance;
        best = i;
      }
    }
    return timestamps[best];
  };

  const hoverIndex =
    effectiveHoverTime !== null ? nearestIndexToTime(effectiveHoverTime) : null;

  const moveHoverIndex = (delta: number) => {
    if (pointCount === 0) return;
    const base = hoverIndex ?? pointCount - 1;
    const next = Math.min(pointCount - 1, Math.max(0, base + delta));
    setHoverTime(timestamps[next]);
  };

  const hover =
    hoverIndex !== null && hoverIndex < pointCount
      ? {
          t: timestamps[hoverIndex],
          x: xFor(timestamps[hoverIndex]),
          index: hoverIndex,
          values: visibleSeries.map((s) => s.points[hoverIndex]),
        }
      : null;
  // Tooltip rows follow the visual order: top band first when stacked.
  const tooltipRows = hover
    ? hover.values
        .map((point, i) => ({ point, seriesIndex: i }))
        .filter(({ seriesIndex }) => !visibleSeries[seriesIndex].dimmed)
    : [];
  if (stacked) tooltipRows.reverse();
  const tooltipOnLeft = hover !== null && hover.x > plotLeft + plotWidth / 2;

  const gridColor = alpha(theme.chart.neutral, 0.15);
  const textColor = alpha(theme.chart.neutral, 0.8);
  const ticks = Array.from(
    { length: Y_DIVISIONS + 1 },
    (_, i) => (axisMax / Y_DIVISIONS) * i,
  );
  const tickFormatter = formatTick ?? formatValue;

  return (
    <div
      aria-label={`${series.map((s) => s.label).join(", ")} history chart`}
      onBlur={() => setHoverTime(null)}
      onFocus={() => {
        if (effectiveHoverTime === null && pointCount > 0) {
          setHoverTime(timestamps[pointCount - 1]);
        }
      }}
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft") {
          event.preventDefault();
          moveHoverIndex(-1);
        } else if (event.key === "ArrowRight") {
          event.preventDefault();
          moveHoverIndex(1);
        }
      }}
      onPointerLeave={() => setHoverTime(null)}
      onPointerMove={(event) => setHoverTime(timeAtClientX(event.clientX))}
      ref={containerRef}
      role="img"
      style={{
        position: "relative",
        width: "100%",
        height: "100%",
        minWidth: 0,
        outline: "none",
      }}
      tabIndex={0}
    >
      {size.width > 0 && size.height > 0 && (
        <svg
          height={size.height}
          style={{ display: "block" }}
          width={size.width}
        >
          {ticks.map((tick) => (
            <g key={tick}>
              <line
                stroke={gridColor}
                strokeWidth={1}
                x1={plotLeft}
                x2={plotLeft + plotWidth}
                y1={yFor(tick)}
                y2={yFor(tick)}
              />
              <text
                dominantBaseline="middle"
                fill={textColor}
                fontSize={9}
                x={plotLeft + plotWidth + 6}
                y={yFor(tick)}
              >
                {tickFormatter(tick)}
              </text>
            </g>
          ))}
          {visibleSeries.map((s, i) => (
            <g key={s.label}>
              <path
                d={paths[i]?.area ?? ""}
                fill={alpha(s.color, s.dimmed ? 0.05 : stacked ? 0.4 : 0.1)}
              />
              <path
                d={paths[i]?.line ?? ""}
                fill="none"
                stroke={s.color}
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeOpacity={s.dimmed ? 0.15 : 1}
                strokeWidth={stacked ? 1.25 : 2}
              />
            </g>
          ))}
          {hover && (
            <g>
              <line
                stroke={alpha(theme.chart.neutral, 0.4)}
                strokeWidth={1}
                x1={hover.x}
                x2={hover.x}
                y1={plotTop}
                y2={plotTop + plotHeight}
              />
              {hover.values.map((point, i) =>
                point && !visibleSeries[i].dimmed ? (
                  <circle
                    cx={hover.x}
                    cy={yFor(plotValues[i][hover.index])}
                    fill={visibleSeries[i].color}
                    key={visibleSeries[i].label}
                    r={stacked ? 3 : 4}
                    stroke={theme.palette.background.paper}
                    strokeWidth={stacked ? 1 : 2}
                  />
                ) : null,
              )}
            </g>
          )}
          {(pointCount > 1 ||
            (pointCount > 0 && domainStart !== undefined)) && (
            <>
              <text
                fill={textColor}
                fontSize={9}
                x={plotLeft}
                y={size.height - 4}
              >
                {formatTimestamp(t0)}
              </text>
              <text
                fill={textColor}
                fontSize={9}
                textAnchor="end"
                x={plotLeft + plotWidth}
                y={size.height - 4}
              >
                {formatTimestamp(t1)}
              </text>
            </>
          )}
        </svg>
      )}
      {hover && (
        <div
          className="chart-tooltip-box"
          style={{
            position: "absolute",
            top: plotTop,
            ...(tooltipOnLeft
              ? { right: size.width - hover.x + 8 }
              : { left: hover.x + 8 }),
            pointerEvents: "none",
            zIndex: 2,
          }}
        >
          <div className="chart-tooltip-time">{formatTimestamp(hover.t)}</div>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: tooltipRows.length > 8 ? "1fr 1fr" : "1fr",
              columnGap: GAP_MD,
            }}
          >
            {tooltipRows.map(({ point, seriesIndex }) =>
              point ? (
                <div
                  className="chart-tooltip-row"
                  key={visibleSeries[seriesIndex].label}
                >
                  <span
                    className="chart-tooltip-chip"
                    style={{ background: visibleSeries[seriesIndex].color }}
                  />
                  <span className="chart-tooltip-value">
                    {formatValue(point.v)}
                  </span>
                  {point.detail ? (
                    <span className="chart-tooltip-label">{point.detail}</span>
                  ) : null}
                  {visibleSeries.length > 1 ? (
                    <span className="chart-tooltip-label">
                      {visibleSeries[seriesIndex].label}
                    </span>
                  ) : null}
                </div>
              ) : null,
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default HistoryAreaChart;
