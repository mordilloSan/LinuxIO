import { useEffect, useEffectEvent, useRef } from "react";
import { SmoothieChart } from "smoothie";

import LiveChartHover from "@/components/charts/LiveChartHover";
import {
  appendLiveSample,
  sampleLiveSeries,
} from "@/components/charts/liveSeriesStore";
import type { LiveTooltipRow } from "@/components/charts/liveTooltip";
import { useLiveSeries } from "@/components/charts/useLiveSeries";
import { LIVE_MILLIS_PER_PIXEL } from "@/constants/liveCharts";
import { useAppTheme } from "@/theme";
import { alpha } from "@/theme/colorUtils";

/** Which theme colour draws a series; the canvas needs the resolved value. */
type LiveChartColor = "rx" | "tx" | "primary";

export interface LiveChartSeries {
  color: LiveChartColor;
  /** Formats a sample for the tooltip and the legend. */
  format: (value: number) => string;
  /** Key of the persistent buffer in the live series store. */
  id: string;
  /** Tooltip and legend label. */
  label?: string;
  /** Latest reading; sampled into the buffer once a second. */
  value: number;
}

interface LiveChartProps {
  /** Seeds empty buffers; see `useLiveSeries`. */
  fetchHistory: Parameters<typeof useLiveSeries>[1];
  /** Show a "label: value" line per series under the canvas. */
  legend?: boolean;
  /** Fixed y-axis ceiling; omit to scale to the data. */
  maxValue?: number;
  series: readonly LiveChartSeries[];
  /** Smoothie's render delay; the hover overlay needs it to map pixels to time. */
  streamDelayMs: number;
}

/**
 * Smoothie live chart that owns the whole lifecycle: persistent series from the
 * store, history backfill, the once-a-second sampler, the chart itself and the
 * shared hover overlay. Callers only describe the series.
 */
const LiveChart = ({
  fetchHistory,
  legend = false,
  maxValue,
  series,
  streamDelayMs,
}: LiveChartProps) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const seriesList = useLiveSeries(
    series.map((entry) => entry.id),
    fetchHistory,
  );
  const colors = series.map((entry) =>
    entry.color === "primary"
      ? theme.palette.primary.main
      : theme.chart[entry.color],
  );
  // Keyed on content, like the series ids, so the chart is rebuilt only when a
  // colour actually changes and not on every render's fresh array.
  const colorsKey = colors.join("|");
  const neutral = theme.chart.neutral;

  const sampleLatest = useEffectEvent(() => {
    for (const entry of series) appendLiveSample(entry.id, entry.value);
  });

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: LIVE_MILLIS_PER_PIXEL,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: alpha(neutral, 0.15),
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      responsive: true,
      minValue: 0,
      ...(maxValue === undefined ? { maxValueScale: 1.15 } : { maxValue }),
    });

    const seriesColors = colorsKey.split("|");
    seriesList.forEach((timeSeries, index) => {
      chart.addTimeSeries(timeSeries, {
        strokeStyle: seriesColors[index],
        fillStyle: alpha(seriesColors[index], 0.09),
        lineWidth: 2,
      });
    });

    chart.streamTo(canvas, streamDelayMs);
    const intervalId = setInterval(() => sampleLatest(), 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
      for (const timeSeries of seriesList) chart.removeTimeSeries(timeSeries);
    };
  }, [colorsKey, maxValue, neutral, seriesList, streamDelayMs]);

  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        position: "relative",
        minWidth: 0,
        display: "flex",
        flexDirection: "column",
      }}
    >
      <div
        style={{ width: "100%", flex: 1, minHeight: 0, position: "relative" }}
      >
        <canvas
          ref={canvasRef}
          style={{ width: "100%", height: "100%", display: "block" }}
        />
        <LiveChartHover
          delayMs={streamDelayMs}
          rowsAt={(t) => {
            const rows: LiveTooltipRow[] = [];
            series.forEach((entry, index) => {
              const value = sampleLiveSeries(seriesList[index], t);
              if (value === null) return;
              rows.push({
                color: colors[index],
                value: entry.format(value),
                label: entry.label,
              });
            });
            return rows;
          }}
        />
      </div>
      {legend && (
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            marginTop: 4,
            fontSize: 12,
            whiteSpace: "nowrap",
            gap: 8,
          }}
        >
          {series.map((entry, index) => (
            <div
              key={entry.id}
              style={{ color: colors[index], fontWeight: 600 }}
            >
              {entry.label}: {entry.format(entry.value)}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default LiveChart;
