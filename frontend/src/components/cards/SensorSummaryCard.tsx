import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

export interface SensorSummaryCardProps {
  adapters: number;
  readings: number;
  maxTemp: number | null;
}

const SensorSummaryCard: React.FC<SensorSummaryCardProps> = ({
  adapters,
  readings,
  maxTemp,
}) => (
  <FrostedCard
    style={{
      paddingInline: 12,
      paddingBlock: 8,
      display: "flex",
      alignItems: "center",
      gap: 12,
      flexWrap: "wrap",
    }}
  >
    <Chip
      size="small"
      label={`${adapters} Adapter${adapters !== 1 ? "s" : ""}`}
      color="primary"
      variant="soft"
    />
    <Chip
      size="small"
      label={`${readings} Reading${readings !== 1 ? "s" : ""}`}
      color="default"
      variant="soft"
    />
    {maxTemp != null && (
      <Chip
        size="small"
        label={`Peak Temp: ${maxTemp}°C`}
        color={maxTemp >= 75 ? "error" : maxTemp >= 50 ? "warning" : "success"}
        variant="soft"
      />
    )}
  </FrostedCard>
);

export interface SensorEmptyCardProps {
  message?: string;
}

export const SensorEmptyCard: React.FC<SensorEmptyCardProps> = ({
  message = (
    <>
      No sensor data available. Ensure <code>lm-sensors</code> is installed and
      configured.
    </>
  ),
}) => (
  <FrostedCard style={{ padding: 16, textAlign: "center" }}>
    <AppTypography variant="body2" color="text.secondary">
      {message}
    </AppTypography>
  </FrostedCard>
);

export default SensorSummaryCard;
