import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface SensorEmptyCardProps {
  message?: React.ReactNode;
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
