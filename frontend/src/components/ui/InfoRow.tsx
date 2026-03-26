import React from "react";

import AppTypography from "@/components/ui/AppTypography";

import "@/theme/section.css";

interface InfoRowProps {
  label: string;
  children: React.ReactNode;
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
      variant="caption"
      color="text.secondary"
      style={{
        textTransform: "uppercase",
        letterSpacing: "0.06em",
        fontSize: "0.62rem",
        flexShrink: 0,
        ...(wrap && { paddingTop: 2 }),
      }}
    >
      {label}
    </AppTypography>
    <AppTypography
      variant="body2"
      fontWeight={500}
      noWrap={!wrap}
      style={{
        textAlign: "right",
        ...(wrap && { whiteSpace: "normal", overflowWrap: "anywhere" }),
      }}
    >
      {children}
    </AppTypography>
  </div>
);

export default InfoRow;
