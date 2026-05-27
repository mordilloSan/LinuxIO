import React, { useEffect, useEffectEvent, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

import SmoothieCanvas from "@/components/charts/SmoothieCanvas";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatThroughput } from "@/utils/formaters";

interface NetworkGraphProps {
  rx: number;
  tx: number;
}

const NetworkGraph: React.FC<NetworkGraphProps> = ({ rx, tx }) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const rxSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const txSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const rxColor = theme.chart.rx;
  const txColor = theme.chart.tx;
  const chartNeutral = theme.chart.neutral;

  const appendLatestTraffic = useEffectEvent(() => {
    const now = Date.now();
    rxSeriesRef.current.append(now, rx);
    txSeriesRef.current.append(now, tx);
  });

  // Initialize chart once on mount
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: 40,
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

    chart.addTimeSeries(rxSeriesRef.current, {
      strokeStyle: rxColor,
      fillStyle: alpha(rxColor, 0.09),
      lineWidth: 2,
    });
    chart.addTimeSeries(txSeriesRef.current, {
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
  }, [chartNeutral, rxColor, txColor]);

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
