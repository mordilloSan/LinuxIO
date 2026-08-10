import { useEffect, useRef } from "react";
import { SmoothieChart, type TimeSeries } from "smoothie";

import LiveChartHover from "@/components/charts/LiveChartHover";
import { sampleLiveSeries } from "@/components/charts/liveSeriesStore";
import { LIVE_MILLIS_PER_PIXEL } from "@/constants/liveCharts";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatThroughput } from "@/utils/formaters";

export const STREAM_DELAY_MS = 1000;

interface NetworkTrafficGraphProps {
  color: string;
  label: string;
  /**
   * Persistent buffer from the live series store, in kB/s. The owner feeds it
   * (history backfill plus live samples) so the chart survives remounts.
   */
  series: TimeSeries;
}

const NetworkTrafficGraph = ({
  color,
  label,
  series,
}: NetworkTrafficGraphProps) => {
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
        verticalSections: 3,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      responsive: true,
      minValue: 0,
      maxValueScale: 1.15,
    });

    chart.addTimeSeries(series, {
      strokeStyle: color,
      fillStyle: alpha(color, 0.09),
      lineWidth: 1.5,
    });

    chart.streamTo(canvas, STREAM_DELAY_MS);

    return () => chart.stop();
  }, [chartNeutral, color, series]);

  return (
    <div style={{ width: "100%", height: "100%", position: "relative" }}>
      <canvas
        ref={canvasRef}
        style={{ width: "100%", height: "100%", display: "block" }}
      />
      <LiveChartHover
        delayMs={STREAM_DELAY_MS}
        rowsAt={(t) => {
          const value = sampleLiveSeries(series, t);
          if (value === null) return [];
          return [{ color, value: formatThroughput(value * 1024), label }];
        }}
      />
    </div>
  );
};

export default NetworkTrafficGraph;
