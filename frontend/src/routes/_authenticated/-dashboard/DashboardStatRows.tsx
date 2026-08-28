import { Icon } from "@iconify/react";
import type { CSSProperties, ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

export interface DashboardStatRow {
  label: string;
  onEdit?: () => void;
  rowStyle?: CSSProperties;
  value: ReactNode;
  valueStyle?: CSSProperties;
  valueTitle?: string;
}

interface DashboardStatRowsProps {
  containerStyle?: CSSProperties;
  rows: readonly DashboardStatRow[];
}

const DashboardStatRows = ({
  containerStyle,
  rows,
}: DashboardStatRowsProps) => {
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "flex-start",
        width: "fit-content",
        ...containerStyle,
      }}
    >
      {rows.map(
        (
          {
            label,
            onEdit,
            rowStyle: customRowStyle,
            value,
            valueStyle,
            valueTitle,
          },
          index,
          items,
        ) => {
          const rowStyle: CSSProperties = {
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: "var(--app-space-2)",
            paddingBottom: "var(--app-space-2)",
            borderBottom:
              index === items.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
            gap: "var(--app-space-4)",
            cursor: onEdit ? "pointer" : undefined,
            ...customRowStyle,
          };
          const rowContent = (
            <>
              <AppTypography
                color="text.secondary"
                style={{
                  textTransform: "uppercase",
                  letterSpacing: "0.06em",
                  flexShrink: 0,
                }}
                variant="caption"
              >
                {label}
              </AppTypography>
              <AppTypography
                fontWeight={500}
                noWrap
                style={valueStyle}
                title={valueTitle}
                variant="body2"
              >
                {value}
              </AppTypography>
              {onEdit && (
                <Icon
                  height={13}
                  icon="mdi:pencil-outline"
                  style={{
                    color: "var(--app-palette-text-secondary)",
                    flexShrink: 0,
                    alignSelf: "center",
                    opacity: 0.7,
                  }}
                  width={13}
                />
              )}
            </>
          );

          return onEdit ? (
            <AppButton
              aria-label={`Edit ${label}`}
              key={label}
              onClick={onEdit}
              style={{ ...rowStyle, paddingInline: 0, textAlign: "left" }}
            >
              {rowContent}
            </AppButton>
          ) : (
            <div key={label} style={rowStyle}>
              {rowContent}
            </div>
          );
        },
      )}
    </div>
  );
};

export default DashboardStatRows;
