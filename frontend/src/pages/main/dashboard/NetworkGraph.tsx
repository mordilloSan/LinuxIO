import React, { useEffect, useEffectEvent, useRef, useState } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

import { linuxio } from "@/api";
import {
  acquireLiveSeries,
  appendLiveSample,
  backfillLiveSeries,
  LIVE_BACKFILL_WINDOW_MS,
  LIVE_MILLIS_PER_PIXEL,
  LIVE_STALE_AFTER_MS,
} from "@/components/charts/liveSeriesStore";
import SmoothieCanvas from "@/components/charts/SmoothieCanvas";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatThroughput } from "@/utils/formaters";

interface NetworkGraphProps {
  interfaceName: string;
  /** kB/s */
  rx: number;
  /** kB/s */
  tx: number;
}

const NetworkGraph: React.FC<NetworkGraphProps> = ({
  interfaceName,
  rx,
  tx,
}) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const rxId = `network:rx:${interfaceName}`;
  const txId = `network:tx:${interfaceName}`;
  const [rxHandle] = useState(() =>
    acquireLiveSeries(rxId, LIVE_STALE_AFTER_MS),
  );
  const [txHandle] = useState(() =>
    acquireLiveSeries(txId, LIVE_STALE_AFTER_MS),
  );
  const { isEnabled: monitoringEnabled } = useCapability("monitoringAvailable");
  const rxColor = theme.chart.rx;
  const txColor = theme.chart.tx;
  const chartNeutral = theme.chart.neutral;

  const appendLatestTraffic = useEffectEvent(() => {
    appendLiveSample(rxId, rx);
    appendLiveSample(txId, tx);
  });

  // Seed empty buffers from the agent's per-interface history so a refresh
  // doesn't start the chart blank. Values arrive in bytes/s; the chart series
  // (like the rx/tx props) are kB/s.
  const shouldBackfill =
    (rxHandle.needsBackfill || txHandle.needsBackfill) && monitoringEnabled;
  useEffect(() => {
    if (!shouldBackfill) return;
    let cancelled = false;
    linuxio.monitoring
      .get_network_history({
        resolution: "1m",
        from_ms: Date.now() - LIVE_BACKFILL_WINDOW_MS,
        limit: 40,
      })
      .then((points) => {
        if (cancelled) return;
        const rxPoints: { t: number; v: number }[] = [];
        const txPoints: { t: number; v: number }[] = [];
        for (const point of points) {
          const rates = point.interfaces?.[interfaceName];
          if (!rates) continue;
          rxPoints.push({
            t: point.captured_at_ms,
            v: rates.recv_bytes_per_sec / 1024,
          });
          txPoints.push({
            t: point.captured_at_ms,
            v: rates.sent_bytes_per_sec / 1024,
          });
        }
        backfillLiveSeries(rxId, rxPoints);
        backfillLiveSeries(txId, txPoints);
      })
      .catch(() => {
        // Best-effort seed; live samples still stream in.
      });
    return () => {
      cancelled = true;
    };
  }, [shouldBackfill, interfaceName, rxId, txId]);

  // Initialize chart once on mount
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: LIVE_MILLIS_PER_PIXEL,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: alpha(chartNeutral, 0.15),
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      tooltip: true,
      tooltipLine: { strokeStyle: alpha(chartNeutral, 0.4), lineWidth: 1 },
      tooltipFormatter: (
        _timestamp: number,
        data: { series: TimeSeries; index: number; value: number }[],
      ) => {
        const labels = ["Rx", "Tx"];
        const colors = [rxColor, txColor];
        return data
          .map(
            (d, i) =>
              `<span style="color:${colors[i]}; font-size: 13px; line-height: 1.3;">${labels[i]}: ${formatThroughput(d.value * 1024)}</span>`,
          )
          .join("<br/>");
      },
      responsive: true,
      minValue: 0,
      maxValueScale: 1.15,
    });

    chart.addTimeSeries(rxHandle.series, {
      strokeStyle: rxColor,
      fillStyle: alpha(rxColor, 0.09),
      lineWidth: 2,
    });
    chart.addTimeSeries(txHandle.series, {
      strokeStyle: txColor,
      fillStyle: alpha(txColor, 0.09),
      lineWidth: 2,
    });

    chart.streamTo(canvas, 1000);
    chartRef.current = chart;

    const intervalId = setInterval(() => {
      appendLatestTraffic();
    }, 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
    };
  }, [chartNeutral, rxColor, txColor, rxHandle.series, txHandle.series]);

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
      <SmoothieCanvas
        chartRef={chartRef}
        ref={canvasRef}
        style={{ width: "100%", flex: 1, minHeight: 0 }}
      />
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginTop: 4,
          fontSize: 12,
          whiteSpace: "nowrap",
        }}
      >
        <div style={{ color: rxColor, fontWeight: 600 }}>
          Rx: {formatThroughput(rx * 1024)}
        </div>
        <div style={{ color: txColor, fontWeight: 600 }}>
          Tx: {formatThroughput(tx * 1024)}
        </div>
      </div>
    </div>
  );
};

export default NetworkGraph;
