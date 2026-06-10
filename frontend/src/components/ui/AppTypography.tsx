import React from "react";

import AppTooltip, { useIsInsideAppTooltip } from "@/components/ui/AppTooltip";
import type { ToastMeta } from "@/contexts/ToastContext";

import "./app-typography.css";

type Variant =
  | "h1"
  | "h2"
  | "h3"
  | "h4"
  | "h5"
  | "h6"
  | "overline"
  | "subtitle1"
  | "subtitle2"
  | "body1"
  | "body2"
  | "caption";

type SemanticColor =
  | "text.primary"
  | "text.secondary"
  | "text.disabled"
  | "error"
  | "success"
  | "warning"
  | "inherit";

const VARIANT_ELEMENT: Record<Variant, keyof React.JSX.IntrinsicElements> = {
  h1: "h1",
  h2: "h2",
  h3: "h3",
  h4: "h4",
  h5: "h5",
  h6: "h6",
  overline: "span",
  subtitle1: "h6",
  subtitle2: "h6",
  body1: "p",
  body2: "p",
  caption: "span",
};

const COLOR_MAP: Record<SemanticColor, string> = {
  "text.primary": "var(--app-palette-text-primary)",
  "text.secondary": "var(--app-palette-text-secondary)",
  "text.disabled": "var(--app-palette-text-disabled)",
  error: "var(--app-palette-error-main)",
  success: "var(--app-palette-success-main)",
  warning: "var(--app-palette-warning-main)",
  inherit: "inherit",
};

const getPlainText = (node: React.ReactNode): string => {
  if (typeof node === "string" || typeof node === "number") {
    return String(node).trim();
  }

  if (Array.isArray(node)) {
    return node.map(getPlainText).filter(Boolean).join(" ").trim();
  }

  return "";
};

export interface AppTypographyProps extends Omit<
  React.HTMLAttributes<HTMLElement>,
  "color"
> {
  align?: "left" | "center" | "right" | "justify";
  children?: React.ReactNode;
  color?: SemanticColor | (string & {});
  component?: React.ElementType;
  copyErrorMessage?: React.ReactNode;
  copySuccessMessage?: React.ReactNode;
  copyText?: string;
  fontSize?: string | number;
  fontWeight?: number | string;
  gutterBottom?: boolean;
  noWrap?: boolean;
  toastMeta?: ToastMeta;
  tooltipOnlyWhenTruncated?: boolean;
  variant?: Variant;
}

const AppTypography = React.forwardRef<HTMLElement, AppTypographyProps>(
  (
    {
      variant = "body1",
      color,
      fontWeight,
      fontSize,
      noWrap,
      align,
      gutterBottom,
      component,
      copyErrorMessage,
      copySuccessMessage,
      copyText,
      className,
      style,
      toastMeta,
      tooltipOnlyWhenTruncated = true,
      children,
      title,
      ...rest
    },
    ref,
  ) => {
    const Tag = (component ?? VARIANT_ELEMENT[variant]) as React.ElementType;
    const isInsideTooltip = useIsInsideAppTooltip();

    const resolvedColor = color
      ? (COLOR_MAP[color as SemanticColor] ?? color)
      : undefined;

    const cls = [
      "app-typo",
      `app-typo--${variant}`,
      noWrap && "app-typo--nowrap",
      gutterBottom && "app-typo--gutter",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    const merged: React.CSSProperties = {
      ...(resolvedColor && { color: resolvedColor }),
      ...(fontWeight != null && { fontWeight: fontWeight as number }),
      ...(fontSize != null && { fontSize }),
      ...(align && { textAlign: align }),
      ...style,
    };

    const tooltipText =
      typeof title === "string" && title.trim()
        ? title.trim()
        : getPlainText(children);
    const showTruncatedTooltip = Boolean(
      noWrap && tooltipText && !isInsideTooltip,
    );
    const tagProps = showTruncatedTooltip ? rest : { ...rest, title };
    const element = (
      <Tag className={cls} ref={ref} style={merged} {...tagProps}>
        {children}
      </Tag>
    );

    if (!showTruncatedTooltip) {
      return element;
    }

    return (
      <AppTooltip
        contentWidth
        copyErrorMessage={copyErrorMessage}
        copySuccessMessage={copySuccessMessage}
        copyText={copyText}
        onlyWhenTruncated={tooltipOnlyWhenTruncated}
        title={tooltipText}
        toastMeta={toastMeta}
      >
        {element}
      </AppTooltip>
    );
  },
);

AppTypography.displayName = "AppTypography";

export default AppTypography;
