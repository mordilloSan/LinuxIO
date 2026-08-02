import { Icon } from "@iconify/react";
import type { CSSProperties, ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

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
  const theme = useAppTheme();

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
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === items.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
            gap: theme.spacing(1),
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
                  fontSize: "0.62rem",
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
                    color: theme.palette.text.secondary,
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
