import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

interface NetworkGraphProps {
  rx: number;
  tx: number;
}

const RX_COLOR = "#8884d8";
const TX_COLOR = "#82ca9d";

const NetworkGraph: React.FC<NetworkGraphProps> = ({ rx, tx }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const rxSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const txSeriesRef = useRef<TimeSeries>(new TimeSeries());

  // Initialize chart once on mount
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: 40,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: "rgba(128, 128, 128, 0.15)",
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      tooltip: true,
      tooltipLine: { strokeStyle: "rgba(128, 128, 128, 0.4)", lineWidth: 1 },
      tooltipFormatter: (
        _timestamp: number,
        data: { series: TimeSeries; index: number; value: number }[],
      ) => {
        const labels = ["Rx", "Tx"];
        const colors = [RX_COLOR, TX_COLOR];
        return data
          .map(
            (d, i) =>
              `<span style="color:${colors[i]}">${labels[i]}: ${d.value.toFixed(2)} kB/s</span>`,
          )
          .join("<br/>");
      },
      responsive: true,
      minValue: 0,
    });

    chart.addTimeSeries(rxSeriesRef.current, {
      strokeStyle: RX_COLOR,
      lineWidth: 2,
    });
    chart.addTimeSeries(txSeriesRef.current, {
      strokeStyle: TX_COLOR,
      lineWidth: 2,
    });

    chart.streamTo(canvas, 1000);
    chartRef.current = chart;

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
      chart.stop();
      canvas.removeEventListener("mousemove", onMove);
    };
  }, []);

  // Append data points when values change
  useEffect(() => {
    const now = Date.now();
    rxSeriesRef.current.append(now, rx);
    txSeriesRef.current.append(now, tx);
  }, [rx, tx]);

  return (
    <div
      style={{
        width: "100%",
        height: "100%",
        position: "relative",
        minWidth: 0,
      }}
    >
      <canvas ref={canvasRef} style={{ width: "100%", height: "100%" }} />
      <div
        style={{
          display: "flex",
          justifyContent: "center",
          alignItems: "center",
          gap: 12,
          marginTop: 4,
          fontSize: 12,
          whiteSpace: "nowrap",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
          <div
            style={{
              width: 8,
              height: 8,
              backgroundColor: RX_COLOR,
              borderRadius: "50%",
            }}
          />
          Rx: {rx.toFixed(2)} kB/s
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
          <div
            style={{
              width: 8,
              height: 8,
              backgroundColor: TX_COLOR,
              borderRadius: "50%",
            }}
          />
          Tx: {tx.toFixed(2)} kB/s
        </div>
      </div>
    </div>
  );
};

export default NetworkGraph;
