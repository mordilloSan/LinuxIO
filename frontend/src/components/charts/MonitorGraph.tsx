import React, { useMemo, useRef, useState } from "react";

import type {
  DiskIOMonitoringSeriesResponse,
  MonitoringRange,
  MonitoringSeriesPoint,
  MonitoringSeriesResponse,
  NetworkMonitoringSeriesResponse,
} from "@/api";
import AppSkeleton from "@/components/ui/AppSkeleton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

// ─── constants ───────────────────────────────────────────────────────────────

export const RANGE_DURATION_MS: Record<MonitoringRange, number> = {
  "1m": 60 * 1000,
  "5m": 5 * 60 * 1000,
  "15m": 15 * 60 * 1000,
  "60m": 60 * 60 * 1000,
  "6h": 6 * 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

// ─── types ───────────────────────────────────────────────────────────────────

type PlotPoint = {
  x: number;
  y: number;
  point: MonitoringSeriesPoint;
};

// ─── helpers ─────────────────────────────────────────────────────────────────

const clampPercent = (value: number) => Math.max(0, Math.min(100, value));

const formatPercent = (value?: number | null): string =>
  typeof value === "number" && Number.isFinite(value)
    ? `${Math.round(value)}%`
    : "—";

export const formatNetworkRate = (value?: number | null): string => {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "—";
  }

  const absoluteValue = Math.abs(value);
  if (absoluteValue >= 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(absoluteValue >= 10 * 1024 * 1024 ? 0 : 1)} GB/s`;
  }
  if (absoluteValue >= 1024) {
    return `${(value / 1024).toFixed(absoluteValue >= 10 * 1024 ? 0 : 1)} MB/s`;
  }
  return `${value.toFixed(absoluteValue >= 100 ? 0 : 1)} kB/s`;
};

export const formatDiskRate = (value?: number | null): string => {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "—";
  }

  const absoluteValue = Math.abs(value);
  if (absoluteValue >= 1024 * 1024 * 1024) {
    return `${(value / (1024 * 1024 * 1024)).toFixed(
      absoluteValue >= 10 * 1024 * 1024 * 1024 ? 0 : 1,
    )} GB/s`;
  }
  if (absoluteValue >= 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(
      absoluteValue >= 10 * 1024 * 1024 ? 0 : 1,
    )} MB/s`;
  }
  if (absoluteValue >= 1024) {
    return `${(value / 1024).toFixed(absoluteValue >= 10 * 1024 ? 0 : 1)} kB/s`;
  }
  return `${value.toFixed(absoluteValue >= 100 ? 0 : 1)} B/s`;
};

const formatChartTimestamp = (
  timestamp: number | undefined,
  range: MonitoringRange,
) => {
  if (!timestamp) {
    return "Waiting for samples";
  }

  const date = new Date(timestamp);

  if (range === "1m" || range === "5m" || range === "15m") {
    return date.toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  }

  if (range === "60m" || range === "6h" || range === "24h") {
    return date.toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
};

export const buildMonitoringPlotPoints = ({
  points,
  paddingLeft,
  paddingTop,
  innerWidth,
  innerHeight,
  rangeDurationMs,
  yAxisMax,
}: {
  points: MonitoringSeriesPoint[];
  paddingLeft: number;
  paddingTop: number;
  innerWidth: number;
  innerHeight: number;
  rangeDurationMs: number;
  yAxisMax: number;
}): PlotPoint[] => {
  if (points.length === 0) {
    return [];
  }

  const latestTimestamp = points[points.length - 1]?.ts ?? 0;
  const windowStart = latestTimestamp - rangeDurationMs;
  const windowSpan = Math.max(rangeDurationMs, 1);

  return points.map((point) => {
    const ratio = Math.max(
      0,
      Math.min(1, (point.ts - windowStart) / windowSpan),
    );
    const value = Math.max(0, Math.min(yAxisMax, point.value));

    return {
      x: paddingLeft + ratio * innerWidth,
      y: paddingTop + ((yAxisMax - value) / yAxisMax) * innerHeight,
      point,
    };
  });
};

// ─── ChartSkeleton ──────────────────────────────────────────────────────────

const SKELETON_VIEW_WIDTH = 220;
const SKELETON_VIEW_HEIGHT = 120;
const SKELETON_PADDING_TOP = 8;
const SKELETON_PADDING_BOTTOM = 16;
const SKELETON_PADDING_LEFT = 4;
const SKELETON_INNER_HEIGHT =
  SKELETON_VIEW_HEIGHT - SKELETON_PADDING_TOP - SKELETON_PADDING_BOTTOM;
const SKELETON_INNER_WIDTH = SKELETON_VIEW_WIDTH - SKELETON_PADDING_LEFT;

const ChartSkeleton: React.FC<{
  ticks: { label: string; ratio: number }[];
}> = ({ ticks }) => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        minWidth: 0,
        width: "100%",
        flex: 1,
        display: "flex",
        flexDirection: "column",
        gap: 8,
      }}
    >
      <div style={{ display: "flex" }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <svg
            viewBox={`0 0 ${SKELETON_VIEW_WIDTH} ${SKELETON_VIEW_HEIGHT}`}
            style={{ width: "100%", height: 120, display: "block" }}
            preserveAspectRatio="none"
          >
            {ticks.map(({ ratio }) => {
              const y =
                SKELETON_PADDING_TOP + (1 - ratio) * SKELETON_INNER_HEIGHT;
              return (
                <line
                  key={ratio}
                  x1={SKELETON_PADDING_LEFT}
                  y1={y}
                  x2={SKELETON_PADDING_LEFT + SKELETON_INNER_WIDTH}
                  y2={y}
                  stroke={alpha(theme.chart.neutral, 0.16)}
                  strokeWidth={1}
                />
              );
            })}
          </svg>
        </div>
        <div
          style={{
            width: 28,
            height: 120,
            position: "relative",
            flexShrink: 0,
          }}
        >
          {ticks.map(({ label, ratio }) => {
            const top =
              SKELETON_PADDING_TOP + (1 - ratio) * SKELETON_INNER_HEIGHT;
            return (
              <div
                key={ratio}
                style={{
                  position: "absolute",
                  top,
                  right: 2,
                  transform: "translateY(-50%)",
                  fontSize: "8px",
                  lineHeight: 1,
                  color: alpha(theme.chart.neutral, 0.75),
                  whiteSpace: "nowrap",
                }}
              >
                {label}
              </div>
            );
          })}
        </div>
      </div>
      <div style={{ display: "flex", gap: 8 }}>
        <AppSkeleton variant="text" width={80} height={14} />
        <div style={{ flex: 1 }} />
        <AppSkeleton variant="text" width={60} height={14} />
      </div>
    </div>
  );
};

const PERCENT_TICKS = [
  { label: "0%", ratio: 0 },
  { label: "25%", ratio: 0.25 },
  { label: "50%", ratio: 0.5 },
  { label: "75%", ratio: 0.75 },
  { label: "100%", ratio: 1 },
];

const RATE_TICKS = [
  { label: "0 kB/s", ratio: 0 },
  { label: "", ratio: 0.25 },
  { label: "", ratio: 0.5 },
  { label: "", ratio: 0.75 },
  { label: "", ratio: 1 },
];

// ─── MonitorGraph ─────────────────────────────────────────────────────────────

export const MonitorGraph: React.FC<{
  color: string;
  label: string;
  range: MonitoringRange;
  series: MonitoringSeriesResponse | undefined;
  loading: boolean;
  emptyMessage: string;
  stackedPercent?: number;
  stackedColor?: string;
  stackedLabel?: string;
  stackedTooltipLabel?: string;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  color,
  label,
  range,
  series,
  loading,
  emptyMessage,
  stackedPercent,
  stackedColor,
  stackedLabel,
  stackedTooltipLabel,
  hoverRatio: externalHoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const chartRef = useRef<HTMLDivElement>(null);
  const [internalHoverRatio, setInternalHoverRatio] = useState<number | null>(
    null,
  );
  const effectiveHoverRatio =
    externalHoverRatio !== undefined ? externalHoverRatio : internalHoverRatio;
  const [mousePos, setMousePos] = useState<{
    x: number;
    y: number;
    containerWidth: number;
  } | null>(null);

  const points = useMemo(
    () => (series?.available ? series.points : []),
    [series],
  );
  const latestPoint = points.at(-1);
  const fallbackMessage = series?.reason || emptyMessage;
  const viewWidth = 220;
  const viewHeight = 120;
  const paddingTop = 8;
  const paddingRight = 0;
  const paddingBottom = 16;
  const paddingLeft = 4;
  const innerWidth = viewWidth - paddingLeft - paddingRight;
  const innerHeight = viewHeight - paddingTop - paddingBottom;
  const rangeDurationMs = RANGE_DURATION_MS[range];
  const hasStackedSegment =
    typeof stackedPercent === "number" &&
    Number.isFinite(stackedPercent) &&
    stackedPercent > 0 &&
    typeof stackedColor === "string" &&
    stackedColor.length > 0 &&
    typeof stackedLabel === "string" &&
    stackedLabel.length > 0;

  const plotPoints = useMemo(() => {
    if (points.length === 0) {
      return [];
    }
    const latestTimestamp = points[points.length - 1]?.ts ?? 0;
    const windowStart = latestTimestamp - rangeDurationMs;
    const windowSpan = Math.max(rangeDurationMs, 1);

    return points.map((point) => {
      const ratio = Math.max(
        0,
        Math.min(1, (point.ts - windowStart) / windowSpan),
      );
      const value = clampPercent(point.value);
      return {
        x: paddingLeft + ratio * innerWidth,
        y: paddingTop + ((100 - value) / 100) * innerHeight,
        point,
      };
    });
  }, [innerHeight, innerWidth, points, rangeDurationMs]);

  const basePlotPoints = useMemo(() => {
    if (!hasStackedSegment) {
      return plotPoints;
    }

    return plotPoints.map(({ x, point }) => {
      const stackedValue = Math.min(
        clampPercent(stackedPercent ?? 0),
        point.value,
      );
      const baseValue = clampPercent(point.value - stackedValue);

      return {
        x,
        y: paddingTop + ((100 - baseValue) / 100) * innerHeight,
        point,
      };
    });
  }, [hasStackedSegment, innerHeight, plotPoints, stackedPercent]);

  const hoverIndex =
    effectiveHoverRatio != null && plotPoints.length > 0
      ? Math.max(
          0,
          Math.min(
            plotPoints.length - 1,
            Math.round(effectiveHoverRatio * (plotPoints.length - 1)),
          ),
        )
      : null;

  const hoveredPoint = hoverIndex != null ? plotPoints[hoverIndex] : undefined;
  const activePoint = hoveredPoint ?? plotPoints.at(-1);
  const activeBasePoint =
    hoverIndex != null ? basePlotPoints[hoverIndex] : undefined;

  const buildLinePath = (chartPoints: typeof plotPoints): string => {
    if (chartPoints.length === 0) {
      return "";
    }

    return chartPoints
      .map(({ x, y }, index) => `${index === 0 ? "M" : "L"} ${x} ${y}`)
      .join(" ");
  };

  const buildAreaPath = (chartPoints: typeof plotPoints): string => {
    if (chartPoints.length === 0) {
      return "";
    }

    const line = buildLinePath(chartPoints);
    const first = chartPoints[0];
    const last = chartPoints[chartPoints.length - 1];
    return `${line} L ${last.x} ${paddingTop + innerHeight} L ${first.x} ${paddingTop + innerHeight} Z`;
  };

  const buildBandPath = (
    lowerPoints: typeof plotPoints,
    upperPoints: typeof plotPoints,
  ): string => {
    if (lowerPoints.length === 0 || lowerPoints.length !== upperPoints.length) {
      return "";
    }

    const upperLine = buildLinePath(upperPoints);
    const lowerLine = lowerPoints
      .slice()
      .reverse()
      .map(({ x, y }) => `L ${x} ${y}`)
      .join(" ");

    return `${upperLine} ${lowerLine} Z`;
  };

  const linePath = buildLinePath(plotPoints);
  const baseLinePath = buildLinePath(basePlotPoints);
  const areaPath = buildAreaPath(basePlotPoints);
  const stackedAreaPath = hasStackedSegment
    ? buildBandPath(basePlotPoints, plotPoints)
    : "";
  const activeStackedPercent = hasStackedSegment
    ? Math.min(clampPercent(stackedPercent ?? 0), activePoint?.point.value ?? 0)
    : 0;

  const handlePointerMove = (clientX: number, clientY: number) => {
    if (!chartRef.current || plotPoints.length === 0) {
      return;
    }
    const rect = chartRef.current.getBoundingClientRect();
    const x = clientX - rect.left;
    const y = clientY - rect.top;
    const ratio = Math.max(0, Math.min(1, x / rect.width));
    setInternalHoverRatio(ratio);
    setMousePos({ x, y, containerWidth: rect.width });
    onHoverChange?.(ratio);
  };

  const handleMouseLeave = () => {
    setInternalHoverRatio(null);
    setMousePos(null);
    onHoverChange?.(null);
  };

  if (loading && points.length === 0) {
    return <ChartSkeleton ticks={PERCENT_TICKS} />;
  }

  if (!series?.available || plotPoints.length === 0) {
    return (
      <div
        style={{
          minHeight: 150,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          textAlign: "center",
          paddingInline: 12,
          color: theme.palette.text.secondary,
        }}
      >
        <AppTypography variant="body2">{fallbackMessage}</AppTypography>
      </div>
    );
  }

  return (
    <div
      style={{
        minWidth: 0,
        width: "100%",
        flex: 1,
        display: "flex",
        flexDirection: "column",
        gap: 8,
      }}
    >
      <div style={{ display: "flex" }}>
        <div
          ref={chartRef}
          style={{ flex: 1, minWidth: 0, position: "relative" }}
          onMouseMove={(event) =>
            handlePointerMove(event.clientX, event.clientY)
          }
          onMouseLeave={handleMouseLeave}
        >
          <svg
            viewBox={`0 0 ${viewWidth} ${viewHeight}`}
            style={{ width: "100%", height: 120, display: "block" }}
            preserveAspectRatio="none"
          >
            {[0, 25, 50, 75, 100].map((tick) => {
              const y = paddingTop + ((100 - tick) / 100) * innerHeight;
              return (
                <line
                  key={tick}
                  x1={paddingLeft}
                  y1={y}
                  x2={paddingLeft + innerWidth}
                  y2={y}
                  stroke={alpha(theme.chart.neutral, 0.16)}
                  strokeWidth={1}
                />
              );
            })}

            <defs>
              <linearGradient
                id={`history-fill-${label}`}
                x1="0"
                x2="0"
                y1="0"
                y2="1"
              >
                <stop offset="0%" stopColor={alpha(color, 0.28)} />
                <stop offset="100%" stopColor={alpha(color, 0.02)} />
              </linearGradient>
              {hasStackedSegment && (
                <linearGradient
                  id={`history-fill-${label}-stacked`}
                  x1="0"
                  x2="0"
                  y1="0"
                  y2="1"
                >
                  <stop offset="0%" stopColor={alpha(stackedColor!, 0.32)} />
                  <stop offset="100%" stopColor={alpha(stackedColor!, 0.1)} />
                </linearGradient>
              )}
            </defs>

            {areaPath && (
              <path
                d={areaPath}
                fill={`url(#history-fill-${label})`}
                stroke="none"
              />
            )}
            {stackedAreaPath && (
              <path
                d={stackedAreaPath}
                fill={`url(#history-fill-${label}-stacked)`}
                stroke="none"
              />
            )}
            {hasStackedSegment && baseLinePath && (
              <path
                d={baseLinePath}
                fill="none"
                stroke={alpha(color, 0.45)}
                strokeWidth={1}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            )}
            {linePath && (
              <path
                d={linePath}
                fill="none"
                stroke={hasStackedSegment ? stackedColor! : color}
                strokeWidth={1.5}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            )}
          </svg>

          {effectiveHoverRatio != null && hoveredPoint && (
            <div
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                pointerEvents: "none",
              }}
            >
              <div
                style={{
                  position: "absolute",
                  left: `${(hoveredPoint.x / viewWidth) * 100}%`,
                  top: paddingTop,
                  height: innerHeight,
                  width: 1,
                  borderLeft: `1px dashed ${alpha(hasStackedSegment ? stackedColor! : color, 0.4)}`,
                  transform: "translateX(-50%)",
                }}
              />
              <div
                style={{
                  position: "absolute",
                  left: `${(hoveredPoint.x / viewWidth) * 100}%`,
                  top: hoveredPoint.y,
                  transform: "translate(-50%, -50%)",
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  backgroundColor: theme.palette.background.paper,
                  border: `2px solid ${hasStackedSegment ? stackedColor! : color}`,
                }}
              />
              {hasStackedSegment && activeBasePoint && (
                <div
                  style={{
                    position: "absolute",
                    left: `${(activeBasePoint.x / viewWidth) * 100}%`,
                    top: activeBasePoint.y,
                    transform: "translate(-50%, -50%)",
                    width: 6,
                    height: 6,
                    borderRadius: "50%",
                    backgroundColor: theme.palette.background.paper,
                    border: `1.5px solid ${stackedColor}`,
                  }}
                />
              )}
            </div>
          )}

          {mousePos && effectiveHoverRatio != null && hoveredPoint && (
            <div
              style={{
                position: "absolute",
                ...(mousePos.x > mousePos.containerWidth / 2
                  ? { right: mousePos.containerWidth - mousePos.x + 12 }
                  : { left: mousePos.x + 12 }),
                top: Math.max(0, mousePos.y - 20),
                pointerEvents: "none",
                zIndex: 10,
                backgroundColor: theme.palette.background.paper,
                border: `1px solid ${alpha(theme.chart.neutral, 0.2)}`,
                borderRadius: 6,
                padding: "4px 8px",
                display: "flex",
                flexDirection: "column",
                gap: 2,
                whiteSpace: "nowrap",
                boxShadow: "0 2px 8px rgba(0,0,0,0.3)",
              }}
            >
              {hasStackedSegment ? (
                <>
                  <AppTypography variant="caption" fontWeight={600}>
                    {stackedTooltipLabel ?? label}:{" "}
                    {formatPercent(
                      hoveredPoint.point.value - activeStackedPercent,
                    )}
                  </AppTypography>
                  <AppTypography variant="caption" color="text.secondary">
                    {stackedLabel}: {formatPercent(activeStackedPercent)}
                  </AppTypography>
                </>
              ) : (
                <AppTypography variant="caption" fontWeight={600}>
                  {label}: {formatPercent(hoveredPoint.point.value)}
                </AppTypography>
              )}
            </div>
          )}
        </div>
        <div
          style={{
            width: 28,
            height: 120,
            position: "relative",
            flexShrink: 0,
            pointerEvents: "none",
          }}
        >
          {[0, 25, 50, 75, 100].map((tick) => {
            const top = paddingTop + ((100 - tick) / 100) * innerHeight;
            return (
              <div
                key={tick}
                style={{
                  position: "absolute",
                  top,
                  right: 2,
                  transform: "translateY(-50%)",
                  fontSize: "8px",
                  lineHeight: 1,
                  color: alpha(theme.chart.neutral, 0.75),
                  whiteSpace: "nowrap",
                }}
              >
                {tick}%
              </div>
            );
          })}
        </div>
      </div>

      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          gap: 8,
        }}
      >
        <AppTypography variant="caption" fontWeight={700}>
          {label}: {formatPercent(latestPoint?.value)}
        </AppTypography>
        <AppTypography variant="caption" color="text.secondary" noWrap>
          {formatChartTimestamp(latestPoint?.ts, range)}
        </AppTypography>
      </div>
    </div>
  );
};

// ─── NetworkMonitorGraph ──────────────────────────────────────────────────────

export const NetworkMonitorGraph: React.FC<{
  range: MonitoringRange;
  series: NetworkMonitoringSeriesResponse | undefined;
  loading: boolean;
  emptyMessage: string;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  range,
  series,
  loading,
  emptyMessage,
  hoverRatio: externalHoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const chartRef = useRef<HTMLDivElement>(null);
  const [internalHoverRatio, setInternalHoverRatio] = useState<number | null>(
    null,
  );
  const [mousePos, setMousePos] = useState<{
    x: number;
    y: number;
    containerWidth: number;
  } | null>(null);
  const effectiveHoverRatio =
    externalHoverRatio !== undefined ? externalHoverRatio : internalHoverRatio;

  const rxSeries = useMemo(
    () => (series?.available ? series.rxPoints : []),
    [series],
  );
  const txSeries = useMemo(
    () => (series?.available ? series.txPoints : []),
    [series],
  );
  const fallbackMessage = series?.reason || emptyMessage;
  const viewWidth = 220;
  const viewHeight = 120;
  const paddingTop = 8;
  const paddingRight = 0;
  const paddingBottom = 16;
  const paddingLeft = 4;
  const innerWidth = viewWidth - paddingLeft - paddingRight;
  const innerHeight = viewHeight - paddingTop - paddingBottom;
  const rangeDurationMs = RANGE_DURATION_MS[range];

  const maxValue = useMemo(() => {
    let currentMax = 0;
    for (const point of rxSeries) {
      currentMax = Math.max(currentMax, point.value);
    }
    for (const point of txSeries) {
      currentMax = Math.max(currentMax, point.value);
    }
    return Math.max(currentMax, 1);
  }, [rxSeries, txSeries]);

  const yAxisMax = Math.max(maxValue * 1.1, 1);

  const rxPlotPoints = useMemo(
    () =>
      buildMonitoringPlotPoints({
        points: rxSeries,
        paddingLeft,
        paddingTop,
        innerWidth,
        innerHeight,
        rangeDurationMs,
        yAxisMax,
      }),
    [
      innerHeight,
      innerWidth,
      paddingLeft,
      paddingTop,
      rangeDurationMs,
      rxSeries,
      yAxisMax,
    ],
  );
  const txPlotPoints = useMemo(
    () =>
      buildMonitoringPlotPoints({
        points: txSeries,
        paddingLeft,
        paddingTop,
        innerWidth,
        innerHeight,
        rangeDurationMs,
        yAxisMax,
      }),
    [
      innerHeight,
      innerWidth,
      paddingLeft,
      paddingTop,
      rangeDurationMs,
      txSeries,
      yAxisMax,
    ],
  );

  const plotPointCount = Math.max(rxPlotPoints.length, txPlotPoints.length);
  const hoverIndex =
    effectiveHoverRatio != null && plotPointCount > 0
      ? Math.max(
          0,
          Math.min(
            plotPointCount - 1,
            Math.round(effectiveHoverRatio * (plotPointCount - 1)),
          ),
        )
      : null;

  const hoveredRXPoint =
    hoverIndex != null ? rxPlotPoints[hoverIndex] : undefined;
  const hoveredTXPoint =
    hoverIndex != null ? txPlotPoints[hoverIndex] : undefined;
  const activeRXPoint = hoveredRXPoint ?? rxPlotPoints.at(-1);
  const activeTXPoint = hoveredTXPoint ?? txPlotPoints.at(-1);
  const activePoint =
    hoveredRXPoint ??
    hoveredTXPoint ??
    rxPlotPoints.at(-1) ??
    txPlotPoints.at(-1);

  const buildLinePath = (chartPoints: { x: number; y: number }[]): string => {
    if (chartPoints.length === 0) {
      return "";
    }

    return chartPoints
      .map(({ x, y }, index) => `${index === 0 ? "M" : "L"} ${x} ${y}`)
      .join(" ");
  };

  const buildAreaPath = (chartPoints: { x: number; y: number }[]): string => {
    if (chartPoints.length === 0) {
      return "";
    }

    const line = buildLinePath(chartPoints);
    const first = chartPoints[0];
    const last = chartPoints[chartPoints.length - 1];
    return `${line} L ${last.x} ${paddingTop + innerHeight} L ${first.x} ${paddingTop + innerHeight} Z`;
  };

  const rxLinePath = buildLinePath(rxPlotPoints);
  const txLinePath = buildLinePath(txPlotPoints);
  const rxAreaPath = buildAreaPath(rxPlotPoints);
  const txAreaPath = buildAreaPath(txPlotPoints);

  const handlePointerMove = (clientX: number, clientY: number) => {
    if (!chartRef.current || plotPointCount === 0) {
      return;
    }

    const rect = chartRef.current.getBoundingClientRect();
    const x = clientX - rect.left;
    const y = clientY - rect.top;
    const ratio = Math.max(0, Math.min(1, x / rect.width));
    setInternalHoverRatio(ratio);
    setMousePos({ x, y, containerWidth: rect.width });
    onHoverChange?.(ratio);
  };

  const handleMouseLeave = () => {
    setInternalHoverRatio(null);
    setMousePos(null);
    onHoverChange?.(null);
  };

  if (loading && plotPointCount === 0) {
    return <ChartSkeleton ticks={RATE_TICKS} />;
  }

  if (!series?.available || plotPointCount === 0) {
    return (
      <div
        style={{
          minHeight: 150,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          textAlign: "center",
          paddingInline: 12,
          color: theme.palette.text.secondary,
        }}
      >
        <AppTypography variant="body2">{fallbackMessage}</AppTypography>
      </div>
    );
  }

  return (
    <div
      style={{
        minWidth: 0,
        width: "100%",
        flex: 1,
        display: "flex",
        flexDirection: "column",
        gap: 8,
      }}
    >
      <div style={{ display: "flex" }}>
        <div
          ref={chartRef}
          style={{ flex: 1, minWidth: 0, position: "relative" }}
          onMouseMove={(event) =>
            handlePointerMove(event.clientX, event.clientY)
          }
          onMouseLeave={handleMouseLeave}
        >
          <svg
            viewBox={`0 0 ${viewWidth} ${viewHeight}`}
            style={{ width: "100%", height: 120, display: "block" }}
            preserveAspectRatio="none"
          >
            {[0, 0.25, 0.5, 0.75, 1].map((tickRatio) => {
              const y = paddingTop + (1 - tickRatio) * innerHeight;
              return (
                <line
                  key={tickRatio}
                  x1={paddingLeft}
                  y1={y}
                  x2={paddingLeft + innerWidth}
                  y2={y}
                  stroke={alpha(theme.chart.neutral, 0.16)}
                  strokeWidth={1}
                />
              );
            })}

            <defs>
              <linearGradient
                id="history-fill-network-rx"
                x1="0"
                x2="0"
                y1="0"
                y2="1"
              >
                <stop offset="0%" stopColor={alpha(theme.chart.rx, 0.24)} />
                <stop offset="100%" stopColor={alpha(theme.chart.rx, 0.02)} />
              </linearGradient>
              <linearGradient
                id="history-fill-network-tx"
                x1="0"
                x2="0"
                y1="0"
                y2="1"
              >
                <stop offset="0%" stopColor={alpha(theme.chart.tx, 0.24)} />
                <stop offset="100%" stopColor={alpha(theme.chart.tx, 0.02)} />
              </linearGradient>
            </defs>

            {rxAreaPath && (
              <path
                d={rxAreaPath}
                fill="url(#history-fill-network-rx)"
                stroke="none"
              />
            )}
            {txAreaPath && (
              <path
                d={txAreaPath}
                fill="url(#history-fill-network-tx)"
                stroke="none"
              />
            )}
            {rxLinePath && (
              <path
                d={rxLinePath}
                fill="none"
                stroke={theme.chart.rx}
                strokeWidth={1.5}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            )}
            {txLinePath && (
              <path
                d={txLinePath}
                fill="none"
                stroke={theme.chart.tx}
                strokeWidth={1.5}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            )}
          </svg>

          {effectiveHoverRatio != null && activePoint && (
            <div
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                pointerEvents: "none",
              }}
            >
              <div
                style={{
                  position: "absolute",
                  left: `${(activePoint.x / viewWidth) * 100}%`,
                  top: paddingTop,
                  height: innerHeight,
                  width: 1,
                  borderLeft: `1px dashed ${alpha(theme.chart.neutral, 0.4)}`,
                  transform: "translateX(-50%)",
                }}
              />
              {activeRXPoint && (
                <div
                  style={{
                    position: "absolute",
                    left: `${(activeRXPoint.x / viewWidth) * 100}%`,
                    top: activeRXPoint.y,
                    transform: "translate(-50%, -50%)",
                    width: 8,
                    height: 8,
                    borderRadius: "50%",
                    backgroundColor: theme.palette.background.paper,
                    border: `2px solid ${theme.chart.rx}`,
                  }}
                />
              )}
              {activeTXPoint && (
                <div
                  style={{
                    position: "absolute",
                    left: `${(activeTXPoint.x / viewWidth) * 100}%`,
                    top: activeTXPoint.y,
                    transform: "translate(-50%, -50%)",
                    width: 8,
                    height: 8,
                    borderRadius: "50%",
                    backgroundColor: theme.palette.background.paper,
                    border: `2px solid ${theme.chart.tx}`,
                  }}
                />
              )}
            </div>
          )}

          {mousePos && effectiveHoverRatio != null && activePoint && (
            <div
              style={{
                position: "absolute",
                ...(mousePos.x > mousePos.containerWidth / 2
                  ? { right: mousePos.containerWidth - mousePos.x + 12 }
                  : { left: mousePos.x + 12 }),
                top: Math.max(0, mousePos.y - 20),
                pointerEvents: "none",
                zIndex: 10,
                backgroundColor: theme.palette.background.paper,
                border: `1px solid ${alpha(theme.chart.neutral, 0.2)}`,
                borderRadius: 6,
                padding: "4px 8px",
                display: "flex",
                flexDirection: "column",
                gap: 2,
                whiteSpace: "nowrap",
                boxShadow: "0 2px 8px rgba(0,0,0,0.3)",
              }}
            >
              <AppTypography
                variant="caption"
                fontWeight={600}
                style={{ color: theme.chart.rx }}
              >
                RX: {formatNetworkRate(activeRXPoint?.point.value ?? 0)}
              </AppTypography>
              <AppTypography
                variant="caption"
                fontWeight={600}
                style={{ color: theme.chart.tx }}
              >
                TX: {formatNetworkRate(activeTXPoint?.point.value ?? 0)}
              </AppTypography>
            </div>
          )}
        </div>
        <div
          style={{
            width: 52,
            height: 120,
            position: "relative",
            flexShrink: 0,
            pointerEvents: "none",
          }}
        >
          {[0, 0.25, 0.5, 0.75, 1].map((tickRatio) => {
            const top = paddingTop + (1 - tickRatio) * innerHeight;
            return (
              <div
                key={tickRatio}
                style={{
                  position: "absolute",
                  top,
                  right: 2,
                  transform: "translateY(-50%)",
                  fontSize: "8px",
                  lineHeight: 1,
                  color: alpha(theme.chart.neutral, 0.75),
                  whiteSpace: "nowrap",
                }}
              >
                {formatNetworkRate(tickRatio * yAxisMax)}
              </div>
            );
          })}
        </div>
      </div>

      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          gap: 8,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            flexWrap: "wrap",
          }}
        >
          <AppTypography
            variant="caption"
            fontWeight={700}
            style={{ color: theme.chart.rx }}
          >
            RX: {formatNetworkRate(activeRXPoint?.point.value ?? 0)}
          </AppTypography>
          <AppTypography
            variant="caption"
            fontWeight={700}
            style={{ color: theme.chart.tx }}
          >
            TX: {formatNetworkRate(activeTXPoint?.point.value ?? 0)}
          </AppTypography>
        </div>
        <AppTypography variant="caption" color="text.secondary" noWrap>
          {formatChartTimestamp(activePoint?.point.ts, range)}
        </AppTypography>
      </div>
    </div>
  );
};

// ─── DiskIOMonitorGraph ───────────────────────────────────────────────────────

export const DiskIOMonitorGraph: React.FC<{
  range: MonitoringRange;
  series: DiskIOMonitoringSeriesResponse | undefined;
  loading: boolean;
  emptyMessage: string;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  range,
  series,
  loading,
  emptyMessage,
  hoverRatio: externalHoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const chartRef = useRef<HTMLDivElement>(null);
  const [internalHoverRatio, setInternalHoverRatio] = useState<number | null>(
    null,
  );
  const [mousePos, setMousePos] = useState<{
    x: number;
    y: number;
    containerWidth: number;
  } | null>(null);
  const effectiveHoverRatio =
    externalHoverRatio !== undefined ? externalHoverRatio : internalHoverRatio;

  const readSeries = useMemo(
    () => (series?.available ? series.readPoints : []),
    [series],
  );
  const writeSeries = useMemo(
    () => (series?.available ? series.writePoints : []),
    [series],
  );
  const fallbackMessage = series?.reason || emptyMessage;
  const viewWidth = 220;
  const viewHeight = 120;
  const paddingTop = 8;
  const paddingRight = 0;
  const paddingBottom = 16;
  const paddingLeft = 4;
  const innerWidth = viewWidth - paddingLeft - paddingRight;
  const innerHeight = viewHeight - paddingTop - paddingBottom;
  const rangeDurationMs = RANGE_DURATION_MS[range];

  const maxValue = useMemo(() => {
    let currentMax = 0;
    for (const point of readSeries) {
      currentMax = Math.max(currentMax, point.value);
    }
    for (const point of writeSeries) {
      currentMax = Math.max(currentMax, point.value);
    }
    return Math.max(currentMax, 1);
  }, [readSeries, writeSeries]);

  const yAxisMax = Math.max(maxValue * 1.1, 1);

  const readPlotPoints = useMemo(
    () =>
      buildMonitoringPlotPoints({
        points: readSeries,
        paddingLeft,
        paddingTop,
        innerWidth,
        innerHeight,
        rangeDurationMs,
        yAxisMax,
      }),
    [
      innerHeight,
      innerWidth,
      paddingLeft,
      paddingTop,
      rangeDurationMs,
      readSeries,
      yAxisMax,
    ],
  );
  const writePlotPoints = useMemo(
    () =>
      buildMonitoringPlotPoints({
        points: writeSeries,
        paddingLeft,
        paddingTop,
        innerWidth,
        innerHeight,
        rangeDurationMs,
        yAxisMax,
      }),
    [
      innerHeight,
      innerWidth,
      paddingLeft,
      paddingTop,
      rangeDurationMs,
      writeSeries,
      yAxisMax,
    ],
  );

  const plotPointCount = Math.max(
    readPlotPoints.length,
    writePlotPoints.length,
  );
  const hoverIndex =
    effectiveHoverRatio != null && plotPointCount > 0
      ? Math.max(
          0,
          Math.min(
            plotPointCount - 1,
            Math.round(effectiveHoverRatio * (plotPointCount - 1)),
          ),
        )
      : null;

  const hoveredReadPoint =
    hoverIndex != null ? readPlotPoints[hoverIndex] : undefined;
  const hoveredWritePoint =
    hoverIndex != null ? writePlotPoints[hoverIndex] : undefined;
  const activeReadPoint = hoveredReadPoint ?? readPlotPoints.at(-1);
  const activeWritePoint = hoveredWritePoint ?? writePlotPoints.at(-1);
  const activePoint =
    hoveredReadPoint ??
    hoveredWritePoint ??
    readPlotPoints.at(-1) ??
    writePlotPoints.at(-1);

  const buildLinePath = (chartPoints: { x: number; y: number }[]): string => {
    if (chartPoints.length === 0) {
      return "";
    }

    return chartPoints
      .map(({ x, y }, index) => `${index === 0 ? "M" : "L"} ${x} ${y}`)
      .join(" ");
  };

  const buildAreaPath = (chartPoints: { x: number; y: number }[]): string => {
    if (chartPoints.length === 0) {
      return "";
    }

    const line = buildLinePath(chartPoints);
    const first = chartPoints[0];
    const last = chartPoints[chartPoints.length - 1];
    return `${line} L ${last.x} ${paddingTop + innerHeight} L ${first.x} ${paddingTop + innerHeight} Z`;
  };

  const readLinePath = buildLinePath(readPlotPoints);
  const writeLinePath = buildLinePath(writePlotPoints);
  const readAreaPath = buildAreaPath(readPlotPoints);
  const writeAreaPath = buildAreaPath(writePlotPoints);

  const handlePointerMove = (clientX: number, clientY: number) => {
    if (!chartRef.current || plotPointCount === 0) {
      return;
    }

    const rect = chartRef.current.getBoundingClientRect();
    const x = clientX - rect.left;
    const y = clientY - rect.top;
    const ratio = Math.max(0, Math.min(1, x / rect.width));
    setInternalHoverRatio(ratio);
    setMousePos({ x, y, containerWidth: rect.width });
    onHoverChange?.(ratio);
  };

  const handleMouseLeave = () => {
    setInternalHoverRatio(null);
    setMousePos(null);
    onHoverChange?.(null);
  };

  if (loading && plotPointCount === 0) {
    return <ChartSkeleton ticks={RATE_TICKS} />;
  }

  if (!series?.available || plotPointCount === 0) {
    return (
      <div
        style={{
          minHeight: 150,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          textAlign: "center",
          paddingInline: 12,
          color: theme.palette.text.secondary,
        }}
      >
        <AppTypography variant="body2">{fallbackMessage}</AppTypography>
      </div>
    );
  }

  return (
    <div
      style={{
        minWidth: 0,
        width: "100%",
        flex: 1,
        display: "flex",
        flexDirection: "column",
        gap: 8,
      }}
    >
      <div style={{ display: "flex" }}>
        <div
          ref={chartRef}
          style={{ flex: 1, minWidth: 0, position: "relative" }}
          onMouseMove={(event) =>
            handlePointerMove(event.clientX, event.clientY)
          }
          onMouseLeave={handleMouseLeave}
        >
          <svg
            viewBox={`0 0 ${viewWidth} ${viewHeight}`}
            style={{ width: "100%", height: 120, display: "block" }}
            preserveAspectRatio="none"
          >
            {[0, 0.25, 0.5, 0.75, 1].map((tickRatio) => {
              const y = paddingTop + (1 - tickRatio) * innerHeight;
              return (
                <line
                  key={tickRatio}
                  x1={paddingLeft}
                  y1={y}
                  x2={paddingLeft + innerWidth}
                  y2={y}
                  stroke={alpha(theme.chart.neutral, 0.16)}
                  strokeWidth={1}
                />
              );
            })}

            <defs>
              <linearGradient
                id="history-fill-disk-read"
                x1="0"
                x2="0"
                y1="0"
                y2="1"
              >
                <stop offset="0%" stopColor={alpha(theme.chart.rx, 0.24)} />
                <stop offset="100%" stopColor={alpha(theme.chart.rx, 0.02)} />
              </linearGradient>
              <linearGradient
                id="history-fill-disk-write"
                x1="0"
                x2="0"
                y1="0"
                y2="1"
              >
                <stop offset="0%" stopColor={alpha(theme.chart.tx, 0.24)} />
                <stop offset="100%" stopColor={alpha(theme.chart.tx, 0.02)} />
              </linearGradient>
            </defs>

            {readAreaPath && (
              <path
                d={readAreaPath}
                fill="url(#history-fill-disk-read)"
                stroke="none"
              />
            )}
            {writeAreaPath && (
              <path
                d={writeAreaPath}
                fill="url(#history-fill-disk-write)"
                stroke="none"
              />
            )}
            {readLinePath && (
              <path
                d={readLinePath}
                fill="none"
                stroke={theme.chart.rx}
                strokeWidth={1.5}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            )}
            {writeLinePath && (
              <path
                d={writeLinePath}
                fill="none"
                stroke={theme.chart.tx}
                strokeWidth={1.5}
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            )}
          </svg>

          {effectiveHoverRatio != null && activePoint && (
            <div
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                pointerEvents: "none",
              }}
            >
              <div
                style={{
                  position: "absolute",
                  left: `${(activePoint.x / viewWidth) * 100}%`,
                  top: paddingTop,
                  height: innerHeight,
                  width: 1,
                  borderLeft: `1px dashed ${alpha(theme.chart.neutral, 0.4)}`,
                  transform: "translateX(-50%)",
                }}
              />
              {activeReadPoint && (
                <div
                  style={{
                    position: "absolute",
                    left: `${(activeReadPoint.x / viewWidth) * 100}%`,
                    top: activeReadPoint.y,
                    transform: "translate(-50%, -50%)",
                    width: 8,
                    height: 8,
                    borderRadius: "50%",
                    backgroundColor: theme.palette.background.paper,
                    border: `2px solid ${theme.chart.rx}`,
                  }}
                />
              )}
              {activeWritePoint && (
                <div
                  style={{
                    position: "absolute",
                    left: `${(activeWritePoint.x / viewWidth) * 100}%`,
                    top: activeWritePoint.y,
                    transform: "translate(-50%, -50%)",
                    width: 8,
                    height: 8,
                    borderRadius: "50%",
                    backgroundColor: theme.palette.background.paper,
                    border: `2px solid ${theme.chart.tx}`,
                  }}
                />
              )}
            </div>
          )}

          {mousePos && effectiveHoverRatio != null && activePoint && (
            <div
              style={{
                position: "absolute",
                ...(mousePos.x > mousePos.containerWidth / 2
                  ? { right: mousePos.containerWidth - mousePos.x + 12 }
                  : { left: mousePos.x + 12 }),
                top: Math.max(0, mousePos.y - 20),
                pointerEvents: "none",
                zIndex: 10,
                backgroundColor: theme.palette.background.paper,
                border: `1px solid ${alpha(theme.chart.neutral, 0.2)}`,
                borderRadius: 6,
                padding: "4px 8px",
                display: "flex",
                flexDirection: "column",
                gap: 2,
                whiteSpace: "nowrap",
                boxShadow: "0 2px 8px rgba(0,0,0,0.3)",
              }}
            >
              <AppTypography
                variant="caption"
                fontWeight={600}
                style={{ color: theme.chart.rx }}
              >
                Read: {formatDiskRate(activeReadPoint?.point.value ?? 0)}
              </AppTypography>
              <AppTypography
                variant="caption"
                fontWeight={600}
                style={{ color: theme.chart.tx }}
              >
                Write: {formatDiskRate(activeWritePoint?.point.value ?? 0)}
              </AppTypography>
            </div>
          )}
        </div>
        <div
          style={{
            width: 52,
            height: 120,
            position: "relative",
            flexShrink: 0,
            pointerEvents: "none",
          }}
        >
          {[0, 0.25, 0.5, 0.75, 1].map((tickRatio) => {
            const top = paddingTop + (1 - tickRatio) * innerHeight;
            return (
              <div
                key={tickRatio}
                style={{
                  position: "absolute",
                  top,
                  right: 2,
                  transform: "translateY(-50%)",
                  fontSize: "8px",
                  lineHeight: 1,
                  color: alpha(theme.chart.neutral, 0.75),
                  whiteSpace: "nowrap",
                }}
              >
                {formatDiskRate(tickRatio * yAxisMax)}
              </div>
            );
          })}
        </div>
      </div>

      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          gap: 8,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            flexWrap: "wrap",
          }}
        >
          <AppTypography
            variant="caption"
            fontWeight={700}
            style={{ color: theme.chart.rx }}
          >
            Read: {formatDiskRate(activeReadPoint?.point.value ?? 0)}
          </AppTypography>
          <AppTypography
            variant="caption"
            fontWeight={700}
            style={{ color: theme.chart.tx }}
          >
            Write: {formatDiskRate(activeWritePoint?.point.value ?? 0)}
          </AppTypography>
        </div>
        <AppTypography variant="caption" color="text.secondary" noWrap>
          {formatChartTimestamp(activePoint?.point.ts, range)}
        </AppTypography>
      </div>
    </div>
  );
};
