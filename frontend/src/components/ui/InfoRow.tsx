import { Icon } from "@iconify/react";
import type { CSSProperties, ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

import "./info-row.css";

interface InfoRowProps {
  /** list only: where the value sits; "end" pushes it to the right edge. */
  align?: "start" | "end";
  children: ReactNode;
  label: string;
  noBorder?: boolean;
  /** list only: renders the row as an "Edit <label>" button with a pencil. */
  onEdit?: () => void;
  /** list/detail: render children as-is instead of in the value text style. */
  raw?: boolean;
  /** list only: extra styles for the row element. */
  rowStyle?: CSSProperties;
  /** detail only: label and value share the row 50/50. */
  split?: boolean;
  /**
   * inline: compact row, value right-aligned (default).
   * list: roomy row for summary cards and stat lists.
   * detail: tight row, fixed label column, free-form children.
   */
  variant?: "inline" | "list" | "detail";
  /** list only: extra styles for the value text. */
  valueStyle?: CSSProperties;
  /** list only: tooltip for the value text. */
  valueTitle?: string;
  /** inline/list: let the value wrap instead of truncating. */
  wrap?: boolean;
}

const LABEL_STYLE: CSSProperties = {
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  flexShrink: 0,
};

const InfoRow = ({
  align = "end",
  children,
  label,
  noBorder = false,
  onEdit,
  raw = false,
  rowStyle,
  split = false,
  variant = "inline",
  valueStyle,
  valueTitle,
  wrap = false,
}: InfoRowProps) => {
  if (variant === "detail") {
    return (
      <div
        className={[
          "info-row-detail",
          "svc-detail-row",
          noBorder && "info-row-detail--no-border",
          split && "info-row-detail--split",
        ]
          .filter(Boolean)
          .join(" ")}
      >
        <AppTypography
          className="info-row-detail__label"
          component="span"
          noWrap={split}
          title={split ? label : undefined}
          variant="caption"
        >
          {label}
        </AppTypography>
        <div className="info-row-detail__value">{children}</div>
      </div>
    );
  }

  if (variant === "list") {
    const labelNode = (
      <AppTypography
        color="text.secondary"
        style={LABEL_STYLE}
        variant="caption"
      >
        {label}
      </AppTypography>
    );
    const value = raw ? (
      children
    ) : (
      <AppTypography
        align={align === "end" ? "right" : undefined}
        fontWeight={500}
        noWrap={!wrap}
        style={align === "end" ? { width: "100%", ...valueStyle } : valueStyle}
        title={valueTitle}
        variant="body2"
      >
        {children}
      </AppTypography>
    );
    const content = (
      <>
        {labelNode}
        {align === "end" ? (
          <div className="info-row__value">{value}</div>
        ) : (
          value
        )}
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
    const className = [
      "info-row",
      "info-row--list",
      noBorder && "info-row--no-border",
      onEdit && "info-row--edit",
    ]
      .filter(Boolean)
      .join(" ");

    return onEdit ? (
      <AppButton
        aria-label={`Edit ${label}`}
        className={className}
        onClick={onEdit}
        style={rowStyle}
      >
        {content}
      </AppButton>
    ) : (
      <div className={className} style={rowStyle}>
        {content}
      </div>
    );
  }

  return (
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
          ...LABEL_STYLE,
          fontSize: "0.62rem",
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
};

/** Free-form label/value row used inside detail panels (the "detail" variant). */
export const DetailRow = (
  props: Omit<InfoRowProps, "variant" | "align" | "onEdit" | "raw">,
) => <InfoRow {...props} variant="detail" />;

export default InfoRow;
