import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";

export interface HardwareTableCardProps {
  children: React.ReactNode;
}

const HardwareTableCard: React.FC<HardwareTableCardProps> = ({ children }) => (
  <FrostedCard style={{ padding: 0, marginBottom: 16, overflow: "hidden" }}>
    {children}
  </FrostedCard>
);

export default HardwareTableCard;
