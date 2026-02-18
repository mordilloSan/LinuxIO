import React, { useEffect, useImperativeHandle, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

interface NetworkTrafficGraphProps {
  value: number;
  color: string;
  label: string;
}

const NetworkTrafficGraph = React.forwardRef<
  HTMLCanvasElement,
  NetworkTrafficGraphProps
>(({ value, color, label }, ref) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const seriesRef = useRef<TimeSeries>(new TimeSeries());
  const valueRef = useRef(value);

  useImperativeHandle(ref, () => canvasRef.current!);

  // Keep the ref in sync with the latest prop
  useEffect(() => {
    valueRef.current = value;
  }, [value]);

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
              `<span style="color:${color}">${label}: ${(d.value / 1024).toFixed(1)} kB/s</span>`,
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

  return <canvas ref={canvasRef} style={{ width: "100%", height: "100%" }} />;
});

NetworkTrafficGraph.displayName = "NetworkTrafficGraph";

export default NetworkTrafficGraph;
