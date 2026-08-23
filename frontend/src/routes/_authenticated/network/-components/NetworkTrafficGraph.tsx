import { useEffect, useRef } from "react";
import { SmoothieChart, type TimeSeries } from "smoothie";

import LiveChartHover from "@/components/charts/LiveChartHover";
import { sampleLiveSeries } from "@/components/charts/liveSeriesStore";
import { LIVE_MILLIS_PER_PIXEL } from "@/constants/liveCharts";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatThroughput } from "@/utils/formaters";

export const STREAM_DELAY_MS = 1000;

/**
 * Half-range floor, in kB/s. An idle interface reads zero in both directions,
 * and a mirrored range of ±0 is degenerate: every sample lands on the zero
 * line and the scale animation has nothing to converge on.
 */
const MIN_HALF_RANGE_KB_PER_SEC = 1;

/**
 * Received traffic is fed in negative so the two directions mirror across
 * zero. Smoothie scales to the data's own extremes, which parks the zero line
 * against whichever edge the quieter direction sits on — a 20 kB/s upload
 * beside a 200 kB/s download leaves zero near the top of the canvas. Widening
 * the quieter half to match the busier one holds zero on the centre line.
 */
export const centerZeroRange = ({ max, min }: { max: number; min: number }) => {
  const halfRange = Math.max(
    Number.isFinite(min) ? Math.abs(min) : 0,
    Number.isFinite(max) ? Math.abs(max) : 0,
    MIN_HALF_RANGE_KB_PER_SEC,
  );
  return { max: halfRange, min: -halfRange };
};

export interface NetworkTrafficSeries {
  color: string;
  label: string;
  /**
   * Persistent buffer from the live series store, in kB/s. The owner feeds it
   * (history backfill plus live samples) so the chart survives remounts.
   */
  series: TimeSeries;
}

interface NetworkTrafficGraphProps {
  series: NetworkTrafficSeries[];
}

const NetworkTrafficGraph = ({ series }: NetworkTrafficGraphProps) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartNeutral = theme.chart.neutral;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: LIVE_MILLIS_PER_PIXEL,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: alpha(chartNeutral, 0.08),
        // Even sections put a grid line on the zero line the mirrored
        // series share, rather than astride it.
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      responsive: true,
      horizontalLines: [
        {
          value: 0,
          color: alpha(chartNeutral, 0.35),
          lineWidth: 1,
        },
      ],
      minValueScale: 1.15,
      maxValueScale: 1.15,
      yRangeFunction: centerZeroRange,
    });

    for (const entry of series) {
      // No fill: smoothie fills from the line to the bottom of the canvas,
      // not to zero, so the positive series would tint the whole negative
      // half and read as traffic that isn't there.
      chart.addTimeSeries(entry.series, {
        strokeStyle: entry.color,
        lineWidth: 1.5,
      });
    }

    chart.streamTo(canvas, STREAM_DELAY_MS);

    return () => chart.stop();
  }, [chartNeutral, series]);

  return (
    <div style={{ width: "100%", height: "100%", position: "relative" }}>
      {/* Absolute so the canvas stays out of intrinsic sizing. Smoothie's
          responsive mode writes the measured height back onto the `height`
          attribute, and a percentage height against an indefinite parent
          falls back to that attribute — an in-flow canvas therefore reports
          its own last size as a content floor and the grid row it sits in can
          only ever grow. */}
      <canvas
        ref={canvasRef}
        style={{
          position: "absolute",
          inset: 0,
          width: "100%",
          height: "100%",
          display: "block",
        }}
      />
      <LiveChartHover
        delayMs={STREAM_DELAY_MS}
        rowsAt={(t) => {
          return series.flatMap((entry) => {
            const value = sampleLiveSeries(entry.series, t);
            if (value === null) return [];
            const prefix = value < 0 ? "−" : "+";
            return [
              {
                color: entry.color,
                value: `${prefix}${formatThroughput(Math.abs(value) * 1024)}`,
                label: entry.label,
              },
            ];
          });
        }}
      />
    </div>
  );
};

export default NetworkTrafficGraph;
