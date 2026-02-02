import { useTheme } from "@mui/material";
import { grey } from "@mui/material/colors";
import React, { useMemo } from "react";

// Utility functions
function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n));
}

// Helper to interpolate between colors
function interpolateColor(
  color1: string,
  color2: string,
  factor: number,
): string {
  const c1 = parseInt(color1.slice(1), 16);
  const c2 = parseInt(color2.slice(1), 16);

  const r1 = (c1 >> 16) & 255;
  const g1 = (c1 >> 8) & 255;
  const b1 = c1 & 255;

  const r2 = (c2 >> 16) & 255;
  const g2 = (c2 >> 8) & 255;
  const b2 = c2 & 255;

  const r = Math.round(r1 + (r2 - r1) * factor);
  const g = Math.round(g1 + (g2 - g1) * factor);
  const b = Math.round(b1 + (b2 - b1) * factor);

  return `#${((1 << 24) + (r << 16) + (g << 8) + b).toString(16).slice(1)}`;
}

// Get color based on percentage value
function getColorForPercentage(pct: number, colors: string[]): string {
  if (colors.length === 0) return "#000000";
  if (colors.length === 1) return colors[0];

  const segmentSize = 100 / (colors.length - 1);
  const segmentIndex = Math.floor(pct / segmentSize);

  if (segmentIndex >= colors.length - 1) return colors[colors.length - 1];

  const localPercent = (pct % segmentSize) / segmentSize;
  return interpolateColor(
    colors[segmentIndex],
    colors[segmentIndex + 1],
    localPercent,
  );
}

// ============================================
// 1. Multi-Value Circular Gauge
// ============================================
interface MultiValueGaugeProps {
  values: { value: number; color: string; label?: string }[];
  size?: number;
  thickness?: number;
  gap?: number;
}

export const MultiValueCircularGauge: React.FC<MultiValueGaugeProps> = ({
  values,
  size = 120,
  thickness = 8,
  gap = 2,
}) => {
  const radius = (size - thickness) / 2;
  const circumference = 2 * Math.PI * radius;
  const center = size / 2;
  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";

  const total = useMemo(
    () => values.reduce((sum, item) => sum + Math.max(0, item.value), 0),
    [values],
  );

  const segments = useMemo(() => {
    const totalGapDegrees = gap * Math.max(0, values.length - 1);
    const availableDegrees = 360 - totalGapDegrees;

    // Precompute degrees and percentages per item
    const data = values.map((item) => {
      const clamped = Math.max(0, item.value);
      const percentage = total > 0 ? (clamped / total) * 100 : 0;
      const degrees = (percentage / 100) * availableDegrees;
      return { item, percentage, degrees };
    });

    // Build segments with an immutable running offset (no reassignments)
    interface Seg {
      value: number;
      color: string;
      label?: string;
      percentage: number;
      strokeDasharray: string;
      rotation: number;
    }

    const result = data.reduce(
      (acc, { item, percentage, degrees }) => {
        const rotation = acc.offset - 90;
        const strokeDasharray = `${(degrees / 360) * circumference} ${circumference}`;
        acc.list.push({
          ...item,
          percentage: Math.round(percentage),
          strokeDasharray,
          rotation,
        } as Seg);
        return { list: acc.list, offset: acc.offset + degrees + gap };
      },
      { list: [] as Seg[], offset: 0 },
    );

    return result.list;
  }, [values, total, circumference, gap]);

  return (
    <div
      style={{
        position: "relative",
        display: "inline-flex",
        width: size,
        height: size,
      }}
    >
      <svg width={size} height={size}>
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke={isDark ? grey[700] : grey[300]}
          strokeWidth={thickness}
        />
        {segments.map((segment, index) => (
          <circle
            key={index}
            cx={center}
            cy={center}
            r={radius}
            fill="none"
            stroke={segment.color}
            strokeWidth={thickness}
            strokeDasharray={segment.strokeDasharray}
            strokeLinecap="round"
            transform={`rotate(${segment.rotation} ${center} ${center})`}
            style={{ transition: "stroke-dasharray 0.3s ease" }}
          />
        ))}
      </svg>
      <div
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexDirection: "column",
        }}
      >
        <div
          style={{
            fontSize: "1.5rem",
            fontWeight: "bold",
            color: isDark ? grey[100] : grey[900],
          }}
        >
          {Math.round(total)}
        </div>
        <div style={{ fontSize: "0.75rem", color: grey[500] }}>Total</div>
      </div>
    </div>
  );
};

// ============================================
// 2. Gradient Circular Gauge (Arc-based)
// ============================================
interface GradientGaugeProps {
  value: number;
  gradientColors?: string[];
  size?: number;
  thickness?: number;
  showPercentage?: boolean;
}

export const GradientCircularGauge: React.FC<GradientGaugeProps> = ({
  value,
  gradientColors = ["#82ca9d", "#f39c12", "#e74c3c"],
  size = 120,
  thickness = 12,
  showPercentage = true,
}) => {
  const pct = clamp(value, 0, 100);
  const radius = (size - thickness) / 2;
  const circumference = 2 * Math.PI * radius;
  const center = size / 2;

  const theme = useTheme();
  const isDark = theme.palette.mode === "dark";
  const backgroundColor = isDark ? grey[700] : grey[300];

  // Create multiple segments for smooth gradient effect
  const segments = useMemo(() => {
    const numSegments = 100;
    const segmentAngle = 360 / numSegments;
    const filledSegments = Math.ceil((pct / 100) * numSegments);

    return Array.from({ length: filledSegments }, (_, i) => {
      const segmentPct = ((i + 1) / numSegments) * 100;
      const color = getColorForPercentage(segmentPct, gradientColors);
      const rotation = i * segmentAngle - 90;
      const strokeDasharray = `${(segmentAngle / 360) * circumference * 0.99} ${circumference}`;

      return { color, rotation, strokeDasharray };
    });
  }, [pct, gradientColors, circumference]);

  return (
    <div
      style={{
        position: "relative",
        display: "inline-flex",
        width: size,
        height: size,
      }}
    >
      <svg width={size} height={size}>
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke={backgroundColor}
          strokeWidth={thickness}
        />
        {segments.map((segment, index) => (
          <circle
            key={index}
            cx={center}
            cy={center}
            r={radius}
            fill="none"
            stroke={segment.color}
            strokeWidth={thickness}
            strokeDasharray={segment.strokeDasharray}
            strokeLinecap="round"
            transform={`rotate(${segment.rotation} ${center} ${center})`}
          />
        ))}
      </svg>
      {showPercentage && (
        <div
          style={{
            position: "absolute",
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <div
            style={{
              fontSize: "1rem",
              fontWeight: "bold",
              color: isDark ? grey[100] : grey[900],
            }}
          >
            {Math.round(pct)}%
          </div>
        </div>
      )}
    </div>
  );
};
