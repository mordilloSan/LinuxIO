import React from "react";

import AppTypography from "@/components/ui/AppTypography";
import "@/theme/section.css";

interface InfoRowProps {
  children: React.ReactNode;
  label: string;
  noBorder?: boolean;
  wrap?: boolean;
}

const InfoRow: React.FC<InfoRowProps> = ({
  label,
  children,
  noBorder = false,
  wrap = false,
}) => (
  <div
    className="info-row"
    style={{
      ...(noBorder && { borderBottom: "none" }),
      ...(wrap && { alignItems: "flex-start" }),
    }}
  >
    <AppTypography
      color="text.secondary"
      style={{
        textTransform: "uppercase",
        letterSpacing: "0.06em",
        fontSize: "0.62rem",
        flexShrink: 0,
        ...(wrap && { paddingTop: 2 }),
      }}
      variant="caption"
    >
      {label}
    </AppTypography>
    <AppTypography
      fontWeight={500}
      noWrap={!wrap}
      style={{
        textAlign: "right",
        ...(wrap && { whiteSpace: "normal", overflowWrap: "anywhere" }),
      }}
      variant="body2"
    >
      {children}
    </AppTypography>
  </div>
);

export default InfoRow;
