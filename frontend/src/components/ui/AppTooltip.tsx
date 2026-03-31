import React, { useCallback, useEffect, useEffectEvent, useRef, useState } from "react";
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
const OFFSET_BOTTOM = 12;

function calcStyle(
  placement: TooltipPlacement,
  rect: DOMRect,
): React.CSSProperties {
  const midX = rect.left + rect.width / 2;
  const midY = rect.top + rect.height / 2;

  // Uses the CSS `translate` property (not `transform`) so that it doesn't
  // conflict with the scale animation applied via `transform` in the CSS.
  switch (placement) {
    case "bottom":
      return {
        top: rect.bottom + OFFSET_BOTTOM,
        left: midX,
        translate: "-50%",
      };
    case "bottom-start":
      return { top: rect.bottom + OFFSET_BOTTOM, left: rect.left };
    case "bottom-end":
      return {
        top: rect.bottom + OFFSET_BOTTOM,
        left: rect.right,
        translate: "-100%",
      };
    case "top":
      return { top: rect.top - OFFSET, left: midX, translate: "-50% -100%" };
    case "top-start":
      return { top: rect.top - OFFSET, left: rect.left, translate: "0 -100%" };
    case "top-end":
      return {
        top: rect.top - OFFSET,
        left: rect.right,
        translate: "-100% -100%",
      };
    case "left":
      return { top: midY, left: rect.left - OFFSET, translate: "-100% -50%" };
    case "left-start":
      return { top: rect.top, left: rect.left - OFFSET, translate: "-100%" };
    case "left-end":
      return {
        top: rect.bottom,
        left: rect.left - OFFSET,
        translate: "-100% -100%",
      };
    case "right":
      return { top: midY, left: rect.right + OFFSET, translate: "0 -50%" };
    case "right-start":
      return { top: rect.top, left: rect.right + OFFSET };
    case "right-end":
      return {
        top: rect.bottom,
        left: rect.right + OFFSET,
        translate: "0 -100%",
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

  const updatePosition = useCallback(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;

    const target = (wrapper.firstElementChild as HTMLElement | null) ?? wrapper;
    setTooltipStyle(calcStyle(placement, target.getBoundingClientRect()));
  }, [placement]);

  const show = useCallback(() => {
    enterTimer.current = setTimeout(() => {
      updatePosition();
      setVisible(true);
    }, 100);
  }, [updatePosition]);

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

  const handleReposition = useEffectEvent(() => {
    updatePosition();
  });

  useEffect(() => {
    if (!visible) return undefined;

    window.addEventListener("scroll", handleReposition, true);
    window.addEventListener("resize", handleReposition);

    return () => {
      window.removeEventListener("scroll", handleReposition, true);
      window.removeEventListener("resize", handleReposition);
    };
  }, [visible]);

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
