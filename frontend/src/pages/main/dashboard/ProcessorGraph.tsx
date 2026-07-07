import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useEffectEvent } from "react";
import { SmoothieChart } from "smoothie";

import { CACHE_TTL_MS, linuxio } from "@/api";
import LiveChartHover from "@/components/charts/LiveChartHover";
import {
  appendLiveSample,
  LIVE_MILLIS_PER_PIXEL,
  sampleLiveSeries,
} from "@/components/charts/liveSeriesStore";
import { useLiveSeries } from "@/components/charts/useLiveSeries";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface CpuGraphProps {
  usage: number;
}

const SERIES_ID = "cpu:usage";
const STREAM_DELAY_MS = 2000;

const CpuGraph: React.FC<CpuGraphProps> = ({ usage }) => {
  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const queryClient = useQueryClient();
  const [series] = useLiveSeries([SERIES_ID], async (request) => {
    // One-shot backfill: the request carries a rolling from_ms, so caching
    // the entry would only pollute the cache.
    const points = await queryClient.fetchQuery(
      linuxio.monitoring.get_cpu_history.queryOptions(request, {
        staleTime: CACHE_TTL_MS.NONE,
        gcTime: CACHE_TTL_MS.NONE,
      }),
    );
    return {
      [SERIES_ID]: points.map((point) => ({
        t: point.captured_at_ms,
        v: point.usage_percent,
      })),
    };
  });
  const theme = useAppTheme();
  const color = theme.palette.primary.main;
  const neutral = theme.chart.neutral;

  const appendLatestUsage = useEffectEvent(() => {
    appendLiveSample(SERIES_ID, usage);
  });

  // Initialize chart once on mount
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
      maxValue: 100,
    });

    chart.addTimeSeries(series, {
      strokeStyle: color,
      fillStyle: `${color}18`,
      lineWidth: 2,
    });

    chart.streamTo(canvas, STREAM_DELAY_MS);

    const intervalId = setInterval(() => {
      appendLatestUsage();
    }, 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
    };
  }, [color, neutral, series]);

  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        minWidth: 0,
        position: "relative",
      }}
    >
      <div
        style={{
          width: "100%",
          minWidth: 0,
          height: "100%",
          position: "relative",
        }}
      >
        <canvas
          ref={canvasRef}
          style={{ width: "100%", height: "100%", display: "block" }}
        />
        <LiveChartHover
          delayMs={STREAM_DELAY_MS}
          rowsAt={(t) => {
            const value = sampleLiveSeries(series, t);
            return value === null
              ? []
              : [{ color, value: `${value.toFixed(1)}%` }];
          }}
        />
      </div>
    </div>
  );
};

export default CpuGraph;
