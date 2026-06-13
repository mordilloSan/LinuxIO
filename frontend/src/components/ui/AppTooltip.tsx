import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";

import type { ToastMeta } from "@/contexts/ToastContext";
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
  arrow?: boolean;
  children: React.ReactNode;
  className?: string;
  contentWidth?: boolean;
  copyErrorMessage?: React.ReactNode;
  copySuccessMessage?: React.ReactNode;
  copyText?: string;
  onlyWhenTruncated?: boolean;
  placement?: TooltipPlacement;
  title: React.ReactNode;
  toastMeta?: ToastMeta;
}

const AppTooltipTriggerContext = React.createContext(false);

export const useIsInsideAppTooltip = () =>
  React.useContext(AppTooltipTriggerContext);

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

function hasTruncatedContent(element: Element): boolean {
  if (element instanceof HTMLElement) {
    const hasOverflowX = element.scrollWidth > element.clientWidth + 1;
    const hasOverflowY = element.scrollHeight > element.clientHeight + 1;
    if (hasOverflowX || hasOverflowY) return true;
  }

  return Array.from(element.children).some(hasTruncatedContent);
}

const AppTooltip: React.FC<AppTooltipProps> = ({
  title,
  children,
  arrow = false,
  placement = "bottom",
  className,
  contentWidth = false,
  copyText,
  copySuccessMessage = "Copied to clipboard",
  copyErrorMessage = "Failed to copy",
  onlyWhenTruncated = false,
  toastMeta,
}) => {
  const [visible, setVisible] = useState(false);
  const [canCopy, setCanCopy] = useState(false);
  const [tooltipStyle, setTooltipStyle] = useState<React.CSSProperties>({});
  const wrapperRef = useRef<HTMLSpanElement>(null);
  const enterTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const getTarget = useCallback(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return null;

    return (wrapper.firstElementChild as HTMLElement | null) ?? wrapper;
  }, []);

  const isTargetTruncated = useCallback(() => {
    const target = getTarget();
    return target ? hasTruncatedContent(target) : false;
  }, [getTarget]);

  const refreshCopyAvailability = useCallback(() => {
    const nextCanCopy = Boolean(copyText && isTargetTruncated());
    setCanCopy((current) => (current === nextCanCopy ? current : nextCanCopy));
    return nextCanCopy;
  }, [copyText, isTargetTruncated]);

  const shouldShowTooltip = useCallback(() => {
    if (!onlyWhenTruncated) return true;

    return isTargetTruncated();
  }, [isTargetTruncated, onlyWhenTruncated]);

  const updatePosition = useCallback(() => {
    const target = getTarget();
    if (!target) return;

    setTooltipStyle(calcStyle(placement, target.getBoundingClientRect()));
  }, [getTarget, placement]);

  const show = useCallback(() => {
    refreshCopyAvailability();
    enterTimer.current = setTimeout(() => {
      if (!shouldShowTooltip()) {
        setVisible(false);
        return;
      }

      updatePosition();
      setVisible(true);
    }, 100);
  }, [refreshCopyAvailability, shouldShowTooltip, updatePosition]);

  const hide = useCallback(() => {
    if (enterTimer.current) clearTimeout(enterTimer.current);
    setVisible(false);
  }, []);

  const handleClick = useCallback(async () => {
    if (!copyText || !refreshCopyAvailability()) return;

    try {
      await navigator.clipboard.writeText(copyText);
      toast.success(
        copySuccessMessage,
        toastMeta ? { meta: toastMeta } : undefined,
      );
    } catch {
      toast.error(
        copyErrorMessage,
        toastMeta ? { meta: toastMeta } : undefined,
      );
    }
  }, [
    copyErrorMessage,
    copySuccessMessage,
    copyText,
    refreshCopyAvailability,
    toastMeta,
  ]);

  useEffect(
    () => () => {
      if (enterTimer.current) clearTimeout(enterTimer.current);
    },
    [],
  );

  const handleReposition = useEffectEvent(() => {
    refreshCopyAvailability();

    if (!shouldShowTooltip()) {
      setVisible(false);
      return;
    }

    updatePosition();
  });

  useEffect(() => {
    const target = getTarget();
    if (!target) return undefined;

    window.addEventListener("resize", refreshCopyAvailability);

    if (typeof ResizeObserver === "undefined") {
      return () => {
        window.removeEventListener("resize", refreshCopyAvailability);
      };
    }

    const observer = new ResizeObserver(refreshCopyAvailability);
    observer.observe(target);

    return () => {
      observer.disconnect();
      window.removeEventListener("resize", refreshCopyAvailability);
    };
  }, [getTarget, refreshCopyAvailability]);

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
        className={[
          "app-tooltip-trigger",
          canCopy && "app-tooltip-trigger--copy",
        ]
          .filter(Boolean)
          .join(" ")}
        onBlur={hide}
        onFocus={show}
        onClick={handleClick}
        onMouseEnter={show}
        onMouseLeave={hide}
        ref={wrapperRef}
      >
        <AppTooltipTriggerContext.Provider value>
          {children}
        </AppTooltipTriggerContext.Provider>
      </span>
      {visible &&
        createPortal(
          <div
            className={[
              "app-tooltip",
              `app-tooltip--${placement}`,
              arrow && "app-tooltip--arrow",
              contentWidth && "app-tooltip--content-width",
              className,
            ]
              .filter(Boolean)
              .join(" ")}
            role="tooltip"
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
