import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

interface NetworkTrafficGraphProps {
  value: number;
  color: string;
  label: string;
  dataUpdatedAt: number;
}

const STREAM_DELAY = 1000;

const NetworkTrafficGraph: React.FC<NetworkTrafficGraphProps> = ({
  value,
  color,
  label,
  dataUpdatedAt,
}) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const seriesRef = useRef<TimeSeries>(new TimeSeries());

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: 40,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: "rgba(128, 128, 128, 0.08)",
        verticalSections: 3,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      tooltip: true,
      tooltipLine: { strokeStyle: "rgba(128, 128, 128, 0.3)", lineWidth: 1 },
      tooltipFormatter: (
        _timestamp: number,
        data: { series: TimeSeries; index: number; value: number }[],
      ) =>
        data
          .map(
            (d) =>
              `<span style="color:${color}">${label}: ${d.value.toFixed(2)} kB/s</span>`,
          )
          .join(""),
      responsive: true,
      minValue: 0,
    });

    chart.addTimeSeries(seriesRef.current, {
      strokeStyle: color,
      fillStyle: `${color}18`,
      lineWidth: 1.5,
    });

    chart.streamTo(canvas, STREAM_DELAY);
    chartRef.current = chart;

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
  }, [color, label]);

  // Append a data point every time the query delivers fresh data
  useEffect(() => {
    if (dataUpdatedAt > 0) {
      seriesRef.current.append(Date.now(), value);
    }
  }, [dataUpdatedAt]);

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
          position: "absolute",
          bottom: 4,
          right: 8,
          fontSize: 11,
          opacity: 0.7,
          display: "flex",
          alignItems: "center",
          gap: 5,
        }}
      >
        <div
          style={{
            width: 7,
            height: 7,
            backgroundColor: color,
            borderRadius: "50%",
          }}
        />
        {label}: {value.toFixed(1)} kB/s
      </div>
    </div>
  );
};

export default NetworkTrafficGraph;
