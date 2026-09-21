import {
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type Ref,
} from "react";
import { createPortal } from "react-dom";

import { targetAllowsContextMenu } from "@/utils/contextMenu";
import { mergeRefs } from "@/utils/mergeRefs";

import "./app-popover.css";

type VerticalOrigin = "top" | "center" | "bottom";
type HorizontalOrigin = "left" | "center" | "right";

export interface AppPopoverOrigin {
  horizontal: HorizontalOrigin;
  vertical: VerticalOrigin;
}

export interface AppPopoverProps {
  anchorEl?: HTMLElement | null;
  anchorOrigin?: AppPopoverOrigin;
  anchorPosition?: { top: number; left: number } | null;
  children: ReactNode;
  className?: string;
  keepMounted?: boolean;
  matchAnchorWidth?: boolean;
  onClose?: () => void;
  open: boolean;
  paperClassName?: string;
  paperRef?: Ref<HTMLDivElement>;
  paperStyle?: CSSProperties;
  style?: CSSProperties;
  transformOrigin?: AppPopoverOrigin;
  zIndex?: number;
}

const DEFAULT_ORIGIN: AppPopoverOrigin = {
  vertical: "top",
  horizontal: "left",
};

const VIEWPORT_MARGIN = 8;

const getHorizontalOffset = (origin: HorizontalOrigin, width: number) => {
  switch (origin) {
    case "center":
      return width / 2;
    case "right":
      return width;
    default:
      return 0;
  }
};

const getVerticalOffset = (origin: VerticalOrigin, height: number) => {
  switch (origin) {
    case "center":
      return height / 2;
    case "bottom":
      return height;
    default:
      return 0;
  }
};

const AppPopover = ({
  open,
  onClose,
  anchorEl,
  anchorPosition,
  anchorOrigin = DEFAULT_ORIGIN,
  transformOrigin = DEFAULT_ORIGIN,
  matchAnchorWidth = false,
  keepMounted = false,
  children,
  className,
  paperClassName,
  style,
  paperStyle,
  paperRef,
  zIndex = 1400,
}: AppPopoverProps) => {
  const internalPaperRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({
    top: -9999,
    left: -9999,
    width: 0,
  });
  const positionTop = anchorPosition?.top;
  const positionLeft = anchorPosition?.left;
  const closeDetachedAnchor = useEffectEvent(() => onClose?.());

  const setPaperRef = useMemo(
    () => mergeRefs(internalPaperRef, paperRef),
    [paperRef],
  );

  useLayoutEffect(() => {
    const paper = internalPaperRef.current;
    if (!open || !paper) {
      return;
    }

    const updatePosition = () => {
      if (anchorEl && !anchorEl.isConnected) {
        closeDetachedAnchor();
        return;
      }
      const anchorRect = anchorEl?.getBoundingClientRect();
      const anchorBox = anchorRect
        ? anchorRect
        : positionTop !== undefined && positionLeft !== undefined
          ? ({
              top: positionTop,
              left: positionLeft,
              right: positionLeft,
              bottom: positionTop,
              width: 0,
              height: 0,
            } as DOMRect)
          : null;

      if (!anchorBox) {
        return;
      }

      const paperRect = paper.getBoundingClientRect();
      const anchorLeft =
        anchorBox.left +
        getHorizontalOffset(anchorOrigin.horizontal, anchorBox.width);
      const anchorTop =
        anchorBox.top +
        getVerticalOffset(anchorOrigin.vertical, anchorBox.height);

      let nextLeft =
        anchorLeft -
        getHorizontalOffset(transformOrigin.horizontal, paperRect.width);
      let nextTop =
        anchorTop -
        getVerticalOffset(transformOrigin.vertical, paperRect.height);

      nextLeft = Math.min(
        Math.max(nextLeft, VIEWPORT_MARGIN),
        window.innerWidth - paperRect.width - VIEWPORT_MARGIN,
      );
      nextTop = Math.min(
        Math.max(nextTop, VIEWPORT_MARGIN),
        window.innerHeight - paperRect.height - VIEWPORT_MARGIN,
      );

      const width = matchAnchorWidth ? anchorBox.width : 0;
      setPosition((previous) =>
        previous.top === nextTop &&
        previous.left === nextLeft &&
        previous.width === width
          ? previous
          : { top: nextTop, left: nextLeft, width },
      );
    };

    updatePosition();
    const rafId = window.requestAnimationFrame(updatePosition);
    const observer = new ResizeObserver(updatePosition);
    observer.observe(paper);
    if (anchorEl) observer.observe(anchorEl);
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);

    return () => {
      window.cancelAnimationFrame(rafId);
      observer.disconnect();
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [
    anchorEl,
    anchorOrigin.horizontal,
    anchorOrigin.vertical,
    matchAnchorWidth,
    open,
    positionLeft,
    positionTop,
    transformOrigin.horizontal,
    transformOrigin.vertical,
  ]);

  const handleDismissPointer = useEffectEvent(
    (event: MouseEvent | TouchEvent) => {
      const target = event.target;

      if (!(target instanceof Node)) {
        return;
      }

      if (internalPaperRef.current?.contains(target)) {
        return;
      }

      if (anchorEl?.contains(target)) {
        return;
      }

      onClose?.();
    },
  );

  const handleDismissContextMenu = useEffectEvent((event: MouseEvent) => {
    const target = event.target;

    if (!(target instanceof Node)) {
      return;
    }

    if (internalPaperRef.current?.contains(target)) {
      return;
    }

    if (anchorEl?.contains(target)) {
      return;
    }

    if (targetAllowsContextMenu(target)) {
      return;
    }

    onClose?.();
  });

  const handleDismissKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if (event.key !== "Escape" || event.defaultPrevented) {
      return;
    }
    // Escape in a filled text control edits the field — its own handler
    // clears it — so only an empty field lets Escape dismiss, the native
    // combobox cascade. This listener runs in the capture phase, before the
    // field could mark the event handled, so the check is on the target.
    const target = event.target;
    if (
      target instanceof Element &&
      internalPaperRef.current?.contains(target) &&
      (target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement) &&
      target.value !== ""
    ) {
      return;
    }
    onClose?.();
    event.preventDefault();
  });

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    document.addEventListener("mousedown", handleDismissPointer, true);
    document.addEventListener("touchstart", handleDismissPointer, true);
    document.addEventListener("contextmenu", handleDismissContextMenu, true);
    document.addEventListener("keydown", handleDismissKeyDown, true);

    return () => {
      document.removeEventListener("mousedown", handleDismissPointer, true);
      document.removeEventListener("touchstart", handleDismissPointer, true);
      document.removeEventListener(
        "contextmenu",
        handleDismissContextMenu,
        true,
      );
      document.removeEventListener("keydown", handleDismissKeyDown, true);
    };
  }, [open]);

  if (!open && !keepMounted) {
    return null;
  }

  return createPortal(
    <div
      className={`app-popover-root ${className || ""}`.trim()}
      style={{
        zIndex,
        ...style,
        display: open ? style?.display : "none",
      }}
    >
      <div
        className={`app-popover__paper ${paperClassName || ""}`.trim()}
        ref={setPaperRef}
        style={{
          top: position.top,
          left: position.left,
          width: position.width || undefined,
          ...paperStyle,
        }}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
};

AppPopover.displayName = "AppPopover";

export default AppPopover;
