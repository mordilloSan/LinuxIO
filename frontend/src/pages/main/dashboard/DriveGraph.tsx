import React, { useEffect, useEffectEvent, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

import SmoothieCanvas from "@/components/charts/SmoothieCanvas";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatThroughput } from "@/utils/formaters";

interface DriveGraphProps {
  readBytesPerSec: number;
  writeBytesPerSec: number;
}

const DriveGraph: React.FC<DriveGraphProps> = ({
  readBytesPerSec,
  writeBytesPerSec,
}) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const chartRef = useRef<SmoothieChart | null>(null);
  const readSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const writeSeriesRef = useRef<TimeSeries>(new TimeSeries());
  const readColor = theme.chart.rx;
  const writeColor = theme.chart.tx;
  const neutral = theme.chart.neutral;

  const appendLatestThroughput = useEffectEvent(() => {
    const now = Date.now();
    readSeriesRef.current.append(now, readBytesPerSec);
    writeSeriesRef.current.append(now, writeBytesPerSec);
  });

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
      tooltipLine: { strokeStyle: alpha(neutral, 0.4), lineWidth: 1 },
      tooltipFormatter: (
        _timestamp: number,
        data: { series: TimeSeries; index: number; value: number }[],
      ) => {
        const labels = ["Read", "Write"];
        const colors = [readColor, writeColor];
        return data
          .map(
            (point, index) =>
              `<span style="color:${colors[index]}; font-size: 13px; line-height: 1.3;">${labels[index]}: ${formatThroughput(point.value)}</span>`,
          )
          .join("<br/>");
      },
      responsive: true,
      minValue: 0,
      maxValueScale: 1.15,
    });

    chart.addTimeSeries(readSeriesRef.current, {
      strokeStyle: readColor,
      fillStyle: alpha(readColor, 0.09),
      lineWidth: 2,
    });
    chart.addTimeSeries(writeSeriesRef.current, {
      strokeStyle: writeColor,
      fillStyle: alpha(writeColor, 0.09),
      lineWidth: 2,
    });

    chart.streamTo(canvas, 1000);
    chartRef.current = chart;

    const intervalId = setInterval(() => {
      appendLatestThroughput();
    }, 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
    };
  }, [neutral, readColor, writeColor]);

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
          gap: 8,
        }}
      >
        <div style={{ color: readColor, fontWeight: 600 }}>
          Read: {formatThroughput(readBytesPerSec)}
        </div>
        <div style={{ color: writeColor, fontWeight: 600 }}>
          Write: {formatThroughput(writeBytesPerSec)}
        </div>
      </div>
    </div>
  );
};

export default DriveGraph;
