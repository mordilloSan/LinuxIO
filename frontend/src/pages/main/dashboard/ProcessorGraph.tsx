import { useTheme } from "@mui/material/styles";
import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

interface CpuGraphProps {
  usage: number;
}

const CpuGraph: React.FC<CpuGraphProps> = ({ usage }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const seriesRef = useRef<TimeSeries>(new TimeSeries());
  const theme = useTheme();
  const color = theme.palette.primary.main;

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
      tooltipFormatter: (_timestamp: number, data: { series: TimeSeries; index: number; value: number }[]) => {
        return data
          .map((d) => `<span style="color:${color}">CPU: ${d.value.toFixed(1)}%</span>`)
          .join("");
      },
      responsive: true,
      minValue: 0,
      maxValue: 100,
    });

    chart.addTimeSeries(seriesRef.current, {
      strokeStyle: color,
      lineWidth: 2,
    });

    chart.streamTo(canvas, 2000);
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
  }, [color]);

  // Append data points when value changes
  useEffect(() => {
    seriesRef.current.append(Date.now(), usage);
  }, [usage]);

  return (
    <div style={{ width: "100%", height: "100%", display: "flex", minWidth: 0 }}>
      <canvas
        ref={canvasRef}
        style={{ flex: 1, minWidth: 0, height: "100%" }}
      />
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          paddingLeft: 4,
          fontSize: 9,
          color: "rgba(128, 128, 128, 0.7)",
          whiteSpace: "nowrap",
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
