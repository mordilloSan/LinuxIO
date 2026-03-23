import React from "react";

import "./app-typography.css";

// Frozen compatibility wrapper: do not introduce new usages.

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

export interface AppTypographyProps extends Omit<
  React.HTMLAttributes<HTMLElement>,
  "color"
> {
  variant?: Variant;
  color?: SemanticColor | (string & {});
  fontWeight?: number | string;
  fontSize?: string | number;
  noWrap?: boolean;
  align?: "left" | "center" | "right" | "justify";
  gutterBottom?: boolean;
  component?: React.ElementType;
  children?: React.ReactNode;
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
      className,
      style,
      children,
      ...rest
    },
    ref,
  ) => {
    const Tag = (component ?? VARIANT_ELEMENT[variant]) as React.ElementType;

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

    return (
      <Tag ref={ref} className={cls} style={merged} {...rest}>
        {children}
      </Tag>
    );
  },
);

AppTypography.displayName = "AppTypography";

export default AppTypography;
