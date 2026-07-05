import React, { useEffect, useEffectEvent, useRef } from "react";
import { SmoothieChart } from "smoothie";

import { linuxio } from "@/api";
import LiveChartHover from "@/components/charts/LiveChartHover";
import {
  appendLiveSample,
  LIVE_MILLIS_PER_PIXEL,
  sampleLiveSeries,
} from "@/components/charts/liveSeriesStore";
import type { LiveTooltipRow } from "@/components/charts/liveTooltip";
import { useLiveSeries } from "@/components/charts/useLiveSeries";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";
import { formatThroughput } from "@/utils/formaters";

interface DriveGraphProps {
  readBytesPerSec: number;
  writeBytesPerSec: number;
}

const READ_ID = "disk:read";
const WRITE_ID = "disk:write";
const STREAM_DELAY_MS = 1000;

const DriveGraph: React.FC<DriveGraphProps> = ({
  readBytesPerSec,
  writeBytesPerSec,
}) => {
  const theme = useAppTheme();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [readSeries, writeSeries] = useLiveSeries(
    [READ_ID, WRITE_ID],
    async (request) => {
      const points = await linuxio.monitoring.get_diskio_history(request);
      return {
        [READ_ID]: points.map((point) => ({
          t: point.captured_at_ms,
          v: point.read_bytes_per_sec,
        })),
        [WRITE_ID]: points.map((point) => ({
          t: point.captured_at_ms,
          v: point.write_bytes_per_sec,
        })),
      };
    },
  );
  const readColor = theme.chart.rx;
  const writeColor = theme.chart.tx;
  const neutral = theme.chart.neutral;

  const appendLatestThroughput = useEffectEvent(() => {
    appendLiveSample(READ_ID, readBytesPerSec);
    appendLiveSample(WRITE_ID, writeBytesPerSec);
  });

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const chart = new SmoothieChart({
      millisPerPixel: LIVE_MILLIS_PER_PIXEL,
      interpolation: "bezier",
      grid: {
        fillStyle: "transparent",
        strokeStyle: alpha(neutral, 0.15),
        verticalSections: 4,
        millisPerLine: 0,
        borderVisible: false,
      },
      labels: { disabled: true },
      responsive: true,
      minValue: 0,
      maxValueScale: 1.15,
    });

    chart.addTimeSeries(readSeries, {
      strokeStyle: readColor,
      fillStyle: alpha(readColor, 0.09),
      lineWidth: 2,
    });
    chart.addTimeSeries(writeSeries, {
      strokeStyle: writeColor,
      fillStyle: alpha(writeColor, 0.09),
      lineWidth: 2,
    });

    chart.streamTo(canvas, STREAM_DELAY_MS);

    const intervalId = setInterval(() => {
      appendLatestThroughput();
    }, 1000);

    return () => {
      clearInterval(intervalId);
      chart.stop();
    };
  }, [neutral, readColor, writeColor, readSeries, writeSeries]);

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
      <div
        style={{ width: "100%", flex: 1, minHeight: 0, position: "relative" }}
      >
        <canvas
          ref={canvasRef}
          style={{ width: "100%", height: "100%", display: "block" }}
        />
        <LiveChartHover
          delayMs={STREAM_DELAY_MS}
          rowsAt={(t) => {
            const rows: LiveTooltipRow[] = [];
            const readValue = sampleLiveSeries(readSeries, t);
            if (readValue !== null) {
              rows.push({
                color: readColor,
                value: formatThroughput(readValue),
                label: "Read",
              });
            }
            const writeValue = sampleLiveSeries(writeSeries, t);
            if (writeValue !== null) {
              rows.push({
                color: writeColor,
                value: formatThroughput(writeValue),
                label: "Write",
              });
            }
            return rows;
          }}
        />
      </div>
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
