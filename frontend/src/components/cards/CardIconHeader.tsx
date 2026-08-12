import { memo, type CSSProperties, type ReactNode } from "react";

import AppTypography from "@/components/ui/AppTypography";
import { GAP_SM } from "@/theme/constants";

export interface CardIconHeaderProps {
  icon: ReactNode;
  /** Content rendered on the right side (chips, buttons, dropdowns…). */
  right?: ReactNode;
  style?: CSSProperties;
  subtitle?: ReactNode;
  title: string;
  /** Content rendered inline, immediately after the title. */
  titleSuffix?: ReactNode;
}

const CardIconHeaderTitle = memo(function CardIconHeaderTitle({
  title,
}: {
  title: string;
}) {
  return (
    <AppTypography
      fontWeight={700}
      noWrap
      style={{ lineHeight: 1.2 }}
      variant="subtitle1"
    >
      {title}
    </AppTypography>
  );
});

const CardIconHeader = ({
  icon,
  title,
  subtitle,
  titleSuffix,
  right,
  style,
}: CardIconHeaderProps) => (
  <div
    style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "space-between",
      ...style,
    }}
  >
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: GAP_SM,
        minWidth: 0,
      }}
    >
      <div
        style={{
          width: 40,
          height: 40,
          borderRadius: 8,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        {icon}
      </div>
      <div style={{ minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: GAP_SM }}>
          <CardIconHeaderTitle title={title} />
          {titleSuffix}
        </div>
        {subtitle !== undefined && (
          <AppTypography color="text.secondary" noWrap variant="caption">
            {subtitle}
          </AppTypography>
        )}
      </div>
    </div>
    {right}
  </div>
);

export default CardIconHeader;
