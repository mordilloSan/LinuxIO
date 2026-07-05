import React, { useEffect, useEffectEvent, useRef } from "react";
import { SmoothieChart } from "smoothie";

import { linuxio } from "@/api";
import LiveChartHover from "@/components/charts/LiveChartHover";
import {
  appendLiveSample,
  LIVE_MILLIS_PER_PIXEL,
  sampleLiveSeries,
} from "@/components/charts/liveSeriesStore";
import type { LiveTooltipRow } from "@/components/charts/liveTooltip";
import {
  type LiveSeriesPoint,
  useLiveSeries,
} from "@/components/charts/useLiveSeries";
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

const STREAM_DELAY_MS = 1000;

const NetworkGraph: React.FC<NetworkGraphProps> = ({
  interfaceName,
  rx,
  tx,
}) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const rxId = `network:rx:${interfaceName}`;
  const txId = `network:tx:${interfaceName}`;
  // History arrives in bytes/s; the chart series (like the rx/tx props) are
  // kB/s.
  const [rxSeries, txSeries] = useLiveSeries([rxId, txId], async (request) => {
    const points = await linuxio.monitoring.get_network_history(request);
    const rxPoints: LiveSeriesPoint[] = [];
    const txPoints: LiveSeriesPoint[] = [];
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
    return { [rxId]: rxPoints, [txId]: txPoints };
  });
  const rxColor = theme.chart.rx;
  const txColor = theme.chart.tx;
  const chartNeutral = theme.chart.neutral;

  const appendLatestTraffic = useEffectEvent(() => {
    appendLiveSample(rxId, rx);
    appendLiveSample(txId, tx);
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
        strokeStyle: alpha(chartNeutral, 0.15),
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      responsive: true,
      minValue: 0,
      maxValueScale: 1.15,
    });

    chart.addTimeSeries(rxSeries, {
      strokeStyle: rxColor,
      fillStyle: alpha(rxColor, 0.09),
      lineWidth: 2,
    });
    chart.addTimeSeries(txSeries, {
      strokeStyle: txColor,
      fillStyle: alpha(txColor, 0.09),
      lineWidth: 2,
    });

    chart.streamTo(canvas, STREAM_DELAY_MS);

    const intervalId = setInterval(() => {
      appendLatestTraffic();
    }, 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
    };
  }, [chartNeutral, rxColor, txColor, rxSeries, txSeries]);

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
          delayMs={STREAM_DELAY_MS}
          rowsAt={(t) => {
            const rows: LiveTooltipRow[] = [];
            const rxValue = sampleLiveSeries(rxSeries, t);
            if (rxValue !== null) {
              rows.push({
                color: rxColor,
                value: formatThroughput(rxValue * 1024),
                label: "Rx",
              });
            }
            const txValue = sampleLiveSeries(txSeries, t);
            if (txValue !== null) {
              rows.push({
                color: txColor,
                value: formatThroughput(txValue * 1024),
                label: "Tx",
              });
            }
            return rows;
          }}
        />
      </div>
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
