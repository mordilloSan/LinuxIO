import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";

export interface HardwareTableCardProps {
  children: ReactNode;
}

const HardwareTableCard = ({ children }: HardwareTableCardProps) => (
  <FrostedCard style={{ padding: 0, marginBottom: 16, overflow: "hidden" }}>
    {children}
  </FrostedCard>
);

export default HardwareTableCard;
