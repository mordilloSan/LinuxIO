import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface SensorEmptyCardProps {
  message?: ReactNode;
}

export const SensorEmptyCard = ({
  message = (
    <>
      No sensor data available. Ensure <code>lm-sensors</code> is installed and
      configured.
    </>
  ),
}: SensorEmptyCardProps) => (
  <FrostedCard style={{ padding: 16, textAlign: "center" }}>
    <AppTypography color="text.secondary" variant="body2">
      {message}
    </AppTypography>
  </FrostedCard>
);
