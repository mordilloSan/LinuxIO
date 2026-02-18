import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

interface NetworkTrafficGraphProps {
  value: number;
  color: string;
  label: string;
}

const NetworkTrafficGraph: React.FC<NetworkTrafficGraphProps> = ({
  value,
  color,
  label,
}) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const seriesRef = useRef<TimeSeries>(new TimeSeries());
  const valueRef = useRef(value);

  // Always keep the ref in sync with the latest prop
  valueRef.current = value;

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
      maxValueScale: 1.15,
    });

    chart.addTimeSeries(seriesRef.current, {
      strokeStyle: color,
      fillStyle: `${color}18`,
      lineWidth: 1.5,
    });

    chart.streamTo(canvas, 1000);
    chartRef.current = chart;

    // Append a data point every second on a fixed interval,
    // completely decoupled from React's render cycle.
    const intervalId = setInterval(() => {
      seriesRef.current.append(Date.now(), valueRef.current);
    }, 1000);

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
  }, [color, label]);

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
