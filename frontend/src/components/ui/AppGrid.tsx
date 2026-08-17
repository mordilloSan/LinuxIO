import type { HTMLAttributes } from "react";
import {
  forwardRef,
  type CSSProperties,
  type ElementType,
  type Ref,
} from "react";

import { BASE_SPACING_UNIT } from "@/theme";

import "./app-grid.css";

export type GridSize =
  | number
  | {
      xs?: number;
      sm?: number;
      md?: number;
      lg?: number;
      xl?: number;
    };

export interface AppGridProps extends HTMLAttributes<HTMLElement> {
  /** Allow extra props to pass through to the underlying component (e.g. motion props). */
  [key: string]: unknown;
  alignItems?: CSSProperties["alignItems"];
  component?: ElementType;
  /** Number of equal-width columns at each breakpoint when used as a container. */
  columns?: GridSize;
  container?: boolean;
  size?: GridSize;
  spacing?: number;
}

function AppGrid(
  {
    container,
    columns,
    spacing,
    size,
    alignItems,
    component: Component = "div",
    children,
    className,
    style,
    ...rest
  }: AppGridProps,
  ref: Ref<HTMLElement>,
) {
  if (container) {
    const cls = ["app-grid", className].filter(Boolean).join(" ");
    const columnVars =
      columns == null
        ? undefined
        : typeof columns === "number"
          ? {
              "--_gcols-xs": columns,
              "--_gcols-sm": columns,
              "--_gcols-md": columns,
              "--_gcols-lg": columns,
              "--_gcols-xl": columns,
            }
          : {
              "--_gcols-xs": columns.xs ?? 12,
              "--_gcols-sm": columns.sm ?? columns.xs ?? 12,
              "--_gcols-md": columns.md ?? columns.sm ?? columns.xs ?? 12,
              "--_gcols-lg":
                columns.lg ?? columns.md ?? columns.sm ?? columns.xs ?? 12,
              "--_gcols-xl":
                columns.xl ??
                columns.lg ??
                columns.md ??
                columns.sm ??
                columns.xs ??
                12,
            };
    return (
      <Component
        className={cls}
        ref={ref}
        style={{
          gap: spacing ? spacing * BASE_SPACING_UNIT : undefined,
          alignItems,
          ...columnVars,
          ...style,
        }}
        {...rest}
      >
        {children}
      </Component>
    );
  }

  // Item mode
  let sizeVars: CSSProperties | undefined;
  if (size != null) {
    if (typeof size === "number") {
      sizeVars = {
        "--_gc-xs": size,
        "--_gc-sm": size,
        "--_gc-md": size,
        "--_gc-lg": size,
        "--_gc-xl": size,
      } as CSSProperties;
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
      } as CSSProperties;
    }
  }

  const cls = ["app-grid-item", className].filter(Boolean).join(" ");

  return (
    <Component
      className={cls}
      ref={ref}
      style={{ ...sizeVars, ...style }}
      {...rest}
    >
      {children}
    </Component>
  );
}

const ForwardedAppGrid = forwardRef(AppGrid);
ForwardedAppGrid.displayName = "AppGrid";

export default ForwardedAppGrid;
