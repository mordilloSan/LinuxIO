import React from "react";

import "./app-grid.css";

// Frozen compatibility wrapper: do not introduce new usages.

type GridSize =
  | number
  | {
      xs?: number;
      sm?: number;
      md?: number;
      lg?: number;
      xl?: number;
    };

export interface AppGridProps extends React.HTMLAttributes<HTMLElement> {
  container?: boolean;
  spacing?: number;
  size?: GridSize;
  alignItems?: React.CSSProperties["alignItems"];
  component?: React.ElementType;
  /** Allow extra props to pass through to the underlying component (e.g. motion props). */
  [key: string]: unknown;
}

function AppGrid(
  {
    container,
    spacing,
    size,
    alignItems,
    component: Component = "div",
    children,
    className,
    style,
    ...rest
  }: AppGridProps,
  ref: React.Ref<HTMLElement>,
) {
  if (container) {
    const cls = ["app-grid", className].filter(Boolean).join(" ");
    return (
      <Component
        ref={ref}
        className={cls}
        style={{
          gap: spacing ? spacing * 4 : undefined,
          alignItems,
          ...style,
        }}
        {...rest}
      >
        {children}
      </Component>
    );
  }

  // Item mode
  let sizeVars: React.CSSProperties | undefined;
  if (size != null) {
    if (typeof size === "number") {
      sizeVars = {
        "--_gc-xs": size,
        "--_gc-sm": size,
        "--_gc-md": size,
        "--_gc-lg": size,
        "--_gc-xl": size,
      } as React.CSSProperties;
    } else {
      const xs = size.xs ?? 12;
      const sm = size.sm ?? xs;
      const md = size.md ?? sm;
      const lg = size.lg ?? md;
      const xl = size.xl ?? lg;
      sizeVars = {
        "--_gc-xs": xs,
        "--_gc-sm": sm,
        "--_gc-md": md,
        "--_gc-lg": lg,
        "--_gc-xl": xl,
      } as React.CSSProperties;
    }
  }

  const cls = ["app-grid-item", className].filter(Boolean).join(" ");

  return (
    <Component
      ref={ref}
      className={cls}
      style={{ ...sizeVars, ...style }}
      {...rest}
    >
      {children}
    </Component>
  );
}

const ForwardedAppGrid = React.forwardRef(AppGrid);
ForwardedAppGrid.displayName = "AppGrid";

export default ForwardedAppGrid;
