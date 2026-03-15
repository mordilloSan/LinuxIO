import React, { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import "./app-tooltip.css";

type TooltipPlacement =
  | "top"
  | "top-start"
  | "top-end"
  | "bottom"
  | "bottom-start"
  | "bottom-end"
  | "left"
  | "left-start"
  | "left-end"
  | "right"
  | "right-start"
  | "right-end";

export interface AppTooltipProps {
  title: React.ReactNode;
  children: React.ReactNode;
  arrow?: boolean;
  placement?: TooltipPlacement;
  className?: string;
}

// Distance (px) from the trigger edge to the tooltip bubble — matches MUI default.
const OFFSET = 8;

function calcStyle(
  placement: TooltipPlacement,
  rect: DOMRect,
): React.CSSProperties {
  const midX = rect.left + rect.width / 2;
  const midY = rect.top + rect.height / 2;

  switch (placement) {
    case "bottom":
      return {
        top: rect.bottom + OFFSET,
        left: midX,
        transform: "translateX(-50%)",
      };
    case "bottom-start":
      return { top: rect.bottom + OFFSET, left: rect.left };
    case "bottom-end":
      return {
        top: rect.bottom + OFFSET,
        left: rect.right,
        transform: "translateX(-100%)",
      };
    case "top":
      return {
        top: rect.top - OFFSET,
        left: midX,
        transform: "translateX(-50%) translateY(-100%)",
      };
    case "top-start":
      return {
        top: rect.top - OFFSET,
        left: rect.left,
        transform: "translateY(-100%)",
      };
    case "top-end":
      return {
        top: rect.top - OFFSET,
        left: rect.right,
        transform: "translateX(-100%) translateY(-100%)",
      };
    case "left":
      return {
        top: midY,
        left: rect.left - OFFSET,
        transform: "translateX(-100%) translateY(-50%)",
      };
    case "left-start":
      return {
        top: rect.top,
        left: rect.left - OFFSET,
        transform: "translateX(-100%)",
      };
    case "left-end":
      return {
        top: rect.bottom,
        left: rect.left - OFFSET,
        transform: "translateX(-100%) translateY(-100%)",
      };
    case "right":
      return {
        top: midY,
        left: rect.right + OFFSET,
        transform: "translateY(-50%)",
      };
    case "right-start":
      return { top: rect.top, left: rect.right + OFFSET };
    case "right-end":
      return {
        top: rect.bottom,
        left: rect.right + OFFSET,
        transform: "translateY(-100%)",
      };
  }
}

const AppTooltip: React.FC<AppTooltipProps> = ({
  title,
  children,
  arrow = false,
  placement = "bottom",
  className,
}) => {
  const [visible, setVisible] = useState(false);
  const [tooltipStyle, setTooltipStyle] = useState<React.CSSProperties>({});
  const wrapperRef = useRef<HTMLSpanElement>(null);
  const enterTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const show = useCallback(() => {
    enterTimer.current = setTimeout(() => {
      const node = wrapperRef.current;
      if (!node) return;
      setTooltipStyle(calcStyle(placement, node.getBoundingClientRect()));
      setVisible(true);
    }, 100);
  }, [placement]);

  const hide = useCallback(() => {
    if (enterTimer.current) clearTimeout(enterTimer.current);
    setVisible(false);
  }, []);

  useEffect(
    () => () => {
      if (enterTimer.current) clearTimeout(enterTimer.current);
    },
    [],
  );

  if (!title) return <>{children}</>;

  return (
    <>
      <span
        ref={wrapperRef}
        className="app-tooltip-trigger"
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
      >
        {children}
      </span>
      {visible &&
        createPortal(
          <div
            role="tooltip"
            className={[
              "app-tooltip",
              `app-tooltip--${placement}`,
              arrow && "app-tooltip--arrow",
              className,
            ]
              .filter(Boolean)
              .join(" ")}
            style={tooltipStyle}
          >
            {title}
          </div>,
          document.body,
        )}
    </>
  );
};

AppTooltip.displayName = "AppTooltip";

export default AppTooltip;
