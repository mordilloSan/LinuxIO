import {
  createContext,
  useContext,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type ReactNode,
  type RefObject,
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

// Automatic tooltips are suppressed inside explicit tooltips and fast table cells.
export const AppTooltipSuppressionContext = createContext(false);

export const useAutomaticTooltipSuppressed = () =>
  useContext(AppTooltipSuppressionContext);

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

function tooltipTarget(wrapper: HTMLSpanElement | null) {
  return wrapper?.firstElementChild ?? wrapper;
}

interface TooltipContentProps extends Pick<
  AppTooltipProps,
  | "title"
  | "arrow"
  | "placement"
  | "className"
  | "contentWidth"
  | "copyText"
  | "onlyWhenTruncated"
> {
  request: number;
  wrapperRef: RefObject<HTMLSpanElement | null>;
  onCopyAvailabilityChange: (canCopy: boolean) => void;
  onClose: () => void;
}

function TooltipContent({
  title,
  arrow = false,
  placement = "bottom",
  className,
  contentWidth = false,
  copyText,
  onlyWhenTruncated = false,
  request,
  wrapperRef,
  onCopyAvailabilityChange,
  onClose,
}: TooltipContentProps) {
  const [tooltipStyle, setTooltipStyle] = useState<CSSProperties | null>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const visible = tooltipStyle !== null;

  const updatePosition = useEffectEvent(() => {
    const target = tooltipTarget(wrapperRef.current);
    if (!target) return;

    const isTruncated =
      (copyText || onlyWhenTruncated) && hasTruncatedContent(target);
    onCopyAvailabilityChange(Boolean(copyText && isTruncated));
    if (onlyWhenTruncated && !isTruncated) {
      onClose();
      return;
    }
    setTooltipStyle(calcStyle(placement, target.getBoundingClientRect()));
  });

  useEffect(() => {
    const timer = setTimeout(updatePosition, 100);
    return () => clearTimeout(timer);
    // A repeated enter/focus restarts the delay even when content is unchanged.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [request]);

  useEffect(() => {
    if (!visible) return undefined;

    window.addEventListener("scroll", updatePosition, true);
    window.addEventListener("resize", updatePosition);

    return () => {
      window.removeEventListener("scroll", updatePosition, true);
      window.removeEventListener("resize", updatePosition);
    };
  }, [visible]);

  // The initial style anchors the bubble to its trigger. Once it is in the
  // portal we can measure its rendered dimensions and nudge that anchor back
  // into the viewport when it would otherwise overflow an edge.
  useLayoutEffect(() => {
    if (!tooltipStyle) return;

    const tooltip = tooltipRef.current;
    if (!tooltip) return;

    const rect = tooltip.getBoundingClientRect();
    // jsdom and hidden elements have no layout box to clamp. Waiting for a
    // measurable box also avoids repeatedly applying an offset to (0, 0).
    if (rect.width === 0 && rect.height === 0) return;

    // Oversized bubbles cannot satisfy both edges. Keep the leading edge
    // visible instead of alternating between opposite overflow corrections.
    const maxLeft = Math.max(
      VIEWPORT_MARGIN,
      window.innerWidth - VIEWPORT_MARGIN - rect.width,
    );
    const maxTop = Math.max(
      VIEWPORT_MARGIN,
      window.innerHeight - VIEWPORT_MARGIN - rect.height,
    );
    const horizontalOffset =
      Math.min(Math.max(rect.left, VIEWPORT_MARGIN), maxLeft) - rect.left;
    const verticalOffset =
      Math.min(Math.max(rect.top, VIEWPORT_MARGIN), maxTop) - rect.top;

    // Browser layout rounds subpixels; chasing a tiny residual can keep this
    // effect updating even though the rendered position does not change.
    if (Math.abs(horizontalOffset) < 0.5 && Math.abs(verticalOffset) < 0.5)
      return;

    setTooltipStyle({
      ...tooltipStyle,
      left: (Number(tooltipStyle.left) || 0) + horizontalOffset,
      top: (Number(tooltipStyle.top) || 0) + verticalOffset,
    });
  }, [tooltipStyle]);

  if (!tooltipStyle) return null;

  return createPortal(
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
  );
}

const AppTooltip = ({
  title,
  children,
  copyText,
  copySuccessMessage = "Copied to clipboard",
  copyErrorMessage = "Failed to copy",
  toastMeta,
  ...contentProps
}: AppTooltipProps) => {
  const [request, setRequest] = useState<number | null>(null);
  const [canCopy, setCanCopy] = useState(false);
  const wrapperRef = useRef<HTMLSpanElement>(null);

  function refreshCopyAvailability() {
    const target = tooltipTarget(wrapperRef.current);
    const nextCanCopy = Boolean(
      copyText && target && hasTruncatedContent(target),
    );
    setCanCopy(nextCanCopy);
    return nextCanCopy;
  }

  function show() {
    refreshCopyAvailability();
    setRequest((current) => (current ?? 0) + 1);
  }

  function hide() {
    setRequest(null);
  }

  function handleFocus(event: FocusEvent<HTMLSpanElement>) {
    // Pointer focus and programmatic restoration do not open tooltips.
    if (
      isTabNavigationActive() &&
      event.target.matches(":not(input, textarea, select, [contenteditable])")
    ) {
      show();
    }
  }

  async function handleClick() {
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
  }

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
        <AppTooltipSuppressionContext.Provider value>
          {children}
        </AppTooltipSuppressionContext.Provider>
      </span>
      {/* Keep the trigger in place while interaction mounts the costly work. */}
      {request !== null && (
        <TooltipContent
          {...contentProps}
          copyText={copyText}
          onClose={hide}
          onCopyAvailabilityChange={setCanCopy}
          request={request}
          title={title}
          wrapperRef={wrapperRef}
        />
      )}
    </>
  );
};

AppTooltip.displayName = "AppTooltip";

export default AppTooltip;
