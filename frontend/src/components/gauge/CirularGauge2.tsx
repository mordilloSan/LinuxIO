import React, { useMemo } from "react";

// Utility functions
function clamp(n: number, min: number, max: number) {
  return Math.min(max, Math.max(min, n));
}

// ============================================
// 1. Multi-Value Circular Gauge
// ============================================
interface MultiValueGaugeProps {
  values: Array<{ value: number; color: string; label?: string }>;
  size?: number;
  thickness?: number;
  gap?: number; // gap between segments in degrees
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

  // Calculate total value for percentage calculation
  const total = useMemo(
    () => values.reduce((sum, item) => sum + Math.max(0, item.value), 0),
    [values]
  );

  // Calculate segments with gaps
  const segments = useMemo(() => {
    let currentOffset = 0;
    const totalGapDegrees = gap * (values.length - 1);
    const availableDegrees = 360 - totalGapDegrees;

    return values.map((item, index) => {
      const percentage = total > 0 ? (item.value / total) * 100 : 0;
      const degrees = (percentage / 100) * availableDegrees;
      const strokeDasharray = `${(degrees / 360) * circumference} ${circumference}`;
      const rotation = currentOffset - 90; // Start from top

      currentOffset += degrees + gap;

      return {
        ...item,
        percentage: Math.round(percentage),
        strokeDasharray,
        rotation,
      };
    });
  }, [values, total, circumference, gap]);

  return (
    <div className="relative inline-flex">
      <svg width={size} height={size}>
        {/* Background circle */}
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke="#e0e0e0"
          strokeWidth={thickness}
        />
        {/* Value segments */}
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
      {/* Center label showing total */}
      <div className="absolute inset-0 flex items-center justify-center flex-col">
        <div className="text-2xl font-bold">{Math.round(total)}</div>
        <div className="text-xs text-gray-500">Total</div>
      </div>
    </div>
  );
};

// ============================================
// 2. Gradient Circular Gauge
// ============================================
interface GradientGaugeProps {
  value: number; // 0..100
  gradientColors?: string[]; // array of color stops
  size?: number;
  thickness?: number;
  showPercentage?: boolean;
}

export const GradientCircularGauge: React.FC<GradientGaugeProps> = ({
  value,
  gradientColors = ["#82ca9d", "#f39c12", "#e74c3c"],
  size = 120,
  thickness = 8,
  showPercentage = true,
}) => {
  const pct = clamp(value, 0, 100);
  const radius = (size - thickness) / 2;
  const circumference = 2 * Math.PI * radius;
  const strokeDasharray = `${(pct / 100) * circumference} ${circumference}`;
  const center = size / 2;
  const gradientId = useMemo(() => `gradient-${Math.random().toString(36).substr(2, 9)}`, []);

  return (
    <div className="relative inline-flex">
      <svg width={size} height={size}>
        {/* Define gradient */}
        <defs>
          <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
            {gradientColors.map((color, index) => (
              <stop
                key={index}
                offset={`${(index / (gradientColors.length - 1)) * 100}%`}
                stopColor={color}
              />
            ))}
          </linearGradient>
        </defs>
        {/* Background circle */}
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke="#e0e0e0"
          strokeWidth={thickness}
        />
        {/* Gradient progress circle */}
        <circle
          cx={center}
          cy={center}
          r={radius}
          fill="none"
          stroke={`url(#${gradientId})`}
          strokeWidth={thickness}
          strokeDasharray={strokeDasharray}
          strokeLinecap="round"
          transform={`rotate(-90 ${center} ${center})`}
          style={{ transition: "stroke-dasharray 0.3s ease" }}
        />
      </svg>
      {/* Center label */}
      {showPercentage && (
        <div className="absolute inset-0 flex items-center justify-center">
          <div className="text-2xl font-bold">{Math.round(pct)}%</div>
        </div>
      )}
    </div>
  );
};