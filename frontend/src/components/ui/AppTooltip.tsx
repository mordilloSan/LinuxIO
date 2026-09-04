import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";

import type { ToastMeta } from "@/types/navigation";
import { copyToClipboard } from "@/utils/clipboard";
import { isTabNavigationActive } from "@/utils/tabNavigation";
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
  children: ReactNode;
  className?: string;
  contentWidth?: boolean;
  copyErrorMessage?: ReactNode;
  copySuccessMessage?: ReactNode;
  copyText?: string;
  onlyWhenTruncated?: boolean;
  placement?: TooltipPlacement;
  title: ReactNode;
  toastMeta?: ToastMeta;
}

const AppTooltipTriggerContext = createContext(false);

export const useIsInsideAppTooltip = () => useContext(AppTooltipTriggerContext);

// Distance (px) from the trigger edge to the tooltip bubble — matches MUI default.
const OFFSET = 8;
const OFFSET_BOTTOM = 12;
const VIEWPORT_MARGIN = 8;

function calcStyle(placement: TooltipPlacement, rect: DOMRect): CSSProperties {
  const midX = rect.left + rect.width / 2;
  const midY = rect.top + rect.height / 2;

  // Uses the CSS `translate` property (not `transform`) so that it doesn't
  // conflict with the entrance slide applied via `transform` in the CSS.
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

const AppTooltip = ({
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
}: AppTooltipProps) => {
  const [visible, setVisible] = useState(false);
  const [canCopy, setCanCopy] = useState(false);
  const [tooltipStyle, setTooltipStyle] = useState<CSSProperties>({});
  const wrapperRef = useRef<HTMLSpanElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
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
    // Re-arming has to cancel first. A trigger can be entered and focused
    // within the same 100ms, and overwriting the handle orphaned the earlier
    // timer: hide() only ever holds the newest one, so the orphan fired after
    // the pointer had already left and put the bubble back on a page with
    // nothing left to dismiss it.
    if (enterTimer.current) clearTimeout(enterTimer.current);
    enterTimer.current = setTimeout(() => {
      enterTimer.current = null;
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
    enterTimer.current = null;
    setVisible(false);
  }, []);

  const handleFocus = useCallback(
    (event: FocusEvent<HTMLSpanElement>) => {
      // Focus tooltips follow the same policy as focus rings: only explicit Tab
      // navigation opts in. Pointer focus, Escape, and programmatic restoration
      // leave the bubble hidden. Text-entry controls remain excluded.
      if (
        isTabNavigationActive() &&
        event.target.matches(":not(input, textarea, select, [contenteditable])")
      ) {
        show();
      }
    },
    [show],
  );

  const handleClick = useCallback(async () => {
    if (!copyText || !refreshCopyAvailability()) return;

    try {
      await copyToClipboard(copyText);
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
    if (!copyText) return undefined;

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
  }, [copyText, getTarget, refreshCopyAvailability]);

  useEffect(() => {
    if (!visible) return undefined;

    window.addEventListener("scroll", handleReposition, true);
    window.addEventListener("resize", handleReposition);

    return () => {
      window.removeEventListener("scroll", handleReposition, true);
      window.removeEventListener("resize", handleReposition);
    };
  }, [visible]);

  // The initial style anchors the bubble to its trigger. Once it is in the
  // portal we can measure its rendered dimensions and nudge that anchor back
  // into the viewport when it would otherwise overflow an edge.
  useLayoutEffect(() => {
    if (!visible) return;

    const tooltip = tooltipRef.current;
    if (!tooltip) return;

    const rect = tooltip.getBoundingClientRect();
    // jsdom and hidden elements have no layout box to clamp. Waiting for a
    // measurable box also avoids repeatedly applying an offset to (0, 0).
    if (rect.width === 0 && rect.height === 0) return;

    const horizontalOffset =
      rect.left < VIEWPORT_MARGIN
        ? VIEWPORT_MARGIN - rect.left
        : rect.right > window.innerWidth - VIEWPORT_MARGIN
          ? window.innerWidth - VIEWPORT_MARGIN - rect.right
          : 0;
    const verticalOffset =
      rect.top < VIEWPORT_MARGIN
        ? VIEWPORT_MARGIN - rect.top
        : rect.bottom > window.innerHeight - VIEWPORT_MARGIN
          ? window.innerHeight - VIEWPORT_MARGIN - rect.bottom
          : 0;

    if (horizontalOffset === 0 && verticalOffset === 0) return;

    setTooltipStyle({
      ...tooltipStyle,
      left: (Number(tooltipStyle.left) || 0) + horizontalOffset,
      top: (Number(tooltipStyle.top) || 0) + verticalOffset,
    });
  }, [tooltipStyle, visible]);

  if (!title) return <>{children}</>;

  return (
    <>
      <span
        className={[
          "app-tooltip-trigger",
          copyText && canCopy && "app-tooltip-trigger--copy",
        ]
          .filter(Boolean)
          .join(" ")}
        onBlur={hide}
        onFocus={handleFocus}
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
            ref={tooltipRef}
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
