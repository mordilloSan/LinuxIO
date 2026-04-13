import React, { useEffect, useRef } from "react";
import { SmoothieChart, TimeSeries } from "smoothie";

import SmoothieCanvas from "@/components/charts/SmoothieCanvas";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

interface DriveGraphProps {
  readBytesPerSec: number;
  writeBytesPerSec: number;
}

function formatThroughput(bytesPerSec: number): string {
  if (!isFinite(bytesPerSec) || bytesPerSec <= 0) return "0 B/s";
  if (bytesPerSec >= 1024 * 1024 * 1024) {
    return `${(bytesPerSec / (1024 * 1024 * 1024)).toFixed(1)} GB/s`;
  }
  if (bytesPerSec >= 1024 * 1024) {
    return `${(bytesPerSec / (1024 * 1024)).toFixed(bytesPerSec >= 10 * 1024 * 1024 ? 0 : 1)} MB/s`;
  }
  if (bytesPerSec >= 1024) {
    return `${(bytesPerSec / 1024).toFixed(bytesPerSec >= 10 * 1024 ? 0 : 1)} kB/s`;
  }
  return `${bytesPerSec.toFixed(0)} B/s`;
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
  const readRef = useRef(readBytesPerSec);
  const writeRef = useRef(writeBytesPerSec);
  const readColor = theme.chart.rx;
  const writeColor = theme.chart.tx;
  const neutral = theme.chart.neutral;

  useEffect(() => {
    readRef.current = readBytesPerSec;
    writeRef.current = writeBytesPerSec;
  }, [readBytesPerSec, writeBytesPerSec]);

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
      const now = Date.now();
      readSeriesRef.current.append(now, readRef.current);
      writeSeriesRef.current.append(now, writeRef.current);
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
        ref={canvasRef}
        chartRef={chartRef}
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
