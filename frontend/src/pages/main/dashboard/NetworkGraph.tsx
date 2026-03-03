import { alpha, useTheme } from "@mui/material/styles";
import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

interface NetworkGraphProps {
  rx: number;
  tx: number;
}

const NetworkGraph: React.FC<NetworkGraphProps> = ({ rx, tx }) => {
  const theme = useTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const rxSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const txSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const rxRef = useRef(rx);
  const txRef = useRef(tx);
  const rxColor = theme.chart.rx;
  const txColor = theme.chart.tx;
  const chartNeutral = theme.chart.neutral;

  useEffect(() => {
    rxRef.current = rx;
    txRef.current = tx;
  }, [rx, tx]);

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
              `<span style="color:${colors[i]}">${labels[i]}: ${d.value.toFixed(2)} kB/s</span>`,
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
      const now = Date.now();
      rxSeriesRef.current.append(now, rxRef.current);
      txSeriesRef.current.append(now, txRef.current);
    }, 1000);

    // Flip tooltip to the left when mouse is on the right half
    const chartAny = chart as SmoothieChart & { tooltipEl?: HTMLElement };
    const onMove = (evt: MouseEvent) => {
      const tooltip = chartAny.tooltipEl;
      if (!tooltip || tooltip.style.display === "none") return;
      const canvasRect = canvas.getBoundingClientRect();
      const mouseRelX = evt.clientX - canvasRect.left;
      if (mouseRelX > canvasRect.width / 2) {
        tooltip.style.left = `${Math.round(evt.pageX) - tooltip.offsetWidth - 10}px`;
      }
    };
    canvas.addEventListener("mousemove", onMove);

    return () => {
      clearInterval(intervalId);
      chart.stop();
      canvas.removeEventListener("mousemove", onMove);
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
      <canvas
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
          Rx: {rx.toFixed(2)} kB/s
        </div>
        <div style={{ color: txColor, fontWeight: 600 }}>
          Tx: {tx.toFixed(2)} kB/s
        </div>
      </div>
    </div>
  );
};

export default NetworkGraph;
