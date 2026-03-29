import { Icon } from "@iconify/react";
import React, { useMemo, useRef, useState } from "react";

import type {
  GpuDevice,
  MonitoringRange,
  MonitoringSeriesPoint,
  MonitoringSeriesResponse,
} from "@/api";
import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { cardHeight } from "@/constants";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatFileSize } from "@/utils/formaters";
import { formatGpuPercent, getGpuType } from "@/utils/gpu";

const RANGE_OPTIONS: { value: MonitoringRange; label: string }[] = [
  { value: "1m", label: "1m" },
  { value: "5m", label: "5m" },
  { value: "15m", label: "15m" },
  { value: "60m", label: "60m" },
  { value: "6h", label: "6h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
];

const RANGE_DURATION_MS: Record<MonitoringRange, number> = {
  "1m": 60 * 1000,
  "5m": 5 * 60 * 1000,
  "15m": 15 * 60 * 1000,
  "60m": 60 * 60 * 1000,
  "6h": 6 * 60 * 60 * 1000,
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
  "30d": 30 * 24 * 60 * 60 * 1000,
};

type SummaryRow = {
  label: string;
  value: React.ReactNode;
};

const clampPercent = (value: number) => Math.max(0, Math.min(100, value));

const formatPercent = (value?: number | null): string =>
  typeof value === "number" && Number.isFinite(value)
    ? `${Math.round(value)}%`
    : "—";

const getLatestPoint = (
  series: MonitoringSeriesResponse | undefined,
): MonitoringSeriesPoint | undefined => series?.points.at(-1);

const getPeakPointValue = (
  series: MonitoringSeriesResponse | undefined,
): number | undefined => {
  if (!series?.available || series.points.length === 0) {
    return undefined;
  }
  return series.points.reduce((peak, point) => Math.max(peak, point.value), 0);
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

const SummaryRowsList: React.FC<{ rows: SummaryRow[] }> = ({ rows }) => {
  const theme = useAppTheme();

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "stretch",
        width: "100%",
      }}
    >
      {rows.map(({ label, value }, index) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "space-between",
            gap: theme.spacing(1),
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
          }}
        >
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </AppTypography>
          <div
            style={{
              minWidth: 0,
              display: "flex",
              justifyContent: "flex-end",
              alignItems: "center",
            }}
          >
            {typeof value === "string" ? (
              <AppTypography variant="body2" fontWeight={500} noWrap>
                {value}
              </AppTypography>
            ) : (
              value
            )}
          </div>
        </div>
      ))}
    </div>
  );
};

const RangeDropdown: React.FC<{
  value: MonitoringRange;
  onChange: (value: MonitoringRange) => void;
  color: string;
}> = ({ value, onChange }) => {
  const theme = useAppTheme();

  return (
    <AppSelect
      size="small"
      variant="standard"
      disableUnderline
      value={value}
      onChange={(event) => onChange(event.target.value as MonitoringRange)}
      style={{
        ["--app-select-input-font-size" as string]: "0.68rem",
        marginLeft: 0,
        color: theme.palette.text.secondary,
        fontSize: "0.75rem",
        lineHeight: theme.typography.body2.lineHeight,
      }}
    >
      {RANGE_OPTIONS.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </AppSelect>
  );
};

const HistoryChart: React.FC<{
  color: string;
  label: string;
  range: MonitoringRange;
  series: MonitoringSeriesResponse | undefined;
  loading: boolean;
  emptyMessage: string;
}> = ({ color, label, range, series, loading, emptyMessage }) => {
  const theme = useAppTheme();
  const chartRef = useRef<HTMLDivElement>(null);
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);

  const points = useMemo(
    () => (series?.available ? series.points : []),
    [series],
  );
  const latestPoint = points.at(-1);
  const fallbackMessage = series?.reason || emptyMessage;
  const viewWidth = 220;
  const viewHeight = 120;
  const paddingTop = 8;
  const paddingRight = 28;
  const paddingBottom = 16;
  const paddingLeft = 4;
  const innerWidth = viewWidth - paddingLeft - paddingRight;
  const innerHeight = viewHeight - paddingTop - paddingBottom;
  const rangeDurationMs = RANGE_DURATION_MS[range];

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

  const hoveredPoint =
    hoverIndex != null && hoverIndex >= 0 ? plotPoints[hoverIndex] : undefined;
  const activePoint = hoveredPoint ?? plotPoints.at(-1);

  const linePath = useMemo(() => {
    if (plotPoints.length === 0) {
      return "";
    }
    return plotPoints
      .map(({ x, y }, index) => `${index === 0 ? "M" : "L"} ${x} ${y}`)
      .join(" ");
  }, [plotPoints]);

  const areaPath = useMemo(() => {
    if (plotPoints.length === 0) {
      return "";
    }
    const first = plotPoints[0];
    const last = plotPoints[plotPoints.length - 1];
    return `${linePath} L ${last.x} ${paddingTop + innerHeight} L ${first.x} ${paddingTop + innerHeight} Z`;
  }, [innerHeight, linePath, plotPoints]);

  const handlePointerMove = (clientX: number) => {
    if (!chartRef.current || plotPoints.length === 0) {
      return;
    }
    const rect = chartRef.current.getBoundingClientRect();
    const ratio = (clientX - rect.left) / rect.width;
    const nextIndex = Math.round(
      (clampPercent(ratio * 100) / 100) * (plotPoints.length - 1),
    );
    setHoverIndex(Math.max(0, Math.min(plotPoints.length - 1, nextIndex)));
  };

  const handleMouseLeave = () => {
    setHoverIndex(null);
  };

  if (loading && points.length === 0) {
    return (
      <div
        style={{
          minHeight: 150,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color: theme.palette.text.secondary,
        }}
      >
        <AppTypography variant="body2">Loading history...</AppTypography>
      </div>
    );
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
      <div
        ref={chartRef}
        style={{ width: "100%", minWidth: 0 }}
        onMouseMove={(event) => handlePointerMove(event.clientX)}
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
              <g key={tick}>
                <line
                  x1={paddingLeft}
                  y1={y}
                  x2={paddingLeft + innerWidth}
                  y2={y}
                  stroke={alpha(theme.chart.neutral, 0.16)}
                  strokeWidth={1}
                />
                <text
                  x={viewWidth - 2}
                  y={y + 3}
                  textAnchor="end"
                  fontSize="8"
                  fill={alpha(theme.chart.neutral, 0.75)}
                >
                  {tick}%
                </text>
              </g>
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
          </defs>

          {areaPath && (
            <path
              d={areaPath}
              fill={`url(#history-fill-${label})`}
              stroke="none"
            />
          )}
          {linePath && (
            <path
              d={linePath}
              fill="none"
              stroke={color}
              strokeWidth={2}
              strokeLinejoin="round"
              strokeLinecap="round"
            />
          )}

          {activePoint && (
            <>
              <line
                x1={activePoint.x}
                y1={paddingTop}
                x2={activePoint.x}
                y2={paddingTop + innerHeight}
                stroke={alpha(color, 0.4)}
                strokeWidth={1}
                strokeDasharray="3 3"
              />
              <circle
                cx={activePoint.x}
                cy={activePoint.y}
                r={4}
                fill={theme.palette.background.paper}
                stroke={color}
                strokeWidth={2}
              />
            </>
          )}
        </svg>
      </div>

      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          gap: 8,
        }}
      >
        <AppTypography variant="body2" fontWeight={700}>
          {label}:{" "}
          {formatPercent(activePoint?.point.value ?? latestPoint?.value)}
        </AppTypography>
        <AppTypography variant="caption" color="text.secondary" noWrap>
          {formatChartTimestamp(
            activePoint?.point.ts ?? latestPoint?.ts,
            range,
          )}
        </AppTypography>
      </div>
    </div>
  );
};

const HistoryCardShell: React.FC<{
  title: string;
  avatarIcon: string;
  accentColor: string;
  range: MonitoringRange;
  onRangeChange: (value: MonitoringRange) => void;
  rows: SummaryRow[];
  chart: React.ReactNode;
}> = ({
  title,
  avatarIcon,
  accentColor,
  range,
  onRangeChange,
  rows,
  chart,
}) => {
  return (
    <FrostedCard
      style={{
        minHeight: cardHeight,
        display: "flex",
        flexDirection: "column",
        padding: 16,
      }}
      hoverLift
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          gap: 12,
          marginBottom: 12,
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
          <AppTypography variant="h5" fontWeight={700}>
            {title}
          </AppTypography>
          <RangeDropdown
            value={range}
            onChange={onRangeChange}
            color={accentColor}
          />
        </div>
        <Icon icon={avatarIcon} width={28} height={28} color={accentColor} />
      </div>

      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 16,
          flex: 1,
        }}
      >
        <div
          style={{
            flex: "0.85 1 160px",
            minWidth: 0,
            display: "flex",
            alignItems: "stretch",
          }}
        >
          <SummaryRowsList rows={rows} />
        </div>
        <div
          style={{
            flex: "1.75 1 280px",
            minWidth: 0,
            display: "flex",
            alignItems: "stretch",
          }}
        >
          {chart}
        </div>
      </div>
    </FrostedCard>
  );
};

export const CPUHistoryCard: React.FC = () => {
  const theme = useAppTheme();
  const [range, setRange] = useState<MonitoringRange>("1m");
  const { data: cpuInfo } = linuxio.system.get_cpu_info.useQuery({
    refetchInterval: 5_000,
  });
  const { data: series, isPending } =
    linuxio.monitoring.get_cpu_series.useQuery(range, {
      refetchInterval: 5_000,
    });

  const averageCpuUsage = cpuInfo?.perCoreUsage?.length
    ? cpuInfo.perCoreUsage.reduce((sum, cpu) => sum + cpu, 0) /
      cpuInfo.perCoreUsage.length
    : 0;
  const latestUsage = getLatestPoint(series)?.value ?? averageCpuUsage;
  const peakUsage =
    getPeakPointValue(series) ??
    (cpuInfo?.perCoreUsage?.length
      ? Math.max(...cpuInfo.perCoreUsage)
      : undefined);

  return (
    <HistoryCardShell
      title="Processor"
      avatarIcon="ph:cpu"
      accentColor={theme.palette.primary.main}
      range={range}
      onRangeChange={setRange}
      rows={[
        { label: "CPU", value: cpuInfo?.modelName ?? "—" },
        {
          label: "Cores",
          value: cpuInfo ? `${cpuInfo.cores} Threads` : "—",
        },
        { label: "Latest", value: formatPercent(latestUsage) },
        { label: "Peak", value: formatPercent(peakUsage) },
      ]}
      chart={
        <HistoryChart
          color={theme.palette.primary.main}
          label="CPU"
          range={range}
          series={series}
          loading={isPending}
          emptyMessage="CPU history is not available yet."
        />
      }
    />
  );
};

export const MemoryHistoryCard: React.FC = () => {
  const theme = useAppTheme();
  const [range, setRange] = useState<MonitoringRange>("1m");
  const { data: memoryData } = linuxio.system.get_memory_info.useQuery({
    refetchInterval: 5_000,
  });
  const { data: series, isPending } =
    linuxio.monitoring.get_memory_series.useQuery(range, {
      refetchInterval: 5_000,
    });

  const swapUsed =
    (memoryData?.system?.swapTotal ?? 0) - (memoryData?.system?.swapFree ?? 0);

  return (
    <HistoryCardShell
      title="Memory Usage"
      avatarIcon="la:memory"
      accentColor={theme.palette.warning.main}
      range={range}
      onRangeChange={setRange}
      rows={[
        {
          label: "Total",
          value: formatFileSize(memoryData?.system?.total ?? 0, 2),
        },
        {
          label: "Used",
          value: formatFileSize(memoryData?.system?.active ?? 0, 2),
        },
        {
          label: "Docker",
          value: formatFileSize(memoryData?.docker?.used ?? 0, 2),
        },
        {
          label: "Swap",
          value: `${formatFileSize(swapUsed, 2)}/${formatFileSize(memoryData?.system?.swapTotal ?? 0, 2)}`,
        },
      ]}
      chart={
        <HistoryChart
          color={theme.palette.warning.main}
          label="Memory"
          range={range}
          series={series}
          loading={isPending}
          emptyMessage="Memory history is not available yet."
        />
      }
    />
  );
};

const gpuSummaryRows = (
  gpu: GpuDevice | undefined,
  gpuCount: number,
): SummaryRow[] => {
  if (!gpu) {
    return [
      { label: "Status", value: "No GPU detected" },
      { label: "History", value: "Unavailable" },
    ];
  }

  return [
    { label: "GPU", value: gpu.model || "—" },
    { label: "Type", value: `${gpu.vendor} • ${getGpuType(gpu)}` },
    {
      label: "Devices",
      value: gpuCount > 1 ? `${gpuCount} GPUs` : "1 GPU",
    },
    {
      label: "Load",
      value: formatGpuPercent(gpu.utilization_percent),
    },
  ];
};

export const GPUHistoryCard: React.FC = () => {
  const theme = useAppTheme();
  const [range, setRange] = useState<MonitoringRange>("1m");
  const { data: gpus } = linuxio.system.get_gpu_info.useQuery({
    refetchInterval: 5_000,
  });
  const { data: series, isPending } =
    linuxio.monitoring.get_gpu_series.useQuery(range, {
      refetchInterval: 5_000,
    });

  const primaryGpu = gpus?.[0];

  return (
    <HistoryCardShell
      title="GPU"
      avatarIcon="bi:gpu-card"
      accentColor={theme.palette.primary.main}
      range={range}
      onRangeChange={setRange}
      rows={gpuSummaryRows(primaryGpu, gpus?.length ?? 0)}
      chart={
        <HistoryChart
          color={theme.palette.primary.main}
          label="GPU"
          range={range}
          series={series}
          loading={isPending}
          emptyMessage="Historical GPU data is not available on this host yet."
        />
      }
    />
  );
};
