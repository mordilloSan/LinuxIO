import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

import SmoothieCanvas from "@/components/charts/SmoothieCanvas";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface CpuGraphProps {
  usage: number;
}

const CpuGraph: React.FC<CpuGraphProps> = ({ usage }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const seriesRef = useRef<TimeSeries>(new TimeSeries());
  const usageRef = useRef(usage);
  const theme = useAppTheme();
  const color = theme.palette.primary.main;
  const neutral = theme.chart.neutral;

  useEffect(() => {
    usageRef.current = usage;
  }, [usage]);

  // Initialize chart once on mount
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: 40,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: alpha(neutral, 0.15),
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      tooltip: true,
      tooltipLine: {
        strokeStyle: alpha(neutral, 0.4),
        lineWidth: 1,
      },
      tooltipFormatter: (
        _timestamp: number,
        data: { series: TimeSeries; index: number; value: number }[],
      ) => {
        return data
          .map(
            (d) =>
              `<span style="color:${color}">CPU: ${d.value.toFixed(1)}%</span>`,
          )
          .join("");
      },
      responsive: true,
      minValue: 0,
      maxValue: 100,
    });

    chart.addTimeSeries(seriesRef.current, {
      strokeStyle: color,
      fillStyle: `${color}18`,
      lineWidth: 2,
    });

    chart.streamTo(canvas, 2000);
    chartRef.current = chart;

    const intervalId = setInterval(() => {
      seriesRef.current.append(Date.now(), usageRef.current);
    }, 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
    };
  }, [color, neutral]);

  return (
    <div
      style={{ width: "100%", height: "100%", display: "flex", minWidth: 0 }}
    >
      <SmoothieCanvas
        ref={canvasRef}
        chartRef={chartRef}
        style={{ flex: 1, minWidth: 0, height: "100%" }}
      />
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          paddingLeft: 4,
          fontSize: 9,
          color: alpha(theme.chart.neutral, 0.7),
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
