import React, { useEffect, useEffectEvent, useState } from "react";
import { SmoothieChart } from "smoothie";

import { linuxio } from "@/api";
import LiveChartHover from "@/components/charts/LiveChartHover";
import {
  acquireLiveSeries,
  appendLiveSample,
  backfillLiveSeries,
  LIVE_BACKFILL_WINDOW_MS,
  LIVE_MILLIS_PER_PIXEL,
  LIVE_STALE_AFTER_MS,
  sampleLiveSeries,
} from "@/components/charts/liveSeriesStore";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface CpuGraphProps {
  usage: number;
}

const SERIES_ID = "cpu:usage";
const STREAM_DELAY_MS = 2000;

const CpuGraph: React.FC<CpuGraphProps> = ({ usage }) => {
  const canvasRef = React.useRef<HTMLCanvasElement>(null);
  const [{ series, needsBackfill }] = useState(() =>
    acquireLiveSeries(SERIES_ID, LIVE_STALE_AFTER_MS),
  );
  const { isEnabled: monitoringEnabled } = useCapability("monitoringAvailable");
  const theme = useAppTheme();
  const color = theme.palette.primary.main;
  const neutral = theme.chart.neutral;

  const appendLatestUsage = useEffectEvent(() => {
    appendLiveSample(SERIES_ID, usage);
  });

  // Seed the empty buffer with the agent's recent samples so a refresh or a
  // long absence doesn't start the chart from a blank canvas.
  const shouldBackfill = needsBackfill && monitoringEnabled;
  useEffect(() => {
    if (!shouldBackfill) return;
    let cancelled = false;
    linuxio.monitoring
      .get_cpu_history({
        resolution: "1m",
        from_ms: Date.now() - LIVE_BACKFILL_WINDOW_MS,
        limit: 40,
      })
      .then((points) => {
        if (cancelled) return;
        backfillLiveSeries(
          SERIES_ID,
          points.map((point) => ({
            t: point.captured_at_ms,
            v: point.usage_percent,
          })),
        );
      })
      .catch(() => {
        // Best-effort seed; live samples still stream in.
      });
    return () => {
      cancelled = true;
    };
  }, [shouldBackfill]);

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
      <div
        style={{
          position: "absolute",
          top: 0,
          right: 0,
          bottom: 0,
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          fontSize: 9,
          color: alpha(theme.chart.neutral, 0.7),
          whiteSpace: "nowrap",
          pointerEvents: "none",
          textAlign: "right",
        }}
      >
        <span>100%</span>
        <span>75%</span>
        <span>50%</span>
        <span>25%</span>
        <span>0%</span>
      </div>
    </div>
  );
};

export default CpuGraph;
